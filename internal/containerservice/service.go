package containerservice

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/id"
	"panel/internal/panelerr"
	"panel/internal/tasks"

	"gopkg.in/yaml.v3"
)

var serviceNameRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

type Service struct {
	db    *sql.DB
	tasks *tasks.Service
}

func NewService(db *sql.DB, taskSvc *tasks.Service) *Service {
	return &Service{db: db, tasks: taskSvc}
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
	forbiddenTop := map[string]bool{"services": true, "include": true, "volumes": true, "networks": true}
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
	if _, err := ParseServiceBody(req.ComposeServiceYAML); err != nil {
		return ContainerService{}, err
	}
	vars := nonNil(req.Variables)
	selector := nonNil(req.Selector)
	hash := specHash(req.ComposeServiceYAML, vars)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO container_services(id,name,enabled,compose_service_yaml,variables_json,selector_json,generation,spec_revision,spec_hash,last_error,last_task_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Name, boolInt(item.Enabled), item.ComposeServiceYAML, string(varsJSON), string(selectorJSON), item.Generation, item.SpecRevision, item.SpecHash, "", "", ts(now), ts(now))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ContainerService{}, panelerr.Conflict("container_service_name_exists", "Container Service name already exists")
		}
		return ContainerService{}, err
	}
	if item.Enabled {
		task, err := s.enqueue(ctx, item.ID, TaskTypeReconcile, TriggerUser, "Reconciling Container Service "+item.Name)
		if err != nil {
			return ContainerService{}, err
		}
		item.LastTaskID = task.ID
		_, _ = s.db.ExecContext(ctx, `UPDATE container_services SET last_task_id=? WHERE id=?`, task.ID, item.ID)
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
	if _, err := ParseServiceBody(req.ComposeServiceYAML); err != nil {
		return ContainerService{}, err
	}
	vars := nonNil(req.Variables)
	selector := nonNil(req.Selector)
	hash := specHash(req.ComposeServiceYAML, vars)
	generation := old.Generation
	if hash != old.SpecHash {
		generation++
	}
	now := time.Now().UTC()
	varsJSON, _ := json.Marshal(vars)
	selectorJSON, _ := json.Marshal(selector)
	revision := hash[:16]
	var lastTaskID string
	if req.Enabled {
		task, err := s.enqueue(ctx, serviceID, TaskTypeReconcile, TriggerUser, "Reconciling Container Service "+old.Name)
		if err != nil {
			return ContainerService{}, err
		}
		lastTaskID = task.ID
	}
	_, err = s.db.ExecContext(ctx, `UPDATE container_services SET enabled=?,compose_service_yaml=?,variables_json=?,selector_json=?,generation=?,spec_revision=?,spec_hash=?,last_task_id=?,updated_at=? WHERE id=?`,
		boolInt(req.Enabled), req.ComposeServiceYAML, string(varsJSON), string(selectorJSON), generation, revision, hash, lastTaskID, ts(now), serviceID)
	if err != nil {
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
	out := []ContainerService{}
	var visit func(ContainerService) error
	visit = func(item ContainerService) error {
		if seen[item.Name] {
			return nil
		}
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
	return Preview{Services: out}, nil
}

func (s *Service) Enable(ctx context.Context, serviceID string) (Preview, error) {
	preview, err := s.EnablePreview(ctx, serviceID)
	if err != nil {
		return Preview{}, err
	}
	opID := id.New("op")
	for _, item := range preview.Services {
		_, err := s.db.ExecContext(ctx, `UPDATE container_services SET enabled=1,updated_at=? WHERE id=?`, ts(time.Now().UTC()), item.ID)
		if err != nil {
			return Preview{}, err
		}
		if _, err := s.tasks.Create(ctx, tasks.CreateInput{OperationID: opID, Type: TaskTypeEnable, ResourceType: ResourceTypeContainerService, ResourceID: item.ID, TriggerType: "service_enable", Summary: "Enabling Container Service " + item.Name}); err != nil {
			return Preview{}, err
		}
	}
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
	return Preview{Services: out}, nil
}

func (s *Service) Disable(ctx context.Context, serviceID string) (Preview, error) {
	preview, err := s.DisablePreview(ctx, serviceID)
	if err != nil {
		return Preview{}, err
	}
	opID := id.New("op")
	for _, item := range preview.Services {
		if _, err := s.db.ExecContext(ctx, `UPDATE container_services SET enabled=0,updated_at=? WHERE id=?`, ts(time.Now().UTC()), item.ID); err != nil {
			return Preview{}, err
		}
		if _, err := s.tasks.Create(ctx, tasks.CreateInput{OperationID: opID, Type: TaskTypeDisable, ResourceType: ResourceTypeContainerService, ResourceID: item.ID, TriggerType: "service_disable", Summary: "Disabling Container Service " + item.Name}); err != nil {
			return Preview{}, err
		}
	}
	return preview, nil
}

func (s *Service) Delete(ctx context.Context, serviceID string) error {
	item, err := s.Get(ctx, serviceID)
	if err != nil {
		return err
	}
	if item.Enabled {
		return panelerr.Conflict("container_service_enabled", "Disable the Container Service before deleting it")
	}
	all, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range all {
		if candidate.ID == item.ID {
			continue
		}
		parsed, err := ParseServiceBody(candidate.ComposeServiceYAML)
		if err != nil {
			return err
		}
		for _, dep := range parsed.Dependencies {
			if dep == item.Name {
				return panelerr.Conflict("container_service_has_dependents", "Container Service has dependents")
			}
		}
	}
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

func (s *Service) Runtime(ctx context.Context, serviceID string) (RuntimeStatus, error) {
	if _, err := s.Get(ctx, serviceID); err != nil {
		return RuntimeStatus{}, err
	}
	return RuntimeStatus{ServiceID: serviceID, Status: "missing"}, nil
}

func (s *Service) Logs(ctx context.Context, serviceID string, tail int) ([]string, error) {
	_ = tail
	if _, err := s.Get(ctx, serviceID); err != nil {
		return nil, err
	}
	return []string{}, nil
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

func ts(t time.Time) string { return t.Format(time.RFC3339Nano) }

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
