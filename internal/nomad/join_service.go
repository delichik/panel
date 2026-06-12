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
	"panel/internal/linux"
	"panel/internal/panelerr"
	"panel/internal/remoteops"
	"panel/internal/server"
	"panel/internal/sshx"
	"panel/internal/tasks"
)

const TaskTypeClientJoin = "nomad_client_join"
const TaskTypeServerBootstrap = "nomad_server_bootstrap"
const TaskTypeNodeRemove = "nomad_node_remove"
const TaskTypeClusterRebuild = "nomad_cluster_rebuild"
const TaskTypeServerSwitch = "nomad_server_switch"
const TaskTypeReverseProxySync = "nomad_reverse_proxy_sync"
const TaskTypeTLSRotate = "nomad_tls_rotate"

const (
	nomadInstallTimeout                = 20 * time.Minute
	nomadMaintenanceTimeout            = 2 * time.Minute
	nomadFirewallTimeout               = time.Minute
	nomadServiceTimeout                = 3 * time.Minute
	nomadLocalHealthTimeout            = 2 * time.Minute
	nomadPanelReachabilityRetryMessage = "checking Nomad API reachability from Panel"
)

var (
	nomadPanelReachabilityTimeout        = 30 * time.Second
	nomadPanelReachabilityRetryInterval  = 2 * time.Second
	nomadClientRegistrationTimeout       = 60 * time.Second
	nomadClientRegistrationRetryInterval = 2 * time.Second
)

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
	configProvider func(config.NomadConfig) config.NomadConfig
	tlsAssets      *TLSAssets
	dataRoot       string
	appProxySource applicationProxySource
	certSource     reverseProxyCertificateSource
	appRestorer    enabledApplicationRestorer
}

func NewJoinService(servers *server.Service, nomadClient nodeClient, exec sshx.RemoteExecutor, taskSvc *tasks.Service, cfg config.NomadConfig, tlsAssets *TLSAssets) *JoinService {
	return &JoinService{servers: servers, nomad: nomadClient, exec: exec, tasks: taskSvc, cfg: cfg, tlsAssets: tlsAssets}
}

func (s *JoinService) SetDataRoot(dataRoot string) {
	s.dataRoot = dataRoot
}

func (s *JoinService) BuiltinCertificates() ([]BuiltinCertificateInfo, error) {
	if s.tlsAssets == nil {
		return nil, panelerr.Validation("nomad_tls_unavailable", "Nomad TLS assets are unavailable")
	}
	return s.tlsAssets.CertificateInfo()
}

func (s *JoinService) RotateTLS(ctx context.Context) (tasks.Task, error) {
	if strings.TrimSpace(s.dataRoot) == "" {
		return tasks.Task{}, panelerr.Validation("nomad_tls_data_root_unavailable", "Nomad TLS data root is unavailable")
	}
	latest := s.latestCompletedBootstrapTask(ctx)
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type: TaskTypeTLSRotate, ServerID: latest.ServerID,
		ResourceType: "nomad_tls", ResourceID: "builtin",
		TriggerType: "user", Status: tasks.StatusRunning,
		Summary: "Rotating Nomad TLS certificates",
	})
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRotateTLS(context.Background(), task.ID, latest.ServerID)
	return task, nil
}

func (s *JoinService) runRotateTLS(ctx context.Context, taskID, serverID string) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Advance(ctx, taskID, "generating", "generating new Nomad CA and certificates")
	assets, err := RegenerateTLSAssets(s.dataRoot)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	*s.tlsAssets = *assets
	if reloader, ok := s.nomad.(interface{ ReloadTLS() error }); ok {
		if err := reloader.ReloadTLS(); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	if strings.TrimSpace(serverID) == "" {
		_ = s.tasks.Complete(ctx, taskID, "Nomad TLS certificates regenerated")
		return
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	adapter, err := s.ensureNomadEligible(srv)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	s.runRebuildCluster(ctx, taskID, srv, adapter)
}

func (s *JoinService) SetConfigProvider(provider func(config.NomadConfig) config.NomadConfig) {
	s.configProvider = provider
}

func (s *JoinService) currentConfig() config.NomadConfig {
	cfg := s.cfg
	if s.configProvider != nil {
		cfg = s.configProvider(cfg)
	}
	return cfg
}

func (s *JoinService) startWorkerTask(ctx context.Context, task tasks.Task) (tasks.Task, error) {
	if s.tasks == nil {
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	return s.tasks.Get(ctx, task.ID)
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

type reverseProxyCertificateSource interface {
	ReverseProxyCertificates(ctx context.Context) ([]ReverseProxyCertificate, error)
}

type enabledApplicationRestorer interface {
	RedeployEnabledApplications(ctx context.Context) (int, error)
}

type ReverseProxyCertificate struct {
	ID             string
	Domains        []string
	CertificatePEM string
	PrivateKeyPEM  string
}

func (s *JoinService) SetReverseProxyCertificateSource(source reverseProxyCertificateSource) {
	s.certSource = source
}

func (s *JoinService) SetEnabledApplicationRestorer(restorer enabledApplicationRestorer) {
	s.appRestorer = restorer
}

func (s *JoinService) ensureNomadEligible(srv server.Server) (linux.DistroAdapter, error) {
	adapter, ok := linux.AdapterFor(srv.OS)
	if !srv.OS.Supported || !ok {
		return nil, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !srv.Reachable {
		return nil, panelerr.Validation("server_not_reachable", "Server connectivity has not been confirmed")
	}
	if !srv.Sudo.Passwordless {
		return nil, panelerr.Validation("passwordless_sudo_required", "Passwordless sudo is required")
	}
	return adapter, nil
}

func serverAdvertiseAddress(srv server.Server) string {
	if address := strings.TrimSpace(srv.Traits[TraitAdvertiseAddress]); address != "" {
		return address
	}
	return strings.TrimSpace(srv.Traits[TraitServerAdvertiseAddress])
}

func serverNetworkAddresses(srv server.Server) []string {
	var out []string
	for _, raw := range strings.Split(srv.Traits["sys.network_interfaces"], ", ") {
		parts := strings.Split(raw, "|")
		if len(parts) < 3 {
			continue
		}
		address := strings.TrimSpace(parts[2])
		if host, _, err := net.ParseCIDR(address); err == nil {
			address = host.String()
		}
		if ip := net.ParseIP(strings.Trim(address, "[]")); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			out = append(out, ip.String())
		}
	}
	return out
}

func serverHostAddress(srv server.Server) string {
	host := strings.TrimSpace(srv.Host)
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

func serverAdvertiseAddressCandidates(srv server.Server) []string {
	out := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		ip := net.ParseIP(strings.Trim(strings.TrimSpace(value), "[]"))
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			return
		}
		address := ip.String()
		if _, ok := seen[address]; ok {
			return
		}
		seen[address] = struct{}{}
		out = append(out, address)
	}
	if existing := serverAdvertiseAddress(srv); existing != "" {
		add(existing)
	}
	if host := serverHostAddress(srv); host != "" {
		add(host)
	}
	for _, address := range serverNetworkAddresses(srv) {
		add(address)
	}
	return out
}

func (s *JoinService) saveServerAdvertiseAddress(ctx context.Context, srv server.Server, raw string) (server.Server, error) {
	ip := net.ParseIP(strings.Trim(strings.TrimSpace(raw), "[]"))
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return server.Server{}, panelerr.Validation("nomad_advertise_address_invalid", "Select a valid Nomad advertise address")
	}
	address := ip.String()
	found := false
	for _, candidate := range serverAdvertiseAddressCandidates(srv) {
		if sameHost(candidate, address) {
			found = true
			break
		}
	}
	if !found {
		return server.Server{}, panelerr.Validation("nomad_advertise_address_not_detected", "The selected Nomad address must match a detected server interface or the SSH host address")
	}
	traits := make(map[string]string, len(srv.Traits)+2)
	for key, value := range srv.Traits {
		traits[key] = value
	}
	traits[TraitAdvertiseAddress] = address
	traits[TraitServerAdvertiseAddress] = address
	return s.servers.Update(ctx, srv.ID, server.SaveRequest{
		Name:         srv.Name,
		Host:         srv.Host,
		Port:         srv.Port,
		SSHUsername:  srv.SSHUsername,
		CredentialID: srv.CredentialID,
		Traits:       traits,
		Notes:        srv.Notes,
	})
}

func nomadJoinEligible(srv server.Server) bool {
	if !srv.OS.Supported || !srv.Reachable || !srv.Sudo.Passwordless {
		return false
	}
	_, ok := linux.AdapterFor(srv.OS)
	return ok
}

func (s *JoinService) Candidates(ctx context.Context) ([]server.Server, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	nodesCtx, cancelNodes := context.WithTimeout(ctx, controlPlaneNomadQueryTimeout)
	defer cancelNodes()
	nodes, err := s.nomad.Nodes(nodesCtx)
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
		if !nomadJoinEligible(srv) {
			continue
		}
		if _, ok := managed[srv.ID]; ok {
			continue
		}
		out = append(out, srv)
	}
	return out, nil
}

func (s *JoinService) JoinClient(ctx context.Context, serverID string, advertiseAddress ...string) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_join_executor_unavailable", "Nomad client join executor is unavailable")
	}
	serverID = strings.TrimSpace(serverID)
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	rawAdvertiseAddress := ""
	if len(advertiseAddress) > 0 {
		rawAdvertiseAddress = advertiseAddress[0]
	}
	if strings.TrimSpace(rawAdvertiseAddress) != "" || serverAdvertiseAddress(srv) == "" {
		srv, err = s.saveServerAdvertiseAddress(ctx, srv, rawAdvertiseAddress)
		if err != nil {
			return tasks.Task{}, err
		}
	}
	adapter, err := s.ensureNomadEligible(srv)
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
	task, err = s.startWorkerTask(ctx, task)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runJoinClient(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *JoinService) BootstrapServer(ctx context.Context, in BootstrapServerInput) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_bootstrap_executor_unavailable", "Nomad server bootstrap executor is unavailable")
	}
	serverID := strings.TrimSpace(in.ServerID)
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	srv, err = s.saveServerAdvertiseAddress(ctx, srv, in.AdvertiseAddress)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, err := s.ensureNomadEligible(srv)
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
	task, err = s.startWorkerTask(ctx, task)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runBootstrapServer(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *JoinService) RedeployNode(ctx context.Context, in RedeployNodeInput) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_redeploy_executor_unavailable", "Nomad redeploy executor is unavailable")
	}
	serverID := strings.TrimSpace(in.ServerID)
	if serverID == "" {
		return tasks.Task{}, panelerr.Validation("nomad_redeploy_server_required", "Server ID is required")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, err := s.ensureNomadEligible(srv)
	if err != nil {
		return tasks.Task{}, err
	}
	role := normalizeProjectedNodeRole(in.Role)
	if role == "" || role == ProjectedNodeRoleUnknown {
		role = s.nomadRoleForServer(ctx, srv)
	}
	if role != ProjectedNodeRoleServer && role != ProjectedNodeRoleClient {
		return tasks.Task{}, panelerr.Validation("nomad_redeploy_role_required", "Nomad node role is required for redeploy")
	}
	if strings.TrimSpace(in.AdvertiseAddress) != "" || serverAdvertiseAddress(srv) == "" {
		srv, err = s.saveServerAdvertiseAddress(ctx, srv, in.AdvertiseAddress)
		if err != nil {
			return tasks.Task{}, err
		}
	}
	taskType := TaskTypeClientJoin
	summary := "Redeploying Nomad client"
	if role == ProjectedNodeRoleServer {
		taskType = TaskTypeServerBootstrap
		summary = "Redeploying Nomad server"
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         taskType,
		ServerID:     serverID,
		ResourceType: "server",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      summary,
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	task, err = s.startWorkerTask(ctx, task)
	if err != nil {
		return tasks.Task{}, err
	}
	if role == ProjectedNodeRoleServer {
		go s.runBootstrapServer(context.Background(), task.ID, srv, adapter)
		return task, nil
	}
	go s.runJoinClient(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *JoinService) RebuildCluster(ctx context.Context, in RebuildClusterInput) (tasks.Task, error) {
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_rebuild_executor_unavailable", "Nomad rebuild executor is unavailable")
	}
	serverID := strings.TrimSpace(in.ServerID)
	if serverID == "" {
		return tasks.Task{}, panelerr.Validation("nomad_rebuild_server_required", "Server ID is required")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, err := s.ensureNomadEligible(srv)
	if err != nil {
		return tasks.Task{}, err
	}
	srv, err = s.saveServerAdvertiseAddress(ctx, srv, in.AdvertiseAddress)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeClusterRebuild,
		ServerID:     serverID,
		ResourceType: "nomad_cluster",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Rebuilding Nomad cluster",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	task, err = s.startWorkerTask(ctx, task)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRebuildCluster(context.Background(), task.ID, srv, adapter)
	return task, nil
}

func (s *JoinService) SwitchServer(ctx context.Context, in SwitchServerInput) (tasks.Task, error) {
	setter, ok := s.nomad.(addressSetter)
	if !ok {
		return tasks.Task{}, panelerr.Validation("nomad_switch_api_unavailable", "Nomad server switch API is unavailable")
	}
	if s.exec == nil {
		return tasks.Task{}, panelerr.Validation("nomad_switch_executor_unavailable", "Nomad server switch executor is unavailable")
	}
	serverID := strings.TrimSpace(in.ServerID)
	if serverID == "" {
		return tasks.Task{}, panelerr.Validation("nomad_switch_server_required", "Server ID is required")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if strings.TrimSpace(srv.Host) == "" {
		return tasks.Task{}, panelerr.Validation("nomad_switch_server_host_required", "Server host is required")
	}
	srv, err = s.saveServerAdvertiseAddress(ctx, srv, in.AdvertiseAddress)
	if err != nil {
		return tasks.Task{}, err
	}
	adapter, err := s.ensureNomadEligible(srv)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := s.tasks.Create(ctx, tasks.CreateInput{
		Type:         TaskTypeServerSwitch,
		ServerID:     serverID,
		ResourceType: "nomad_server",
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      "Switching Nomad server",
		MaxRetries:   0,
	})
	if err != nil {
		return tasks.Task{}, err
	}
	task, err = s.startWorkerTask(ctx, task)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runSwitchServer(context.Background(), task.ID, srv, adapter, setter)
	return task, nil
}

type RemoveNodeInput struct {
	ServerID string `json:"serverId"`
	NodeID   string `json:"nodeId"`
}

type JoinClientInput struct {
	ServerID         string `json:"serverId"`
	AdvertiseAddress string `json:"advertiseAddress"`
}

type RedeployNodeInput struct {
	ServerID         string `json:"serverId"`
	Role             string `json:"role"`
	AdvertiseAddress string `json:"advertiseAddress"`
}

type BootstrapServerInput struct {
	ServerID         string `json:"serverId"`
	AdvertiseAddress string `json:"advertiseAddress"`
}

type RebuildClusterInput struct {
	ServerID         string `json:"serverId"`
	AdvertiseAddress string `json:"advertiseAddress"`
}

type SwitchServerInput struct {
	ServerID         string `json:"serverId"`
	AdvertiseAddress string `json:"advertiseAddress"`
}

type ReverseProxyInput struct {
	ServerID    string                   `json:"serverId"`
	Enabled     bool                     `json:"enabled"`
	StaticFiles bool                     `json:"staticFiles"`
	StaticSites []ReverseProxyStaticSite `json:"staticSites"`
}

type ReverseProxyUpdateResult struct {
	Server server.Server `json:"server"`
	TaskID string        `json:"taskId,omitempty"`
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

func (s *JoinService) UpdateReverseProxy(ctx context.Context, in ReverseProxyInput) (ReverseProxyUpdateResult, error) {
	serverID := strings.TrimSpace(in.ServerID)
	if serverID == "" {
		return ReverseProxyUpdateResult{}, panelerr.Validation("nomad_reverse_proxy_server_required", "Server ID is required")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return ReverseProxyUpdateResult{}, err
	}
	staticSites, err := normalizeReverseProxyStaticSites(in.StaticSites)
	if err != nil {
		return ReverseProxyUpdateResult{}, err
	}
	taskID := ""
	if s.tasks != nil {
		task, err := s.tasks.Create(ctx, tasks.CreateInput{
			Type:         TaskTypeReverseProxySync,
			ServerID:     srv.ID,
			ResourceType: "server",
			ResourceID:   srv.ID,
			TriggerType:  "user",
			Status:       tasks.StatusRunning,
			Summary:      "Synchronizing reverse proxy for " + firstNonEmpty(srv.Name, srv.ID),
		})
		if err != nil {
			return ReverseProxyUpdateResult{}, err
		}
		taskID = task.ID
		defer s.tasks.FinishExecution(taskID)
	}
	fail := func(err error) (ReverseProxyUpdateResult, error) {
		if s.tasks != nil && taskID != "" {
			_ = s.tasks.Fail(ctx, taskID, err)
		}
		return ReverseProxyUpdateResult{TaskID: taskID}, err
	}
	advance := func(stage, message string) error {
		if s.tasks == nil || taskID == "" {
			return nil
		}
		return s.tasks.Advance(ctx, taskID, stage, message)
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
	if in.Enabled {
		if _, err := s.ensureNomadEligible(srv); err != nil {
			return fail(err)
		}
		if s.exec != nil {
			if err := advance("opening_firewall", "opening reverse proxy firewall ports"); err != nil {
				return fail(err)
			}
			if taskID != "" {
				if err := s.execSudoLogged(ctx, taskID, srv.Target(), reverseProxyUFWAllowScript(), nomadFirewallTimeout); err != nil {
					return fail(err)
				}
			} else if _, err := (remoteops.Runner{Exec: s.exec, Target: srv.Target()}).RunSudoLogged(ctx, reverseProxyUFWAllowScript(), nomadFirewallTimeout); err != nil {
				return fail(err)
			}
		}
	}
	staticJSON, _ := json.Marshal(staticSites)
	traits[TraitReverseProxyEnabled] = boolTrait(in.Enabled)
	traits[TraitReverseProxyStaticFiles] = boolTrait(in.StaticFiles)
	traits[TraitReverseProxyStaticSites] = string(staticJSON)
	if err := advance("saving_config", "saving reverse proxy server traits"); err != nil {
		return fail(err)
	}
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
		return fail(err)
	}
	if err := advance("reconciling", "reconciling reverse proxy Nomad job"); err != nil {
		return fail(err)
	}
	if err := s.reconcileReverseProxyJob(ctx); err != nil {
		return fail(err)
	}
	if s.tasks != nil && taskID != "" {
		if err := s.tasks.Complete(ctx, taskID, "Reverse proxy synchronized"); err != nil {
			return ReverseProxyUpdateResult{}, err
		}
	}
	return ReverseProxyUpdateResult{Server: updated, TaskID: taskID}, nil
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
	task, err = s.startWorkerTask(ctx, task)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runRemoveNode(context.Background(), task.ID, srv, strings.TrimSpace(in.NodeID))
	return task, nil
}

func (s *JoinService) restoreNomadAddressFromBootstrap(ctx context.Context) {
	setter, ok := s.nomad.(addressSetter)
	cfg := s.currentConfig()
	if !ok || !isLocalRPCAddress(nomadRPCAddress(cfg.Address)) {
		return
	}
	task := s.latestCompletedBootstrapTask(ctx)
	if task.ServerID == "" {
		return
	}
	srv, err := s.servers.Get(ctx, task.ServerID)
	if err != nil || serverAdvertiseAddress(srv) == "" {
		return
	}
	s.setNomadAddress(setter, nomadHTTPAddressForServer(srv))
}

func (s *JoinService) RestoreNomadAddressFromBootstrap(ctx context.Context) {
	s.restoreNomadAddressFromBootstrap(ctx)
}

func (s *JoinService) setNomadAddress(setter addressSetter, address string) {
	setter.SetAddress(address)
	s.cfg.Address = address
}

type nomadAddressChange struct {
	setter   addressSetter
	previous string
	next     string
	active   bool
}

func (s *JoinService) beginNomadServerAddressChange(srv server.Server) nomadAddressChange {
	setter, ok := s.nomad.(addressSetter)
	if !ok {
		return nomadAddressChange{}
	}
	previous := s.currentConfig().Address
	next := nomadHTTPAddressForServer(srv)
	s.setNomadAddress(setter, next)
	return nomadAddressChange{setter: setter, previous: previous, next: next, active: true}
}

func (s *JoinService) restoreNomadAddressAfterFailure(ctx context.Context, taskID string, change nomadAddressChange) {
	if !change.active || strings.TrimSpace(change.previous) == "" || change.previous == change.next {
		return
	}
	_ = s.tasks.AppendLog(ctx, taskID, "system", "restoring previous Nomad server address after failed operation")
	s.setNomadAddress(change.setter, change.previous)
}

func (s *JoinService) runJoinClient(ctx context.Context, taskID string, srv server.Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	target := srv.Target()
	rpc, err := s.serverJoinRPCAddress(ctx)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.runNomadCommandSteps(ctx, taskID, target, []nomadCommandStep{
		{Stage: "installing_nomad", Message: "checking or installing Nomad", Command: adapter.NomadInstallScript(), Timeout: nomadInstallTimeout},
		{Stage: "preparing_runtime", Message: "checking Docker and Nomad CNI runtime", Command: adapter.NomadRuntimePrereqsScript(), Timeout: nomadInstallTimeout},
		{Stage: "configuring", Message: "writing Nomad client configuration", Command: s.joinConfigScript(srv, rpc), Timeout: nomadMaintenanceTimeout},
		{Stage: "opening_firewall", Message: "opening local Nomad firewall ports", Command: nomadUFWAllowScript(traitBool(srv.Traits, TraitReverseProxyEnabled)), Timeout: nomadFirewallTimeout},
		{Stage: "starting", Message: "starting Nomad client service", Command: adapter.NomadServiceRestartScript(), Timeout: nomadServiceTimeout},
		{Stage: "verifying_local", Message: "checking local Nomad client API", Command: nomadLocalHealthScript(), Timeout: nomadLocalHealthTimeout},
	}); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.waitForNomadClientRegistration(ctx, taskID, srv); err != nil {
		_ = s.execSudoLogged(ctx, taskID, target, nomadClusterDiagnosticsScript(), nomadMaintenanceTimeout)
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	s.reconcileReverseProxyJobLogged(ctx, taskID)
	_ = s.tasks.Complete(ctx, taskID, "Nomad client joined")
}

func (s *JoinService) runBootstrapServer(ctx context.Context, taskID string, srv server.Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	addressChange := s.beginNomadServerAddressChange(srv)
	keepAddress := false
	defer func() {
		if !keepAddress {
			s.restoreNomadAddressAfterFailure(ctx, taskID, addressChange)
		}
	}()
	if err := s.runBootstrapServerSteps(ctx, taskID, srv, adapter); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.waitForPanelNomadReachability(ctx, taskID); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	keepAddress = true
	s.reconcileReverseProxyJobLogged(ctx, taskID)
	_ = s.tasks.Complete(ctx, taskID, "Nomad server bootstrap requested")
}

func (s *JoinService) runBootstrapServerSteps(ctx context.Context, taskID string, srv server.Server, adapter linux.DistroAdapter) error {
	return s.runNomadCommandSteps(ctx, taskID, srv.Target(), []nomadCommandStep{
		{Stage: "installing_nomad", Message: "checking or installing Nomad", Command: adapter.NomadInstallScript(), Timeout: nomadInstallTimeout},
		{Stage: "preparing_runtime", Message: "checking Docker and Nomad CNI runtime", Command: adapter.NomadRuntimePrereqsScript(), Timeout: nomadInstallTimeout},
		{Stage: "configuring", Message: "writing Nomad server configuration", Command: s.bootstrapConfigScript(srv), Timeout: nomadMaintenanceTimeout},
		{Stage: "opening_firewall", Message: "opening local Nomad firewall ports", Command: nomadUFWAllowScript(traitBool(srv.Traits, TraitReverseProxyEnabled)), Timeout: nomadFirewallTimeout},
		{Stage: "starting", Message: "starting Nomad server service", Command: adapter.NomadServiceRestartScript(), Timeout: nomadServiceTimeout},
		{Stage: "verifying_local", Message: "checking local Nomad server API", Command: nomadLocalHealthScript(), Timeout: nomadLocalHealthTimeout},
	})
}

func (s *JoinService) runRebuildCluster(ctx context.Context, taskID string, bootstrap server.Server, adapter linux.DistroAdapter) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	managed, err := s.panelManagedServers(ctx, bootstrap)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	addressChange := s.beginNomadServerAddressChange(bootstrap)
	keepAddress := false
	defer func() {
		if !keepAddress {
			s.restoreNomadAddressAfterFailure(ctx, taskID, addressChange)
		}
	}()
	if err := s.runBootstrapServerSteps(ctx, taskID, bootstrap, adapter); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.waitForPanelNomadReachability(ctx, taskID); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	keepAddress = true
	if err := s.tasks.Advance(ctx, taskID, "resetting_nodes", "resetting existing Panel-managed Nomad nodes"); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	for _, srv := range managed {
		if srv.ID == bootstrap.ID {
			continue
		}
		_ = s.tasks.AppendLog(ctx, taskID, "system", "resetting Nomad on "+firstNonEmpty(srv.Name, srv.ID, srv.Host))
		nodeAdapter, _ := linux.AdapterFor(srv.OS)
		if err := s.execSudoLogged(ctx, taskID, srv.Target(), removeNodeScript(nodeAdapter), nomadMaintenanceTimeout); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	if err := s.tasks.Advance(ctx, taskID, "rejoining_nodes", "rejoining existing Panel-managed Nomad clients"); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	rpc := net.JoinHostPort(serverAdvertiseAddress(bootstrap), "4647")
	for _, srv := range managed {
		if srv.ID == bootstrap.ID {
			continue
		}
		adapter, err := s.ensureNomadEligible(srv)
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, fmt.Errorf("cannot rejoin Nomad client %s: %w", firstNonEmpty(srv.Name, srv.ID), err))
			return
		}
		if err := s.configureManagedClient(ctx, taskID, srv, adapter, rpc); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
	}
	if s.appRestorer != nil {
		if err := s.tasks.Advance(ctx, taskID, "restoring_applications", "restoring enabled applications"); err != nil {
			_ = s.tasks.Fail(ctx, taskID, err)
			return
		}
		restored, err := s.appRestorer.RedeployEnabledApplications(ctx)
		if err != nil {
			_ = s.tasks.Fail(ctx, taskID, fmt.Errorf("cannot restore enabled applications: %w", err))
			return
		}
		_ = s.tasks.AppendLog(ctx, taskID, "system", fmt.Sprintf("restored %d enabled applications", restored))
	}
	s.reconcileReverseProxyJobLogged(ctx, taskID)
	_ = s.tasks.Complete(ctx, taskID, "Nomad cluster rebuild requested")
}

func (s *JoinService) runSwitchServer(ctx context.Context, taskID string, srv server.Server, adapter linux.DistroAdapter, setter addressSetter) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	previous := s.currentConfig().Address
	next := nomadHTTPAddressForServer(srv)
	if err := s.runBootstrapServerSteps(ctx, taskID, srv, adapter); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "switching", "switching Nomad server address")
	s.setNomadAddress(setter, next)
	if err := s.waitForPanelNomadReachability(ctx, taskID); err != nil {
		if strings.TrimSpace(previous) != "" {
			s.setNomadAddress(setter, previous)
		}
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if err := s.syncManagedClientsToServer(ctx, taskID, srv); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	s.reconcileReverseProxyJobLogged(ctx, taskID)
	_ = s.tasks.Complete(ctx, taskID, "Nomad server switched")
}

func (s *JoinService) syncManagedClientsToServer(ctx context.Context, taskID string, control server.Server) error {
	clients, err := s.panelManagedClientServers(ctx, control.ID)
	if err != nil {
		return err
	}
	rpc := net.JoinHostPort(serverAdvertiseAddress(control), "4647")
	for _, srv := range clients {
		adapter, err := s.ensureNomadEligible(srv)
		if err != nil {
			return fmt.Errorf("cannot synchronize Nomad client %s: %w", firstNonEmpty(srv.Name, srv.ID), err)
		}
		if err := s.configureManagedClient(ctx, taskID, srv, adapter, rpc); err != nil {
			return err
		}
	}
	return nil
}

func (s *JoinService) configureManagedClient(ctx context.Context, taskID string, srv server.Server, adapter linux.DistroAdapter, rpc string) error {
	name := firstNonEmpty(srv.Name, srv.ID, srv.Host)
	_ = s.tasks.AppendLog(ctx, taskID, "system", "configuring Nomad client "+name+" for "+rpc)
	if err := s.runNomadCommandSteps(ctx, taskID, srv.Target(), []nomadCommandStep{
		{Stage: "configuring_clients", Message: "writing Nomad client configuration on " + name, Command: s.joinConfigScript(srv, rpc), Timeout: nomadMaintenanceTimeout},
		{Stage: "opening_firewall", Message: "opening Nomad firewall ports on " + name, Command: nomadUFWAllowScript(traitBool(srv.Traits, TraitReverseProxyEnabled)), Timeout: nomadFirewallTimeout},
		{Stage: "restarting_clients", Message: "restarting Nomad client on " + name, Command: adapter.NomadServiceRestartScript(), Timeout: nomadServiceTimeout},
		{Stage: "verifying_clients", Message: "checking Nomad client on " + name, Command: nomadLocalHealthScript(), Timeout: nomadLocalHealthTimeout},
	}); err != nil {
		return fmt.Errorf("cannot configure Nomad client %s: %w", name, err)
	}
	if err := s.waitForNomadClientRegistration(ctx, taskID, srv); err != nil {
		return fmt.Errorf("cannot configure Nomad client %s: %w", name, err)
	}
	return nil
}

func (s *JoinService) reconcileReverseProxyJobLogged(ctx context.Context, taskID string) {
	if err := s.reconcileReverseProxyJob(ctx); err != nil {
		_ = s.tasks.AppendLog(ctx, taskID, "stderr", "reverse proxy reconcile failed: "+err.Error())
	}
}

func (s *JoinService) runRemoveNode(ctx context.Context, taskID string, srv server.Server, nodeID string) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Start(ctx, taskID)
	if srv.ID != "" {
		if s.exec == nil {
			_ = s.tasks.Fail(ctx, taskID, panelerr.Validation("nomad_remove_executor_unavailable", "Nomad remove executor is unavailable"))
			return
		}
		_ = s.tasks.Advance(ctx, taskID, "stopping", "stopping Nomad on managed server")
		adapter, _ := linux.AdapterFor(srv.OS)
		if err := s.execSudoLogged(ctx, taskID, srv.Target(), removeNodeScript(adapter), nomadMaintenanceTimeout); err != nil {
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

type nomadCommandStep struct {
	Stage   string
	Message string
	Command string
	Timeout time.Duration
}

func (s *JoinService) runNomadCommandSteps(ctx context.Context, taskID string, target sshx.Target, steps []nomadCommandStep) error {
	for _, step := range steps {
		if strings.TrimSpace(step.Command) == "" {
			continue
		}
		timeout := step.Timeout
		if timeout == 0 {
			timeout = nomadMaintenanceTimeout
		}
		if err := s.tasks.Advance(ctx, taskID, step.Stage, step.Message); err != nil {
			return err
		}
		if err := s.execSudoLogged(ctx, taskID, target, shellScript(step.Command), timeout); err != nil {
			return err
		}
	}
	return nil
}

func (s *JoinService) panelManagedServers(ctx context.Context, bootstrap server.Server) ([]server.Server, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	serverByID := map[string]server.Server{}
	for _, srv := range servers {
		serverByID[srv.ID] = srv
	}
	ids := map[string]struct{}{}
	if bootstrap.ID != "" {
		ids[bootstrap.ID] = struct{}{}
	}
	latestTasks, err := s.latestNomadTasks(ctx)
	if err != nil {
		return nil, err
	}
	for serverID, task := range latestTasks {
		if serverID == "" || taskCompletedRemove(task) {
			continue
		}
		ids[serverID] = struct{}{}
	}
	nodesCtx, cancelNodes := context.WithTimeout(ctx, controlPlaneNomadQueryTimeout)
	defer cancelNodes()
	if nodes, err := s.nomad.Nodes(nodesCtx); err == nil {
		for _, node := range nodes {
			serverID := serverIDForNode(node)
			if serverID != "" {
				ids[serverID] = struct{}{}
			}
		}
	}
	out := make([]server.Server, 0, len(ids))
	for id := range ids {
		if srv, ok := serverByID[id]; ok {
			out = append(out, srv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID == bootstrap.ID {
			return true
		}
		if out[j].ID == bootstrap.ID {
			return false
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *JoinService) panelManagedClientServers(ctx context.Context, excludeServerID string) ([]server.Server, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return nil, err
	}
	serverByID := make(map[string]server.Server, len(servers))
	for _, srv := range servers {
		serverByID[srv.ID] = srv
	}

	clientIDs := map[string]struct{}{}
	serverIDs := map[string]struct{}{}
	latestTasks, err := s.latestNomadTasks(ctx)
	if err != nil {
		return nil, err
	}
	for serverID, task := range latestTasks {
		if serverID == "" || taskCompletedRemove(task) {
			continue
		}
		switch roleForTask(task) {
		case ProjectedNodeRoleClient:
			clientIDs[serverID] = struct{}{}
		case ProjectedNodeRoleServer:
			serverIDs[serverID] = struct{}{}
		}
	}

	nodesCtx, cancelNodes := context.WithTimeout(ctx, controlPlaneNomadQueryTimeout)
	defer cancelNodes()
	if nodes, err := s.nomad.Nodes(nodesCtx); err == nil {
		for _, node := range nodes {
			if serverID := serverIDForNode(node); serverID != "" {
				clientIDs[serverID] = struct{}{}
			}
		}
	}

	out := make([]server.Server, 0, len(clientIDs))
	for id := range clientIDs {
		if id == excludeServerID {
			continue
		}
		if _, isServer := serverIDs[id]; isServer {
			continue
		}
		if srv, ok := serverByID[id]; ok {
			out = append(out, srv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *JoinService) nomadRoleForServer(ctx context.Context, srv server.Server) string {
	latestTasks, err := s.latestNomadTasks(ctx)
	if err == nil {
		if role := roleForTask(latestTasks[srv.ID]); role == ProjectedNodeRoleServer || role == ProjectedNodeRoleClient {
			return role
		}
	}
	if nomadHTTPAddressMatchesServer(s.currentConfig().Address, srv) {
		return ProjectedNodeRoleServer
	}
	return ProjectedNodeRoleUnknown
}

func (s *JoinService) waitForPanelNomadReachability(ctx context.Context, taskID string) error {
	client, ok := s.nomad.(statusClient)
	if !ok {
		return nil
	}
	cfg := s.currentConfig()
	address := strings.TrimSpace(cfg.Address)
	if err := s.tasks.Advance(ctx, taskID, "verifying_panel", nomadPanelReachabilityRetryMessage); err != nil {
		return err
	}
	if address != "" {
		_ = s.tasks.AppendLog(ctx, taskID, "system", "Panel will connect to "+address+". If this check fails, open TCP 4646 from the Panel host to the Nomad server.")
	}
	checkCtx, cancel := context.WithTimeout(ctx, nomadPanelReachabilityTimeout)
	defer cancel()
	var lastErr error
	for {
		status, err := client.Status(checkCtx)
		if err == nil && status.Connected {
			_ = s.tasks.AppendLog(ctx, taskID, "system", "Panel can reach the Nomad API.")
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("Nomad status endpoint did not report connected")
		}
		select {
		case <-checkCtx.Done():
			if lastErr == nil {
				lastErr = checkCtx.Err()
			}
			if address == "" {
				return fmt.Errorf("Panel cannot reach the Nomad API from this host. Open TCP 4646 from the Panel host to the Nomad server and retry. Last error: %w", lastErr)
			}
			return fmt.Errorf("Panel cannot reach the Nomad API at %s from this host. Open TCP 4646 from the Panel host to the Nomad server and retry. Last error: %w", address, lastErr)
		case <-time.After(nomadPanelReachabilityRetryInterval):
		}
	}
}

func (s *JoinService) waitForNomadClientRegistration(ctx context.Context, taskID string, srv server.Server) error {
	if err := s.tasks.Advance(ctx, taskID, "verifying_cluster", "waiting for Nomad client registration"); err != nil {
		return err
	}
	deadline := time.Now().Add(nomadClientRegistrationTimeout)
	lastStatus := ""
	var lastErr error
	for {
		queryCtx, cancel := context.WithTimeout(ctx, controlPlaneNomadQueryTimeout)
		nodes, err := s.nomad.Nodes(queryCtx)
		cancel()
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
			lastStatus = ""
			for _, node := range nodes {
				if strings.TrimSpace(serverIDForNode(node)) != srv.ID {
					continue
				}
				lastStatus = strings.TrimSpace(node.Status)
				if strings.EqualFold(lastStatus, "ready") {
					_ = s.tasks.AppendLog(ctx, taskID, "system", "Nomad client registered as ready")
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			target := firstNonEmpty(srv.Name, srv.ID, srv.Host)
			switch {
			case lastErr != nil:
				return fmt.Errorf("Nomad client %s did not register before timeout; last Nomad API error: %w", target, lastErr)
			case lastStatus != "":
				return fmt.Errorf("Nomad client %s did not become ready before timeout; last node status: %s", target, lastStatus)
			default:
				return fmt.Errorf("Nomad client %s did not appear in the Nomad node list before timeout", target)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(nomadClientRegistrationRetryInterval):
		}
	}
}

func (s *JoinService) execSudoLogged(ctx context.Context, taskID string, target sshx.Target, command string, timeout time.Duration) error {
	_, err := remoteops.Runner{Exec: s.exec, Target: target, Log: nomadTaskLogSink{s.tasks, taskID}}.RunSudoLogged(ctx, command, timeout)
	return err
}

type nomadTaskLogSink struct {
	tasks  *tasks.Service
	taskID string
}

func (s nomadTaskLogSink) AppendLog(ctx context.Context, stream, line string) error {
	return s.tasks.AppendLog(ctx, s.taskID, stream, line)
}

func (s *JoinService) joinConfigScript(srv server.Server, rpc string) string {
	nodeName := safeNodeName("panel-" + srv.ID)
	cfg := s.currentConfig()
	datacenter := firstNonEmpty(strings.TrimSpace(cfg.Datacenter), "dc1")
	region := firstNonEmpty(strings.TrimSpace(cfg.Region), "global")
	advertiseAddress := serverAdvertiseAddress(srv)
	return fmt.Sprintf(`%s
%s
cat >/etc/nomad.d/panel-client.hcl <<'EOF'
name = "%s"
region = "%s"
datacenter = "%s"
data_dir = "/opt/nomad/data"
bind_addr = "0.0.0.0"

advertise {
  http = "%s"
  rpc = "%s"
  serf = "%s"
}

tls {
  http = true
  rpc = true
  rpc_upgrade_mode = true
  verify_https_client = true
  verify_server_hostname = false
  ca_file = "/etc/nomad.d/tls/ca.pem"
  cert_file = "/etc/nomad.d/tls/agent.pem"
  key_file = "/etc/nomad.d/tls/agent-key.pem"
}

server {
  enabled = false
}

client {
  enabled = true
  server_join {
    retry_join = ["%s"]
    retry_interval = "5s"
    retry_max = 0
  }
  meta {
    panel_server_id = "%s"
    panel_server_name = "%s"
    panel_ssh_host = "%s"
    panel_ssh_port = "%d"
    panel_ssh_username = "%s"
    panel_reverse_proxy_enabled = "%s"
    panel_reverse_proxy_static_files = "%s"
  }
}
EOF
`, resetNomadConfigScript(), s.nomadTLSWriteScript(), nodeName, shellEscapeHCL(region), shellEscapeHCL(datacenter), shellEscapeHCL(advertiseAddress), shellEscapeHCL(advertiseAddress), shellEscapeHCL(advertiseAddress), shellEscapeHCL(rpc), srv.ID, shellEscapeHCL(srv.Name), shellEscapeHCL(srv.Host), srv.Port, shellEscapeHCL(srv.SSHUsername), boolTrait(traitBool(srv.Traits, TraitReverseProxyEnabled)), boolTrait(traitBool(srv.Traits, TraitReverseProxyStaticFiles)))
}

func (s *JoinService) bootstrapConfigScript(srv server.Server) string {
	nodeName := safeNodeName("panel-" + srv.ID)
	cfg := s.currentConfig()
	datacenter := firstNonEmpty(strings.TrimSpace(cfg.Datacenter), "dc1")
	region := firstNonEmpty(strings.TrimSpace(cfg.Region), "global")
	advertiseAddress := serverAdvertiseAddress(srv)
	return fmt.Sprintf(`%s
%s
cat >/etc/nomad.d/panel-server.hcl <<'EOF'
name = "%s"
region = "%s"
datacenter = "%s"
data_dir = "/opt/nomad/data"
bind_addr = "0.0.0.0"

advertise {
  http = "%s"
  rpc = "%s"
  serf = "%s"
}

tls {
  http = true
  rpc = true
  rpc_upgrade_mode = true
  verify_https_client = true
  verify_server_hostname = false
  ca_file = "/etc/nomad.d/tls/ca.pem"
  cert_file = "/etc/nomad.d/tls/agent.pem"
  key_file = "/etc/nomad.d/tls/agent-key.pem"
}

server {
  enabled = true
  bootstrap_expect = 1
}

client {
  enabled = true
  meta {
    panel_server_id = "%s"
    panel_server_name = "%s"
    panel_ssh_host = "%s"
    panel_ssh_port = "%d"
    panel_ssh_username = "%s"
    panel_reverse_proxy_enabled = "%s"
    panel_reverse_proxy_static_files = "%s"
  }
}
EOF
`, resetNomadConfigScript(), s.nomadTLSWriteScript(), nodeName, shellEscapeHCL(region), shellEscapeHCL(datacenter), shellEscapeHCL(advertiseAddress), shellEscapeHCL(advertiseAddress), shellEscapeHCL(advertiseAddress), srv.ID, shellEscapeHCL(srv.Name), shellEscapeHCL(srv.Host), srv.Port, shellEscapeHCL(srv.SSHUsername), boolTrait(traitBool(srv.Traits, TraitReverseProxyEnabled)), boolTrait(traitBool(srv.Traits, TraitReverseProxyStaticFiles)))
}

func (s *JoinService) serverJoinRPCAddress(ctx context.Context) (string, error) {
	latest := s.latestCompletedBootstrapTask(ctx)
	if latest.ServerID != "" {
		if srv, err := s.servers.Get(ctx, latest.ServerID); err == nil && serverAdvertiseAddress(srv) != "" {
			return net.JoinHostPort(serverAdvertiseAddress(srv), "4647"), nil
		}
		return "", panelerr.Validation("nomad_advertise_address_migration_required", "Rebuild the Nomad cluster with an explicit network address before joining clients")
	}
	configured := nomadRPCAddress(s.currentConfig().Address)
	if !isLocalRPCAddress(configured) {
		return configured, nil
	}
	return "", panelerr.Validation("nomad_advertise_address_migration_required", "Rebuild the Nomad cluster with an explicit network address before joining clients")
}

func (s *JoinService) latestCompletedBootstrapTask(ctx context.Context) tasks.Task {
	var latest tasks.Task
	for _, taskType := range []string{TaskTypeServerBootstrap, TaskTypeClusterRebuild, TaskTypeServerSwitch} {
		result, err := s.tasks.List(ctx, tasks.ListFilter{Type: taskType, Limit: 200})
		if err != nil {
			continue
		}
		for _, task := range result.Items {
			if task.Status != tasks.StatusCompleted {
				continue
			}
			if latest.ID == "" || task.CreatedAt.After(latest.CreatedAt) {
				latest = task
			}
		}
	}
	return latest
}

func resetNomadConfigScript() string {
	return `echo "[panel] resetting Panel-managed Nomad configuration"
rm -rf /etc/nomad.d/tls
install -d -m 0755 /etc/nomad.d /etc/nomad.d/tls /opt/nomad/data
find /etc/nomad.d -maxdepth 1 -type f \( -name '*.hcl' -o -name '*.json' -o -name '*.pem' \) -delete`
}

func nomadUFWAllowScript(reverseProxy bool) string {
	// Nomad uses HTTP for the API, RPC for client/server traffic, and Serf
	// gossip over both TCP and UDP. Keep all four rules together so repair
	// paths cannot leave an agent locally healthy but isolated from its peers.
	rules := []remoteops.UFWRule{
		{Port: 4646, Protocol: "tcp"},
		{Port: 4647, Protocol: "tcp"},
		{Port: 4648, Protocol: "tcp"},
		{Port: 4648, Protocol: "udp"},
	}
	if reverseProxy {
		for _, port := range reverseProxyTCPPorts {
			rules = append(rules, remoteops.UFWRule{Port: port, Protocol: "tcp"})
		}
	}
	return remoteops.MustUFWAllowScript(rules...)
}

func reverseProxyUFWAllowScript() string {
	rules := make([]remoteops.UFWRule, 0, len(reverseProxyTCPPorts))
	for _, port := range reverseProxyTCPPorts {
		rules = append(rules, remoteops.UFWRule{Port: port, Protocol: "tcp"})
	}
	return remoteops.MustUFWAllowScript(rules...)
}

func nomadLocalHealthScript() string {
	return `export NOMAD_ADDR="https://127.0.0.1:4646"
export NOMAD_CACERT="/etc/nomad.d/tls/ca.pem"
export NOMAD_CLIENT_CERT="/etc/nomad.d/tls/agent.pem"
export NOMAD_CLIENT_KEY="/etc/nomad.d/tls/agent-key.pem"
echo "[panel] waiting for Nomad HTTP API on 127.0.0.1:4646"
attempts=0
while ! timeout 3s nomad agent-info >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  if [ "$attempts" -ge 20 ]; then
    echo "[panel] Nomad HTTP API did not become ready on 127.0.0.1:4646" >&2
    systemctl status nomad --no-pager -l >&2 || true
    journalctl -u nomad -n 80 --no-pager >&2 || true
    exit 1
  fi
  sleep 2
done
echo "[panel] Nomad HTTP API is responding locally"`
}

func nomadClusterDiagnosticsScript() string {
	return `set +e
echo "[panel] Nomad client did not register; collecting diagnostics"
export NOMAD_ADDR="https://127.0.0.1:4646"
export NOMAD_CACERT="/etc/nomad.d/tls/ca.pem"
export NOMAD_CLIENT_CERT="/etc/nomad.d/tls/agent.pem"
export NOMAD_CLIENT_KEY="/etc/nomad.d/tls/agent-key.pem"
timeout 10s nomad agent-info
systemctl status nomad --no-pager -l
journalctl -u nomad -n 120 --no-pager
exit 0`
}

func shellScript(parts ...string) string {
	out := []string{"set -eu"}
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, "\n") + "\n"
}

func removeNodeScript(adapter linux.DistroAdapter) string {
	stopScript := genericNomadServiceStopScript()
	if adapter != nil {
		stopScript = adapter.NomadServiceStopScript()
	}
	return `set -eu
` + stopScript + "\n" + resetNomadConfigScript()
}

func genericNomadServiceStopScript() string {
	return `systemctl disable --now nomad || true
systemctl reset-failed nomad || true`
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
	var certs []ReverseProxyCertificate
	if s.certSource != nil {
		certs, err = s.certSource.ReverseProxyCertificates(ctx)
		if err != nil {
			return err
		}
	}
	job, enabled := s.renderReverseProxyJob(servers, appConfigs, certs)
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

var reverseProxyTCPPorts = []int{80, 443}

func (s *JoinService) renderReverseProxyJob(servers []server.Server, appConfigs []ApplicationReverseProxyConfig, certs []ReverseProxyCertificate) (Job, bool) {
	cfg := s.currentConfig()
	datacenter := firstNonEmpty(strings.TrimSpace(cfg.Datacenter), "dc1")
	certIndex := newReverseProxyCertificateIndex(certs)
	job := Job{
		ID:          reverseProxyJobID,
		Name:        "panel-nginx",
		Type:        "service",
		Namespace:   firstNonEmpty(strings.TrimSpace(cfg.Namespace), "default"),
		Region:      firstNonEmpty(strings.TrimSpace(cfg.Region), "global"),
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
		tlsResolver := newReverseProxyTLSResolver(certIndex)
		appSnippets := reverseProxyAppSnippetsForServer(srv.ID, appConfigs, tlsResolver)
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
				EmbeddedTmpl: renderNginxStaticConfig(staticSites, tlsResolver),
				DestPath:     "local/nginx.conf.d/panel-static.conf",
				Perms:        "0644",
				ChangeMode:   "restart",
			})
		}
		templates = append(templates, tlsResolver.Templates()...)
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

func reverseProxyAppSnippetsForServer(serverID string, apps []ApplicationReverseProxyConfig, tlsResolver *reverseProxyTLSResolver) []nginxSnippet {
	out := []nginxSnippet{}
	for _, app := range apps {
		if len(app.Routes) == 0 || !applicationTargetsServer(app, serverID) {
			continue
		}
		out = append(out, nginxSnippet{
			Name:    reverseProxyAppConfigName(app),
			Content: renderNginxProxyConfig(app.ApplicationName, app.Routes, tlsResolver),
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

func renderNginxProxyConfig(appName string, routes []ReverseProxyRoute, tlsResolver *reverseProxyTLSResolver) string {
	var b strings.Builder
	b.WriteString("# Managed by Panel. Application: ")
	b.WriteString(appName)
	b.WriteString("\n")
	for _, route := range routes {
		if tls, ok := tlsResolver.Match(route.Domain); ok {
			writeNginxHTTPSRedirectServer(&b, route.Domain)
			b.WriteString("\nserver {\n")
			b.WriteString("    listen 443 ssl;\n")
			b.WriteString("    server_name ")
			b.WriteString(route.Domain)
			b.WriteString(";\n")
			writeNginxTLSDirectives(&b, tls)
			writeNginxProxyLocations(&b, route)
			b.WriteString("}\n")
			continue
		}
		b.WriteString("\nserver {\n")
		b.WriteString("    listen 80;\n")
		b.WriteString("    server_name ")
		b.WriteString(route.Domain)
		b.WriteString(";\n")
		writeNginxProxyLocations(&b, route)
		b.WriteString("}\n")
	}
	return b.String()
}

func renderNginxStaticConfig(staticSites []ReverseProxyStaticSite, tlsResolver *reverseProxyTLSResolver) string {
	var b strings.Builder
	b.WriteString("# Managed by Panel. Node static sites.\n")
	for index, site := range staticSites {
		root := "/panel-static/static-" + strconv.Itoa(index)
		if tls, ok := tlsResolver.Match(site.Domain); ok {
			writeNginxHTTPSRedirectServer(&b, site.Domain)
			writeNginxStaticServer(&b, site, root, "443 ssl", tls)
			continue
		}
		writeNginxStaticServer(&b, site, root, "80", nil)
	}
	return b.String()
}

func writeNginxProxyLocations(b *strings.Builder, route ReverseProxyRoute) {
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
}

func writeNginxHTTPSRedirectServer(b *strings.Builder, domain string) {
	b.WriteString("\nserver {\n")
	b.WriteString("    listen 80;\n")
	b.WriteString("    server_name ")
	b.WriteString(domain)
	b.WriteString(";\n")
	b.WriteString("    return 301 https://$host$request_uri;\n")
	b.WriteString("}\n")
}

func writeNginxTLSDirectives(b *strings.Builder, tls *reverseProxyTLSConfig) {
	b.WriteString("\n")
	b.WriteString("    ssl_certificate ")
	b.WriteString(tls.CertificatePath)
	b.WriteString(";\n")
	b.WriteString("    ssl_certificate_key ")
	b.WriteString(tls.PrivateKeyPath)
	b.WriteString(";\n")
	b.WriteString("    ssl_protocols TLSv1.2 TLSv1.3;\n")
}

func writeNginxStaticServer(b *strings.Builder, site ReverseProxyStaticSite, root string, listen string, tls *reverseProxyTLSConfig) {
	b.WriteString("\nserver {\n")
	b.WriteString("    listen ")
	b.WriteString(listen)
	b.WriteString(";\n")
	b.WriteString("    server_name ")
	b.WriteString(site.Domain)
	b.WriteString(";\n")
	if tls != nil {
		writeNginxTLSDirectives(b, tls)
	}
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

type reverseProxyTLSConfig struct {
	CertificatePath string
	PrivateKeyPath  string
}

type reverseProxyCertificateRef struct {
	Cert     ReverseProxyCertificate
	FileBase string
	Domains  []string
}

type reverseProxyCertificateIndex struct {
	refs []reverseProxyCertificateRef
}

func newReverseProxyCertificateIndex(certs []ReverseProxyCertificate) *reverseProxyCertificateIndex {
	refs := make([]reverseProxyCertificateRef, 0, len(certs))
	usedNames := map[string]int{}
	for index, cert := range certs {
		fileBase := safeNodeName(cert.ID)
		if fileBase == "panel-node" {
			fileBase = "cert-" + strconv.Itoa(index+1)
		}
		if seen := usedNames[fileBase]; seen > 0 {
			usedNames[fileBase] = seen + 1
			fileBase += "-" + strconv.Itoa(seen+1)
		} else {
			usedNames[fileBase] = 1
		}
		domains := make([]string, 0, len(cert.Domains))
		for _, domain := range cert.Domains {
			if normalized := normalizeCertificateDomain(domain); normalized != "" {
				domains = append(domains, normalized)
			}
		}
		if len(domains) == 0 || strings.TrimSpace(cert.CertificatePEM) == "" || strings.TrimSpace(cert.PrivateKeyPEM) == "" {
			continue
		}
		refs = append(refs, reverseProxyCertificateRef{Cert: cert, FileBase: fileBase, Domains: domains})
	}
	return &reverseProxyCertificateIndex{refs: refs}
}

func (idx *reverseProxyCertificateIndex) Match(domain string) (reverseProxyCertificateRef, bool) {
	if idx == nil {
		return reverseProxyCertificateRef{}, false
	}
	normalized := normalizeCertificateDomain(domain)
	if normalized == "" {
		return reverseProxyCertificateRef{}, false
	}
	for _, ref := range idx.refs {
		for _, certDomain := range ref.Domains {
			if certDomain == normalized {
				return ref, true
			}
		}
	}
	for _, ref := range idx.refs {
		for _, certDomain := range ref.Domains {
			if wildcardCertificateDomainMatches(certDomain, normalized) {
				return ref, true
			}
		}
	}
	return reverseProxyCertificateRef{}, false
}

type reverseProxyTLSResolver struct {
	index *reverseProxyCertificateIndex
	used  map[string]reverseProxyCertificateRef
}

func newReverseProxyTLSResolver(index *reverseProxyCertificateIndex) *reverseProxyTLSResolver {
	return &reverseProxyTLSResolver{index: index, used: map[string]reverseProxyCertificateRef{}}
}

func (r *reverseProxyTLSResolver) Match(domain string) (*reverseProxyTLSConfig, bool) {
	if r == nil || r.index == nil {
		return nil, false
	}
	ref, ok := r.index.Match(domain)
	if !ok {
		return nil, false
	}
	r.used[ref.FileBase] = ref
	return &reverseProxyTLSConfig{
		CertificatePath: "/local/certs/" + ref.FileBase + ".pem",
		PrivateKeyPath:  "/local/certs/" + ref.FileBase + "-key.pem",
	}, true
}

func (r *reverseProxyTLSResolver) Templates() []Template {
	if r == nil || len(r.used) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.used))
	for name := range r.used {
		names = append(names, name)
	}
	sort.Strings(names)
	templates := make([]Template, 0, len(names)*2)
	for _, name := range names {
		ref := r.used[name]
		templates = append(templates, Template{
			EmbeddedTmpl: strings.TrimSpace(ref.Cert.CertificatePEM) + "\n",
			DestPath:     "local/certs/" + name + ".pem",
			Perms:        "0644",
			ChangeMode:   "restart",
		}, Template{
			EmbeddedTmpl: strings.TrimSpace(ref.Cert.PrivateKeyPEM) + "\n",
			DestPath:     "local/certs/" + name + "-key.pem",
			Perms:        "0600",
			ChangeMode:   "restart",
		})
	}
	return templates
}

func normalizeCertificateDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if strings.HasPrefix(domain, "*.") {
		base := strings.TrimPrefix(domain, "*.")
		if base == "" || strings.Contains(base, "*") {
			return ""
		}
		return "*." + base
	}
	if strings.Contains(domain, "*") {
		return ""
	}
	return domain
}

func wildcardCertificateDomainMatches(pattern string, domain string) bool {
	if !strings.HasPrefix(pattern, "*.") {
		return false
	}
	base := strings.TrimPrefix(pattern, "*.")
	if !strings.HasSuffix(domain, "."+base) {
		return false
	}
	prefix := strings.TrimSuffix(domain, "."+base)
	return prefix != "" && !strings.Contains(prefix, ".")
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

func normalizeProjectedNodeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case ProjectedNodeRoleServer:
		return ProjectedNodeRoleServer
	case ProjectedNodeRoleClient:
		return ProjectedNodeRoleClient
	case ProjectedNodeRoleUnknown:
		return ProjectedNodeRoleUnknown
	default:
		return ""
	}
}

func nomadHTTPAddressMatchesServer(address string, srv server.Server) bool {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Host == "" {
		return false
	}
	return sameHost(parsed.Hostname(), srv.Host) || sameHost(parsed.Hostname(), serverAdvertiseAddress(srv))
}

func nomadHTTPAddressForServer(srv server.Server) string {
	return "https://" + net.JoinHostPort(serverAdvertiseAddress(srv), "4646")
}

func sameHost(left, right string) bool {
	left = strings.Trim(strings.ToLower(strings.TrimSpace(left)), "[]")
	right = strings.Trim(strings.ToLower(strings.TrimSpace(right)), "[]")
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	leftIP := net.ParseIP(left)
	rightIP := net.ParseIP(right)
	return leftIP != nil && rightIP != nil && leftIP.Equal(rightIP)
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

func (s *JoinService) nomadTLSWriteScript() string {
	if s.tlsAssets == nil {
		return ""
	}
	return fmt.Sprintf(`cat >/etc/nomad.d/tls/ca.pem <<'EOF'
%sEOF
cat >/etc/nomad.d/tls/agent.pem <<'EOF'
%sEOF
cat >/etc/nomad.d/tls/agent-key.pem <<'EOF'
%sEOF
chmod 0600 /etc/nomad.d/tls/ca.pem /etc/nomad.d/tls/agent.pem /etc/nomad.d/tls/agent-key.pem
`, string(s.tlsAssets.CAPEM), string(s.tlsAssets.AgentCertPEM), string(s.tlsAssets.AgentKeyPEM))
}
