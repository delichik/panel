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
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"panel/internal/agent"
	"panel/internal/appruntime"
	"panel/internal/appspec"
	"panel/internal/id"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/tasks"
	"panel/internal/templatex"
)

type Config struct {
	Namespace      string
	Region         string
	Datacenter     string
	SaveSessionDir string
}

type AgentRuntimeClient interface {
	RuntimeDeploy(ctx context.Context, baseURL string, req agent.RuntimeDeployRequest) (agent.RuntimeInstanceResponse, error)
	RuntimeStop(ctx context.Context, baseURL string, req agent.RuntimeStopRequest) (agent.RuntimeInstanceResponse, error)
	RuntimeRestart(ctx context.Context, baseURL string, req agent.RuntimeRestartRequest) (agent.RuntimeInstanceResponse, error)
	RuntimeStatus(ctx context.Context, baseURL, instanceID string) (agent.RuntimeStatusResponse, error)
	RuntimeLogs(ctx context.Context, baseURL, instanceID string, tail int) (agent.RuntimeLogsResponse, error)
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
	panelFiles      PanelFileProvider
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

type BuiltinVariableResolver interface {
	BuiltinVariables(ctx context.Context) (map[string]any, error)
}

type PanelFileProvider interface {
	PanelFileCatalog(ctx context.Context) ([]PanelFileDefinition, error)
	ReadPanelFile(ctx context.Context, source string) ([]byte, error)
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

type saveSession struct {
	ID            string
	ApplicationID string
	Input         SaveInput
	Dir           string
	Files         map[string]*stagedFile
	CreatedAt     time.Time
	UpdatedAt     time.Time
	ExpiresAt     time.Time
}

type stagedFile struct {
	ID            string
	ApplicationID string
	Path          string
	Kind          string
	ContentType   string
	Size          int64
	SHA256        string
	DiskPath      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
	s := &Service{db: db, runtimeClient: runtimeClient, tasks: taskSvc, config: cfg, renderer: templatex.NewGoRenderer(), imageResolver: NewRegistryImageResolver(), saveSessions: map[string]*saveSession{}}
	s.startSaveSessionCleanup()
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

func (s *Service) SetBuiltinVariableResolver(resolver BuiltinVariableResolver) {
	s.builtinResolver = resolver
}

func (s *Service) SetPanelFileProvider(provider PanelFileProvider) {
	s.panelFiles = provider
}

func (s *Service) SetServerProvider(provider ServerProvider) {
	s.servers = provider
	if handler, ok := provider.(AgentErrorHandler); ok {
		s.agentErrors = handler
	}
}

func (s *Service) TemplateCatalog(ctx context.Context) (TemplateCatalog, error) {
	catalog := TemplateCatalog{Variables: []TemplateVariableDefinition{
		{Key: "server.id", Category: "server", SpecExpression: "${node.meta.panel_server_id}", TemplateExpression: `{{ env "PANEL_SERVER_ID" }}`},
		{Key: "server.name", Category: "server", SpecExpression: "${node.meta.panel_server_name}", TemplateExpression: `{{ env "PANEL_SERVER_NAME" }}`},
		{Key: "server.ssh_host", Category: "server", SpecExpression: "${node.meta.panel_ssh_host}", TemplateExpression: `{{ env "PANEL_SERVER_SSH_HOST" }}`},
		{Key: "server.ssh_port", Category: "server", SpecExpression: "${node.meta.panel_ssh_port}", TemplateExpression: `{{ env "PANEL_SERVER_SSH_PORT" }}`},
		{Key: "server.ssh_username", Category: "server", SpecExpression: "${node.meta.panel_ssh_username}", TemplateExpression: `{{ env "PANEL_SERVER_SSH_USERNAME" }}`},
	}}
	if s.panelFiles != nil {
		files, err := s.panelFiles.PanelFileCatalog(ctx)
		if err != nil {
			return TemplateCatalog{}, err
		}
		catalog.PanelFiles = files
	}
	return catalog, nil
}

func (s *Service) SetReverseProxyReconciler(reconciler ReverseProxyReconciler) {
	s.proxyReconciler = reconciler
}

func (s *Service) SetConfigProvider(provider func() Config) {
	s.configProvider = provider
}

func (s *Service) SetImageDigestResolver(resolver ImageDigestResolver) {
	s.imageResolver = resolver
}

func (s *Service) SetContainerOperationQueue(queue ContainerOperationQueue) {
	s.operationQueue = queue
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
	return apps, rows.Err()
}

func (s *Service) Get(ctx context.Context, appID string) (Application, error) {
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

func (s *Service) ListFiles(ctx context.Context, appID string) ([]ApplicationFile, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return nil, err
	}
	return s.listFiles(ctx, appID, false)
}

func (s *Service) SaveFile(ctx context.Context, appID string, in FileSaveInput) (ApplicationFile, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return ApplicationFile{}, err
	}
	targetPath, err := normalizeApplicationFilePath(in.Path)
	if err != nil {
		return ApplicationFile{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != "binary" && kind != "template" {
		return ApplicationFile{}, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
	if err != nil {
		return ApplicationFile{}, panelerr.Validation("application_file_content_invalid", "file content must be base64 encoded")
	}
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	file := ApplicationFile{
		ID:            id.New("afile"),
		ApplicationID: appID,
		Path:          targetPath,
		Kind:          kind,
		ContentType:   strings.TrimSpace(in.ContentType),
		Size:          int64(len(content)),
		SHA256:        hex.EncodeToString(sum[:]),
		Content:       content,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO application_files(id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(application_id,path) DO UPDATE SET kind=excluded.kind,content_type=excluded.content_type,size=excluded.size,sha256=excluded.sha256,content=excluded.content,updated_at=excluded.updated_at`,
		file.ID, file.ApplicationID, file.Path, file.Kind, file.ContentType, file.Size, file.SHA256, file.Content, formatTime(file.CreatedAt), formatTime(file.UpdatedAt))
	if err != nil {
		return ApplicationFile{}, err
	}
	if err := s.redeployIfEnabled(ctx, app); err != nil {
		return ApplicationFile{}, err
	}
	return s.getFileByPath(ctx, appID, targetPath, false)
}

func (s *Service) DeleteFile(ctx context.Context, appID, fileID string) error {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM application_files WHERE application_id=? AND id=?`, appID, fileID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return panelerr.NotFound("application_file")
	}
	return s.redeployIfEnabled(ctx, app)
}

func (s *Service) BeginSaveSession(ctx context.Context, in BeginSaveSessionInput) (SaveSessionResult, error) {
	if in.ApplicationID != "" {
		if _, err := s.Get(ctx, in.ApplicationID); err != nil {
			return SaveSessionResult{}, err
		}
	}
	sessionID := id.New("asave")
	now := time.Now().UTC()
	session := &saveSession{
		ID:            sessionID,
		ApplicationID: in.ApplicationID,
		Input:         in.Save,
		Dir:           filepath.Join(s.config.SaveSessionDir, sessionID),
		Files:         map[string]*stagedFile{},
		CreatedAt:     now,
		UpdatedAt:     now,
		ExpiresAt:     now.Add(30 * time.Minute),
	}
	if err := os.MkdirAll(session.Dir, 0o700); err != nil {
		return SaveSessionResult{}, err
	}
	if in.ApplicationID != "" {
		files, err := s.listFiles(ctx, in.ApplicationID, true)
		if err != nil {
			return SaveSessionResult{}, err
		}
		for _, file := range files {
			staged, err := s.stageFileBytes(session, file.Path, file.Kind, file.ContentType, file.Content, file.CreatedAt)
			if err != nil {
				return SaveSessionResult{}, err
			}
			staged.ID = file.ID
			staged.ApplicationID = file.ApplicationID
			staged.CreatedAt = file.CreatedAt
			staged.UpdatedAt = file.UpdatedAt
		}
	}
	s.sessionMu.Lock()
	s.saveSessions[session.ID] = session
	s.sessionMu.Unlock()
	return session.result(), nil
}

func (s *Service) UploadSaveSessionFile(ctx context.Context, sessionID string, in FileSaveInput) (ApplicationFile, error) {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return ApplicationFile{}, err
	}
	content, err := decodeApplicationFileInput(in)
	if err != nil {
		return ApplicationFile{}, err
	}
	staged, err := s.stageFileBytes(session, in.Path, in.Kind, in.ContentType, content, time.Now().UTC())
	if err != nil {
		return ApplicationFile{}, err
	}
	_ = ctx
	return staged.applicationFile(nil), nil
}

func (s *Service) DeleteSaveSessionFile(ctx context.Context, sessionID string, in FileDeleteInput) error {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return err
	}
	targetPath, err := normalizeApplicationFilePath(in.Path)
	if err != nil {
		return err
	}
	s.sessionMu.Lock()
	staged := session.Files[targetPath]
	if staged != nil {
		delete(session.Files, targetPath)
		session.UpdatedAt = time.Now().UTC()
	}
	s.sessionMu.Unlock()
	if staged != nil {
		_ = os.Remove(staged.DiskPath)
	}
	_ = ctx
	return nil
}

func (s *Service) CommitSaveSession(ctx context.Context, sessionID string) (Application, error) {
	session, err := s.getSaveSession(sessionID)
	if err != nil {
		return Application{}, err
	}
	files, err := session.applicationFiles()
	if err != nil {
		return Application{}, err
	}
	var app Application
	if session.ApplicationID == "" {
		app, err = s.createWithFiles(ctx, session.Input, files)
	} else {
		app, err = s.updateWithFiles(ctx, session.ApplicationID, session.Input, files)
	}
	if err != nil {
		return Application{}, err
	}
	s.discardSaveSession(sessionID)
	return app, nil
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
		_, _ = s.recordTask(ctx, TaskTypeImageCheck, app.ID, "Checking image for "+app.Name)
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
	if _, err := s.recordTask(ctx, TaskTypeImageCheck, app.ID, "Checking image for "+app.Name); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func (s *Service) UpdateImage(ctx context.Context, appID string) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	if !app.Enabled {
		return OperationResult{}, panelerr.Conflict("application_disabled", "enable the application before updating its image")
	}
	app, err = s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	result, checkedAt, err := s.resolveApplicationImage(ctx, app)
	if err != nil {
		app.ImageCheckedAt = &checkedAt
		app.ImageLastError = err.Error()
		app.UpdatedAt = time.Now().UTC()
		_ = s.updateApplication(ctx, app)
		return OperationResult{}, err
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
		return OperationResult{}, err
	}
	if len(issues) > 0 {
		return OperationResult{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	if len(targets) == 0 {
		return OperationResult{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	if err := s.updateApplication(ctx, app); err != nil {
		return OperationResult{}, err
	}
	if err := s.insertRevision(ctx, app, job); err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordRunningTask(ctx, TaskTypeImageUpdate, app.ID, "Updating image for "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.deployRuntimeSpec(ctx, taskID, app, job); err != nil {
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskID, EvalID: app.LastEvalID, Application: app}, nil
}

func (s *Service) Deploy(ctx context.Context, appID string) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	app, err = s.refreshApplicationSnapshot(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	job, issues, err := s.renderApplication(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	if len(issues) > 0 {
		return OperationResult{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	targets, err := s.deploymentTargets(ctx, app)
	if err != nil {
		return OperationResult{}, err
	}
	if len(targets) == 0 {
		return OperationResult{}, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
	}
	app.Enabled = true
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, app); err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordRunningTask(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	if err := s.deployRuntimeSpec(ctx, taskID, app, job); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return OperationResult{}, err
	}
	if err := s.reconcileReverseProxy(ctx); err != nil {
		return OperationResult{}, err
	}
	result := OperationResult{TaskID: taskID, Application: app}
	if runtime, err := s.Runtime(ctx, app.ID); err == nil {
		result.ApplicationRuntime = &runtime
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "Current application runtime status: "+runtime.Status)
		}
	}
	return result, nil
}

func (s *Service) RedeployChangedApplications(ctx context.Context) (int, error) {
	apps, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	redeployed := 0
	for _, app := range apps {
		if !app.Enabled {
			continue
		}
		beforeHash := app.SpecHash
		refreshed, err := s.refreshApplicationSnapshot(ctx, app)
		if err != nil {
			return redeployed, err
		}
		if refreshed.SpecHash == beforeHash {
			continue
		}
		spec, issues, err := s.renderApplication(ctx, refreshed)
		if err != nil {
			return redeployed, err
		}
		if len(issues) > 0 {
			return redeployed, panelerr.Validation("application_invalid", issues[0].Message)
		}
		targets, err := s.deploymentTargets(ctx, refreshed)
		if err != nil {
			return redeployed, err
		}
		if len(targets) == 0 {
			return redeployed, panelerr.Validation("application_no_runtime_targets", "No agent runtime targets are available")
		}
		refreshed.UpdatedAt = time.Now().UTC()
		if err := s.updateApplication(ctx, refreshed); err != nil {
			return redeployed, err
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
		if _, err := s.Deploy(ctx, app.ID); err != nil {
			return redeployed, err
		}
		redeployed++
	}
	return redeployed, nil
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
	taskID, err := s.recordRunningTask(ctx, TaskTypeStop, app.ID, "Stopping application "+app.Name)
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
	out.Status = aggregateRuntimeStatus(app.Enabled, out.Instances)
	return out, nil
}

func (s *Service) Logs(ctx context.Context, appID string, in LogInput) (LogResult, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return LogResult{}, err
	}
	if in.Tail == 0 {
		in.Tail = 200
	}
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
	logs, err := s.runtimeClient.RuntimeLogs(ctx, baseURL, instance.ID, in.Tail)
	if err != nil {
		_ = s.handleAgentError(ctx, srv, err)
		return LogResult{}, runtimeOperationError(err)
	}
	return LogResult{InstanceID: instance.ID, ContainerName: instance.ContainerName, Type: "combined", Logs: logs.Logs}, nil
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
	data, err := s.templateData(ctx, variables, files)
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
	data, err := s.templateData(ctx, app.Variables, files)
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
	data, err := s.templateData(ctx, variables, nil)
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
	data, err := s.templateData(ctx, app.Variables, files)
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

func (s *Service) templateData(ctx context.Context, variables map[string]string, files []ApplicationFile) (map[string]any, error) {
	data := map[string]any{}
	varMap := map[string]any{}
	for key, value := range variables {
		data[key] = value
		varMap[key] = value
	}
	data["vars"] = varMap
	data["files"] = fileVariables(files)
	if s.builtinResolver != nil {
		builtins, err := s.builtinResolver.BuiltinVariables(ctx)
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
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Advance(ctx, taskID, "deploying", "deploying application instances")
	}
	for _, target := range targets {
		baseURL, ok := agentURLFromServer(target)
		if !ok {
			return panelerr.Validation("agent_required", "Agent is required for application deployment")
		}
		instanceSpec := runtimeSpecForServer(app, spec, target)
		if err := s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusDeploying, "", ""); err != nil {
			return err
		}
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "deploying "+instanceSpec.ContainerName+" on "+firstNonEmpty(target.Name, target.ID, target.Host))
		}
		var result agent.RuntimeInstanceResponse
		err := s.executeContainerOperation(ctx, target.ID, func(runCtx context.Context) error {
			var runErr error
			result, runErr = s.runtimeClient.RuntimeDeploy(runCtx, baseURL, agent.RuntimeDeployRequest{ServerID: target.ID, Spec: instanceSpec})
			return runErr
		})
		if err != nil {
			_ = s.handleAgentError(ctx, target, err)
			_ = s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, appruntime.StatusFailed, "", err.Error())
			return runtimeOperationError(err)
		}
		if err := s.upsertRuntimeInstance(ctx, app.ID, target.ID, instanceSpec, appruntime.DesiredRunning, result.Status, result.ContainerID, ""); err != nil {
			return err
		}
	}
	if s.tasks != nil && taskID != "" {
		_ = s.tasks.Complete(ctx, taskID, "Application deployed")
	}
	return nil
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
		var result agent.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
			var runErr error
			result, runErr = s.runtimeClient.RuntimeStop(runCtx, baseURL, agent.RuntimeStopRequest{InstanceID: instance.ID, Purge: purge})
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
		var result agent.RuntimeInstanceResponse
		err = s.executeContainerOperation(ctx, instance.ServerID, func(runCtx context.Context) error {
			var runErr error
			result, runErr = s.runtimeClient.RuntimeRestart(runCtx, baseURL, agent.RuntimeRestartRequest{InstanceID: instance.ID})
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
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "application",
		ResourceID:   appID,
		Status:       tasks.StatusCompleted,
		Summary:      summary,
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

func (s *Service) recordRunningTask(ctx context.Context, taskType, appID, summary string) (string, error) {
	if s.tasks == nil {
		return "", nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ResourceType: "application",
		ResourceID:   appID,
		Status:       tasks.StatusRunning,
		Summary:      summary,
	})
	if err != nil {
		return "", err
	}
	return task.ID, nil
}

func (s *Service) listFiles(ctx context.Context, appID string, includeContent bool) ([]ApplicationFile, error) {
	columns := `id,application_id,path,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? ORDER BY path ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApplicationFile{}
	for rows.Next() {
		file, err := scanApplicationFile(rows, includeContent)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func (s *Service) getFileByPath(ctx context.Context, appID, filePath string, includeContent bool) (ApplicationFile, error) {
	columns := `id,application_id,path,kind,content_type,size,sha256,created_at,updated_at`
	if includeContent {
		columns = `id,application_id,path,kind,content_type,size,sha256,content,created_at,updated_at`
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM application_files WHERE application_id=? AND path=?`, appID, filePath)
	file, err := scanApplicationFile(row, includeContent)
	if err == sql.ErrNoRows {
		return ApplicationFile{}, panelerr.NotFound("application_file")
	}
	return file, err
}

func scanApplicationFile(row appScanner, includeContent bool) (ApplicationFile, error) {
	var file ApplicationFile
	var createdAt, updatedAt string
	if includeContent {
		if err := row.Scan(&file.ID, &file.ApplicationID, &file.Path, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &file.Content, &createdAt, &updatedAt); err != nil {
			return ApplicationFile{}, err
		}
	} else {
		if err := row.Scan(&file.ID, &file.ApplicationID, &file.Path, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &createdAt, &updatedAt); err != nil {
			return ApplicationFile{}, err
		}
	}
	file.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	file.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return file, nil
}

func (s *Service) attachFiles(ctx context.Context, job appruntime.Spec, spec appspec.Spec, files []ApplicationFile, data map[string]any) (appruntime.Spec, error) {
	fileMounts := applicationFileMounts(spec.Mounts)
	if len(fileMounts) == 0 {
		return job, nil
	}
	filesByPath := map[string]ApplicationFile{}
	for _, file := range files {
		filesByPath[file.Path] = file
	}
	mounts := make([]appruntime.Mount, 0, len(job.Mounts)+len(fileMounts))
	for _, mount := range job.Mounts {
		if mount.Type != "managed_file" {
			mounts = append(mounts, mount)
		}
	}
	managed := append([]appruntime.ManagedFile(nil), job.Files...)
	for _, mount := range fileMounts {
		if strings.TrimSpace(mount.Type) == "panel_file" {
			if s.panelFiles == nil {
				return appruntime.Spec{}, panelerr.Validation("panel_file_provider_unavailable", "Panel managed file provider is unavailable")
			}
			content, err := s.panelFiles.ReadPanelFile(ctx, mount.Source)
			if err != nil {
				return appruntime.Spec{}, err
			}
			rel := panelFileAllocationName(mount.Source)
			managed = append(managed, appruntime.ManagedFile{Path: rel, Content: content, Mode: panelFilePerms(mount.Source)})
			mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: rel, Target: mount.Target, ReadOnly: true})
			continue
		}
		rel, err := normalizeApplicationWorkspacePath(mount.Source)
		if err != nil {
			return appruntime.Spec{}, err
		}
		file, ok := filesByPath[rel]
		if !ok {
			return appruntime.Spec{}, panelerr.Validation("application_file_mount_missing", "mounted application file "+rel+" does not exist")
		}
		rendered := file.Content
		if file.Kind == "template" {
			text := string(file.Content)
			if s.renderer != nil {
				text, err = s.renderer.Render(ctx, text, data)
				if err != nil {
					return appruntime.Spec{}, err
				}
			}
			rendered = []byte(text)
		}
		managed = append(managed, appruntime.ManagedFile{Path: rel, Content: rendered, Mode: "0644"})
		mounts = append(mounts, appruntime.Mount{Type: "managed_file", Source: rel, Target: mount.Target, ReadOnly: mount.ReadOnly})
	}
	job.Mounts = mounts
	job.Files = managed
	return job, nil
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
			if err := ensureAgentRuntimeReady(srv); err != nil {
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
	if srv.Traits[agent.TraitStatus] != agent.StatusCompatible {
		return panelerr.Validation("agent_incompatible", "Agent is not compatible with application runtime")
	}
	return nil
}

func runtimeSpecForServer(app Application, spec appruntime.Spec, srv server.Server) appruntime.Spec {
	out := spec
	out.ApplicationID = app.ID
	out.InstanceID = runtimeInstanceID(app.ID, srv.ID)
	out.ContainerName = runtimeContainerName(app.ID, srv.ID)
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
	return out
}

func runtimeInstanceID(appID, serverID string) string {
	return strings.TrimSpace(appID) + "-" + strings.TrimSpace(serverID)
}

func runtimeContainerName(appID, serverID string) string {
	return "panel-" + sanitizeRuntimeName(runtimeInstanceID(appID, serverID))
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
				if ensureAgentRuntimeReady(srv) == nil {
					baseURL, _ := agentURLFromServer(srv)
					if remote, err := s.runtimeClient.RuntimeStatus(ctx, baseURL, instance.ID); err == nil {
						status = remote.InstanceStatus
						status.ServerID = instance.ServerID
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
	if srv.Traits == nil || strings.TrimSpace(srv.Traits[agent.TraitEnabled]) != "true" {
		return "", false
	}
	u := strings.TrimSpace(srv.Traits[agent.TraitURL])
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
	data, err := s.templateData(ctx, app.Variables, files)
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
		data, err := s.templateData(ctx, app.Variables, files)
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
		"resolved":       resolved,
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

func decodeApplicationFileInput(in FileSaveInput) ([]byte, error) {
	if _, err := normalizeApplicationFilePath(in.Path); err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind != "binary" && kind != "template" {
		return nil, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	content, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
	if err != nil {
		return nil, panelerr.Validation("application_file_content_invalid", "file content must be base64 encoded")
	}
	return content, nil
}

func (s *Service) getSaveSession(sessionID string) (*saveSession, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	session := s.saveSessions[sessionID]
	if session == nil {
		return nil, panelerr.NotFound("application_save_session")
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		delete(s.saveSessions, sessionID)
		_ = os.RemoveAll(session.Dir)
		return nil, panelerr.NotFound("application_save_session")
	}
	session.UpdatedAt = time.Now().UTC()
	session.ExpiresAt = session.UpdatedAt.Add(30 * time.Minute)
	return session, nil
}

func (s *Service) discardSaveSession(sessionID string) {
	s.sessionMu.Lock()
	session := s.saveSessions[sessionID]
	delete(s.saveSessions, sessionID)
	s.sessionMu.Unlock()
	if session != nil {
		_ = os.RemoveAll(session.Dir)
	}
}

func (s *Service) stageFileBytes(session *saveSession, targetPath, kind, contentType string, content []byte, createdAt time.Time) (*stagedFile, error) {
	targetPath, err := normalizeApplicationFilePath(targetPath)
	if err != nil {
		return nil, err
	}
	kind = strings.TrimSpace(kind)
	if kind != "binary" && kind != "template" {
		return nil, panelerr.Validation("application_file_kind_invalid", "file kind must be binary or template")
	}
	sum := sha256.Sum256(content)
	now := time.Now().UTC()
	if createdAt.IsZero() {
		createdAt = now
	}
	staged := &stagedFile{
		ID:            id.New("afile"),
		ApplicationID: session.ApplicationID,
		Path:          targetPath,
		Kind:          kind,
		ContentType:   strings.TrimSpace(contentType),
		Size:          int64(len(content)),
		SHA256:        hex.EncodeToString(sum[:]),
		DiskPath:      filepath.Join(session.Dir, id.New("blob")),
		CreatedAt:     createdAt,
		UpdatedAt:     now,
	}
	if err := os.WriteFile(staged.DiskPath, content, 0o600); err != nil {
		return nil, err
	}
	s.sessionMu.Lock()
	old := session.Files[targetPath]
	session.Files[targetPath] = staged
	session.UpdatedAt = now
	session.ExpiresAt = now.Add(30 * time.Minute)
	s.sessionMu.Unlock()
	if old != nil {
		_ = os.Remove(old.DiskPath)
	}
	return staged, nil
}

func (s *Service) startSaveSessionCleanup() {
	s.cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				s.cleanupExpiredSaveSessions(time.Now().UTC())
			}
		}()
	})
}

func (s *Service) cleanupExpiredSaveSessions(now time.Time) {
	expired := []*saveSession{}
	s.sessionMu.Lock()
	for key, session := range s.saveSessions {
		if now.Sub(session.UpdatedAt) <= 30*time.Minute {
			continue
		}
		expired = append(expired, session)
		delete(s.saveSessions, key)
	}
	s.sessionMu.Unlock()
	for _, session := range expired {
		_ = os.RemoveAll(session.Dir)
	}
	entries, err := os.ReadDir(s.config.SaveSessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) <= 30*time.Minute {
			continue
		}
		_ = os.RemoveAll(filepath.Join(s.config.SaveSessionDir, entry.Name()))
	}
}

func (session *saveSession) result() SaveSessionResult {
	files := make([]ApplicationFile, 0, len(session.Files))
	for _, staged := range session.Files {
		files = append(files, staged.applicationFile(nil))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return SaveSessionResult{
		ID:            session.ID,
		ApplicationID: session.ApplicationID,
		ExpiresAt:     session.ExpiresAt,
		Files:         files,
	}
}

func (session *saveSession) applicationFiles() ([]ApplicationFile, error) {
	files := make([]ApplicationFile, 0, len(session.Files))
	for _, staged := range session.Files {
		content, err := os.ReadFile(staged.DiskPath)
		if err != nil {
			return nil, err
		}
		files = append(files, staged.applicationFile(content))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (file *stagedFile) applicationFile(content []byte) ApplicationFile {
	return ApplicationFile{
		ID:            file.ID,
		ApplicationID: file.ApplicationID,
		Path:          file.Path,
		Kind:          file.Kind,
		ContentType:   file.ContentType,
		Size:          file.Size,
		SHA256:        file.SHA256,
		Content:       content,
		CreatedAt:     file.CreatedAt,
		UpdatedAt:     file.UpdatedAt,
	}
}

func normalizeApplicationFilesForSave(appID string, files []ApplicationFile, now time.Time) []ApplicationFile {
	if files == nil {
		return nil
	}
	out := make([]ApplicationFile, 0, len(files))
	for _, file := range files {
		if file.ID == "" {
			file.ID = id.New("afile")
		}
		file.ApplicationID = appID
		if file.CreatedAt.IsZero() {
			file.CreatedAt = now
		}
		if file.UpdatedAt.IsZero() {
			file.UpdatedAt = now
		}
		out = append(out, file)
	}
	return out
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
