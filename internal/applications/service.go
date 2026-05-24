package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"panel/internal/appspec"
	"panel/internal/id"
	"panel/internal/nomad"
	"panel/internal/panelerr"
	"panel/internal/tasks"
)

type Config struct {
	Namespace  string
	Region     string
	Datacenter string
}

type NomadClient interface {
	ValidateJob(ctx context.Context, job nomad.Job) (nomad.ValidateResponse, error)
	PlanJob(ctx context.Context, id string, job nomad.Job) (nomad.PlanResponse, error)
	RegisterJob(ctx context.Context, id string, job nomad.Job) (nomad.RegisterResponse, error)
	StopJob(ctx context.Context, id string, purge bool) (nomad.StopResponse, error)
	JobAllocations(ctx context.Context, id string) ([]nomad.AllocationListItem, error)
	JobDeployment(ctx context.Context, id string) (nomad.Deployment, error)
	JobEvaluations(ctx context.Context, id string) ([]nomad.Evaluation, error)
	RestartAllocation(ctx context.Context, allocID, task string) error
	AllocationLogs(ctx context.Context, allocID, task, logType string, tail int) (string, error)
}

type Service struct {
	db     *sql.DB
	nomad  NomadClient
	tasks  *tasks.Service
	config Config
}

type ApplicationRuntime = Runtime

type PlanResult struct {
	Application Application        `json:"application"`
	Job         nomad.Job          `json:"job"`
	Plan        nomad.PlanResponse `json:"plan"`
}

type LogInput struct {
	AllocID string `json:"allocId"`
	Task    string `json:"task"`
	Type    string `json:"type"`
	Tail    int    `json:"tail"`
}

type LogResult struct {
	AllocID string `json:"allocId"`
	Task    string `json:"task"`
	Type    string `json:"type"`
	Logs    string `json:"logs"`
}

func NewService(db *sql.DB, nomadClient NomadClient, taskSvc *tasks.Service, cfg Config) *Service {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.Region == "" {
		cfg.Region = "global"
	}
	if cfg.Datacenter == "" {
		cfg.Datacenter = "dc1"
	}
	return &Service{db: db, nomad: nomadClient, tasks: taskSvc, config: cfg}
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
	prepared, err := s.prepare(in, 1, "")
	if err != nil {
		return Application{}, err
	}
	now := time.Now().UTC()
	app := Application{
		ID:         id.New("app"),
		Name:       in.Name,
		Enabled:    in.Enabled,
		SpecYAML:   in.SpecYAML,
		Variables:  prepared.variables,
		Generation: 1,
		SpecHash:   prepared.hash,
		JobID:      prepared.job.ID,
		Namespace:  s.config.Namespace,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if app.Name == "" {
		app.Name = prepared.spec.Name
	}
	if app.Enabled {
		if err := s.validatePlanRegister(ctx, prepared.job, &app); err != nil {
			return Application{}, err
		}
	}
	if err := s.insertApplication(ctx, app); err != nil {
		return Application{}, err
	}
	if err := s.insertRevision(ctx, app, prepared.job); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, app.ID)
}

func (s *Service) Update(ctx context.Context, appID string, in SaveInput) (Application, error) {
	current, err := s.Get(ctx, appID)
	if err != nil {
		return Application{}, err
	}
	generation := current.Generation
	prepared, err := s.prepare(in, generation, appID)
	if err != nil {
		return Application{}, err
	}
	if prepared.hash != current.SpecHash {
		generation++
		prepared, err = s.prepare(in, generation, appID)
		if err != nil {
			return Application{}, err
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
	app.Generation = generation
	app.SpecHash = prepared.hash
	app.JobID = prepared.job.ID
	app.Namespace = s.config.Namespace
	app.UpdatedAt = time.Now().UTC()
	if app.Enabled && prepared.hash != current.SpecHash {
		if err := s.validatePlanRegister(ctx, prepared.job, &app); err != nil {
			return Application{}, err
		}
	}
	if err := s.updateApplication(ctx, app); err != nil {
		return Application{}, err
	}
	if prepared.hash != current.SpecHash {
		if err := s.insertRevision(ctx, app, prepared.job); err != nil {
			return Application{}, err
		}
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
	return err
}

func (s *Service) Validate(ctx context.Context, appID string) (ValidationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return ValidationResult{}, err
	}
	job, issues, err := s.renderApplication(app)
	if err != nil || len(issues) > 0 {
		return validationResult(issues), err
	}
	resp, err := s.nomad.ValidateJob(ctx, job)
	if err != nil {
		return ValidationResult{}, err
	}
	for _, msg := range resp.ValidationErrors {
		issues = append(issues, ValidationIssue{Field: "nomad", Message: msg})
	}
	return validationResult(issues), nil
}

func (s *Service) Plan(ctx context.Context, appID string) (PlanResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return PlanResult{}, err
	}
	job, issues, err := s.renderApplication(app)
	if err != nil {
		return PlanResult{}, err
	}
	if len(issues) > 0 {
		return PlanResult{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	plan, err := s.nomad.PlanJob(ctx, job.ID, job)
	if err != nil {
		return PlanResult{}, err
	}
	return PlanResult{Application: app, Job: job, Plan: plan}, nil
}

func (s *Service) Deploy(ctx context.Context, appID string) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	job, issues, err := s.renderApplication(app)
	if err != nil {
		return OperationResult{}, err
	}
	if len(issues) > 0 {
		return OperationResult{}, panelerr.Validation("application_invalid", issues[0].Message)
	}
	if err := s.validatePlanRegister(ctx, job, &app); err != nil {
		return OperationResult{}, err
	}
	app.Enabled = true
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, app); err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordTask(ctx, TaskTypeDeploy, app.ID, "Deploying application "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskID, EvalID: app.LastEvalID, Application: app}, nil
}

func (s *Service) Stop(ctx context.Context, appID string, purge bool) (OperationResult, error) {
	app, err := s.Get(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	resp, err := s.nomad.StopJob(ctx, app.JobID, purge)
	if err != nil {
		return OperationResult{}, err
	}
	app.Enabled = false
	app.LastEvalID = resp.EvalID
	app.UpdatedAt = time.Now().UTC()
	if err := s.updateApplication(ctx, app); err != nil {
		return OperationResult{}, err
	}
	taskID, err := s.recordTask(ctx, TaskTypeStop, app.ID, "Stopping application "+app.Name)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskID, EvalID: app.LastEvalID, Application: app}, nil
}

func (s *Service) Restart(ctx context.Context, appID string) (OperationResult, error) {
	runtime, err := s.Runtime(ctx, appID)
	if err != nil {
		return OperationResult{}, err
	}
	for _, alloc := range runtime.Allocations {
		if err := s.nomad.RestartAllocation(ctx, alloc.ID, ""); err != nil {
			return OperationResult{}, err
		}
	}
	taskID, err := s.recordTask(ctx, TaskTypeRestart, appID, "Restarting application")
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
		JobID:         app.JobID,
		JobStatus:     "stopped",
		ObservedAt:    time.Now().UTC(),
	}
	if !app.Enabled {
		return out, nil
	}
	deployment, err := s.nomad.JobDeployment(ctx, app.JobID)
	if err != nil {
		out.JobStatus = "unknown"
		return out, nil
	}
	allocations, err := s.nomad.JobAllocations(ctx, app.JobID)
	if err != nil {
		out.JobStatus = "unknown"
		return out, nil
	}
	evaluations, err := s.nomad.JobEvaluations(ctx, app.JobID)
	if err != nil {
		out.JobStatus = "unknown"
		return out, nil
	}
	out.Deployment = &deployment
	out.Allocations = allocations
	out.Evaluations = evaluations
	out.JobStatus = runtimeStatus(app.Enabled, deployment, allocations)
	return out, nil
}

func (s *Service) Logs(ctx context.Context, appID string, in LogInput) (LogResult, error) {
	if _, err := s.Get(ctx, appID); err != nil {
		return LogResult{}, err
	}
	logType := in.Type
	if logType == "" {
		logType = "stdout"
	}
	if in.Tail == 0 {
		in.Tail = 200
	}
	logs, err := s.nomad.AllocationLogs(ctx, in.AllocID, in.Task, logType, in.Tail)
	if err != nil {
		return LogResult{}, err
	}
	return LogResult{AllocID: in.AllocID, Task: in.Task, Type: logType, Logs: logs}, nil
}

type preparedApplication struct {
	spec      appspec.Spec
	variables map[string]string
	hash      string
	job       nomad.Job
}

func (s *Service) prepare(in SaveInput, generation int, appID string) (preparedApplication, error) {
	spec, specIssues := appspec.DecodeYAML(in.SpecYAML)
	if len(specIssues) > 0 {
		return preparedApplication{}, panelerr.Validation("application_invalid", specIssues[0].Message)
	}
	variables := in.Variables
	if variables == nil {
		variables = map[string]string{}
	}
	hash, err := appspec.Hash(spec, variables)
	if err != nil {
		return preparedApplication{}, err
	}
	job, renderIssues := appspec.Render(appspec.RenderInput{
		AppID:      appID,
		Generation: generation,
		SpecHash:   hash,
		Namespace:  s.config.Namespace,
		Region:     s.config.Region,
		Datacenter: s.config.Datacenter,
		Spec:       spec,
	})
	if len(renderIssues) > 0 {
		return preparedApplication{}, panelerr.Validation("application_invalid", renderIssues[0].Message)
	}
	return preparedApplication{spec: spec, variables: variables, hash: hash, job: job}, nil
}

func (s *Service) renderApplication(app Application) (nomad.Job, []ValidationIssue, error) {
	spec, specIssues := appspec.DecodeYAML(app.SpecYAML)
	issues := make([]ValidationIssue, 0, len(specIssues))
	for _, issue := range specIssues {
		issues = append(issues, ValidationIssue{Field: issue.Field, Message: issue.Message})
	}
	if len(issues) > 0 {
		return nomad.Job{}, issues, nil
	}
	job, renderIssues := appspec.Render(appspec.RenderInput{
		AppID:      app.ID,
		Generation: app.Generation,
		SpecHash:   app.SpecHash,
		Namespace:  s.config.Namespace,
		Region:     s.config.Region,
		Datacenter: s.config.Datacenter,
		Spec:       spec,
	})
	for _, issue := range renderIssues {
		issues = append(issues, ValidationIssue{Field: issue.Field, Message: issue.Message})
	}
	return job, issues, nil
}

func (s *Service) validatePlanRegister(ctx context.Context, job nomad.Job, app *Application) error {
	if _, err := s.nomad.ValidateJob(ctx, job); err != nil {
		return err
	}
	if _, err := s.nomad.PlanJob(ctx, job.ID, job); err != nil {
		return err
	}
	resp, err := s.nomad.RegisterJob(ctx, job.ID, job)
	if err != nil {
		return err
	}
	app.LastEvalID = resp.EvalID
	return nil
}

func (s *Service) insertApplication(ctx context.Context, app Application) error {
	variables, err := json.Marshal(app.Variables)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO applications(id,name,enabled,spec_yaml,variables_json,generation,spec_hash,job_id,namespace,last_eval_id,last_deployment_id,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		app.ID, app.Name, boolInt(app.Enabled), app.SpecYAML, string(variables), app.Generation, app.SpecHash, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.CreatedAt), formatTime(app.UpdatedAt))
	return err
}

func (s *Service) updateApplication(ctx context.Context, app Application) error {
	variables, err := json.Marshal(app.Variables)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE applications SET name=?,enabled=?,spec_yaml=?,variables_json=?,generation=?,spec_hash=?,job_id=?,namespace=?,last_eval_id=?,last_deployment_id=?,last_error=?,updated_at=? WHERE id=?`,
		app.Name, boolInt(app.Enabled), app.SpecYAML, string(variables), app.Generation, app.SpecHash, app.JobID, app.Namespace, app.LastEvalID, app.LastDeploymentID, app.LastError, formatTime(app.UpdatedAt), app.ID)
	return err
}

func (s *Service) insertRevision(ctx context.Context, app Application, job nomad.Job) error {
	raw, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO application_revisions(id,application_id,generation,spec_hash,spec_yaml,job_json,created_at) VALUES(?,?,?,?,?,?,?)`,
		id.New("arev"), app.ID, app.Generation, app.SpecHash, app.SpecYAML, string(raw), formatTime(time.Now().UTC()))
	return err
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

func validationResult(issues []ValidationIssue) ValidationResult {
	return ValidationResult{Valid: len(issues) == 0, Issues: issues}
}

type appScanner interface{ Scan(...any) error }

const applicationColumns = `id,name,enabled,spec_yaml,variables_json,generation,spec_hash,job_id,namespace,last_eval_id,last_deployment_id,last_error,created_at,updated_at`

func scanApplication(row appScanner) (Application, error) {
	var app Application
	var enabled int
	var variables string
	var createdAt, updatedAt string
	if err := row.Scan(&app.ID, &app.Name, &enabled, &app.SpecYAML, &variables, &app.Generation, &app.SpecHash, &app.JobID, &app.Namespace, &app.LastEvalID, &app.LastDeploymentID, &app.LastError, &createdAt, &updatedAt); err != nil {
		return Application{}, err
	}
	app.Enabled = enabled == 1
	if variables != "" {
		_ = json.Unmarshal([]byte(variables), &app.Variables)
	}
	if app.Variables == nil {
		app.Variables = map[string]string{}
	}
	app.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	app.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return app, nil
}

func runtimeStatus(enabled bool, deployment nomad.Deployment, allocations []nomad.AllocationListItem) string {
	if !enabled {
		return "stopped"
	}
	if len(allocations) == 0 {
		return "pending"
	}
	for _, alloc := range allocations {
		if alloc.ClientStatus == "failed" {
			return "failed"
		}
	}
	if deployment.Status == "running" {
		for _, alloc := range allocations {
			if alloc.ClientStatus != "running" {
				return "pending"
			}
		}
		return "running"
	}
	return "pending"
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
