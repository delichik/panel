package nomad

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/config"
	"panel/internal/panelerr"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

const TaskTypeClientJoin = "nomad_client_join"
const TaskTypeServerBootstrap = "nomad_server_bootstrap"
const TaskTypeNodeRemove = "nomad_node_remove"

type nodeClient interface {
	Nodes(ctx context.Context) ([]NodeListItem, error)
}

type nodePurger interface {
	PurgeNode(ctx context.Context, id string) error
}

type reverseProxyJobClient interface {
	ValidateJob(ctx context.Context, job Job) (ValidateResponse, error)
	PlanJob(ctx context.Context, id string, job Job) (PlanResponse, error)
	RegisterJob(ctx context.Context, id string, job Job) (RegisterResponse, error)
	StopJob(ctx context.Context, id string, purge bool) (StopResponse, error)
}

type addressSetter interface {
	SetAddress(address string)
}

type JoinService struct {
	servers        *server.Service
	nomad          nodeClient
	exec           sshx.RemoteExecutor
	tasks          *tasks.Service
	cfg            config.NomadConfig
	appProxySource applicationProxySource
}

func NewJoinService(servers *server.Service, nomadClient nodeClient, exec sshx.RemoteExecutor, taskSvc *tasks.Service, cfg config.NomadConfig) *JoinService {
	return &JoinService{servers: servers, nomad: nomadClient, exec: exec, tasks: taskSvc, cfg: cfg}
}

type applicationProxySource interface {
	ApplicationReverseProxyConfigs(ctx context.Context) ([]ApplicationReverseProxyConfig, error)
}

type ApplicationReverseProxyConfig struct {
	ApplicationID     string
	ApplicationName   string
	DeploymentMode    string
	DeploymentServers []string
	Routes            []ReverseProxyRoute
}

func (s *JoinService) SetApplicationProxySource(source applicationProxySource) {
	s.appProxySource = source
}

func (s *JoinService) Candidates(ctx context.Context) ([]server.Server, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.nomad.Nodes(ctx)
	if err != nil {
		nodes = nil
	}
	latestTasks, err := s.latestNomadTasks(ctx)
	if err != nil {
		return nil, err
	}
	managed := map[string]struct{}{}
	for _, node := range nodes {
		if node.Meta == nil {
			continue
		}
		if serverID := strings.TrimSpace(node.Meta["panel_server_id"]); serverID != "" {
			if taskCompletedRemove(latestTasks[serverID]) {
				continue
			}
			managed[serverID] = struct{}{}
		}
	}
	out := []server.Server{}
	for _, srv := range servers {
		if _, ok := managed[srv.ID]; ok {
			continue
		}
		out = append(out, srv)
	}
	return out, nil
}

func (s *JoinService) JoinClient(ctx context.Context, serverID string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_join_executor_unavailable", "Nomad client join executor is unavailable")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	candidates, err := s.Candidates(ctx)
	if err != nil {
		return tasks.Task{}, err
	}
	allowed := false
	for _, candidate := range candidates {
		if candidate.ID == serverID {
			allowed = true
			break
		}
	}
	if !allowed {
		return tasks.Task{}, panelerr.Conflict("nomad_node_already_managed", "Server is already linked to a Nomad node")
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeClientJoin,
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Joining server to Nomad",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runJoinClient(context.Background(), task.ID, srv)
	return task, nil
}

func (s *JoinService) BootstrapServer(ctx context.Context, serverID string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_bootstrap_executor_unavailable", "Nomad server bootstrap executor is unavailable")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeServerBootstrap,
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Bootstrapping Nomad server",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	if setter, ok := s.nomad.(addressSetter); ok {
		s.setNomadAddress(setter, "http://"+net.JoinHostPort(srv.Host, "4646"))
	}
	go s.runBootstrapServer(context.Background(), task.ID, srv)
	return task, nil
}

type RemoveNodeInput struct {
	ServerID string `json:"serverId"`
	NodeID   string `json:"nodeId"`
}

type ReverseProxyInput struct {
	ServerID    string                   `json:"serverId"`
	Enabled     bool                     `json:"enabled"`
	StaticFiles bool                     `json:"staticFiles"`
	StaticSites []ReverseProxyStaticSite `json:"staticSites"`
}

type ReverseProxyRoute struct {
	Domain     string             `json:"domain"`
	TargetPort int                `json:"targetPort"`
	Paths      []ReverseProxyPath `json:"paths"`
}

type ReverseProxyPath struct {
	Path      string `json:"path"`
	WebSocket bool   `json:"webSocket"`
}

type ReverseProxyStaticSite struct {
	Domain string `json:"domain"`
	Root   string `json:"root"`
	Index  string `json:"index"`
}

func (s *JoinService) UpdateReverseProxy(ctx context.Context, in ReverseProxyInput) (server.Server, error) {
	serverID := strings.TrimSpace(in.ServerID)
	if serverID == "" {
		return server.Server{}, panelerr.Validation("nomad_reverse_proxy_server_required", "Server ID is required")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return server.Server{}, err
	}
	staticSites, err := normalizeReverseProxyStaticSites(in.StaticSites)
	if err != nil {
		return server.Server{}, err
	}
	traits := map[string]string{}
	for key, value := range srv.Traits {
		traits[key] = value
	}
	if !in.Enabled {
		in.StaticFiles = false
		staticSites = nil
	}
	if len(staticSites) > 0 {
		in.StaticFiles = true
	}
	staticJSON, _ := json.Marshal(staticSites)
	traits[TraitReverseProxyEnabled] = boolTrait(in.Enabled)
	traits[TraitReverseProxyStaticFiles] = boolTrait(in.StaticFiles)
	traits[TraitReverseProxyStaticSites] = string(staticJSON)
	updated, err := s.servers.Update(ctx, serverID, server.SaveRequest{
		Name:         srv.Name,
		Host:         srv.Host,
		Port:         srv.Port,
		SSHUsername:  srv.SSHUsername,
		CredentialID: srv.CredentialID,
		Traits:       traits,
		Notes:        srv.Notes,
	})
	if err != nil {
		return server.Server{}, err
	}
	if err := s.reconcileReverseProxyJob(ctx); err != nil {
		return server.Server{}, err
	}
	return updated, nil
}

func (s *JoinService) RemoveNode(ctx context.Context, in RemoveNodeInput) (tasks.Task, error) {
	if strings.TrimSpace(in.ServerID) == "" && strings.TrimSpace(in.NodeID) == "" {
		return tasks.Task{}, panelerr.Validation("nomad_remove_target_required", "Server ID or Nomad node ID is required")
	}
	var srv server.Server
	if strings.TrimSpace(in.ServerID) != "" {
		var err error
		srv, err = s.servers.Get(ctx, in.ServerID)
		if err != nil {
			return tasks.Task{}, err
		}
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeNodeRemove,
		ServerID:     in.ServerID,
		NodeID:       firstNonEmpty(in.NodeID, in.ServerID),
		ResourceType: "nomad_node",
		ResourceID:   firstNonEmpty(in.NodeID, in.ServerID),
		TriggerType:  "user",
		Summary:      "Removing Nomad node",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRemoveNode(context.Background(), task.ID, srv, strings.TrimSpace(in.NodeID))
	return task, nil
}

func (s *JoinService) restoreNomadAddressFromBootstrap(ctx context.Context) {
	setter, ok := s.nomad.(addressSetter)
	if !ok || !isLocalRPCAddress(nomadRPCAddress(s.cfg.Address)) {
		return
	}
	task := s.latestCompletedBootstrapTask(ctx)
	if task.ServerID == "" {
		return
	}
	srv, err := s.servers.Get(ctx, task.ServerID)
	if err != nil || strings.TrimSpace(srv.Host) == "" {
		return
	}
	s.setNomadAddress(setter, "http://"+net.JoinHostPort(srv.Host, "4646"))
}

func (s *JoinService) RestoreNomadAddressFromBootstrap(ctx context.Context) {
	s.restoreNomadAddressFromBootstrap(ctx)
}

func (s *JoinService) setNomadAddress(setter addressSetter, address string) {
	setter.SetAddress(address)
	s.cfg.Address = address
}

func (s *JoinService) runJoinClient(ctx context.Context, taskID string, srv server.Server) {
	_ = s.tasks.Start(ctx, taskID)
	_ = s.tasks.Advance(ctx, taskID, "installing", "installing or updating Nomad client")
	target := srv.Target()
	err := s.execSudoLogged(ctx, taskID, target, s.joinScript(srv, s.serverJoinRPCAddress(ctx)))
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "registered", "Nomad client service restarted")
	s.reconcileReverseProxyJobLogged(ctx, taskID)
	_ = s.tasks.Complete(ctx, taskID, "Nomad client join requested")
}

func (s *JoinService) runBootstrapServer(ctx context.Context, taskID string, srv server.Server) {
	_ = s.tasks.Start(ctx, taskID)
	_ = s.tasks.Advance(ctx, taskID, "bootstrapping", "installing and starting Nomad server")
	target := srv.Target()
	err := s.execSudoLogged(ctx, taskID, target, s.bootstrapScript(srv))
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "started", "Nomad server service restarted")
	s.reconcileReverseProxyJobLogged(ctx, taskID)
	_ = s.tasks.Complete(ctx, taskID, "Nomad server bootstrap requested")
}

func (s *JoinService) reconcileReverseProxyJobLogged(ctx context.Context, taskID string) {
	if err := s.reconcileReverseProxyJob(ctx); err != nil {
		_ = s.tasks.AppendLog(ctx, taskID, "stderr", "reverse proxy reconcile failed: "+err.Error())
	}
}

func (s *JoinService) runRemoveNode(ctx context.Context, taskID string, srv server.Server, nodeID string) {
	_ = s.tasks.Start(ctx, taskID)
	if srv.ID != "" {
		if s.exec == nil {
			_ = s.tasks.Fail(ctx, taskID, panelerr.Validation("nomad_remove_executor_unavailable", "Nomad remove executor is unavailable"))
			return
		}
		_ = s.tasks.Advance(ctx, taskID, "stopping", "stopping Nomad on managed server")
		if err := s.execSudoLogged(ctx, taskID, srv.Target(), removeNodeScript()); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	if nodeID != "" {
		purger, ok := s.nomad.(nodePurger)
		if !ok {
			_ = s.tasks.Fail(ctx, taskID, panelerr.Validation("nomad_remove_api_unavailable", "Nomad node purge API is unavailable"))
			return
		}
		_ = s.tasks.Advance(ctx, taskID, "purging", "purging Nomad node registration")
		if err := purger.PurgeNode(ctx, nodeID); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	_ = s.tasks.Complete(ctx, taskID, "Nomad node remove requested")
}

func (s *JoinService) execSudoLogged(ctx context.Context, taskID string, target sshx.Target, command string) error {
	stdoutStreamed := false
	stderrStreamed := false
	res, err := s.exec.ExecSudo(ctx, target, sshx.CommandSpec{
		Command: command,
		Timeout: 5 * time.Minute,
		OnStdout: func(line string) {
			stdoutStreamed = true
			_ = s.tasks.AppendLog(ctx, taskID, "stdout", line)
		},
		OnStderr: func(line string) {
			stderrStreamed = true
			_ = s.tasks.AppendLog(ctx, taskID, "stderr", line)
		},
	})
	if !stdoutStreamed {
		s.appendCommandOutput(ctx, taskID, "stdout", res.Stdout)
	}
	if !stderrStreamed {
		s.appendCommandOutput(ctx, taskID, "stderr", res.Stderr)
	}
	return err
}

func (s *JoinService) appendCommandOutput(ctx context.Context, taskID, stream, out string) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			_ = s.tasks.AppendLog(ctx, taskID, stream, line)
		}
	}
}

func (s *JoinService) joinScript(srv server.Server, rpc string) string {
	nodeName := safeNodeName("panel-" + srv.ID)
	datacenter := firstNonEmpty(strings.TrimSpace(s.cfg.Datacenter), "dc1")
	return fmt.Sprintf(`set -eu
export DEBIAN_FRONTEND=noninteractive
if ! command -v nomad >/dev/null 2>&1; then
  apt-get update
  apt-get install -y gpg wget lsb-release
  wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
  echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" > /etc/apt/sources.list.d/hashicorp.list
  apt-get update
  apt-get install -y nomad
fi
%s
%s
cat >/etc/nomad.d/panel-client.hcl <<'EOF'
name = "%s"
datacenter = "%s"
data_dir = "/opt/nomad/data"
bind_addr = "0.0.0.0"

server {
  enabled = false
}

client {
  enabled = true
  servers = ["%s"]
  meta {
    panel_server_id = "%s"
    panel_server_name = "%s"
    panel_reverse_proxy_enabled = "%s"
    panel_reverse_proxy_static_files = "%s"
  }
}
EOF
systemctl enable nomad
systemctl restart nomad
systemctl is-active --quiet nomad
nomad version
`, runtimePrereqsScript(), resetNomadConfigScript(), nodeName, shellEscapeHCL(datacenter), shellEscapeHCL(rpc), srv.ID, shellEscapeHCL(srv.Name), boolTrait(traitBool(srv.Traits, TraitReverseProxyEnabled)), boolTrait(traitBool(srv.Traits, TraitReverseProxyStaticFiles)))
}

func (s *JoinService) bootstrapScript(srv server.Server) string {
	nodeName := safeNodeName("panel-" + srv.ID)
	datacenter := firstNonEmpty(strings.TrimSpace(s.cfg.Datacenter), "dc1")
	return fmt.Sprintf(`set -eu
export DEBIAN_FRONTEND=noninteractive
if ! command -v nomad >/dev/null 2>&1; then
  apt-get update
  apt-get install -y gpg wget lsb-release
  wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
  echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" > /etc/apt/sources.list.d/hashicorp.list
  apt-get update
  apt-get install -y nomad
fi
%s
%s
cat >/etc/nomad.d/panel-server.hcl <<'EOF'
name = "%s"
datacenter = "%s"
data_dir = "/opt/nomad/data"
bind_addr = "0.0.0.0"

server {
  enabled = true
  bootstrap_expect = 1
}

client {
  enabled = true
  meta {
    panel_server_id = "%s"
    panel_server_name = "%s"
    panel_reverse_proxy_enabled = "%s"
    panel_reverse_proxy_static_files = "%s"
  }
}
EOF
systemctl enable nomad
systemctl restart nomad
systemctl is-active --quiet nomad
nomad version
`, runtimePrereqsScript(), resetNomadConfigScript(), nodeName, shellEscapeHCL(datacenter), srv.ID, shellEscapeHCL(srv.Name), boolTrait(traitBool(srv.Traits, TraitReverseProxyEnabled)), boolTrait(traitBool(srv.Traits, TraitReverseProxyStaticFiles)))
}

func (s *JoinService) serverJoinRPCAddress(ctx context.Context) string {
	configured := nomadRPCAddress(s.cfg.Address)
	if !isLocalRPCAddress(configured) {
		return configured
	}
	if client, ok := s.nomad.(statusClient); ok {
		status, err := client.Status(ctx)
		if err == nil && status.Connected {
			if rpc := normalizeNomadRPCAddress(status.Leader); rpc != "" && !isLocalRPCAddress(rpc) {
				return rpc
			}
		}
	}
	latest := s.latestCompletedBootstrapTask(ctx)
	if latest.ServerID != "" {
		if srv, err := s.servers.Get(ctx, latest.ServerID); err == nil && strings.TrimSpace(srv.Host) != "" {
			return net.JoinHostPort(srv.Host, "4647")
		}
	}
	return configured
}

func (s *JoinService) latestCompletedBootstrapTask(ctx context.Context) tasks.Task {
	latestTasks, err := s.latestNomadTasks(ctx)
	if err != nil {
		return tasks.Task{}
	}
	var latest tasks.Task
	for _, task := range latestTasks {
		if task.Type != TaskTypeServerBootstrap || task.Status != tasks.StatusCompleted {
			continue
		}
		if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
			latest = task
		}
	}
	return latest
}

func runtimePrereqsScript() string {
	return `if ! command -v docker >/dev/null 2>&1; then
  apt-get update
  apt-get install -y docker.io
fi
systemctl enable docker
systemctl restart docker
if [ ! -x /opt/cni/bin/bridge ]; then
  apt-get update
  apt-get install -y containernetworking-plugins
  install -d -m 0755 /opt/cni/bin
  for plugin in bridge firewall host-local loopback portmap; do
    for dir in /usr/lib/cni /usr/libexec/cni /opt/cni/bin; do
      if [ -x "$dir/$plugin" ] && [ "$dir" != "/opt/cni/bin" ]; then
        cp "$dir/$plugin" "/opt/cni/bin/$plugin"
      fi
    done
  done
fi`
}

func resetNomadConfigScript() string {
	return `install -d -m 0755 /etc/nomad.d /opt/nomad/data
find /etc/nomad.d -maxdepth 1 -type f \( -name '*.hcl' -o -name '*.json' \) -delete`
}

func removeNodeScript() string {
	return `set -eu
systemctl disable --now nomad || true
systemctl reset-failed nomad || true
` + resetNomadConfigScript()
}

func (s *JoinService) ReconcileReverseProxy(ctx context.Context) error {
	return s.reconcileReverseProxyJob(ctx)
}

func (s *JoinService) reconcileReverseProxyJob(ctx context.Context) error {
	client, ok := s.nomad.(reverseProxyJobClient)
	if !ok {
		return nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		return err
	}
	var appConfigs []ApplicationReverseProxyConfig
	if s.appProxySource != nil {
		appConfigs, err = s.appProxySource.ApplicationReverseProxyConfigs(ctx)
		if err != nil {
			return err
		}
	}
	job, enabled := s.renderReverseProxyJob(servers, appConfigs)
	if !enabled {
		_, err := client.StopJob(ctx, reverseProxyJobID, true)
		if err != nil && !strings.Contains(err.Error(), "404") {
			return err
		}
		return nil
	}
	if _, err := client.ValidateJob(ctx, job); err != nil {
		return err
	}
	if _, err := client.PlanJob(ctx, job.ID, job); err != nil {
		return err
	}
	_, err = client.RegisterJob(ctx, job.ID, job)
	return err
}

const reverseProxyJobID = "panel-nginx"

func (s *JoinService) renderReverseProxyJob(servers []server.Server, appConfigs []ApplicationReverseProxyConfig) (Job, bool) {
	datacenter := firstNonEmpty(strings.TrimSpace(s.cfg.Datacenter), "dc1")
	job := Job{
		ID:          reverseProxyJobID,
		Name:        "panel-nginx",
		Type:        "service",
		Namespace:   firstNonEmpty(strings.TrimSpace(s.cfg.Namespace), "default"),
		Region:      firstNonEmpty(strings.TrimSpace(s.cfg.Region), "global"),
		Datacenters: []string{datacenter},
		Meta: map[string]string{
			"panel.component": "reverse-proxy",
		},
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].ID < servers[j].ID })
	for _, srv := range servers {
		if !traitBool(srv.Traits, TraitReverseProxyEnabled) {
			continue
		}
		appSnippets := reverseProxyAppSnippetsForServer(srv.ID, appConfigs)
		staticSites := reverseProxyStaticSitesFromTraits(srv.Traits)
		templates := []Template{{
			EmbeddedTmpl: renderNginxBaseConfig(),
			DestPath:     "local/nginx.conf",
			Perms:        "0644",
			ChangeMode:   "restart",
		}, {
			EmbeddedTmpl: "# Managed by Panel. Empty default include.\n",
			DestPath:     "local/nginx.conf.d/panel-empty.conf",
			Perms:        "0644",
			ChangeMode:   "restart",
		}}
		for _, snippet := range appSnippets {
			templates = append(templates, Template{
				EmbeddedTmpl: snippet.Content,
				DestPath:     "local/nginx.conf.d/" + snippet.Name,
				Perms:        "0644",
				ChangeMode:   "restart",
			})
		}
		if len(staticSites) > 0 {
			templates = append(templates, Template{
				EmbeddedTmpl: renderNginxStaticConfig(staticSites),
				DestPath:     "local/nginx.conf.d/panel-static.conf",
				Perms:        "0644",
				ChangeMode:   "restart",
			})
		}
		group := TaskGroup{
			Name:  "nginx-" + safeNodeName(srv.ID),
			Count: 1,
			Constraints: []Constraint{{
				LTarget: "${meta.panel_server_id}",
				Operand: "=",
				RTarget: srv.ID,
			}},
			Networks: []Network{{Mode: "host"}},
			Tasks: []Task{{
				Name:   "nginx",
				Driver: "docker",
				Config: map[string]any{
					"image":        "nginx:1.27",
					"network_mode": "host",
					"command":      "nginx",
					"args":         []string{"-g", "daemon off;", "-c", "/local/nginx.conf"},
					"mounts":       nginxStaticMounts(staticSites),
				},
				Templates: templates,
				Resources: &Resources{CPU: 100, MemoryMB: 128},
			}},
		}
		job.TaskGroups = append(job.TaskGroups, group)
	}
	return job, len(job.TaskGroups) > 0
}

type nginxSnippet struct {
	Name    string
	Content string
}

func reverseProxyAppSnippetsForServer(serverID string, apps []ApplicationReverseProxyConfig) []nginxSnippet {
	out := []nginxSnippet{}
	for _, app := range apps {
		if len(app.Routes) == 0 || !applicationTargetsServer(app, serverID) {
			continue
		}
		out = append(out, nginxSnippet{
			Name:    reverseProxyAppConfigName(app),
			Content: renderNginxProxyConfig(app.ApplicationName, app.Routes),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func applicationTargetsServer(app ApplicationReverseProxyConfig, serverID string) bool {
	if strings.TrimSpace(app.DeploymentMode) == "" || app.DeploymentMode == "all" {
		return true
	}
	for _, target := range app.DeploymentServers {
		if target == serverID {
			return true
		}
	}
	return false
}

func reverseProxyAppConfigName(app ApplicationReverseProxyConfig) string {
	name := safeNodeName(app.ApplicationName)
	if name == "panel-node" {
		name = safeNodeName(app.ApplicationID)
	}
	return "panel-" + name + ".conf"
}

func renderNginxBaseConfig() string {
	return `worker_processes auto;
error_log /dev/stderr;
pid /tmp/nginx.pid;

events {
    worker_connections 1024;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;
    access_log /dev/stdout;
    client_max_body_size 50m;

    map $http_upgrade $connection_upgrade {
        default upgrade;
        ''      close;
    }

    server {
        listen 80 default_server;
        return 404;
    }

    include /local/nginx.conf.d/*.conf;
}
`
}

func renderNginxProxyConfig(appName string, routes []ReverseProxyRoute) string {
	var b strings.Builder
	b.WriteString("# Managed by Panel. Application: ")
	b.WriteString(appName)
	b.WriteString("\n")
	for _, route := range routes {
		b.WriteString("\nserver {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    server_name ")
		b.WriteString(route.Domain)
		b.WriteString(";\n")
		for _, item := range route.Paths {
			b.WriteString("\n    location ")
			b.WriteString(item.Path)
			b.WriteString(" {\n")
			b.WriteString("        proxy_pass http://127.0.0.1:")
			b.WriteString(strconv.Itoa(route.TargetPort))
			b.WriteString(";\n")
			b.WriteString("        proxy_set_header Host $host;\n")
			b.WriteString("        proxy_set_header X-Real-IP $remote_addr;\n")
			b.WriteString("        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
			b.WriteString("        proxy_set_header X-Forwarded-Proto $scheme;\n")
			if item.WebSocket {
				b.WriteString("        proxy_http_version 1.1;\n")
				b.WriteString("        proxy_set_header Upgrade $http_upgrade;\n")
				b.WriteString("        proxy_set_header Connection $connection_upgrade;\n")
			}
			b.WriteString("    }\n")
		}
		b.WriteString("}\n")
	}
	return b.String()
}

func renderNginxStaticConfig(staticSites []ReverseProxyStaticSite) string {
	var b strings.Builder
	b.WriteString("# Managed by Panel. Node static sites.\n")
	for index, site := range staticSites {
		root := "/panel-static/static-" + strconv.Itoa(index)
		b.WriteString("\nserver {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    server_name ")
		b.WriteString(site.Domain)
		b.WriteString(";\n")
		b.WriteString("    root ")
		b.WriteString(root)
		b.WriteString(";\n")
		b.WriteString("    index ")
		b.WriteString(site.Index)
		b.WriteString(";\n")
		b.WriteString("    location / {\n")
		b.WriteString("        try_files $uri $uri/ /")
		b.WriteString(firstIndex(site.Index))
		b.WriteString(";\n")
		b.WriteString("    }\n")
		b.WriteString("}\n")
	}
	return b.String()
}

func nginxStaticMounts(sites []ReverseProxyStaticSite) []map[string]any {
	mounts := make([]map[string]any, 0, len(sites))
	for index, site := range sites {
		mounts = append(mounts, map[string]any{
			"type":     "bind",
			"source":   site.Root,
			"target":   "/panel-static/static-" + strconv.Itoa(index),
			"readonly": true,
		})
	}
	return mounts
}

func firstIndex(index string) string {
	fields := strings.Fields(index)
	if len(fields) == 0 {
		return "index.html"
	}
	return fields[0]
}

func normalizeReverseProxyRoutes(routes []ReverseProxyRoute) ([]ReverseProxyRoute, error) {
	out := make([]ReverseProxyRoute, 0, len(routes))
	for _, route := range routes {
		domain := strings.TrimSpace(route.Domain)
		if domain == "" {
			continue
		}
		if !validNginxToken(domain) {
			return nil, panelerr.Validation("nomad_reverse_proxy_domain_invalid", "reverse proxy domain is invalid")
		}
		if route.TargetPort <= 0 || route.TargetPort > 65535 {
			return nil, panelerr.Validation("nomad_reverse_proxy_port_invalid", "target port must be between 1 and 65535")
		}
		paths := make([]ReverseProxyPath, 0, len(route.Paths))
		for _, item := range route.Paths {
			proxyPath := strings.TrimSpace(item.Path)
			if proxyPath == "" {
				proxyPath = "/"
			}
			if !strings.HasPrefix(proxyPath, "/") || !validNginxPath(proxyPath) {
				return nil, panelerr.Validation("nomad_reverse_proxy_path_invalid", "reverse proxy path must start with /")
			}
			paths = append(paths, ReverseProxyPath{Path: proxyPath, WebSocket: item.WebSocket})
		}
		if len(paths) == 0 {
			paths = append(paths, ReverseProxyPath{Path: "/"})
		}
		out = append(out, ReverseProxyRoute{Domain: domain, TargetPort: route.TargetPort, Paths: paths})
	}
	return out, nil
}

func normalizeReverseProxyStaticSites(sites []ReverseProxyStaticSite) ([]ReverseProxyStaticSite, error) {
	out := make([]ReverseProxyStaticSite, 0, len(sites))
	for _, site := range sites {
		domain := strings.TrimSpace(site.Domain)
		sourceRoot := strings.TrimSpace(site.Root)
		root := path.Clean(sourceRoot)
		index := strings.TrimSpace(site.Index)
		if domain == "" && sourceRoot == "" {
			continue
		}
		if !validNginxToken(domain) {
			return nil, panelerr.Validation("nomad_static_domain_invalid", "static site domain is invalid")
		}
		if sourceRoot == "" || !strings.HasPrefix(sourceRoot, "/") || root == "." {
			return nil, panelerr.Validation("nomad_static_root_invalid", "static site root must be an absolute path")
		}
		if index == "" {
			index = "index.html"
		}
		for _, item := range strings.Fields(index) {
			if !validNginxToken(item) || strings.Contains(item, "/") {
				return nil, panelerr.Validation("nomad_static_index_invalid", "static site index is invalid")
			}
		}
		out = append(out, ReverseProxyStaticSite{Domain: domain, Root: root, Index: index})
	}
	return out, nil
}

func reverseProxyStaticSitesFromTraits(traits map[string]string) []ReverseProxyStaticSite {
	var sites []ReverseProxyStaticSite
	_ = json.Unmarshal([]byte(strings.TrimSpace(traits[TraitReverseProxyStaticSites])), &sites)
	sites, _ = normalizeReverseProxyStaticSites(sites)
	return sites
}

func validNginxToken(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n;{}")
}

func validNginxPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n;{}")
}

func nomadRPCAddress(address string) string {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u.Host == "" {
		return "127.0.0.1:4647"
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" || port == "4646" {
		port = "4647"
	}
	return net.JoinHostPort(host, port)
}

func normalizeNomadRPCAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.Contains(address, "://") {
		return nomadRPCAddress(address)
	}
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		if port == "" {
			port = "4647"
		}
		return net.JoinHostPort(host, port)
	}
	return net.JoinHostPort(address, "4647")
}

func isLocalRPCAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

var nodeNamePattern = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func safeNodeName(name string) string {
	name = nodeNamePattern.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "panel-node"
	}
	return name
}

func shellEscapeHCL(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func boolTrait(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
