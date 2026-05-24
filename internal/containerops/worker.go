package containerops

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"panel/internal/containerrender"
	"panel/internal/containerservice"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

const (
	defaultRootDir = "/opt/panel/container-services"
	defaultProject = "panel_managed"
)

type Worker struct {
	tasks    *tasks.Service
	locks    *LeaseService
	services *containerservice.Service
	servers  *server.Service
	exec     sshx.RemoteExecutor
}

func NewWorker(taskSvc *tasks.Service, locks *LeaseService, serviceSvc *containerservice.Service, serverSvc *server.Service, exec sshx.RemoteExecutor) *Worker {
	return &Worker{tasks: taskSvc, locks: locks, services: serviceSvc, servers: serverSvc, exec: exec}
}

func (w *Worker) RunDue(ctx context.Context) error {
	queued, err := w.tasks.List(ctx, tasks.ListFilter{Status: tasks.StatusQueued, Limit: 50})
	if err != nil {
		return err
	}
	for _, task := range queued.Items {
		if !isContainerTask(task.Type) {
			continue
		}
		if err := w.RunNow(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) RunNow(ctx context.Context, task tasks.Task) error {
	switch task.Type {
	case containerservice.TaskTypeReconcile:
		go w.runReconcile(context.Background(), task)
		return nil
	case containerservice.TaskTypeRestart:
		go w.runRestart(context.Background(), task)
		return nil
	case containerservice.TaskTypeEnable:
		go w.runEnable(context.Background(), task)
		return nil
	case containerservice.TaskTypeDisable:
		go w.runDisable(context.Background(), task)
		return nil
	case containerservice.TaskTypeDelete:
		go w.runDelete(context.Background(), task)
		return nil
	default:
		return panelerr.Validation("container_task_unsupported", "This Container Services task type is not supported")
	}
}

func (w *Worker) runDelete(ctx context.Context, task tasks.Task) {
	if err := w.runWithServiceLock(ctx, task, func(ctx context.Context, item containerservice.ContainerService) error {
		node, ok, err := w.placedNode(ctx, item.ID)
		if err != nil {
			return err
		}
		if ok {
			if err := w.withNodeLock(ctx, task, node.ID, func(ctx context.Context) error {
				_ = w.step(ctx, task.ID, "compose_remove", tasks.StatusRunning, 35, "")
				cmd := fmt.Sprintf("cd %s && docker compose -p %s -f root.compose.yaml rm -sf %s || true", shellQuote(defaultRootDir), shellQuote(defaultProject), shellQuote(item.Name))
				if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: 2 * time.Minute}); err != nil {
					return err
				}
				if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: "rm -rf " + shellQuote(defaultRootDir+"/"+item.Name), Timeout: 2 * time.Minute}); err != nil {
					return err
				}
				if err := w.services.ClearPlacement(ctx, item.ID); err != nil {
					return err
				}
				if err := w.refreshRootCompose(ctx, task, node); err != nil {
					return err
				}
				_ = w.step(ctx, task.ID, "compose_remove", tasks.StatusCompleted, 100, "")
				return nil
			}); err != nil {
				return err
			}
		}
		return w.services.DeleteRecord(ctx, item.ID)
	}); err != nil {
		_ = w.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = w.tasks.Complete(ctx, task.ID, "Container Service deleted")
}

func (w *Worker) runEnable(ctx context.Context, task tasks.Task) {
	w.runReconcile(ctx, task)
}

func (w *Worker) runDisable(ctx context.Context, task tasks.Task) {
	if err := w.runWithServiceLock(ctx, task, func(ctx context.Context, item containerservice.ContainerService) error {
		node, ok, err := w.placedNode(ctx, item.ID)
		if err != nil || !ok {
			_ = w.step(ctx, task.ID, "schedule", tasks.StatusCompleted, 100, "No node runtime to remove")
			_ = w.services.ClearPlacement(ctx, item.ID)
			return nil
		}
		if err := w.withNodeLock(ctx, task, node.ID, func(ctx context.Context) error {
			_ = w.step(ctx, task.ID, "compose_down", tasks.StatusRunning, 40, "")
			cmd := fmt.Sprintf("cd %s && docker compose -p %s -f root.compose.yaml rm -sf %s || true", shellQuote(defaultRootDir), shellQuote(defaultProject), shellQuote(item.Name))
			if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: 2 * time.Minute}); err != nil {
				return err
			}
			_ = w.step(ctx, task.ID, "compose_down", tasks.StatusCompleted, 100, "")
			if err := w.refreshRootCompose(ctx, task, node); err != nil {
				return err
			}
			if err := w.services.ClearPlacement(ctx, item.ID); err != nil {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = w.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = w.tasks.Complete(ctx, task.ID, "Container Service disabled")
}

func (w *Worker) runRestart(ctx context.Context, task tasks.Task) {
	if err := w.runWithServiceLock(ctx, task, func(ctx context.Context, item containerservice.ContainerService) error {
		node, ok, err := w.placedNode(ctx, item.ID)
		if err != nil {
			return err
		}
		if !ok {
			return panelerr.Validation("no_eligible_node", "No eligible node is available for restart")
		}
		return w.withNodeLock(ctx, task, node.ID, func(ctx context.Context) error {
			_ = w.step(ctx, task.ID, "compose_restart", tasks.StatusRunning, 50, "")
			cmd := fmt.Sprintf("cd %s && docker compose -p %s -f root.compose.yaml restart %s", shellQuote(defaultRootDir), shellQuote(defaultProject), shellQuote(item.Name))
			if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: 2 * time.Minute}); err != nil {
				return err
			}
			_ = w.step(ctx, task.ID, "compose_restart", tasks.StatusCompleted, 100, "")
			return nil
		})
	}); err != nil {
		_ = w.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = w.tasks.Complete(ctx, task.ID, "Container Service restarted")
}

func (w *Worker) runReconcile(ctx context.Context, task tasks.Task) {
	if err := w.runWithServiceLock(ctx, task, func(ctx context.Context, item containerservice.ContainerService) error {
		if !item.Enabled {
			return panelerr.Validation("container_service_disabled", "Disabled Container Services are not reconciled")
		}
		node, ok, err := w.chooseNode(ctx, item)
		if err != nil {
			return err
		}
		if !ok {
			return panelerr.Validation("no_eligible_node", "No eligible node is available for this Container Service")
		}
		task.NodeID = node.ID
		return w.withNodeLock(ctx, task, node.ID, func(ctx context.Context) error {
			state, err := w.inspectExistingContainer(ctx, node, item.Name)
			if err != nil {
				return err
			}
			if state.Exists {
				if !state.Managed {
					return panelerr.Conflict("container_name_unmanaged_conflict", "A container named "+item.Name+" already exists on the selected node and is not managed by panel")
				}
				if state.ServiceID != "" && state.ServiceID != item.ID {
					return panelerr.Conflict("container_name_managed_conflict", "A container named "+item.Name+" belongs to another Container Service")
				}
				if state.Generation == item.Generation && state.SpecRevision == item.SpecRevision {
					if err := w.services.SetPlacement(ctx, containerservice.Placement{ServiceID: item.ID, NodeID: node.ID, Generation: item.Generation, SpecRevision: item.SpecRevision, Status: "running"}); err != nil {
						return err
					}
					_ = w.step(ctx, task.ID, "idempotency_check", tasks.StatusCompleted, 100, "current generation already deployed")
					return nil
				}
			}
			if err := w.validateDependencyArtifacts(ctx, task, node, item); err != nil {
				return err
			}
			parsed, err := containerservice.ParseServiceBody(item.ComposeServiceYAML)
			if err != nil {
				return err
			}
			files, err := w.services.ListFiles(ctx, item.ID)
			if err != nil {
				return err
			}
			renderFiles := make([]containerrender.FileInput, 0, len(files))
			for _, file := range files {
				renderFiles = append(renderFiles, containerrender.FileInput{Path: file.Path, Kind: file.Kind, Content: []byte(file.Content)})
			}
			_ = w.step(ctx, task.ID, "render_generation", tasks.StatusRunning, 20, "")
			out, err := containerrender.Render(containerrender.Input{
				ServiceID:          item.ID,
				ServiceName:        item.Name,
				NodeID:             node.ID,
				Generation:         item.Generation,
				SpecRevision:       item.SpecRevision,
				PortClaims:         parsed.PortClaims,
				RootDir:            defaultRootDir,
				ComposeProjectName: defaultProject,
				ComposeServiceYAML: item.ComposeServiceYAML,
				Variables:          item.Variables,
				Files:              renderFiles,
			})
			if err != nil {
				return err
			}
			manifest, _ := json.MarshalIndent(out.Manifest, "", "  ")
			_ = w.step(ctx, task.ID, "render_generation", tasks.StatusCompleted, 100, "")
			if err := w.writeArtifact(ctx, task, node, item, out, string(manifest)); err != nil {
				return err
			}
			if err := w.refreshRootCompose(ctx, task, node); err != nil {
				return err
			}
			_ = w.step(ctx, task.ID, "compose_up", tasks.StatusRunning, 80, "")
			cmd := fmt.Sprintf("cd %s && docker compose -p %s -f root.compose.yaml up -d %s", shellQuote(defaultRootDir), shellQuote(defaultProject), shellQuote(item.Name))
			if result, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: 5 * time.Minute}); err != nil {
				_ = w.tasks.AppendLog(ctx, task.ID, "stdout", result.Stdout)
				_ = w.tasks.AppendLog(ctx, task.ID, "stderr", result.Stderr)
				return err
			}
			_ = w.step(ctx, task.ID, "compose_up", tasks.StatusCompleted, 100, "")
			_ = w.step(ctx, task.ID, "verify_runtime", tasks.StatusRunning, 90, "")
			verify := fmt.Sprintf("cd %s && docker compose -p %s -f root.compose.yaml ps --format json %s", shellQuote(defaultRootDir), shellQuote(defaultProject), shellQuote(item.Name))
			if result, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: verify, Timeout: time.Minute}); err != nil {
				_ = w.tasks.AppendLog(ctx, task.ID, "stdout", result.Stdout)
				_ = w.tasks.AppendLog(ctx, task.ID, "stderr", result.Stderr)
				return err
			}
			if err := w.services.SetPlacement(ctx, containerservice.Placement{ServiceID: item.ID, NodeID: node.ID, Generation: item.Generation, SpecRevision: item.SpecRevision, Status: "running"}); err != nil {
				return err
			}
			_ = w.step(ctx, task.ID, "verify_runtime", tasks.StatusCompleted, 100, "")
			return nil
		})
	}); err != nil {
		_ = w.tasks.Fail(ctx, task.ID, err)
		return
	}
	_ = w.tasks.Complete(ctx, task.ID, "Container Service reconciled")
}

type existingContainerState struct {
	Exists       bool
	Managed      bool
	ServiceID    string
	Generation   int
	SpecRevision string
}

func (w *Worker) inspectExistingContainer(ctx context.Context, node server.Server, name string) (existingContainerState, error) {
	cmd := fmt.Sprintf("docker inspect --format '{{json .Config.Labels}}' %s 2>/dev/null || true", shellQuote(name))
	result, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: time.Minute})
	if err != nil {
		return existingContainerState{}, err
	}
	out := strings.TrimSpace(result.Stdout)
	if out == "" || out == "null" {
		return existingContainerState{}, nil
	}
	labels := map[string]string{}
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return existingContainerState{}, err
	}
	gen, _ := strconv.Atoi(labels["panel.service.generation"])
	return existingContainerState{
		Exists:       true,
		Managed:      labels["panel.managed"] == "true",
		ServiceID:    labels["panel.service.id"],
		Generation:   gen,
		SpecRevision: labels["panel.service.spec_revision"],
	}, nil
}

func (w *Worker) placedNode(ctx context.Context, serviceID string) (server.Server, bool, error) {
	placement, ok, err := w.services.Placement(ctx, serviceID)
	if err != nil || !ok {
		return server.Server{}, ok, err
	}
	node, err := w.servers.Get(ctx, placement.NodeID)
	if err != nil {
		return server.Server{}, false, err
	}
	return node, true, nil
}

func (w *Worker) runWithServiceLock(ctx context.Context, task tasks.Task, run func(context.Context, containerservice.ContainerService) error) error {
	_ = w.tasks.Start(ctx, task.ID)
	item, err := w.services.Get(ctx, task.ResourceID)
	if err != nil {
		return err
	}
	ok, err := w.locks.Acquire(ctx, "service", item.ID, task.ID)
	if err != nil {
		return err
	}
	if !ok {
		return panelerr.Conflict("container_service_locked", "Container Service already has an active operation")
	}
	defer w.locks.Release(ctx, "service", item.ID, task.ID)
	return run(ctx, item)
}

func (w *Worker) withNodeLock(ctx context.Context, task tasks.Task, nodeID string, run func(context.Context) error) error {
	ok, err := w.locks.Acquire(ctx, "node", nodeID, task.ID)
	if err != nil {
		return err
	}
	if !ok {
		return panelerr.Conflict("container_node_locked", "Node already has an active Container Services operation")
	}
	defer w.locks.Release(ctx, "node", nodeID, task.ID)
	return run(ctx)
}

func (w *Worker) chooseNode(ctx context.Context, item containerservice.ContainerService) (server.Server, bool, error) {
	servers, err := w.servers.List(ctx)
	if err != nil {
		return server.Server{}, false, err
	}
	for _, node := range servers {
		if !node.Reachable || !node.OS.Supported {
			continue
		}
		if !selectorMatches(item.Selector, node) {
			continue
		}
		cap, err := readCapability(ctx, w.locks.db, node.ID)
		if err != nil || !cap {
			continue
		}
		if claimsConflict(ctx, w.locks.db, node.ID, item) {
			continue
		}
		if dependenciesSchedulable(ctx, w.locks.db, item) {
			return node, true, nil
		}
	}
	return server.Server{}, false, nil
}

func selectorMatches(selector map[string]string, node server.Server) bool {
	for key, want := range selector {
		switch key {
		case "id":
			if node.ID != want {
				return false
			}
		case "name":
			if node.Name != want {
				return false
			}
		case "host":
			if node.Host != want {
				return false
			}
		default:
			if node.Traits[key] != want {
				return false
			}
		}
	}
	return true
}

func readCapability(ctx context.Context, db *sql.DB, nodeID string) (bool, error) {
	var docker, compose, include, supported int
	err := db.QueryRowContext(ctx, `SELECT docker_installed,compose_installed,include_supported,supported FROM docker_capabilities WHERE server_id=?`, nodeID).Scan(&docker, &compose, &include, &supported)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return docker == 1 && compose == 1 && include == 1 && supported == 1, err
}

func claimsConflict(ctx context.Context, db *sql.DB, nodeID string, item containerservice.ContainerService) bool {
	parsed, err := containerservice.ParseServiceBody(item.ComposeServiceYAML)
	if err != nil || len(parsed.PortClaims) == 0 {
		return false
	}
	wanted := map[int]bool{}
	for _, port := range parsed.PortClaims {
		wanted[port] = true
	}
	var payload string
	err = db.QueryRowContext(ctx, `SELECT payload FROM docker_runtime_cache WHERE server_id=? AND resource='services'`, nodeID).Scan(&payload)
	if err == nil {
		containers := []struct {
			Labels map[string]string `json:"labels"`
		}{}
		if json.Unmarshal([]byte(payload), &containers) == nil {
			for _, container := range containers {
				if labelsConflictWithClaims(container.Labels, item.Name, wanted) {
					return true
				}
			}
		}
	}
	var labelsJSON string
	rows, err := db.QueryContext(ctx, `SELECT labels_json FROM container_runtime_cache WHERE node_id=? AND managed=1`, nodeID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&labelsJSON); err != nil {
			continue
		}
		labels := map[string]string{}
		_ = json.Unmarshal([]byte(labelsJSON), &labels)
		if labels["panel.service.name"] == item.Name {
			continue
		}
		if labelsConflictWithClaims(labels, item.Name, wanted) {
			return true
		}
	}
	return false
}

func labelsConflictWithClaims(labels map[string]string, serviceName string, wanted map[int]bool) bool {
	if labels == nil || labels["panel.service.name"] == serviceName {
		return false
	}
	for _, raw := range strings.Split(labels["panel.claims.ports"], ",") {
		port, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && wanted[port] {
			return true
		}
	}
	return false
}

func dependenciesSchedulable(ctx context.Context, db *sql.DB, item containerservice.ContainerService) bool {
	parsed, err := containerservice.ParseServiceBody(item.ComposeServiceYAML)
	if err != nil {
		return false
	}
	for _, dep := range parsed.Dependencies {
		var enabled int
		if err := db.QueryRowContext(ctx, `SELECT enabled FROM container_services WHERE name=?`, dep).Scan(&enabled); err != nil || enabled != 1 {
			return false
		}
	}
	return true
}

func (w *Worker) validateDependencyArtifacts(ctx context.Context, task tasks.Task, node server.Server, item containerservice.ContainerService) error {
	parsed, err := containerservice.ParseServiceBody(item.ComposeServiceYAML)
	if err != nil {
		return err
	}
	_ = w.step(ctx, task.ID, "validate_dependencies", tasks.StatusRunning, 15, "")
	for _, dep := range parsed.Dependencies {
		cmd := fmt.Sprintf("test -f %s", shellQuote(defaultRootDir+"/"+dep+"/current/compose.yaml"))
		if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: time.Minute}); err != nil {
			return panelerr.Validation("dependency_current_artifact_missing", "Dependency "+dep+" has no current artifact on selected node")
		}
	}
	_ = w.step(ctx, task.ID, "validate_dependencies", tasks.StatusCompleted, 100, "")
	return nil
}

func (w *Worker) writeArtifact(ctx context.Context, task tasks.Task, node server.Server, item containerservice.ContainerService, out containerrender.Output, manifest string) error {
	_ = w.step(ctx, task.ID, "write_current", tasks.StatusRunning, 45, "")
	base := defaultRootDir + "/" + item.Name
	if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: "rm -rf " + shellQuote(base+"/current") + " && mkdir -p " + shellQuote(base+"/current/files") + " " + shellQuote(base+"/generations/"+fmt.Sprint(item.Generation)) + " " + shellQuote(base+"/data"), Timeout: 2 * time.Minute}); err != nil {
		return err
	}
	if err := w.writeRemote(ctx, node, base+"/current/compose.yaml", out.ComposeYAML); err != nil {
		return err
	}
	if err := w.writeRemote(ctx, node, base+"/current/panel.override.yaml", out.OverrideYAML); err != nil {
		return err
	}
	if err := w.writeRemote(ctx, node, base+"/current/manifest.json", manifest); err != nil {
		return err
	}
	for _, file := range out.Files {
		if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: "mkdir -p " + shellQuote(dir(file.RemotePath)), Timeout: time.Minute}); err != nil {
			return err
		}
		if err := w.writeRemote(ctx, node, file.RemotePath, string(file.Content)); err != nil {
			return err
		}
	}
	_ = w.step(ctx, task.ID, "write_current", tasks.StatusCompleted, 100, "")
	return nil
}

func (w *Worker) refreshRootCompose(ctx context.Context, task tasks.Task, node server.Server) error {
	_ = w.step(ctx, task.ID, "refresh_root_compose", tasks.StatusRunning, 65, "")
	services, err := w.services.List(ctx)
	if err != nil {
		return err
	}
	lines := []string{"include:"}
	for _, item := range services {
		if !item.Enabled {
			continue
		}
		if !w.serviceArtifactExists(ctx, node, item.Name) {
			continue
		}
		lines = append(lines, "  - ./"+item.Name+"/current/compose.yaml", "  - ./"+item.Name+"/current/panel.override.yaml")
	}
	if len(lines) == 1 {
		lines = append(lines, "  []")
	}
	if _, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: "mkdir -p " + shellQuote(defaultRootDir), Timeout: time.Minute}); err != nil {
		return err
	}
	if err := w.writeRemote(ctx, node, defaultRootDir+"/root.compose.yaml", strings.Join(lines, "\n")+"\n"); err != nil {
		return err
	}
	_ = w.step(ctx, task.ID, "refresh_root_compose", tasks.StatusCompleted, 100, "")
	return nil
}

func (w *Worker) serviceArtifactExists(ctx context.Context, node server.Server, name string) bool {
	cmd := fmt.Sprintf("test -f %s && test -f %s", shellQuote(defaultRootDir+"/"+name+"/current/compose.yaml"), shellQuote(defaultRootDir+"/"+name+"/current/panel.override.yaml"))
	_, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: time.Minute})
	return err == nil
}

func (w *Worker) writeRemote(ctx context.Context, node server.Server, remotePath, content string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("printf %%s %s | base64 -d > %s", shellQuote(encoded), shellQuote(remotePath))
	_, err := w.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: 2 * time.Minute})
	return err
}

func (w *Worker) step(ctx context.Context, taskID, name, status string, pct float64, meta string) error {
	if meta == "" {
		meta = "{}"
	}
	_, err := w.tasks.UpsertStep(ctx, taskID, tasks.StepInput{Step: name, Status: status, Percentage: pct, MetadataJSON: meta})
	return err
}

func isContainerTask(taskType string) bool {
	switch taskType {
	case containerservice.TaskTypeReconcile, containerservice.TaskTypeRestart, containerservice.TaskTypeEnable, containerservice.TaskTypeDisable, containerservice.TaskTypeDelete:
		return true
	default:
		return false
	}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func dir(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return "."
	}
	return p[:idx]
}
