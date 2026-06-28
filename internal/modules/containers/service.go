package containerization

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

const (
	TaskContainerStart       = "container_start"
	TaskContainerStop        = "container_stop"
	TaskContainerRestart     = "container_restart"
	TaskContainerDelete      = "container_delete"
	TaskContainerRefresh     = "container_refresh"
	TaskImagePull            = "image_pull"
	TaskImageRefresh         = "image_refresh"
	TaskImageDelete          = "image_delete"
	TaskImageDeleteUnused    = "image_delete_unused"
	TaskImageUpgradeMany     = "application_image_upgrade_selected"
	TaskImageUpgradeAll      = "application_image_upgrade_all"
	TaskVolumeDelete         = "volume_delete"
	TaskVolumeDeleteUnused   = "volume_delete_unused"
	TaskVolumeRefresh        = "volume_refresh"
	TaskApplicationReconcile = "application_reconcile"
)

const applicationReconcileMaxBackoff = 10 * time.Minute

type AgentClient interface {
	DockerContainers(context.Context, string) ([]agentcontract.DockerContainer, error)
	DockerContainerLogs(context.Context, string, string, int) (agentcontract.DockerContainerLogsResponse, error)
	DockerContainerAction(context.Context, string, string, string) error
	DockerContainerDelete(context.Context, string, string) error
	DockerImages(context.Context, string) ([]agentcontract.DockerImage, error)
	DockerImagePull(context.Context, string, string) error
	DockerImageDelete(context.Context, string, string) error
	DockerNetworks(context.Context, string) ([]agentcontract.DockerNetwork, error)
	DockerVolumes(context.Context, string) ([]agentcontract.DockerVolume, error)
	DockerVolumeDelete(context.Context, string, string) error
}

type ServerProvider interface {
	Get(context.Context, string) (server.Server, error)
	List(context.Context) ([]server.Server, error)
}

type AgentErrorHandler interface {
	HandleAgentError(context.Context, server.Server, error) bool
}

type ApplicationUpdater interface {
	List(context.Context) ([]applications.Application, error)
	UpdateImage(context.Context, string) (applications.OperationResult, error)
	Deploy(context.Context, string) (applications.OperationResult, error)
	ReconcileDeploy(context.Context, string) (applications.OperationResult, error)
}

type Container struct {
	agentcontract.DockerContainer
	Managed       bool   `json:"managed"`
	ApplicationID string `json:"applicationId,omitempty"`
	InstanceID    string `json:"instanceId,omitempty"`
}

type ContainerLogs struct {
	ContainerID string `json:"containerId"`
	Logs        string `json:"logs"`
}

type Image struct {
	agentcontract.DockerImage
	Reference       string     `json:"reference"`
	LocalDigest     string     `json:"localDigest,omitempty"`
	LatestDigest    string     `json:"latestDigest,omitempty"`
	Checkable       bool       `json:"checkable"`
	UpdateAvailable bool       `json:"updateAvailable"`
	CheckedAt       *time.Time `json:"checkedAt,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	InUse           bool       `json:"inUse"`
	ApplicationIDs  []string   `json:"applicationIds"`
	Upgradeable     bool       `json:"upgradeable"`
}

type ImageList struct {
	ServerID        string     `json:"serverId"`
	Items           []Image    `json:"items"`
	LastRefreshedAt *time.Time `json:"lastRefreshedAt,omitempty"`
	Refreshing      bool       `json:"refreshing"`
}

type OperationResult struct {
	RefreshTaskID string `json:"refreshTaskId,omitempty"`
}

type queueJob struct {
	run    func(context.Context) error
	result chan error
}

type serverQueue struct {
	jobs chan queueJob
}

type Service struct {
	db          *sql.DB
	servers     ServerProvider
	agentErrors AgentErrorHandler
	agent       AgentClient
	tasks       *tasks.Service
	apps        ApplicationUpdater
	resolver    applications.ImageDigestResolver
	queueMu     sync.Mutex
	queues      map[string]*serverQueue
	refreshMu   sync.Mutex
	refreshing  map[string]bool
}

type Option func(*Service)

func WithApplicationUpdater(updater ApplicationUpdater) Option {
	return func(s *Service) { s.apps = updater }
}

func WithImageDigestResolver(resolver applications.ImageDigestResolver) Option {
	return func(s *Service) { s.resolver = resolver }
}

func NewService(db *sql.DB, servers ServerProvider, agentClient AgentClient, taskSvc *tasks.Service, opts ...Option) *Service {
	s := &Service{
		db: db, servers: servers, agent: agentClient, tasks: taskSvc,
		resolver: applications.NewRegistryImageResolver(),
		queues:   map[string]*serverQueue{}, refreshing: map[string]bool{},
	}
	if handler, ok := servers.(AgentErrorHandler); ok {
		s.agentErrors = handler
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) Containers(ctx context.Context, serverID string) ([]Container, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	items, err := s.agent.DockerContainers(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return nil, err
	}
	out := make([]Container, 0, len(items))
	for _, item := range items {
		appID, instanceID, managed := managedLabels(item.Labels)
		out = append(out, Container{DockerContainer: item, Managed: managed, ApplicationID: appID, InstanceID: instanceID})
	}
	return out, nil
}

func (s *Service) ContainerLogs(ctx context.Context, serverID, containerID string, tail int) (ContainerLogs, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return ContainerLogs{}, err
	}
	logs, err := s.agent.DockerContainerLogs(ctx, baseURL, containerID, normalizeLogTail(tail))
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return ContainerLogs{}, err
	}
	return ContainerLogs{ContainerID: logs.ContainerID, Logs: logs.Logs}, nil
}

func (s *Service) ContainerAction(ctx context.Context, serverID, containerID, action string) (OperationResult, error) {
	if _, ok := map[string]struct{}{"start": {}, "stop": {}, "restart": {}}[action]; !ok {
		return OperationResult{}, panelerr.Validation("container_action_invalid", "Unsupported container action")
	}
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		err := s.agent.DockerContainerAction(runCtx, baseURL, containerID, action)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
		}
		return err
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "containers")
}

func (s *Service) DeleteContainer(ctx context.Context, serverID, containerID string) (OperationResult, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		err := s.agent.DockerContainerDelete(runCtx, baseURL, containerID)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
		}
		return err
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "containers")
}

func (s *Service) Images(ctx context.Context, serverID string) (ImageList, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return ImageList{}, err
	}
	images, err := s.agent.DockerImages(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return ImageList{}, err
	}
	containers, err := s.agent.DockerContainers(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return ImageList{}, err
	}
	imageApps := map[string]map[string]struct{}{}
	inUse := map[string]bool{}
	for _, container := range containers {
		inUse[container.ImageID] = true
		appID, _, managed := managedLabels(container.Labels)
		if !managed || appID == "" {
			continue
		}
		if imageApps[container.ImageID] == nil {
			imageApps[container.ImageID] = map[string]struct{}{}
		}
		imageApps[container.ImageID][appID] = struct{}{}
	}
	cache, refreshedAt, err := s.imageCache(ctx, serverID)
	if err != nil {
		return ImageList{}, err
	}
	out := make([]Image, 0, len(images))
	for _, raw := range images {
		reference := firstTaggedReference(raw.RepoTags)
		item := Image{
			DockerImage:    raw,
			Reference:      reference,
			LocalDigest:    firstDigest(raw.RepoDigests),
			Checkable:      reference != "",
			InUse:          inUse[raw.ID],
			ApplicationIDs: sortedKeys(imageApps[raw.ID]),
		}
		item.Upgradeable = len(item.ApplicationIDs) > 0
		if cached, ok := cache[reference]; ok {
			item.LocalDigest = firstNonEmpty(item.LocalDigest, cached.LocalDigest)
			item.LatestDigest = cached.LatestDigest
			item.UpdateAvailable = cached.UpdateAvailable
			item.CheckedAt = cached.CheckedAt
			item.LastError = cached.LastError
		}
		out = append(out, item)
	}
	return ImageList{ServerID: serverID, Items: out, LastRefreshedAt: refreshedAt, Refreshing: s.isRefreshing(serverID)}, nil
}

func (s *Service) PullImage(ctx context.Context, serverID, reference string) (OperationResult, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return OperationResult{}, panelerr.Validation("image_reference_required", "Image reference is required")
	}
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		err := s.agent.DockerImagePull(runCtx, baseURL, reference)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
		}
		return err
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "images")
}

func (s *Service) DeleteImage(ctx context.Context, serverID, imageID string) (OperationResult, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		containers, err := s.agent.DockerContainers(runCtx, baseURL)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		for _, container := range containers {
			if container.ImageID == imageID {
				return panelerr.Conflict("image_in_use", "Image is in use")
			}
		}
		err = s.agent.DockerImageDelete(runCtx, baseURL, imageID)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
		}
		return err
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "images")
}

func (s *Service) DeleteUnusedImages(ctx context.Context, serverID string) (OperationResult, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		images, err := s.agent.DockerImages(runCtx, baseURL)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		containers, err := s.agent.DockerContainers(runCtx, baseURL)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		inUse := map[string]bool{}
		for _, container := range containers {
			inUse[container.ImageID] = true
		}
		var failures []string
		for _, image := range images {
			if image.ID == "" {
				continue
			}
			if inUse[image.ID] {
				continue
			}
			if err := s.agent.DockerImageDelete(runCtx, baseURL, image.ID); err != nil {
				failures = append(failures, imageReferenceLabel(image)+": "+err.Error())
				continue
			}
		}
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "images")
}

func (s *Service) Networks(ctx context.Context, serverID string) ([]agentcontract.DockerNetwork, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	items, err := s.agent.DockerNetworks(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
	}
	return items, err
}

func (s *Service) Volumes(ctx context.Context, serverID string) ([]agentcontract.DockerVolume, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	items, err := s.agent.DockerVolumes(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
	}
	return items, err
}

func (s *Service) DeleteVolume(ctx context.Context, serverID, name string) (OperationResult, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		volumes, err := s.agent.DockerVolumes(runCtx, baseURL)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		for _, volume := range volumes {
			if volume.Name == name && volume.InUse {
				return panelerr.Conflict("volume_in_use", "Volume is in use")
			}
		}
		err = s.agent.DockerVolumeDelete(runCtx, baseURL, name)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
		}
		return err
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "volumes")
}

func (s *Service) DeleteUnusedVolumes(ctx context.Context, serverID string) (OperationResult, error) {
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.Execute(ctx, serverID, func(runCtx context.Context) error {
		volumes, err := s.agent.DockerVolumes(runCtx, baseURL)
		if err != nil {
			_ = s.handleAgentError(runCtx, srv, err)
			return err
		}
		var failures []string
		for _, volume := range volumes {
			if volume.Name == "" {
				continue
			}
			if volume.InUse {
				continue
			}
			if err := s.agent.DockerVolumeDelete(runCtx, baseURL, volume.Name); err != nil {
				failures = append(failures, volume.Name+": "+err.Error())
				continue
			}
		}
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	}); err != nil {
		return OperationResult{}, err
	}
	return s.refreshOperationResult(ctx, serverID, "volumes")
}

func (s *Service) RefreshContainers(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error) {
	return s.startSimpleResourceRefresh(ctx, serverID, TaskContainerRefresh, triggerType, operationID, "Refreshing containers", "Containers refreshed", func(runCtx context.Context, baseURL string) error {
		_, err := s.agent.DockerContainers(runCtx, baseURL)
		return err
	})
}

func (s *Service) RefreshVolumes(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error) {
	return s.startSimpleResourceRefresh(ctx, serverID, TaskVolumeRefresh, triggerType, operationID, "Refreshing volumes", "Volumes refreshed", func(runCtx context.Context, baseURL string) error {
		_, err := s.agent.DockerVolumes(runCtx, baseURL)
		return err
	})
}

func (s *Service) RefreshImages(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error) {
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	if !s.markRefreshing(serverID) {
		return tasks.Task{}, panelerr.Conflict("image_refresh_running", "Image refresh is already running")
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		OperationID: operationID, Type: TaskImageRefresh, ServerID: serverID,
		ResourceType: "server", ResourceID: serverID, TriggerType: triggerType,
		Summary: "Refreshing image updates",
	}, tasks.Trigger{Type: triggerType, Periodic: triggerType == "scheduler"})
	if err != nil {
		s.clearRefreshing(serverID)
		return tasks.Task{}, err
	}
	if !created {
		s.clearRefreshing(serverID)
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		s.clearRefreshing(serverID)
		return tasks.Task{}, err
	}
	go s.runImageRefresh(s.tasks.ExecutionContext(task.ID), task, serverID)
	return task, nil
}

func (s *Service) RefreshAllScheduled(ctx context.Context) error {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	operationID := id.New("op")
	for _, srv := range servers {
		if !srv.Reachable || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			continue
		}
		if _, err := s.RefreshImages(ctx, srv.ID, "scheduler", operationID); err != nil {
			var pe *panelerr.Error
			if errors.As(err, &pe) && pe.Code == "image_refresh_running" {
				continue
			}
		}
	}
	return nil
}

func (s *Service) RunImageRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	serverID := firstNonEmpty(task.ServerID, task.ResourceID)
	if serverID == "" {
		return panelerr.Validation("server_required", "Server is required")
	}
	if !s.markRefreshing(serverID) {
		return nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		s.clearRefreshing(serverID)
		return err
	}
	s.runImageRefresh(s.tasks.ExecutionContext(task.ID), task, serverID)
	return nil
}

func (s *Service) MonitorApplications(ctx context.Context) error {
	if s.apps == nil || s.tasks == nil {
		return nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	apps, err := s.apps.List(ctx)
	if err != nil {
		return err
	}
	appByID := map[string]applications.Application{}
	for _, app := range apps {
		appByID[app.ID] = app
	}
	for _, srv := range servers {
		if !srv.Reachable || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			continue
		}
		baseURL := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
		containers, err := s.agent.DockerContainers(ctx, baseURL)
		if err != nil {
			_ = s.handleAgentError(ctx, srv, err)
			continue
		}
		observed := map[string]agentcontract.DockerContainer{}
		for _, container := range containers {
			appID, instanceID, managed := managedLabels(container.Labels)
			if !managed {
				continue
			}
			observed[instanceID] = container
			_, _ = s.db.ExecContext(ctx, `INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at)
				VALUES(?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET application_id=excluded.application_id,server_id=excluded.server_id,observed_at=excluded.observed_at`,
				instanceID, appID, srv.ID, time.Now().UTC().Format(time.RFC3339Nano))
		}
		rows, err := s.db.QueryContext(ctx, `SELECT instance_id,application_id FROM application_reconcile_states WHERE server_id=?`, srv.ID)
		if err != nil {
			continue
		}
		type expected struct{ instanceID, appID string }
		var expectedItems []expected
		for rows.Next() {
			var item expected
			if rows.Scan(&item.instanceID, &item.appID) == nil {
				expectedItems = append(expectedItems, item)
			}
		}
		rows.Close()
		for _, item := range expectedItems {
			app, ok := appByID[item.appID]
			if !ok || !app.Enabled {
				continue
			}
			container, found := observed[item.instanceID]
			drifted := !found || container.State != "running"
			if found {
				drifted = drifted ||
					container.Labels["panel.application.generation"] != strconv.Itoa(app.Generation) ||
					container.Labels["panel.application.spec.hash"] != app.SpecHash
			}
			if !drifted {
				continue
			}
			nextRunAt, err := s.nextApplicationReconcileRunAt(ctx, app.ID)
			if err != nil {
				continue
			}
			task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
				Type: TaskApplicationReconcile, ServerID: srv.ID, ResourceType: "application",
				ResourceID: app.ID, TriggerType: "scheduler", Summary: "Reconciling application " + app.Name,
				MaxRetries: 3, NextRunAt: nextRunAt,
			}, tasks.Trigger{Type: "scheduler", Periodic: true})
			if err != nil || !created {
				continue
			}
			if nextRunAt != nil && nextRunAt.After(time.Now().UTC()) {
				continue
			}
			if err := s.tasks.Start(ctx, task.ID); err != nil {
				continue
			}
			go s.runApplicationReconcile(s.tasks.ExecutionContext(task.ID), task, app.ID)
		}
	}
	return nil
}

func (s *Service) CollectApplicationReconcileTasks(ctx context.Context, operationID string) ([]tasks.CreateInput, error) {
	if s.apps == nil || s.tasks == nil {
		return nil, nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	apps, err := s.apps.List(ctx)
	if err != nil {
		return nil, err
	}
	appByID := map[string]applications.Application{}
	for _, app := range apps {
		appByID[app.ID] = app
	}
	inputs := []tasks.CreateInput{}
	for _, srv := range servers {
		if !srv.Reachable || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			continue
		}
		baseURL := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
		containers, err := s.agent.DockerContainers(ctx, baseURL)
		if err != nil {
			_ = s.handleAgentError(ctx, srv, err)
			continue
		}
		observed := map[string]agentcontract.DockerContainer{}
		for _, container := range containers {
			appID, instanceID, managed := managedLabels(container.Labels)
			if !managed {
				continue
			}
			observed[instanceID] = container
			_, _ = s.db.ExecContext(ctx, `INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at)
				VALUES(?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET application_id=excluded.application_id,server_id=excluded.server_id,observed_at=excluded.observed_at`,
				instanceID, appID, srv.ID, time.Now().UTC().Format(time.RFC3339Nano))
		}
		rows, err := s.db.QueryContext(ctx, `SELECT instance_id,application_id FROM application_reconcile_states WHERE server_id=?`, srv.ID)
		if err != nil {
			continue
		}
		type expected struct{ instanceID, appID string }
		var expectedItems []expected
		for rows.Next() {
			var item expected
			if rows.Scan(&item.instanceID, &item.appID) == nil {
				expectedItems = append(expectedItems, item)
			}
		}
		rows.Close()
		for _, item := range expectedItems {
			app, ok := appByID[item.appID]
			if !ok || !app.Enabled {
				continue
			}
			container, found := observed[item.instanceID]
			drifted := !found || container.State != "running"
			if found {
				drifted = drifted ||
					container.Labels["panel.application.generation"] != strconv.Itoa(app.Generation) ||
					container.Labels["panel.application.spec.hash"] != app.SpecHash
			}
			if !drifted {
				continue
			}
			nextRunAt, err := s.nextApplicationReconcileRunAt(ctx, app.ID)
			if err != nil {
				return inputs, err
			}
			inputs = append(inputs, tasks.CreateInput{
				OperationID:  operationID,
				Type:         TaskApplicationReconcile,
				ServerID:     srv.ID,
				ResourceType: "application",
				ResourceID:   app.ID,
				TriggerType:  "scheduler",
				Summary:      "Reconciling application " + app.Name,
				MaxRetries:   3,
				NextRunAt:    nextRunAt,
			})
		}
	}
	return inputs, nil
}

func (s *Service) nextApplicationReconcileRunAt(ctx context.Context, appID string) (*time.Time, error) {
	if s.tasks == nil {
		return nil, nil
	}
	failures, err := s.tasks.CountFailuresSinceLastSuccess(ctx, TaskApplicationReconcile, "application", appID, []string{tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked, tasks.StatusCancelled}, "user")
	if err != nil {
		return nil, err
	}
	if failures <= 0 {
		return nil, nil
	}
	delay := applicationReconcileBackoff(failures)
	next := time.Now().UTC().Add(delay)
	return &next, nil
}

func applicationReconcileBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	delay := 30 * time.Second
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= applicationReconcileMaxBackoff {
			return applicationReconcileMaxBackoff
		}
	}
	return delay
}

func (s *Service) RunApplicationReconcileTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.TriggerResourceID)
	if appID == "" {
		return panelerr.Validation("application_required", "Application is required")
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	s.runApplicationReconcile(s.tasks.ExecutionContext(task.ID), task, appID)
	return nil
}

func (s *Service) UpgradeApplications(ctx context.Context, applicationIDs []string, all bool) (tasks.Task, error) {
	if s.apps == nil || s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("application_updater_unavailable", "Application updater is unavailable")
	}
	taskType := TaskImageUpgradeMany
	if all {
		taskType = TaskImageUpgradeAll
		apps, err := s.apps.List(ctx)
		if err != nil {
			return tasks.Task{}, err
		}
		applicationIDs = applicationIDs[:0]
		for _, app := range apps {
			if app.Enabled && app.ImageUpdateAvailable {
				applicationIDs = append(applicationIDs, app.ID)
			}
		}
	}
	applicationIDs = uniqueStrings(applicationIDs)
	if len(applicationIDs) == 0 {
		return tasks.Task{}, panelerr.Validation("applications_required", "At least one application is required")
	}
	task, _, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type: taskType, ResourceType: "applications", ResourceID: strings.Join(applicationIDs, ","),
		TriggerType: "user", Summary: "Updating application images",
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	go s.runApplicationUpdates(task, applicationIDs)
	return task, nil
}

func (s *Service) Execute(ctx context.Context, serverID string, run func(context.Context) error) error {
	result := make(chan error, 1)
	select {
	case s.queue(serverID).jobs <- queueJob{run: func(runCtx context.Context) error {
		return run(runCtx)
	}, result: result}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) queue(serverID string) *serverQueue {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if q := s.queues[serverID]; q != nil {
		return q
	}
	q := &serverQueue{jobs: make(chan queueJob, 128)}
	s.queues[serverID] = q
	go s.runQueue(q)
	return q
}

func (s *Service) runQueue(q *serverQueue) {
	for job := range q.jobs {
		ctx := context.Background()
		err := job.run(ctx)
		if job.result != nil {
			job.result <- err
		}
	}
}

func (s *Service) refreshOperationResult(ctx context.Context, serverID, resource string) (OperationResult, error) {
	refreshCtx := context.WithoutCancel(ctx)
	var (
		task tasks.Task
		err  error
	)
	switch resource {
	case "containers":
		task, err = s.RefreshContainers(refreshCtx, serverID, "user", "")
	case "images":
		task, err = s.RefreshImages(refreshCtx, serverID, "user", "")
	case "volumes":
		task, err = s.RefreshVolumes(refreshCtx, serverID, "user", "")
	default:
		return OperationResult{}, nil
	}
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{RefreshTaskID: task.ID}, nil
}

func (s *Service) startSimpleResourceRefresh(ctx context.Context, serverID, taskType, triggerType, operationID, summary, completedSummary string, refresh func(context.Context, string) error) (tasks.Task, error) {
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		OperationID: operationID, Type: taskType, ServerID: serverID,
		ResourceType: "server", ResourceID: serverID, TriggerType: triggerType,
		Summary: summary,
	}, tasks.Trigger{Type: triggerType, Periodic: triggerType == "scheduler"})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	go s.runSimpleResourceRefresh(s.tasks.ExecutionContext(task.ID), task, serverID, completedSummary, refresh)
	return task, nil
}

func (s *Service) runSimpleResourceRefresh(ctx context.Context, task tasks.Task, serverID, completedSummary string, refresh func(context.Context, string) error) {
	defer s.tasks.FinishExecution(task.ID)
	if err := ctx.Err(); err != nil {
		return
	}
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	if err := refresh(ctx, baseURL); err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = s.tasks.Complete(ctx, task.ID, completedSummary)
}

func (s *Service) runImageRefresh(ctx context.Context, task tasks.Task, serverID string) {
	defer s.tasks.FinishExecution(task.ID)
	defer s.clearRefreshing(serverID)
	if err := ctx.Err(); err != nil {
		return
	}
	srv, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	images, err := s.agent.DockerImages(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	now := time.Now().UTC()
	type imageUpdate struct {
		reference       string
		localDigest     string
		latestDigest    string
		updateAvailable bool
		lastError       string
	}
	updates := make([]imageUpdate, 0, len(images))
	for _, image := range images {
		reference := firstTaggedReference(image.RepoTags)
		if reference == "" {
			continue
		}
		localDigest := firstDigest(image.RepoDigests)
		latestDigest, lastError := "", ""
		if result, resolveErr := s.resolver.Resolve(ctx, reference); resolveErr != nil {
			lastError = resolveErr.Error()
		} else {
			latestDigest = result.Digest
		}
		updates = append(updates, imageUpdate{
			reference:       reference,
			localDigest:     localDigest,
			latestDigest:    latestDigest,
			updateAvailable: localDigest != "" && latestDigest != "" && localDigest != latestDigest,
			lastError:       lastError,
		})
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM image_updates WHERE server_id=?`, serverID); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	for _, update := range updates {
		if _, err = tx.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at) VALUES(?,?,?,?,?,?,?)`,
			serverID, update.reference, update.localDigest, update.latestDigest, boolInt(update.updateAvailable), update.lastError, now.Format(time.RFC3339Nano)); err != nil {
			_ = s.tasks.Fail(ctx, task.ID, err)
			return
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO image_refreshes(server_id,refreshed_at) VALUES(?,?) ON CONFLICT(server_id) DO UPDATE SET refreshed_at=excluded.refreshed_at`, serverID, now.Format(time.RFC3339Nano)); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	if err = tx.Commit(); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = s.tasks.Complete(ctx, task.ID, "Image updates refreshed")
}

func (s *Service) runApplicationUpdates(task tasks.Task, applicationIDs []string) {
	ctx := context.Background()
	defer s.tasks.FinishExecution(task.ID)
	var failures []string
	for _, appID := range applicationIDs {
		_ = s.tasks.AppendLog(ctx, task.ID, "system", "Updating application "+appID)
		if _, err := s.apps.UpdateImage(ctx, appID); err != nil {
			failures = append(failures, appID+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		_ = s.tasks.Fail(ctx, task.ID, errors.New(strings.Join(failures, "; ")))
		return
	}
	_ = s.tasks.Complete(ctx, task.ID, "Application images updated")
}

func (s *Service) runApplicationReconcile(ctx context.Context, task tasks.Task, appID string) {
	defer s.tasks.FinishExecution(task.ID)
	if err := ctx.Err(); err != nil {
		return
	}
	if _, err := s.apps.ReconcileDeploy(ctx, appID); err != nil {
		_ = s.tasks.FailRetryable(ctx, task.ID, err)
		return
	}
	_ = s.tasks.Complete(ctx, task.ID, "Application reconciled")
}

func (s *Service) readyServer(ctx context.Context, serverID string) (server.Server, string, error) {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return server.Server{}, "", err
	}
	baseURL := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	if baseURL == "" {
		return server.Server{}, "", panelerr.Validation("agent_required", "Agent is required for Docker resources")
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return server.Server{}, "", panelerr.Validation("agent_incompatible", "Agent is not compatible with Docker resources")
	}
	return srv, baseURL, nil
}

func (s *Service) handleAgentError(ctx context.Context, srv server.Server, err error) bool {
	if s.agentErrors == nil {
		return false
	}
	return s.agentErrors.HandleAgentError(ctx, srv, err)
}

type cachedImage struct {
	LocalDigest     string
	LatestDigest    string
	UpdateAvailable bool
	CheckedAt       *time.Time
	LastError       string
}

func (s *Service) imageCache(ctx context.Context, serverID string) (map[string]cachedImage, *time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT reference,local_digest,latest_digest,update_available,last_error,checked_at FROM image_updates WHERE server_id=?`, serverID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := map[string]cachedImage{}
	for rows.Next() {
		var item cachedImage
		var available int
		var checked string
		var reference string
		if err := rows.Scan(&reference, &item.LocalDigest, &item.LatestDigest, &available, &item.LastError, &checked); err != nil {
			return nil, nil, err
		}
		item.UpdateAvailable = available == 1
		if parsed, err := time.Parse(time.RFC3339Nano, checked); err == nil {
			item.CheckedAt = &parsed
		}
		out[reference] = item
	}
	var raw sql.NullString
	_ = s.db.QueryRowContext(ctx, `SELECT refreshed_at FROM image_refreshes WHERE server_id=?`, serverID).Scan(&raw)
	var refreshedAt *time.Time
	if raw.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, raw.String); err == nil {
			refreshedAt = &parsed
		}
	}
	return out, refreshedAt, rows.Err()
}

func managedLabels(labels map[string]string) (string, string, bool) {
	if labels["panel.application.managed"] != "true" {
		return "", "", false
	}
	appID := strings.TrimSpace(labels["panel.application.id"])
	instanceID := strings.TrimSpace(labels["panel.application.instance.id"])
	return appID, instanceID, appID != "" && instanceID != ""
}

func firstTaggedReference(tags []string) string {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && tag != "<none>:<none>" {
			return tag
		}
	}
	return ""
}

func firstDigest(digests []string) string {
	for _, digest := range digests {
		if _, value, ok := strings.Cut(digest, "@"); ok {
			return value
		}
	}
	return ""
}

func imageReferenceLabel(image agentcontract.DockerImage) string {
	if reference := firstTaggedReference(image.RepoTags); reference != "" {
		return reference
	}
	if image.ID != "" {
		return image.ID
	}
	return "unknown image"
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeLogTail(tail int) int {
	if tail <= 0 {
		return 200
	}
	if tail > 10000 {
		return 10000
	}
	return tail
}

func (s *Service) markRefreshing(serverID string) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.refreshing[serverID] {
		return false
	}
	s.refreshing[serverID] = true
	return true
}

func (s *Service) clearRefreshing(serverID string) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	delete(s.refreshing, serverID)
}

func (s *Service) isRefreshing(serverID string) bool {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshing[serverID]
}
