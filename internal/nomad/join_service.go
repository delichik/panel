package nomad

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
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

type nodeClient interface {
	Nodes(ctx context.Context) ([]NodeListItem, error)
}

type addressSetter interface {
	SetAddress(address string)
}

type JoinService struct {
	servers *server.Service
	nomad   nodeClient
	exec    sshx.RemoteExecutor
	tasks   *tasks.Service
	cfg     config.NomadConfig
}

func NewJoinService(servers *server.Service, nomadClient nodeClient, exec sshx.RemoteExecutor, taskSvc *tasks.Service, cfg config.NomadConfig) *JoinService {
	return &JoinService{servers: servers, nomad: nomadClient, exec: exec, tasks: taskSvc, cfg: cfg}
}

func (s *JoinService) Candidates(ctx context.Context) ([]server.Server, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := s.nomad.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	managed := map[string]struct{}{}
	for _, node := range nodes {
		if node.Meta == nil {
			continue
		}
		if serverID := strings.TrimSpace(node.Meta["panel_server_id"]); serverID != "" {
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
		setter.SetAddress("http://" + net.JoinHostPort(srv.Host, "4646"))
	}
	go s.runBootstrapServer(context.Background(), task.ID, srv)
	return task, nil
}

func (s *JoinService) runJoinClient(ctx context.Context, taskID string, srv server.Server) {
	_ = s.tasks.Start(ctx, taskID)
	_ = s.tasks.Advance(ctx, taskID, "installing", "installing or updating Nomad client")
	target := srv.Target()
	err := s.execSudoLogged(ctx, taskID, target, s.joinScript(srv))
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "registered", "Nomad client service restarted")
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
	_ = s.tasks.Complete(ctx, taskID, "Nomad server bootstrap requested")
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

func (s *JoinService) joinScript(srv server.Server) string {
	nodeName := safeNodeName("panel-" + srv.ID)
	rpc := nomadRPCAddress(s.cfg.Address)
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
install -d -m 0755 /etc/nomad.d /opt/nomad/data
cat >/etc/nomad.d/panel-client.hcl <<'EOF'
name = "%s"
datacenter = "%s"
data_dir = "/opt/nomad/data"
bind_addr = "0.0.0.0"

client {
  enabled = true
  meta {
    panel_server_id = "%s"
    panel_server_name = "%s"
  }
}

server_join {
  retry_join = ["%s"]
}
EOF
systemctl enable nomad
systemctl restart nomad
nomad version
`, nodeName, shellEscapeHCL(datacenter), srv.ID, shellEscapeHCL(srv.Name), shellEscapeHCL(rpc))
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
install -d -m 0755 /etc/nomad.d /opt/nomad/data
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
  }
}
EOF
systemctl enable nomad
systemctl restart nomad
nomad version
`, nodeName, shellEscapeHCL(datacenter), srv.ID, shellEscapeHCL(srv.Name))
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
