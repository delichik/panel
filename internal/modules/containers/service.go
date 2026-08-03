package containerization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

const (
	TaskContainerStart       = "container_start"
	TaskContainerStop        = "container_stop"
	TaskContainerRestart     = "container_restart"
	TaskContainerDelete      = "container_delete"
	TaskImagePull            = "image_pull"
	TaskImageRefresh         = "image_refresh"
	TaskImageDelete          = "image_delete"
	TaskImageDeleteUnused    = "image_delete_unused"
	TaskImageUpgradeMany     = "application_image_upgrade_selected"
	TaskImageUpgradeAll      = "application_image_upgrade_all"
	TaskVolumeDelete         = "volume_delete"
	TaskVolumeDeleteUnused   = "volume_delete_unused"
	TaskVolumeRefresh        = "volume_refresh"
	TaskNetworkRefresh       = "network_refresh"
	TaskApplicationReconcile = "application_reconcile"
)

const (
	applicationReconcileMaxBackoff           = 10 * time.Minute
	applicationReconcileHealthyChecksToReset = 5
)

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
	PlanApplicationDeployment(context.Context, applications.DeploymentPlanRequest) (applications.DeploymentPlanResult, error)
}

type ApplicationReconcileLister interface {
	ListForReconcile(context.Context) ([]applications.Application, error)
}

type Container struct {
	agentcontract.DockerContainer
	Managed       bool   `json:"managed"`
	ApplicationID string `json:"applicationId,omitempty"`
	InstanceID    string `json:"instanceId,omitempty"`
}

type ContainerSummary struct {
	ID            string                     `json:"id"`
	Names         []string                   `json:"names"`
	Image         string                     `json:"image"`
	State         string                     `json:"state"`
	Status        string                     `json:"status"`
	Ports         []agentcontract.DockerPort `json:"ports"`
	Managed       bool                       `json:"managed"`
	ApplicationID string                     `json:"applicationId,omitempty"`
	InstanceID    string                     `json:"instanceId,omitempty"`
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

type SnapshotList[T any] struct {
	Items            []T        `json:"items"`
	ObservedAt       *time.Time `json:"observedAt,omitempty"`
	Stale            bool       `json:"stale"`
	Refreshing       bool       `json:"refreshing"`
	RefreshTaskID    string     `json:"refreshTaskId,omitempty"`
	LastRefreshError string     `json:"lastRefreshError,omitempty"`
}

type ImageList = SnapshotList[Image]

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

// containerObservationRow 是 container_observations 表的本地行映射：container_json /
// summary_json / sample_at 必须按原始文本解析（写入端可能产生任意 JSON 文本，且旧
// 代码对 sample_at 的解析失败是静默容错的），因此不能直接使用 models.ContainerObservation
// 的 map[string]any / time.Time 语义。
type containerObservationRow struct {
	ServerID      string
	ContainerID   string
	SampleAt      string
	ContainerJSON string
	SummaryJSON   string
	Managed       bool
	ApplicationID string
	InstanceID    string
	UpdatedAt     string
}

func (s *Service) Containers(ctx context.Context, serverID string) (SnapshotList[ContainerSummary], error) {
	if _, _, err := s.readyServer(ctx, serverID); err != nil {
		return SnapshotList[ContainerSummary]{}, err
	}
	items, observedAt, err := s.reportedContainerSummaries(ctx, serverID)
	if err != nil {
		return SnapshotList[ContainerSummary]{}, err
	}
	return SnapshotList[ContainerSummary]{Items: items, ObservedAt: observedAt, Stale: observedAt == nil}, nil
}

func (s *Service) SaveReportedContainers(ctx context.Context, serverID string, sampleAt time.Time, items []agentcontract.DockerContainer) error {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return panelerr.Validation("server_required", "Server is required")
	}
	if sampleAt.IsZero() {
		sampleAt = time.Now().UTC()
	}
	sampleAt = sampleAt.UTC().Truncate(time.Second)
	now := time.Now().UTC()
	return orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).From("container_observations").Where("server_id = ?", serverID).Delete(ctx); err != nil {
			return err
		}
		for _, item := range items {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			appID, instanceID, managed := managedLabels(item.Labels)
			raw, err := json.Marshal(item)
			if err != nil {
				return err
			}
			summaryRaw, err := json.Marshal(ContainerSummary{ID: item.ID, Names: item.Names, Image: item.Image, State: item.State, Status: item.Status, Ports: item.Ports, Managed: managed, ApplicationID: appID, InstanceID: instanceID})
			if err != nil {
				return err
			}
			var containerJSON, summaryJSON map[string]any
			if err := json.Unmarshal(raw, &containerJSON); err != nil {
				return err
			}
			if err := json.Unmarshal(summaryRaw, &summaryJSON); err != nil {
				return err
			}
			if err := orm.New(tx).Insert(ctx, &models.ContainerObservation{
				ServerID:      serverID,
				ContainerID:   item.ID,
				SampleAt:      sampleAt,
				ContainerJSON: containerJSON,
				SummaryJSON:   summaryJSON,
				Managed:       managed,
				ApplicationID: appID,
				InstanceID:    instanceID,
				UpdatedAt:     now,
			}); err != nil {
				return err
			}
			if managed {
				if _, err := orm.RawExec(ctx, tx, `INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at)
					VALUES(?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET application_id=excluded.application_id,server_id=excluded.server_id,observed_at=excluded.observed_at`,
					instanceID, appID, serverID, sampleAt.Format(time.RFC3339Nano)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Service) reportedContainerSummaries(ctx context.Context, serverID string) ([]ContainerSummary, *time.Time, error) {
	rows := []containerObservationRow{}
	if err := orm.New(s.db).From("container_observations").
		Select("summary_json", "container_json", "sample_at").
		Where("server_id = ?", serverID).OrderBy("container_id").
		All(ctx, &rows); err != nil {
		return nil, nil, err
	}
	items := make([]ContainerSummary, 0, len(rows))
	var latest time.Time
	for _, row := range rows {
		raw, legacyRaw, sampleRaw := row.SummaryJSON, row.ContainerJSON, row.SampleAt
		if raw == "" || raw == "{}" {
			raw = legacyRaw
		}
		var item ContainerSummary
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, nil, err
		}
		if sampleAt, err := time.Parse(time.RFC3339Nano, sampleRaw); err == nil && sampleAt.After(latest) {
			latest = sampleAt
		}
		items = append(items, item)
	}
	if latest.IsZero() {
		return items, nil, nil
	}
	return items, &latest, nil
}

func (s *Service) reportedContainers(ctx context.Context, serverID string) ([]agentcontract.DockerContainer, *time.Time, error) {
	rows := []containerObservationRow{}
	if err := orm.New(s.db).From("container_observations").
		Select("container_json", "sample_at").
		Where("server_id = ?", serverID).OrderBy("container_id").
		All(ctx, &rows); err != nil {
		return nil, nil, err
	}
	items := make([]agentcontract.DockerContainer, 0, len(rows))
	var latest time.Time
	for _, row := range rows {
		raw, sampleRaw := row.ContainerJSON, row.SampleAt
		var item agentcontract.DockerContainer
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, nil, err
		}
		if sampleAt, err := time.Parse(time.RFC3339Nano, sampleRaw); err == nil && sampleAt.After(latest) {
			latest = sampleAt
		}
		items = append(items, item)
	}
	if latest.IsZero() {
		return items, nil, nil
	}
	return items, &latest, nil
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
	var images []agentcontract.DockerImage
	observedAt, err := s.resourceSnapshot(ctx, serverID, "images", &images)
	if err != nil {
		return ImageList{}, err
	}
	containers, _, err := s.reportedContainers(ctx, serverID)
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
	if refreshedAt == nil {
		refreshedAt = observedAt
	}
	if refreshedAt == nil {
		refreshedAt = observedAt
	}
	return ImageList{Items: out, ObservedAt: refreshedAt, Stale: refreshedAt == nil, Refreshing: s.isRefreshing(serverID)}, nil
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

func (s *Service) Networks(ctx context.Context, serverID string) (SnapshotList[agentcontract.DockerNetwork], error) {
	items := []agentcontract.DockerNetwork{}
	observedAt, err := s.resourceSnapshot(ctx, serverID, "networks", &items)
	return SnapshotList[agentcontract.DockerNetwork]{Items: items, ObservedAt: observedAt, Stale: observedAt == nil, Refreshing: s.isRefreshing(serverID)}, err
}

func (s *Service) Volumes(ctx context.Context, serverID string) (SnapshotList[agentcontract.DockerVolume], error) {
	items := []agentcontract.DockerVolume{}
	observedAt, err := s.resourceSnapshot(ctx, serverID, "volumes", &items)
	return SnapshotList[agentcontract.DockerVolume]{Items: items, ObservedAt: observedAt, Stale: observedAt == nil, Refreshing: s.isRefreshing(serverID)}, err
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

func (s *Service) RefreshVolumes(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error) {
	return s.startSimpleResourceRefresh(ctx, serverID, TaskVolumeRefresh, triggerType, operationID, "Refreshing volumes", "Volumes refreshed", func(runCtx context.Context, baseURL string) error {
		items, err := s.agent.DockerVolumes(runCtx, baseURL)
		if err != nil {
			return err
		}
		return s.replaceResourceSnapshot(runCtx, serverID, "volumes", items)
	})
}

func (s *Service) RefreshNetworks(ctx context.Context, serverID, triggerType, operationID string) (tasks.Task, error) {
	return s.startSimpleResourceRefresh(ctx, serverID, TaskNetworkRefresh, triggerType, operationID, "Refreshing networks", "Networks refreshed", func(runCtx context.Context, baseURL string) error {
		items, err := s.agent.DockerNetworks(runCtx, baseURL)
		if err != nil {
			return err
		}
		return s.replaceResourceSnapshot(runCtx, serverID, "networks", items)
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

func (s *Service) TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	if s.tasks == nil {
		return tasks.Task{}, false, panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	if trigger.Type == "" {
		trigger.Type = "manual"
	}
	batch, shouldRun, err := s.CollectApplicationReconcileInputs(ctx, trigger)
	if err != nil || !shouldRun {
		return tasks.Task{}, false, err
	}
	if batch.Type == "" {
		batch.Type = TaskApplicationReconcile
	}
	if batch.TriggerType == "" {
		batch.TriggerType = trigger.Type
	}
	manager := tasks.NewManager(s.tasks)
	task, created, err := manager.CreateBatchAndRun(ctx, batch, tasks.Trigger{Type: batch.TriggerType, Manual: trigger.Manual, Periodic: true})
	return task, created, err
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
	inputs, err := s.CollectApplicationReconcileTasks(ctx, id.New("op"), tasks.PeriodicTrigger{Type: "scheduler"})
	if err != nil {
		return err
	}
	manager := tasks.NewManager(s.tasks)
	for _, input := range inputs {
		task, created, err := manager.Create(ctx, input, tasks.Trigger{Type: "scheduler", Periodic: true})
		if err != nil || !created {
			continue
		}
		if task.NextRunAt != nil && task.NextRunAt.After(time.Now().UTC()) {
			continue
		}
		go func(task tasks.Task) {
			defer s.tasks.FinishExecution(task.ID)
			_ = manager.Run(context.Background(), task)
		}(task)
	}
	return nil
}

func (s *Service) CollectApplicationReconcileTasks(ctx context.Context, _ string, trigger tasks.PeriodicTrigger) ([]tasks.CreateInput, error) {
	if s.apps == nil || s.tasks == nil {
		return nil, nil
	}
	explicit := applicationReconcileTriggerPayload(trigger)
	if explicit.requiresExplicitTasks() {
		return nil, s.planExplicitApplicationReconcile(ctx, trigger, explicit)
	}
	bypassBackoff := applicationReconcileBypassesBackoff(trigger, explicit)
	triggerType := firstNonEmpty(trigger.Type, "scheduler")
	wantedApps := stringSet(explicit.ApplicationIDs)
	wantedServers := stringSet(explicit.ServerIDs)
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	apps, err := s.listApplicationsForReconcile(ctx)
	if err != nil {
		return nil, err
	}
	appByID := map[string]applications.Application{}
	for _, app := range apps {
		appByID[app.ID] = app
	}
	observations := map[string]reconcileObservation{}
	for _, srv := range servers {
		if len(wantedServers) > 0 && !wantedServers[srv.ID] {
			continue
		}
		if !srv.Reachable || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			continue
		}
		containers, _, err := s.reportedContainers(ctx, srv.ID)
		if err != nil {
			continue
		}
		observed := map[string]agentcontract.DockerContainer{}
		for _, container := range containers {
			_, instanceID, managed := managedLabels(container.Labels)
			if !managed {
				continue
			}
			observed[instanceID] = container
		}
		type expected struct {
			ID            string
			ApplicationID string
		}
		var expectedItems []expected
		if err := orm.New(s.db).From("application_instances").Select("id", "application_id").
			Where("server_id = ?", srv.ID).And("desired_state = 'running'").
			All(ctx, &expectedItems); err != nil {
			return nil, fmt.Errorf("query desired application instances for server %s: %w", srv.ID, err)
		}
		for _, item := range expectedItems {
			if len(wantedApps) > 0 && !wantedApps[item.ApplicationID] {
				continue
			}
			app, ok := appByID[item.ApplicationID]
			if !ok || !app.Enabled || !applicationWantsServer(app, srv.ID) {
				continue
			}
			container, found := observed[item.ID]
			drifted := !found || container.State != "running"
			if found {
				drifted = drifted ||
					container.Labels["panel.application.generation"] != strconv.Itoa(app.Generation) ||
					container.Labels["panel.application.spec.hash"] != app.SpecHash ||
					container.Labels["panel.application.managed_files.drift"] == "true"
			}
			observation := observations[app.ID]
			observation.app = app
			observation.seen = true
			if drifted {
				observation.driftedServerIDs = append(observation.driftedServerIDs, srv.ID)
			}
			observations[app.ID] = observation
		}
	}
	for appID, observation := range observations {
		if !observation.seen {
			continue
		}
		if len(observation.driftedServerIDs) == 0 {
			_ = s.recordApplicationReconcileHealthy(ctx, appID)
			continue
		}
		_ = s.resetApplicationReconcileHealthStreak(ctx, appID)
		nextRunAt, err := s.nextApplicationReconcileRunAt(ctx, appID)
		if err != nil {
			return nil, err
		}
		if !bypassBackoff && nextRunAt != nil && nextRunAt.After(time.Now().UTC()) {
			continue
		}
		_, err = s.apps.PlanApplicationDeployment(ctx, applications.DeploymentPlanRequest{
			ApplicationID:        appID,
			ServerIDs:            observation.driftedServerIDs,
			ObservedRuntimeDrift: true,
			TriggerType:          triggerType,
		})
		if err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func applicationWantsServer(app applications.Application, serverID string) bool {
	if app.DeploymentMode == applications.DeploymentModeAll || strings.TrimSpace(app.DeploymentMode) == "" {
		return true
	}
	if app.DeploymentMode != applications.DeploymentModeSelected {
		return false
	}
	for _, desiredServerID := range app.DeploymentServers {
		if strings.TrimSpace(desiredServerID) == serverID {
			return true
		}
	}
	return false
}

func applicationReconcileBypassesBackoff(trigger tasks.PeriodicTrigger, payload ApplicationReconcileTrigger) bool {
	return trigger.Type == "agent_report" && strings.TrimSpace(payload.Reason) == "container_change"
}

func (s *Service) listApplicationsForReconcile(ctx context.Context) ([]applications.Application, error) {
	if lister, ok := s.apps.(ApplicationReconcileLister); ok {
		return lister.ListForReconcile(ctx)
	}
	return s.apps.List(ctx)
}

func (payload ApplicationReconcileTrigger) requiresExplicitTasks() bool {
	if payload.Force || payload.Purge || len(payload.StopServers) > 0 || len(payload.ApplicationIDs) > 0 {
		return true
	}
	return false
}

func (s *Service) planExplicitApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger, payload ApplicationReconcileTrigger) error {
	triggerType := firstNonEmpty(trigger.Type, "manual")
	appIDs := payload.ApplicationIDs
	if len(appIDs) == 0 {
		apps, err := s.apps.List(ctx)
		if err != nil {
			return err
		}
		for _, app := range apps {
			if app.Enabled {
				appIDs = append(appIDs, app.ID)
			}
		}
	}
	for _, appID := range appIDs {
		if !payload.Force && !trigger.Manual && triggerType != "user" {
			nextRunAt, err := s.nextApplicationReconcileRunAt(ctx, appID)
			if err != nil {
				return err
			}
			if nextRunAt != nil && nextRunAt.After(time.Now().UTC()) {
				continue
			}
		}
		_, err := s.apps.PlanApplicationDeployment(ctx, applications.DeploymentPlanRequest{
			ApplicationID:       appID,
			ServerIDs:           payload.ServerIDs,
			StopServers:         payload.StopServers,
			Purge:               payload.Purge,
			Force:               payload.Force,
			Manual:              trigger.Manual,
			TriggerType:         triggerType,
			TriggerResourceType: trigger.TriggerResourceType,
			TriggerResourceID:   trigger.TriggerResourceID,
			Reason:              payload.Reason,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type reconcileObservation struct {
	app              applications.Application
	seen             bool
	driftedServerIDs []string
}

func (s *Service) nextApplicationReconcileRunAt(ctx context.Context, appID string) (*time.Time, error) {
	state, err := s.applicationReconcileState(ctx, appID)
	if err != nil {
		return nil, err
	}
	failures := state.failures
	if failures <= 0 {
		return nil, nil
	}
	if state.nextRunAt != nil {
		return state.nextRunAt, nil
	}
	next := time.Now().UTC().Add(applicationReconcileBackoff(failures))
	_ = s.updateApplicationReconcileBackoff(ctx, appID, failures, next)
	return &next, nil
}

type applicationReconcileState struct {
	failures      int
	nextRunAt     *time.Time
	successStreak int
}

func (s *Service) applicationReconcileState(ctx context.Context, appID string) (applicationReconcileState, error) {
	var failures sql.NullInt64
	var nextRunAt sql.NullString
	var successStreak sql.NullInt64
	err := orm.RawRow(ctx, s.db, `SELECT MAX(reconcile_failures), MAX(NULLIF(reconcile_next_run_at,'')), MAX(reconcile_success_streak) FROM application_reconcile_states WHERE application_id=?`, appID).Scan(&failures, &nextRunAt, &successStreak)
	if err != nil {
		return applicationReconcileState{}, err
	}
	state := applicationReconcileState{}
	if failures.Valid && failures.Int64 > 0 {
		state.failures = int(failures.Int64)
	}
	if successStreak.Valid && successStreak.Int64 > 0 {
		state.successStreak = int(successStreak.Int64)
	}
	if nextRunAt.Valid && strings.TrimSpace(nextRunAt.String) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, nextRunAt.String); err == nil {
			state.nextRunAt = &parsed
		}
	}
	return state, nil
}

func (s *Service) updateApplicationReconcileBackoff(ctx context.Context, appID string, failures int, next time.Time) error {
	return orm.New(s.db).From("application_reconcile_states").
		Where("application_id = ?", appID).
		UpdateColumns(ctx, map[string]any{
			"reconcile_failures":       failures,
			"reconcile_next_run_at":    next.UTC().Format(time.RFC3339Nano),
			"reconcile_success_streak": 0,
		})
}

func (s *Service) recordApplicationReconcileFailure(ctx context.Context, appID string) error {
	state, err := s.applicationReconcileState(ctx, appID)
	if err != nil {
		return err
	}
	failures := state.failures + 1
	next := time.Now().UTC().Add(applicationReconcileBackoff(failures))
	return s.updateApplicationReconcileBackoff(ctx, appID, failures, next)
}

func (s *Service) recordApplicationReconcileHealthy(ctx context.Context, appID string) error {
	state, err := s.applicationReconcileState(ctx, appID)
	if err != nil || state.failures <= 0 {
		return err
	}
	streak := state.successStreak + 1
	if streak >= applicationReconcileHealthyChecksToReset {
		return orm.New(s.db).From("application_reconcile_states").
			Where("application_id = ?", appID).
			UpdateColumns(ctx, map[string]any{
				"reconcile_failures":       0,
				"reconcile_next_run_at":    "",
				"reconcile_success_streak": 0,
			})
	}
	return orm.New(s.db).From("application_reconcile_states").
		Where("application_id = ?", appID).
		UpdateColumns(ctx, map[string]any{"reconcile_success_streak": streak})
}

func (s *Service) resetApplicationReconcileHealthStreak(ctx context.Context, appID string) error {
	return orm.New(s.db).From("application_reconcile_states").
		Where("application_id = ?", appID).
		UpdateColumns(ctx, map[string]any{"reconcile_success_streak": 0})
}

func (s *Service) clearApplicationReconcileNextRun(ctx context.Context, appID string) error {
	return orm.New(s.db).From("application_reconcile_states").
		Where("application_id = ?", appID).
		UpdateColumns(ctx, map[string]any{"reconcile_next_run_at": ""})
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
	return panelerr.Validation("application_reconcile_collector_only", "Application reconcile is a collector-only task")
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
		return OperationResult{}, nil
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
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).From("image_updates").Where("server_id = ?", serverID).Delete(ctx); err != nil {
			return err
		}
		for _, update := range updates {
			if err := orm.New(tx).Insert(ctx, &models.ImageUpdate{
				ServerID:        serverID,
				Reference:       update.reference,
				LocalDigest:     update.localDigest,
				LatestDigest:    update.latestDigest,
				UpdateAvailable: update.updateAvailable,
				LastError:       update.lastError,
				CheckedAt:       now,
			}); err != nil {
				return err
			}
		}
		if _, err := orm.RawExec(ctx, tx, `INSERT INTO image_refreshes(server_id,refreshed_at) VALUES(?,?) ON CONFLICT(server_id) DO UPDATE SET refreshed_at=excluded.refreshed_at`, serverID, now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	if err = s.replaceResourceSnapshot(ctx, serverID, "images", images); err != nil {
		_ = s.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = s.tasks.Complete(ctx, task.ID, "Image updates refreshed")
}

func (s *Service) replaceResourceSnapshot(ctx context.Context, serverID, resourceType string, items any) error {
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = orm.RawExec(ctx, s.db, `INSERT INTO docker_resource_snapshots(server_id,resource_type,items_json,observed_at) VALUES(?,?,?,?)
		ON CONFLICT(server_id,resource_type) DO UPDATE SET items_json=excluded.items_json,observed_at=excluded.observed_at`,
		serverID, resourceType, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Service) resourceSnapshot(ctx context.Context, serverID, resourceType string, out any) (*time.Time, error) {
	if _, _, err := s.readyServer(ctx, serverID); err != nil {
		return nil, err
	}
	var row dockerResourceSnapshotRow
	err := orm.New(s.db).From("docker_resource_snapshots").Select("items_json", "observed_at").
		Where("server_id = ?", serverID).And("resource_type = ?", resourceType).
		First(ctx, &row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(row.ItemsJSON), out); err != nil {
		return nil, err
	}
	observed, err := time.Parse(time.RFC3339Nano, row.ObservedAt)
	if err != nil {
		return nil, nil
	}
	return &observed, nil
}

// dockerResourceSnapshotRow 是 docker_resource_snapshots 的本地行映射：items_json /
// observed_at 按原始文本读取，保留旧代码对 observed_at 解析失败的静默容错。
type dockerResourceSnapshotRow struct {
	ItemsJSON  string
	ObservedAt string
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

// imageUpdateRow 是 image_updates 表的本地行映射：checked_at/update_available 需要
// 按旧代码的容错语义解析（时间解析失败时静默忽略），不能直接用 models.ImageUpdate
// 的 time.Time/bool 语义（解析失败会直接报错）。
type imageUpdateRow struct {
	Reference       string
	LocalDigest     string
	LatestDigest    string
	UpdateAvailable bool
	LastError       string
	CheckedAt       string
}

func (s *Service) imageCache(ctx context.Context, serverID string) (map[string]cachedImage, *time.Time, error) {
	var rows []imageUpdateRow
	if err := orm.New(s.db).From("image_updates").
		Select("reference", "local_digest", "latest_digest", "update_available", "last_error", "checked_at").
		Where("server_id = ?", serverID).
		All(ctx, &rows); err != nil {
		return nil, nil, err
	}
	out := map[string]cachedImage{}
	for _, row := range rows {
		item := cachedImage{
			LocalDigest:     row.LocalDigest,
			LatestDigest:    row.LatestDigest,
			UpdateAvailable: row.UpdateAvailable,
			LastError:       row.LastError,
		}
		if parsed, err := time.Parse(time.RFC3339Nano, row.CheckedAt); err == nil {
			item.CheckedAt = &parsed
		}
		out[row.Reference] = item
	}
	var raw sql.NullString
	_ = orm.RawRow(ctx, s.db, `SELECT refreshed_at FROM image_refreshes WHERE server_id=?`, serverID).Scan(&raw)
	var refreshedAt *time.Time
	if raw.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, raw.String); err == nil {
			refreshedAt = &parsed
		}
	}
	return out, refreshedAt, nil
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

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
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
