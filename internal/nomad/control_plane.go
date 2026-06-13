package nomad

import (
	"context"
	"strings"
	"time"

	"panel/internal/server"
	"panel/internal/tasks"
)

const (
	ControlPlaneUnconfigured  = "unconfigured"
	ControlPlaneBootstrapping = "bootstrapping"
	ControlPlaneConnected     = "connected"
	ControlPlaneDegraded      = "degraded"
	ControlPlaneMigration     = "migration_required"

	ProjectedNodeManaged   = "managed"
	ProjectedNodeMissing   = "missing"
	ProjectedNodePending   = "pending"
	ProjectedNodeUnmanaged = "unmanaged"

	ProjectedNodeRoleServer  = "server"
	ProjectedNodeRoleClient  = "client"
	ProjectedNodeRoleUnknown = "unknown"

	completedTaskProjectionWindow = 15 * time.Minute
)

var controlPlaneNomadQueryTimeout = 3 * time.Second

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
	Kind                    string                   `json:"kind"`
	ServerID                string                   `json:"serverId,omitempty"`
	NodeID                  string                   `json:"nodeId,omitempty"`
	Name                    string                   `json:"name"`
	Host                    string                   `json:"host,omitempty"`
	Traits                  map[string]string        `json:"traits,omitempty"`
	Role                    string                   `json:"role"`
	Status                  string                   `json:"status"`
	ReverseProxy            bool                     `json:"reverseProxy"`
	ReverseProxyStatic      bool                     `json:"reverseProxyStatic"`
	ReverseProxyStaticSites []ReverseProxyStaticSite `json:"reverseProxyStaticSites"`
	JoinEligible            bool                     `json:"joinEligible"`
	TaskID                  string                   `json:"taskId,omitempty"`
	Error                   string                   `json:"error,omitempty"`
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

	s.restoreNomadAddressFromBootstrap(ctx)

	status := StatusResponse{}
	connected := false
	nomadCtx, cancelNomad := context.WithTimeout(ctx, controlPlaneNomadQueryTimeout)
	defer cancelNomad()
	if client, ok := s.nomad.(statusClient); ok {
		if got, err := client.Status(nomadCtx); err == nil && got.Connected {
			status = got
			connected = true
		}
	}

	nodes := []NodeListItem{}
	if connected {
		nodes, err = s.nomad.Nodes(nomadCtx)
		if err != nil {
			connected = false
		}
	}

	latestTasks, err := s.latestNomadTasks(ctx)
	if err != nil {
		return ControlPlane{}, err
	}

	managedServers := map[string]struct{}{}
	nodesByServer := map[string]NodeListItem{}
	unmanagedNodes := []NodeListItem{}
	if connected {
		for _, node := range nodes {
			serverID := serverIDForNode(node)
			if serverID == "" {
				unmanagedNodes = append(unmanagedNodes, node)
				continue
			}
			if _, ok := serverByID[serverID]; !ok {
				unmanagedNodes = append(unmanagedNodes, node)
				continue
			}
			if taskCompletedRemove(latestTasks[serverID]) {
				continue
			}
			if existing, ok := nodesByServer[serverID]; !ok || preferProjectedNomadNode(node, existing) {
				nodesByServer[serverID] = node
			}
			managedServers[serverID] = struct{}{}
		}
	}

	projected := []ProjectedNode{}
	for _, srv := range servers {
		task := latestTasks[srv.ID]
		if taskCompletedRemove(task) {
			continue
		}
		joinEligible := nomadJoinEligible(srv)
		if node, ok := nodesByServer[srv.ID]; ok {
			projected = append(projected, ProjectedNode{
				Kind:                    ProjectedNodeManaged,
				ServerID:                srv.ID,
				NodeID:                  node.ID,
				Name:                    firstNonEmpty(node.Name, srv.Name, node.ID),
				Host:                    firstNonEmpty(node.Address, srv.Host),
				Traits:                  srv.Traits,
				Role:                    s.roleForManagedServer(srv, task),
				Status:                  firstNonEmpty(node.Status, "unknown"),
				ReverseProxy:            traitBool(srv.Traits, TraitReverseProxyEnabled),
				ReverseProxyStatic:      traitBool(srv.Traits, TraitReverseProxyStaticFiles),
				ReverseProxyStaticSites: reverseProxyStaticSitesFromTraits(srv.Traits),
				JoinEligible:            false,
			})
			continue
		}
		if taskProjectsAsPending(task) {
			projected = append(projected, ProjectedNode{
				Kind:                    ProjectedNodePending,
				ServerID:                srv.ID,
				Name:                    firstNonEmpty(srv.Name, srv.ID),
				Host:                    srv.Host,
				Traits:                  srv.Traits,
				Role:                    roleForTask(task),
				Status:                  projectionStatus(task),
				ReverseProxy:            traitBool(srv.Traits, TraitReverseProxyEnabled),
				ReverseProxyStatic:      traitBool(srv.Traits, TraitReverseProxyStaticFiles),
				ReverseProxyStaticSites: reverseProxyStaticSitesFromTraits(srv.Traits),
				JoinEligible:            joinEligible,
				TaskID:                  task.ID,
				Error:                   task.Error,
			})
			continue
		}
		status := "missing"
		if !connected {
			status = "nomad_unreachable"
		}
		projected = append(projected, ProjectedNode{
			Kind:                    ProjectedNodeMissing,
			ServerID:                srv.ID,
			Name:                    firstNonEmpty(srv.Name, srv.ID),
			Host:                    srv.Host,
			Traits:                  srv.Traits,
			Role:                    roleForTask(task),
			Status:                  status,
			ReverseProxy:            traitBool(srv.Traits, TraitReverseProxyEnabled),
			ReverseProxyStatic:      traitBool(srv.Traits, TraitReverseProxyStaticFiles),
			ReverseProxyStaticSites: reverseProxyStaticSitesFromTraits(srv.Traits),
			JoinEligible:            joinEligible,
			TaskID:                  task.ID,
			Error:                   task.Error,
		})
	}

	if connected {
		for _, node := range unmanagedNodes {
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
		if serverID == "" {
			continue
		}
		if _, ok := serverByID[serverID]; ok {
			continue
		}
		if _, ok := managedServers[serverID]; ok || !taskProjectsAsPending(task) {
			continue
		}
		projected = append(projected, ProjectedNode{
			Kind:     ProjectedNodePending,
			ServerID: serverID,
			Name:     serverID,
			Role:     roleForTask(task),
			Status:   projectionStatus(task),
			TaskID:   task.ID,
			Error:    task.Error,
		})
	}

	joinCandidates := []server.Server{}
	for _, srv := range servers {
		if !nomadJoinEligible(srv) {
			continue
		}
		if _, ok := managedServers[srv.ID]; ok {
			continue
		}
		if taskBlocksJoin(latestTasks[srv.ID]) {
			continue
		}
		joinCandidates = append(joinCandidates, srv)
	}
	bootstrapCandidates := []server.Server{}
	for _, srv := range servers {
		if nomadJoinEligible(srv) {
			bootstrapCandidates = append(bootstrapCandidates, srv)
		}
	}

	migrationRequired := false
	if latest := s.latestCompletedBootstrapTask(ctx); latest.ServerID != "" {
		if srv, ok := serverByID[latest.ServerID]; !ok || serverAdvertiseAddress(srv) == "" {
			migrationRequired = true
		}
	}
	cpStatus := ControlPlaneConnected
	switch {
	case hasActiveBootstrap(latestTasks):
		cpStatus = ControlPlaneBootstrapping
	case migrationRequired:
		cpStatus = ControlPlaneMigration
	case connected:
		cpStatus = ControlPlaneConnected
	case len(latestTasks) > 0:
		cpStatus = ControlPlaneDegraded
	default:
		cpStatus = ControlPlaneUnconfigured
	}

	return ControlPlane{
		Status:              cpStatus,
		Leader:              status.Leader,
		Nodes:               projected,
		JoinCandidates:      joinCandidates,
		BootstrapCandidates: bootstrapCandidates,
	}, nil
}

func (s *JoinService) latestNomadTasks(ctx context.Context) (map[string]tasks.Task, error) {
	out := map[string]tasks.Task{}
	for _, taskType := range []string{TaskTypeServerBootstrap, TaskTypeClientJoin, TaskTypeClusterRebuild, TaskTypeNodeRemove, TaskTypeServerSwitch} {
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
	if task.ID == "" {
		return false
	}
	if task.Type == TaskTypeNodeRemove && task.Status == tasks.StatusCompleted {
		return false
	}
	switch task.Status {
	case tasks.StatusQueued, tasks.StatusScheduled, tasks.StatusRunning, tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked:
		return true
	case tasks.StatusCompleted:
		return time.Since(task.CreatedAt) <= completedTaskProjectionWindow
	default:
		return false
	}
}

func taskBlocksJoin(task tasks.Task) bool {
	if task.ID == "" {
		return false
	}
	if task.Type == TaskTypeNodeRemove && task.Status == tasks.StatusCompleted {
		return false
	}
	switch task.Status {
	case tasks.StatusQueued, tasks.StatusScheduled, tasks.StatusRunning, tasks.StatusFailedRetryable:
		return true
	case tasks.StatusCompleted:
		return time.Since(task.CreatedAt) <= completedTaskProjectionWindow
	default:
		return false
	}
}

func serverIDForNode(node NodeListItem) string {
	if node.Meta == nil {
		return ""
	}
	return node.Meta["panel_server_id"]
}

func preferProjectedNomadNode(candidate, existing NodeListItem) bool {
	return nomadNodeStatusPriority(candidate.Status) > nomadNodeStatusPriority(existing.Status)
}

func nomadNodeStatusPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready":
		return 3
	case "initializing":
		return 2
	case "down", "disconnected":
		return 0
	default:
		return 1
	}
}

func taskCompletedRemove(task tasks.Task) bool {
	return task.Type == TaskTypeNodeRemove && task.Status == tasks.StatusCompleted
}

func hasActiveBootstrap(taskByServer map[string]tasks.Task) bool {
	for _, task := range taskByServer {
		if (task.Type == TaskTypeServerBootstrap || task.Type == TaskTypeClusterRebuild) &&
			(task.Status == tasks.StatusQueued || task.Status == tasks.StatusRunning || task.Status == tasks.StatusScheduled) {
			return true
		}
	}
	return false
}

func roleForTask(task tasks.Task) string {
	switch task.Type {
	case TaskTypeServerBootstrap:
		return ProjectedNodeRoleServer
	case TaskTypeClusterRebuild:
		return ProjectedNodeRoleServer
	case TaskTypeServerSwitch:
		return ProjectedNodeRoleServer
	case TaskTypeClientJoin:
		return ProjectedNodeRoleClient
	default:
		return ProjectedNodeRoleUnknown
	}
}

func (s *JoinService) roleForManagedServer(srv server.Server, task tasks.Task) string {
	if nomadHTTPAddressMatchesServer(s.currentConfig().Address, srv) {
		return ProjectedNodeRoleServer
	}
	if task.Type == TaskTypeServerSwitch && !taskCompletedRemove(task) {
		return ProjectedNodeRoleServer
	}
	if task.ID != "" && task.Status != tasks.StatusCompleted {
		if role := roleForTask(task); role == ProjectedNodeRoleServer || role == ProjectedNodeRoleClient {
			return role
		}
	}
	return ProjectedNodeRoleClient
}

func projectionStatus(task tasks.Task) string {
	if task.Status == tasks.StatusFailed || task.Status == tasks.StatusBlocked {
		return "failed"
	}
	if task.Type == TaskTypeNodeRemove {
		return "removing"
	}
	if task.Status == tasks.StatusCompleted {
		return "registering"
	}
	if task.Type == TaskTypeClusterRebuild {
		return "rebuilding"
	}
	if task.Type == TaskTypeServerBootstrap {
		return "bootstrapping"
	}
	if task.Type == TaskTypeClientJoin {
		return "joining"
	}
	return firstNonEmpty(task.Status, "pending")
}
