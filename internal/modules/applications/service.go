package applications

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"path"
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
	deployment       DeploymentDispatcher
	imageResolver    ImageDigestResolver
	operationQueue   ContainerOperationQueue
	facilityRuntime  FacilityRuntimeProvider
	events           runtimeevents.EventWriter
	sessionMu        sync.Mutex
	saveSessions     map[string]*saveSession
	cleanupOnce      sync.Once
	editCleanupOnce  sync.Once
}

type ApplicationRuntime = Runtime

var errLifecycleTargetLeaseLost = errors.New("application lifecycle target lease lost")

const FacilityReverseProxyApplicationID = "facility-reverse-proxy"

type PlanResult struct {
	Application Application             `json:"application"`
	Spec        appruntime.Spec         `json:"spec"`
	Plan        appruntime.PlanResponse `json:"plan"`
}

type LogInput struct {
	InstanceID    string `json:"instanceId"`
	ContainerName string `json:"containerName"`
	Type          string `json:"type"`
	Tail          int    `json:"tail"`
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
	s := &Service{db: db, runtimeClient: runtimeClient, tasks: taskSvc, config: cfg, renderer: templatex.NewGoRenderer(), builtinResolver: NewApplicationVariableRegistry(), imageResolver: NewRegistryImageResolver(), saveSessions: map[string]*saveSession{}}
	s.startSaveSessionCleanup()
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

func WithImageDigestResolver(resolver ImageDigestResolver) Option {
	return func(s *Service) { s.imageResolver = resolver }
}

func WithReverseProxyReconciler(reconciler ReverseProxyReconciler) Option {
	return func(s *Service) { s.proxyReconciler = reconciler }
}

func WithApplicationReconcileTrigger(trigger ApplicationReconcileTrigger) Option {
	return func(s *Service) { s.reconcileTrigger = trigger }
}

func WithDeploymentDispatcher(dispatcher DeploymentDispatcher) Option {
	return func(s *Service) { s.deployment = dispatcher }
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

func WithFacilityRuntimeProvider(provider FacilityRuntimeProvider) Option {
	return func(s *Service) { s.facilityRuntime = provider }
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

func (s *Service) SetDeploymentDispatcher(dispatcher DeploymentDispatcher) {
	s.deployment = dispatcher
}

func (s *Service) SetFacilityRuntimeProvider(provider FacilityRuntimeProvider) {
	s.facilityRuntime = provider
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
	if s.logDB != nil {
		return s.logDB
	}
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
	apps := make([]Application, 0, len(rows))
	for _, m := range rows {
		apps = append(apps, toDomainApplication(m))
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
	if err := orm.New(s.db).From("application_instances").Select("application_id", "status").AndIn("application_id", pageIDs).All(ctx, &instanceRows); err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}
	for _, m := range instanceRows {
		statuses[m.ApplicationID] = append(statuses[m.ApplicationID], appruntime.InstanceStatus{Status: m.Status})
		instanceCounts[m.ApplicationID]++
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(pageIDs)), ",")
	lifecycleRows, err := s.lifecycleDB().QueryContext(ctx, `WITH latest AS (
		SELECT id,application_id,ROW_NUMBER() OVER (PARTITION BY application_id ORDER BY created_at DESC,id DESC) AS row_num
		FROM application_lifecycle_operations
		WHERE application_id IN (`+placeholders+`)
	)
	SELECT latest.application_id,target.status
	FROM latest
	JOIN application_lifecycle_targets target ON target.operation_id=latest.id
	WHERE latest.row_num=1`, pageIDs...)
	if err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}
	defer lifecycleRows.Close()
	for lifecycleRows.Next() {
		var appID, status string
		if err := lifecycleRows.Scan(&appID, &status); err != nil {
			return httpx.ListPage[ApplicationSummary]{}, err
		}
		if _, ok := byID[appID]; ok {
			statuses[appID] = append(statuses[appID], appruntime.InstanceStatus{Status: runtimeStatusFromLifecycleTarget(status)})
		}
	}
	if err := lifecycleRows.Err(); err != nil {
		return httpx.ListPage[ApplicationSummary]{}, err
	}

	for i := range summaries {
		summaries[i].RuntimeStatus = aggregateRuntimeStatus(summaries[i].Enabled, statuses[summaries[i].ID])
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
	apps := make([]Application, 0, len(rows))
	for _, m := range rows {
		apps = append(apps, toDomainApplication(m))
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
	return toDomainApplication(m), nil
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
		if err := s.insertApplication(ctx, app); err != nil {
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
			if err := s.updateApplication(ctx, app); err != nil {
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
	if err := s.updateApplication(ctx, app); err != nil {
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

func (s *Service) Plan(ctx context.Context, appID string) (PlanResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return PlanResult{}, err
	}
	spec, issues, err := s.renderApplication(ctx, app)
	if err != nil {
		return PlanResult{}, err
	}
	if len(issues) > 0 {
		return PlanResult{}, applicationValidationError(issues)
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return PlanResult{}, err
	}
	serverIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		serverIDs = append(serverIDs, target.ID)
	}
	return PlanResult{Application: app, Spec: spec, Plan: appruntime.PlanResponse{InstanceCount: len(serverIDs), TargetServers: serverIDs}}, nil
}

func (s *Service) CheckImageUpdate(ctx context.Context, appID string) (Application, error) {
	app, err := s.checkImageUpdate(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	if _, err := s.recordTask(ctx, TaskTypeImageCheck, app.ID, "Checking image for "+app.Name); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
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
	if err := s.updateApplication(ctx, app); err != nil {
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

func (s *Service) RunDeployTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	opts := deployTaskOptions(task)
	claimed, err := s.ensureLifecycleTargetClaimedForTask(ctx, task, opts)
	if err != nil {
		return err
	}
	if !claimed {
		if s.tasks != nil && task.ID != "" {
			_ = s.tasks.Complete(ctx, task.ID, "Application target already claimed or completed")
		}
		return nil
	}
	if opts.action == "stop" || opts.action == "purge" {
		app, err := s.Get(ctx, appID)
		if err != nil {
			return err
		}
		if len(opts.targetIDs) == 0 {
			return panelerr.Validation("application_task_target_required", "Application task target is required")
		}
		if strings.TrimSpace(opts.lifecycleTargetID) == "" {
			if s.tasks != nil && task.ID != "" {
				_ = s.tasks.Complete(ctx, task.ID, "Application target task has no lifecycle target")
			}
			return nil
		}
		target, err := s.lifecycleTargetByID(ctx, opts.lifecycleTargetID)
		if err != nil {
			return err
		}
		if err := s.runStopLifecycleTargetTask(ctx, task, app, target, opts.action, opts.removeApplicationData); err != nil {
			return err
		}
		if app.DeletionRequested {
			if err := s.deleteApplicationIfRuntimeGone(ctx, app.ID); err != nil {
				return err
			}
		}
		if s.tasks != nil && task.ID != "" {
			_ = s.tasks.Complete(ctx, task.ID, "Application target stopped")
		}
		return nil
	}
	if opts.action == "apply" {
		app, err := s.Get(ctx, appID)
		if err != nil {
			return err
		}
		if deploymentTaskSuperseded(app, opts) {
			if err := s.supersedeLifecycleTargetForTask(ctx, task, "application is not enabled or deletion was requested before this target started"); err != nil {
				return err
			}
			if s.tasks != nil && task.ID != "" {
				_ = s.tasks.Complete(ctx, task.ID, "Application target superseded")
			}
			return nil
		}
	}
	app, job, err := s.prepareDeploy(ctx, appID)
	if err != nil {
		return err
	}
	if opts.action == "apply" && deploymentTaskSuperseded(app, opts) {
		if err := s.supersedeLifecycleTargetForTask(ctx, task, "desired state changed before this target started"); err != nil {
			return err
		}
		if s.tasks != nil && task.ID != "" {
			_ = s.tasks.Complete(ctx, task.ID, "Application target superseded")
		}
		return nil
	}
	if strings.TrimSpace(opts.lifecycleTargetID) != "" {
		target, err := s.lifecycleTargetByID(ctx, opts.lifecycleTargetID)
		if err != nil {
			return err
		}
		if err := s.runApplyLifecycleTargetTask(ctx, task, app, job, target); err != nil {
			_ = s.recordApplicationReconcileFailure(ctx, app.ID)
			return err
		}
		return nil
	}
	if s.tasks != nil && task.ID != "" {
		_ = s.tasks.Complete(ctx, task.ID, "Application target task has no lifecycle target")
	}
	return nil
}

func (s *Service) handleTargetTaskFailure(ctx context.Context, task tasks.Task, cause error) error {
	return s.failLifecycleTargetForTask(ctx, task, cause)
}

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

func (s *Service) runDeployTask(ctx context.Context, taskID string, app Application, job appruntime.Spec, targetIDs []string, lifecycleOperationID string) error {
	if err := s.deployRuntimeSpecTargets(ctx, taskID, app, job, targetIDs, lifecycleOperationID); err != nil {
		return err
	}
	return s.reconcileReverseProxy(ctx)
}

func (s *Service) Migrate(ctx context.Context, appID string, in MigrationInput) (OperationResult, error) {
	sourceServerID := strings.TrimSpace(in.SourceServerID)
	targetServerID := strings.TrimSpace(in.TargetServerID)
	if sourceServerID == "" || targetServerID == "" {
		return OperationResult{}, panelerr.Validation("application_migration_servers_required", "Source and target servers are required")
	}
	if sourceServerID == targetServerID {
		return OperationResult{}, panelerr.Validation("application_migration_servers_must_differ", "Source and target servers must differ")
	}
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	if !app.Enabled {
		return OperationResult{}, panelerr.Conflict("application_migration_requires_enabled", "Application must be enabled before migration")
	}
	if strings.TrimSpace(app.PersistentPath) != "" {
		return OperationResult{}, panelerr.Conflict("application_migration_persistent_not_supported", "Applications with persistent storage cannot use lossless migration")
	}
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return OperationResult{}, err
	}
	if len(instances) != 1 || instances[0].ServerID != sourceServerID {
		return OperationResult{}, panelerr.Conflict("application_migration_source_not_exclusive", "Source server must be the only deployed application instance")
	}
	if _, err := s.runtimeInstanceForServer(ctx, app.ID, targetServerID); err == nil {
		return OperationResult{}, panelerr.Conflict("application_migration_target_exists", "Target server already has an application deployment")
	} else if !isNotFound(err) {
		return OperationResult{}, err
	}
	sourceSrv, err := s.servers.Get(ctx, sourceServerID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := ensureAgentRuntimeReady(sourceSrv); err != nil {
		return OperationResult{}, err
	}
	sourceBaseURL, _ := agentURLFromServer(sourceSrv)
	sourceStatus, err := s.runtimeClient.RuntimeStatus(ctx, sourceBaseURL, instances[0].ID, instances[0].ContainerName)
	if err != nil {
		_ = s.handleAgentError(ctx, sourceSrv, err)
		return OperationResult{}, runtimeOperationError(err)
	}
	if sourceStatus.Status != appruntime.StatusRunning {
		return OperationResult{}, panelerr.Conflict("application_migration_source_not_running", "Source application instance must be running")
	}
	targetSrv, err := s.servers.Get(ctx, targetServerID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := ensureAgentRuntimeReady(targetSrv); err != nil {
		return OperationResult{}, err
	}
	currentJob, issues, err := s.renderApplication(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	if len(issues) > 0 {
		return OperationResult{}, applicationValidationError(issues)
	}
	if runtimeSpecUsesExternalMounts(currentJob) {
		return OperationResult{}, panelerr.Conflict("application_migration_mounts_not_supported", "Applications with host paths or Docker volumes cannot use lossless migration")
	}
	input := SaveInput{
		Name:              app.Name,
		Enabled:           app.Enabled,
		SpecYAML:          app.SpecYAML,
		DeploymentMode:    DeploymentModeSelected,
		DeploymentServers: []string{targetServerID},
		ReverseProxy:      app.ReverseProxy,
	}
	generation := app.Generation
	prepared, err := s.prepare(ctx, input, generation, app.ID)
	if err != nil {
		return OperationResult{}, err
	}
	if prepared.hash != app.SpecHash {
		generation++
		prepared, err = s.prepare(ctx, input, generation, app.ID)
		if err != nil {
			return OperationResult{}, err
		}
	}
	migrated := app
	migrated.DeploymentMode = prepared.deploymentMode
	migrated.DeploymentServers = prepared.deploymentServers
	migrated.PersistentPath = prepared.persistentPath
	migrated.ReverseProxy = prepared.reverseProxy
	migrated.Generation = generation
	migrated.SpecHash = prepared.hash
	migrated.JobID = prepared.job.ID
	migrated.Namespace = s.currentConfig().Namespace
	migrated.UpdatedAt = time.Now().UTC()
	job, issues, err := s.renderApplication(ctx, migrated)
	if err != nil {
		return OperationResult{}, err
	}
	if len(issues) > 0 {
		return OperationResult{}, applicationValidationError(issues)
	}
	if err := s.updateApplication(ctx, migrated); err != nil {
		return OperationResult{}, applicationSaveError(err)
	}
	if prepared.hash != app.SpecHash {
		s.insertRevisionBestEffort(ctx, migrated, job)
	}
	task, err := s.triggerApplicationReconcileTask(ctx, migrated.ID, "application_migrate", map[string]any{
		"applicationIds": []string{migrated.ID},
		"force":          true,
		"reason":         "application_migrate",
	})
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	out, err := s.Get(ctx, app.ID)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: task.ID, Application: out}, nil
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
	if err := s.updateApplication(ctx, app); err != nil {
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
	if err := s.ensureRuntimeInstancesReady(ctx, app.ID); err != nil {
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
	return OperationResult{DeploymentID: firstString(plan.OperationIDs), ApplicationRuntime: &runtime}, nil
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
	if operation, err := s.latestLifecycleOperation(ctx, app.ID); err == nil && operation.ID != "" {
		out.Operation = &operation
		out.Instances = mergeLifecycleTargetsIntoStatuses(out.Instances, operation.Targets)
	} else if app.Enabled {
		out.Instances = mergeLifecycleTargetsIntoStatuses(out.Instances, s.expectedLifecycleTargets(ctx, app))
	}
	out.Status = aggregateRuntimeStatus(app.Enabled, out.Instances)
	return out, nil
}

func (s *Service) latestLifecycleOperation(ctx context.Context, appID string) (LifecycleOperation, error) {
	var m models.ApplicationLifecycleOperation
	err := orm.New(s.lifecycleDB()).From("application_lifecycle_operations").Where("application_id=?", appID).OrderBy("created_at DESC", "id DESC").First(ctx, &m)
	if err == sql.ErrNoRows {
		return LifecycleOperation{}, panelerr.NotFound("application_lifecycle_operation")
	}
	if err != nil {
		return LifecycleOperation{}, err
	}
	op := toDomainLifecycleOperation(m)
	targets, err := s.lifecycleTargets(ctx, op.ID)
	if err != nil {
		return LifecycleOperation{}, err
	}
	op.Targets = targets
	return op, nil
}

func observedStateOf(instance *appruntime.Instance) string {
	if instance == nil {
		return ""
	}
	return strings.TrimSpace(instance.Status)
}

func observedErrorOf(instance *appruntime.Instance) string {
	if instance == nil {
		return ""
	}
	return strings.TrimSpace(instance.LastError)
}

func observedGenerationOf(instance *appruntime.Instance) int {
	if instance == nil {
		return 0
	}
	return instance.LastDeployedGeneration
}

func observedSpecHashOf(instance *appruntime.Instance) string {
	if instance == nil {
		return ""
	}
	return strings.TrimSpace(instance.RuntimeSpec.SpecHash)
}

func observedImageOf(instance *appruntime.Instance) string {
	if instance == nil {
		return ""
	}
	return strings.TrimSpace(instance.RuntimeSpec.Image)
}

func (s *Service) lifecycleTargets(ctx context.Context, operationID string) ([]LifecycleTarget, error) {
	var rows []lifecycleTargetRow
	if err := orm.New(s.lifecycleDB()).From("application_lifecycle_targets").Where("operation_id=?", operationID).OrderBy("server_id ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]LifecycleTarget, 0, len(rows))
	for _, r := range rows {
		target := toDomainLifecycleTarget(r)
		if s.servers != nil {
			if srv, err := s.servers.Get(ctx, target.ServerID); err == nil {
				target.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
			}
		}
		out = append(out, target)
	}
	return out, nil
}

func (s *Service) lifecycleTargetByID(ctx context.Context, targetID string) (LifecycleTarget, error) {
	var r lifecycleTargetRow
	err := orm.New(s.lifecycleDB()).From("application_lifecycle_targets").Where("id=?", targetID).First(ctx, &r)
	if err != nil {
		return LifecycleTarget{}, err
	}
	return toDomainLifecycleTarget(r), nil
}

func (s *Service) expectedLifecycleTargets(ctx context.Context, app Application) []LifecycleTarget {
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	out := make([]LifecycleTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, LifecycleTarget{
			ID:            "expected-" + runtimeInstanceID(app.ID, target.ID),
			ApplicationID: app.ID,
			ServerID:      target.ID,
			ServerName:    strings.TrimSpace(firstNonEmpty(target.Name, target.ID)),
			Status:        LifecycleTargetStatusPending,
			DesiredState:  appruntime.DesiredRunning,
			InstanceID:    runtimeInstanceID(app.ID, target.ID),
			ContainerName: runtimeContainerName(app),
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return out
}

func mergeLifecycleTargetsIntoStatuses(statuses []appruntime.InstanceStatus, targets []LifecycleTarget) []appruntime.InstanceStatus {
	if len(targets) == 0 {
		return statuses
	}
	out := append([]appruntime.InstanceStatus(nil), statuses...)
	byServer := map[string]int{}
	byInstance := map[string]int{}
	for i, status := range out {
		if status.ServerID != "" {
			byServer[status.ServerID] = i
		}
		if status.InstanceID != "" {
			byInstance[status.InstanceID] = i
		}
	}
	for _, target := range targets {
		idx, ok := byInstance[target.InstanceID]
		if !ok {
			idx, ok = byServer[target.ServerID]
		}
		if ok {
			if out[idx].LastError == "" {
				out[idx].LastError = target.Error
			}
			if out[idx].ContainerName == "" {
				out[idx].ContainerName = target.ContainerName
			}
			if out[idx].ContainerID == "" {
				out[idx].ContainerID = target.ContainerID
			}
			if out[idx].Stage == "" {
				out[idx].Stage = target.Stage
			}
			continue
		}
		out = append(out, appruntime.InstanceStatus{
			InstanceID:    target.InstanceID,
			ServerID:      target.ServerID,
			ServerName:    target.ServerName,
			ContainerName: target.ContainerName,
			ContainerID:   target.ContainerID,
			Status:        runtimeStatusFromLifecycleTarget(target.Status),
			DesiredState:  target.DesiredState,
			Stage:         target.Stage,
			LastError:     target.Error,
			ObservedAt:    target.UpdatedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ServerID == out[j].ServerID {
			return out[i].InstanceID < out[j].InstanceID
		}
		return out[i].ServerID < out[j].ServerID
	})
	return out
}

func runtimeStatusFromLifecycleTarget(status string) string {
	switch status {
	case LifecycleTargetStatusRunning:
		return appruntime.StatusRunning
	case LifecycleTargetStatusFailed:
		return appruntime.StatusFailed
	case LifecycleTargetStatusDeploying, LifecycleTargetStatusPreparing:
		return appruntime.StatusDeploying
	case LifecycleTargetStatusSuperseded:
		return appruntime.StatusPending
	default:
		return appruntime.StatusPending
	}
}

func (s *Service) withRuntimeSummary(ctx context.Context, app Application) (Application, error) {
	instances, err := s.runtimeInstances(ctx, app.ID)
	if err != nil {
		return Application{}, err
	}
	statuses := s.cachedInstanceStatuses(ctx, instances)
	if operation, err := s.latestLifecycleOperation(ctx, app.ID); err == nil && operation.ID != "" {
		statuses = mergeLifecycleTargetsIntoStatuses(statuses, operation.Targets)
	} else if app.Enabled {
		statuses = mergeLifecycleTargetsIntoStatuses(statuses, s.expectedLifecycleTargets(ctx, app))
	}
	app.RuntimeStatus = aggregateRuntimeStatus(app.Enabled, statuses)
	return app, nil
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
	return OperationResult{DeploymentID: firstString(plan.OperationIDs), Application: app, ApplicationRuntime: &runtime}, nil
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
	reverseProxy, err := normalizeReverseProxyRules(in.ReverseProxy)
	if err != nil {
		return preparedApplication{}, err
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

func (s *Service) deployRuntimeSpec(ctx context.Context, taskID string, app Application, spec appruntime.Spec) error {
	return s.deployRuntimeSpecTargets(ctx, taskID, app, spec, nil, "")
}

func (s *Service) runApplyLifecycleTargetTask(ctx context.Context, task tasks.Task, app Application, spec appruntime.Spec, targetRow LifecycleTarget) error {
	targetID := targetRow.ID
	taskID := task.ID
	target, err := s.servers.Get(ctx, targetRow.ServerID)
	if err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "load_server", "server_unavailable", err, true)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	targetName := firstNonEmpty(target.Name, target.ID, target.Host)
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStatePreparing, Stage: "validate_agent", Started: true, OwnerTaskID: taskID}); err != nil {
		return err
	}
	if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, taskID); err != nil {
		return err
	}
	if err := ensureAgentRuntimeReady(target); err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "validate_agent", "agent_unavailable", err, true)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	baseURL, ok := agentURLFromServer(target)
	if !ok {
		err := panelerr.Validation("agent_required", "Agent is required for application deployment")
		_ = s.failLifecycleTargetExecution(ctx, targetID, "validate_agent", "agent_unavailable", err, true)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	files, err := s.listFiles(ctx, app.ID, true)
	if err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "load_files", "render_failed", err, false)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStatePreparing, Stage: "render", OwnerTaskID: taskID}); err != nil {
		return err
	}
	instanceSpec, err := s.runtimeSpecForServer(ctx, app, spec, target, files)
	if err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "render", "render_failed", err, false)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	previous := appruntime.Instance{}
	previousContainerName := ""
	if current, err := s.runtimeInstanceForServer(ctx, app.ID, target.ID); err == nil {
		previous = current
		previousContainerName = current.ContainerName
	}
	if err := s.upsertRuntimeInstancePreservingContainerName(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "prepare_instance", "render_failed", err, false)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "deploying "+instanceSpec.ContainerName+" on "+targetName)
	}
	var result agentcontract.RuntimeInstanceResponse
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "waiting_server_queue", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName, OwnerTaskID: taskID}); err != nil {
		return err
	}
	err = s.executeContainerOperation(ctx, target.ID, func(runCtx context.Context) error {
		return s.withLifecycleTargetLeaseHeartbeat(runCtx, targetID, taskID, func(runCtx context.Context) error {
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
				return err
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "plan_update", OwnerTaskID: taskID}); err != nil {
				return err
			}
			reloaded, reloadResult, reloadErr := s.tryRuntimeReload(runCtx, taskID, targetName, baseURL, app, target, previous, instanceSpec)
			if reloadErr != nil {
				return reloadErr
			}
			if reloaded {
				result = reloadResult
				return nil
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "write_files", StageDetail: fmt.Sprintf("写入 %d 个文件", len(files)), OwnerTaskID: taskID}); err != nil {
				return err
			}
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "write files", func(context.Context) error {
				return s.runtimeClient.RuntimeWriteFiles(runCtx, baseURL, agentcontract.RuntimeWriteFilesRequest{Spec: instanceSpec})
			}); err != nil {
				return deploymentStageError{stage: "write_files", code: "write_files_failed", retryable: true, err: err}
			}
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
				return err
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "pull_image", StageDetail: instanceSpec.Image, OwnerTaskID: taskID}); err != nil {
				return err
			}
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "pull image", func(context.Context) error {
				return s.runtimeClient.DockerImagePull(runCtx, baseURL, instanceSpec.Image)
			}); err != nil {
				return deploymentStageError{stage: "pull_image", code: "pull_image_failed", retryable: true, err: err}
			}
			if previousContainerName != "" && previousContainerName != instanceSpec.ContainerName {
				if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
					return err
				}
				if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "remove_previous_container", OwnerTaskID: taskID}); err != nil {
					return err
				}
				if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "remove previous container", func(context.Context) error {
					return s.runtimeClient.DockerContainerDelete(runCtx, baseURL, previousContainerName)
				}); err != nil {
					return deploymentStageError{stage: "remove_previous_container", code: "remove_container_failed", retryable: true, err: err}
				}
				if err := s.upsertRuntimeInstance(runCtx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
					return deploymentStageError{stage: "remove_previous_container", code: "remove_container_failed", retryable: true, err: err}
				}
			}
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
				return err
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "remove_target_container", OwnerTaskID: taskID}); err != nil {
				return err
			}
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "remove target container", func(context.Context) error {
				return s.runtimeClient.DockerContainerDelete(runCtx, baseURL, instanceSpec.ContainerName)
			}); err != nil {
				return deploymentStageError{stage: "remove_target_container", code: "remove_container_failed", retryable: true, err: err}
			}
			var created agentcontract.RuntimeCreateContainerResponse
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
				return err
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "create_container", OwnerTaskID: taskID}); err != nil {
				return err
			}
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "create container", func(context.Context) error {
				var createErr error
				created, createErr = s.runtimeClient.RuntimeCreateContainer(runCtx, baseURL, agentcontract.RuntimeCreateContainerRequest{ServerID: target.ID, Spec: instanceSpec})
				return createErr
			}); err != nil {
				return deploymentStageError{stage: "create_container", code: "create_container_failed", retryable: false, err: err}
			}
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
				return err
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "start_container", StageDetail: "容器 " + instanceSpec.ContainerName, ContainerID: created.ContainerID, OwnerTaskID: taskID}); err != nil {
				return err
			}
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "start container", func(context.Context) error {
				return s.runtimeClient.DockerContainerAction(runCtx, baseURL, firstNonEmpty(created.ContainerID, instanceSpec.ContainerName), "start")
			}); err != nil {
				return deploymentStageError{stage: "start_container", code: "start_container_failed", retryable: false, err: err}
			}
			var status agentcontract.RuntimeStatusResponse
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
				return err
			}
			if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateApplying, Stage: "inspect", OwnerTaskID: taskID}); err != nil {
				return err
			}
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "inspect container", func(context.Context) error {
				var statusErr error
				status, statusErr = s.runtimeClient.RuntimeStatus(runCtx, baseURL, instanceSpec.InstanceID, instanceSpec.ContainerName)
				return statusErr
			}); err != nil {
				return deploymentStageError{stage: "inspect", code: "verify_failed", retryable: true, err: err}
			}
			result = agentcontract.RuntimeInstanceResponse{
				InstanceID:    instanceSpec.InstanceID,
				ContainerName: instanceSpec.ContainerName,
				ContainerID:   firstNonEmpty(status.ContainerID, created.ContainerID),
				Status:        status.Status,
				Error:         status.LastError,
				ObservedAt:    status.ObservedAt,
			}
			if result.Status != appruntime.StatusRunning {
				if strings.TrimSpace(result.Error) != "" {
					return deploymentStageError{stage: "inspect", code: "verify_failed", retryable: false, err: errors.New(result.Error)}
				}
				return deploymentStageError{stage: "inspect", code: "verify_failed", retryable: false, err: fmt.Errorf("container %s is %s after start", instanceSpec.ContainerName, firstNonEmpty(result.Status, "not running"))}
			}
			return nil
		})
	})
	if err != nil {
		if errors.Is(err, errLifecycleTargetLeaseLost) {
			return err
		}
		stageErr := normalizeDeploymentStageError(err, "apply", "application_runtime_operation_failed", true)
		_ = s.handleAgentError(ctx, target, stageErr.err)
		_ = s.upsertRuntimeInstancePreservingContainerName(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusFailed, "", stageErr.err.Error())
		_ = s.failLifecycleTargetExecution(ctx, targetID, stageErr.stage, stageErr.code, stageErr.err, stageErr.retryable)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "stderr", "deploying on "+targetName+" failed: "+stageErr.err.Error())
		}
		return stageErr.err
	}
	if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, taskID); err != nil {
		return err
	}
	if err := s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, result.Status, result.ContainerID, ""); err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "store_instance", "verify_failed", err, false)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateVerifying, Stage: "inspect", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName, ContainerID: result.ContainerID, OwnerTaskID: taskID}); err != nil {
		return err
	}
	if err := s.enqueueLifecycleTargetVerification(ctx, targetID, targetRow.OperationID); err != nil {
		return err
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application synced")
	}
	return nil
}

func (s *Service) runStopLifecycleTargetTask(ctx context.Context, task tasks.Task, app Application, targetRow LifecycleTarget, action string, removeApplicationData bool) error {
	targetID := targetRow.ID
	taskID := task.ID
	if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, taskID); err != nil {
		return err
	}
	srv, err := s.servers.Get(ctx, targetRow.ServerID)
	if err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "load_server", "server_unavailable", err, true)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	targetName := firstNonEmpty(srv.Name, srv.ID, srv.Host)
	if err := ensureAgentRuntimeReady(srv); err != nil {
		_ = s.failLifecycleTargetExecution(ctx, targetID, "validate_agent", "agent_unavailable", err, true)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		return err
	}
	state := LifecycleTargetStateStopping
	stage := "stop_container"
	if action == LifecycleTargetActionPurge || removeApplicationData {
		state = LifecycleTargetStatePurging
		stage = "purge_runtime"
	}
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStatePreparing, Stage: "validate_agent", Started: true, OwnerTaskID: taskID}); err != nil {
		return err
	}
	if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, taskID); err != nil {
		return err
	}
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: state, Stage: "waiting_server_queue", OwnerTaskID: taskID}); err != nil {
		return err
	}
	if s.tasks != nil && taskID != "" {
		verb := "stopping"
		if state == LifecycleTargetStatePurging {
			verb = "purging"
		}
		_ = s.tasks.AppendLog(ctx, taskID, "system", verb+" "+app.Name+" on "+targetName)
	}
	err = s.withLifecycleTargetLeaseHeartbeat(ctx, targetID, taskID, func(runCtx context.Context) error {
		if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, taskID); err != nil {
			return err
		}
		if err := s.updateLifecycleTarget(runCtx, targetID, lifecycleTargetUpdate{State: state, Stage: stage, OwnerTaskID: taskID}); err != nil {
			return err
		}
		if state == LifecycleTargetStatePurging {
			return s.purgeRuntimeInstanceForServer(runCtx, taskID, app.ID, targetRow.ServerID, removeApplicationData)
		}
		return s.stopRuntimeInstanceForServer(runCtx, taskID, app.ID, targetRow.ServerID)
	})
	if err != nil {
		if errors.Is(err, errLifecycleTargetLeaseLost) {
			return err
		}
		stageErr := normalizeDeploymentStageError(err, stage, "application_runtime_operation_failed", true)
		_ = s.handleAgentError(ctx, srv, stageErr.err)
		_ = s.failLifecycleTargetExecution(ctx, targetID, stageErr.stage, stageErr.code, stageErr.err, stageErr.retryable)
		_ = s.enqueueDeploymentAggregate(ctx, targetRow.OperationID)
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "stderr", stage+" on "+targetName+" failed: "+stageErr.err.Error())
		}
		return stageErr.err
	}
	if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, taskID); err != nil {
		return err
	}
	if err := s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{State: LifecycleTargetStateVerifying, Stage: "verify", OwnerTaskID: taskID}); err != nil {
		return err
	}
	if err := s.enqueueLifecycleTargetVerification(ctx, targetID, targetRow.OperationID); err != nil {
		return err
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application target stopped")
	}
	return nil
}

func (s *Service) deployRuntimeSpecTargets(ctx context.Context, taskID string, app Application, spec appruntime.Spec, targetIDs []string, lifecycleOperationID string) error {
	if s.runtimeClient == nil {
		return panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return err
	}
	targets = filterDeploymentTargets(targets, targetIDs)
	if len(targets) == 0 {
		return panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	operation := LifecycleOperation{ID: strings.TrimSpace(lifecycleOperationID)}
	if operation.ID == "" {
		var err error
		operation, err = s.createLifecycleOperation(ctx, app, spec, taskID, LifecycleTypeDeploy, targets)
		if err != nil {
			return err
		}
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Advance(ctx, taskID, "deploying", "deploying application instances")
	}
	leaseTaskID := ""
	if strings.TrimSpace(lifecycleOperationID) != "" {
		leaseTaskID = taskID
	}
	files, err := s.listFiles(ctx, app.ID, true)
	if err != nil {
		_ = s.finishLifecycleOperation(ctx, operation.ID, LifecycleStatusFailed, err)
		return err
	}
	failures := []runtimeDeploymentFailure{}
	for _, target := range targets {
		targetName := firstNonEmpty(target.Name, target.ID, target.Host)
		targetID := lifecycleTargetID(operation.ID, target.ID)
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusPreparing, Stage: "validate_agent", Started: true})
		if err := ensureAgentRuntimeReady(target); err != nil {
			failures = append(failures, runtimeDeploymentFailure{targetName: targetName, err: err})
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Stage: "validate_agent", Error: err.Error(), Finished: true})
			if s.tasks != nil && taskID != "" {
				_ = s.tasks.AppendLog(ctx, taskID, "stderr", "deploying on "+targetName+" failed: "+err.Error())
			}
			continue
		}
		baseURL, ok := agentURLFromServer(target)
		if !ok {
			err := panelerr.Validation("agent_required", "Agent is required for application deployment")
			failures = append(failures, runtimeDeploymentFailure{targetName: targetName, err: err})
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Stage: "validate_agent", Error: err.Error(), Finished: true})
			if s.tasks != nil && taskID != "" {
				_ = s.tasks.AppendLog(ctx, taskID, "stderr", "deploying on "+targetName+" failed: "+err.Error())
			}
			continue
		}
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusPreparing, Stage: "render"})
		instanceSpec, err := s.runtimeSpecForServer(ctx, app, spec, target, files)
		if err != nil {
			failures = append(failures, runtimeDeploymentFailure{targetName: targetName, err: err})
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Stage: "render", Error: err.Error(), Finished: true})
			if s.tasks != nil && taskID != "" {
				_ = s.tasks.AppendLog(ctx, taskID, "stderr", "rendering on "+targetName+" failed: "+err.Error())
			}
			continue
		}
		previous := appruntime.Instance{}
		previousContainerName := ""
		if current, err := s.runtimeInstanceForServer(ctx, app.ID, target.ID); err == nil {
			previous = current
			previousContainerName = current.ContainerName
		}
		if err := s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Stage: "prepare_instance", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName, Error: err.Error(), Finished: true})
			return err
		}
		if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, leaseTaskID); err != nil {
			return err
		}
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "write_files", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName})
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "deploying "+instanceSpec.ContainerName+" on "+targetName)
		}
		var result agentcontract.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, target.ID, func(runCtx context.Context) error {
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "plan_update"})
			reloaded, reloadResult, reloadErr := s.tryRuntimeReload(runCtx, taskID, targetName, baseURL, app, target, previous, instanceSpec)
			if reloadErr != nil {
				return reloadErr
			}
			if reloaded {
				result = reloadResult
				return nil
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "write_files"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "write files", func(context.Context) error {
				return s.runtimeClient.RuntimeWriteFiles(runCtx, baseURL, agentcontract.RuntimeWriteFilesRequest{Spec: instanceSpec})
			}); err != nil {
				return err
			}
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "pull_image"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "pull image", func(context.Context) error {
				return s.runtimeClient.DockerImagePull(runCtx, baseURL, instanceSpec.Image)
			}); err != nil {
				return err
			}
			if previousContainerName != "" && previousContainerName != instanceSpec.ContainerName {
				if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
					return err
				}
				_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "remove_previous_container"})
				if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "remove previous container", func(context.Context) error {
					return s.runtimeClient.DockerContainerDelete(runCtx, baseURL, previousContainerName)
				}); err != nil {
					return err
				}
			}
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "remove_target_container"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "remove target container", func(context.Context) error {
				return s.runtimeClient.DockerContainerDelete(runCtx, baseURL, instanceSpec.ContainerName)
			}); err != nil {
				return err
			}
			var created agentcontract.RuntimeCreateContainerResponse
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "create_container"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "create container", func(context.Context) error {
				var createErr error
				created, createErr = s.runtimeClient.RuntimeCreateContainer(runCtx, baseURL, agentcontract.RuntimeCreateContainerRequest{ServerID: target.ID, Spec: instanceSpec})
				return createErr
			}); err != nil {
				return err
			}
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "start_container", ContainerID: created.ContainerID})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "start container", func(context.Context) error {
				return s.runtimeClient.DockerContainerAction(runCtx, baseURL, firstNonEmpty(created.ContainerID, instanceSpec.ContainerName), "start")
			}); err != nil {
				return err
			}
			var status agentcontract.RuntimeStatusResponse
			if err := s.ensureLifecycleTargetStillOwnedByTask(runCtx, targetID, leaseTaskID); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "inspect"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "inspect container", func(context.Context) error {
				var statusErr error
				status, statusErr = s.runtimeClient.RuntimeStatus(runCtx, baseURL, instanceSpec.InstanceID, instanceSpec.ContainerName)
				return statusErr
			}); err != nil {
				return err
			}
			result = agentcontract.RuntimeInstanceResponse{
				InstanceID:    instanceSpec.InstanceID,
				ContainerName: instanceSpec.ContainerName,
				ContainerID:   firstNonEmpty(status.ContainerID, created.ContainerID),
				Status:        status.Status,
				Error:         status.LastError,
				ObservedAt:    status.ObservedAt,
			}
			if result.Status != appruntime.StatusRunning {
				if strings.TrimSpace(result.Error) != "" {
					return errors.New(result.Error)
				}
				return fmt.Errorf("container %s is %s after start", instanceSpec.ContainerName, firstNonEmpty(result.Status, "not running"))
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errLifecycleTargetLeaseLost) {
				return err
			}
			_ = s.handleAgentError(ctx, target, err)
			_ = s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusFailed, "", err.Error())
			failures = append(failures, runtimeDeploymentFailure{targetName: targetName, err: err})
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Error: err.Error(), Finished: true})
			if s.tasks != nil && taskID != "" {
				_ = s.tasks.AppendLog(ctx, taskID, "stderr", "deploying on "+targetName+" failed: "+err.Error())
			}
			continue
		}
		if err := s.ensureLifecycleTargetStillOwnedByTask(ctx, targetID, leaseTaskID); err != nil {
			return err
		}
		if err := s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, result.Status, result.ContainerID, ""); err != nil {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Stage: "store_instance", Error: err.Error(), Finished: true})
			return err
		}
		targetStatus := LifecycleTargetStatusRunning
		if result.Status != appruntime.StatusRunning {
			targetStatus = result.Status
		}
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: targetStatus, Stage: "inspect", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName, ContainerID: result.ContainerID, Finished: true})
	}
	if len(failures) > 0 {
		err := runtimeDeploymentError(len(targets), failures)
		if lifecycleOperationID == "" {
			status := LifecycleStatusFailed
			if len(failures) < len(targets) {
				status = LifecycleStatusPartiallyDeployed
			}
			_ = s.finishLifecycleOperation(ctx, operation.ID, status, err)
		} else {
			_ = s.finishDeploymentOperationFromTargets(ctx, operation.ID)
		}
		return err
	}
	if lifecycleOperationID == "" {
		if err := s.finishLifecycleOperation(ctx, operation.ID, LifecycleStatusDeployed, nil); err != nil {
			return err
		}
	} else if err := s.finishDeploymentOperationFromTargets(ctx, operation.ID); err != nil {
		return err
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application synced")
	}
	return nil
}

type lifecycleTargetUpdate struct {
	State         string
	Status        string
	Stage         string
	StageDetail   string
	InstanceID    string
	ContainerName string
	ContainerID   string
	Error         string
	ErrorCode     string
	ErrorMessage  string
	ErrorDetail   string
	Started       bool
	Finished      bool
	OwnerTaskID   string
}

type deploymentStageError struct {
	stage     string
	code      string
	retryable bool
	err       error
}

func (e deploymentStageError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e deploymentStageError) Unwrap() error {
	return e.err
}

func normalizeDeploymentStageError(err error, stage, code string, retryable bool) deploymentStageError {
	var stageErr deploymentStageError
	if errors.As(err, &stageErr) {
		if stageErr.stage == "" {
			stageErr.stage = stage
		}
		if stageErr.code == "" {
			stageErr.code = code
		}
		return stageErr
	}
	return deploymentStageError{stage: stage, code: code, retryable: retryable, err: err}
}

type lifecycleOperationCreateOptions struct {
	DesiredState string
	Action       string
	InitialState string
	Trigger      string
}

func (s *Service) createLifecycleOperation(ctx context.Context, app Application, spec appruntime.Spec, taskID, opType string, targets []server.Server) (LifecycleOperation, error) {
	targetIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		targetIDs = append(targetIDs, target.ID)
	}
	return s.createLifecycleOperationForServerIDs(ctx, app, spec, taskID, opType, targetIDs, appruntime.DesiredRunning)
}

func (s *Service) createLifecycleOperationForServerIDs(ctx context.Context, app Application, spec appruntime.Spec, taskID, opType string, serverIDs []string, desiredState string) (LifecycleOperation, error) {
	return s.createLifecycleOperationForServerIDsWithOptions(ctx, app, spec, taskID, opType, serverIDs, lifecycleOperationCreateOptions{DesiredState: desiredState})
}

func (s *Service) createLifecycleOperationForServerIDsWithOptions(ctx context.Context, app Application, spec appruntime.Spec, taskID, opType string, serverIDs []string, opts lifecycleOperationCreateOptions) (LifecycleOperation, error) {
	now := time.Now().UTC()
	operation := LifecycleOperation{
		ID:            id.New("alop"),
		ApplicationID: app.ID,
		Type:          opType,
		Status:        LifecycleStatusDeploying,
		TaskID:        taskID,
		Generation:    spec.Generation,
		SpecHash:      spec.SpecHash,
		Trigger:       firstNonEmpty(opts.Trigger, "system"),
		CreatedAt:     now,
		StartedAt:     &now,
		UpdatedAt:     now,
	}
	serverIDs = uniqueStringItems(serverIDs)
	tx, err := s.lifecycleDB().BeginTx(ctx, nil)
	if err != nil {
		return LifecycleOperation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := orm.New(tx).From("application_lifecycle_operations").Insert(ctx, fromDomainLifecycleOperation(operation)); err != nil {
		return LifecycleOperation{}, err
	}
	targets := make([]LifecycleTarget, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		targetID := lifecycleTargetID(operation.ID, serverID)
		instanceID := runtimeInstanceID(app.ID, serverID)
		containerName := runtimeContainerName(app)
		desiredState := firstNonEmpty(opts.DesiredState, appruntime.DesiredRunning)
		action := firstNonEmpty(opts.Action, lifecycleActionForDesiredState(desiredState))
		state := firstNonEmpty(opts.InitialState, LifecycleTargetStatePlanned)
		status := lifecycleStatusForState(state)
		targetKey := lifecycleTargetKey(app.ID, serverID)
		priority := lifecyclePriorityForAction(action)
		var instance *appruntime.Instance
		if inst, err := s.runtimeInstanceForServer(ctx, app.ID, serverID); err == nil {
			instance = &inst
			instanceID = inst.ID
			containerName = inst.ContainerName
		}
		observedAt := now
		targetRow := fromDomainLifecycleTarget(LifecycleTarget{
			ID:                 targetID,
			OperationID:        operation.ID,
			ApplicationID:      app.ID,
			ServerID:           serverID,
			Action:             action,
			State:              state,
			Status:             status,
			TargetKey:          targetKey,
			DesiredState:       desiredState,
			DesiredGeneration:  spec.Generation,
			DesiredSpecHash:    spec.SpecHash,
			Priority:           priority,
			InstanceID:         instanceID,
			ContainerName:      containerName,
			ObservedState:      observedStateOf(instance),
			ObservedExitCode:   "",
			ObservedError:      observedErrorOf(instance),
			ObservedGeneration: observedGenerationOf(instance),
			ObservedSpecHash:   observedSpecHashOf(instance),
			ObservedImage:      observedImageOf(instance),
			ObservedAt:         &observedAt,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
		if err := orm.New(tx).From("application_lifecycle_targets").Insert(ctx, &targetRow); err != nil {
			return LifecycleOperation{}, err
		}
		target := LifecycleTarget{
			ID:                 targetID,
			OperationID:        operation.ID,
			ApplicationID:      app.ID,
			ServerID:           serverID,
			Action:             action,
			State:              state,
			Status:             status,
			TargetKey:          targetKey,
			DesiredState:       desiredState,
			DesiredGeneration:  spec.Generation,
			DesiredSpecHash:    spec.SpecHash,
			Priority:           priority,
			InstanceID:         instanceID,
			ContainerName:      containerName,
			ObservedState:      observedStateOf(instance),
			ObservedExitCode:   "",
			ObservedError:      observedErrorOf(instance),
			ObservedGeneration: observedGenerationOf(instance),
			ObservedSpecHash:   observedSpecHashOf(instance),
			ObservedImage:      observedImageOf(instance),
			ObservedAt:         &observedAt,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		targets = append(targets, target)
	}
	if err := tx.Commit(); err != nil {
		return LifecycleOperation{}, err
	}
	operation.Targets = targets
	s.writeApplicationOperationEvent(ctx, runtimeevents.EventApplicationOperationCreated, operation, app, "")
	return operation, nil
}

func (s *Service) updateLifecycleTarget(ctx context.Context, targetID string, in lifecycleTargetUpdate) error {
	now := formatTime(time.Now().UTC())
	updates := []string{"updated_at=?"}
	args := []any{now}
	if in.Status != "" {
		updates = append(updates, "status=?", `state=CASE
			WHEN ?='pending' THEN 'planned'
			WHEN ?='preparing' THEN 'preparing'
			WHEN ?='deploying' AND action='stop' THEN 'stopping'
			WHEN ?='deploying' AND action='purge' THEN 'purging'
			WHEN ?='deploying' THEN 'applying'
			WHEN ?='running' THEN 'succeeded'
			WHEN ?='failed' THEN 'failed'
			WHEN ?='superseded' THEN 'superseded'
			ELSE state END`)
		args = append(args, in.Status)
		for i := 0; i < 8; i++ {
			args = append(args, in.Status)
		}
	}
	if in.State != "" {
		updates = append(updates, "state=?", "status=?")
		args = append(args, in.State, lifecycleStatusForState(in.State))
	}
	if in.Stage != "" {
		updates = append(updates, "stage=?")
		args = append(args, in.Stage)
	}
	if in.InstanceID != "" {
		updates = append(updates, "instance_id=?")
		args = append(args, in.InstanceID)
	}
	if in.ContainerName != "" {
		updates = append(updates, "container_name=?")
		args = append(args, in.ContainerName)
	}
	if in.ContainerID != "" {
		updates = append(updates, "container_id=?")
		args = append(args, in.ContainerID)
	}
	if in.Error != "" {
		updates = append(updates, "error=?")
		args = append(args, in.Error)
	}
	if in.ErrorCode != "" {
		updates = append(updates, "error_code=?")
		args = append(args, in.ErrorCode)
	}
	if in.ErrorMessage != "" {
		updates = append(updates, "error_message=?")
		args = append(args, in.ErrorMessage)
	}
	if in.ErrorDetail != "" {
		updates = append(updates, "error_detail=?")
		args = append(args, in.ErrorDetail)
	}
	if in.Started {
		updates = append(updates, "started_at=COALESCE(started_at, ?)")
		args = append(args, now)
	}
	if in.Finished {
		updates = append(updates, "finished_at=?")
		args = append(args, now)
	}
	where := ` WHERE id=?`
	args = append(args, targetID)
	if strings.TrimSpace(in.OwnerTaskID) != "" {
		where += ` AND claimed_task_id=? AND lease_owner=? AND lease_expires_at<>'' AND lease_expires_at>? AND state IN (?,?,?,?,?)`
		args = append(args,
			strings.TrimSpace(in.OwnerTaskID),
			lifecycleTaskLeaseOwner(in.OwnerTaskID),
			formatTime(time.Now().UTC()),
			LifecycleTargetStateClaimed,
			LifecycleTargetStatePreparing,
			LifecycleTargetStateApplying,
			LifecycleTargetStateStopping,
			LifecycleTargetStatePurging)
	}
	res, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_lifecycle_targets SET `+strings.Join(updates, ",")+where, args...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.OwnerTaskID) != "" {
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return errLifecycleTargetLeaseLost
		}
	}
	if strings.TrimSpace(in.Stage) != "" && strings.TrimSpace(in.OwnerTaskID) != "" {
		stageStatus := "running"
		if in.State == LifecycleTargetStateSucceeded || (in.Finished && in.State == "") {
			stageStatus = "succeeded"
		}
		if in.State == LifecycleTargetStateFailed || in.State == LifecycleTargetStateFailedRetryable || in.State == LifecycleTargetStateCancelled {
			stageStatus = "failed"
		}
		detail := strings.TrimSpace(in.StageDetail)
		if detail == "" {
			detail = firstNonEmpty(in.ErrorMessage, in.ErrorDetail, in.Error)
		}
		var finishedAt *time.Time
		if stageStatus == "succeeded" || stageStatus == "failed" {
			finished := time.Now().UTC()
			finishedAt = &finished
		}
		if err := s.finishTargetRunningStages(ctx, targetID, "succeeded", nil, in.Stage); err != nil {
			return err
		}
		if err := s.recordTargetStage(ctx, targetID, in.Stage, stageStatus, detail, nil, finishedAt); err != nil {
			return err
		}
	}
	return err
}

func (s *Service) failLifecycleTargetExecution(ctx context.Context, targetID, stage, code string, cause error, retryable bool) error {
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil
	}
	if cause == nil {
		cause = errors.New("application lifecycle target failed")
	}
	now := time.Now().UTC()
	state := LifecycleTargetStateFailed
	nextRunAt := ""
	finishedAt := any(formatTime(now))
	if retryable {
		state = LifecycleTargetStateFailedRetryable
		nextRunAt = formatTime(now.Add(lifecycleExecutionRetryDelay(ctx, s.lifecycleDB(), targetID)))
		finishedAt = nil
	}
	res, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?,
			status=?,
			stage=?,
			error=?,
			error_code=?,
			error_message=?,
			error_detail=?,
			attempt=attempt+1,
			next_run_at=?,
			lease_owner='',
			lease_expires_at='',
			finished_at=?,
			updated_at=?
		WHERE id=?
		  AND state IN ('claimed','preparing','applying','stopping','purging','verifying')`,
		state,
		lifecycleStatusForState(state),
		stage,
		cause.Error(),
		firstNonEmpty(code, "application_runtime_operation_failed"),
		cause.Error(),
		cause.Error(),
		nextRunAt,
		finishedAt,
		formatTime(now),
		targetID)
	if err != nil {
		return err
	}
	_, err = res.RowsAffected()
	if err != nil {
		return err
	}
	if err := s.finishTargetRunningStages(ctx, targetID, "succeeded", nil, stage); err != nil {
		return err
	}
	return s.recordTargetStage(ctx, targetID, stage, "failed", cause.Error(), nil, &now)
}

func (s *Service) enqueueLifecycleTargetVerification(ctx context.Context, targetID, operationID string) error {
	if s.deployment != nil {
		s.deployment.EnqueueVerify(targetID)
		return nil
	}
	if err := s.verifyLifecycleTargetNow(ctx, targetID); err != nil {
		return err
	}
	target, err := s.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		return err
	}
	if err := s.afterLifecycleTargetVerified(ctx, target); err != nil {
		return err
	}
	return s.finishDeploymentOperationFromTargets(ctx, operationID)
}

func (s *Service) enqueueDeploymentAggregate(ctx context.Context, operationID string) error {
	if s.deployment != nil {
		s.deployment.EnqueueAggregate(operationID)
		return nil
	}
	return s.finishDeploymentOperationFromTargets(ctx, operationID)
}

func (s *Service) afterLifecycleTargetVerified(ctx context.Context, target LifecycleTarget) error {
	app, err := s.Get(ctx, target.ApplicationID)
	if err != nil && !isNotFound(err) {
		return err
	}
	if err == nil && app.DeletionRequested {
		if err := s.deleteApplicationIfRuntimeGone(ctx, app.ID); err != nil {
			return err
		}
	}
	if target.Action == LifecycleTargetActionApply || target.Action == LifecycleTargetActionStop || target.Action == LifecycleTargetActionPurge {
		return s.reconcileReverseProxy(ctx)
	}
	return nil
}

func lifecycleExecutionRetryDelay(ctx context.Context, db *sql.DB, targetID string) time.Duration {
	attempt := 0
	if db != nil {
		_ = orm.New(db).From("application_lifecycle_targets").Select("attempt").Where("id=?", targetID).ScanValue(ctx, &attempt)
	}
	delays := []time.Duration{
		10 * time.Second,
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
	}
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(delays) {
		return withLifecycleRetryJitter(delays[len(delays)-1])
	}
	return withLifecycleRetryJitter(delays[attempt])
}

func withLifecycleRetryJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	jitterMax := delay / 5
	if jitterMax <= 0 {
		return delay
	}
	return delay + time.Duration(rand.Int64N(int64(jitterMax)+1))
}

func (s *Service) verifyLifecycleTargetNow(ctx context.Context, targetID string) error {
	target, err := s.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		return err
	}
	switch target.Action {
	case LifecycleTargetActionApply:
		instance, err := s.runtimeInstanceForServer(ctx, target.ApplicationID, target.ServerID)
		if err != nil {
			_ = s.failLifecycleTargetExecution(ctx, target.ID, "verify", "verify_failed", err, true)
			return err
		}
		if instance.DesiredState != appruntime.DesiredRunning || instance.Status != appruntime.StatusRunning {
			err := fmt.Errorf("runtime instance %s is %s/%s", instance.ID, instance.DesiredState, instance.Status)
			_ = s.failLifecycleTargetExecution(ctx, target.ID, "verify", "verify_failed", err, true)
			return err
		}
		if instance.LastDeployedGeneration != target.DesiredGeneration {
			err := fmt.Errorf("runtime generation %d does not match desired generation %d", instance.LastDeployedGeneration, target.DesiredGeneration)
			_ = s.failLifecycleTargetExecution(ctx, target.ID, "verify", "verify_failed", err, false)
			return err
		}
		if strings.TrimSpace(target.DesiredSpecHash) != "" && strings.TrimSpace(instance.RuntimeSpec.SpecHash) != strings.TrimSpace(target.DesiredSpecHash) {
			err := fmt.Errorf("runtime container configuration does not match the expected configuration (spec hash mismatch: running %s, expected %s)", instance.RuntimeSpec.SpecHash, target.DesiredSpecHash)
			_ = s.failLifecycleTargetExecution(ctx, target.ID, "verify", "verify_failed", err, true)
			return err
		}
	case LifecycleTargetActionStop, LifecycleTargetActionPurge:
		instance, err := s.runtimeInstanceForServer(ctx, target.ApplicationID, target.ServerID)
		if err != nil && !isNotFound(err) {
			_ = s.failLifecycleTargetExecution(ctx, target.ID, "verify", "verify_failed", err, true)
			return err
		}
		if err == nil && instance.DesiredState != appruntime.DesiredStopped && instance.Status == appruntime.StatusRunning {
			err := fmt.Errorf("runtime instance %s is still running", instance.ID)
			_ = s.failLifecycleTargetExecution(ctx, target.ID, "verify", "verify_failed", err, true)
			return err
		}
	}
	now := formatTime(time.Now().UTC())
	err = orm.New(s.lifecycleDB()).From("application_lifecycle_targets").Where("id=?", target.ID).And("state=?", LifecycleTargetStateVerifying).UpdateColumns(ctx, map[string]any{
		"state":            LifecycleTargetStateSucceeded,
		"status":           lifecycleStatusForState(LifecycleTargetStateSucceeded),
		"stage":            firstNonEmpty(target.Stage, "verify"),
		"error":            "",
		"error_code":       "",
		"error_message":    "",
		"error_detail":     "",
		"attempt":          0,
		"next_run_at":      "",
		"lease_owner":      "",
		"lease_expires_at": "",
		"finished_at":      now,
		"updated_at":       now,
	})
	if err != nil {
		return err
	}
	return s.finishTargetRunningStages(ctx, target.ID, "succeeded", nil, "")
}

func (s *Service) finishLifecycleOperation(ctx context.Context, operationID, status string, cause error) error {
	now := formatTime(time.Now().UTC())
	errText := ""
	if cause != nil {
		errText = cause.Error()
	}
	err := orm.New(s.lifecycleDB()).From("application_lifecycle_operations").Where("id=?", operationID).UpdateColumns(ctx, map[string]any{
		"status":      status,
		"error":       errText,
		"finished_at": now,
		"updated_at":  now,
	})
	if err != nil {
		return err
	}
	op, getErr := s.lifecycleOperationByID(ctx, operationID)
	if getErr != nil {
		return nil
	}
	app, appErr := s.Get(ctx, op.ApplicationID)
	if appErr != nil {
		return nil
	}
	eventType := runtimeevents.EventApplicationOperationCompleted
	severity := runtimeevents.SeverityInfo
	if status == LifecycleStatusFailed || status == LifecycleStatusPartiallyDeployed {
		eventType = runtimeevents.EventApplicationOperationFailed
		severity = runtimeevents.SeverityError
	}
	s.writeApplicationOperationEvent(ctx, eventType, op, app, errText, severity)
	return nil
}

func (s *Service) finishDeploymentOperationFromTargets(ctx context.Context, operationID string) error {
	var targets []models.ApplicationLifecycleTarget
	if err := orm.New(s.lifecycleDB()).From("application_lifecycle_targets").Select("server_id", "state", "error").Where("operation_id=?", operationID).All(ctx, &targets); err != nil {
		return err
	}
	total := 0
	failed := 0
	pending := 0
	superseded := 0
	failures := []runtimeDeploymentFailure{}
	for _, t := range targets {
		total++
		serverID, state, errText := t.ServerID, t.State, t.Error
		targetName := serverNameForImageTarget(ctx, s.servers, serverID)
		if targetName == "" {
			targetName = serverID
		}
		switch state {
		case LifecycleTargetStateFailed, LifecycleTargetStateCancelled:
			failed++
			failures = append(failures, runtimeDeploymentFailure{targetName: targetName, err: fmt.Errorf("%s", errText)})
		case LifecycleTargetStateSuperseded:
			superseded++
		case LifecycleTargetStateSucceeded:
		default:
			pending++
		}
	}
	if pending > 0 {
		return nil
	}
	if superseded == total {
		return s.finishLifecycleOperation(ctx, operationID, LifecycleStatusSuperseded, nil)
	}
	if failed == 0 {
		return s.finishLifecycleOperation(ctx, operationID, LifecycleStatusDeployed, nil)
	}
	status := LifecycleStatusPartiallyDeployed
	if failed == total {
		status = LifecycleStatusFailed
	}
	return s.finishLifecycleOperation(ctx, operationID, status, runtimeDeploymentError(total, failures))
}

func (s *Service) writeApplicationOperationEvent(ctx context.Context, eventType string, op LifecycleOperation, app Application, failureSummary string, severityOpt ...string) {
	if s == nil || s.events == nil {
		return
	}
	severity := runtimeevents.SeverityInfo
	if len(severityOpt) > 0 && strings.TrimSpace(severityOpt[0]) != "" {
		severity = severityOpt[0]
	}
	s.events.Log(ctx, runtimeevents.WriteEventInput{
		EventType:    eventType,
		Category:     runtimeevents.CategoryApplication,
		Severity:     severity,
		Source:       firstNonEmpty(op.Trigger, "system"),
		SourceModule: "applications",
		DedupeKey:    "application_operation:" + op.ID + ":" + eventType,
		Summary:      applicationOperationSummary(eventType, app, op, failureSummary),
		OccurredAt:   time.Now().UTC(),
	})
}

func lifecycleOperationAction(op LifecycleOperation) string {
	for _, target := range op.Targets {
		if strings.TrimSpace(target.Action) != "" {
			return target.Action
		}
	}
	switch op.Type {
	case LifecycleTypeDeploy:
		return LifecycleTargetActionApply
	default:
		return op.Type
	}
}

func countLifecycleTargets(targets []LifecycleTarget, states ...string) int {
	wanted := stringBoolSet(states)
	count := 0
	for _, target := range targets {
		if wanted[target.State] {
			count++
		}
	}
	return count
}

func lifecycleTargetIDs(targets []LifecycleTarget, extra string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(extra)
	for _, target := range targets {
		add(target.ID)
	}
	return out
}

func nonEmptyStrings(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func applicationOperationSummary(eventType string, app Application, op LifecycleOperation, failureSummary string) string {
	summary := "Application operation created: " + app.Name
	switch eventType {
	case runtimeevents.EventApplicationOperationFailed:
		summary = "Application operation failed: " + app.Name
	case runtimeevents.EventApplicationOperationCompleted:
		summary = "Application operation completed: " + app.Name
	}
	if (eventType == runtimeevents.EventApplicationOperationCompleted || eventType == runtimeevents.EventApplicationOperationFailed) && len(op.Targets) > 0 {
		summary += fmt.Sprintf(" (%d/%d targets succeeded)", countLifecycleTargets(op.Targets, LifecycleTargetStateSucceeded), len(op.Targets))
	}
	if strings.TrimSpace(failureSummary) != "" {
		summary += " - " + strings.TrimSpace(failureSummary)
	}
	return summary
}

// FailStaleTargetTaskAnchors 取消仍处于 queued/scheduled/failed_retryable、
// 但其生命周期目标已不存在、已终态或不再由该任务持有的目标任务锚点。
// 这类幽灵锚点不会自行结束，会永久占住服务器的部署并发键，必须定期清理。
func (s *Service) FailStaleTargetTaskAnchors(ctx context.Context) error {
	if s == nil || s.tasks == nil {
		return nil
	}
	var joined error
	for _, taskType := range []string{TaskTypeTargetApply, TaskTypeTargetStop, TaskTypeTargetPurge} {
		offset := 0
		for {
			result, err := s.tasks.List(ctx, tasks.ListFilter{
				Type:            taskType,
				Statuses:        []string{tasks.StatusQueued, tasks.StatusScheduled, tasks.StatusFailedRetryable},
				Limit:           200,
				Offset:          offset,
				IncludeInternal: true,
			})
			if err != nil {
				joined = errors.Join(joined, err)
				break
			}
			for _, task := range result.Items {
				if err := s.cancelStaleTargetAnchor(ctx, task); err != nil {
					joined = errors.Join(joined, err)
				}
			}
			offset += len(result.Items)
			if len(result.Items) == 0 || offset >= result.Total {
				break
			}
		}
	}
	return joined
}

func (s *Service) cancelStaleTargetAnchor(ctx context.Context, task tasks.Task) error {
	opts := deployTaskOptions(task)
	targetID := strings.TrimSpace(opts.lifecycleTargetID)
	if targetID == "" && strings.TrimSpace(opts.lifecycleOperationID) != "" && len(opts.targetIDs) == 1 {
		targetID = lifecycleTargetID(opts.lifecycleOperationID, opts.targetIDs[0])
	}
	if targetID == "" {
		return s.cancelObsoleteTargetAnchor(ctx, task, "Application target task has no lifecycle target")
	}
	target, err := s.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.cancelObsoleteTargetAnchor(ctx, task, "Application lifecycle target no longer exists")
		}
		return err
	}
	switch target.State {
	case LifecycleTargetStateSucceeded,
		LifecycleTargetStateFailed,
		LifecycleTargetStateFailedRetryable,
		LifecycleTargetStateSuperseded,
		LifecycleTargetStateCancelled:
		return s.cancelObsoleteTargetAnchor(ctx, task, "Application lifecycle target already finished")
	}
	if strings.TrimSpace(target.ClaimedTaskID) != "" && strings.TrimSpace(target.ClaimedTaskID) != task.ID {
		return s.cancelObsoleteTargetAnchor(ctx, task, "Application lifecycle target is owned by another task")
	}
	return nil
}

func (s *Service) cancelObsoleteTargetAnchor(ctx context.Context, task tasks.Task, message string) error {
	if s == nil || s.tasks == nil {
		return nil
	}
	return s.tasks.Cancel(ctx, task.ID, message)
}

func (s *Service) ReconcileInterruptedLifecycleTasks(ctx context.Context) error {
	if s.tasks == nil {
		return nil
	}
	var joined error
	for _, taskType := range []string{TaskTypeTargetApply, TaskTypeTargetStop, TaskTypeTargetPurge} {
		offset := 0
		for {
			result, err := s.tasks.List(ctx, tasks.ListFilter{
				Type:            taskType,
				Statuses:        []string{tasks.StatusFailed, tasks.StatusCancelled},
				Limit:           200,
				Offset:          offset,
				IncludeInternal: true,
			})
			if err != nil {
				return err
			}
			for _, task := range result.Items {
				if err := s.failLifecycleTargetForTask(ctx, task, nil); err != nil {
					joined = errors.Join(joined, err)
				}
			}
			offset += len(result.Items)
			if len(result.Items) == 0 || offset >= result.Total {
				break
			}
		}
	}
	return joined
}

func (s *Service) failLifecycleTargetForTask(ctx context.Context, task tasks.Task, cause error) error {
	opts := deployTaskOptions(task)
	if opts.lifecycleOperationID == "" || len(opts.targetIDs) == 0 {
		return nil
	}
	message := targetTaskFailureMessage(task, cause)
	var joined error
	for _, serverID := range opts.targetIDs {
		targetID := lifecycleTargetID(opts.lifecycleOperationID, serverID)
		changed, err := s.failLifecycleTargetIfActive(ctx, targetID, message)
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		if !changed {
			continue
		}
		if opts.action == "apply" {
			if err := s.failDeployingRuntimeInstanceForTarget(ctx, task.ResourceID, serverID, message); err != nil {
				joined = errors.Join(joined, err)
			}
		}
		if err := s.finishDeploymentOperationFromTargets(ctx, opts.lifecycleOperationID); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (s *Service) FailPlannedLifecycleTargets(ctx context.Context, inputs []tasks.CreateInput, cause error) error {
	message := "Application target task was not created"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = cause.Error()
	}
	seenOperations := map[string]bool{}
	var joined error
	for _, input := range inputs {
		if strings.TrimSpace(input.ParamsJSON) == "" {
			continue
		}
		var params deployTaskParams
		if err := json.Unmarshal([]byte(input.ParamsJSON), &params); err != nil {
			continue
		}
		operationID := strings.TrimSpace(params.LifecycleOperationID)
		serverID := strings.TrimSpace(params.ServerID)
		if operationID == "" || serverID == "" {
			continue
		}
		targetID := lifecycleTargetID(operationID, serverID)
		if _, err := s.failLifecycleTargetIfActive(ctx, targetID, message); err != nil {
			joined = errors.Join(joined, err)
		}
		seenOperations[operationID] = true
	}
	for operationID := range seenOperations {
		if err := s.finishDeploymentOperationFromTargets(ctx, operationID); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func deploymentTaskSuperseded(app Application, opts deployTaskRunOptions) bool {
	if !app.Enabled || app.DeletionRequested {
		return true
	}
	if opts.desiredGeneration > 0 && opts.desiredGeneration != app.Generation {
		return true
	}
	if strings.TrimSpace(opts.desiredSpecHash) != "" && opts.desiredSpecHash != app.SpecHash {
		return true
	}
	return false
}

func (s *Service) supersedeLifecycleTargetForTask(ctx context.Context, task tasks.Task, message string) error {
	opts := deployTaskOptions(task)
	if opts.lifecycleOperationID == "" || len(opts.targetIDs) == 0 {
		return nil
	}
	var joined error
	for _, serverID := range opts.targetIDs {
		targetID := lifecycleTargetID(opts.lifecycleOperationID, serverID)
		if err := s.supersedeLifecycleTargetIfActive(ctx, targetID, message); err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		if err := s.finishDeploymentOperationFromTargets(ctx, opts.lifecycleOperationID); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (s *Service) supersedeLifecycleTargetIfActive(ctx context.Context, targetID, message string) error {
	now := formatTime(time.Now().UTC())
	_, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?, status=?, error=?, error_code=CASE WHEN error_code='' THEN ? ELSE error_code END,
			error_message=CASE WHEN error_message='' THEN ? ELSE error_message END,
			error_detail=CASE WHEN error_detail='' THEN ? ELSE error_detail END,
			stage=?,
			attempt=0,
			next_run_at='',
			lease_owner='',
			lease_expires_at='',
			finished_at=COALESCE(finished_at, ?),
			updated_at=?
		WHERE id=? AND state IN (?,?,?,?,?)`,
		LifecycleTargetStateSuperseded, LifecycleTargetStatusSuperseded, message, "superseded", message, message, "superseded", now, now, targetID,
		LifecycleTargetStatePlanned, LifecycleTargetStateReady, LifecycleTargetStateClaimed, LifecycleTargetStatePreparing, LifecycleTargetStateFailedRetryable)
	return err
}

func targetTaskFailureMessage(task tasks.Task, cause error) string {
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		return cause.Error()
	}
	if strings.TrimSpace(task.Error) != "" {
		return task.Error
	}
	if strings.TrimSpace(task.Status) != "" {
		return "Task ended with status " + task.Status
	}
	return "Application target task ended before lifecycle target finished"
}

func (s *Service) failLifecycleTargetIfActive(ctx context.Context, targetID, message string) (bool, error) {
	now := formatTime(time.Now().UTC())
	res, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?, status=?, error=?,
			error_code=CASE WHEN error_code='' THEN ? ELSE error_code END,
			error_message=CASE WHEN error_message='' THEN ? ELSE error_message END,
			error_detail=CASE WHEN error_detail='' THEN ? ELSE error_detail END,
			stage=CASE WHEN stage='' THEN ? ELSE stage END,
			finished_at=COALESCE(finished_at, ?), updated_at=?
		WHERE id=? AND state IN (?,?,?,?,?,?,?,?)`,
		LifecycleTargetStateFailed, LifecycleTargetStatusFailed, message, "task_create_failed", message, message, "interrupted", now, now, targetID,
		LifecycleTargetStatePlanned, LifecycleTargetStateReady, LifecycleTargetStateClaimed, LifecycleTargetStatePreparing, LifecycleTargetStateApplying, LifecycleTargetStateStopping, LifecycleTargetStatePurging, LifecycleTargetStateVerifying)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (s *Service) failDeployingRuntimeInstanceForTarget(ctx context.Context, appID, serverID, message string) error {
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(serverID) == "" {
		return nil
	}
	err := orm.New(s.db).From("application_instances").Where("application_id=?", appID).And("server_id=?", serverID).AndIn("status", []string{appruntime.StatusPending, appruntime.StatusDeploying}).UpdateColumns(ctx, map[string]any{
		"status":     appruntime.StatusFailed,
		"last_error": message,
		"updated_at": formatTime(time.Now().UTC()),
	})
	return err
}

func lifecycleTargetID(operationID, serverID string) string {
	return strings.TrimSpace(operationID) + "-" + sanitizeRuntimeName(serverID)
}

func lifecycleTargetKey(appID, serverID string) string {
	return "application:" + strings.TrimSpace(appID) + ":server:" + strings.TrimSpace(serverID)
}

func lifecycleActionForDesiredState(desiredState string) string {
	if strings.TrimSpace(desiredState) == appruntime.DesiredStopped {
		return LifecycleTargetActionStop
	}
	return LifecycleTargetActionApply
}

func lifecyclePriorityForAction(action string) int {
	switch strings.TrimSpace(action) {
	case LifecycleTargetActionPurge:
		return 30
	case LifecycleTargetActionStop:
		return 20
	default:
		return 10
	}
}

func lifecycleStatusForState(state string) string {
	switch strings.TrimSpace(state) {
	case LifecycleTargetStateSucceeded:
		return LifecycleTargetStatusRunning
	case LifecycleTargetStateFailedRetryable, LifecycleTargetStateFailed, LifecycleTargetStateCancelled:
		return LifecycleTargetStatusFailed
	case LifecycleTargetStateSuperseded:
		return LifecycleTargetStatusSuperseded
	case LifecycleTargetStateClaimed, LifecycleTargetStatePreparing:
		return LifecycleTargetStatusPreparing
	case LifecycleTargetStateApplying, LifecycleTargetStateStopping, LifecycleTargetStatePurging, LifecycleTargetStateVerifying:
		return LifecycleTargetStatusDeploying
	default:
		return LifecycleTargetStatusPending
	}
}

func lifecycleStateForStatus(status, action string) string {
	switch strings.TrimSpace(status) {
	case LifecycleTargetStatusPreparing:
		return LifecycleTargetStatePreparing
	case LifecycleTargetStatusDeploying:
		switch strings.TrimSpace(action) {
		case LifecycleTargetActionStop:
			return LifecycleTargetStateStopping
		case LifecycleTargetActionPurge:
			return LifecycleTargetStatePurging
		default:
			return LifecycleTargetStateApplying
		}
	case LifecycleTargetStatusRunning:
		return LifecycleTargetStateSucceeded
	case LifecycleTargetStatusFailed:
		return LifecycleTargetStateFailed
	case LifecycleTargetStatusSuperseded:
		return LifecycleTargetStateSuperseded
	default:
		return LifecycleTargetStatePlanned
	}
}

func (s *Service) runRuntimeDeployStep(ctx context.Context, taskID, targetName, step string, run func(context.Context) error) error {
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "system", step+" on "+targetName)
	}
	if err := run(ctx); err != nil {
		return fmt.Errorf("%s failed: %w", step, err)
	}
	return nil
}

func (s *Service) tryRuntimeReload(ctx context.Context, taskID, targetName, baseURL string, app Application, target server.Server, current appruntime.Instance, desired appruntime.Spec) (bool, agentcontract.RuntimeInstanceResponse, error) {
	planner, ok := s.facilityRuntime.(FacilityRuntimeUpdatePlanner)
	if !ok || current.ID == "" || current.Status != appruntime.StatusRunning {
		return false, agentcontract.RuntimeInstanceResponse{}, nil
	}
	plan := planner.PlanRuntimeUpdate(ctx, app, target, current.RuntimeSpec, desired)
	if plan.Mode != appruntime.UpdateModeReload || plan.Strategy == nil || len(plan.Strategy.ValidateCommand) == 0 || len(plan.Strategy.ReloadCommand) == 0 {
		return false, agentcontract.RuntimeInstanceResponse{}, nil
	}
	if !runtimeReloadStructureEqual(current.RuntimeSpec, desired) {
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "recreating "+desired.ContainerName+" on "+targetName+" because container structure changed")
		}
		return false, agentcontract.RuntimeInstanceResponse{}, nil
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "reloading "+desired.ContainerName+" on "+targetName)
	}
	response, err := s.runtimeClient.RuntimeReload(ctx, baseURL, agentcontract.RuntimeReloadRequest{
		Spec: desired, ContainerName: desired.ContainerName,
		ValidateCommand: append([]string(nil), plan.Strategy.ValidateCommand...),
		ReloadCommand:   append([]string(nil), plan.Strategy.ReloadCommand...),
	})
	if err != nil {
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "stderr", "reload request failed; falling back to recreation: "+err.Error())
		}
		return false, agentcontract.RuntimeInstanceResponse{}, nil
	}
	if response.Reloaded {
		return true, agentcontract.RuntimeInstanceResponse{
			InstanceID: desired.InstanceID, ContainerName: desired.ContainerName, ContainerID: current.ContainerID,
			Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC(),
		}, nil
	}
	message := firstNonEmpty(strings.TrimSpace(response.Error), "runtime reload failed")
	if strings.TrimSpace(response.Output) != "" {
		message += ": " + strings.TrimSpace(response.Output)
	}
	if response.Phase == "validate" {
		return false, agentcontract.RuntimeInstanceResponse{}, deploymentStageError{stage: "validate_reload", code: "reload_validation_failed", retryable: false, err: errors.New(message)}
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "stderr", message+"; falling back to recreation")
	}
	return false, agentcontract.RuntimeInstanceResponse{}, nil
}

func runtimeReloadStructureEqual(current, desired appruntime.Spec) bool {
	current.Files = nil
	desired.Files = nil
	current.Generation = 0
	desired.Generation = 0
	current.SpecHash = ""
	desired.SpecHash = ""
	return reflect.DeepEqual(current, desired)
}

type runtimeDeploymentFailure struct {
	targetName string
	err        error
}

func runtimeDeploymentError(targetCount int, failures []runtimeDeploymentFailure) error {
	if len(failures) == 0 {
		return nil
	}
	if targetCount == 1 && len(failures) == 1 {
		return runtimeOperationError(failures[0].err)
	}
	parts := make([]string, 0, len(failures))
	for _, failure := range failures {
		parts = append(parts, failure.targetName+": "+failure.err.Error())
	}
	return panelerr.BadGateway(
		"application_runtime_operation_failed",
		"Application runtime operation failed: deployment failed on "+strconv.Itoa(len(failures))+" of "+strconv.Itoa(targetCount)+" targets: "+strings.Join(parts, "; "),
	)
}

func (s *Service) ensureRuntimeInstancesReady(ctx context.Context, appID string) error {
	if s.servers == nil {
		return panelerr.Validation("server_provider_unavailable", "Server provider is unavailable")
	}
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		srv, err := s.servers.Get(ctx, instance.ServerID)
		if err != nil {
			return err
		}
		if err := ensureAgentRuntimeReady(srv); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) stopRuntimeInstances(ctx context.Context, taskID, appID string, purge bool, targetIDs ...[]string) error {
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return err
	}
	var wanted map[string]bool
	if len(targetIDs) > 0 {
		wanted = stringBoolSet(targetIDs[0])
	}
	for _, instance := range instances {
		if len(wanted) > 0 && !wanted[instance.ServerID] {
			continue
		}
		srv, err := s.servers.Get(ctx, instance.ServerID)
		if err != nil {
			return err
		}
		if err := ensureAgentRuntimeReady(srv); err != nil {
			return err
		}
		baseURL, _ := agentURLFromServer(srv)
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "stopping "+instance.ContainerName+" on "+firstNonEmpty(srv.Name, srv.ID, srv.Host))
		}
		var result agentcontract.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
			var runErr error
			result, runErr = s.runtimeClient.RuntimeStop(runCtx, baseURL, agentcontract.RuntimeStopRequest{ApplicationID: appID, InstanceID: instance.ID, ContainerName: instance.ContainerName, Purge: purge})
			return runErr
		})
		if err != nil {
			_ = s.handleAgentError(ctx, srv, err)
			_ = s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredStopped, appruntime.StatusFailed, "", err.Error())
			return runtimeOperationError(err)
		}
		status := result.Status
		if status == "purged" {
			status = appruntime.StatusStopped
		}
		if err := s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredStopped, status, result.ContainerID, ""); err != nil {
			return err
		}
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application stopped")
	}
	return nil
}

func (s *Service) executeContainerOperation(ctx context.Context, serverID string, run func(context.Context) error) error {
	if s.operationQueue == nil {
		return run(ctx)
	}
	return s.operationQueue.Execute(ctx, serverID, run)
}

func (s *Service) withLifecycleTargetLeaseHeartbeat(ctx context.Context, targetID, taskID string, run func(context.Context) error) error {
	if strings.TrimSpace(targetID) == "" || strings.TrimSpace(taskID) == "" {
		return run(ctx)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	interval := defaultDeploymentLeaseTTL / 3
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	errCh := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := s.renewLifecycleTargetTaskLease(runCtx, targetID, taskID); err != nil {
					errCh <- err
					cancel()
					return
				}
			}
		}
	}()
	if err := s.renewLifecycleTargetTaskLease(runCtx, targetID, taskID); err != nil {
		return err
	}
	err := run(runCtx)
	cancel()
	select {
	case heartbeatErr := <-errCh:
		if err == nil || errors.Is(err, context.Canceled) {
			return heartbeatErr
		}
	default:
	}
	return err
}

func (s *Service) renewLifecycleTargetTaskLease(ctx context.Context, targetID, taskID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	now := time.Now().UTC()
	res, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET lease_expires_at=?,
			updated_at=?
		WHERE id=?
		  AND claimed_task_id=?
		  AND lease_owner=?
		  AND lease_expires_at<>''
		  AND lease_expires_at>?
		  AND state IN (?,?,?,?,?)`,
		formatTime(now.Add(defaultDeploymentLeaseTTL)),
		formatTime(now),
		strings.TrimSpace(targetID),
		strings.TrimSpace(taskID),
		lifecycleTaskLeaseOwner(taskID),
		formatTime(now),
		LifecycleTargetStateClaimed,
		LifecycleTargetStatePreparing,
		LifecycleTargetStateApplying,
		LifecycleTargetStateStopping,
		LifecycleTargetStatePurging)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errLifecycleTargetLeaseLost
	}
	return nil
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
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	_, err = orm.RawExec(ctx, s.db, `UPDATE applications SET version=version+1,kind=?,name=?,enabled=?,deletion_requested=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,reverse_proxy_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		applicationKind(app.Kind), app.Name, boolInt(app.Enabled), boolInt(app.DeletionRequested), app.SpecYAML, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
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
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	result, err := orm.RawExec(ctx, exec, `UPDATE applications SET version=version+1,kind=?,name=?,enabled=?,deletion_requested=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,reverse_proxy_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=? AND version=?`,
		applicationKind(app.Kind), app.Name, boolInt(app.Enabled), boolInt(app.DeletionRequested), app.SpecYAML, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID, expectedVersion)
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
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	_, err = orm.RawExec(ctx, exec, `UPDATE applications SET version=version+1,kind=?,name=?,enabled=?,deletion_requested=?,spec_yaml=?,deployment_mode=?,deployment_server_ids_json=?,reverse_proxy_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		applicationKind(app.Kind), app.Name, boolInt(app.Enabled), boolInt(app.DeletionRequested), app.SpecYAML, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
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
	_, err = orm.RawExec(ctx, exec, `INSERT OR IGNORE INTO application_revisions(id,application_id,generation,spec_hash,spec_yaml,job_json,created_at) VALUES(?,?,?,?,?,?,?)`,
		id.New("arev"), app.ID, app.Generation, app.SpecHash, app.SpecYAML, string(raw), formatTime(time.Now().UTC()))
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

func isLifecycleTargetActiveConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "application_lifecycle_targets.target_key") || strings.Contains(msg, "idx_application_lifecycle_targets_active_key")
}

func (s *Service) recordTask(ctx context.Context, taskType, appID, summary string) (string, error) {
	if s.tasks == nil {
		return "", nil
	}
	task, _, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "application",
		ResourceID:   appID,
		Status:       tasks.StatusCompleted,
		Summary:      summary,
	}, tasks.Trigger{Type: "system"})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

func (s *Service) recordRunningTask(ctx context.Context, taskType, appID, summary string) (string, error) {
	task, _, err := s.recordRunningTaskObjectWithParams(ctx, taskType, appID, summary, "")
	return task.ID, err
}

func (s *Service) recordRunningTaskWithParams(ctx context.Context, taskType, appID, summary, paramsJSON string) (string, error) {
	task, _, err := s.recordRunningTaskObjectWithParams(ctx, taskType, appID, summary, paramsJSON)
	return task.ID, err
}

func (s *Service) recordRunningTaskObject(ctx context.Context, taskType, appID, summary string) (tasks.Task, bool, error) {
	return s.recordRunningTaskObjectWithParams(ctx, taskType, appID, summary, "")
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

type deployTaskParams struct {
	AppID                 string `json:"appId,omitempty"`
	ServerID              string `json:"serverId,omitempty"`
	LifecycleOperationID  string `json:"lifecycleOperationId,omitempty"`
	LifecycleTargetID     string `json:"lifecycleTargetId,omitempty"`
	Generation            int    `json:"generation,omitempty"`
	SpecHash              string `json:"specHash,omitempty"`
	Action                string `json:"action,omitempty"`
	Purge                 bool   `json:"purge,omitempty"`
	RemoveApplicationData bool   `json:"removeApplicationData,omitempty"`
}

func (s *Service) PlanApplicationDeployment(ctx context.Context, req DeploymentPlanRequest) (DeploymentPlanResult, error) {
	result, err := s.planApplicationDeployment(ctx, req)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	s.enqueueDeploymentPlanResult(result)
	return result, nil
}

func (s *Service) planApplicationDeployment(ctx context.Context, req DeploymentPlanRequest) (DeploymentPlanResult, error) {
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
	targetIDs := uniqueStringItems(req.ServerIDs)
	stopRequestIDs := uniqueStringItems(append(append([]string{}, req.StopServers...), req.ServerIDs...))
	triggerType := firstNonEmpty(req.TriggerType, "system")
	if app.DeletionRequested || !app.Enabled {
		stopTargets, err := s.reconcileStopTargets(ctx, app, stopRequestIDs)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		action := LifecycleTargetActionStop
		if app.DeletionRequested || req.Purge {
			action = LifecycleTargetActionPurge
		}
		return s.planTargetActions(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, stopTargets, action, appruntime.DesiredStopped, triggerType)
	}
	if app.Kind == ApplicationKindFacility && app.DeploymentMode == DeploymentModeSelected && len(app.DeploymentServers) == 0 {
		stopTargets, err := s.reconcileStopTargets(ctx, app, stopRequestIDs)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
		action := LifecycleTargetActionStop
		if req.Purge {
			action = LifecycleTargetActionPurge
		}
		return s.planTargetActions(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, stopTargets, action, appruntime.DesiredStopped, triggerType)
	}
	planApply := len(req.StopServers) == 0 || len(targetIDs) > 0
	result := DeploymentPlanResult{}
	if !planApply {
		explicitStopResult := DeploymentPlanResult{}
		if len(req.StopServers) > 0 {
			action := LifecycleTargetActionStop
			if req.Purge {
				action = LifecycleTargetActionPurge
			}
			explicitStopResult, err = s.planTargetActions(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, req.StopServers, action, appruntime.DesiredStopped, triggerType)
			if err != nil {
				return DeploymentPlanResult{}, err
			}
		}
		return explicitStopResult, nil
	}
	app, job, err := s.prepareDeploy(ctx, app.ID)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	targets = filterDeploymentTargets(targets, targetIDs)
	if !req.Force && !req.ObservedRuntimeDrift {
		targets, err = s.filterUnsatisfiedDeploymentTargets(ctx, app, job, targets)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
	}
	result, err = s.planTargetActions(ctx, app, job, serverIDsFromTargets(targets), LifecycleTargetActionApply, appruntime.DesiredRunning, triggerType)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	stopTargets, err := s.reconcileRemovedTargets(ctx, app)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	stopResult, err := s.planTargetActions(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, stopTargets, LifecycleTargetActionPurge, appruntime.DesiredStopped, triggerType)
	if err != nil {
		return DeploymentPlanResult{}, err
	}
	explicitStopResult := DeploymentPlanResult{}
	if len(req.StopServers) > 0 {
		action := LifecycleTargetActionStop
		if req.Purge {
			action = LifecycleTargetActionPurge
		}
		explicitStopResult, err = s.planTargetActions(ctx, app, appruntime.Spec{Generation: app.Generation, SpecHash: app.SpecHash}, req.StopServers, action, appruntime.DesiredStopped, triggerType)
		if err != nil {
			return DeploymentPlanResult{}, err
		}
	}
	return mergeDeploymentPlanResults(result, stopResult, explicitStopResult), nil
}

func (s *Service) enqueueDeploymentPlanResult(result DeploymentPlanResult) {
	if s == nil || s.deployment == nil {
		return
	}
	for _, target := range result.CreatedTargets {
		s.deployment.EnqueueExecute(target.ID)
	}
	for _, target := range result.SupersededTargets {
		if strings.TrimSpace(target.OperationID) != "" {
			s.deployment.EnqueueAggregate(target.OperationID)
		}
	}
	for _, operationID := range result.OperationIDs {
		s.deployment.EnqueueAggregate(operationID)
	}
}

func (s *Service) planTargetActions(ctx context.Context, app Application, spec appruntime.Spec, serverIDs []string, action, desiredState, triggerType string) (DeploymentPlanResult, error) {
	serverIDs = uniqueStringItems(serverIDs)
	result := DeploymentPlanResult{}
	if len(serverIDs) == 0 {
		return result, nil
	}
	action = firstNonEmpty(action, lifecycleActionForDesiredState(desiredState))
	createIDs := []string{}
	for _, serverID := range serverIDs {
		active, found, err := s.activeLifecycleTarget(ctx, app.ID, serverID)
		if err != nil {
			return result, err
		}
		if !found {
			createIDs = append(createIDs, serverID)
			continue
		}
		if active.Action == action && active.DesiredGeneration == spec.Generation && strings.TrimSpace(active.DesiredSpecHash) == strings.TrimSpace(spec.SpecHash) {
			result.ReusedTargetIDs = append(result.ReusedTargetIDs, active.ID)
			result.ReusedTargets = append(result.ReusedTargets, active)
			continue
		}
		if action == LifecycleTargetActionApply && active.Action == LifecycleTargetActionApply &&
			(active.DesiredGeneration != spec.Generation || strings.TrimSpace(active.DesiredSpecHash) != strings.TrimSpace(spec.SpecHash)) {
			if !lifecycleTargetCanBeSupersededBeforeMutation(active.State) {
				result.BlockedTargetIDs = append(result.BlockedTargetIDs, active.ID)
				result.BlockedTargets = append(result.BlockedTargets, active)
				continue
			}
			if err := s.supersedeLifecycleTargetIfActive(ctx, active.ID, "Desired application revision changed before this target started"); err != nil {
				return result, err
			}
			active.State = LifecycleTargetStateSuperseded
			active.Status = LifecycleTargetStatusSuperseded
			result.SupersededTargetIDs = append(result.SupersededTargetIDs, active.ID)
			result.SupersededTargets = append(result.SupersededTargets, active)
			createIDs = append(createIDs, serverID)
			continue
		}
		if lifecyclePriorityForAction(action) > active.Priority && lifecycleTargetCanBeSupersededBeforeMutation(active.State) {
			if err := s.supersedeLifecycleTargetIfActive(ctx, active.ID, "Higher-priority application target action replaced this target"); err != nil {
				return result, err
			}
			active.State = LifecycleTargetStateSuperseded
			active.Status = LifecycleTargetStatusSuperseded
			result.SupersededTargetIDs = append(result.SupersededTargetIDs, active.ID)
			result.SupersededTargets = append(result.SupersededTargets, active)
			createIDs = append(createIDs, serverID)
			continue
		}
		result.BlockedTargetIDs = append(result.BlockedTargetIDs, active.ID)
		result.BlockedTargets = append(result.BlockedTargets, active)
	}
	if len(createIDs) == 0 {
		return result, nil
	}
	operation, err := s.createLifecycleOperationForServerIDsWithOptions(ctx, app, spec, "", LifecycleTypeDeploy, createIDs, lifecycleOperationCreateOptions{
		DesiredState: desiredState,
		Action:       action,
		InitialState: LifecycleTargetStateReady,
		Trigger:      triggerType,
	})
	if err != nil {
		if !isLifecycleTargetActiveConflict(err) {
			return result, err
		}
		for _, serverID := range createIDs {
			active, found, activeErr := s.activeLifecycleTarget(ctx, app.ID, serverID)
			if activeErr != nil {
				return result, activeErr
			}
			if !found {
				return result, err
			}
			if active.Action == action && active.DesiredGeneration == spec.Generation && strings.TrimSpace(active.DesiredSpecHash) == strings.TrimSpace(spec.SpecHash) {
				result.ReusedTargetIDs = append(result.ReusedTargetIDs, active.ID)
				result.ReusedTargets = append(result.ReusedTargets, active)
				continue
			}
			result.BlockedTargetIDs = append(result.BlockedTargetIDs, active.ID)
			result.BlockedTargets = append(result.BlockedTargets, active)
		}
		return result, nil
	}
	result.OperationIDs = append(result.OperationIDs, operation.ID)
	created, err := s.lifecycleTargets(ctx, operation.ID)
	if err != nil {
		return result, err
	}
	for _, target := range created {
		result.CreatedTargetIDs = append(result.CreatedTargetIDs, target.ID)
		result.CreatedTargets = append(result.CreatedTargets, target)
	}
	return result, nil
}

func (s *Service) activeLifecycleTarget(ctx context.Context, appID, serverID string) (LifecycleTarget, bool, error) {
	var row lifecycleTargetRow
	if err := orm.New(s.lifecycleDB()).From("application_lifecycle_targets").
		Where("target_key=?", lifecycleTargetKey(appID, serverID)).
		And("target_key <> ''").
		And("state IN ('planned','ready','claimed','preparing','applying','stopping','purging','verifying','failed_retryable')").
		OrderBy("updated_at DESC", "created_at DESC", "id DESC").
		First(ctx, &row); err != nil {
		if err == sql.ErrNoRows {
			return LifecycleTarget{}, false, nil
		}
		return LifecycleTarget{}, false, err
	}
	return toDomainLifecycleTarget(row), true, nil
}

func lifecycleTargetCanBeSupersededBeforeMutation(state string) bool {
	switch strings.TrimSpace(state) {
	case LifecycleTargetStatePlanned, LifecycleTargetStateReady, LifecycleTargetStateClaimed, LifecycleTargetStatePreparing, LifecycleTargetStateFailedRetryable:
		return true
	default:
		return false
	}
}

func mergeDeploymentPlanResults(items ...DeploymentPlanResult) DeploymentPlanResult {
	out := DeploymentPlanResult{}
	for _, item := range items {
		out.OperationIDs = append(out.OperationIDs, item.OperationIDs...)
		out.CreatedTargetIDs = append(out.CreatedTargetIDs, item.CreatedTargetIDs...)
		out.ReusedTargetIDs = append(out.ReusedTargetIDs, item.ReusedTargetIDs...)
		out.SupersededTargetIDs = append(out.SupersededTargetIDs, item.SupersededTargetIDs...)
		out.BlockedTargetIDs = append(out.BlockedTargetIDs, item.BlockedTargetIDs...)
		out.CreatedTargets = append(out.CreatedTargets, item.CreatedTargets...)
		out.ReusedTargets = append(out.ReusedTargets, item.ReusedTargets...)
		out.SupersededTargets = append(out.SupersededTargets, item.SupersededTargets...)
		out.BlockedTargets = append(out.BlockedTargets, item.BlockedTargets...)
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

func targetTaskTypeForAction(action string) string {
	switch strings.TrimSpace(action) {
	case "stop":
		return TaskTypeTargetStop
	case "purge":
		return TaskTypeTargetPurge
	default:
		return TaskTypeTargetApply
	}
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

func (s *Service) deploymentOperationError(ctx context.Context, operationID string) error {
	var op models.ApplicationLifecycleOperation
	if err := orm.New(s.lifecycleDB()).From("application_lifecycle_operations").Where("id=?", operationID).First(ctx, &op); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if op.Status != LifecycleStatusFailed && op.Status != LifecycleStatusPartiallyDeployed {
		return nil
	}
	errText := op.Error
	if strings.TrimSpace(errText) == "" {
		errText = "Application sync failed"
	}
	return panelerr.BadGateway("application_runtime_operation_failed", errText)
}

type deployTaskRunOptions struct {
	targetIDs             []string
	lifecycleOperationID  string
	lifecycleTargetID     string
	desiredGeneration     int
	desiredSpecHash       string
	action                string
	purge                 bool
	removeApplicationData bool
}

func deployTaskOptions(task tasks.Task) deployTaskRunOptions {
	if strings.TrimSpace(task.ParamsJSON) != "" && strings.TrimSpace(task.ParamsJSON) != "{}" {
		var params deployTaskParams
		if err := json.Unmarshal([]byte(task.ParamsJSON), &params); err == nil {
			action := params.Action
			if strings.TrimSpace(action) == "" {
				action = targetActionForTaskType(task.Type)
			}
			return deployTaskRunOptions{
				targetIDs:             []string{strings.TrimSpace(params.ServerID)},
				lifecycleOperationID:  strings.TrimSpace(params.LifecycleOperationID),
				lifecycleTargetID:     strings.TrimSpace(params.LifecycleTargetID),
				desiredGeneration:     params.Generation,
				desiredSpecHash:       strings.TrimSpace(params.SpecHash),
				action:                action,
				purge:                 params.Purge,
				removeApplicationData: params.RemoveApplicationData,
			}
		}
	}
	if strings.TrimSpace(task.ServerID) != "" && strings.TrimSpace(task.ResourceID) != "" {
		return deployTaskRunOptions{targetIDs: []string{strings.TrimSpace(task.ServerID)}, action: targetActionForTaskType(task.Type)}
	}
	return deployTaskRunOptions{}
}
func (s *Service) ensureLifecycleTargetClaimedForTask(ctx context.Context, task tasks.Task, opts deployTaskRunOptions) (bool, error) {
	targetID := strings.TrimSpace(opts.lifecycleTargetID)
	if targetID == "" && strings.TrimSpace(opts.lifecycleOperationID) != "" && len(opts.targetIDs) == 1 {
		targetID = lifecycleTargetID(opts.lifecycleOperationID, opts.targetIDs[0])
	}
	if targetID == "" {
		return true, nil
	}
	target, err := s.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	owner := lifecycleTaskLeaseOwner(task.ID)
	now := time.Now().UTC()
	leaseActive := target.LeaseExpiresAt != nil && target.LeaseExpiresAt.After(now)
	if target.State == LifecycleTargetStateClaimed && target.ClaimedTaskID == task.ID && target.LeaseOwner == owner && leaseActive {
		return true, nil
	}
	switch target.State {
	case LifecycleTargetStatePreparing, LifecycleTargetStateApplying, LifecycleTargetStateStopping, LifecycleTargetStatePurging:
		return target.ClaimedTaskID == task.ID && target.LeaseOwner == owner && leaseActive, nil
	case LifecycleTargetStateReady:
	default:
		return false, nil
	}
	res, err := orm.RawExec(ctx, s.lifecycleDB(), `UPDATE application_lifecycle_targets
		SET state=?,
			status=?,
			lease_owner=?,
			lease_expires_at=?,
			claimed_task_id=?,
			started_at=COALESCE(started_at, ?),
			updated_at=?
		WHERE id=?
		  AND state=?
		  AND (next_run_at='' OR next_run_at<=?)
		  AND (claimed_task_id='' OR claimed_task_id=?)`,
		LifecycleTargetStateClaimed,
		lifecycleStatusForState(LifecycleTargetStateClaimed),
		owner,
		formatTime(now.Add(defaultDeploymentLeaseTTL)),
		task.ID,
		formatTime(now),
		formatTime(now),
		targetID,
		LifecycleTargetStateReady,
		formatTime(now),
		task.ID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

func (s *Service) ensureLifecycleTargetStillOwnedByTask(ctx context.Context, targetID, taskID string) error {
	targetID = strings.TrimSpace(targetID)
	taskID = strings.TrimSpace(taskID)
	if targetID == "" || taskID == "" {
		return nil
	}
	target, err := s.lifecycleTargetByID(ctx, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if target.ClaimedTaskID == taskID && target.LeaseOwner == lifecycleTaskLeaseOwner(taskID) && target.LeaseExpiresAt != nil && target.LeaseExpiresAt.After(time.Now().UTC()) {
		switch target.State {
		case LifecycleTargetStateClaimed, LifecycleTargetStatePreparing, LifecycleTargetStateApplying, LifecycleTargetStateStopping, LifecycleTargetStatePurging:
			return nil
		}
	}
	return errLifecycleTargetLeaseLost
}

func lifecycleTaskLeaseOwner(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "task"
	}
	return "task:" + taskID
}

func targetActionForTaskType(taskType string) string {
	switch strings.TrimSpace(taskType) {
	case TaskTypeTargetStop:
		return "stop"
	case TaskTypeTargetPurge:
		return "purge"
	case TaskTypeTargetApply:
		return "apply"
	default:
		return ""
	}
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

func stopTaskParamsJSON(purge bool) string {
	if !purge {
		return "{}"
	}
	return `{"purge":true}`
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

func stopTaskPurge(paramsJSON string) (bool, error) {
	if strings.TrimSpace(paramsJSON) == "" || strings.TrimSpace(paramsJSON) == "{}" {
		return false, nil
	}
	var params struct {
		Purge bool `json:"purge"`
	}
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return false, err
	}
	return params.Purge, nil
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

func (s *Service) cleanupRemovedDeploymentInstances(ctx context.Context, taskID string, before, after Application) error {
	if after.DeploymentMode != DeploymentModeSelected {
		return nil
	}
	desired := map[string]struct{}{}
	for _, serverID := range after.DeploymentServers {
		serverID = strings.TrimSpace(serverID)
		if serverID != "" {
			desired[serverID] = struct{}{}
		}
	}
	instances, err := s.runtimeInstances(ctx, before.ID)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if _, keep := desired[instance.ServerID]; keep {
			continue
		}
		if err := s.purgeRuntimeInstance(ctx, taskID, instance, false); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) purgeRuntimeInstanceForServer(ctx context.Context, taskID, appID, serverID string, removeApplicationData bool) error {
	instance, err := s.runtimeInstanceForServer(ctx, appID, serverID)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return s.purgeRuntimeInstance(ctx, taskID, instance, removeApplicationData)
}

func (s *Service) stopRuntimeInstanceForServer(ctx context.Context, taskID, appID, serverID string) error {
	instance, err := s.runtimeInstanceForServer(ctx, appID, serverID)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	srv, err := s.servers.Get(ctx, instance.ServerID)
	if err != nil {
		return err
	}
	if err := ensureAgentRuntimeReady(srv); err != nil {
		return err
	}
	baseURL, _ := agentURLFromServer(srv)
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "stopping "+instance.ContainerName+" on "+firstNonEmpty(srv.Name, srv.ID, srv.Host))
	}
	var result agentcontract.RuntimeInstanceResponse
	err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
		var runErr error
		result, runErr = s.runtimeClient.RuntimeStop(runCtx, baseURL, agentcontract.RuntimeStopRequest{
			ApplicationID: instance.ApplicationID,
			InstanceID:    instance.ID,
			ContainerName: instance.ContainerName,
		})
		return runErr
	})
	if err != nil {
		if isRuntimeAlreadyRequestedState(err) {
			return s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredStopped, appruntime.StatusStopped, "", "")
		}
		_ = s.handleAgentError(ctx, srv, err)
		_ = s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredStopped, appruntime.StatusFailed, "", err.Error())
		return runtimeOperationError(err)
	}
	status := result.Status
	if strings.TrimSpace(status) == "" || status == "purged" {
		status = appruntime.StatusStopped
	}
	return s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredStopped, status, result.ContainerID, "")
}

func (s *Service) deleteApplicationIfRuntimeGone(ctx context.Context, appID string) error {
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		return nil
	}
	return orm.New(s.db).From("applications").Where("id=?", appID).And("deletion_requested=1").Delete(ctx)
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
	return orm.New(s.db).From("applications").Where("id=?", appID).UpdateColumns(ctx, map[string]any{
		"reconcile_stopped": 1,
		"updated_at":        formatTime(time.Now().UTC()),
	})
}

func (s *Service) resetApplicationReconcileStopped(ctx context.Context, appID string) error {
	if err := s.resetApplicationReconcileFailures(ctx, appID); err != nil {
		return err
	}
	return orm.New(s.db).From("applications").Where("id=?", appID).UpdateColumns(ctx, map[string]any{
		"reconcile_stopped": 0,
		"updated_at":        formatTime(time.Now().UTC()),
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

func (s *Service) purgeApplicationRuntimeData(ctx context.Context, appID string) error {
	instances, err := s.runtimeInstances(ctx, appID)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return nil
	}
	byServer := map[string]appruntime.Instance{}
	for _, instance := range instances {
		byServer[instance.ServerID] = instance
	}
	for _, instance := range byServer {
		if err := s.purgeRuntimeInstance(ctx, "", instance, true); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) purgeRuntimeInstance(ctx context.Context, taskID string, instance appruntime.Instance, removeApplicationData bool) error {
	srv, err := s.servers.Get(ctx, instance.ServerID)
	if err != nil {
		return err
	}
	if err := ensureAgentRuntimeReady(srv); err != nil {
		return err
	}
	baseURL, _ := agentURLFromServer(srv)
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "cleaning "+instance.ContainerName+" on "+firstNonEmpty(srv.Name, srv.ID, srv.Host))
	}
	err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
		_, runErr := s.runtimeClient.RuntimeStop(runCtx, baseURL, agentcontract.RuntimeStopRequest{
			ApplicationID:         instance.ApplicationID,
			InstanceID:            instance.ID,
			ContainerName:         instance.ContainerName,
			Purge:                 true,
			RemoveApplicationData: removeApplicationData,
		})
		return runErr
	})
	if err != nil {
		if isRuntimeAlreadyRequestedState(err) {
			return s.deleteRuntimeInstanceForServer(ctx, instance.ApplicationID, instance.ServerID)
		}
		_ = s.handleAgentError(ctx, srv, err)
		return runtimeOperationError(err)
	}
	return s.deleteRuntimeInstanceForServer(ctx, instance.ApplicationID, instance.ServerID)
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
		targetType := normalizeReverseProxyTargetType(rule.TargetType)
		if targetType == "" {
			return nil, panelerr.Validation("application_reverse_proxy_target_type_invalid", "reverse proxy target type is invalid")
		}
		if rule.TargetPort <= 0 || rule.TargetPort > 65535 {
			return nil, panelerr.Validation("application_reverse_proxy_target_port_invalid", "reverse proxy target port must be between 1 and 65535")
		}
		originServerIDs := uniqueSortedStrings(rule.OriginServerIDs)
		anyAccess, err := NormalizeAnyAccessConfig(rule.AnyAccess, originServerIDs)
		if err != nil {
			return nil, err
		}
		paths := make([]ReverseProxyPath, 0, len(rule.Paths))
		pathKeys := map[string]struct{}{}
		for _, item := range rule.Paths {
			proxyPath := strings.TrimSpace(item.Path)
			if proxyPath == "" {
				proxyPath = "/"
			}
			if !strings.HasPrefix(proxyPath, "/") {
				return nil, panelerr.Validation("application_reverse_proxy_path_invalid", "reverse proxy path must start with /")
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
			OriginServerIDs: originServerIDs,
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
		if !validNginxToken(domain) {
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
			if !strings.HasPrefix(proxyPath, "/") || !validNginxPath(proxyPath) {
				return nil, panelerr.Validation("application_reverse_proxy_path_invalid", "reverse proxy path must start with /")
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

func validNginxPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n;{}")
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

func (s *Service) Package(ctx context.Context, appID string) (PackageResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return PackageResult{}, err
	}
	files, err := s.listFiles(ctx, app.ID, true)
	if err != nil {
		return PackageResult{}, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile := func(name string, data []byte) error {
		w, err := zw.Create(filepath.ToSlash(name))
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := writeZipFile("spec.yaml", []byte(app.SpecYAML)); err != nil {
		return PackageResult{}, err
	}
	metadata, _ := json.MarshalIndent(map[string]any{
		"id":                app.ID,
		"name":              app.Name,
		"generation":        app.Generation,
		"specHash":          app.SpecHash,
		"jobId":             app.JobID,
		"namespace":         app.Namespace,
		"persistentPath":    app.PersistentPath,
		"deploymentMode":    app.DeploymentMode,
		"deploymentServers": app.DeploymentServers,
		"reverseProxy":      app.ReverseProxy,
	}, "", "  ")
	if err := writeZipFile("application.json", metadata); err != nil {
		return PackageResult{}, err
	}
	for _, file := range files {
		name := path.Join("files", strings.TrimPrefix(file.Name, "/"))
		if err := writeZipFile(name, file.Content); err != nil {
			return PackageResult{}, err
		}
	}
	if name, content, err := s.renderReverseProxyConfig(ctx, app, files); err != nil {
		return PackageResult{}, err
	} else if name != "" {
		if err := writeZipFile(path.Join("nginx", name), []byte(content)); err != nil {
			return PackageResult{}, err
		}
	}
	if err := zw.Close(); err != nil {
		return PackageResult{}, err
	}
	return PackageResult{Filename: app.Name + "-package.zip", Content: buf.Bytes()}, nil
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
