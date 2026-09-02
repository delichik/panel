package applications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications/runtime"
	"panel/internal/modules/applications/spec"
	"panel/internal/modules/runtimeevents"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	id "panel/internal/platform/identity"
	"panel/internal/platform/templating"
)

type Config struct {
	Namespace      string
	Region         string
	Datacenter     string
	SaveSessionDir string
}

type AgentRuntimeClient interface {
	RuntimeWriteFiles(ctx context.Context, baseURL string, req agentcontract.RuntimeWriteFilesRequest) error
	RuntimeReload(ctx context.Context, baseURL string, req agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error)
	RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error)
	RuntimeStop(ctx context.Context, baseURL string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error)
	RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (agentcontract.RuntimeStatusResponse, error)
	RuntimeLogs(ctx context.Context, baseURL, instanceID, containerName string, tail int) (agentcontract.RuntimeLogsResponse, error)
	RuntimePersistentArchive(ctx context.Context, baseURL, applicationID string) (agentcontract.RuntimePersistentArchiveResponse, error)
	RuntimePersistentRestore(ctx context.Context, baseURL, applicationID string, content []byte) (agentcontract.RuntimePersistentRestoreResponse, error)
	DockerImagePull(ctx context.Context, baseURL, reference string) error
	DockerContainerDelete(ctx context.Context, baseURL, id string) error
	DockerContainerAction(ctx context.Context, baseURL, id, action string) error
}

type ServerProvider interface {
	List(ctx context.Context) ([]server.Server, error)
	Get(ctx context.Context, id string) (server.Server, error)
}

type AgentErrorHandler interface {
	HandleAgentError(ctx context.Context, srv server.Server, cause error) bool
}

type Service struct {
	db               *sql.DB
	logDB            *sql.DB
	coordDB          *sql.DB
	runtimeClient    AgentRuntimeClient
	servers          ServerProvider
	agentErrors      AgentErrorHandler
	tasks            *tasks.Service
	config           Config
	configProvider   func() Config
	renderer         templatex.Renderer
	builtinResolver  BuiltinVariableResolver
	internalFiles    InternalFileProvider
	proxyReconciler  ReverseProxyReconciler
	proxyPolicy      ReverseProxyPolicyProvider
	reconcileTrigger ApplicationReconcileTrigger
	imageResolver    ImageDigestResolver
	operationQueue   ContainerOperationQueue
	facilityRuntime  FacilityRuntimeProvider
	storageResolver  StorageShareResolver
	events           runtimeevents.EventWriter
	orchestrator     *controlplane.Controller
	editCleanupOnce  sync.Once
}

type ApplicationRuntime = Runtime

var errLifecycleTargetLeaseLost = errors.New("application lifecycle target lease lost")

type LogInput struct {
	InstanceID string `json:"instanceId"`
	Type       string `json:"type"`
	Tail       int    `json:"tail"`
}

type LogResult struct {
	InstanceID    string `json:"instanceId"`
	ContainerName string `json:"containerName"`
	Type          string `json:"type"`
	Logs          string `json:"logs"`
}

type PackageResult struct {
	Filename string
	Content  []byte
}

type ReverseProxyReconciler interface {
	ReconcileReverseProxy(ctx context.Context) error
}

type ReverseProxyPolicyProvider interface {
	ResolveApplicationOrigins(ctx context.Context, applicationID, deploymentMode string, deploymentServers []string) ([]string, error)
	ValidateApplicationReverseProxy(ctx context.Context, applicationID, deploymentMode string, deploymentServers []string, rules []ReverseProxyRule) error
}

type ApplicationReconcileTrigger interface {
	TriggerApplicationReconcile(ctx context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error)
}

type ImageDigestResolver interface {
	Resolve(ctx context.Context, image string) (ImageDigestResult, error)
}

type ContainerOperationQueue interface {
	Execute(ctx context.Context, serverID string, run func(context.Context) error) error
}

type FacilityRuntimeProvider interface {
	RuntimeSpecForServer(ctx context.Context, app Application, srv server.Server) (appruntime.Spec, bool, error)
}

type FacilityRuntimeUpdatePlanner interface {
	PlanRuntimeUpdate(ctx context.Context, app Application, srv server.Server, current, desired appruntime.Spec) appruntime.UpdatePlan
}

func NewService(db *sql.DB, runtimeClient AgentRuntimeClient, taskSvc *tasks.Service, cfg Config) *Service {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Region == "" {
		cfg.Region = "global"
	}
	if cfg.Datacenter == "" {
		cfg.Datacenter = "dc1"
	}
	if cfg.SaveSessionDir == "" {
		cfg.SaveSessionDir = filepath.Join("tmp", "application-save-sessions")
	}
	s := &Service{db: db, runtimeClient: runtimeClient, tasks: taskSvc, config: cfg, renderer: templatex.NewGoRenderer(), builtinResolver: NewApplicationVariableRegistry(), imageResolver: NewRegistryImageResolver()}
	controlStore := controlplane.NewStore(db)
	s.orchestrator = controlplane.NewController(controlStore, &serviceRuntimeReconciler{service: s}, controlplane.ControllerConfig{
		Owner:       "application-orchestrator",
		OnSucceeded: s.onOrchestratorJobSucceeded,
		OnFailed:    s.onOrchestratorJobFailed,
	})
	s.startEditSessionCleanup()
	return s
}

type Option func(*Service)

func WithServerProvider(provider ServerProvider) Option {
	return func(s *Service) {
		s.servers = provider
		if handler, ok := provider.(AgentErrorHandler); ok {
			s.agentErrors = handler
		}
	}
}

func WithBuiltinVariableResolver(resolver BuiltinVariableResolver) Option {
	return func(s *Service) { s.builtinResolver = resolver }
}

func WithInternalFileProvider(provider InternalFileProvider) Option {
	return func(s *Service) { s.internalFiles = provider }
}

func WithContainerOperationQueue(queue ContainerOperationQueue) Option {
	return func(s *Service) { s.operationQueue = queue }
}

func WithApplicationReconcileTrigger(trigger ApplicationReconcileTrigger) Option {
	return func(s *Service) { s.reconcileTrigger = trigger }
}

func WithLogDB(db *sql.DB) Option {
	return func(s *Service) {
		if db != nil {
			s.logDB = db
		}
	}
}

// WithCoordDB 注入协调库（lifecycle 操作/目标/步骤日志）。
func WithCoordDB(db *sql.DB) Option {
	return func(s *Service) {
		if db != nil {
			s.coordDB = db
		}
	}
}

func WithRuntimeEvents(events runtimeevents.EventWriter) Option {
	return func(s *Service) { s.events = events }
}

func (s *Service) SetReverseProxyReconciler(reconciler ReverseProxyReconciler) {
	s.proxyReconciler = reconciler
}

func (s *Service) SetReverseProxyPolicyProvider(provider ReverseProxyPolicyProvider) {
	s.proxyPolicy = provider
}

func (s *Service) SetApplicationReconcileTrigger(trigger ApplicationReconcileTrigger) {
	s.reconcileTrigger = trigger
}

func (s *Service) SetFacilityRuntimeProvider(provider FacilityRuntimeProvider) {
	s.facilityRuntime = provider
}

// StartOrchestrator starts the durable AppDB job controller. Runtime
// mutations planned by application triggers are executed by this controller.
func (s *Service) StartOrchestrator(ctx context.Context) error {
	if s == nil || s.orchestrator == nil {
		return controlplane.ErrStoreUnavailable
	}
	return s.orchestrator.Start(ctx)
}

func (s *Service) StopOrchestrator() error {
	if s == nil || s.orchestrator == nil {
		return nil
	}
	return s.orchestrator.Stop()
}

// onOrchestratorJobSucceeded finalizes the AppDB projection after a purge.
// A successful remote purge removes the current instance row; an application
// marked for deletion is then removed once no instance rows remain.
func (s *Service) onOrchestratorJobSucceeded(ctx context.Context, job controlplane.Job, _ controlplane.ReconcileResponse) {
	if s == nil || s.db == nil || job.Action != controlplane.ActionPurge {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if _, err := orm.RawExec(cleanupCtx, s.db, `DELETE FROM application_instances WHERE id=? AND desired_state='purged'`, job.InstanceID); err != nil {
		log.Printf("application purge projection cleanup failed app_id=%s instance_id=%s: %v", job.ApplicationID, job.InstanceID, err)
		return
	}
	if _, err := orm.RawExec(cleanupCtx, s.db, `DELETE FROM application_reconcile_states WHERE instance_id=?`, job.InstanceID); err != nil {
		log.Printf("application reconcile state cleanup failed app_id=%s instance_id=%s: %v", job.ApplicationID, job.InstanceID, err)
	}
	if err := s.deleteApplicationIfRuntimeGone(cleanupCtx, job.ApplicationID); err != nil && !isNotFound(err) {
		log.Printf("application purge finalization failed app_id=%s: %v", job.ApplicationID, err)
	}
}

// onOrchestratorJobFailed records every Job failure (retryable and terminal)
// against the application reconcile failure counter. Consecutive failures grow
// the exponential backoff and, once they reach ReconcileStopAfterFailures,
// place the application into reconcile_stopped so automatic reconciliation
// terminates instead of retrying forever. An explicit user operation resets
// the counter through PlanApplicationDeployment.
func (s *Service) onOrchestratorJobFailed(ctx context.Context, job controlplane.Job, _ controlplane.ReconcileResponse) {
	if s == nil || s.db == nil || strings.TrimSpace(job.ApplicationID) == "" {
		return
	}
	if err := s.recordApplicationReconcileFailure(ctx, job.ApplicationID); err != nil {
		log.Printf("application reconcile failure recording failed app_id=%s: %v", job.ApplicationID, err)
	}
}

func (s *Service) lifecycleDB() *sql.DB {
	if s.coordDB != nil {
		return s.coordDB
	}
	if s.logDB != nil {
		return s.logDB
	}
	return s.db
}

func (s *Service) revisionDB() *sql.DB {
	return s.db
}

func NewServiceWithOptions(db *sql.DB, runtimeClient AgentRuntimeClient, taskSvc *tasks.Service, cfg Config, opts ...Option) *Service {
	s := NewService(db, runtimeClient, taskSvc, cfg)
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) currentConfig() Config {
	cfg := s.config
	if s.configProvider != nil {
		next := s.configProvider()
		if next.Namespace != "" {
			cfg.Namespace = next.Namespace
		}
		if next.Region != "" {
			cfg.Region = next.Region
		}
		if next.Datacenter != "" {
			cfg.Datacenter = next.Datacenter
		}
		if next.SaveSessionDir != "" {
			cfg.SaveSessionDir = next.SaveSessionDir
		}
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Region == "" {
		cfg.Region = "global"
	}
	if cfg.Datacenter == "" {
		cfg.Datacenter = "dc1"
	}
	return cfg
}

func (s *Service) TemplateCatalog(ctx context.Context) (TemplateCatalog, error) {
	catalog := TemplateCatalog{Variables: []TemplateVariableDefinition{
		{Key: "app.id", Category: "application", SpecExpression: "{{ .app.id }}", TemplateExpression: "{{ .app.id }}"},
		{Key: "app.name", Category: "application", SpecExpression: "{{ .app.name }}", TemplateExpression: "{{ .app.name }}"},
		{Key: "app.namespace", Category: "application", SpecExpression: "{{ .app.namespace }}", TemplateExpression: "{{ .app.namespace }}"},
		{Key: "app.generation", Category: "application", SpecExpression: "{{ .app.generation }}", TemplateExpression: "{{ .app.generation }}"},
		{Key: "server.id", Category: "server", SpecExpression: "${node.meta.panel_server_id}", TemplateExpression: `{{ env "PANEL_SERVER_ID" }}`},
		{Key: "server.name", Category: "server", SpecExpression: "${node.meta.panel_server_name}", TemplateExpression: `{{ env "PANEL_SERVER_NAME" }}`},
		{Key: "server.host", Category: "server", SpecExpression: "${node.meta.panel_ssh_host}", TemplateExpression: `{{ .server.host }}`},
		{Key: "server.ssh_host", Category: "server", SpecExpression: "${node.meta.panel_ssh_host}", TemplateExpression: `{{ env "PANEL_SERVER_SSH_HOST" }}`},
		{Key: "server.ssh_port", Category: "server", SpecExpression: "${node.meta.panel_ssh_port}", TemplateExpression: `{{ env "PANEL_SERVER_SSH_PORT" }}`},
		{Key: "server.ssh_username", Category: "server", SpecExpression: "${node.meta.panel_ssh_username}", TemplateExpression: `{{ env "PANEL_SERVER_SSH_USERNAME" }}`},
		{Key: "server.variables.<key>", Category: "server", SpecExpression: "", TemplateExpression: `{{ index .server.variables "<key>" }}`},
	}}
	if s.internalFiles != nil {
		files, err := s.internalFiles.InternalFileCatalog(ctx)
		if err != nil {
			return TemplateCatalog{}, err
		}
		catalog.PanelFiles = files
	}
	return catalog, nil
}

func (s *Service) List(ctx context.Context) ([]Application, error) {
	var rows []models.Application
	if err := orm.New(s.db).From("applications").Where("kind<>?", ApplicationKindFacility).And("deletion_requested=0").OrderBy("name ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	routesByApp, err := s.loadReverseProxyRoutesByApp(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]Application, 0, len(rows))
	for _, m := range rows {
		app := toDomainApplication(m)
		if routes := routesByApp[app.ID]; len(routes) > 0 {
			app.ReverseProxy = routes
		}
		apps = append(apps, app)
	}
	for i := range apps {
		app, err := s.withImageUpdateStatus(ctx, apps[i])
		if err != nil {
			return nil, err
		}
		apps[i] = app
	}
	return apps, nil
}

// ListSummaries is the list-specific read path. It intentionally avoids the
// application detail scanner and enriches runtime state with a fixed number of
// local queries.
func (s *Service) ListSummaries(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[ApplicationSummary], error) {
	base := orm.New(s.db).From("applications").Where("kind<>?", ApplicationKindFacility).And("deletion_requested=0")
	if query != "" {
		term := orm.LikeEscaped(query)
		base.WhereGroup(func(c *orm.Condition) {
			c.Or("name LIKE ? ESCAPE '\\'", term)
			c.Or("image_reference LIKE ? ESCAPE '\\'", term)
			c.Or("namespace LIKE ? ESCAPE '\\'", term)
		})
	}
	total64, err := base.Count(ctx)
	if err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}
	total := int(total64)
	var rows []models.Application
	err = base.Select("id", "name", "enabled", "reconcile_stopped", "image_reference", "image_update_available", "job_id", "namespace", "last_error", "updated_at").
		OrderBy("name ASC", "id ASC").Limit(pageSize).Offset((page-1)*pageSize).All(ctx, &rows)
	if err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}

	summaries := []ApplicationSummary{}
	byID := map[string]int{}
	for _, m := range rows {
		summary := ApplicationSummary{
			ID:                   m.ID,
			Name:                 m.Name,
			Enabled:              m.Enabled,
			ReconcileStopped:     m.ReconcileStopped,
			ImageReference:       m.ImageReference,
			ImageUpdateAvailable: m.ImageUpdateAvailable,
			JobID:                m.JobID,
			Namespace:            m.Namespace,
			LastError:            m.LastError,
			UpdatedAt:            m.UpdatedAt,
		}
		byID[summary.ID] = len(summaries)
		summaries = append(summaries, summary)
	}
	if len(summaries) == 0 {
		return httpx.ListPage[ApplicationSummary]{Items: summaries, Total: total, Page: page, PageSize: pageSize}, nil
	}

	pageIDs := make([]any, 0, len(summaries))
	for _, summary := range summaries {
		pageIDs = append(pageIDs, summary.ID)
	}
	statuses := make(map[string][]appruntime.InstanceStatus, len(summaries))
	instanceCounts := make(map[string]int, len(summaries))
	var instanceRows []models.ApplicationInstance
	if err := orm.New(s.db).From("application_instances").Select("application_id", "status", "observed_state", "observed_source").AndIn("application_id", pageIDs).All(ctx, &instanceRows); err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}
	for _, m := range instanceRows {
		status := m.Status
		if strings.TrimSpace(m.ObservedSource) != "" && strings.TrimSpace(m.ObservedState) != "" {
			status = m.ObservedState
		}
		statuses[m.ApplicationID] = append(statuses[m.ApplicationID], appruntime.InstanceStatus{Status: status})
		instanceCounts[m.ApplicationID]++
	}

	// 列表摘要只读 AppDB 实例观测与活跃 Job：pending/running Job 表示部署
	// 中，failed_retryable 表示失败重试；不再读取旧 lifecycle 表。
	activeJobStates := make(map[string]string, len(summaries))
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(pageIDs)), ",")
	jobRows, err := s.db.QueryContext(ctx, `SELECT application_id,state FROM jobs
		WHERE application_id IN (`+placeholders+`) AND state IN ('pending','running','failed_retryable')`, pageIDs...)
	if err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var appID, state string
		if err := jobRows.Scan(&appID, &state); err != nil {
			return httpx.ListPage[ApplicationSummary]{}, err
		}
		if _, ok := byID[appID]; ok {
			if state == "failed_retryable" {
				activeJobStates[appID] = "failed"
			} else if activeJobStates[appID] != "failed" {
				activeJobStates[appID] = "deploying"
			}
		}
	}
	if err := jobRows.Err(); err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}

	for i := range summaries {
		status := aggregateRuntimeStatus(summaries[i].Enabled, statuses[summaries[i].ID])
		if jobStatus := activeJobStates[summaries[i].ID]; jobStatus != "" {
			status = jobStatus
		}
		summaries[i].RuntimeStatus = status
		summaries[i].InstanceCount = instanceCounts[summaries[i].ID]
	}
	return httpx.ListPage[ApplicationSummary]{Items: summaries, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Service) ListWithRuntime(ctx context.Context) ([]Application, error) {
	apps, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range apps {
		app, err := s.withRuntimeSummary(ctx, apps[i])
		if err != nil {
			return nil, err
		}
		apps[i] = app
	}
	return apps, nil
}

func (s *Service) ListForReconcile(ctx context.Context) ([]Application, error) {
	var rows []models.Application
	if err := orm.New(s.db).From("applications").Where("deletion_requested=0").OrderBy("kind ASC", "name ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	routesByApp, err := s.loadReverseProxyRoutesByApp(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]Application, 0, len(rows))
	for _, m := range rows {
		app := toDomainApplication(m)
		if app.Kind != ApplicationKindFacility {
			// 设施应用的 reverse_proxy_routes 行是设施域名路由（target_port 恒
			// 为 0），不属于应用反向代理规则，协调巡检只读取状态字段。
			if routes := routesByApp[app.ID]; len(routes) > 0 {
				app.ReverseProxy = routes
			}
		}
		apps = append(apps, app)
	}
	return apps, nil
}

func (s *Service) Get(ctx context.Context, appID string) (Application, error) {
	app, err := s.getApplication(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	return s.withImageUpdateStatus(ctx, app)
}

func (s *Service) getApplication(ctx context.Context, appID string) (Application, error) {
	var m models.Application
	err := orm.New(s.db).From("applications").Where("id=?", appID).First(ctx, &m)
	if err == sql.ErrNoRows {
		return Application{}, panelerr.NotFound("application")
	}
	if err != nil {
		return Application{}, err
	}
	app := toDomainApplication(m)
	if app.Kind != ApplicationKindFacility {
		// 设施应用（如 facility-reverse-proxy）的 reverse_proxy_routes 行是
		// 设施域名路由，由设施模块管理（target_port 恒为 0，不使用应用目标
		// 端口语义），不得作为应用反向代理规则加载，否则部署规划会在
		// normalizeReverseProxyRules 处把它们当作非法端口拒绝。
		routes, err := s.loadReverseProxyRoutes(ctx, appID)
		if err != nil {
			return Application{}, err
		}
		app.ReverseProxy = routes
	}
	return app, nil
}

func (s *Service) Create(ctx context.Context, in SaveInput) (Application, error) {
	return s.createWithFiles(ctx, in, nil)
}

func (s *Service) createWithFiles(ctx context.Context, in SaveInput, files []ApplicationFile) (Application, error) {
	return s.createWithFilesID(ctx, id.New("app"), in, files)
}

func (s *Service) createWithFilesID(ctx context.Context, appID string, in SaveInput, files []ApplicationFile) (Application, error) {
	files = normalizeApplicationFilesForSave(appID, files, time.Now().UTC())
	prepared, err := s.prepare(ctx, in, 1, appID)
	if err != nil {
		return Application{}, err
	}
	if files != nil {
		prepared, err = s.prepareWithFiles(ctx, in, 1, appID, files)
		if err != nil {
			return Application{}, err
		}
	}
	now := time.Now().UTC()
	app := Application{
		ID:                appID,
		Kind:              ApplicationKindUser,
		Name:              in.Name,
		Enabled:           in.Enabled,
		SpecYAML:          in.SpecYAML,
		PersistentPath:    prepared.persistentPath,
		DeploymentMode:    prepared.deploymentMode,
		DeploymentServers: prepared.deploymentServers,
		ReverseProxy:      prepared.reverseProxy,
		Generation:        1,
		SpecHash:          prepared.hash,
		JobID:             prepared.job.ID,
		Namespace:         s.currentConfig().Namespace,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if app.Name == "" {
		app.Name = prepared.spec.Name
	}
	if app.Enabled {
		prepared.job, _, err = s.renderApplicationWithFiles(ctx, app, files)
		if err != nil {
			return Application{}, err
		}
	}
	if files == nil {
		if err := s.insertApplicationWithRoutes(ctx, app); err != nil {
			return Application{}, applicationSaveError(err)
		}
		s.insertRevisionBestEffort(ctx, app, prepared.job)
		if app.Enabled {
			if err := s.triggerApplicationReconcile(ctx, app, prepared.job, "application_save", "Syncing application "+app.Name); err != nil {
				return Application{}, err
			}
		}
		if err := s.reconcileReverseProxy(ctx); err != nil {
			return Application{}, err
		}
		return s.Get(ctx, app.ID)
	}
	if err := s.commitApplicationState(ctx, app, files, prepared.job, true, true); err != nil {
		return Application{}, applicationSaveError(err)
	}
	if app.Enabled {
		if err := s.triggerApplicationReconcile(ctx, app, prepared.job, "application_save", "Syncing application "+app.Name); err != nil {
			return Application{}, err
		}
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func (s *Service) Update(ctx context.Context, appID string, in SaveInput) (Application, error) {
	return s.updateWithFiles(ctx, appID, in, nil)
}

func (s *Service) updateWithFiles(ctx context.Context, appID string, in SaveInput, files []ApplicationFile) (Application, error) {
	return s.updateWithFilesVersioned(ctx, appID, 0, false, in, files)
}

func (s *Service) updateWithFilesIfVersion(ctx context.Context, appID string, expectedVersion int, in SaveInput, files []ApplicationFile) (Application, error) {
	return s.updateWithFilesVersioned(ctx, appID, expectedVersion, true, in, files)
}

func (s *Service) updateWithFilesVersioned(ctx context.Context, appID string, expectedVersion int, enforceVersion bool, in SaveInput, files []ApplicationFile) (Application, error) {
	current, err := s.Get(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	if enforceVersion && current.Version != expectedVersion {
		return Application{}, resourceVersionConflict(expectedVersion, current.Version)
	}
	var currentFiles []ApplicationFile
	if files != nil {
		currentFiles, err = s.listFiles(ctx, appID, false)
		if err != nil {
			return Application{}, err
		}
		files = normalizeApplicationFilesForSave(appID, files, time.Now().UTC())
	}
	generation := current.Generation
	prepared, err := s.prepare(ctx, in, generation, appID)
	if err != nil {
		return Application{}, err
	}
	if files != nil {
		prepared, err = s.prepareWithFiles(ctx, in, generation, appID, files)
		if err != nil {
			return Application{}, err
		}
	}
	if prepared.hash != current.SpecHash {
		generation++
		prepared, err = s.prepare(ctx, in, generation, appID)
		if err != nil {
			return Application{}, err
		}
		if files != nil {
			prepared, err = s.prepareWithFiles(ctx, in, generation, appID, files)
			if err != nil {
				return Application{}, err
			}
		}
	}
	app := current
	app.Name = in.Name
	if app.Name == "" {
		app.Name = prepared.spec.Name
	}
	app.Enabled = in.Enabled
	app.SpecYAML = in.SpecYAML
	app.PersistentPath = prepared.persistentPath
	app.DeploymentMode = prepared.deploymentMode
	app.DeploymentServers = prepared.deploymentServers
	app.ReverseProxy = prepared.reverseProxy
	app.Generation = generation
	app.SpecHash = prepared.hash
	app.JobID = prepared.job.ID
	app.Namespace = s.currentConfig().Namespace
	app.UpdatedAt = time.Now().UTC()
	configurationChanged := !applicationUserConfigurationEqual(current, app)
	if files != nil && !applicationFileSetsMatch(currentFiles, files) {
		configurationChanged = true
	}
	shouldDeploy := app.Enabled && (!current.Enabled || prepared.hash != current.SpecHash)
	shouldStop := current.Enabled && !app.Enabled
	if shouldDeploy {
		job, issues, err := s.renderApplicationWithFiles(ctx, app, files)
		if err != nil {
			return Application{}, err
		}
		if len(issues) > 0 {
			return Application{}, applicationValidationError(issues)
		}
		prepared.job = job
	}
	if files == nil {
		if configurationChanged {
			if err := s.updateApplicationWithRoutes(ctx, app); err != nil {
				return Application{}, applicationSaveError(err)
			}
		} else if err := s.updateApplicationDerived(ctx, app); err != nil {
			return Application{}, err
		}
		if prepared.hash != current.SpecHash {
			s.insertRevisionBestEffort(ctx, app, prepared.job)
		}
		if shouldDeploy || shouldStop {
			if err := s.triggerApplicationReconcile(ctx, app, prepared.job, "application_save", "Syncing application "+app.Name); err != nil {
				return Application{}, err
			}
		}
		if err := s.reconcileReverseProxy(ctx); err != nil {
			return Application{}, err
		}
		return s.Get(ctx, app.ID)
	}
	if err := s.commitApplicationStateVersioned(ctx, app, files, prepared.job, false, prepared.hash != current.SpecHash, expectedVersion, enforceVersion, configurationChanged); err != nil {
		return Application{}, applicationSaveError(err)
	}
	if shouldDeploy || shouldStop {
		if err := s.triggerApplicationReconcile(ctx, app, prepared.job, "application_save", "Syncing application "+app.Name); err != nil {
			return Application{}, err
		}
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func applicationUserConfigurationEqual(left, right Application) bool {
	leftRaw, err1 := json.Marshal(saveInputFromApplication(left))
	rightRaw, err2 := json.Marshal(saveInputFromApplication(right))
	return err1 == nil && err2 == nil && string(leftRaw) == string(rightRaw)
}

func resourceVersionConflict(expected, current int) error {
	return panelerr.WithDetails(panelerr.Conflict("resource_version_conflict", "application changed while editing"), map[string]any{
		"expectedVersion": strconv.Itoa(expected),
		"currentVersion":  strconv.Itoa(current),
	})
}

func (s *Service) Delete(ctx context.Context, appID string) error {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return err
	}
	app.Enabled = false
	app.DeletionRequested = true
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplicationLifecycleFlags(ctx, app.ID, false, true); err != nil {
		return err
	}
	if instances, err := s.runtimeInstances(ctx, app.ID); err != nil {
		return err
	} else if len(instances) == 0 {
		if err := orm.New(s.db).From("applications").Where("id=?", app.ID).And("deletion_requested=1").Delete(ctx); err != nil {
			return err
		}
		return s.reconcileReverseProxy(ctx)
	}
	if err := s.triggerApplicationReconcileWithPayload(ctx, app.ID, "application_delete", map[string]any{
		"applicationIds": []string{app.ID},
		"force":          true,
		"purge":          true,
		"reason":         "application_delete",
	}); err != nil {
		return err
	}
	return s.reconcileReverseProxy(ctx)
}

func (s *Service) Validate(ctx context.Context, appID string) (ValidationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return ValidationResult{}, err
	}
	_, issues, err := s.renderApplication(ctx, app)
	if err != nil || len(issues) > 0 {
		return validationResult(issues), err
	}
	return validationResult(issues), nil
}

func (s *Service) RunImageCheckTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	app, err := s.checkImageUpdate(ctx, appID)
	if err != nil {
		return err
	}
	if s.tasks == nil {
		return nil
	}
	return s.tasks.Complete(ctx, task.ID, "Checked image for "+app.Name)
}

func (s *Service) checkImageUpdate(ctx context.Context, appID string) (Application, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	app, err = s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return Application{}, err
	}
	result, checkedAt, resolveErr := s.resolveApplicationImage(ctx, app)
	app.ImageCheckedAt = &checkedAt
	if result.Reference != "" {
		if app.ImageReference != "" && app.ImageReference != result.Reference {
			app.ImageDigest = ""
		}
		app.ImageReference = result.Reference
	}
	if resolveErr != nil {
		app.ImageLastError = resolveErr.Error()
		app.UpdatedAt = time.Now().UTC()
		if err := s.updateApplicationDerived(ctx, app); err != nil {
			return Application{}, err
		}
		return s.Get(ctx, app.ID)
	}
	app.ImageLatestDigest = result.Digest
	if app.ImageDigest == "" {
		app.ImageDigest = result.Digest
	}
	app.ImageUpdateAvailable = app.ImageDigest != "" && result.Digest != "" && app.ImageDigest != result.Digest
	app.ImageLastError = ""
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplicationDerived(ctx, app); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func (s *Service) UpdateImage(ctx context.Context, appID string) (OperationResult, error) {
	app, job, err := s.prepareImageUpdate(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.triggerApplicationReconcile(ctx, app, job, "application_image_update", "Updating image for "+app.Name); err != nil {
		return OperationResult{}, err
	}
	if err := s.markApplicationImageTargetsCurrent(ctx, app); err != nil {
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	app, _ = s.withImageUpdateStatus(ctx, app)
	return OperationResult{EvalID: app.LastEvalID, Application: app}, nil
}

func (s *Service) RunImageUpdateTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	if s.tasks != nil && task.ID != "" {
		if err := s.tasks.Start(ctx, task.ID); err != nil {
			return err
		}
	}
	app, job, err := s.prepareImageUpdate(ctx, appID)
	if err != nil {
		return err
	}
	if err := s.triggerApplicationReconcile(ctx, app, job, "application_image_update", "Updating image for "+app.Name); err != nil {
		return err
	}
	if err := s.markApplicationImageTargetsCurrent(ctx, app); err != nil {
		return err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return err
	}
	if s.tasks != nil && task.ID != "" {
		return s.tasks.Complete(ctx, task.ID, "Application image update planned")
	}
	return nil
}

func (s *Service) prepareImageUpdate(ctx context.Context, appID string) (Application, appruntime.Spec, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if !app.Enabled {
		return Application{}, appruntime.Spec{}, panelerr.Conflict("application_disabled", "enable the application before updating its image")
	}
	app, err = s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	result, checkedAt, err := s.resolveApplicationImage(ctx, app)
	if err != nil {
		app.ImageCheckedAt = &checkedAt
		app.ImageLastError = err.Error()
		app.UpdatedAt = time.Now().UTC()
		_ = s.updateApplicationDerived(ctx, app)
		return Application{}, appruntime.Spec{}, err
	}
	app.Generation++
	app.ImageReference = result.Reference
	app.ImageDigest = result.Digest
	app.ImageLatestDigest = result.Digest
	app.ImageCheckedAt = &checkedAt
	app.ImageUpdateAvailable = false
	app.ImageLastError = ""
	app.UpdatedAt = time.Now().UTC()
	job, issues, err := s.renderApplication(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(issues) > 0 {
		return Application{}, appruntime.Spec{}, applicationValidationError(issues)
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(targets) == 0 {
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	if err := s.updateApplicationDerived(ctx, app); err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	s.insertRevisionBestEffort(ctx, app, job)
	return app, job, nil
}

func (s *Service) markApplicationImageTargetsCurrent(ctx context.Context, app Application) error {
	if strings.TrimSpace(app.ImageDigest) == "" {
		return nil
	}
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return err
	}
	checkedAt := time.Now().UTC()
	if app.ImageCheckedAt != nil && !app.ImageCheckedAt.IsZero() {
		checkedAt = *app.ImageCheckedAt
	}
	for _, instance := range instances {
		references := imageReferenceCandidates(instance.RuntimeSpec.Image, app.ImageReference)
		if len(references) == 0 {
			continue
		}
		for _, reference := range references {
			if _, err := orm.RawExec(ctx, s.db, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at)
			VALUES(?,?,?,?,0,'',?)
			ON CONFLICT(server_id,reference) DO UPDATE SET
				local_digest=excluded.local_digest,
				latest_digest=excluded.latest_digest,
				update_available=0,
				last_error='',
				checked_at=excluded.checked_at`,
				instance.ServerID, reference, app.ImageDigest, app.ImageDigest, formatTime(checkedAt)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Deploy(ctx context.Context, appID string) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	app, err = s.enableApplicationForDeploy(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	app, _, err = s.prepareDeploy(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	task, err := s.triggerApplicationReconcileTask(ctx, app.ID, "application_sync", map[string]any{
		"applicationIds": []string{app.ID},
		"reason":         "application_sync",
	})
	if err != nil {
		return OperationResult{}, err
	}
	result := OperationResult{TaskID: task.ID, Application: app}
	if runtime, err := s.Runtime(ctx, app.ID); err == nil {
		result.ApplicationRuntime = &runtime
	}
	return result, nil
}

// RunDeploymentProjectionTask keeps old target-task rows readable without
// allowing them to own runtime execution in a production process. New code
// persists an AppDB Job and the orchestrator performs the only RuntimeReconcile
// call. The legacy executor remains available when a service is constructed by
// migration/compatibility tests before the controller is started.

func (s *Service) prepareDeploy(ctx context.Context, appID string) (Application, appruntime.Spec, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	app, err = s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	job, issues, err := s.renderApplication(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(issues) > 0 {
		return Application{}, appruntime.Spec{}, applicationValidationError(issues)
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(targets) == 0 {
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	return app, job, nil
}

func (s *Service) enableApplicationForDeploy(ctx context.Context, app Application) (Application, error) {
	if app.Enabled {
		return app, nil
	}
	if app.DeletionRequested {
		return app, panelerr.Conflict("application_deletion_requested", "Application deletion is already requested")
	}
	app.Enabled = true
	app.UpdatedAt = time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.updateApplicationWithExecIfVersion(ctx, tx, app, app.Version); err != nil {
		return app, err
	}
	if err := tx.Commit(); err != nil {
		return app, err
	}
	app.Version++
	return app, nil
}

func (s *Service) RedeployChangedApplications(ctx context.Context) (int, error) {
	apps, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	redeployed := 0
	for _, app := range apps {
		changed, refreshed, spec, err := s.prepareChangedApplicationRefresh(ctx, app)
		if err != nil {
			return redeployed, err
		}
		if !changed {
			continue
		}
		if err := s.triggerApplicationReconcile(ctx, refreshed, spec, "application_refresh", "Refreshing application "+refreshed.Name); err != nil {
			return redeployed, err
		}
		redeployed++
	}
	if redeployed > 0 {
		if err := s.reconcileReverseProxy(ctx); err != nil {
			return redeployed, err
		}
	}
	return redeployed, nil
}

func (s *Service) RunRefreshTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	if s.tasks != nil && task.ID != "" {
		if err := s.tasks.Start(ctx, task.ID); err != nil {
			return err
		}
	}
	app, err := s.Get(ctx, appID)
	if err != nil {
		return err
	}
	changed, refreshed, spec, err := s.prepareChangedApplicationRefresh(ctx, app)
	if err != nil {
		return err
	}
	if !changed {
		if s.tasks == nil {
			return nil
		}
		return s.tasks.Complete(ctx, task.ID, "Application refresh not needed")
	}
	if err := s.triggerApplicationReconcile(ctx, refreshed, spec, "application_refresh", "Refreshing application "+refreshed.Name); err != nil {
		return err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return err
	}
	if s.tasks != nil && task.ID != "" {
		return s.tasks.Complete(ctx, task.ID, "Application refresh planned")
	}
	return nil
}

func (s *Service) prepareChangedApplicationRefresh(ctx context.Context, app Application) (bool, Application, appruntime.Spec, error) {
	if !app.Enabled {
		return false, app, appruntime.Spec{}, nil
	}
	beforeHash := app.SpecHash
	refreshed, err := s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return false, Application{}, appruntime.Spec{}, err
	}
	if refreshed.SpecHash == beforeHash {
		return false, refreshed, appruntime.Spec{}, nil
	}
	spec, issues, err := s.renderApplication(ctx, refreshed)
	if err != nil {
		return false, Application{}, appruntime.Spec{}, err
	}
	if len(issues) > 0 {
		return false, Application{}, appruntime.Spec{}, applicationValidationError(issues)
	}
	targets, err := s.deploymentTargets(ctx, refreshed)
	if err != nil {
		return false, Application{}, appruntime.Spec{}, err
	}
	if len(targets) == 0 {
		return false, Application{}, appruntime.Spec{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	refreshed.UpdatedAt = time.Now().UTC()
	if err := s.updateApplicationDerived(ctx, refreshed); err != nil {
		return false, Application{}, appruntime.Spec{}, err
	}
	return true, refreshed, spec, nil
}

func (s *Service) RedeployEnabledApplications(ctx context.Context) (int, error) {
	apps, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	redeployed := 0
	for _, app := range apps {
		if !app.Enabled {
			continue
		}
		refreshed, _, err := s.prepareEnabledApplicationRedeploy(ctx, app)
		if err != nil {
			return redeployed, err
		}
		if err := s.triggerApplicationReconcileWithPayload(ctx, refreshed.ID, "application_redeploy", map[string]any{
			"applicationIds": []string{refreshed.ID},
			"force":          true,
			"reason":         "application_redeploy",
		}); err != nil {
			return redeployed, err
		}
		redeployed++
	}
	return redeployed, nil
}

func (s *Service) prepareEnabledApplicationRedeploy(ctx context.Context, app Application) (Application, appruntime.Spec, error) {
	refreshed, err := s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	spec, issues, err := s.renderApplication(ctx, refreshed)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(issues) > 0 {
		return Application{}, appruntime.Spec{}, applicationValidationError(issues)
	}
	targets, err := s.deploymentTargets(ctx, refreshed)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(targets) == 0 {
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	return refreshed, spec, nil
}

func (s *Service) Stop(ctx context.Context, appID string, purge bool) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	app.Enabled = false
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplicationLifecycleFlags(ctx, app.ID, false, false); err != nil {
		return OperationResult{}, err
	}
	payload := map[string]any{
		"applicationIds": []string{app.ID},
		"force":          true,
		"reason":         "application_stop",
	}
	if purge {
		payload["purge"] = true
	}
	if err := s.triggerApplicationReconcileWithPayload(ctx, app.ID, "application_stop", payload); err != nil {
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	return OperationResult{Application: app}, nil
}

func (s *Service) RunStopTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	if s.tasks != nil && task.ID != "" {
		if err := s.tasks.Start(ctx, task.ID); err != nil {
			return err
		}
	}
	opts, err := stopTaskOptions(task)
	if err != nil {
		return err
	}
	result, err := s.Stop(ctx, appID, opts.purge)
	if err != nil {
		return err
	}
	if s.tasks != nil && task.ID != "" {
		return s.tasks.Complete(ctx, task.ID, firstNonEmpty(result.DeploymentID, "Application stop planned"))
	}
	return nil
}

func (s *Service) Restart(ctx context.Context, appID string) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	plan, err := s.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID: app.ID,
		Force:         true,
		Manual:        true,
		TriggerType:   "application_restart",
		Reason:        "application_restart",
	})
	if err != nil {
		return OperationResult{}, err
	}
	runtime, err := s.Runtime(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{DeploymentID: firstString(plan.JobIDs), ApplicationRuntime: &runtime}, nil
}

func (s *Service) RunRestartTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	result, err := s.Restart(ctx, appID)
	if err != nil {
		return err
	}
	if s.tasks != nil && task.ID != "" {
		return s.tasks.Complete(ctx, task.ID, firstNonEmpty(result.DeploymentID, "Application restart planned"))
	}
	return nil
}

func (s *Service) Runtime(ctx context.Context, appID string) (ApplicationRuntime, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return ApplicationRuntime{}, err
	}
	out := ApplicationRuntime{
		ApplicationID: app.ID,
		RuntimeID:     app.JobID,
		Status:        appruntime.StatusStopped,
		ObservedAt:    time.Now().UTC(),
	}
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return ApplicationRuntime{}, err
	}
	out.Instances = s.refreshInstanceStatuses(ctx, instances)
	out.Status, out.Operation = s.jobDerivedRuntimeStatus(ctx, app, out.Instances)
	return out, nil
}

func (s *Service) withRuntimeSummary(ctx context.Context, app Application) (Application, error) {
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return Application{}, err
	}
	statuses := s.cachedInstanceStatuses(ctx, instances)
	status, _ := s.jobDerivedRuntimeStatus(ctx, app, statuses)
	app.RuntimeStatus = status
	return app, nil
}

// jobDerivedRuntimeStatus 从 AppDB 实例观测与活跃 Job 派生运行时状态与
// 当前操作投影：存在 pending/running Job 表示部署中，failed_retryable
// 表示失败重试；否则按实例观测聚合。不再读取旧 lifecycle 表。
func (s *Service) jobDerivedRuntimeStatus(ctx context.Context, app Application, instanceStatuses []appruntime.InstanceStatus) (string, *LifecycleOperation) {
	base := aggregateRuntimeStatus(app.Enabled, instanceStatuses)
	if s == nil || s.db == nil {
		return base, nil
	}
	var rows []struct {
		ID                string
		Action            string
		State             string
		Trigger           string
		DesiredGeneration int
		DesiredSpecHash   string
		UpdatedAt         string
	}
	if err := orm.New(s.db).From("jobs").Select("id", "action", "state", "trigger_type", "desired_generation", "desired_spec_hash", "updated_at").
		Where("application_id=?", app.ID).And("state IN (?,?,?)", controlplane.JobPending, controlplane.JobRunning, controlplane.JobFailedRetryable).
		All(ctx, &rows); err != nil {
		return base, nil
	}
	if len(rows) == 0 {
		return base, nil
	}
	status := appruntime.StatusDeploying
	var first LifecycleOperation
	for _, row := range rows {
		if row.State == controlplane.JobFailedRetryable {
			status = appruntime.StatusFailed
		}
		if first.ID == "" {
			first = LifecycleOperation{
				ID:            row.ID,
				ApplicationID: app.ID,
				Type:          row.Action,
				Status:        status,
				Generation:    row.DesiredGeneration,
				SpecHash:      row.DesiredSpecHash,
				Trigger:       row.Trigger,
				CreatedAt:     parseApplicationTime(row.UpdatedAt),
				UpdatedAt:     parseApplicationTime(row.UpdatedAt),
			}
		}
	}
	first.Status = status
	return status, &first
}

func parseApplicationTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func (s *Service) withImageUpdateStatus(ctx context.Context, app Application) (Application, error) {
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return Application{}, err
	}
	if len(instances) == 0 {
		return app, nil
	}
	targets := make([]ImageUpdateTarget, 0, len(instances))
	updateAvailable := false
	var latestCheckedAt *time.Time
	var firstLatestDigest string
	var firstLocalDigest string
	for _, instance := range instances {
		reference := strings.TrimSpace(instance.RuntimeSpec.Image)
		if reference == "" {
			reference = app.ImageReference
		}
		target := ImageUpdateTarget{
			ServerID:   instance.ServerID,
			ServerName: serverNameForImageTarget(ctx, s.servers, instance.ServerID),
			Reference:  reference,
		}
		if reference != "" {
			cached, ok, err := s.cachedImageUpdate(ctx, instance.ServerID, imageReferenceCandidates(reference, app.ImageReference))
			if err != nil {
				return Application{}, err
			}
			if ok {
				target.LocalDigest = cached.LocalDigest
				target.LatestDigest = cached.LatestDigest
				target.UpdateAvailable = cached.UpdateAvailable
				target.CheckedAt = cached.CheckedAt
				target.LastError = cached.LastError
				if firstLocalDigest == "" {
					firstLocalDigest = cached.LocalDigest
				}
				if firstLatestDigest == "" {
					firstLatestDigest = cached.LatestDigest
				}
				if cached.CheckedAt != nil && (latestCheckedAt == nil || cached.CheckedAt.After(*latestCheckedAt)) {
					checked := *cached.CheckedAt
					latestCheckedAt = &checked
				}
			}
		}
		if target.UpdateAvailable {
			updateAvailable = true
		}
		targets = append(targets, target)
	}
	app.ImageUpdateTargets = targets
	app.ImageUpdateAvailable = updateAvailable
	if firstLocalDigest != "" && app.ImageDigest == "" {
		app.ImageDigest = firstLocalDigest
	}
	if firstLatestDigest != "" {
		app.ImageLatestDigest = firstLatestDigest
	}
	if latestCheckedAt != nil {
		app.ImageCheckedAt = latestCheckedAt
	}
	return app, nil
}

func (s *Service) Logs(ctx context.Context, appID string, in LogInput) (LogResult, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return LogResult{}, err
	}
	in.Tail = normalizeLogTail(in.Tail)
	instance, err := s.runtimeInstance(ctx, appID, in.InstanceID)
	if err != nil {
		return LogResult{}, err
	}
	srv, err := s.servers.Get(ctx, instance.ServerID)
	if err != nil {
		return LogResult{}, err
	}
	if err := ensureAgentRuntimeReady(srv); err != nil {
		return LogResult{}, err
	}
	baseURL, _ := agentURLFromServer(srv)
	logs, err := s.runtimeClient.RuntimeLogs(ctx, baseURL, instance.ID, instance.ContainerName, in.Tail)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return LogResult{}, runtimeOperationError(err)
	}
	return LogResult{InstanceID: instance.ID, ContainerName: instance.ContainerName, Type: "combined", Logs: logs.Logs}, nil
}

func (s *Service) PersistentData(ctx context.Context, appID string) (PackageResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return PackageResult{}, err
	}
	if strings.TrimSpace(app.PersistentPath) == "" {
		return PackageResult{}, panelerr.Validation("application_persistent_data_unavailable", "Application does not use persistent storage")
	}
	instance, err := s.primaryRuntimeInstance(ctx, app.ID)
	if err != nil {
		return PackageResult{}, err
	}
	srv, err := s.servers.Get(ctx, instance.ServerID)
	if err != nil {
		return PackageResult{}, err
	}
	if err := ensureAgentRuntimeReady(srv); err != nil {
		return PackageResult{}, err
	}
	baseURL, _ := agentURLFromServer(srv)
	archive, err := s.runtimeClient.RuntimePersistentArchive(ctx, baseURL, app.ID)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return PackageResult{}, runtimeOperationError(err)
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(archive.ContentBase64))
	if err != nil {
		return PackageResult{}, panelerr.BadGateway("application_persistent_archive_invalid", "Persistent data archive from agent is invalid")
	}
	filename := strings.TrimSpace(archive.Filename)
	if filename == "" {
		filename = app.Name + "-persistent.zip"
	}
	return PackageResult{Filename: filename, Content: content}, nil
}

func (s *Service) RestorePersistentData(ctx context.Context, appID string, content []byte) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	if strings.TrimSpace(app.PersistentPath) == "" {
		return OperationResult{}, panelerr.Validation("application_persistent_data_unavailable", "Application does not use persistent storage")
	}
	if len(content) == 0 {
		return OperationResult{}, panelerr.Validation("application_persistent_archive_required", "Persistent data archive is required")
	}
	instance, err := s.primaryRuntimeInstance(ctx, app.ID)
	shouldRestart := true
	serverID := instance.ServerID
	if err != nil {
		if !isPanelNotFound(err) {
			return OperationResult{}, err
		}
		serverID, err = persistentRestoreTargetServer(app)
		if err != nil {
			return OperationResult{}, err
		}
		shouldRestart = false
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := ensureAgentRuntimeReady(srv); err != nil {
		return OperationResult{}, err
	}
	baseURL, _ := agentURLFromServer(srv)
	if _, err := s.runtimeClient.RuntimePersistentRestore(ctx, baseURL, app.ID, content); err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return OperationResult{}, runtimeOperationError(err)
	}
	if !shouldRestart {
		return OperationResult{Application: app}, nil
	}
	plan, err := s.PlanApplicationDeployment(ctx, DeploymentPlanRequest{
		ApplicationID: app.ID,
		ServerIDs:     []string{serverID},
		Force:         true,
		Manual:        true,
		TriggerType:   "application_persistent_restore",
		Reason:        "application_persistent_restore",
	})
	if err != nil {
		return OperationResult{}, err
	}
	runtime, err := s.Runtime(ctx, app.ID)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{DeploymentID: firstString(plan.JobIDs), Application: app, ApplicationRuntime: &runtime}, nil
}

type preparedApplication struct {
	spec              appspec.Spec
	persistentPath    string
	deploymentMode    string
	deploymentServers []string
	reverseProxy      []ReverseProxyRule
	hash              string
	job               appruntime.Spec
}

func (s *Service) prepare(ctx context.Context, in SaveInput, generation int, appID string) (preparedApplication, error) {
	var files []ApplicationFile
	if appID != "" {
		var err error
		files, err = s.listFiles(ctx, appID, true)
		if err != nil {
			return preparedApplication{}, err
		}
	}
	return s.prepareWithFiles(ctx, in, generation, appID, files)
}

func (s *Service) prepareWithFiles(ctx context.Context, in SaveInput, generation int, appID string, files []ApplicationFile) (preparedApplication, error) {
	appContext := Application{ID: appID, Name: in.Name, Generation: generation, Namespace: s.currentConfig().Namespace, DeploymentMode: in.DeploymentMode}
	data, err := s.templateData(ctx, appContext, files, nil)
	if err != nil {
		return preparedApplication{}, err
	}
	renderedYAML, err := s.renderTemplate(ctx, in.SpecYAML, data)
	if err != nil {
		return preparedApplication{}, err
	}
	spec, specIssues := appspec.DecodeYAML(renderedYAML)
	if len(specIssues) > 0 {
		return preparedApplication{}, applicationSpecIssueError(specIssues[0])
	}
	persistentPath := ""
	if specUsesPersistentMount(spec) {
		persistentPath = applicationPersistentDir(appID)
	}
	deploymentMode, deploymentServers, err := normalizeDeploymentTargets(in.DeploymentMode, in.DeploymentServers, persistentPath)
	if err != nil {
		return preparedApplication{}, err
	}
	enforceHostModeProxyTarget(spec.NetworkMode, in.ReverseProxy)
	reverseProxy, err := normalizeReverseProxyRules(in.ReverseProxy)
	if err != nil {
		return preparedApplication{}, err
	}
	if s.proxyPolicy != nil {
		origins, err := s.proxyPolicy.ResolveApplicationOrigins(ctx, appID, deploymentMode, deploymentServers)
		if err != nil {
			return preparedApplication{}, err
		}
		for i := range reverseProxy {
			if strings.TrimSpace(reverseProxy[i].Domain) == "" {
				continue
			}
			if len(origins) == 0 {
				return preparedApplication{}, panelerr.Validation("reverse_proxy_origin_servers_required", "Reverse proxy route requires at least one origin server")
			}
			reverseProxy[i].OriginServerIDs = append([]string(nil), origins...)
			anyAccess, err := NormalizeAnyAccessConfig(reverseProxy[i].AnyAccess, origins)
			if err != nil {
				return preparedApplication{}, err
			}
			reverseProxy[i].AnyAccess = anyAccess
		}
	}
	resolvedReverseProxy, err := s.renderReverseProxyRules(ctx, reverseProxy, data)
	if err != nil {
		return preparedApplication{}, err
	}
	if s.proxyPolicy != nil {
		if err := s.proxyPolicy.ValidateApplicationReverseProxy(ctx, appID, deploymentMode, deploymentServers, resolvedReverseProxy); err != nil {
			return preparedApplication{}, err
		}
	}
	hash, err := applicationHash(spec, deploymentMode, deploymentServers, resolvedReverseProxy, files, data)
	if err != nil {
		return preparedApplication{}, err
	}
	cfg := s.currentConfig()
	job, renderIssues := appspec.Render(appspec.RenderInput{
		AppID:      appID,
		Generation: generation,
		SpecHash:   hash,
		Namespace:  cfg.Namespace,
		Region:     cfg.Region,
		Datacenter: cfg.Datacenter,
		Spec:       spec,
	})
	if len(renderIssues) > 0 {
		return preparedApplication{}, applicationValidationError(validationIssuesFromSpecIssues(renderIssues))
	}
	return preparedApplication{spec: spec, persistentPath: persistentPath, deploymentMode: deploymentMode, deploymentServers: deploymentServers, reverseProxy: reverseProxy, hash: hash, job: job}, nil
}

func (s *Service) renderApplication(ctx context.Context, app Application) (appruntime.Spec, []ValidationIssue, error) {
	return s.renderApplicationWithFiles(ctx, app, nil)
}

func (s *Service) renderApplicationWithFiles(ctx context.Context, app Application, files []ApplicationFile) (appruntime.Spec, []ValidationIssue, error) {
	var err error
	if files == nil {
		files, err = s.listFiles(ctx, app.ID, true)
		if err != nil {
			return appruntime.Spec{}, nil, err
		}
	}
	data, err := s.templateData(ctx, app, files, nil)
	if err != nil {
		return appruntime.Spec{}, nil, err
	}
	renderedYAML, err := s.renderTemplate(ctx, app.SpecYAML, data)
	if err != nil {
		return appruntime.Spec{}, nil, err
	}
	spec, specIssues := appspec.DecodeYAML(renderedYAML)
	issues := make([]ValidationIssue, 0, len(specIssues))
	for _, issue := range specIssues {
		issues = append(issues, ValidationIssue{Field: issue.Field, Message: issue.Message})
	}
	if len(issues) > 0 {
		return appruntime.Spec{}, issues, nil
	}
	cfg := s.currentConfig()
	job, renderIssues := appspec.Render(appspec.RenderInput{
		AppID:      app.ID,
		Generation: app.Generation,
		SpecHash:   app.SpecHash,
		Namespace:  cfg.Namespace,
		Region:     cfg.Region,
		Datacenter: cfg.Datacenter,
		Spec:       spec,
	})
	for _, issue := range renderIssues {
		issues = append(issues, ValidationIssue{Field: issue.Field, Message: issue.Message})
	}
	if len(issues) == 0 {
		job, err = s.attachFiles(ctx, job, spec, files, data)
		if err != nil {
			return appruntime.Spec{}, nil, err
		}
	}
	if len(issues) == 0 {
		job, err = applyDeploymentTargets(job, app)
		if err != nil {
			return appruntime.Spec{}, []ValidationIssue{{Field: "deploymentServers", Message: err.Error()}}, nil
		}
	}
	return job, issues, nil
}

func (s *Service) resolveApplicationImage(ctx context.Context, app Application) (ImageDigestResult, time.Time, error) {
	checkedAt := time.Now().UTC()
	if s.imageResolver == nil {
		return ImageDigestResult{}, checkedAt, panelerr.BadGateway("image_registry_unavailable", "image registry resolver is not configured")
	}
	files, err := s.listFiles(ctx, app.ID, true)
	if err != nil {
		return ImageDigestResult{}, checkedAt, err
	}
	data, err := s.templateData(ctx, app, files, nil)
	if err != nil {
		return ImageDigestResult{}, checkedAt, err
	}
	renderedYAML, err := s.renderTemplate(ctx, app.SpecYAML, data)
	if err != nil {
		return ImageDigestResult{}, checkedAt, err
	}
	spec, issues := appspec.DecodeYAML(renderedYAML)
	if len(issues) > 0 {
		return ImageDigestResult{}, checkedAt, applicationValidationError(validationIssuesFromSpecIssues(issues))
	}
	result, err := s.imageResolver.Resolve(ctx, spec.Image)
	if result.Reference == "" {
		result.Reference = strings.TrimSpace(spec.Image)
	}
	return result, checkedAt, err
}

func (s *Service) renderTemplate(ctx context.Context, source string, data map[string]any) (string, error) {
	if s.renderer == nil {
		return source, nil
	}
	return s.renderer.Render(ctx, source, data)
}

func (s *Service) templateData(ctx context.Context, app Application, files []ApplicationFile, target *server.Server) (map[string]any, error) {
	data := map[string]any{}
	data["files"] = fileVariables(files)
	if s.builtinResolver != nil {
		builtins, err := s.builtinResolver.BuiltinVariables(ctx, ApplicationVariableContext{Application: app, Config: s.currentConfig(), Server: target})
		if err != nil {
			return nil, err
		}
		for key, value := range builtins {
			data[key] = value
		}
	}
	return data, nil
}

// FailStaleTargetTaskAnchors 取消仍处于 queued/scheduled/failed_retryable、
// 但其生命周期目标已不存在、已终态或不再由该任务持有的目标任务锚点。
// 这类幽灵锚点不会自行结束，会永久占住服务器的部署并发键，必须定期清理。

// lifecycleHeartbeatResult 决定心跳错误与运行错误的优先级：租约丢失是所有权交接信号，
// 无论 run 返回什么普通错误都必须优先返回 errLifecycleTargetLeaseLost。

func (s *Service) loadReverseProxyRoutes(ctx context.Context, appID string) ([]ReverseProxyRule, error) {
	var rows []models.ReverseProxyRoute
	if err := orm.New(s.db).From("reverse_proxy_routes").Where("app_id=?", appID).OrderBy("domain ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]ReverseProxyRule, 0, len(rows))
	for _, row := range rows {
		out = append(out, reverseProxyRouteFromModel(row))
	}
	return out, nil
}

func (s *Service) loadReverseProxyRoutesByApp(ctx context.Context) (map[string][]ReverseProxyRule, error) {
	var rows []models.ReverseProxyRoute
	if err := orm.New(s.db).From("reverse_proxy_routes").OrderBy("domain ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string][]ReverseProxyRule{}
	for _, row := range rows {
		out[row.AppID] = append(out[row.AppID], reverseProxyRouteFromModel(row))
	}
	return out, nil
}

func replaceApplicationReverseProxyRoutes(ctx context.Context, exec orm.Executor, appID string, rules []ReverseProxyRule) error {
	if err := orm.New(exec).From("reverse_proxy_routes").Where("app_id=?", appID).Delete(ctx); err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	items := make([]*models.ReverseProxyRoute, 0, len(rules))
	for _, rule := range rules {
		route := reverseProxyRuleToModel(appID, rule)
		items = append(items, &route)
	}
	return orm.New(exec).From("reverse_proxy_routes").InsertBatch(ctx, items)
}

func reverseProxyRuleToModel(appID string, rule ReverseProxyRule) models.ReverseProxyRoute {
	anyAccessRaw, _ := json.Marshal(rule.AnyAccess)
	var anyAccess map[string]any
	_ = json.Unmarshal(anyAccessRaw, &anyAccess)
	pathsRaw, _ := json.Marshal(rule.Paths)
	var paths []map[string]any
	_ = json.Unmarshal(pathsRaw, &paths)
	now := time.Now().UTC()
	return models.ReverseProxyRoute{
		Domain:          strings.ToLower(strings.TrimSpace(rule.Domain)),
		AppID:           appID,
		OriginServerIDs: append([]string(nil), rule.OriginServerIDs...),
		AnyAccessJSON:   anyAccess,
		TargetType:      normalizeReverseProxyTargetType(rule.TargetType),
		TargetPort:      rule.TargetPort,
		PathsJSON:       paths,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func reverseProxyRouteFromModel(m models.ReverseProxyRoute) ReverseProxyRule {
	anyAccessRaw, _ := json.Marshal(m.AnyAccessJSON)
	var anyAccess AnyAccessConfig
	_ = json.Unmarshal(anyAccessRaw, &anyAccess)
	pathsRaw, _ := json.Marshal(m.PathsJSON)
	var paths []ReverseProxyPath
	_ = json.Unmarshal(pathsRaw, &paths)
	targetType := strings.TrimSpace(m.TargetType)
	if targetType == "" {
		targetType = ReverseProxyTargetLocal
	}
	return ReverseProxyRule{
		Domain:          m.Domain,
		TargetType:      targetType,
		TargetPort:      m.TargetPort,
		OriginServerIDs: append([]string(nil), m.OriginServerIDs...),
		AnyAccess:       anyAccess,
		Paths:           paths,
	}
}
func (s *Service) insertApplicationWithRoutes(ctx context.Context, app Application) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.insertApplicationWithExec(ctx, tx, app); err != nil {
		return err
	}
	if err := replaceApplicationReverseProxyRoutes(ctx, tx, app.ID, app.ReverseProxy); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) updateApplicationWithRoutes(ctx context.Context, app Application) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.updateApplicationWithExec(ctx, tx, app); err != nil {
		return err
	}
	if err := replaceApplicationReverseProxyRoutes(ctx, tx, app.ID, app.ReverseProxy); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) insertApplication(ctx context.Context, app Application) error {
	m := fromDomainApplication(app)
	m.Version = 1
	return orm.New(s.db).From("applications").Insert(ctx, m)
}

func (s *Service) updateApplication(ctx context.Context, app Application) error {
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}

	_, err = orm.RawExec(ctx, s.db, `UPDATE applications SET version=version+1,kind=?,name=?,enabled=?,deletion_requested=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		applicationKind(app.Kind), app.Name, boolInt(app.Enabled), boolInt(app.DeletionRequested), app.SpecYAML, app.DeploymentMode, string(deploymentServers), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
	return err
}

// updateApplicationLifecycleFlags 只更新 Stop/Delete 这类生命周期入口需要的列，
// 避免用旧快照整行覆盖并发编辑产生的用户字段；version 递增让进行中的编辑会话在提交时自然冲突。
func (s *Service) updateApplicationLifecycleFlags(ctx context.Context, appID string, enabled, deletionRequested bool) error {
	now := formatTime(time.Now().UTC())
	_, err := orm.RawExec(ctx, s.db, `UPDATE applications SET enabled=?,deletion_requested=?,version=version+1,updated_at=? WHERE id=?`,
		boolInt(enabled), boolInt(deletionRequested), now, appID)
	return err
}

// updateApplicationDerived persists renderer, image-inspection, and runtime
// snapshot data without changing the user configuration resource version or
// its updated_at timestamp. It deliberately excludes user-owned fields so a
// background refresh cannot overwrite a concurrent edit.
func (s *Service) updateApplicationDerived(ctx context.Context, app Application) error {
	return updateApplicationDerivedWithExec(ctx, s.db, app)
}

func updateApplicationDerivedWithExec(ctx context.Context, exec orm.Executor, app Application) error {
	return orm.New(exec).From("applications").Where("id=?", app.ID).UpdateColumns(ctx, map[string]any{
		"generation":             app.Generation,
		"spec_hash":              app.SpecHash,
		"image_reference":        app.ImageReference,
		"image_digest":           app.ImageDigest,
		"image_latest_digest":    app.ImageLatestDigest,
		"image_checked_at":       nullableTime(app.ImageCheckedAt),
		"image_update_available": boolInt(app.ImageUpdateAvailable),
		"image_last_error":       app.ImageLastError,
		"job_id":                 app.JobID,
		"namespace":              app.Namespace,
		"last_eval_id":           app.LastEvalID,
		"last_deployment_id":     app.LastDeploymentID,
		"last_error":             app.LastError,
	})
}

func (s *Service) commitApplicationState(ctx context.Context, app Application, files []ApplicationFile, job appruntime.Spec, insertApp bool, insertRevision bool) error {
	return s.commitApplicationStateVersioned(ctx, app, files, job, insertApp, insertRevision, 0, false, true)
}

func (s *Service) commitApplicationStateVersioned(ctx context.Context, app Application, files []ApplicationFile, job appruntime.Spec, insertApp bool, insertRevision bool, expectedVersion int, enforceVersion bool, configurationChanged bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if insertApp {
		if err := s.insertApplicationWithExec(ctx, tx, app); err != nil {
			return err
		}
	} else if configurationChanged {
		if enforceVersion {
			if err := s.updateApplicationWithExecIfVersion(ctx, tx, app, expectedVersion); err != nil {
				return err
			}
		} else if err := s.updateApplicationWithExec(ctx, tx, app); err != nil {
			return err
		}
	} else if enforceVersion {
		var currentVersion int
		if err := orm.New(tx).From("applications").Select("version").Where("id=?", app.ID).ScanValue(ctx, &currentVersion); err != nil {
			return err
		}
		if currentVersion != expectedVersion {
			return resourceVersionConflict(expectedVersion, currentVersion)
		}
	}
	if !insertApp && !configurationChanged {
		if err := updateApplicationDerivedWithExec(ctx, tx, app); err != nil {
			return err
		}
	}
	if configurationChanged {
		if err := replaceApplicationFiles(ctx, tx, app.ID, files); err != nil {
			return err
		}
	}
	if insertApp || configurationChanged {
		if err := replaceApplicationReverseProxyRoutes(ctx, tx, app.ID, app.ReverseProxy); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if insertRevision {
		s.insertRevisionBestEffort(ctx, app, job)
	}
	return nil
}

func (s *Service) updateApplicationWithExecIfVersion(ctx context.Context, exec *sql.Tx, app Application, expectedVersion int) error {
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}

	result, err := orm.RawExec(ctx, exec, `UPDATE applications SET version=version+1,kind=?,name=?,enabled=?,deletion_requested=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=? AND version=?`,
		applicationKind(app.Kind), app.Name, boolInt(app.Enabled), boolInt(app.DeletionRequested), app.SpecYAML, app.DeploymentMode, string(deploymentServers), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID, expectedVersion)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		var currentVersion int
		if scanErr := orm.New(exec).From("applications").Select("version").Where("id=?", app.ID).ScanValue(ctx, &currentVersion); scanErr != nil {
			currentVersion = expectedVersion + 1
		}
		return resourceVersionConflict(expectedVersion, currentVersion)
	}
	return nil
}

func (s *Service) insertApplicationWithExec(ctx context.Context, exec orm.Executor, app Application) error {
	m := fromDomainApplication(app)
	m.Version = 1
	return orm.New(exec).From("applications").Insert(ctx, m)
}

func (s *Service) updateApplicationWithExec(ctx context.Context, exec orm.Executor, app Application) error {
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}

	_, err = orm.RawExec(ctx, exec, `UPDATE applications SET version=version+1,kind=?,name=?,enabled=?,deletion_requested=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		applicationKind(app.Kind), app.Name, boolInt(app.Enabled), boolInt(app.DeletionRequested), app.SpecYAML, app.DeploymentMode, string(deploymentServers), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
	return err
}

func (s *Service) insertRevision(ctx context.Context, app Application, job appruntime.Spec) error {
	return insertRevisionWithExec(ctx, s.revisionDB(), app, job)
}

func (s *Service) insertRevisionBestEffort(ctx context.Context, app Application, job appruntime.Spec) {
	if err := s.insertRevision(ctx, app, job); err != nil {
		log.Printf("application revision record failed app_id=%s generation=%d: %v", app.ID, app.Generation, err)
	}
}
func insertRevisionWithExec(ctx context.Context, exec orm.Executor, app Application, job appruntime.Spec) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = orm.RawExec(ctx, exec, `INSERT OR IGNORE INTO application_revisions(id,application_id,generation,spec_hash,rendered_runtime_spec,managed_file_manifest,image_reference,resolved_image_digest,spec_yaml,job_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id.New("arev"), app.ID, app.Generation, app.SpecHash, string(raw), "[]", app.ImageReference, app.ImageDigest, app.SpecYAML, string(raw), formatTime(time.Now().UTC()))
	return err
}

func replaceApplicationFiles(ctx context.Context, exec orm.Executor, appID string, files []ApplicationFile) error {
	if err := orm.New(exec).From("application_files").Where("application_id=?", appID).Delete(ctx); err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	items := make([]*models.ApplicationFile, 0, len(files))
	for _, file := range files {
		item := fromDomainApplicationFile(file)
		item.ApplicationID = appID
		// 会话文件表的 file_key 对新建文件只是应用内 name（跨应用不唯一），
		// 落库前必须为这些新文件分配全局唯一主键；已有全局 ID 的文件（含 legacy 暂存文件）保留原 ID。
		if strings.TrimSpace(item.ID) == "" || item.ID == file.Name {
			item.ID = id.New("afile")
		}
		items = append(items, item)
	}
	return orm.New(exec).From("application_files").InsertBatch(ctx, items)
}

func applicationSaveError(err error) error {
	if isApplicationNameConflict(err) {
		return panelerr.Validation("application_name_duplicate", "application name must be unique")
	}
	return err
}

func runtimeOperationError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *panelerr.Error
	if errors.As(err, &appErr) {
		return err
	}
	return panelerr.BadGateway("application_runtime_operation_failed", "Application runtime operation failed: "+err.Error())
}

func applicationSpecIssueError(issue appspec.Issue) error {
	issues := validationIssuesFromSpecIssues([]appspec.Issue{issue})
	switch issue.Field {
	case "command":
		return panelerr.WithDetails(panelerr.Validation("application_command_invalid", issue.Message), map[string]any{"issues": issues})
	case "specYaml":
		return panelerr.WithDetails(panelerr.Validation("application_spec_yaml_invalid", issue.Message), map[string]any{"issues": issues})
	default:
		return applicationValidationError(issues)
	}
}

func isApplicationNameConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed: applications.name") || strings.Contains(msg, "constraint failed: applications.name")
}

func (s *Service) recordRunningTaskObjectWithParams(ctx context.Context, taskType, appID, summary, paramsJSON string) (tasks.Task, bool, error) {
	if s.tasks == nil {
		return tasks.Task{}, false, nil
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "application",
		ResourceID:   appID,
		Status:       tasks.StatusRunning,
		ParamsJSON:   paramsJSON,
		Summary:      summary,
	}, tasks.Trigger{Type: "system"})
	if err != nil {
		return tasks.Task{}, false, err
	}
	return task, created, nil
}

func (s *Service) PlanApplicationDeployment(ctx context.Context, req DeploymentPlanRequest) (DeploymentPlanResult, error) {
	if s == nil || s.orchestrator == nil {
		return DeploymentPlanResult{}, controlplane.ErrStoreUnavailable
	}
	result, err := s.planApplicationDeploymentV3(ctx, req)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	s.enqueueDeploymentPlanResult(result)
	return result, nil
}

// planApplicationDeploymentV3 is the durable control-plane entry point. It
// writes desired Instance state and the single active Job for each
// application/server conflict domain; it never creates a task anchor or
// performs a remote mutation.
func (s *Service) planApplicationDeploymentV3(ctx context.Context, req DeploymentPlanRequest) (DeploymentPlanResult, error) {
	if s == nil || s.orchestrator == nil {
		return DeploymentPlanResult{}, controlplane.ErrStoreUnavailable
	}
	app, err := s.Get(ctx, req.ApplicationID)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	autoDrift := req.ObservedRuntimeDrift && !req.Manual && !req.Force
	if autoDrift && app.ReconcileStopped {
		return DeploymentPlanResult{}, nil
	}
	if app.ReconcileStopped {
		if err := s.resetApplicationReconcileStopped(ctx, app.ID); err != nil {
			return DeploymentPlanResult{}, err
		}
	}
	triggerType := firstNonEmpty(req.TriggerType, "system")
	targetIDs := uniqueStringItems(req.ServerIDs)
	stopRequestIDs := uniqueStringItems(append(append([]string{}, req.StopServers...), req.ServerIDs...))

	if app.DeletionRequested || !app.Enabled {
		stopTargets, err := s.reconcileStopTargets(ctx, app, stopRequestIDs)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		action, desired := controlplane.ActionStop, controlplane.DesiredStopped
		if app.DeletionRequested || req.Purge {
			action, desired = controlplane.ActionPurge, controlplane.DesiredPurged
		}
		return s.planOrchestratorTargets(ctx, app, stopTargets, action, desired, controlplane.Revision{}, appruntime.Spec{}, nil, req, triggerType)
	}
	if app.Kind == ApplicationKindFacility && app.DeploymentMode == DeploymentModeSelected && len(app.DeploymentServers) == 0 {
		stopTargets, err := s.reconcileStopTargets(ctx, app, stopRequestIDs)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		action, desired := controlplane.ActionStop, controlplane.DesiredStopped
		if req.Purge {
			action, desired = controlplane.ActionPurge, controlplane.DesiredPurged
		}
		return s.planOrchestratorTargets(ctx, app, stopTargets, action, desired, controlplane.Revision{}, appruntime.Spec{}, nil, req, triggerType)
	}

	result := DeploymentPlanResult{}
	if len(req.StopServers) == 0 || len(targetIDs) > 0 {
		app, baseSpec, err := s.prepareDeploy(ctx, app.ID)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		files, err := s.listFiles(ctx, app.ID, true)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		targets, err := s.deploymentTargets(ctx, app)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		targets = filterDeploymentTargets(targets, targetIDs)
		if !req.Force && !req.ObservedRuntimeDrift {
			targets, err = s.filterUnsatisfiedDeploymentTargets(ctx, app, baseSpec, targets)
			if err != nil {
				return DeploymentPlanResult{}, err
			}
		}
		result, err = s.planOrchestratorTargets(ctx, app, serverIDsFromTargets(targets), controlplane.ActionApply, controlplane.DesiredRunning, controlplane.Revision{}, baseSpec, files, req, triggerType)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
	}

	removedTargets, err := s.reconcileRemovedTargets(ctx, app)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	removedResult, err := s.planOrchestratorTargets(ctx, app, removedTargets, controlplane.ActionPurge, controlplane.DesiredPurged, controlplane.Revision{}, appruntime.Spec{}, nil, req, triggerType)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	result = mergeDeploymentPlanResults(result, removedResult)
	if len(req.StopServers) > 0 {
		action, desired := controlplane.ActionStop, controlplane.DesiredStopped
		if req.Purge {
			action, desired = controlplane.ActionPurge, controlplane.DesiredPurged
		}
		stopResult, err := s.planOrchestratorTargets(ctx, app, uniqueStringItems(req.StopServers), action, desired, controlplane.Revision{}, appruntime.Spec{}, nil, req, triggerType)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		result = mergeDeploymentPlanResults(result, stopResult)
	}
	return result, nil
}

func (s *Service) planOrchestratorTargets(ctx context.Context, app Application, serverIDs []string, action, desired string, revision controlplane.Revision, baseSpec appruntime.Spec, files []ApplicationFile, req DeploymentPlanRequest, triggerType string) (DeploymentPlanResult, error) {
	result := DeploymentPlanResult{}
	serverIDs = uniqueStringItems(serverIDs)
	if len(serverIDs) == 0 {
		return result, nil
	}
	intentID := id.New("intent")
	forceNonce := int64(0)
	if req.Force {
		forceNonce = time.Now().UnixNano()
	}
	inputs := make([]controlplane.PlanInput, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		instanceID := runtimeInstanceID(app.ID, serverID)
		containerName := runtimeContainerName(app)
		desiredSpec := []byte(`{}`)
		if action == controlplane.ActionApply {
			if s.servers == nil {
				return result, panelerr.Validation("server_provider_unavailable", "Server provider is unavailable")
			}
			srv, err := s.servers.Get(ctx, serverID)
			if err != nil {
				return result, err
			}
			targetSpec, err := s.runtimeSpecForServer(ctx, app, baseSpec, srv, files)
			if err != nil {
				return result, err
			}
			containerName = targetSpec.ContainerName
			desiredSpec, err = json.Marshal(targetSpec)
			if err != nil {
				return result, err
			}
		}
		inputs = append(inputs, controlplane.PlanInput{
			ApplicationID:       app.ID,
			ServerID:            serverID,
			InstanceID:          instanceID,
			Action:              action,
			DesiredState:        desired,
			DesiredGeneration:   app.Generation,
			DesiredSpecHash:     app.SpecHash,
			DesiredRevisionID:   revision.ID,
			DesiredSpecJSON:     desiredSpec,
			ContainerName:       containerName,
			RemoveData:          app.DeletionRequested && action == controlplane.ActionPurge,
			ForceNonce:          forceNonce,
			Priority:            orchestratorPriority(action),
			IntentID:            intentID,
			TriggerType:         triggerType,
			TriggerResourceType: req.TriggerResourceType,
			TriggerResourceID:   req.TriggerResourceID,
			Reason:              req.Reason,
		})
	}
	var planned []controlplane.PlanResult
	var err error
	if action == controlplane.ActionApply && revision.ID == "" {
		raw, marshalErr := json.Marshal(baseSpec)
		if marshalErr != nil {
			return result, marshalErr
		}
		_, planned, err = s.orchestrator.Planner().EnsureRevisionAndPlanBatch(ctx, controlplane.RevisionInput{
			ApplicationID:       app.ID,
			Generation:          app.Generation,
			SpecHash:            app.SpecHash,
			RenderedRuntimeSpec: raw,
			ManagedFileManifest: runtimeManagedFileManifest(baseSpec),
			ImageReference:      app.ImageReference,
			ResolvedImageDigest: app.ImageDigest,
			SpecYAML:            app.SpecYAML,
		}, inputs)
	} else {
		planned, err = s.orchestrator.Planner().PlanBatch(ctx, inputs)
	}
	if err != nil {
		return result, err
	}
	for _, item := range planned {
		result.JobIDs = append(result.JobIDs, item.Job.ID)
		if item.Created {
			result.CreatedJobIDs = append(result.CreatedJobIDs, item.Job.ID)
		}
	}
	s.orchestrator.Wake()
	return result, nil
}

func orchestratorPriority(action string) int {
	switch action {
	case controlplane.ActionPurge:
		return 30
	case controlplane.ActionStop:
		return 20
	default:
		return 10
	}
}

func runtimeManagedFileManifest(spec appruntime.Spec) []map[string]any {
	manifest := make([]map[string]any, 0, len(spec.Files))
	for _, file := range spec.Files {
		sum := sha256.Sum256(file.Content)
		item := map[string]any{
			"path":   file.Path,
			"mode":   file.Mode,
			"sha256": hex.EncodeToString(sum[:]),
		}
		if file.UID != nil {
			item["uid"] = *file.UID
		}
		if file.GID != nil {
			item["gid"] = *file.GID
		}
		manifest = append(manifest, item)
	}
	return manifest
}

func (s *Service) enqueueDeploymentPlanResult(result DeploymentPlanResult) {
	// The durable AppDB jobs were already written by the planner. The wake
	// signal is only a latency optimization; a lost wake is repaired by the
	// controller's DB due scan.
	if s != nil && s.orchestrator != nil {
		s.orchestrator.Wake()
	}
}

func mergeDeploymentPlanResults(items ...DeploymentPlanResult) DeploymentPlanResult {
	out := DeploymentPlanResult{}
	for _, item := range items {
		out.JobIDs = append(out.JobIDs, item.JobIDs...)
		out.CreatedJobIDs = append(out.CreatedJobIDs, item.CreatedJobIDs...)
	}
	return out
}

func (s *Service) reconcileStopTargets(ctx context.Context, app Application, targetIDs []string) ([]string, error) {
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	wanted := stringBoolSet(targetIDs)
	out := []string{}
	for _, instance := range instances {
		if len(wanted) > 0 && !wanted[instance.ServerID] {
			continue
		}
		out = append(out, instance.ServerID)
	}
	if app.Kind == ApplicationKindFacility {
		for _, serverID := range targetIDs {
			out = append(out, serverID)
		}
	}
	return uniqueStringItems(out), nil
}

func (s *Service) reconcileRemovedTargets(ctx context.Context, app Application) ([]string, error) {
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return nil, err
	}
	desired := stringBoolSet(app.DeploymentServers)
	if app.DeploymentMode == DeploymentModeAll || strings.TrimSpace(app.DeploymentMode) == "" {
		targets, err := s.deploymentTargets(ctx, app)
		if err != nil {
			return nil, err
		}
		desired = map[string]bool{}
		for _, target := range targets {
			desired[target.ID] = true
		}
	}
	out := []string{}
	for _, instance := range instances {
		if !desired[instance.ServerID] {
			out = append(out, instance.ServerID)
		}
	}
	return uniqueStringItems(out), nil
}

func (s *Service) filterUnsatisfiedDeploymentTargets(ctx context.Context, app Application, spec appruntime.Spec, targets []server.Server) ([]server.Server, error) {
	out := make([]server.Server, 0, len(targets))
	for _, target := range targets {
		desired, err := s.runtimeSpecForServer(ctx, app, spec, target, nil)
		if err != nil {
			return nil, err
		}
		instance, err := s.runtimeInstanceForServer(ctx, app.ID, target.ID)
		if err != nil {
			if isNotFound(err) {
				out = append(out, target)
				continue
			}
			return nil, err
		}
		if !runtimeInstanceSatisfiesDesired(instance, desired) {
			out = append(out, target)
		}
	}
	return out, nil
}

func runtimeInstanceSatisfiesDesired(instance appruntime.Instance, desired appruntime.Spec) bool {
	if instance.DesiredState != appruntime.DesiredRunning || instance.Status != appruntime.StatusRunning {
		return false
	}
	if instance.LastDeployedGeneration != desired.Generation {
		return false
	}
	if strings.TrimSpace(instance.RuntimeSpec.SpecHash) != "" {
		return instance.RuntimeSpec.SpecHash == desired.SpecHash
	}
	return strings.TrimSpace(desired.SpecHash) == ""
}

func (s *Service) triggerApplicationReconcile(ctx context.Context, app Application, spec appruntime.Spec, triggerType, fallbackSummary string) error {
	return s.triggerApplicationReconcileWithPayload(ctx, app.ID, triggerType, map[string]any{
		"applicationIds": []string{app.ID},
		"force":          true,
		"reason":         firstNonEmpty(triggerType, "application_change"),
	})
}

func (s *Service) triggerApplicationReconcileWithPayload(ctx context.Context, appID, triggerType string, payload map[string]any) error {
	_, err := s.triggerApplicationReconcileTask(ctx, appID, triggerType, payload)
	return err
}

func (s *Service) triggerApplicationReconcileTask(ctx context.Context, appID, triggerType string, payload map[string]any) (tasks.Task, error) {
	if s.reconcileTrigger == nil {
		return tasks.Task{}, panelerr.Validation("application_reconciler_unavailable", "Application reconciler is unavailable")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["applicationIds"]; !ok {
		payload["applicationIds"] = []string{appID}
	}
	task, _, err := s.reconcileTrigger.TriggerApplicationReconcile(ctx, tasks.PeriodicTrigger{
		Type:                firstNonEmpty(triggerType, "application_change"),
		TriggerResourceType: "application",
		TriggerResourceID:   appID,
		Payload:             payload,
	})
	return task, err
}

func filterDeploymentTargets(targets []server.Server, targetIDs []string) []server.Server {
	if len(targetIDs) == 0 {
		return targets
	}
	wanted := stringBoolSet(targetIDs)
	if len(wanted) == 0 {
		return targets
	}
	out := make([]server.Server, 0, len(targets))
	for _, target := range targets {
		if wanted[target.ID] {
			out = append(out, target)
		}
	}
	return out
}

func serverIDsFromTargets(targets []server.Server) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, target.ID)
	}
	return uniqueStringItems(out)
}

func stringBoolSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func uniqueStringItems(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

type stopTaskRunOptions struct {
	purge     bool
	targetIDs []string
}

func stopTaskOptions(task tasks.Task) (stopTaskRunOptions, error) {
	out := stopTaskRunOptions{}
	if strings.TrimSpace(task.ParamsJSON) != "" && strings.TrimSpace(task.ParamsJSON) != "{}" {
		var params struct {
			Purge    bool   `json:"purge"`
			ServerID string `json:"serverId"`
		}
		if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err != nil {
			return stopTaskRunOptions{}, err
		}
		out.purge = params.Purge
		if strings.TrimSpace(params.ServerID) != "" {
			out.targetIDs = []string{strings.TrimSpace(params.ServerID)}
		}
	}
	if len(out.targetIDs) == 0 && strings.TrimSpace(task.ServerID) != "" && strings.TrimSpace(task.ResourceID) != "" {
		out.targetIDs = []string{strings.TrimSpace(task.ServerID)}
	}
	return out, nil
}

func applyDeploymentTargets(job appruntime.Spec, app Application) (appruntime.Spec, error) {
	_, _, err := normalizeDeploymentTargets(app.DeploymentMode, app.DeploymentServers, app.PersistentPath)
	if err != nil {
		return appruntime.Spec{}, err
	}
	return job, nil
}

func (s *Service) deploymentTargets(ctx context.Context, app Application) ([]server.Server, error) {
	if s.servers == nil {
		return nil, panelerr.Validation("server_provider_unavailable", "Server provider is unavailable")
	}
	mode, selected, err := normalizeDeploymentTargets(app.DeploymentMode, app.DeploymentServers, app.PersistentPath)
	if err != nil {
		return nil, err
	}
	if mode == DeploymentModeSelected {
		out := make([]server.Server, 0, len(selected))
		for _, serverID := range selected {
			srv, err := s.servers.Get(ctx, serverID)
			if err != nil {
				return nil, err
			}
			out = append(out, srv)
		}
		return out, nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]server.Server, 0, len(servers))
	for _, srv := range servers {
		if ensureAgentRuntimeReady(srv) == nil {
			out = append(out, srv)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func ensureAgentRuntimeReady(srv server.Server) error {
	baseURL, ok := agentURLFromServer(srv)
	if !ok || strings.TrimSpace(baseURL) == "" {
		return panelerr.Validation("agent_required", "Agent is required for application runtime")
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return panelerr.Validation("agent_incompatible", "Agent is not compatible with application runtime")
	}
	return nil
}

func (s *Service) runtimeSpecForServer(ctx context.Context, app Application, spec appruntime.Spec, srv server.Server, files []ApplicationFile) (appruntime.Spec, error) {
	if s.facilityRuntime != nil {
		if facilitySpec, ok, err := s.facilityRuntime.RuntimeSpecForServer(ctx, app, srv); err != nil || ok {
			return facilitySpec, err
		}
	}
	out := spec
	out.ApplicationID = app.ID
	out.InstanceID = runtimeInstanceID(app.ID, srv.ID)
	out.ContainerName = runtimeContainerName(app)
	if s.storageResolver != nil {
		mountsBefore := out.Mounts
		resolved, err := s.storageResolver.ResolveStorageShareMounts(ctx, app, srv, out.Mounts)
		if err != nil {
			return appruntime.Spec{}, err
		}
		out.Mounts = resolved
		// 设施配置（存储服务器/根目录）变化会改变解析后的挂载，进而改变
		// 实例期望 spec hash，触发巡检重建，避免容器继续挂旧路径。
		if !reflect.DeepEqual(mountsBefore, resolved) {
			out.SpecHash = runtimeSpecHashWithMounts(app.SpecHash, resolved)
		}
	}
	if out.Env == nil {
		out.Env = map[string]string{}
	} else {
		out.Env = cloneStringMap(out.Env)
	}
	for key, value := range map[string]string{
		"PANEL_SERVER_ID":           srv.ID,
		"PANEL_SERVER_NAME":         srv.Name,
		"PANEL_SERVER_SSH_HOST":     srv.Host,
		"PANEL_SERVER_SSH_PORT":     strconv.Itoa(srv.Port),
		"PANEL_SERVER_SSH_USERNAME": srv.SSHUsername,
	} {
		if _, exists := out.Env[key]; !exists {
			out.Env[key] = value
		}
	}
	renderedFiles, err := s.renderManagedFilesForServer(ctx, app, srv, out.Files, files)
	if err != nil {
		return appruntime.Spec{}, err
	}
	out.Files = renderedFiles
	return out, nil
}

func runtimeSpecHashWithMounts(base string, mounts []appruntime.Mount) string {
	raw, _ := json.Marshal(mounts)
	sum := sha256.Sum256([]byte(base + "\n" + string(raw)))
	return hex.EncodeToString(sum[:])
}

func (s *Service) renderManagedFilesForServer(ctx context.Context, app Application, srv server.Server, managed []appruntime.ManagedFile, files []ApplicationFile) ([]appruntime.ManagedFile, error) {
	if len(managed) == 0 || len(files) == 0 {
		return managed, nil
	}
	filesByAllocation := map[string]ApplicationFile{}
	for _, file := range files {
		filesByAllocation[applicationFileAllocationName(file.ID)] = file
	}
	data, err := s.templateData(ctx, app, files, &srv)
	if err != nil {
		return nil, err
	}
	out := append([]appruntime.ManagedFile(nil), managed...)
	for i, item := range out {
		file, ok := filesByAllocation[item.Path]
		if !ok || file.Kind != ApplicationFileKindTemplate {
			continue
		}
		text := string(file.Content)
		if s.renderer != nil {
			text, err = s.renderer.Render(ctx, text, data)
			if err != nil {
				return nil, err
			}
		}
		out[i].Content = []byte(text)
	}
	return out, nil
}

func runtimeInstanceID(appID, serverID string) string {
	return strings.TrimSpace(appID) + "-" + strings.TrimSpace(serverID)
}

func runtimeContainerName(app Application) string {
	name := strings.TrimSpace(app.Name)
	if name == "" {
		name = app.ID
	}
	return "panel-" + sanitizeRuntimeName(name)
}

func sanitizeRuntimeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "runtime"
	}
	return out
}

func (s *Service) upsertRuntimeInstance(ctx context.Context, appID, serverID string, spec appruntime.Spec, desired, status, containerID, lastErr string) error {
	return s.upsertRuntimeInstanceWithContainerNamePolicy(ctx, appID, serverID, spec, desired, status, containerID, lastErr, false)
}

func (s *Service) upsertRuntimeInstancePreservingContainerName(ctx context.Context, appID, serverID string, spec appruntime.Spec, desired, status, containerID, lastErr string) error {
	return s.upsertRuntimeInstanceWithContainerNamePolicy(ctx, appID, serverID, spec, desired, status, containerID, lastErr, true)
}

func (s *Service) upsertRuntimeInstanceWithContainerNamePolicy(ctx context.Context, appID, serverID string, spec appruntime.Spec, desired, status, containerID, lastErr string, preserveContainerName bool) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	containerNameExpr := "excluded.container_name"
	if preserveContainerName {
		containerNameExpr = "COALESCE(NULLIF(application_instances.container_name, ''), excluded.container_name)"
	}
	_, err = orm.RawExec(ctx, s.db, fmt.Sprintf(`INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(application_id,server_id) DO UPDATE SET
			id=excluded.id,
			container_name=%s,
			container_id=excluded.container_id,
			desired_state=excluded.desired_state,
			status=excluded.status,
			runtime_spec_json=excluded.runtime_spec_json,
			last_deployed_generation=excluded.last_deployed_generation,
			last_error=excluded.last_error,
			updated_at=excluded.updated_at`, containerNameExpr),
		spec.InstanceID, appID, serverID, spec.ContainerName, containerID, desired, status, string(raw), spec.Generation, lastErr, now, now)
	return err
}

func (s *Service) markRuntimeInstance(ctx context.Context, instanceID, desired, status, containerID, lastErr string) error {
	now := formatTime(time.Now().UTC())
	_, err := orm.RawExec(ctx, s.db, `UPDATE application_instances SET desired_state=?,status=?,container_id=COALESCE(NULLIF(?, ''), container_id),last_error=?,updated_at=? WHERE id=?`,
		desired, status, containerID, lastErr, now, instanceID)
	return err
}

func (s *Service) deleteRuntimeInstanceForServer(ctx context.Context, appID, serverID string) error {
	instance, err := s.runtimeInstanceForServer(ctx, appID, serverID)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if err := orm.New(s.db).From("application_instances").Where("application_id=?", appID).And("server_id=?", serverID).Delete(ctx); err != nil {
		return err
	}
	return orm.New(s.db).From("application_reconcile_states").Where("instance_id=?", instance.ID).Delete(ctx)
}

func (s *Service) deleteApplicationIfRuntimeGone(ctx context.Context, appID string) error {
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		return nil
	}
	// 先清理本应用的终态 Job：jobs 对 applications 使用 RESTRICT 外键（设计上
	// 不允许级联删除 Job），不清理会导致删除 finalizer 无法物理删除应用。
	if _, err := orm.RawExec(ctx, s.db, `DELETE FROM jobs WHERE application_id=? AND state IN ('succeeded','failed','cancelled')`, appID); err != nil {
		return err
	}
	if err := orm.New(s.db).From("applications").Where("id=?", appID).And("deletion_requested=1").Delete(ctx); err != nil {
		return err
	}
	return orm.New(s.db).From("reverse_proxy_routes").Where("app_id=?", appID).Delete(ctx)
}

func (s *Service) recordApplicationReconcileFailure(ctx context.Context, appID string) error {
	if err := s.ensureApplicationReconcileStateRows(ctx, appID); err != nil {
		return err
	}
	var failures sql.NullInt64
	if err := orm.RawRow(ctx, s.db, `SELECT MAX(reconcile_failures) FROM application_reconcile_states WHERE application_id=?`, appID).Scan(&failures); err != nil {
		return err
	}
	nextFailures := 1
	if failures.Valid && failures.Int64 > 0 {
		nextFailures = int(failures.Int64) + 1
	}
	nextRun := time.Now().UTC().Add(applicationReconcileFailureBackoff(nextFailures))
	if nextFailures >= ReconcileStopAfterFailures {
		if err := s.markApplicationReconcileStopped(ctx, appID); err != nil {
			return err
		}
	}
	return orm.New(s.db).From("application_reconcile_states").Where("application_id=?", appID).UpdateColumns(ctx, map[string]any{
		"reconcile_failures":       nextFailures,
		"reconcile_next_run_at":    nextRun.Format(time.RFC3339Nano),
		"reconcile_success_streak": 0,
	})
}

func (s *Service) markApplicationReconcileStopped(ctx context.Context, appID string) error {
	// 协调停止是派生状态，不写 applications.updated_at，避免协调动作污染配置更新时间。
	return orm.New(s.db).From("applications").Where("id=?", appID).UpdateColumns(ctx, map[string]any{
		"reconcile_stopped": 1,
	})
}

func (s *Service) resetApplicationReconcileStopped(ctx context.Context, appID string) error {
	if err := s.resetApplicationReconcileFailures(ctx, appID); err != nil {
		return err
	}
	return orm.New(s.db).From("applications").Where("id=?", appID).UpdateColumns(ctx, map[string]any{
		"reconcile_stopped": 0,
	})
}

func (s *Service) resetApplicationReconcileFailures(ctx context.Context, appID string) error {
	return orm.New(s.db).From("application_reconcile_states").Where("application_id=?", appID).UpdateColumns(ctx, map[string]any{
		"reconcile_failures":       0,
		"reconcile_next_run_at":    "",
		"reconcile_success_streak": 0,
	})
}

func (s *Service) ensureApplicationReconcileStateRows(ctx context.Context, appID string) error {
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, instance := range instances {
		if _, err := orm.RawExec(ctx, s.db, `INSERT INTO application_reconcile_states(instance_id,application_id,server_id,observed_at)
			VALUES(?,?,?,?) ON CONFLICT(instance_id) DO UPDATE SET application_id=excluded.application_id,server_id=excluded.server_id,observed_at=excluded.observed_at`,
			instance.ID, instance.ApplicationID, instance.ServerID, now); err != nil {
			return err
		}
	}
	return nil
}

func isRuntimeAlreadyRequestedState(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "already has requested state")
}

func applicationReconcileFailureBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	delay := 30 * time.Second
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= 10*time.Minute {
			return 10 * time.Minute
		}
	}
	return delay
}

func (s *Service) runtimeInstances(ctx context.Context, appID string) ([]appruntime.Instance, error) {
	var rows []models.ApplicationInstance
	if err := orm.New(s.db).From("application_instances").Where("application_id=?", appID).OrderBy("server_id ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]appruntime.Instance, 0, len(rows))
	for _, m := range rows {
		out = append(out, toRuntimeInstance(m))
	}
	return out, nil
}

func (s *Service) runtimeInstance(ctx context.Context, appID, instanceID string) (appruntime.Instance, error) {
	if strings.TrimSpace(instanceID) == "" {
		return appruntime.Instance{}, panelerr.Validation("runtime_instance_required", "Runtime instance is required")
	}
	var m models.ApplicationInstance
	if err := orm.New(s.db).From("application_instances").Where("application_id=?", appID).And("id=?", instanceID).First(ctx, &m); err != nil {
		if err == sql.ErrNoRows {
			return appruntime.Instance{}, panelerr.NotFound("application_instance")
		}
		return appruntime.Instance{}, err
	}
	return toRuntimeInstance(m), nil
}

func (s *Service) primaryRuntimeInstance(ctx context.Context, appID string) (appruntime.Instance, error) {
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return appruntime.Instance{}, err
	}
	if len(instances) == 0 {
		return appruntime.Instance{}, panelerr.NotFound("application_instance")
	}
	return instances[0], nil
}

func persistentRestoreTargetServer(app Application) (string, error) {
	mode, servers, err := normalizeDeploymentTargets(app.DeploymentMode, app.DeploymentServers, app.PersistentPath)
	if err != nil {
		return "", err
	}
	if mode != DeploymentModeSelected || len(servers) != 1 {
		return "", panelerr.Validation("application_persistent_single_target_required", "persistent applications must target exactly one server")
	}
	return servers[0], nil
}

func isPanelNotFound(err error) bool {
	var panelError *panelerr.Error
	return errors.As(err, &panelError) && panelError.Code == "not_found"
}

func (s *Service) runtimeInstanceForServer(ctx context.Context, appID, serverID string) (appruntime.Instance, error) {
	var m models.ApplicationInstance
	if err := orm.New(s.db).From("application_instances").Where("application_id=?", appID).And("server_id=?", serverID).First(ctx, &m); err != nil {
		if err == sql.ErrNoRows {
			return appruntime.Instance{}, panelerr.NotFound("application_instance")
		}
		return appruntime.Instance{}, err
	}
	return toRuntimeInstance(m), nil
}

type cachedImageUpdate struct {
	LocalDigest     string
	LatestDigest    string
	UpdateAvailable bool
	CheckedAt       *time.Time
	LastError       string
}

func (s *Service) cachedImageUpdate(ctx context.Context, serverID string, references []string) (cachedImageUpdate, bool, error) {
	for _, reference := range references {
		var m models.ImageUpdate
		if err := orm.New(s.db).From("image_updates").Where("server_id=?", serverID).And("reference=?", reference).First(ctx, &m); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return cachedImageUpdate{}, false, err
		}
		item := cachedImageUpdate{
			LocalDigest:     m.LocalDigest,
			LatestDigest:    m.LatestDigest,
			UpdateAvailable: m.UpdateAvailable,
			LastError:       m.LastError,
		}
		checked := m.CheckedAt
		item.CheckedAt = &checked
		return item, true, nil
	}
	return cachedImageUpdate{}, false, nil
}

func (s *Service) refreshInstanceStatuses(ctx context.Context, instances []appruntime.Instance) []appruntime.InstanceStatus {
	out := s.cachedInstanceStatuses(ctx, instances)
	for i, instance := range instances {
		status := out[i]
		if s.runtimeClient != nil && s.servers != nil {
			if srv, err := s.servers.Get(ctx, instance.ServerID); err == nil {
				status.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
				if ensureAgentRuntimeReady(srv) == nil {
					baseURL, _ := agentURLFromServer(srv)
					if resp, err := s.runtimeClient.RuntimeStatus(ctx, baseURL, instance.ID, instance.ContainerName); err == nil {
						status = resp.InstanceStatus
						status.ServerID = instance.ServerID
						status.InstanceID = instance.ID
						if strings.TrimSpace(status.ServerName) == "" {
							status.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
						}
					}
				}
			}
		}
		out[i] = status
	}
	return out
}
func (s *Service) cachedInstanceStatuses(ctx context.Context, instances []appruntime.Instance) []appruntime.InstanceStatus {
	out := make([]appruntime.InstanceStatus, 0, len(instances))
	for _, instance := range instances {
		status := appruntime.InstanceStatus{
			InstanceID:    instance.ID,
			ServerID:      instance.ServerID,
			ContainerName: instance.ContainerName,
			ContainerID:   instance.ContainerID,
			Status:        instance.Status,
			DesiredState:  instance.DesiredState,
			LastError:     instance.LastError,
			ObservedAt:    time.Now().UTC(),
		}
		if s.servers != nil {
			if srv, err := s.servers.Get(ctx, instance.ServerID); err == nil {
				status.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
			}
		}
		out = append(out, status)
	}
	return out
}

func (s *Service) handleAgentError(ctx context.Context, srv server.Server, err error) bool {
	if s.agentErrors == nil {
		return false
	}
	return s.agentErrors.HandleAgentError(ctx, srv, err)
}

func aggregateRuntimeStatus(enabled bool, instances []appruntime.InstanceStatus) string {
	if !enabled {
		return appruntime.StatusStopped
	}
	if len(instances) == 0 {
		return appruntime.StatusPending
	}
	allRunning := true
	anyMissing := false
	for _, instance := range instances {
		switch instance.Status {
		case appruntime.StatusFailed:
			return appruntime.StatusFailed
		case appruntime.StatusMissing:
			anyMissing = true
			allRunning = false
		case appruntime.StatusRunning:
		default:
			allRunning = false
		}
	}
	if allRunning {
		return appruntime.StatusRunning
	}
	if anyMissing {
		return appruntime.StatusMissing
	}
	return appruntime.StatusPending
}

func agentURLFromServer(srv server.Server) (string, bool) {
	if srv.Traits == nil || strings.TrimSpace(srv.Traits[agentcontract.TraitEnabled]) != "true" {
		return "", false
	}
	u := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	return u, u != ""
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func serverNameForImageTarget(ctx context.Context, provider ServerProvider, serverID string) string {
	if provider == nil {
		return ""
	}
	srv, err := provider.Get(ctx, serverID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
}

func imageReferenceCandidates(values ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, candidate := range []string{value, imageReferenceWithLatest(value)} {
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func imageReferenceWithLatest(reference string) string {
	if reference == "" || strings.Contains(reference, "@") {
		return ""
	}
	lastSlash := strings.LastIndex(reference, "/")
	lastColon := strings.LastIndex(reference, ":")
	if lastColon > lastSlash {
		return ""
	}
	return reference + ":latest"
}

func normalizeDeploymentTargets(mode string, servers []string, persistentPath string) (string, []string, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = DeploymentModeAll
	}
	if mode != DeploymentModeAll && mode != DeploymentModeSelected {
		return "", nil, panelerr.Validation("application_deployment_mode_invalid", "deployment mode must be all or selected")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(servers))
	for _, serverID := range servers {
		serverID = strings.TrimSpace(serverID)
		if serverID == "" {
			continue
		}
		if _, ok := seen[serverID]; ok {
			continue
		}
		seen[serverID] = struct{}{}
		out = append(out, serverID)
	}
	sort.Strings(out)
	if mode == DeploymentModeAll {
		out = nil
	}
	if mode == DeploymentModeSelected && len(out) == 0 {
		return "", nil, panelerr.Validation("application_deployment_servers_required", "select at least one deployment server")
	}
	if strings.TrimSpace(persistentPath) != "" && (mode != DeploymentModeSelected || len(out) != 1) {
		return "", nil, panelerr.Validation("application_persistent_single_target_required", "persistent applications must target exactly one server")
	}
	return mode, out, nil
}

func normalizeReverseProxyRules(rules []ReverseProxyRule) ([]ReverseProxyRule, error) {
	out := make([]ReverseProxyRule, 0, len(rules))
	for _, rule := range rules {
		domain := strings.ToLower(strings.TrimSpace(rule.Domain))
		if domain == "" {
			continue
		}
		if !validReverseProxyDomain(domain) {
			return nil, panelerr.Validation("application_reverse_proxy_domain_invalid", "reverse proxy domain is invalid")
		}
		targetType := normalizeReverseProxyTargetType(rule.TargetType)
		if targetType == "" {
			return nil, panelerr.Validation("application_reverse_proxy_target_type_invalid", "reverse proxy target type is invalid")
		}
		if rule.TargetPort <= 0 || rule.TargetPort > 65535 {
			return nil, panelerr.Validation("application_reverse_proxy_target_port_invalid", "reverse proxy target port must be between 1 and 65535")
		}
		// Origin membership and the primary origin are resolved by the facility
		// reverse-proxy policy from the global gateway nodes and the deployment
		// targets; any client-provided values are ignored.
		anyAccess := rule.AnyAccess
		anyAccess.PrimaryOriginServerID = ""
		paths := make([]ReverseProxyPath, 0, len(rule.Paths))
		pathKeys := map[string]struct{}{}
		for _, item := range rule.Paths {
			proxyPath := strings.TrimSpace(item.Path)
			if proxyPath == "" {
				proxyPath = "/"
			}
			if !validNginxPath(proxyPath) {
				return nil, panelerr.Validation("application_reverse_proxy_path_invalid", "reverse proxy path is invalid")
			}
			if _, ok := pathKeys[proxyPath]; ok {
				return nil, panelerr.Validation("application_reverse_proxy_path_duplicate", "reverse proxy path is duplicated")
			}
			pathKeys[proxyPath] = struct{}{}
			defaultWebSocketMode := HTTPRouteModeOff
			if item.WebSocket {
				defaultWebSocketMode = HTTPRouteModeOn
			}
			options, err := NormalizeHTTPRouteOptions(item.Options, true, true, defaultWebSocketMode)
			if err != nil {
				return nil, err
			}
			paths = append(paths, ReverseProxyPath{Path: proxyPath, WebSocket: options.WebSocketMode != HTTPRouteModeOff, Options: options})
		}
		if len(paths) == 0 {
			options, _ := NormalizeHTTPRouteOptions(HTTPRouteOptions{}, true, true, HTTPRouteModeOff)
			paths = append(paths, ReverseProxyPath{Path: "/", Options: options})
		}
		out = append(out, ReverseProxyRule{
			Domain:          domain,
			TargetType:      targetType,
			TargetPort:      rule.TargetPort,
			OriginServerIDs: nil,
			AnyAccess:       anyAccess,
			Paths:           paths,
		})
	}
	return out, nil
}

func normalizeReverseProxyTargetType(value string) string {
	switch strings.TrimSpace(value) {
	case "", ReverseProxyTargetLocal:
		return ReverseProxyTargetLocal
	case ReverseProxyTargetContainer:
		return ReverseProxyTargetContainer
	default:
		return ""
	}
}

// enforceHostModeProxyTarget 强制 host 模式应用的反代目标为 local：host 模式
// 容器不在受管容器网桥内，反代容器无法按名解析其上游，前端已禁选 container
// 目标，这里在后端保存路径上做同样的降级，防止绕过前端直接提交。
func enforceHostModeProxyTarget(networkMode string, rules []ReverseProxyRule) {
	if networkMode != "host" {
		return
	}
	for i := range rules {
		if normalizeReverseProxyTargetType(rules[i].TargetType) == ReverseProxyTargetContainer {
			rules[i].TargetType = ReverseProxyTargetLocal
		}
	}
}

func (s *Service) renderReverseProxyRules(ctx context.Context, rules []ReverseProxyRule, data map[string]any) ([]ReverseProxyRule, error) {
	out := make([]ReverseProxyRule, 0, len(rules))
	for _, rule := range rules {
		domain, err := s.renderTemplate(ctx, strings.TrimSpace(rule.Domain), data)
		if err != nil {
			return nil, err
		}
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if !validReverseProxyDomain(domain) {
			return nil, panelerr.Validation("application_reverse_proxy_domain_invalid", "reverse proxy domain is invalid")
		}
		originServerIDs := uniqueSortedStrings(rule.OriginServerIDs)
		anyAccess, err := NormalizeAnyAccessConfig(rule.AnyAccess, originServerIDs)
		if err != nil {
			return nil, err
		}
		paths := make([]ReverseProxyPath, 0, len(rule.Paths))
		pathKeys := map[string]struct{}{}
		for _, item := range rule.Paths {
			proxyPath, err := s.renderTemplate(ctx, strings.TrimSpace(item.Path), data)
			if err != nil {
				return nil, err
			}
			proxyPath = strings.TrimSpace(proxyPath)
			if proxyPath == "" {
				proxyPath = "/"
			}
			if !validNginxPath(proxyPath) {
				return nil, panelerr.Validation("application_reverse_proxy_path_invalid", "reverse proxy path is invalid")
			}
			if _, ok := pathKeys[proxyPath]; ok {
				return nil, panelerr.Validation("application_reverse_proxy_path_duplicate", "reverse proxy path is duplicated")
			}
			pathKeys[proxyPath] = struct{}{}
			defaultWebSocketMode := HTTPRouteModeOff
			if item.WebSocket {
				defaultWebSocketMode = HTTPRouteModeOn
			}
			options, err := NormalizeHTTPRouteOptions(item.Options, true, true, defaultWebSocketMode)
			if err != nil {
				return nil, err
			}
			paths = append(paths, ReverseProxyPath{Path: proxyPath, WebSocket: options.WebSocketMode != HTTPRouteModeOff, Options: options})
		}
		if len(paths) == 0 {
			options, _ := NormalizeHTTPRouteOptions(HTTPRouteOptions{}, true, true, HTTPRouteModeOff)
			paths = append(paths, ReverseProxyPath{Path: "/", Options: options})
		}
		out = append(out, ReverseProxyRule{Domain: domain, TargetType: normalizeReverseProxyTargetType(rule.TargetType), TargetPort: rule.TargetPort, OriginServerIDs: originServerIDs, AnyAccess: anyAccess, Paths: paths})
	}
	return out, nil
}

func (s *Service) renderReverseProxyConfig(ctx context.Context, app Application, files []ApplicationFile) (string, string, error) {
	if len(app.ReverseProxy) == 0 {
		return "", "", nil
	}
	data, err := s.templateData(ctx, app, files, nil)
	if err != nil {
		return "", "", err
	}
	rules, err := s.renderReverseProxyRules(ctx, app.ReverseProxy, data)
	if err != nil {
		return "", "", err
	}
	if len(rules) == 0 {
		return "", "", nil
	}
	var b strings.Builder
	b.WriteString("# Managed by Panel. Application: ")
	b.WriteString(app.Name)
	b.WriteString(" (")
	b.WriteString(app.ID)
	b.WriteString(")\n")
	for _, rule := range rules {
		b.WriteString("\nserver {\n")
		b.WriteString("    listen 443 ssl;\n")
		b.WriteString("    server_name ")
		b.WriteString(rule.Domain)
		b.WriteString(";\n\n")
		for _, proxyPath := range rule.Paths {
			b.WriteString("    location ")
			b.WriteString(proxyPath.Path)
			b.WriteString(" {\n")
			b.WriteString("        proxy_pass ")
			b.WriteString(reverseProxyUpstream(rule, runtimeContainerName(app), "127.0.0.1"))
			b.WriteString(";\n")
			b.WriteString("        proxy_set_header Host $host;\n")
			b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
			b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
			b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
			WriteNginxHTTPRouteOptions(&b, proxyPath.Options, "        ", true)
			WriteNginxWebSocketOptions(&b, proxyPath.Options.WebSocketMode, "        ")
			b.WriteString("    }\n")
		}
		b.WriteString("}\n")
	}
	return reverseProxyConfigName(app), b.String(), nil
}

func reverseProxyUpstream(rule ReverseProxyRule, containerName, localHost string) string {
	host := strings.TrimSpace(localHost)
	if host == "" {
		host = "127.0.0.1"
	}
	if normalizeReverseProxyTargetType(rule.TargetType) == ReverseProxyTargetContainer {
		container := strings.TrimSpace(containerName)
		if container != "" && validNginxToken(container) {
			host = container
		}
	}
	return "http://" + host + ":" + strconv.Itoa(rule.TargetPort)
}

func (s *Service) ApplicationReverseProxyConfigs(ctx context.Context) ([]ApplicationReverseProxyConfig, error) {
	apps, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []ApplicationReverseProxyConfig{}
	for _, app := range apps {
		if !app.Enabled || len(app.ReverseProxy) == 0 {
			continue
		}
		files, err := s.listFiles(ctx, app.ID, true)
		if err != nil {
			return nil, err
		}
		data, err := s.templateData(ctx, app, files, nil)
		if err != nil {
			return nil, err
		}
		rules, err := s.renderReverseProxyRules(ctx, app.ReverseProxy, data)
		if err != nil {
			return nil, err
		}
		routes := make([]ReverseProxyRoute, 0, len(rules))
		for _, rule := range rules {
			paths := make([]ReverseProxyPath, 0, len(rule.Paths))
			for _, item := range rule.Paths {
				paths = append(paths, ReverseProxyPath{Path: item.Path, WebSocket: item.WebSocket, Options: item.Options})
			}
			routes = append(routes, ReverseProxyRoute{Domain: rule.Domain, TargetType: normalizeReverseProxyTargetType(rule.TargetType), TargetPort: rule.TargetPort, TargetContainer: runtimeContainerName(app), OriginServerIDs: append([]string(nil), rule.OriginServerIDs...), AnyAccess: rule.AnyAccess, Paths: paths})
		}
		out = append(out, ApplicationReverseProxyConfig{
			ApplicationID:     app.ID,
			ApplicationName:   app.Name,
			DeploymentMode:    app.DeploymentMode,
			DeploymentServers: append([]string(nil), app.DeploymentServers...),
			Routes:            routes,
		})
	}
	return out, nil
}

func reverseProxyConfigName(app Application) string {
	name := strings.TrimSpace(app.Name)
	if name == "" {
		name = app.ID
	}
	name = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = app.ID
	}
	return "panel-" + name + ".conf"
}

func validNginxToken(value string) bool {
	if value == "" {
		return false
	}
	return !strings.ContainsAny(value, " \t\r\n;{}")
}

// validReverseProxyDomain 按 DNS hostname 规则校验反向代理域名，支持单个 *. 通配前缀。
func validReverseProxyDomain(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*.")
		if value == "" {
			return false
		}
	}
	if len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func validNginxPath(value string) bool {
	// 排除空白、nginx 配置分隔符以及 # ? \ 等 URI/配置特殊字符。
	return value != "" && strings.HasPrefix(value, "/") && !strings.ContainsAny(value, " \t\r\n;{}#?\\'\"")
}

func (s *Service) redeployIfEnabled(ctx context.Context, app Application) error {
	current, err := s.Get(ctx, app.ID)
	if err != nil {
		return err
	}
	if !current.Enabled {
		return nil
	}
	current, err = s.refreshApplicationSnapshot(ctx, current)
	if err != nil {
		return err
	}
	job, issues, err := s.renderApplication(ctx, current)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return applicationValidationError(issues)
	}
	current.UpdatedAt = time.Now().UTC()
	if err := s.updateApplicationDerived(ctx, current); err != nil {
		return err
	}
	if err := s.triggerApplicationReconcile(ctx, current, job, "application_change", "Syncing application "+current.Name); err != nil {
		return err
	}
	return s.reconcileReverseProxy(ctx)
}

func (s *Service) ReconcileReverseProxy(ctx context.Context) error {
	return s.reconcileReverseProxy(ctx)
}

func (s *Service) reconcileReverseProxy(ctx context.Context) error {
	if s.proxyReconciler == nil {
		return nil
	}
	return runtimeOperationError(s.proxyReconciler.ReconcileReverseProxy(ctx))
}

func (s *Service) refreshApplicationSnapshot(ctx context.Context, current Application) (Application, error) {
	// 设施应用（如入口代理 facility-reverse-proxy）的 generation/spec_hash
	// 由设施模块独占维护（facilityapps.ensureReverseProxyApplication，
	// hash 为 facilityConfigHash）。这里不得用应用级 applicationHash 覆盖
	// SpecHash 或据此递增 generation：两个写入方交替改写同一行会造成每次
	// 协调触发都 bump 代次，容器 applied-state 标签永远落后于应用行代次，
	// 5 秒漂移巡检把入口代理当作"全部漂移"无限全量重部署，generation 持续
	// 增长且协调记录被刷屏。
	if current.Kind == ApplicationKindFacility {
		return current, nil
	}
	in := SaveInput{
		Name:              current.Name,
		Enabled:           current.Enabled,
		SpecYAML:          current.SpecYAML,
		DeploymentMode:    current.DeploymentMode,
		DeploymentServers: current.DeploymentServers,
		ReverseProxy:      current.ReverseProxy,
	}
	generation := current.Generation
	prepared, err := s.prepare(ctx, in, generation, current.ID)
	if err != nil {
		return Application{}, err
	}
	changed := prepared.hash != current.SpecHash
	if changed {
		generation++
		prepared, err = s.prepare(ctx, in, generation, current.ID)
		if err != nil {
			return Application{}, err
		}
	}
	app := current
	app.PersistentPath = prepared.persistentPath
	app.DeploymentMode = prepared.deploymentMode
	app.DeploymentServers = prepared.deploymentServers
	app.ReverseProxy = prepared.reverseProxy
	app.Generation = generation
	app.SpecHash = prepared.hash
	app.JobID = prepared.job.ID
	app.Namespace = s.currentConfig().Namespace
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplicationDerived(ctx, app); err != nil {
		return Application{}, err
	}
	if changed {
		revisionJob, issues, err := s.renderApplication(ctx, app)
		if err != nil {
			return Application{}, err
		}
		if len(issues) > 0 {
			revisionJob = prepared.job
		}
		s.insertRevisionBestEffort(ctx, app, revisionJob)
	}
	return app, nil
}

func fileVariables(files []ApplicationFile) map[string]any {
	items := make([]map[string]any, 0, len(files))
	byName := map[string]any{}
	for _, file := range files {
		content := string(file.Content)
		item := map[string]any{
			"name":        file.Name,
			"kind":        file.Kind,
			"contentType": file.ContentType,
			"size":        file.Size,
			"sha256":      file.SHA256,
			"content":     content,
			"base64":      base64.StdEncoding.EncodeToString(file.Content),
		}
		items = append(items, item)
		byName[file.Name] = item
	}
	return map[string]any{"items": items, "byName": byName}
}

func applicationHash(spec appspec.Spec, deploymentMode string, deploymentServers []string, reverseProxy []ReverseProxyRule, files []ApplicationFile, resolved map[string]any) (string, error) {
	fileRefs := make([]map[string]any, 0, len(files))
	for _, file := range files {
		fileRefs = append(fileRefs, map[string]any{
			"name":   file.Name,
			"kind":   file.Kind,
			"sha256": file.SHA256,
			"size":   file.Size,
		})
	}
	payload := map[string]any{
		"spec":     appspec.Normalize(spec),
		"resolved": stableResolvedVariables(resolved),
		"deployment": map[string]any{
			"mode":    deploymentMode,
			"servers": deploymentServers,
		},
		"reverseProxy": reverseProxy,
		"files":        fileRefs,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func stableResolvedVariables(resolved map[string]any) map[string]any {
	raw, _ := json.Marshal(resolved)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	if appValue, ok := out["app"].(map[string]any); ok {
		delete(appValue, "generation")
	}
	return out
}

func normalizeApplicationFileName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", panelerr.Validation("application_file_name_invalid", "application file name is invalid")
	}
	// 拒绝路径分隔符、目录穿越和控制字符，避免 zip-slip 与“能保存无法下载”的死数据；同时限制长度。
	if len(name) > 255 {
		return "", panelerr.Validation("application_file_name_invalid", "application file name is too long")
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", panelerr.Validation("application_file_name_invalid", "application file name is invalid")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", panelerr.Validation("application_file_name_invalid", "application file name is invalid")
		}
	}
	return name, nil
}

func applicationPersistentDir(appID string) string {
	if strings.TrimSpace(appID) == "" {
		return "__panel_persistent__"
	}
	return "/opt/panel/apps/" + appID + "/persistent"
}

func specUsesPersistentMount(spec appspec.Spec) bool {
	for _, mount := range spec.Mounts {
		if strings.TrimSpace(mount.Type) == "persistent" {
			return true
		}
	}
	return false
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

func runtimeSpecUsesExternalMounts(spec appruntime.Spec) bool {
	for _, mount := range spec.Mounts {
		switch strings.TrimSpace(mount.Type) {
		case "volume", "bind", "persistent":
			return true
		}
	}
	return false
}

func isNotFound(err error) bool {
	var pe *panelerr.Error
	return errors.As(err, &pe) && pe.Code == "not_found"
}

func applicationFileMounts(mounts []appspec.Mount) []appspec.Mount {
	out := make([]appspec.Mount, 0, len(mounts))
	for _, mount := range mounts {
		if mountType := strings.TrimSpace(mount.Type); mountType == "file" || mountType == "panel_file" {
			out = append(out, mount)
		}
	}
	return out
}

func panelFileAllocationName(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "managed/" + hex.EncodeToString(sum[:8]) + ".pem"
}

func panelFilePerms(source string) string {
	if strings.HasSuffix(source, ":private_key") || strings.HasSuffix(source, ":ca_private_key") {
		return "0600"
	}
	return "0644"
}

func validationResult(issues []ValidationIssue) ValidationResult {
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}
}

func applicationValidationError(issues []ValidationIssue) error {
	if len(issues) == 0 {
		return panelerr.Validation("application_invalid", "application definition is invalid")
	}
	message := issues[0].Message
	if field := strings.TrimSpace(issues[0].Field); field != "" {
		message = field + ": " + message
	}
	return panelerr.WithDetails(panelerr.Validation("application_invalid", message), map[string]any{
		"issues": issues,
	})
}

func validationIssuesFromSpecIssues(specIssues []appspec.Issue) []ValidationIssue {
	issues := make([]ValidationIssue, 0, len(specIssues))
	for _, issue := range specIssues {
		issues = append(issues, ValidationIssue{Field: issue.Field, Message: issue.Message})
	}
	return issues
}

func persistentPathForSpecYAML(appID, specYAML string) string {
	spec, issues := appspec.DecodeYAML(specYAML)
	if len(issues) > 0 || !specUsesPersistentMount(spec) {
		return ""
	}
	return applicationPersistentDir(appID)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func applicationKind(value string) string {
	switch strings.TrimSpace(value) {
	case ApplicationKindFacility:
		return ApplicationKindFacility
	default:
		return ApplicationKindUser
	}
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}
