package nomad

import (
	"context"

	"panel/internal/server"
	"panel/internal/tasks"
)

const (
	ControlPlaneUnconfigured = "unconfigured"
	ControlPlaneBootstrapping = "bootstrapping"
	ControlPlaneConnected    = "connected"
	ControlPlaneDegraded     = "degraded"

	ProjectedNodeManaged   = "managed"
	ProjectedNodePending   = "pending"
	ProjectedNodeUnmanaged = "unmanaged"

	ProjectedNodeRoleServer  = "server"
	ProjectedNodeRoleClient  = "client"
	ProjectedNodeRoleUnknown = "unknown"
)

type statusClient interface {
	Status(ctx context.Context) (StatusResponse, error)
}

type ControlPlane struct {
	Status              string          `json:"status"`
	Leader              string          `json:"leader,omitempty"`
	Nodes               []ProjectedNode `json:"nodes"`
	JoinCandidates      []server.Server `json:"joinCandidates"`
	BootstrapCandidates []server.Server `json:"bootstrapCandidates"`
}

type ProjectedNode struct {
	Kind     string `json:"kind"`
	ServerID string `json:"serverId,omitempty"`
	NodeID   string `json:"nodeId,omitempty"`
	Name     string `json:"name"`
	Host     string `json:"host,omitempty"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	TaskID   string `json:"taskId,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (s *JoinService) ControlPlane(ctx context.Context) (ControlPlane, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return ControlPlane{}, err
	}
	serverByID := map[string]server.Server{}
	for _, srv := range servers {
		serverByID[srv.ID] = srv
	}

	status := StatusResponse{}
	connected := false
	if client, ok := s.nomad.(statusClient); ok {
		if got, err := client.Status(ctx); err == nil && got.Connected {
			status = got
			connected = true
		}
	}

	nodes := []NodeListItem{}
	if connected {
		nodes, err = s.nomad.Nodes(ctx)
		if err != nil {
			connected = false
		}
	}

	latestTasks, err := s.latestNomadTasks(ctx)
	if err != nil {
		return ControlPlane{}, err
	}

	managedServers := map[string]struct{}{}
	projected := []ProjectedNode{}
	if connected {
		for _, node := range nodes {
			serverID := ""
			if node.Meta != nil {
				serverID = node.Meta["panel_server_id"]
			}
			if serverID != "" {
				managedServers[serverID] = struct{}{}
				srv := serverByID[serverID]
				projected = append(projected, ProjectedNode{
					Kind:     ProjectedNodeManaged,
					ServerID: serverID,
					NodeID:   node.ID,
					Name:     firstNonEmpty(node.Name, srv.Name, node.ID),
					Host:     firstNonEmpty(node.Address, srv.Host),
					Role:     roleForTask(latestTasks[serverID]),
					Status:   firstNonEmpty(node.Status, "unknown"),
				})
				continue
			}
			projected = append(projected, ProjectedNode{
				Kind:   ProjectedNodeUnmanaged,
				NodeID: node.ID,
				Name:   firstNonEmpty(node.Name, node.ID),
				Host:   node.Address,
				Role:   ProjectedNodeRoleUnknown,
				Status: "unmanaged",
			})
		}
	}

	for serverID, task := range latestTasks {
		if _, ok := managedServers[serverID]; ok || !taskProjectsAsPending(task) {
			continue
		}
		srv := serverByID[serverID]
		projected = append(projected, ProjectedNode{
			Kind:     ProjectedNodePending,
			ServerID: serverID,
			Name:     firstNonEmpty(srv.Name, serverID),
			Host:     srv.Host,
			Role:     roleForTask(task),
			Status:   projectionStatus(task),
			TaskID:   task.ID,
			Error:    task.Error,
		})
	}

	joinCandidates := []server.Server{}
	for _, srv := range servers {
		if _, ok := managedServers[srv.ID]; ok {
			continue
		}
		if taskProjectsAsPending(latestTasks[srv.ID]) {
			continue
		}
		joinCandidates = append(joinCandidates, srv)
	}

	cpStatus := ControlPlaneConnected
	if !connected {
		if hasActiveBootstrap(latestTasks) {
			cpStatus = ControlPlaneBootstrapping
		} else if len(latestTasks) > 0 {
			cpStatus = ControlPlaneDegraded
		} else {
			cpStatus = ControlPlaneUnconfigured
		}
	}

	return ControlPlane{
		Status:              cpStatus,
		Leader:              status.Leader,
		Nodes:               projected,
		JoinCandidates:      joinCandidates,
		BootstrapCandidates: servers,
	}, nil
}

func (s *JoinService) latestNomadTasks(ctx context.Context) (map[string]tasks.Task, error) {
	out := map[string]tasks.Task{}
	for _, taskType := range []string{TaskTypeServerBootstrap, TaskTypeClientJoin} {
		result, err := s.tasks.List(ctx, tasks.ListFilter{Type: taskType, Limit: 200})
		if err != nil {
			return nil, err
		}
		for _, task := range result.Items {
			if existing, ok := out[task.ServerID]; !ok || task.CreatedAt.After(existing.CreatedAt) {
				out[task.ServerID] = task
			}
		}
	}
	return out, nil
}

func taskProjectsAsPending(task tasks.Task) bool {
	switch task.Status {
	case tasks.StatusQueued, tasks.StatusScheduled, tasks.StatusRunning, tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked:
		return task.ID != ""
	default:
		return false
	}
}

func hasActiveBootstrap(taskByServer map[string]tasks.Task) bool {
	for _, task := range taskByServer {
		if task.Type == TaskTypeServerBootstrap && taskProjectsAsPending(task) && task.Status != tasks.StatusFailed && task.Status != tasks.StatusBlocked {
			return true
		}
	}
	return false
}

func roleForTask(task tasks.Task) string {
	switch task.Type {
	case TaskTypeServerBootstrap:
		return ProjectedNodeRoleServer
	case TaskTypeClientJoin:
		return ProjectedNodeRoleClient
	default:
		return ProjectedNodeRoleUnknown
	}
}

func projectionStatus(task tasks.Task) string {
	if task.Status == tasks.StatusFailed || task.Status == tasks.StatusBlocked {
		return "failed"
	}
	if task.Type == TaskTypeServerBootstrap {
		return "bootstrapping"
	}
	if task.Type == TaskTypeClientJoin {
		return "joining"
	}
	return firstNonEmpty(task.Status, "pending")
}
