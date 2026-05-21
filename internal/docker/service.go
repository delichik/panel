package docker

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/tasks"
)

const capabilityRefreshAfter = 5 * time.Minute
const dockerRefreshTimeout = 2 * time.Minute

type Service struct {
	db         *sql.DB
	servers    *server.Service
	runtime    ContainerRuntime
	tasks      *tasks.Service
	refreshSem chan struct{}
}

type ResourceOperation struct {
	Kind    string
	Action  string
	ID      string
	Summary string
}

type ImageUpdateOperation struct {
	Action   string
	ImageIDs []string
	Summary  string
}

func NewService(db *sql.DB, servers *server.Service, runtime ContainerRuntime, taskSvc *tasks.Service) *Service {
	return &Service{db: db, servers: servers, runtime: runtime, tasks: taskSvc, refreshSem: make(chan struct{}, 1)}
}

func (s *Service) Capability(ctx context.Context, serverID string) (DockerCapability, error) {
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return DockerCapability{}, err
	}
	cap, err := s.readCapability(ctx, serverID)
	if err == sql.ErrNoRows {
		task, taskErr := s.Refresh(ctx, serverID)
		if taskErr != nil {
			return DockerCapability{}, taskErr
		}
		return DockerCapability{ServerID: serverID, Supported: false, Pending: true, Stale: true, TaskID: task.ID, LastError: "Docker capability check is queued"}, nil
	}
	if err != nil {
		return cap, err
	}
	if cap.LastCheckedAt == nil || time.Since(*cap.LastCheckedAt) > capabilityRefreshAfter {
		if task, taskErr := s.Refresh(ctx, serverID); taskErr == nil {
			cap.Pending = true
			cap.TaskID = task.ID
			cap.Stale = true
		}
	}
	return cap, nil
}

func (s *Service) Refresh(ctx context.Context, serverID string) (tasks.Task, error) {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if task, ok, err := s.runningRefresh(ctx, serverID); err != nil {
		return tasks.Task{}, err
	} else if ok {
		if task.Status != tasks.StatusRunning && (task.NextRunAt == nil || !task.NextRunAt.After(time.Now().UTC())) {
			task, err = s.tasks.RunNow(ctx, task.ID)
			if err != nil {
				return tasks.Task{}, err
			}
			go s.runRefresh(context.Background(), task.ID, srv)
		}
		return task, nil
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{Type: "docker_status_refresh", ServerID: serverID, ResourceType: "server", ResourceID: serverID, Summary: "Refreshing Docker runtime status", MaxRetries: 8})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRefresh(context.Background(), task.ID, srv)
	return task, nil
}

func (s *Service) RefreshReachable(ctx context.Context) error {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	for _, srv := range servers {
		if !srv.Reachable {
			continue
		}
		if _, err := s.Refresh(ctx, srv.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListProjects(ctx context.Context, serverID string) (RuntimeList[ComposeProject], error) {
	if _, err := s.ensureSupported(ctx, serverID); err != nil {
		return RuntimeList[ComposeProject]{}, err
	}
	out, err := readRuntimeCache[ComposeProject](ctx, s.db, serverID, "projects")
	if err == sql.ErrNoRows {
		return emptyRuntimeList[ComposeProject](ctx, s, serverID)
	}
	if err != nil {
		return RuntimeList[ComposeProject]{}, err
	}
	return out, nil
}

func (s *Service) ComposeStatus(ctx context.Context, serverID, project string) (ComposeStatus, error) {
	if _, err := s.ensureSupported(ctx, serverID); err != nil {
		return ComposeStatus{}, err
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return ComposeStatus{}, panelerr.Validation("project_required", "Project name is required")
	}
	cached, err := readRuntimeCache[RuntimeService](ctx, s.db, serverID, "services")
	if err == sql.ErrNoRows {
		if _, refreshErr := s.Refresh(ctx, serverID); refreshErr != nil {
			return ComposeStatus{}, refreshErr
		}
		return ComposeStatus{Project: project, State: "pending", Services: []RuntimeService{}, CheckedAt: time.Now().UTC()}, nil
	}
	if err != nil {
		return ComposeStatus{}, err
	}
	services := []RuntimeService{}
	state := "empty"
	for _, svc := range cached.Items {
		if svc.Project != project {
			continue
		}
		services = append(services, svc)
		if svc.State == "running" || svc.Status == "running" {
			state = "running"
		} else if state == "empty" {
			state = firstNonEmpty(svc.State, svc.Status, "unknown")
		}
	}
	checkedAt := time.Now().UTC()
	if cached.LastRefreshedAt != nil {
		checkedAt = *cached.LastRefreshedAt
	}
	return ComposeStatus{Project: project, State: state, Services: services, CheckedAt: checkedAt}, nil
}

func (s *Service) ListServices(ctx context.Context, serverID string) (RuntimeList[RuntimeService], error) {
	if _, err := s.ensureSupported(ctx, serverID); err != nil {
		return RuntimeList[RuntimeService]{}, err
	}
	out, err := readRuntimeCache[RuntimeService](ctx, s.db, serverID, "services")
	if err == sql.ErrNoRows {
		return emptyRuntimeList[RuntimeService](ctx, s, serverID)
	}
	if err != nil {
		return RuntimeList[RuntimeService]{}, err
	}
	return out, nil
}

func (s *Service) ListNetworks(ctx context.Context, serverID string) (RuntimeList[RuntimeNetwork], error) {
	if _, err := s.ensureSupported(ctx, serverID); err != nil {
		return RuntimeList[RuntimeNetwork]{}, err
	}
	out, err := readRuntimeCache[RuntimeNetwork](ctx, s.db, serverID, "networks")
	if err == sql.ErrNoRows {
		return emptyRuntimeList[RuntimeNetwork](ctx, s, serverID)
	}
	if err != nil {
		return RuntimeList[RuntimeNetwork]{}, err
	}
	return out, nil
}

func (s *Service) ListVolumes(ctx context.Context, serverID string) (RuntimeList[RuntimeVolume], error) {
	if _, err := s.ensureSupported(ctx, serverID); err != nil {
		return RuntimeList[RuntimeVolume]{}, err
	}
	out, err := readRuntimeCache[RuntimeVolume](ctx, s.db, serverID, "volumes")
	if err == sql.ErrNoRows {
		return emptyRuntimeList[RuntimeVolume](ctx, s, serverID)
	}
	if err != nil {
		return RuntimeList[RuntimeVolume]{}, err
	}
	return out, nil
}

func (s *Service) ListImages(ctx context.Context, serverID string) (RuntimeList[RuntimeImage], error) {
	if _, err := s.ensureSupported(ctx, serverID); err != nil {
		return RuntimeList[RuntimeImage]{}, err
	}
	out, err := readRuntimeCache[RuntimeImage](ctx, s.db, serverID, "images")
	if err == sql.ErrNoRows {
		return emptyRuntimeList[RuntimeImage](ctx, s, serverID)
	}
	if err != nil {
		return RuntimeList[RuntimeImage]{}, err
	}
	return out, nil
}

func (s *Service) NotImplementedTask(ctx context.Context, serverID, taskType, summary string) (tasks.Task, error) {
	if _, err := s.servers.Get(ctx, serverID); err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{Type: taskType, ServerID: serverID, Summary: summary})
	if err != nil {
		return tasks.Task{}, err
	}
	go func() {
		_ = s.tasks.Start(context.Background(), task.ID)
		_ = s.tasks.Advance(context.Background(), task.ID, "blocked", "operation is reserved for Phase 2B implementation")
		_ = s.tasks.Fail(context.Background(), task.ID, panelerr.Validation("not_implemented", "Docker mutation is not implemented in this backend slice"))
	}()
	return task, nil
}

func (s *Service) ResourceTask(ctx context.Context, serverID string, op ResourceOperation) (tasks.Task, error) {
	srv, err := s.ensureSupported(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	taskType := "docker_" + op.Kind + "_" + op.Action
	task, err := s.tasks.Create(ctx, tasks.CreateInput{Type: taskType, ServerID: serverID, Summary: op.Summary})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runResourceOperation(context.Background(), task.ID, srv, op)
	return task, nil
}

func (s *Service) ImageUpdateTask(ctx context.Context, serverID string, op ImageUpdateOperation) (tasks.Task, error) {
	srv, err := s.ensureSupported(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{Type: "docker_image_" + strings.ReplaceAll(op.Action, "-", "_"), ServerID: serverID, Summary: op.Summary})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runImageUpdateOperation(context.Background(), task.ID, srv, op)
	return task, nil
}

func (s *Service) runImageUpdateOperation(ctx context.Context, taskID string, srv server.Server, op ImageUpdateOperation) {
	ctx, cancel := context.WithTimeout(ctx, dockerRefreshTimeout)
	defer cancel()
	_ = s.tasks.Start(ctx, taskID)
	cached, err := readRuntimeCache[RuntimeImage](ctx, s.db, srv.ID, "images")
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	images := cached.Items
	switch op.Action {
	case "check_updates":
		_ = s.tasks.Advance(ctx, taskID, "checking", "checking Docker image manifests")
		for i := range images {
			update, checkErr := s.runtime.CheckImageUpdate(ctx, srv.Target(), images[i])
			if checkErr != nil {
				_ = s.tasks.Fail(ctx, taskID, checkErr)
				return
			}
			images[i].Update = &update
		}
		if err := s.writeCache(ctx, srv.ID, "images", images, time.Now().UTC()); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Complete(ctx, taskID, "Docker image update check completed")
	case "update_selected", "update_all":
		selected := selectImages(images, op.ImageIDs, op.Action == "update_all")
		if len(selected) == 0 {
			_ = s.tasks.Fail(ctx, taskID, panelerr.Validation("docker_image_selection_empty", "No Docker images selected for update"))
			return
		}
		for _, image := range selected {
			_ = s.tasks.Advance(ctx, taskID, "pulling", "pulling "+image.Repository+":"+image.Tag)
			if err := s.runtime.PullImage(ctx, srv.Target(), image.Repository, image.Tag); err != nil {
				_ = s.tasks.Fail(ctx, taskID, err)
				return
			}
		}
		_ = s.tasks.Advance(ctx, taskID, "refreshing", "refreshing Docker runtime cache")
		if err := s.refreshRuntimeLists(ctx, taskID, srv); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		_ = s.tasks.Complete(ctx, taskID, "Docker image update completed")
	default:
		_ = s.tasks.Fail(ctx, taskID, panelerr.NotFound("docker_image_operation"))
	}
}

func selectImages(images []RuntimeImage, ids []string, all bool) []RuntimeImage {
	if all {
		out := []RuntimeImage{}
		for _, image := range images {
			if image.Update == nil || image.Update.UpdateAvailable {
				out = append(out, image)
			}
		}
		return out
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := []RuntimeImage{}
	for _, image := range images {
		if wanted[image.ID] {
			out = append(out, image)
		}
	}
	return out
}

func (s *Service) runResourceOperation(ctx context.Context, taskID string, srv server.Server, op ResourceOperation) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	_ = s.tasks.Start(ctx, taskID)
	_ = s.tasks.Advance(ctx, taskID, "running", op.Summary)
	var err error
	switch {
	case op.Kind == "network" && op.Action == "delete":
		err = s.runtime.DeleteNetwork(ctx, srv.Target(), op.ID)
	case op.Kind == "network" && op.Action == "prune":
		err = s.runtime.PruneNetworks(ctx, srv.Target())
	case op.Kind == "volume" && op.Action == "delete":
		err = s.runtime.DeleteVolume(ctx, srv.Target(), op.ID)
	case op.Kind == "volume" && op.Action == "prune":
		err = s.runtime.PruneVolumes(ctx, srv.Target())
	case op.Kind == "image" && op.Action == "delete":
		err = s.runtime.DeleteImage(ctx, srv.Target(), op.ID)
	case op.Kind == "image" && op.Action == "prune":
		err = s.runtime.PruneImages(ctx, srv.Target())
	default:
		err = panelerr.NotFound("docker_operation")
	}
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "refreshing", "refreshing Docker runtime cache")
	if err := s.refreshRuntimeLists(ctx, taskID, srv); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, op.Summary+" completed")
}

func (s *Service) runRefresh(ctx context.Context, taskID string, srv server.Server) {
	s.refreshSem <- struct{}{}
	defer func() { <-s.refreshSem }()
	ctx, cancel := context.WithTimeout(ctx, dockerRefreshTimeout)
	defer cancel()
	_ = s.tasks.Start(ctx, taskID)
	_ = s.tasks.Advance(ctx, taskID, "capability", "checking Docker and Compose availability")
	cap, err := s.runtime.Detect(ctx, srv.Target())
	if err != nil {
		_ = s.writeCapabilityFailure(ctx, srv.ID, err)
		_ = s.tasks.FailRetryable(ctx, taskID, err)
		return
	}
	if err := s.writeCapability(ctx, cap); err != nil {
		_ = s.tasks.FailRetryable(ctx, taskID, err)
		return
	}
	if !cap.Supported {
		_ = s.tasks.Complete(ctx, taskID, "Docker is unsupported on this server")
		return
	}
	if err := s.refreshRuntimeLists(ctx, taskID, srv); err != nil {
		_ = s.tasks.FailRetryable(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "Docker runtime status refreshed")
}

func (s *Service) refreshRuntimeLists(ctx context.Context, taskID string, srv server.Server) error {
	now := time.Now().UTC()
	_ = s.tasks.Advance(ctx, taskID, "services", "reading Docker containers")
	services, err := s.runtime.ListServices(ctx, srv.Target())
	if err != nil {
		return err
	}
	if err := s.writeCache(ctx, srv.ID, "services", services, now); err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "networks", "reading Docker networks")
	networks, err := s.runtime.ListNetworks(ctx, srv.Target())
	if err != nil {
		return err
	}
	if err := s.writeCache(ctx, srv.ID, "networks", networks, now); err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "volumes", "reading Docker volumes")
	volumes, err := s.runtime.ListVolumes(ctx, srv.Target())
	if err != nil {
		return err
	}
	if err := s.writeCache(ctx, srv.ID, "volumes", volumes, now); err != nil {
		return err
	}
	_ = s.tasks.Advance(ctx, taskID, "images", "reading Docker images")
	images, err := s.runtime.ListImages(ctx, srv.Target())
	if err != nil {
		return err
	}
	return s.writeCache(ctx, srv.ID, "images", images, now)
}

func (s *Service) ensureSupported(ctx context.Context, serverID string) (server.Server, error) {
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return server.Server{}, err
	}
	cap, err := s.readCapability(ctx, serverID)
	if err == sql.ErrNoRows {
		if _, refreshErr := s.Refresh(ctx, serverID); refreshErr != nil {
			return server.Server{}, refreshErr
		}
		return server.Server{}, panelerr.Validation("docker_capability_pending", "Docker capability check is running in the background")
	}
	if err != nil {
		return server.Server{}, err
	}
	if cap.DockerInstalled && cap.ComposeInstalled && cap.DockerVersion == "" {
		if _, refreshErr := s.Refresh(ctx, serverID); refreshErr == nil {
			return server.Server{}, panelerr.Validation("docker_capability_pending", "Docker capability check is running in the background")
		}
	}
	if !cap.Supported {
		return server.Server{}, panelerr.Validation("docker_unsupported", "Docker or Docker Compose is not available on this server")
	}
	return srv, nil
}

func (s *Service) runningRefresh(ctx context.Context, serverID string) (tasks.Task, bool, error) {
	var taskID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tasks WHERE server_id=? AND type='docker_status_refresh' AND status IN ('queued','scheduled','running','failed_retryable') ORDER BY created_at DESC LIMIT 1`, serverID).Scan(&taskID)
	if err == sql.ErrNoRows {
		return tasks.Task{}, false, nil
	}
	if err != nil {
		return tasks.Task{}, false, err
	}
	task, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return tasks.Task{}, false, err
	}
	return task, true, nil
}

func (s *Service) readCapability(ctx context.Context, serverID string) (DockerCapability, error) {
	var cap DockerCapability
	var checkedAt string
	err := s.db.QueryRowContext(ctx, `SELECT server_id,docker_installed,docker_version,compose_installed,compose_version,supported,last_checked_at,last_error,stale FROM docker_capabilities WHERE server_id=?`, serverID).
		Scan(&cap.ServerID, &cap.DockerInstalled, &cap.DockerVersion, &cap.ComposeInstalled, &cap.ComposeVersion, &cap.Supported, &checkedAt, &cap.LastError, &cap.Stale)
	if err != nil {
		return DockerCapability{}, err
	}
	t, _ := time.Parse(time.RFC3339Nano, checkedAt)
	cap.LastCheckedAt = &t
	return cap, nil
}

func (s *Service) writeCapability(ctx context.Context, cap DockerCapability) error {
	now := time.Now().UTC()
	if cap.LastCheckedAt == nil {
		cap.LastCheckedAt = &now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO docker_capabilities(server_id,docker_installed,docker_version,compose_installed,compose_version,supported,last_checked_at,last_error,stale)
		VALUES(?,?,?,?,?,?,?,?,0)
		ON CONFLICT(server_id) DO UPDATE SET docker_installed=excluded.docker_installed,docker_version=excluded.docker_version,compose_installed=excluded.compose_installed,compose_version=excluded.compose_version,supported=excluded.supported,last_checked_at=excluded.last_checked_at,last_error=excluded.last_error,stale=0`,
		cap.ServerID, cap.DockerInstalled, cap.DockerVersion, cap.ComposeInstalled, cap.ComposeVersion, cap.Supported, cap.LastCheckedAt.Format(time.RFC3339Nano), cap.LastError)
	return err
}

func (s *Service) writeCapabilityFailure(ctx context.Context, serverID string, cause error) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT INTO docker_capabilities(server_id,docker_installed,docker_version,compose_installed,compose_version,supported,last_checked_at,last_error,stale)
		VALUES(?,?,?,?,?,?,?,?,1)
		ON CONFLICT(server_id) DO UPDATE SET last_checked_at=excluded.last_checked_at,last_error=excluded.last_error,stale=1`,
		serverID, false, "", false, "", false, now, cause.Error())
	return err
}

func (s *Service) writeCache(ctx context.Context, serverID, resource string, value any, refreshedAt time.Time) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO docker_runtime_cache(server_id,resource,payload,refreshed_at)
		VALUES(?,?,?,?)
		ON CONFLICT(server_id,resource) DO UPDATE SET payload=excluded.payload,refreshed_at=excluded.refreshed_at`,
		serverID, resource, string(payload), refreshedAt.Format(time.RFC3339Nano))
	return err
}

func emptyRuntimeList[T any](ctx context.Context, s *Service, serverID string) (RuntimeList[T], error) {
	if _, err := s.Refresh(ctx, serverID); err != nil {
		return RuntimeList[T]{}, err
	}
	return RuntimeList[T]{ServerID: serverID, Items: []T{}}, nil
}

func readRuntimeCache[T any](ctx context.Context, db *sql.DB, serverID, resource string) (RuntimeList[T], error) {
	var payload, refreshedAt string
	if err := db.QueryRowContext(ctx, `SELECT payload,refreshed_at FROM docker_runtime_cache WHERE server_id=? AND resource=?`, serverID, resource).Scan(&payload, &refreshedAt); err != nil {
		return RuntimeList[T]{}, err
	}
	items := []T{}
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		return RuntimeList[T]{}, err
	}
	t, _ := time.Parse(time.RFC3339Nano, refreshedAt)
	return RuntimeList[T]{ServerID: serverID, LastRefreshedAt: &t, Items: items}, nil
}
