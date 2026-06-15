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

	"panel/internal/agent"
	"panel/internal/applications"
	"panel/internal/id"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/tasks"
)

const (
	TaskContainerStart       = "container_start"
	TaskContainerStop        = "container_stop"
	TaskContainerRestart     = "container_restart"
	TaskContainerDelete      = "container_delete"
	TaskImagePull            = "image_pull"
	TaskImageRefresh         = "image_refresh"
	TaskImageDelete          = "image_delete"
	TaskImageUpgradeMany     = "application_image_upgrade_selected"
	TaskImageUpgradeAll      = "application_image_upgrade_all"
	TaskVolumeDelete         = "volume_delete"
	TaskApplicationReconcile = "application_reconcile"
)

type AgentClient interface {
	DockerContainers(context.Context, string) ([]agent.DockerContainer, error)
	DockerContainerAction(context.Context, string, string, string) error
	DockerContainerDelete(context.Context, string, string) error
	DockerImages(context.Context, string) ([]agent.DockerImage, error)
	DockerImagePull(context.Context, string, string) error
	DockerImageDelete(context.Context, string, string) error
	DockerNetworks(context.Context, string) ([]agent.DockerNetwork, error)
	DockerVolumes(context.Context, string) ([]agent.DockerVolume, error)
	DockerVolumeDelete(context.Context, string, string) error
}

type ServerProvider interface {
	Get(context.Context, string) (server.Server, error)
	List(context.Context) ([]server.Server, error)
}

type ApplicationUpdater interface {
	List(context.Context) ([]applications.Application, error)
	UpdateImage(context.Context, string) (applications.OperationResult, error)
	Deploy(context.Context, string) (applications.OperationResult, error)
}

type Container struct {
	agent.DockerContainer
	Managed       bool   `json:"managed"`
	ApplicationID string `json:"applicationId,omitempty"`
	InstanceID    string `json:"instanceId,omitempty"`
}

type Image struct {
	agent.DockerImage
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

type queueJob struct {
	task   tasks.Task
	run    func(context.Context) error
	result chan error
}

type serverQueue struct {
	jobs chan queueJob
}

type Service struct {
	db         *sql.DB
	servers    ServerProvider
	agent      AgentClient
	tasks      *tasks.Service
	apps       ApplicationUpdater
	resolver   applications.ImageDigestResolver
	queueMu    sync.Mutex
	queues     map[string]*serverQueue
	refreshMu  sync.Mutex
	refreshing map[string]bool
}

func NewService(db *sql.DB, servers ServerProvider, agentClient AgentClient, taskSvc *tasks.Service) *Service {
	return &Service{
		db: db, servers: servers, agent: agentClient, tasks: taskSvc,
		resolver: applications.NewRegistryImageResolver(),
		queues:   map[string]*serverQueue{}, refreshing: map[string]bool{},
	}
}

func (s *Service) SetApplicationUpdater(updater ApplicationUpdater) {
	s.apps = updater
}

func (s *Service) Containers(ctx context.Context, serverID string) ([]Container, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	items, err := s.agent.DockerContainers(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(items))
	for _, item := range items {
		appID, instanceID, managed := managedLabels(item.Labels)
		out = append(out, Container{DockerContainer: item, Managed: managed, ApplicationID: appID, InstanceID: instanceID})
	}
	return out, nil
}

func (s *Service) ContainerAction(ctx context.Context, serverID, containerID, action string) (tasks.Task, error) {
	taskType := map[string]string{"start": TaskContainerStart, "stop": TaskContainerStop, "restart": TaskContainerRestart}[action]
	if taskType == "" {
		return tasks.Task{}, panelerr.Validation("container_action_invalid", "Unsupported container action")
	}
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, serverID, taskType, "container", containerID, func(runCtx context.Context) error {
		return s.agent.DockerContainerAction(runCtx, baseURL, containerID, action)
	})
}

func (s *Service) DeleteContainer(ctx context.Context, serverID, containerID string) (tasks.Task, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, serverID, TaskContainerDelete, "container", containerID, func(runCtx context.Context) error {
		return s.agent.DockerContainerDelete(runCtx, baseURL, containerID)
	})
}

func (s *Service) Images(ctx context.Context, serverID string) (ImageList, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return ImageList{}, err
	}
	images, err := s.agent.DockerImages(ctx, baseURL)
	if err != nil {
		return ImageList{}, err
	}
	containers, err := s.agent.DockerContainers(ctx, baseURL)
	if err != nil {
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

func (s *Service) PullImage(ctx context.Context, serverID, reference string) (tasks.Task, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return tasks.Task{}, panelerr.Validation("image_reference_required", "Image reference is required")
	}
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, serverID, TaskImagePull, "image", reference, func(runCtx context.Context) error {
		return s.agent.DockerImagePull(runCtx, baseURL, reference)
	})
}

func (s *Service) DeleteImage(ctx context.Context, serverID, imageID string) (tasks.Task, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, serverID, TaskImageDelete, "image", imageID, func(runCtx context.Context) error {
		containers, err := s.agent.DockerContainers(runCtx, baseURL)
		if err != nil {
			return err
		}
		for _, container := range containers {
			if container.ImageID == imageID {
				return panelerr.Conflict("image_in_use", "Image is in use")
			}
		}
		return s.agent.DockerImageDelete(runCtx, baseURL, imageID)
	})
}

func (s *Service) Networks(ctx context.Context, serverID string) ([]agent.DockerNetwork, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return s.agent.DockerNetworks(ctx, baseURL)
}

func (s *Service) Volumes(ctx context.Context, serverID string) ([]agent.DockerVolume, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return s.agent.DockerVolumes(ctx, baseURL)
}

func (s *Service) DeleteVolume(ctx context.Context, serverID, name string) (tasks.Task, error) {
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, serverID, TaskVolumeDelete, "volume", name, func(runCtx context.Context) error {
		volumes, err := s.agent.DockerVolumes(runCtx, baseURL)
		if err != nil {
			return err
		}
		for _, volume := range volumes {
			if volume.Name == name && volume.InUse {
				return panelerr.Conflict("volume_in_use", "Volume is in use")
			}
		}
		return s.agent.DockerVolumeDelete(runCtx, baseURL, name)
	})
}

func (s *Service) RefreshImages(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error) {
	if s.tasks == nil {
		return tasks.Task{}, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	if existing, ok, err := s.tasks.ExistingActive(ctx, TaskImageRefresh, "server", serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		return existing, nil
	}
	if !s.markRefreshing(serverID) {
		return tasks.Task{}, panelerr.Conflict("image_refresh_running", "Image refresh is already running")
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		OperationID: operationID, Type: TaskImageRefresh, ServerID: serverID,
		ResourceType: "server", ResourceID: serverID, TriggerType: triggerType,
		Summary: "Refreshing image updates",
	})
	if err != nil {
		s.clearRefreshing(serverID)
		return tasks.Task{}, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		s.clearRefreshing(serverID)
		return tasks.Task{}, err
	}
	go s.runImageRefresh(task, serverID)
	return task, nil
}

func (s *Service) RefreshAllScheduled(ctx context.Context) error {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	operationID := id.New("op")
	for _, srv := range servers {
		if !srv.Reachable || srv.Traits[agent.TraitStatus] != agent.StatusCompatible {
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
		if !srv.Reachable || srv.Traits[agent.TraitStatus] != agent.StatusCompatible {
			continue
		}
		baseURL := strings.TrimSpace(srv.Traits[agent.TraitURL])
		containers, err := s.agent.DockerContainers(ctx, baseURL)
		if err != nil {
			continue
		}
		observed := map[string]agent.DockerContainer{}
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
			if _, active, _ := s.tasks.ExistingActive(ctx, TaskApplicationReconcile, "application", app.ID); active {
				continue
			}
			task, err := s.tasks.Create(ctx, tasks.CreateInput{
				Type: TaskApplicationReconcile, ServerID: srv.ID, ResourceType: "application",
				ResourceID: app.ID, TriggerType: "scheduler", Summary: "Reconciling application " + app.Name,
				MaxRetries: 3,
			})
			if err != nil {
				continue
			}
			if err := s.tasks.Start(ctx, task.ID); err != nil {
				continue
			}
			go s.runApplicationReconcile(task, app.ID)
		}
	}
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
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type: taskType, ResourceType: "applications", ResourceID: strings.Join(applicationIDs, ","),
		TriggerType: "user", Summary: "Updating application images",
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	go s.runApplicationUpdates(task, applicationIDs)
	return task, nil
}

func (s *Service) enqueue(ctx context.Context, serverID, taskType, resourceType, resourceID string, run func(context.Context) error) (tasks.Task, error) {
	key := serverID + ":" + resourceID
	if existing, ok, err := s.tasks.ExistingActive(ctx, taskType, resourceType, key); err != nil {
		return tasks.Task{}, err
	} else if ok {
		return existing, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type: taskType, ServerID: serverID, ResourceType: resourceType, ResourceID: key,
		TriggerType: "user", Summary: taskType,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	q := s.queue(serverID)
	q.jobs <- queueJob{task: task, run: run}
	return task, nil
}

func (s *Service) Execute(ctx context.Context, serverID string, run func(context.Context) error) error {
	result := make(chan error, 1)
	select {
	case s.queue(serverID).jobs <- queueJob{run: run, result: result}:
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
		if job.task.ID == "" {
			err := job.run(ctx)
			if job.result != nil {
				job.result <- err
			}
			continue
		}
		if err := s.tasks.Start(ctx, job.task.ID); err != nil {
			continue
		}
		_ = s.tasks.Advance(ctx, job.task.ID, "running", "")
		err := job.run(ctx)
		if err != nil {
			_ = s.tasks.Fail(ctx, job.task.ID, err)
		} else {
			_ = s.tasks.Complete(ctx, job.task.ID, job.task.Summary)
		}
		s.tasks.FinishExecution(job.task.ID)
		if job.result != nil {
			job.result <- err
		}
	}
}

func (s *Service) runImageRefresh(task tasks.Task, serverID string) {
	ctx := context.Background()
	defer s.tasks.FinishExecution(task.ID)
	defer s.clearRefreshing(serverID)
	_, baseURL, err := s.readyServer(ctx, serverID)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	images, err := s.agent.DockerImages(ctx, baseURL)
	if err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	now := time.Now().UTC()
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
		updateAvailable := localDigest != "" && latestDigest != "" && localDigest != latestDigest
		if _, err = tx.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at) VALUES(?,?,?,?,?,?,?)`,
			serverID, reference, localDigest, latestDigest, boolInt(updateAvailable), lastError, now.Format(time.RFC3339Nano)); err != nil {
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

func (s *Service) runApplicationReconcile(task tasks.Task, appID string) {
	ctx := context.Background()
	defer s.tasks.FinishExecution(task.ID)
	if _, err := s.apps.Deploy(ctx, appID); err != nil {
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
	baseURL := strings.TrimSpace(srv.Traits[agent.TraitURL])
	if baseURL == "" {
		return server.Server{}, "", panelerr.Validation("agent_required", "Agent is required for Docker resources")
	}
	if srv.Traits[agent.TraitStatus] != agent.StatusCompatible {
		return server.Server{}, "", panelerr.Validation("agent_incompatible", "Agent is not compatible with Docker resources")
	}
	return srv, baseURL, nil
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
