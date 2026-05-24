package containerservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/containerrender"
	"panel/internal/id"
	"panel/internal/panelerr"
	"panel/internal/tasks"

	"gopkg.in/yaml.v3"
)

var serviceNameRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

type Service struct {
	db        *sql.DB
	tasks     *tasks.Service
	logReader LogReader
}

func NewService(db *sql.DB, taskSvc *tasks.Service) *Service {
	return &Service{db: db, tasks: taskSvc}
}

type LogReader interface {
	ReadContainerLogs(ctx context.Context, nodeID, containerName string, tail int) ([]string, error)
}

func (s *Service) SetLogReader(reader LogReader) {
	s.logReader = reader
}

func ValidateName(name string) error {
	if !serviceNameRE.MatchString(strings.TrimSpace(name)) {
		return panelerr.Validation("container_service_name_invalid", "Service name must be 1-32 lowercase letters, digits, or dashes, and start/end with a letter or digit")
	}
	return nil
}

func ParseServiceBody(source string) (ParsedServiceBody, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(maskTemplateActions(source)), &doc); err != nil {
		return ParsedServiceBody{}, panelerr.Validation("container_service_yaml_invalid", "Compose service body YAML is invalid: "+err.Error())
	}
	if len(doc) == 0 {
		return ParsedServiceBody{}, panelerr.Validation("container_service_yaml_invalid", "Compose service body is required")
	}
	forbiddenTop := map[string]bool{"services": true, "include": true}
	for key := range doc {
		if forbiddenTop[key] {
			return ParsedServiceBody{}, panelerr.Validation("container_service_full_compose_forbidden", "Users must provide one Compose service body, not a full Compose document")
		}
	}
	if _, ok := doc["container_name"]; ok {
		return ParsedServiceBody{}, panelerr.Validation("container_name_forbidden", "container_name is not allowed for Container Services")
	}
	labels := composeLabels(doc["labels"])
	hostMode := fmt.Sprint(doc["network_mode"]) == "host"
	_, hasClaims := labels["panel.claims.ports"]
	for key := range labels {
		if strings.HasPrefix(key, "panel.") && key != "panel.claims.ports" {
			return ParsedServiceBody{}, panelerr.Validation("panel_labels_reserved", "panel.* Docker labels are reserved")
		}
	}
	if hostMode && !hasClaims {
		return ParsedServiceBody{}, panelerr.Validation("host_port_claims_required", "Host-network services must declare panel.claims.ports")
	}
	if !hostMode && hasClaims {
		return ParsedServiceBody{}, panelerr.Validation("host_port_claims_forbidden", "panel.claims.ports is only allowed with host network mode")
	}
	deps, err := dependencies(doc["depends_on"])
	if err != nil {
		return ParsedServiceBody{}, err
	}
	claims, err := portClaims(doc["ports"], labels["panel.claims.ports"], hostMode)
	if err != nil {
		return ParsedServiceBody{}, err
	}
	return ParsedServiceBody{Fields: doc, Dependencies: deps, PortClaims: claims}, nil
}

func ValidateDependencyGraph(root string, graph map[string][]string) error {
	if _, ok := graph[root]; !ok {
		return panelerr.Validation("container_service_dependency_missing", "Dependency graph root is missing")
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string) error
	walk = func(name string) error {
		if visiting[name] {
			return panelerr.Validation("container_service_dependency_cycle", "Container Service dependency cycle detected")
		}
		if visited[name] {
			return nil
		}
		deps, ok := graph[name]
		if !ok {
			return panelerr.Validation("container_service_dependency_missing", "Dependency "+name+" does not exist")
		}
		visiting[name] = true
		defer delete(visiting, name)
		for _, dep := range deps {
			if dep == name || dep == root && name == root {
				return panelerr.Validation("container_service_dependency_self", "Container Service cannot depend on itself")
			}
			if err := ValidateName(dep); err != nil {
				return err
			}
			if _, ok := graph[dep]; !ok {
				return panelerr.Validation("container_service_dependency_missing", "Dependency "+dep+" does not exist")
			}
			if err := walk(dep); err != nil {
				return err
			}
		}
		visited[name] = true
		return nil
	}
	return walk(root)
}

func (s *Service) Create(ctx context.Context, req SaveRequest) (ContainerService, error) {
	name := strings.TrimSpace(req.Name)
	if err := ValidateName(name); err != nil {
		return ContainerService{}, err
	}
	if _, err := s.validateSpec(ctx, "", name, req.ComposeServiceYAML); err != nil {
		return ContainerService{}, err
	}
	vars := nonNil(req.Variables)
	selector := nonNil(req.Selector)
	hash := specHashWithFiles(req.ComposeServiceYAML, vars, nil)
	now := time.Now().UTC()
	item := ContainerService{
		ID:                 id.New("csvc"),
		Name:               name,
		Enabled:            req.Enabled,
		ComposeServiceYAML: req.ComposeServiceYAML,
		Variables:          vars,
		Selector:           selector,
		Generation:         1,
		SpecHash:           hash,
		SpecRevision:       hash[:16],
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	varsJSON, _ := json.Marshal(vars)
	selectorJSON, _ := json.Marshal(selector)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContainerService{}, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO container_services(id,name,enabled,compose_service_yaml,variables_json,selector_json,generation,spec_revision,spec_hash,last_error,last_task_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Name, boolInt(item.Enabled), item.ComposeServiceYAML, string(varsJSON), string(selectorJSON), item.Generation, item.SpecRevision, item.SpecHash, "", "", ts(now), ts(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ContainerService{}, panelerr.Conflict("container_service_name_exists", "Container Service name already exists")
		}
		return ContainerService{}, err
	}
	if item.Enabled {
		task, err := s.enqueueTx(ctx, tx, item.ID, TaskTypeReconcile, TriggerUser, "Reconciling Container Service "+item.Name)
		if err != nil {
			return ContainerService{}, err
		}
		item.LastTaskID = task.ID
		if _, err := tx.ExecContext(ctx, `UPDATE container_services SET last_task_id=? WHERE id=?`, task.ID, item.ID); err != nil {
			return ContainerService{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ContainerService{}, err
	}
	return item, nil
}

func (s *Service) Update(ctx context.Context, serviceID string, req SaveRequest) (ContainerService, error) {
	old, err := s.Get(ctx, serviceID)
	if err != nil {
		return ContainerService{}, err
	}
	if strings.TrimSpace(req.ComposeServiceYAML) == "" {
		req.ComposeServiceYAML = old.ComposeServiceYAML
	}
	if req.Variables == nil {
		req.Variables = old.Variables
	}
	if req.Selector == nil {
		req.Selector = old.Selector
	}
	if _, err := s.validateSpec(ctx, serviceID, old.Name, req.ComposeServiceYAML); err != nil {
		return ContainerService{}, err
	}
	vars := nonNil(req.Variables)
	selector := nonNil(req.Selector)
	files, err := s.ListFiles(ctx, serviceID)
	if err != nil {
		return ContainerService{}, err
	}
	hash := specHashWithFiles(req.ComposeServiceYAML, vars, files)
	generation := old.Generation
	if hash != old.SpecHash {
		generation++
	}
	now := time.Now().UTC()
	varsJSON, _ := json.Marshal(vars)
	selectorJSON, _ := json.Marshal(selector)
	revision := hash[:16]
	var lastTaskID string
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ContainerService{}, err
	}
	defer tx.Rollback()
	if req.Enabled {
		task, err := s.enqueueTx(ctx, tx, serviceID, TaskTypeReconcile, TriggerUser, "Reconciling Container Service "+old.Name)
		if err != nil {
			return ContainerService{}, err
		}
		lastTaskID = task.ID
	}
	_, err = tx.ExecContext(ctx, `UPDATE container_services SET enabled=?,compose_service_yaml=?,variables_json=?,selector_json=?,generation=?,spec_revision=?,spec_hash=?,last_task_id=?,updated_at=? WHERE id=?`,
		boolInt(req.Enabled), req.ComposeServiceYAML, string(varsJSON), string(selectorJSON), generation, revision, hash, lastTaskID, ts(now), serviceID)
	if err != nil {
		return ContainerService{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContainerService{}, err
	}
	return s.Get(ctx, serviceID)
}

func (s *Service) Get(ctx context.Context, serviceID string) (ContainerService, error) {
	item, err := scanService(s.db.QueryRowContext(ctx, `SELECT id,name,enabled,compose_service_yaml,variables_json,selector_json,generation,spec_revision,spec_hash,last_error,last_task_id,created_at,updated_at FROM container_services WHERE id=?`, serviceID))
	if err == sql.ErrNoRows {
		return ContainerService{}, panelerr.NotFound("container_service")
	}
	return item, err
}

func (s *Service) List(ctx context.Context) ([]ContainerService, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,enabled,compose_service_yaml,variables_json,selector_json,generation,spec_revision,spec_hash,last_error,last_task_id,created_at,updated_at FROM container_services ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContainerService{}
	for rows.Next() {
		item, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) Validate(ctx context.Context, serviceID string, req SaveRequest) (ParsedServiceBody, error) {
	_ = ctx
	_ = serviceID
	return ParseServiceBody(req.ComposeServiceYAML)
}

func (s *Service) ValidateResult(ctx context.Context, serviceID string, req SaveRequest) (ValidationResult, error) {
	name := strings.TrimSpace(req.Name)
	if serviceID != "" && serviceID != "draft" {
		if old, err := s.Get(ctx, serviceID); err == nil {
			name = old.Name
			if strings.TrimSpace(req.ComposeServiceYAML) == "" {
				req.ComposeServiceYAML = old.ComposeServiceYAML
			}
		}
	}
	if name == "" {
		name = "preview"
	}
	parsed, err := s.validateSpec(ctx, serviceID, name, req.ComposeServiceYAML)
	if err != nil {
		return ValidationResult{}, err
	}
	return ValidationResult{Valid: true, DependencyNames: parsed.Dependencies}, nil
}

func (s *Service) RenderPreview(ctx context.Context, serviceID string, req SaveRequest) (RenderPreview, error) {
	item, err := s.serviceForPreview(ctx, serviceID, req)
	if err != nil {
		return RenderPreview{}, err
	}
	files, err := s.ListFiles(ctx, item.ID)
	if err != nil && item.ID != "draft" {
		return RenderPreview{}, err
	}
	renderFiles := make([]containerrender.FileInput, 0, len(files))
	for _, file := range files {
		renderFiles = append(renderFiles, containerrender.FileInput{Path: file.Path, Kind: file.Kind, Content: []byte(file.Content)})
	}
	out, err := containerrender.Render(containerrender.Input{
		ServiceID:          item.ID,
		ServiceName:        item.Name,
		NodeID:             "preview",
		Generation:         item.Generation,
		SpecRevision:       item.SpecRevision,
		PortClaims:         parsedOrEmptyClaims(item.ComposeServiceYAML),
		ComposeServiceYAML: item.ComposeServiceYAML,
		Variables:          item.Variables,
		Files:              renderFiles,
	})
	if err != nil {
		return RenderPreview{}, err
	}
	manifest, _ := json.MarshalIndent(out.Manifest, "", "  ")
	return RenderPreview{ComposeYAML: out.ComposeYAML, OverrideYAML: out.OverrideYAML, ManifestJSON: string(manifest)}, nil
}

func parsedOrEmptyClaims(body string) []int {
	parsed, err := ParseServiceBody(body)
	if err != nil {
		return nil
	}
	return parsed.PortClaims
}

func (s *Service) SchedulePreview(ctx context.Context, serviceID string, req SaveRequest) (SchedulePreview, error) {
	item, err := s.serviceForPreview(ctx, serviceID, req)
	if err != nil {
		return SchedulePreview{}, err
	}
	parsed, err := ParseServiceBody(item.ComposeServiceYAML)
	if err != nil {
		return SchedulePreview{}, err
	}
	all, err := s.List(ctx)
	if err != nil {
		return SchedulePreview{}, err
	}
	names := byName(all)
	issues := []ValidationIssue{}
	for _, dep := range parsed.Dependencies {
		if _, ok := names[dep]; !ok {
			issues = append(issues, ValidationIssue{Path: "depends_on", Message: "Dependency " + dep + " does not exist", Severity: "error"})
			continue
		}
		if !names[dep].Enabled {
			issues = append(issues, ValidationIssue{Path: "depends_on", Message: "Dependency " + dep + " is disabled", Severity: "error"})
		}
	}
	candidates, err := s.scheduleCandidates(ctx, item)
	if err != nil {
		return SchedulePreview{}, err
	}
	selected := ""
	for _, candidate := range candidates {
		if candidate.Eligible {
			selected = candidate.NodeID
			break
		}
	}
	return SchedulePreview{SelectedNodeID: selected, Candidates: candidates, Errors: issues}, nil
}

func (s *Service) Reconcile(ctx context.Context, serviceID string) (tasks.Task, error) {
	item, err := s.Get(ctx, serviceID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, item.ID, TaskTypeReconcile, TriggerUser, "Reconciling Container Service "+item.Name)
}

func (s *Service) Restart(ctx context.Context, serviceID string) (tasks.Task, error) {
	item, err := s.Get(ctx, serviceID)
	if err != nil {
		return tasks.Task{}, err
	}
	return s.enqueue(ctx, item.ID, TaskTypeRestart, TriggerUser, "Restarting Container Service "+item.Name)
}

func (s *Service) EnablePreview(ctx context.Context, serviceID string) (Preview, error) {
	target, err := s.Get(ctx, serviceID)
	if err != nil {
		return Preview{}, err
	}
	all, err := s.List(ctx)
	if err != nil {
		return Preview{}, err
	}
	byName := byName(all)
	seen := map[string]bool{}
	visiting := map[string]bool{}
	out := []ContainerService{}
	var visit func(ContainerService) error
	visit = func(item ContainerService) error {
		if visiting[item.Name] {
			return panelerr.Validation("container_service_dependency_cycle", "Container Service dependency cycle detected")
		}
		if seen[item.Name] {
			return nil
		}
		visiting[item.Name] = true
		defer delete(visiting, item.Name)
		seen[item.Name] = true
		parsed, err := ParseServiceBody(item.ComposeServiceYAML)
		if err != nil {
			return err
		}
		for _, dep := range parsed.Dependencies {
			next, ok := byName[dep]
			if !ok {
				return panelerr.Validation("container_service_dependency_missing", "Dependency "+dep+" does not exist")
			}
			if err := visit(next); err != nil {
				return err
			}
		}
		out = append(out, item)
		return nil
	}
	if err := visit(target); err != nil {
		return Preview{}, err
	}
	return Preview{Operation: "enable", TargetServiceID: target.ID, TargetServiceName: target.Name, AffectedServices: out, Services: out}, nil
}

func (s *Service) Enable(ctx context.Context, serviceID string) (Preview, error) {
	preview, err := s.EnablePreview(ctx, serviceID)
	if err != nil {
		return Preview{}, err
	}
	opID := id.New("op")
	created := []tasks.Task{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Preview{}, err
	}
	defer tx.Rollback()
	for _, item := range preview.Services {
		_, err := tx.ExecContext(ctx, `UPDATE container_services SET enabled=1,updated_at=? WHERE id=?`, ts(time.Now().UTC()), item.ID)
		if err != nil {
			return Preview{}, err
		}
		task, err := s.tasks.CreateTx(ctx, tx, tasks.CreateInput{OperationID: opID, Type: TaskTypeEnable, ResourceType: ResourceTypeContainerService, ResourceID: item.ID, TriggerType: "service_enable", Summary: "Enabling Container Service " + item.Name})
		if err != nil {
			return Preview{}, err
		}
		created = append(created, task)
	}
	if err := tx.Commit(); err != nil {
		return Preview{}, err
	}
	preview.OperationID = opID
	preview.Tasks = created
	preview.ExpectedTasks = created
	return preview, nil
}

func (s *Service) DisablePreview(ctx context.Context, serviceID string) (Preview, error) {
	target, err := s.Get(ctx, serviceID)
	if err != nil {
		return Preview{}, err
	}
	all, err := s.List(ctx)
	if err != nil {
		return Preview{}, err
	}
	out := []ContainerService{}
	seen := map[string]bool{}
	var visitDependents func(ContainerService) error
	visitDependents = func(item ContainerService) error {
		for _, candidate := range all {
			parsed, err := ParseServiceBody(candidate.ComposeServiceYAML)
			if err != nil {
				return err
			}
			for _, dep := range parsed.Dependencies {
				if dep == item.Name {
					if err := visitDependents(candidate); err != nil {
						return err
					}
				}
			}
		}
		if !seen[item.ID] {
			seen[item.ID] = true
			out = append(out, item)
		}
		return nil
	}
	if err := visitDependents(target); err != nil {
		return Preview{}, err
	}
	return Preview{Operation: "disable", TargetServiceID: target.ID, TargetServiceName: target.Name, AffectedServices: out, Services: out}, nil
}

func (s *Service) Disable(ctx context.Context, serviceID string) (Preview, error) {
	preview, err := s.DisablePreview(ctx, serviceID)
	if err != nil {
		return Preview{}, err
	}
	opID := id.New("op")
	created := []tasks.Task{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Preview{}, err
	}
	defer tx.Rollback()
	for _, item := range preview.Services {
		if _, err := tx.ExecContext(ctx, `UPDATE container_services SET enabled=0,updated_at=? WHERE id=?`, ts(time.Now().UTC()), item.ID); err != nil {
			return Preview{}, err
		}
		task, err := s.tasks.CreateTx(ctx, tx, tasks.CreateInput{OperationID: opID, Type: TaskTypeDisable, ResourceType: ResourceTypeContainerService, ResourceID: item.ID, TriggerType: "service_disable", Summary: "Disabling Container Service " + item.Name})
		if err != nil {
			return Preview{}, err
		}
		created = append(created, task)
	}
	if err := tx.Commit(); err != nil {
		return Preview{}, err
	}
	preview.OperationID = opID
	preview.Tasks = created
	preview.ExpectedTasks = created
	return preview, nil
}

func (s *Service) Delete(ctx context.Context, serviceID string) (tasks.Task, error) {
	item, err := s.Get(ctx, serviceID)
	if err != nil {
		return tasks.Task{}, err
	}
	if item.Enabled {
		return tasks.Task{}, panelerr.Conflict("container_service_enabled", "Disable the Container Service before deleting it")
	}
	all, err := s.List(ctx)
	if err != nil {
		return tasks.Task{}, err
	}
	for _, candidate := range all {
		if candidate.ID == item.ID {
			continue
		}
		parsed, err := ParseServiceBody(candidate.ComposeServiceYAML)
		if err != nil {
			return tasks.Task{}, err
		}
		for _, dep := range parsed.Dependencies {
			if dep == item.Name {
				return tasks.Task{}, panelerr.Conflict("container_service_has_dependents", "Container Service has dependents")
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return tasks.Task{}, err
	}
	defer tx.Rollback()
	task, err := s.enqueueTx(ctx, tx, item.ID, TaskTypeDelete, TriggerUser, "Deleting Container Service "+item.Name)
	if err != nil {
		return tasks.Task{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE container_services SET last_task_id=? WHERE id=?`, task.ID, item.ID); err != nil {
		return tasks.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return tasks.Task{}, err
	}
	return task, nil
}

func (s *Service) DeleteRecord(ctx context.Context, serviceID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM container_services WHERE id=?`, serviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("container_service")
	}
	return nil
}

func (s *Service) validateSpec(ctx context.Context, serviceID, name, body string) (ParsedServiceBody, error) {
	if err := ValidateName(name); err != nil {
		return ParsedServiceBody{}, err
	}
	parsed, err := ParseServiceBody(body)
	if err != nil {
		return ParsedServiceBody{}, err
	}
	graph, err := s.dependencyGraph(ctx, serviceID, name, parsed.Dependencies)
	if err != nil {
		return ParsedServiceBody{}, err
	}
	if err := ValidateDependencyGraph(name, graph); err != nil {
		return ParsedServiceBody{}, err
	}
	return parsed, nil
}

func (s *Service) dependencyGraph(ctx context.Context, serviceID, name string, deps []string) (map[string][]string, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	graph := map[string][]string{}
	for _, item := range all {
		if item.ID == serviceID {
			continue
		}
		if item.Name == name {
			return nil, panelerr.Conflict("container_service_name_exists", "Container Service name already exists")
		}
		parsed, err := ParseServiceBody(item.ComposeServiceYAML)
		if err != nil {
			return nil, err
		}
		graph[item.Name] = parsed.Dependencies
	}
	graph[name] = deps
	return graph, nil
}

func (s *Service) Runtime(ctx context.Context, serviceID string) (RuntimeStatus, error) {
	item, err := s.Get(ctx, serviceID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	placement, hasPlacement, err := s.Placement(ctx, serviceID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	query := `SELECT s.id,s.name,c.payload,c.refreshed_at FROM docker_runtime_cache c JOIN servers s ON s.id=c.server_id WHERE c.resource='services'`
	args := []any{}
	if hasPlacement {
		query += ` AND s.id=?`
		args = append(args, placement.NodeID)
	}
	query += ` ORDER BY s.name ASC,s.id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return RuntimeStatus{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, nodeName, payload, refreshedAt string
		if err := rows.Scan(&nodeID, &nodeName, &payload, &refreshedAt); err != nil {
			return RuntimeStatus{}, err
		}
		containers := []runtimeCacheContainer{}
		if err := json.Unmarshal([]byte(payload), &containers); err != nil {
			return RuntimeStatus{}, err
		}
		for _, container := range containers {
			labels := nonNil(container.Labels)
			if labels["panel.service.id"] != item.ID && labels["panel.service.name"] != item.Name {
				continue
			}
			observedAt, _ := time.Parse(time.RFC3339Nano, refreshedAt)
			status := normalizeRuntimeStatus(container.State, container.Status)
			return RuntimeStatus{
				ServiceID:            item.ID,
				ServiceName:          item.Name,
				Status:               status,
				NodeID:               nodeID,
				NodeName:             nodeName,
				ObservedGeneration:   intPointerFromLabel(labels["panel.service.generation"]),
				ObservedSpecRevision: labels["panel.service.spec_revision"],
				Labels:               labels,
				Ports:                splitPorts(container.Ports),
				ContainerID:          container.ID,
				Managed:              container.Managed || labels["panel.managed"] == "true",
				ObservedAt:           &observedAt,
			}, nil
		}
	}
	if err := rows.Err(); err != nil {
		return RuntimeStatus{}, err
	}
	return RuntimeStatus{ServiceID: item.ID, ServiceName: item.Name, Status: "missing"}, nil
}

func (s *Service) Placement(ctx context.Context, serviceID string) (Placement, bool, error) {
	var p Placement
	var updated string
	err := s.db.QueryRowContext(ctx, `SELECT p.service_id,p.node_id,s.name,p.generation,p.spec_revision,p.container_id,p.status,p.updated_at FROM container_service_placements p JOIN servers s ON s.id=p.node_id WHERE p.service_id=?`, serviceID).
		Scan(&p.ServiceID, &p.NodeID, &p.NodeName, &p.Generation, &p.SpecRevision, &p.ContainerID, &p.Status, &updated)
	if err == sql.ErrNoRows {
		return Placement{}, false, nil
	}
	if err != nil {
		return Placement{}, false, err
	}
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return p, true, nil
}

func (s *Service) SetPlacement(ctx context.Context, p Placement) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO container_service_placements(service_id,node_id,generation,spec_revision,container_id,status,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(service_id) DO UPDATE SET node_id=excluded.node_id,generation=excluded.generation,spec_revision=excluded.spec_revision,container_id=excluded.container_id,status=excluded.status,updated_at=excluded.updated_at`,
		p.ServiceID, p.NodeID, p.Generation, p.SpecRevision, p.ContainerID, p.Status, now)
	return err
}

func (s *Service) ClearPlacement(ctx context.Context, serviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM container_service_placements WHERE service_id=?`, serviceID)
	return err
}

func (s *Service) Logs(ctx context.Context, serviceID string, tail int) ([]string, error) {
	item, err := s.Get(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	placement, ok, err := s.Placement(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, panelerr.Conflict("container_service_runtime_missing", "Container Service has no placement; deploy it before reading logs")
	}
	if tail < 0 {
		tail = 0
	}
	if tail == 0 {
		tail = 200
	}
	if s.logReader == nil {
		return nil, panelerr.Conflict("container_service_logs_unavailable", "Container Service live logs are unavailable")
	}
	return s.logReader.ReadContainerLogs(ctx, placement.NodeID, item.Name, tail)
}

type runtimeCacheContainer struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	State   string            `json:"state"`
	Status  string            `json:"status"`
	Ports   string            `json:"ports"`
	Labels  map[string]string `json:"labels"`
	Managed bool              `json:"managed"`
}

func (s *Service) ListFiles(ctx context.Context, serviceID string) ([]File, error) {
	if _, err := s.Get(ctx, serviceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,service_id,path,kind,content_type,size,sha256,content,created_at,updated_at FROM container_service_files WHERE service_id=? ORDER BY path ASC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		file, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func (s *Service) CreateFile(ctx context.Context, serviceID string, in FileInput) (File, error) {
	rel, err := cleanFilePath(in.Path)
	if err != nil {
		return File{}, panelerr.Validation("container_service_file_path_invalid", "Service file path must be a safe relative path")
	}
	if in.Kind != containerrender.FileKindBinary && in.Kind != containerrender.FileKindTemplate {
		return File{}, panelerr.Validation("container_service_file_kind_invalid", "Service file kind must be binary or template")
	}
	now := time.Now().UTC()
	content := []byte(in.Content)
	sum := sha256.Sum256(content)
	file := File{ID: id.New("csfile"), ServiceID: serviceID, Path: rel, Kind: in.Kind, ContentType: in.ContentType, Size: len(content), SHA256: hex.EncodeToString(sum[:]), Content: in.Content, CreatedAt: now, UpdatedAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()
	if _, err := s.getTx(ctx, tx, serviceID); err != nil {
		return File{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO container_service_files(id,service_id,path,kind,content_type,size,sha256,content,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		file.ID, file.ServiceID, file.Path, file.Kind, file.ContentType, file.Size, file.SHA256, content, ts(now), ts(now))
	if err != nil {
		return File{}, err
	}
	if err := s.bumpGenerationForFileChangeTx(ctx, tx, serviceID, now); err != nil {
		return File{}, err
	}
	if err := tx.Commit(); err != nil {
		return File{}, err
	}
	return file, nil
}

func (s *Service) UpdateFile(ctx context.Context, serviceID, fileID string, in FileInput) (File, error) {
	rel, err := cleanFilePath(in.Path)
	if err != nil {
		return File{}, panelerr.Validation("container_service_file_path_invalid", "Service file path must be a safe relative path")
	}
	if in.Kind != containerrender.FileKindBinary && in.Kind != containerrender.FileKindTemplate {
		return File{}, panelerr.Validation("container_service_file_kind_invalid", "Service file kind must be binary or template")
	}
	now := time.Now().UTC()
	content := []byte(in.Content)
	sum := sha256.Sum256(content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback()
	if _, err := s.getTx(ctx, tx, serviceID); err != nil {
		return File{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE container_service_files SET path=?,kind=?,content_type=?,size=?,sha256=?,content=?,updated_at=? WHERE id=? AND service_id=?`,
		rel, in.Kind, in.ContentType, len(content), hex.EncodeToString(sum[:]), content, ts(now), fileID, serviceID)
	if err != nil {
		return File{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return File{}, panelerr.NotFound("container_service_file")
	}
	if err := s.bumpGenerationForFileChangeTx(ctx, tx, serviceID, now); err != nil {
		return File{}, err
	}
	file, err := s.getFileTx(ctx, tx, serviceID, fileID)
	if err != nil {
		return File{}, err
	}
	if err := tx.Commit(); err != nil {
		return File{}, err
	}
	return file, nil
}

func (s *Service) DeleteFile(ctx context.Context, serviceID, fileID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := s.getTx(ctx, tx, serviceID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM container_service_files WHERE id=? AND service_id=?`, fileID, serviceID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("container_service_file")
	}
	if err := s.bumpGenerationForFileChangeTx(ctx, tx, serviceID, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) enqueue(ctx context.Context, serviceID, taskType, trigger, summary string) (tasks.Task, error) {
	return s.tasks.Create(ctx, tasks.CreateInput{
		OperationID:         id.New("op"),
		Type:                taskType,
		ResourceType:        ResourceTypeContainerService,
		ResourceID:          serviceID,
		TriggerType:         trigger,
		TriggerResourceType: ResourceTypeContainerService,
		TriggerResourceID:   serviceID,
		Summary:             summary,
	})
}

func (s *Service) enqueueTx(ctx context.Context, tx *sql.Tx, serviceID, taskType, trigger, summary string) (tasks.Task, error) {
	return s.tasks.CreateTx(ctx, tx, tasks.CreateInput{
		OperationID:         id.New("op"),
		Type:                taskType,
		ResourceType:        ResourceTypeContainerService,
		ResourceID:          serviceID,
		TriggerType:         trigger,
		TriggerResourceType: ResourceTypeContainerService,
		TriggerResourceID:   serviceID,
		Summary:             summary,
	})
}

func dependencies(raw any) ([]string, error) {
	out := []string{}
	switch v := raw.(type) {
	case nil:
	case []any:
		for _, item := range v {
			name := strings.TrimSpace(fmt.Sprint(item))
			if name == "" {
				continue
			}
			if err := ValidateName(name); err != nil {
				return nil, err
			}
			out = append(out, name)
		}
	case map[string]any:
		for name := range v {
			name = strings.TrimSpace(name)
			if err := ValidateName(name); err != nil {
				return nil, err
			}
			out = append(out, name)
		}
		sort.Strings(out)
	default:
		return nil, panelerr.Validation("depends_on_invalid", "depends_on must use Compose short or long syntax")
	}
	return out, nil
}

func portClaims(raw any, label string, hostMode bool) ([]int, error) {
	if hostMode {
		return parseClaimLabel(label)
	}
	out := []int{}
	switch v := raw.(type) {
	case nil:
	case []any:
		for _, item := range v {
			if port, ok := hostPort(fmt.Sprint(item)); ok {
				out = append(out, port)
			}
		}
	}
	return out, nil
}

func parseClaimLabel(label string) ([]int, error) {
	if strings.TrimSpace(label) == "" {
		return []int{}, nil
	}
	parts := strings.Split(label, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		port, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || port <= 0 || port > 65535 {
			return nil, panelerr.Validation("port_claim_invalid", "panel.claims.ports must contain TCP port numbers")
		}
		out = append(out, port)
	}
	return out, nil
}

func hostPort(s string) (int, bool) {
	s = strings.Trim(s, `"'`)
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return 0, false
	}
	candidate := parts[len(parts)-2]
	if strings.Contains(candidate, ".") {
		candidate = parts[len(parts)-3]
	}
	port, err := strconv.Atoi(candidate)
	return port, err == nil && port > 0 && port <= 65535
}

func composeLabels(raw any) map[string]string {
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]any:
		for key, value := range v {
			out[key] = fmt.Sprint(value)
		}
	case []any:
		for _, item := range v {
			key, value, ok := strings.Cut(fmt.Sprint(item), "=")
			if ok {
				out[strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
	return out
}

func specHash(compose string, vars map[string]string) string {
	payload, _ := json.Marshal(struct {
		Compose string            `json:"compose"`
		Vars    map[string]string `json:"vars"`
	}{compose, vars})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func specHashWithFiles(compose string, vars map[string]string, files []File) string {
	type fileHash struct {
		Path   string `json:"path"`
		Kind   string `json:"kind"`
		SHA256 string `json:"sha256"`
	}
	fileHashes := make([]fileHash, 0, len(files))
	for _, file := range files {
		fileHashes = append(fileHashes, fileHash{Path: file.Path, Kind: file.Kind, SHA256: file.SHA256})
	}
	sort.Slice(fileHashes, func(i, j int) bool {
		return fileHashes[i].Path < fileHashes[j].Path
	})
	payload, _ := json.Marshal(struct {
		Compose string            `json:"compose"`
		Vars    map[string]string `json:"vars"`
		Files   []fileHash        `json:"files"`
	}{compose, vars, fileHashes})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func scanService(row interface{ Scan(...any) error }) (ContainerService, error) {
	var item ContainerService
	var enabled int
	var vars, selector, created, updated string
	if err := row.Scan(&item.ID, &item.Name, &enabled, &item.ComposeServiceYAML, &vars, &selector, &item.Generation, &item.SpecRevision, &item.SpecHash, &item.LastError, &item.LastTaskID, &created, &updated); err != nil {
		return ContainerService{}, err
	}
	item.Enabled = enabled == 1
	item.Variables = map[string]string{}
	item.Selector = map[string]string{}
	_ = json.Unmarshal([]byte(vars), &item.Variables)
	_ = json.Unmarshal([]byte(selector), &item.Selector)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func byName(items []ContainerService) map[string]ContainerService {
	out := map[string]ContainerService{}
	for _, item := range items {
		out[item.Name] = item
	}
	return out
}

func (s *Service) scheduleCandidates(ctx context.Context, item ContainerService) ([]ScheduleCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,host,traits,os_supported,reachable FROM servers ORDER BY name ASC,id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScheduleCandidate{}
	for rows.Next() {
		var id, name, host, traitsJSON string
		var osSupported, reachable int
		if err := rows.Scan(&id, &name, &host, &traitsJSON, &osSupported, &reachable); err != nil {
			return nil, err
		}
		traits := map[string]string{}
		_ = json.Unmarshal([]byte(traitsJSON), &traits)
		reasons := []string{}
		if reachable != 1 {
			reasons = append(reasons, "node is unreachable")
		}
		if osSupported != 1 {
			reasons = append(reasons, "node OS is unsupported")
		}
		if !selectorMatchesCandidate(item.Selector, id, name, host, traits) {
			reasons = append(reasons, "selector mismatch")
		}
		if ok, _ := s.nodeDockerSupported(ctx, id); !ok {
			reasons = append(reasons, "Docker, Compose, or Compose include is unavailable")
		}
		out = append(out, ScheduleCandidate{NodeID: id, NodeName: name, Eligible: len(reasons) == 0, Reasons: reasons})
	}
	return out, rows.Err()
}

func selectorMatchesCandidate(selector map[string]string, id, name, host string, traits map[string]string) bool {
	for key, want := range selector {
		switch key {
		case "id":
			if id != want {
				return false
			}
		case "name":
			if name != want {
				return false
			}
		case "host":
			if host != want {
				return false
			}
		default:
			if traits[key] != want {
				return false
			}
		}
	}
	return true
}

func (s *Service) nodeDockerSupported(ctx context.Context, nodeID string) (bool, error) {
	var docker, compose, include, supported int
	err := s.db.QueryRowContext(ctx, `SELECT docker_installed,compose_installed,include_supported,supported FROM docker_capabilities WHERE server_id=?`, nodeID).Scan(&docker, &compose, &include, &supported)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return docker == 1 && compose == 1 && include == 1 && supported == 1, err
}

func nonNil(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func normalizeRuntimeStatus(state, status string) string {
	state = strings.ToLower(strings.TrimSpace(state))
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case state == "running":
		return "running"
	case state == "exited" || state == "dead":
		return "exited"
	case state != "":
		return state
	case strings.Contains(status, "up"):
		return "running"
	case strings.Contains(status, "exited"):
		return "exited"
	default:
		return "unknown"
	}
}

func intPointerFromLabel(value string) *int {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &n
}

func splitPorts(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func ts(t time.Time) string { return t.Format(time.RFC3339Nano) }

func (s *Service) serviceForPreview(ctx context.Context, serviceID string, req SaveRequest) (ContainerService, error) {
	if serviceID != "" && serviceID != "draft" {
		old, err := s.Get(ctx, serviceID)
		if err == nil {
			if strings.TrimSpace(req.ComposeServiceYAML) == "" {
				req.ComposeServiceYAML = old.ComposeServiceYAML
			}
			if req.Variables == nil {
				req.Variables = old.Variables
			}
			return ContainerService{ID: old.ID, Name: old.Name, Enabled: req.Enabled, ComposeServiceYAML: req.ComposeServiceYAML, Variables: nonNil(req.Variables), Selector: nonNil(req.Selector), Generation: old.Generation, SpecRevision: old.SpecRevision, SpecHash: old.SpecHash}, nil
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "preview"
	}
	if err := ValidateName(name); err != nil {
		return ContainerService{}, err
	}
	if _, err := ParseServiceBody(req.ComposeServiceYAML); err != nil {
		return ContainerService{}, err
	}
	hash := specHash(req.ComposeServiceYAML, nonNil(req.Variables))
	return ContainerService{ID: "draft", Name: name, Enabled: req.Enabled, ComposeServiceYAML: req.ComposeServiceYAML, Variables: nonNil(req.Variables), Selector: nonNil(req.Selector), Generation: 1, SpecRevision: hash[:16], SpecHash: hash}, nil
}

func (s *Service) bumpGenerationForFileChange(ctx context.Context, serviceID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.bumpGenerationForFileChangeTx(ctx, tx, serviceID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) bumpGenerationForFileChangeTx(ctx context.Context, tx *sql.Tx, serviceID string, now time.Time) error {
	item, err := s.getTx(ctx, tx, serviceID)
	if err != nil {
		return err
	}
	files, err := s.listFilesTx(ctx, tx, serviceID)
	if err != nil {
		return err
	}
	hash := specHashWithFiles(item.ComposeServiceYAML, item.Variables, files)
	if hash == item.SpecHash {
		return nil
	}
	lastTaskID := item.LastTaskID
	if item.Enabled {
		task, err := s.enqueueTx(ctx, tx, serviceID, TaskTypeReconcile, TriggerUser, "Reconciling Container Service "+item.Name)
		if err != nil {
			return err
		}
		lastTaskID = task.ID
	}
	_, err = tx.ExecContext(ctx, `UPDATE container_services SET generation=generation+1,spec_revision=?,spec_hash=?,last_task_id=?,updated_at=? WHERE id=?`, hash[:16], hash, lastTaskID, ts(now), serviceID)
	if err != nil {
		return err
	}
	return nil
}

func (s *Service) getFile(ctx context.Context, serviceID, fileID string) (File, error) {
	file, err := scanFile(s.db.QueryRowContext(ctx, `SELECT id,service_id,path,kind,content_type,size,sha256,content,created_at,updated_at FROM container_service_files WHERE id=? AND service_id=?`, fileID, serviceID))
	if err == sql.ErrNoRows {
		return File{}, panelerr.NotFound("container_service_file")
	}
	return file, err
}

func (s *Service) getTx(ctx context.Context, tx *sql.Tx, serviceID string) (ContainerService, error) {
	item, err := scanService(tx.QueryRowContext(ctx, `SELECT id,name,enabled,compose_service_yaml,variables_json,selector_json,generation,spec_revision,spec_hash,last_error,last_task_id,created_at,updated_at FROM container_services WHERE id=?`, serviceID))
	if err == sql.ErrNoRows {
		return ContainerService{}, panelerr.NotFound("container_service")
	}
	return item, err
}

func (s *Service) listFilesTx(ctx context.Context, tx *sql.Tx, serviceID string) ([]File, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,service_id,path,kind,content_type,size,sha256,content,created_at,updated_at FROM container_service_files WHERE service_id=? ORDER BY path ASC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		file, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func (s *Service) getFileTx(ctx context.Context, tx *sql.Tx, serviceID, fileID string) (File, error) {
	file, err := scanFile(tx.QueryRowContext(ctx, `SELECT id,service_id,path,kind,content_type,size,sha256,content,created_at,updated_at FROM container_service_files WHERE id=? AND service_id=?`, fileID, serviceID))
	if err == sql.ErrNoRows {
		return File{}, panelerr.NotFound("container_service_file")
	}
	return file, err
}

func scanFile(row interface{ Scan(...any) error }) (File, error) {
	var file File
	var content []byte
	var created, updated string
	if err := row.Scan(&file.ID, &file.ServiceID, &file.Path, &file.Kind, &file.ContentType, &file.Size, &file.SHA256, &content, &created, &updated); err != nil {
		return File{}, err
	}
	file.Content = string(content)
	file.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	file.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return file, nil
}

func cleanFilePath(p string) (string, error) {
	p = path.Clean(strings.TrimSpace(strings.ReplaceAll(p, "\\", "/")))
	if p == "." || p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || strings.Contains(p, ":") {
		return "", fmt.Errorf("invalid file path")
	}
	return p, nil
}

func maskTemplateActions(source string) string {
	var b strings.Builder
	for {
		start := strings.Index(source, "{{")
		if start < 0 {
			b.WriteString(source)
			return b.String()
		}
		b.WriteString(source[:start])
		rest := source[start+2:]
		end := strings.Index(rest, "}}")
		if end < 0 {
			b.WriteString(source[start:])
			return b.String()
		}
		action := strings.TrimSpace(strings.Trim(rest[:end], "-"))
		switch {
		case strings.HasPrefix(action, "if "), strings.HasPrefix(action, "range "), strings.HasPrefix(action, "with "), action == "else", strings.HasPrefix(action, "else "), action == "end":
		default:
			b.WriteString("panel_template_value")
		}
		source = rest[end+2:]
	}
}
