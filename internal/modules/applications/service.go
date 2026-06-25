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
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications/runtime"
	"panel/internal/modules/applications/spec"
	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
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
	RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error)
	RuntimeStop(ctx context.Context, baseURL string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error)
	RuntimeRestart(ctx context.Context, baseURL string, req agentcontract.RuntimeRestartRequest) (agentcontract.RuntimeInstanceResponse, error)
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
	db              *sql.DB
	runtimeClient   AgentRuntimeClient
	servers         ServerProvider
	agentErrors     AgentErrorHandler
	tasks           *tasks.Service
	config          Config
	configProvider  func() Config
	renderer        templatex.Renderer
	builtinResolver BuiltinVariableResolver
	internalFiles   InternalFileProvider
	proxyReconciler ReverseProxyReconciler
	imageResolver   ImageDigestResolver
	operationQueue  ContainerOperationQueue
	sessionMu       sync.Mutex
	saveSessions    map[string]*saveSession
	cleanupOnce     sync.Once
}

type ApplicationRuntime = Runtime

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

type ImageDigestResolver interface {
	Resolve(ctx context.Context, image string) (ImageDigestResult, error)
}

type ContainerOperationQueue interface {
	Execute(ctx context.Context, serverID string, run func(context.Context) error) error
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
	rows, err := s.db.QueryContext(ctx, `SELECT `+applicationColumns+` FROM applications ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	apps := []Application{}
	for rows.Next() {
		app, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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

func (s *Service) Get(ctx context.Context, appID string) (Application, error) {
	app, err := s.getApplication(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	return s.withImageUpdateStatus(ctx, app)
}

func (s *Service) getApplication(ctx context.Context, appID string) (Application, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+applicationColumns+` FROM applications WHERE id=?`, appID)
	app, err := scanApplication(row)
	if err == sql.ErrNoRows {
		return Application{}, panelerr.NotFound("application")
	}
	return app, err
}

func (s *Service) Create(ctx context.Context, in SaveInput) (Application, error) {
	return s.createWithFiles(ctx, in, nil)
}

func (s *Service) createWithFiles(ctx context.Context, in SaveInput, files []ApplicationFile) (Application, error) {
	appID := id.New("app")
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
		Name:              in.Name,
		Enabled:           in.Enabled,
		SpecYAML:          in.SpecYAML,
		Variables:         prepared.variables,
		ResolvedVariables: prepared.resolvedVariables,
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
		if err := s.insertRevision(ctx, app, prepared.job); err != nil {
			return Application{}, err
		}
		if app.Enabled {
			taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
			if err != nil {
				return Application{}, err
			}
			if err := s.deployRuntimeSpec(ctx, taskID, app, prepared.job); err != nil {
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
		taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
		if err != nil {
			return Application{}, err
		}
		if err := s.deployRuntimeSpec(ctx, taskID, app, prepared.job); err != nil {
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
	current, err := s.Get(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	if files != nil {
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
	app.Variables = prepared.variables
	app.ResolvedVariables = prepared.resolvedVariables
	app.PersistentPath = prepared.persistentPath
	app.DeploymentMode = prepared.deploymentMode
	app.DeploymentServers = prepared.deploymentServers
	app.ReverseProxy = prepared.reverseProxy
	app.Generation = generation
	app.SpecHash = prepared.hash
	app.JobID = prepared.job.ID
	app.Namespace = s.currentConfig().Namespace
	app.UpdatedAt = time.Now().UTC()
	shouldDeploy := app.Enabled && (!current.Enabled || prepared.hash != current.SpecHash)
	shouldStop := current.Enabled && !app.Enabled
	if shouldDeploy {
		job, issues, err := s.renderApplicationWithFiles(ctx, app, files)
		if err != nil {
			return Application{}, err
		}
		if len(issues) > 0 {
			return Application{}, panelerr.Validation("application_invalid", issues[0].Message)
		}
		prepared.job = job
	}
	if files == nil {
		if err := s.updateApplication(ctx, app); err != nil {
			return Application{}, applicationSaveError(err)
		}
		if prepared.hash != current.SpecHash {
			if err := s.insertRevision(ctx, app, prepared.job); err != nil {
				return Application{}, err
			}
		}
		if shouldDeploy {
			taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
			if err != nil {
				return Application{}, err
			}
			if err := s.deployRuntimeSpec(ctx, taskID, app, prepared.job); err != nil {
				return Application{}, err
			}
		}
		if shouldStop {
			taskID, err := s.recordRunningTask(ctx, TaskTypeStop, app.ID, "Stopping application "+app.Name)
			if err != nil {
				return Application{}, err
			}
			if err := s.stopRuntimeInstances(ctx, taskID, app.ID, false); err != nil {
				return Application{}, err
			}
		}
		if err := s.reconcileReverseProxy(ctx); err != nil {
			return Application{}, err
		}
		return s.Get(ctx, app.ID)
	}
	if err := s.commitApplicationState(ctx, app, files, prepared.job, false, prepared.hash != current.SpecHash); err != nil {
		return Application{}, applicationSaveError(err)
	}
	if shouldDeploy {
		taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
		if err != nil {
			return Application{}, err
		}
		if err := s.deployRuntimeSpec(ctx, taskID, app, prepared.job); err != nil {
			return Application{}, err
		}
	}
	if shouldStop {
		taskID, err := s.recordRunningTask(ctx, TaskTypeStop, app.ID, "Stopping application "+app.Name)
		if err != nil {
			return Application{}, err
		}
		if err := s.stopRuntimeInstances(ctx, taskID, app.ID, false); err != nil {
			return Application{}, err
		}
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func (s *Service) Delete(ctx context.Context, appID string) error {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return err
	}
	if app.Enabled {
		return panelerr.Conflict("application_enabled", "Disable the application before deleting it")
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM applications WHERE id=?`, appID)
	if err != nil {
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
		return PlanResult{}, panelerr.Validation("application_invalid", issues[0].Message)
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
		if err := s.updateApplication(ctx, app); err != nil {
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
	if err := s.updateApplication(ctx, app); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func (s *Service) UpdateImage(ctx context.Context, appID string) (OperationResult, error) {
	app, job, err := s.prepareImageUpdate(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordRunningTask(ctx, TaskTypeImageUpdate, app.ID, "Updating image for "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.deployRuntimeSpec(ctx, taskID, app, job); err != nil {
		return OperationResult{}, err
	}
	if err := s.markApplicationImageTargetsCurrent(ctx, app); err != nil {
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	app, _ = s.withImageUpdateStatus(ctx, app)
	return OperationResult{TaskID: taskID, EvalID: app.LastEvalID, Application: app}, nil
}

func (s *Service) RunImageUpdateTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	app, job, err := s.prepareImageUpdate(ctx, appID)
	if err != nil {
		return err
	}
	if err := s.deployRuntimeSpec(ctx, task.ID, app, job); err != nil {
		return err
	}
	if err := s.markApplicationImageTargetsCurrent(ctx, app); err != nil {
		return err
	}
	return s.reconcileReverseProxy(ctx)
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
		_ = s.updateApplication(ctx, app)
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
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_invalid", issues[0].Message)
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
	if err := s.insertRevision(ctx, app, job); err != nil {
		return Application{}, appruntime.Spec{}, err
	}
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
			if _, err := s.db.ExecContext(ctx, `INSERT INTO image_updates(server_id,reference,local_digest,latest_digest,update_available,last_error,checked_at)
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
	app, job, err := s.prepareDeploy(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	if s.tasks == nil {
		if err := s.runDeployTask(ctx, "", app, job); err != nil {
			return OperationResult{}, err
		}
		return OperationResult{Application: app}, nil
	}
	task, created, err := s.recordRunningTaskObject(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	if created {
		go func() {
			defer s.tasks.FinishExecution(task.ID)
			runCtx := s.tasks.ExecutionContext(task.ID)
			if err := s.runDeployTask(runCtx, task.ID, app, job); err != nil {
				_ = s.tasks.Fail(context.Background(), task.ID, err)
			}
		}()
	}
	result := OperationResult{TaskID: task.ID, Application: app}
	if runtime, err := s.Runtime(ctx, app.ID); err == nil {
		result.ApplicationRuntime = &runtime
		if created {
			_ = s.tasks.AppendLog(ctx, task.ID, "system", "Current application runtime status: "+runtime.Status)
		}
	}
	return result, nil
}

func (s *Service) RunDeployTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	app, job, err := s.prepareDeploy(ctx, appID)
	if err != nil {
		return err
	}
	return s.runDeployTask(ctx, task.ID, app, job)
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
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	if len(targets) == 0 {
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	app.Enabled = true
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, app); err != nil {
		return Application{}, appruntime.Spec{}, err
	}
	return app, job, nil
}

func (s *Service) runDeployTask(ctx context.Context, taskID string, app Application, job appruntime.Spec) error {
	if err := s.deployRuntimeSpec(ctx, taskID, app, job); err != nil {
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
		return OperationResult{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	if runtimeSpecUsesExternalMounts(currentJob) {
		return OperationResult{}, panelerr.Conflict("application_migration_mounts_not_supported", "Applications with host paths or Docker volumes cannot use lossless migration")
	}
	input := SaveInput{
		Name:              app.Name,
		Enabled:           app.Enabled,
		SpecYAML:          app.SpecYAML,
		Variables:         app.Variables,
		PersistentPath:    app.PersistentPath,
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
	migrated.Variables = prepared.variables
	migrated.ResolvedVariables = prepared.resolvedVariables
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
		return OperationResult{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	if err := s.updateApplication(ctx, migrated); err != nil {
		return OperationResult{}, applicationSaveError(err)
	}
	if prepared.hash != app.SpecHash {
		if err := s.insertRevision(ctx, migrated, job); err != nil {
			return OperationResult{}, err
		}
	}
	taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, app.ID, "Migrating application "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.deployRuntimeSpec(ctx, taskID, migrated, job); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return OperationResult{}, err
	}
	if err := s.deleteRuntimeInstanceForServer(ctx, app.ID, sourceServerID); err != nil {
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	out, err := s.Get(ctx, app.ID)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskID, Application: out}, nil
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
		taskID, err := s.recordRunningTask(ctx, TaskTypeRefresh, refreshed.ID, "Refreshing application "+refreshed.Name)
		if err != nil {
			return redeployed, err
		}
		if err := s.deployRuntimeSpec(ctx, taskID, refreshed, spec); err != nil {
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
	if err := s.deployRuntimeSpec(ctx, task.ID, refreshed, spec); err != nil {
		return err
	}
	return s.reconcileReverseProxy(ctx)
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
		return false, Application{}, appruntime.Spec{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	targets, err := s.deploymentTargets(ctx, refreshed)
	if err != nil {
		return false, Application{}, appruntime.Spec{}, err
	}
	if len(targets) == 0 {
		return false, Application{}, appruntime.Spec{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	refreshed.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, refreshed); err != nil {
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
		refreshed, spec, err := s.prepareEnabledApplicationRedeploy(ctx, app)
		if err != nil {
			return redeployed, err
		}
		taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, refreshed.ID, "Deploying application "+refreshed.Name)
		if err != nil {
			return redeployed, err
		}
		if err := s.runDeployTask(ctx, taskID, refreshed, spec); err != nil {
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
		return Application{}, appruntime.Spec{}, panelerr.Validation("application_invalid", issues[0].Message)
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
	if err := s.ensureRuntimeInstancesReady(ctx, app.ID); err != nil {
		return OperationResult{}, err
	}
	app.Enabled = false
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, app); err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordRunningTaskWithParams(ctx, TaskTypeStop, app.ID, "Stopping application "+app.Name, stopTaskParamsJSON(purge))
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.stopRuntimeInstances(ctx, taskID, app.ID, purge); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskID, Application: app}, nil
}

func (s *Service) RunStopTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	if err := s.ensureRuntimeInstancesReady(ctx, appID); err != nil {
		return err
	}
	purge, err := stopTaskPurge(task.ParamsJSON)
	if err != nil {
		return err
	}
	return s.stopRuntimeInstances(ctx, task.ID, appID, purge)
}

func (s *Service) Restart(ctx context.Context, appID string) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.ensureRuntimeInstancesReady(ctx, app.ID); err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordRunningTask(ctx, TaskTypeRestart, appID, "Restarting application")
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.restartRuntimeInstances(ctx, taskID, app.ID); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return OperationResult{}, err
	}
	runtime, err := s.Runtime(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskID, ApplicationRuntime: &runtime}, nil
}

func (s *Service) RunRestartTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	appID := firstNonEmpty(task.ResourceID, task.ServerID, task.NodeID)
	if appID == "" {
		return panelerr.Validation("application_task_resource_required", "Application task resource is required")
	}
	if err := s.ensureRuntimeInstancesReady(ctx, appID); err != nil {
		return err
	}
	return s.restartRuntimeInstances(ctx, task.ID, appID)
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
	row := s.db.QueryRowContext(ctx, `SELECT id,application_id,type,status,task_id,generation,spec_hash,trigger,error,created_at,started_at,finished_at,updated_at
		FROM application_lifecycle_operations WHERE application_id=? ORDER BY created_at DESC, id DESC LIMIT 1`, appID)
	op, err := scanLifecycleOperation(row)
	if err == sql.ErrNoRows {
		return LifecycleOperation{}, panelerr.NotFound("application_lifecycle_operation")
	}
	if err != nil {
		return LifecycleOperation{}, err
	}
	targets, err := s.lifecycleTargets(ctx, op.ID)
	if err != nil {
		return LifecycleOperation{}, err
	}
	op.Targets = targets
	return op, nil
}

func (s *Service) lifecycleTargets(ctx context.Context, operationID string) ([]LifecycleTarget, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,operation_id,application_id,server_id,status,desired_state,instance_id,container_name,container_id,stage,error,created_at,started_at,finished_at,updated_at
		FROM application_lifecycle_targets WHERE operation_id=? ORDER BY server_id ASC`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LifecycleTarget{}
	for rows.Next() {
		target, err := scanLifecycleTarget(rows)
		if err != nil {
			return nil, err
		}
		if s.servers != nil {
			if srv, err := s.servers.Get(ctx, target.ServerID); err == nil {
				target.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
			}
		}
		out = append(out, target)
	}
	return out, rows.Err()
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
	default:
		return appruntime.StatusPending
	}
}

func (s *Service) withRuntimeSummary(ctx context.Context, app Application) (Application, error) {
	runtime, err := s.Runtime(ctx, app.ID)
	if err != nil {
		return Application{}, err
	}
	app.RuntimeStatus = runtime.Status
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
	if err != nil {
		return OperationResult{}, err
	}
	srv, err := s.servers.Get(ctx, instance.ServerID)
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
	return s.Restart(ctx, app.ID)
}

type preparedApplication struct {
	spec              appspec.Spec
	variables         map[string]string
	resolvedVariables map[string]any
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
	variables := in.Variables
	if variables == nil {
		variables = map[string]string{}
	}
	persistentPath, err := normalizePersistentPath(in.PersistentPath)
	if err != nil {
		return preparedApplication{}, err
	}
	appContext := Application{ID: appID, Name: in.Name, Generation: generation, Namespace: s.currentConfig().Namespace, DeploymentMode: in.DeploymentMode}
	data, err := s.templateData(ctx, appContext, variables, files, nil)
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
	if persistentPath == "" && specUsesPersistentMount(spec) {
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
	hash, err := applicationHash(spec, variables, persistentPath, deploymentMode, deploymentServers, resolvedReverseProxy, files, data)
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
		return preparedApplication{}, panelerr.Validation("application_invalid", renderIssues[0].Message)
	}
	return preparedApplication{spec: spec, variables: variables, resolvedVariables: data, persistentPath: persistentPath, deploymentMode: deploymentMode, deploymentServers: deploymentServers, reverseProxy: reverseProxy, hash: hash, job: job}, nil
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
	data, err := s.templateData(ctx, app, app.Variables, files, nil)
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

func (s *Service) renderSpecYAML(ctx context.Context, source string, variables map[string]string) (string, error) {
	data, err := s.templateData(ctx, Application{}, variables, nil, nil)
	if err != nil {
		return "", err
	}
	return s.renderTemplate(ctx, source, data)
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
	data, err := s.templateData(ctx, app, app.Variables, files, nil)
	if err != nil {
		return ImageDigestResult{}, checkedAt, err
	}
	renderedYAML, err := s.renderTemplate(ctx, app.SpecYAML, data)
	if err != nil {
		return ImageDigestResult{}, checkedAt, err
	}
	spec, issues := appspec.DecodeYAML(renderedYAML)
	if len(issues) > 0 {
		return ImageDigestResult{}, checkedAt, panelerr.Validation("application_invalid", issues[0].Message)
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

func (s *Service) templateData(ctx context.Context, app Application, variables map[string]string, files []ApplicationFile, target *server.Server) (map[string]any, error) {
	data := map[string]any{}
	varMap := map[string]any{}
	for key, value := range variables {
		data[key] = value
		varMap[key] = value
	}
	data["vars"] = varMap
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
	if s.runtimeClient == nil {
		return panelerr.Validation("agent_runtime_unavailable", "Agent runtime client is unavailable")
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	operation, err := s.createLifecycleOperation(ctx, app, spec, taskID, LifecycleTypeDeploy, targets)
	if err != nil {
		return err
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Advance(ctx, taskID, "deploying", "deploying application instances")
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
		previousContainerName := ""
		if previous, err := s.runtimeInstanceForServer(ctx, app.ID, target.ID); err == nil {
			previousContainerName = previous.ContainerName
		}
		if err := s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Stage: "prepare_instance", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName, Error: err.Error(), Finished: true})
			return err
		}
		_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "write_files", InstanceID: instanceSpec.InstanceID, ContainerName: instanceSpec.ContainerName})
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "deploying "+instanceSpec.ContainerName+" on "+targetName)
		}
		var result agentcontract.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, target.ID, func(runCtx context.Context) error {
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "write_files"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "write files", func(context.Context) error {
				return s.runtimeClient.RuntimeWriteFiles(runCtx, baseURL, agentcontract.RuntimeWriteFilesRequest{Spec: instanceSpec})
			}); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "pull_image"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "pull image", func(context.Context) error {
				return s.runtimeClient.DockerImagePull(runCtx, baseURL, instanceSpec.Image)
			}); err != nil {
				return err
			}
			if previousContainerName != "" && previousContainerName != instanceSpec.ContainerName {
				_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "remove_previous_container"})
				if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "remove previous container", func(context.Context) error {
					return s.runtimeClient.DockerContainerDelete(runCtx, baseURL, previousContainerName)
				}); err != nil {
					return err
				}
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "remove_target_container"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "remove target container", func(context.Context) error {
				return s.runtimeClient.DockerContainerDelete(runCtx, baseURL, instanceSpec.ContainerName)
			}); err != nil {
				return err
			}
			var created agentcontract.RuntimeCreateContainerResponse
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "create_container"})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "create container", func(context.Context) error {
				var createErr error
				created, createErr = s.runtimeClient.RuntimeCreateContainer(runCtx, baseURL, agentcontract.RuntimeCreateContainerRequest{ServerID: target.ID, Spec: instanceSpec})
				return createErr
			}); err != nil {
				return err
			}
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusDeploying, Stage: "start_container", ContainerID: created.ContainerID})
			if err := s.runRuntimeDeployStep(runCtx, taskID, targetName, "start container", func(context.Context) error {
				return s.runtimeClient.DockerContainerAction(runCtx, baseURL, firstNonEmpty(created.ContainerID, instanceSpec.ContainerName), "start")
			}); err != nil {
				return err
			}
			var status agentcontract.RuntimeStatusResponse
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
			return nil
		})
		if err != nil {
			_ = s.handleAgentError(ctx, target, err)
			_ = s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusFailed, "", err.Error())
			failures = append(failures, runtimeDeploymentFailure{targetName: targetName, err: err})
			_ = s.updateLifecycleTarget(ctx, targetID, lifecycleTargetUpdate{Status: LifecycleTargetStatusFailed, Error: err.Error(), Finished: true})
			if s.tasks != nil && taskID != "" {
				_ = s.tasks.AppendLog(ctx, taskID, "stderr", "deploying on "+targetName+" failed: "+err.Error())
			}
			continue
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
		status := LifecycleStatusFailed
		if len(failures) < len(targets) {
			status = LifecycleStatusPartiallyDeployed
		}
		_ = s.finishLifecycleOperation(ctx, operation.ID, status, err)
		return err
	}
	if err := s.finishLifecycleOperation(ctx, operation.ID, LifecycleStatusDeployed, nil); err != nil {
		return err
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application deployed")
	}
	return nil
}

type lifecycleTargetUpdate struct {
	Status        string
	Stage         string
	InstanceID    string
	ContainerName string
	ContainerID   string
	Error         string
	Started       bool
	Finished      bool
}

func (s *Service) createLifecycleOperation(ctx context.Context, app Application, spec appruntime.Spec, taskID, opType string, targets []server.Server) (LifecycleOperation, error) {
	now := time.Now().UTC()
	operation := LifecycleOperation{
		ID:            id.New("alop"),
		ApplicationID: app.ID,
		Type:          opType,
		Status:        LifecycleStatusDeploying,
		TaskID:        taskID,
		Generation:    spec.Generation,
		SpecHash:      spec.SpecHash,
		Trigger:       "system",
		CreatedAt:     now,
		StartedAt:     &now,
		UpdatedAt:     now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_lifecycle_operations(id,application_id,type,status,task_id,generation,spec_hash,trigger,error,created_at,started_at,finished_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		operation.ID, operation.ApplicationID, operation.Type, operation.Status, operation.TaskID, operation.Generation, operation.SpecHash, operation.Trigger, operation.Error, formatTime(operation.CreatedAt), nullableTime(operation.StartedAt), nil, formatTime(operation.UpdatedAt))
	if err != nil {
		return LifecycleOperation{}, err
	}
	for _, target := range targets {
		targetID := lifecycleTargetID(operation.ID, target.ID)
		instanceID := runtimeInstanceID(app.ID, target.ID)
		containerName := runtimeContainerName(app)
		_, err := s.db.ExecContext(ctx, `INSERT INTO application_lifecycle_targets(id,operation_id,application_id,server_id,status,desired_state,instance_id,container_name,container_id,stage,error,created_at,started_at,finished_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			targetID, operation.ID, app.ID, target.ID, LifecycleTargetStatusPending, appruntime.DesiredRunning, instanceID, containerName, "", "", "", formatTime(now), nil, nil, formatTime(now))
		if err != nil {
			return LifecycleOperation{}, err
		}
	}
	return operation, nil
}

func (s *Service) updateLifecycleTarget(ctx context.Context, targetID string, in lifecycleTargetUpdate) error {
	now := formatTime(time.Now().UTC())
	updates := []string{"updated_at=?"}
	args := []any{now}
	if in.Status != "" {
		updates = append(updates, "status=?")
		args = append(args, in.Status)
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
	if in.Started {
		updates = append(updates, "started_at=COALESCE(started_at, ?)")
		args = append(args, now)
	}
	if in.Finished {
		updates = append(updates, "finished_at=?")
		args = append(args, now)
	}
	args = append(args, targetID)
	_, err := s.db.ExecContext(ctx, `UPDATE application_lifecycle_targets SET `+strings.Join(updates, ",")+` WHERE id=?`, args...)
	return err
}

func (s *Service) finishLifecycleOperation(ctx context.Context, operationID, status string, cause error) error {
	now := formatTime(time.Now().UTC())
	errText := ""
	if cause != nil {
		errText = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE application_lifecycle_operations SET status=?, error=?, finished_at=?, updated_at=? WHERE id=?`, status, errText, now, now, operationID)
	return err
}

func lifecycleTargetID(operationID, serverID string) string {
	return strings.TrimSpace(operationID) + "-" + sanitizeRuntimeName(serverID)
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

func (s *Service) stopRuntimeInstances(ctx context.Context, taskID, appID string, purge bool) error {
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
		baseURL, _ := agentURLFromServer(srv)
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "stopping "+instance.ContainerName+" on "+firstNonEmpty(srv.Name, srv.ID, srv.Host))
		}
		var result agentcontract.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
			var runErr error
			result, runErr = s.runtimeClient.RuntimeStop(runCtx, baseURL, agentcontract.RuntimeStopRequest{InstanceID: instance.ID, ContainerName: instance.ContainerName, Purge: purge})
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

func (s *Service) restartRuntimeInstances(ctx context.Context, taskID, appID string) error {
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
		baseURL, _ := agentURLFromServer(srv)
		var result agentcontract.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
			var runErr error
			result, runErr = s.runtimeClient.RuntimeRestart(runCtx, baseURL, agentcontract.RuntimeRestartRequest{InstanceID: instance.ID, ContainerName: instance.ContainerName})
			return runErr
		})
		if err != nil {
			_ = s.handleAgentError(ctx, srv, err)
			_ = s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredRunning, appruntime.StatusFailed, "", err.Error())
			return runtimeOperationError(err)
		}
		if err := s.markRuntimeInstance(ctx, instance.ID, appruntime.DesiredRunning, result.Status, result.ContainerID, ""); err != nil {
			return err
		}
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application restarted")
	}
	return nil
}

func (s *Service) executeContainerOperation(ctx context.Context, serverID string, run func(context.Context) error) error {
	if s.operationQueue == nil {
		return run(ctx)
	}
	return s.operationQueue.Execute(ctx, serverID, run)
}

func (s *Service) insertApplication(ctx context.Context, app Application) error {
	variables, err := json.Marshal(app.Variables)
	if err != nil {
		return err
	}
	resolved, err := json.Marshal(app.ResolvedVariables)
	if err != nil {
		return err
	}
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO applications(id,name,enabled,spec_yaml,variables_json,resolved_variables_json,persistent_path,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,image_reference,image_digest,image_latest_digest,image_checked_at,image_update_available,image_last_error,job_id,namespace,last_eval_id,last_deployment_id,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		app.ID, app.Name, boolInt(app.Enabled), app.SpecYAML, string(variables), string(resolved), app.PersistentPath, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.CreatedAt), formatTime(app.UpdatedAt))
	return err
}

func (s *Service) updateApplication(ctx context.Context, app Application) error {
	variables, err := json.Marshal(app.Variables)
	if err != nil {
		return err
	}
	resolved, err := json.Marshal(app.ResolvedVariables)
	if err != nil {
		return err
	}
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE applications SET name=?,enabled=?,spec_yaml=?,variables_json=?,resolved_variables_json=?,persistent_path=?,deployment_mode=?,deployment_server_ids_json=?,reverse_proxy_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		app.Name, boolInt(app.Enabled), app.SpecYAML, string(variables), string(resolved), app.PersistentPath, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
	return err
}

func (s *Service) commitApplicationState(ctx context.Context, app Application, files []ApplicationFile, job appruntime.Spec, insertApp bool, insertRevision bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if insertApp {
		if err := s.insertApplicationWithExec(ctx, tx, app); err != nil {
			return err
		}
	} else if err := s.updateApplicationWithExec(ctx, tx, app); err != nil {
		return err
	}
	if err := replaceApplicationFiles(ctx, tx, app.ID, files); err != nil {
		return err
	}
	if insertRevision {
		if err := insertRevisionWithExec(ctx, tx, app, job); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) insertApplicationWithExec(ctx context.Context, exec sqlExec, app Application) error {
	variables, err := json.Marshal(app.Variables)
	if err != nil {
		return err
	}
	resolved, err := json.Marshal(app.ResolvedVariables)
	if err != nil {
		return err
	}
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO applications(id,name,enabled,spec_yaml,variables_json,resolved_variables_json,persistent_path,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,image_reference,image_digest,image_latest_digest,image_checked_at,image_update_available,image_last_error,job_id,namespace,last_eval_id,last_deployment_id,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		app.ID, app.Name, boolInt(app.Enabled), app.SpecYAML, string(variables), string(resolved), app.PersistentPath, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.CreatedAt), formatTime(app.UpdatedAt))
	return err
}

func (s *Service) updateApplicationWithExec(ctx context.Context, exec sqlExec, app Application) error {
	variables, err := json.Marshal(app.Variables)
	if err != nil {
		return err
	}
	resolved, err := json.Marshal(app.ResolvedVariables)
	if err != nil {
		return err
	}
	deploymentServers, err := json.Marshal(app.DeploymentServers)
	if err != nil {
		return err
	}
	reverseProxy, err := json.Marshal(app.ReverseProxy)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `UPDATE applications SET name=?,enabled=?,spec_yaml=?,variables_json=?,resolved_variables_json=?,persistent_path=?,deployment_mode=?,deployment_server_ids_json=?,reverse_proxy_json=?,generation=?,spec_hash=?,image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		app.Name, boolInt(app.Enabled), app.SpecYAML, string(variables), string(resolved), app.PersistentPath, app.DeploymentMode, string(deploymentServers), string(reverseProxy), app.Generation, app.SpecHash, app.ImageReference, app.ImageDigest, app.ImageLatestDigest, nullableTime(app.ImageCheckedAt), boolInt(app.ImageUpdateAvailable), app.ImageLastError, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
	return err
}

func (s *Service) insertRevision(ctx context.Context, app Application, job appruntime.Spec) error {
	return insertRevisionWithExec(ctx, s.db, app, job)
}

func insertRevisionWithExec(ctx context.Context, exec sqlExec, app Application, job appruntime.Spec) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT OR IGNORE INTO application_revisions(id,application_id,generation,spec_hash,spec_yaml,job_json,created_at) VALUES(?,?,?,?,?,?,?)`,
		id.New("arev"), app.ID, app.Generation, app.SpecHash, app.SpecYAML, string(raw), formatTime(time.Now().UTC()))
	return err
}

func replaceApplicationFiles(ctx context.Context, exec sqlExec, appID string, files []ApplicationFile) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM application_files WHERE application_id=?`, appID); err != nil {
		return err
	}
	for _, file := range files {
		if _, err := exec.ExecContext(ctx, `INSERT INTO application_files(id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			file.ID, appID, file.Path, file.Kind, file.ContentType, file.Size, file.SHA256, file.Content, formatTime(file.CreatedAt), formatTime(file.UpdatedAt)); err != nil {
			return err
		}
	}
	return nil
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
	switch issue.Field {
	case "command":
		return panelerr.Validation("application_command_invalid", issue.Message)
	case "specYaml":
		return panelerr.Validation("application_spec_yaml_invalid", issue.Message)
	default:
		return panelerr.Validation("application_invalid", issue.Message)
	}
}

func isApplicationNameConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed: applications.name") || strings.Contains(msg, "constraint failed: applications.name")
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

func stopTaskParamsJSON(purge bool) string {
	if !purge {
		return "{}"
	}
	return `{"purge":true}`
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
	filesByPath := map[string]ApplicationFile{}
	for _, file := range files {
		filesByPath[file.Path] = file
	}
	data, err := s.templateData(ctx, app, app.Variables, files, &srv)
	if err != nil {
		return nil, err
	}
	out := append([]appruntime.ManagedFile(nil), managed...)
	for i, item := range out {
		file, ok := filesByPath[item.Path]
		if !ok || file.Kind != "template" {
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
	raw, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	now := formatTime(time.Now().UTC())
	_, err = s.db.ExecContext(ctx, `INSERT INTO application_instances(id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(application_id,server_id) DO UPDATE SET
			id=excluded.id,
			container_name=excluded.container_name,
			container_id=excluded.container_id,
			desired_state=excluded.desired_state,
			status=excluded.status,
			runtime_spec_json=excluded.runtime_spec_json,
			last_deployed_generation=excluded.last_deployed_generation,
			last_error=excluded.last_error,
			updated_at=excluded.updated_at`,
		spec.InstanceID, appID, serverID, spec.ContainerName, containerID, desired, status, string(raw), spec.Generation, lastErr, now, now)
	return err
}

func (s *Service) markRuntimeInstance(ctx context.Context, instanceID, desired, status, containerID, lastErr string) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `UPDATE application_instances SET desired_state=?,status=?,container_id=COALESCE(NULLIF(?, ''), container_id),last_error=?,updated_at=? WHERE id=?`,
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
	if _, err := s.db.ExecContext(ctx, `DELETE FROM application_instances WHERE application_id=? AND server_id=?`, appID, serverID); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM application_reconcile_states WHERE instance_id=?`, instance.ID)
	return err
}

func (s *Service) runtimeInstances(ctx context.Context, appID string) ([]appruntime.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at FROM application_instances WHERE application_id=? ORDER BY server_id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []appruntime.Instance{}
	for rows.Next() {
		instance, err := scanRuntimeInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, instance)
	}
	return out, rows.Err()
}

func (s *Service) runtimeInstance(ctx context.Context, appID, instanceID string) (appruntime.Instance, error) {
	if strings.TrimSpace(instanceID) == "" {
		return appruntime.Instance{}, panelerr.Validation("runtime_instance_required", "Runtime instance is required")
	}
	row := s.db.QueryRowContext(ctx, `SELECT id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at FROM application_instances WHERE application_id=? AND id=?`, appID, instanceID)
	instance, err := scanRuntimeInstance(row)
	if err == sql.ErrNoRows {
		return appruntime.Instance{}, panelerr.NotFound("application_instance")
	}
	return instance, err
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

func (s *Service) runtimeInstanceForServer(ctx context.Context, appID, serverID string) (appruntime.Instance, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,application_id,server_id,container_name,container_id,desired_state,status,runtime_spec_json,last_deployed_generation,last_error,created_at,updated_at FROM application_instances WHERE application_id=? AND server_id=?`, appID, serverID)
	instance, err := scanRuntimeInstance(row)
	if err == sql.ErrNoRows {
		return appruntime.Instance{}, panelerr.NotFound("application_instance")
	}
	return instance, err
}

func scanRuntimeInstance(row appScanner) (appruntime.Instance, error) {
	var instance appruntime.Instance
	var rawSpec, created, updated string
	if err := row.Scan(&instance.ID, &instance.ApplicationID, &instance.ServerID, &instance.ContainerName, &instance.ContainerID, &instance.DesiredState, &instance.Status, &rawSpec, &instance.LastDeployedGeneration, &instance.LastError, &created, &updated); err != nil {
		return appruntime.Instance{}, err
	}
	_ = json.Unmarshal([]byte(rawSpec), &instance.RuntimeSpec)
	instance.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	instance.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return instance, nil
}

func scanLifecycleOperation(row appScanner) (LifecycleOperation, error) {
	var op LifecycleOperation
	var created, updated string
	var started, finished sql.NullString
	if err := row.Scan(&op.ID, &op.ApplicationID, &op.Type, &op.Status, &op.TaskID, &op.Generation, &op.SpecHash, &op.Trigger, &op.Error, &created, &started, &finished, &updated); err != nil {
		return LifecycleOperation{}, err
	}
	op.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	op.StartedAt = parseOptionalTime(started)
	op.FinishedAt = parseOptionalTime(finished)
	op.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return op, nil
}

func scanLifecycleTarget(row appScanner) (LifecycleTarget, error) {
	var target LifecycleTarget
	var created, updated string
	var started, finished sql.NullString
	if err := row.Scan(&target.ID, &target.OperationID, &target.ApplicationID, &target.ServerID, &target.Status, &target.DesiredState, &target.InstanceID, &target.ContainerName, &target.ContainerID, &target.Stage, &target.Error, &created, &started, &finished, &updated); err != nil {
		return LifecycleTarget{}, err
	}
	target.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	target.StartedAt = parseOptionalTime(started)
	target.FinishedAt = parseOptionalTime(finished)
	target.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return target, nil
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
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
		var item cachedImageUpdate
		var available int
		var checked string
		err := s.db.QueryRowContext(ctx, `SELECT local_digest,latest_digest,update_available,last_error,checked_at FROM image_updates WHERE server_id=? AND reference=?`, serverID, reference).
			Scan(&item.LocalDigest, &item.LatestDigest, &available, &item.LastError, &checked)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return cachedImageUpdate{}, false, err
		}
		item.UpdateAvailable = available == 1
		if parsed, err := time.Parse(time.RFC3339Nano, checked); err == nil {
			item.CheckedAt = &parsed
		}
		return item, true, nil
	}
	return cachedImageUpdate{}, false, nil
}

func (s *Service) refreshInstanceStatuses(ctx context.Context, instances []appruntime.Instance) []appruntime.InstanceStatus {
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
		if s.runtimeClient != nil && s.servers != nil {
			if srv, err := s.servers.Get(ctx, instance.ServerID); err == nil {
				status.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
				if ensureAgentRuntimeReady(srv) == nil {
					baseURL, _ := agentURLFromServer(srv)
					if remote, err := s.runtimeClient.RuntimeStatus(ctx, baseURL, instance.ID, instance.ContainerName); err == nil {
						status = remote.InstanceStatus
						status.ServerID = instance.ServerID
						status.ServerName = strings.TrimSpace(firstNonEmpty(srv.Name, srv.ID))
						status.DesiredState = instance.DesiredState
						_ = s.markRuntimeInstance(ctx, instance.ID, instance.DesiredState, status.Status, status.ContainerID, status.LastError)
					} else {
						_ = s.handleAgentError(ctx, srv, err)
					}
				}
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
	for _, instance := range instances {
		switch instance.Status {
		case appruntime.StatusFailed:
			return appruntime.StatusFailed
		case appruntime.StatusRunning:
		default:
			allRunning = false
		}
	}
	if allRunning {
		return appruntime.StatusRunning
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
		domain := strings.TrimSpace(rule.Domain)
		if domain == "" {
			continue
		}
		if rule.TargetPort <= 0 || rule.TargetPort > 65535 {
			return nil, panelerr.Validation("application_reverse_proxy_target_port_invalid", "reverse proxy target port must be between 1 and 65535")
		}
		paths := make([]ReverseProxyPath, 0, len(rule.Paths))
		for _, item := range rule.Paths {
			proxyPath := strings.TrimSpace(item.Path)
			if proxyPath == "" {
				proxyPath = "/"
			}
			if !strings.HasPrefix(proxyPath, "/") {
				return nil, panelerr.Validation("application_reverse_proxy_path_invalid", "reverse proxy path must start with /")
			}
			paths = append(paths, ReverseProxyPath{Path: proxyPath, WebSocket: item.WebSocket})
		}
		if len(paths) == 0 {
			paths = append(paths, ReverseProxyPath{Path: "/"})
		}
		out = append(out, ReverseProxyRule{
			Domain:     domain,
			TargetPort: rule.TargetPort,
			Paths:      paths,
		})
	}
	return out, nil
}

func (s *Service) renderReverseProxyRules(ctx context.Context, rules []ReverseProxyRule, data map[string]any) ([]ReverseProxyRule, error) {
	out := make([]ReverseProxyRule, 0, len(rules))
	for _, rule := range rules {
		domain, err := s.renderTemplate(ctx, strings.TrimSpace(rule.Domain), data)
		if err != nil {
			return nil, err
		}
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if !validNginxToken(domain) {
			return nil, panelerr.Validation("application_reverse_proxy_domain_invalid", "reverse proxy domain is invalid")
		}
		paths := make([]ReverseProxyPath, 0, len(rule.Paths))
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
			paths = append(paths, ReverseProxyPath{Path: proxyPath, WebSocket: item.WebSocket})
		}
		if len(paths) == 0 {
			paths = append(paths, ReverseProxyPath{Path: "/"})
		}
		out = append(out, ReverseProxyRule{Domain: domain, TargetPort: rule.TargetPort, Paths: paths})
	}
	return out, nil
}

func (s *Service) renderReverseProxyConfig(ctx context.Context, app Application, files []ApplicationFile) (string, string, error) {
	if len(app.ReverseProxy) == 0 {
		return "", "", nil
	}
	data, err := s.templateData(ctx, app, app.Variables, files, nil)
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
			b.WriteString("        proxy_pass http://127.0.0.1:")
			b.WriteString(strconv.Itoa(rule.TargetPort))
			b.WriteString(";\n")
			b.WriteString("        proxy_set_header Host $host;\n")
			b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
			b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
			b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
			if proxyPath.WebSocket {
				b.WriteString("        proxy_http_version 1.1;\n")
				b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
				b.WriteString("        proxy_set_header Connection $connection_upgrade;\n")
			}
			b.WriteString("    }\n")
		}
		b.WriteString("}\n")
	}
	return reverseProxyConfigName(app), b.String(), nil
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
		data, err := s.templateData(ctx, app, app.Variables, files, nil)
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
				paths = append(paths, ReverseProxyPath{Path: item.Path, WebSocket: item.WebSocket})
			}
			routes = append(routes, ReverseProxyRoute{Domain: rule.Domain, TargetPort: rule.TargetPort, Paths: paths})
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
		return panelerr.Validation("application_invalid", issues[0].Message)
	}
	current.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, current); err != nil {
		return err
	}
	taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, current.ID, "Deploying application "+current.Name)
	if err != nil {
		return err
	}
	if err := s.deployRuntimeSpec(ctx, taskID, current, job); err != nil {
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
	return s.proxyReconciler.ReconcileReverseProxy(ctx)
}

func (s *Service) refreshApplicationSnapshot(ctx context.Context, current Application) (Application, error) {
	in := SaveInput{
		Name:              current.Name,
		Enabled:           current.Enabled,
		SpecYAML:          current.SpecYAML,
		Variables:         current.Variables,
		PersistentPath:    current.PersistentPath,
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
	app.Variables = prepared.variables
	app.ResolvedVariables = prepared.resolvedVariables
	app.PersistentPath = prepared.persistentPath
	app.DeploymentMode = prepared.deploymentMode
	app.DeploymentServers = prepared.deploymentServers
	app.ReverseProxy = prepared.reverseProxy
	app.Generation = generation
	app.SpecHash = prepared.hash
	app.JobID = prepared.job.ID
	app.Namespace = s.currentConfig().Namespace
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, app); err != nil {
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
		if err := s.insertRevision(ctx, app, revisionJob); err != nil {
			return Application{}, err
		}
	}
	return app, nil
}

func fileVariables(files []ApplicationFile) map[string]any {
	items := make([]map[string]any, 0, len(files))
	byPath := map[string]any{}
	for _, file := range files {
		content := string(file.Content)
		item := map[string]any{
			"path":        file.Path,
			"kind":        file.Kind,
			"contentType": file.ContentType,
			"size":        file.Size,
			"sha256":      file.SHA256,
			"content":     content,
			"base64":      base64.StdEncoding.EncodeToString(file.Content),
		}
		items = append(items, item)
		byPath[file.Path] = item
	}
	return map[string]any{"items": items, "byPath": byPath}
}

func applicationHash(spec appspec.Spec, variables map[string]string, persistentPath string, deploymentMode string, deploymentServers []string, reverseProxy []ReverseProxyRule, files []ApplicationFile, resolved map[string]any) (string, error) {
	fileRefs := make([]map[string]any, 0, len(files))
	for _, file := range files {
		fileRefs = append(fileRefs, map[string]any{
			"path":   file.Path,
			"kind":   file.Kind,
			"sha256": file.SHA256,
			"size":   file.Size,
		})
	}
	payload := map[string]any{
		"spec":           appspec.Normalize(spec),
		"variables":      variables,
		"resolved":       stableResolvedVariables(resolved),
		"persistentPath": persistentPath,
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

func normalizePersistentPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	return "", panelerr.Validation("application_persistent_path_invalid", "persistent path is managed by Panel and must not be customized")
}

func normalizeApplicationFilePath(value string) (string, error) {
	return normalizeApplicationWorkspacePath(value)
}

func normalizeApplicationWorkspacePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return "", panelerr.Validation("application_file_path_invalid", "application file path must be relative to the application workspace")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", panelerr.Validation("application_file_path_invalid", "application file path must stay inside the application workspace")
	}
	return cleaned, nil
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
	variables, _ := json.MarshalIndent(app.Variables, "", "  ")
	if err := writeZipFile("variables.json", variables); err != nil {
		return PackageResult{}, err
	}
	resolved, _ := json.MarshalIndent(app.ResolvedVariables, "", "  ")
	if err := writeZipFile("resolved_variables.json", resolved); err != nil {
		return PackageResult{}, err
	}
	for _, file := range files {
		name := path.Join("files", strings.TrimPrefix(file.Path, "/"))
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

type appScanner interface{ Scan(...any) error }

type sqlExec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

const applicationColumns = `id,name,enabled,spec_yaml,variables_json,resolved_variables_json,persistent_path,deployment_mode,deployment_server_ids_json,reverse_proxy_json,generation,spec_hash,image_reference,image_digest,image_latest_digest,image_checked_at,image_update_available,image_last_error,job_id,namespace,last_eval_id,last_deployment_id,last_error,created_at,updated_at`

func scanApplication(row appScanner) (Application, error) {
	var app Application
	var enabled, imageUpdateAvailable int
	var variables, resolvedVariables, deploymentServers, reverseProxy string
	var createdAt, updatedAt string
	var imageCheckedAt sql.NullString
	if err := row.Scan(&app.ID, &app.Name, &enabled, &app.SpecYAML, &variables, &resolvedVariables, &app.PersistentPath, &app.DeploymentMode, &deploymentServers, &reverseProxy, &app.Generation, &app.SpecHash, &app.ImageReference, &app.ImageDigest, &app.ImageLatestDigest, &imageCheckedAt, &imageUpdateAvailable, &app.ImageLastError, &app.JobID, &app.Namespace, &app.LastEvalID, &app.LastDeploymentID, &app.LastError, &createdAt, &updatedAt); err != nil {
		return Application{}, err
	}
	app.Enabled = enabled == 1
	app.ImageUpdateAvailable = imageUpdateAvailable == 1
	if imageCheckedAt.Valid && imageCheckedAt.String != "" {
		checkedAt, _ := time.Parse(time.RFC3339Nano, imageCheckedAt.String)
		if !checkedAt.IsZero() {
			app.ImageCheckedAt = &checkedAt
		}
	}
	if variables != "" {
		_ = json.Unmarshal([]byte(variables), &app.Variables)
	}
	if app.Variables == nil {
		app.Variables = map[string]string{}
	}
	if resolvedVariables != "" {
		_ = json.Unmarshal([]byte(resolvedVariables), &app.ResolvedVariables)
	}
	if app.ResolvedVariables == nil {
		app.ResolvedVariables = map[string]any{}
	}
	if app.DeploymentMode == "" {
		app.DeploymentMode = DeploymentModeAll
	}
	if deploymentServers != "" {
		_ = json.Unmarshal([]byte(deploymentServers), &app.DeploymentServers)
	}
	if app.DeploymentServers == nil {
		app.DeploymentServers = []string{}
	}
	if reverseProxy != "" {
		_ = json.Unmarshal([]byte(reverseProxy), &app.ReverseProxy)
	}
	if app.ReverseProxy == nil {
		app.ReverseProxy = []ReverseProxyRule{}
	}
	app.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	app.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return app, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
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
