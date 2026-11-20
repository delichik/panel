package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
	server "panel/internal/modules/servers"
	controlplane "panel/internal/orchestrator"
	"panel/internal/platform/database/orm"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// serviceRuntimeReconciler is deliberately owned by the orchestrator
// controller. The application service only supplies the server endpoint and
// the existing agent transport; it is never called directly by planners.
type serviceRuntimeReconciler struct {
	service *Service
}

// AgentRuntimeReconcileClient is implemented by the current agent transport.
// It is kept separate from AgentRuntimeClient so existing service fakes and
// non-orchestrator callers can continue to use the primitive APIs.
type AgentRuntimeReconcileClient interface {
	RuntimeReconcile(ctx context.Context, baseURL string, req agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error)
}

type firewallControlChannelChecker interface {
	VerifyFirewallControlChannels(context.Context, string) error
}

type agentRuntimeHealthClient interface {
	Health(ctx context.Context, baseURL string) (agentcontract.HealthResponse, error)
}

func (r *serviceRuntimeReconciler) Reconcile(ctx context.Context, req controlplane.ReconcileRequestRPC) (controlplane.ReconcileResponse, error) {
	if r == nil || r.service == nil || r.service.runtimeClient == nil {
		return controlplane.ReconcileResponse{ErrorCode: "runtime_unavailable", ErrorClass: "agent_unavailable", ErrorMessage: "agent runtime client is unavailable", Retryable: true}, nil
	}
	if r.service.servers == nil {
		return controlplane.ReconcileResponse{ErrorCode: "server_provider_unavailable", ErrorClass: "configuration", ErrorMessage: "server provider is unavailable", Retryable: false}, nil
	}
	serverRecord, err := r.service.servers.Get(ctx, req.ServerID)
	if err != nil {
		return failureResponse("server_unavailable", "agent_unavailable", true, nil, err)
	}
	if err := ensureAgentRuntimeReady(serverRecord); err != nil {
		return failureResponse("agent_not_ready", "agent_unavailable", true, nil, err)
	}
	endpoint, ok := agentURLFromServer(serverRecord)
	if !ok || strings.TrimSpace(endpoint) == "" {
		return controlplane.ReconcileResponse{ErrorCode: "agent_required", ErrorClass: "agent_unavailable", ErrorMessage: "agent endpoint is not configured", Retryable: true}, nil
	}
	// The server trait is the last successful health state and is used as the
	// cheap admission check above. A production transport can also refresh the
	// capability set here, so an older agent without the single reconcile RPC
	// is retried instead of being sent a partially overlapping primitive plan.
	if healthClient, ok := r.service.runtimeClient.(agentRuntimeHealthClient); ok {
		health, err := healthClient.Health(ctx, endpoint)
		if err != nil {
			return failureResponse("agent_health_failed", "agent_unavailable", true, nil, err)
		}
		if !containsAgentCapability(health.Capabilities, "runtime-reconcile") || !containsAgentCapability(health.Capabilities, agentcontract.CapabilityExecutionEvents) {
			return controlplane.ReconcileResponse{ErrorCode: "agent_capability_missing", ErrorClass: "agent_unavailable", ErrorMessage: "agent does not support runtime-reconcile", Retryable: true}, nil
		}
	}

	client, ok := r.service.runtimeClient.(AgentRuntimeReconcileClient)
	if !ok {
		// ORCH-CTRL-007: without AgentRuntimeReconcileClient there is no 10s
		// stable verify path. Refuse every action (including stop/purge)
		// instead of an asymmetric primitive degrade that could still mutate Docker.
		return controlplane.ReconcileResponse{
			ErrorCode: "agent_capability_missing", ErrorClass: "agent_unavailable",
			ErrorMessage: "runtime reconcile client is required for runtime actions", Retryable: true,
		}, nil
	}

	// Bound the synchronous RPC wait; longer Agent work (image pull / verify)
	// continues under monitor mode after transport deadline via step reports.
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	spec, err := runtimeSpecForRequest(req)
	if err != nil {
		return failureResponse("invalid_spec", "invalid_spec", false, nil, err)
	}
	sshPort := normalizedRuntimeSSHPort(serverRecord.Port)
	agentPort := normalizedRuntimeAgentPort(endpoint)
	if code, err := validateRuntimeFirewallPorts(spec, sshPort, agentPort); err != nil {
		return controlplane.ReconcileResponse{ErrorCode: code, ErrorClass: "configuration", ErrorMessage: err.Error(), Retryable: false}, nil
	}
	checker, checksChannels := r.service.servers.(firewallControlChannelChecker)
	if checksChannels {
		if err := checker.VerifyFirewallControlChannels(runCtx, req.ServerID); err != nil {
			return firewallChannelFailure("firewall_channel_preflight_failed", nil, err), nil
		}
	}
	// Share the per-server Docker write queue with ordinary container/image/volume
	// mutations so facility/app reconcile cannot race UI-driven Docker ops.
	response, callErr := r.withServerQueue(runCtx, req.ServerID, func(queuedCtx context.Context) (controlplane.ReconcileResponse, error) {
		if reloadResp, ok, reloadErr := r.tryFacilityConfigReload(queuedCtx, endpoint, req, spec); ok {
			return reloadResp, reloadErr
		}
		reconcileReq := agentcontract.RuntimeReconcileRequest{
			OperationID: req.OperationID, RunID: req.RunID,
			JobID: req.JobID, ExecutionID: req.ExecutionID, ApplicationID: req.ApplicationID, InstanceID: req.InstanceID,
			ServerID: req.ServerID, Action: req.Action, DesiredGeneration: req.DesiredGeneration, DesiredSpecHash: req.DesiredSpecHash,
			DesiredRevisionID: req.DesiredRevisionID, Spec: spec, RemoveData: req.RemoveData, PreviousContainerName: req.PreviousContainerName,
			SSHPort: sshPort, AgentPort: agentPort,
		}
		// 入口代理重建前用新镜像做 nginx -t 预检并回传 nginx -v；只在该设施的
		// apply 路径设置，其他应用保持原行为。旧 Agent 会忽略新字段（回传空版本）。
		if req.ApplicationID == FacilityProxyApplicationID && req.Action == controlplane.ActionApply {
			reconcileReq.ImagePreflightCommand = []string{"nginx", "-t", "-c", "/etc/panel-nginx/nginx.conf"}
			reconcileReq.ImageVersionCommand = []string{"nginx", "-v"}
		}
		return r.callTracked(queuedCtx, endpoint, req, client, reconcileReq)
	})
	var postflightErr error
	if checksChannels {
		postflightErr = checker.VerifyFirewallControlChannels(context.WithoutCancel(ctx), req.ServerID)
	}
	if callErr != nil || response.ErrorCode != "" {
		return response, callErr
	}
	if postflightErr != nil {
		// Do not fail the reconcile: agent keeps a rollback timer for firewall
		// mutations, and a postflight error must not block unrelated runtime work.
		// Skipping Confirm leaves the timer armed so the node can recover reachability.
		logging.From(ctx).Warn("firewall postflight failed after runtime reconcile; agent will roll back pending rules",
			zap.String("serverId", req.ServerID),
			zap.String("applicationId", req.ApplicationID),
			zap.Error(postflightErr),
		)
		return response, nil
	}
	if confirmer, ok := r.service.runtimeClient.(firewallPendingConfirmer); ok {
		if err := confirmer.UFWConfirmPending(context.WithoutCancel(ctx), endpoint); err != nil {
			logging.From(ctx).Warn("firewall pending confirm failed after runtime reconcile",
				zap.String("serverId", req.ServerID),
				zap.String("applicationId", req.ApplicationID),
				zap.Error(err),
			)
		}
	}
	return response, nil
}

type firewallPendingConfirmer interface {
	UFWConfirmPending(ctx context.Context, baseURL string) error
}

type agentRuntimeReloadClient interface {
	RuntimeReload(ctx context.Context, baseURL string, req agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error)
}

// tryFacilityConfigReload applies facility reverse-proxy config via RuntimeReload
// (write files + nginx -t + reload) without recreating the container. Returns
// ok=false to fall through to RuntimeReconcile for proxy-plane / first install.
func (r *serviceRuntimeReconciler) tryFacilityConfigReload(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC, spec appruntime.Spec) (controlplane.ReconcileResponse, bool, error) {
	if r == nil || r.service == nil || r.service.facilityRuntime == nil {
		return controlplane.ReconcileResponse{}, false, nil
	}
	if req.ApplicationID != "facility-reverse-proxy" {
		return controlplane.ReconcileResponse{}, false, nil
	}
	if req.Action != controlplane.ActionApply {
		return controlplane.ReconcileResponse{}, false, nil
	}
	// FAC-RP-009：把该实例上一次成功应用的 runtime spec 作为 current 传入，
	// 设施才能按身份字段（镜像/命令/端口/mount 结构/restart）判定 recreate，
	// 只按配置内容变化判定 reload。缺失 current 的语义是首装 → recreate。
	current := r.storedRuntimeSpec(ctx, req)
	plan := r.service.facilityRuntime.PlanRuntimeUpdate(ctx, Application{ID: req.ApplicationID}, server.Server{ID: req.ServerID}, current, spec)
	if plan.Mode != appruntime.UpdateModeReload || plan.Strategy == nil {
		return controlplane.ReconcileResponse{}, false, nil
	}
	reloadClient, ok := r.service.runtimeClient.(agentRuntimeReloadClient)
	if !ok {
		return controlplane.ReconcileResponse{}, false, nil
	}
	result, err := reloadClient.RuntimeReload(ctx, endpoint, agentcontract.RuntimeReloadRequest{
		Spec:            spec,
		ContainerName:   firstNonEmpty(spec.ContainerName, req.PreviousContainerName),
		ValidateCommand: plan.Strategy.ValidateCommand,
		ReloadCommand:   plan.Strategy.ReloadCommand,
	})
	if err != nil {
		resp, _ := failureResponse("facility_config_reload_failed", "runtime", true, nil, err)
		return resp, true, nil
	}
	if result.Phase == "unsupported" {
		// No running worker / archive / legacy workspace → proxy-plane reconcile.
		return controlplane.ReconcileResponse{}, false, nil
	}
	if strings.TrimSpace(result.Error) != "" || !result.Reloaded {
		// Do not recreate on validate/reload failure; surface error and keep worker.
		msg := firstNonEmpty(result.Error, result.Output, "facility config reload failed")
		return controlplane.ReconcileResponse{
			ErrorCode: "facility_config_reload_failed", ErrorClass: "runtime",
			ErrorMessage: msg, Retryable: true,
		}, true, nil
	}
	return controlplane.ReconcileResponse{ObservedState: "running"}, true, nil
}

// storedRuntimeSpec 读取 application_instances.runtime_spec_json 中该实例最近
// 一次成功应用的 runtime spec。它只用于设施反向代理的 reload/recreate 身份比较：
// 读失败或空 spec 时返回零值（设施按“无 current = 首装”规划 recreate），绝不阻塞
// 协调本身。
func (r *serviceRuntimeReconciler) storedRuntimeSpec(ctx context.Context, req controlplane.ReconcileRequestRPC) appruntime.Spec {
	if r == nil || r.service == nil || r.service.db == nil {
		return appruntime.Spec{}
	}
	query := orm.New(r.service.db).From("application_instances").Select("runtime_spec_json")
	if instanceID := strings.TrimSpace(req.InstanceID); instanceID != "" {
		query = query.Where("id=?", instanceID)
	} else {
		query = query.Where("application_id=?", req.ApplicationID).And("server_id=?", req.ServerID)
	}
	var raw string
	if err := query.ScanValue(ctx, &raw); err != nil {
		if err != sql.ErrNoRows {
			logging.From(ctx).Warn("read stored runtime spec failed; facility plan will treat it as first install",
				zap.String("serverId", req.ServerID),
				zap.String("applicationId", req.ApplicationID),
				zap.String("instanceId", req.InstanceID),
				zap.Error(err),
			)
		}
		return appruntime.Spec{}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return appruntime.Spec{}
	}
	var spec appruntime.Spec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		logging.From(ctx).Warn("stored runtime spec is not valid JSON; facility plan will treat it as first install",
			zap.String("serverId", req.ServerID),
			zap.String("applicationId", req.ApplicationID),
			zap.String("instanceId", req.InstanceID),
			zap.Error(err),
		)
		return appruntime.Spec{}
	}
	return spec
}

// withServerQueue serializes Docker-mutating reconcile work through the shared
// per-server queue when one is injected. Tests and degraded wiring without a
// queue keep the direct path.
func (r *serviceRuntimeReconciler) withServerQueue(ctx context.Context, serverID string, run func(context.Context) (controlplane.ReconcileResponse, error)) (controlplane.ReconcileResponse, error) {
	if r == nil || r.service == nil || r.service.operationQueue == nil {
		return run(ctx)
	}
	var (
		resp   controlplane.ReconcileResponse
		runErr error
	)
	if err := r.service.operationQueue.Execute(ctx, serverID, func(runCtx context.Context) error {
		resp, runErr = run(runCtx)
		return runErr
	}); err != nil && runErr == nil {
		return resp, err
	}
	return resp, runErr
}

func validateRuntimeFirewallPorts(spec appruntime.Spec, sshPort, agentPort int) (string, error) {
	for _, port := range spec.Ports {
		if port.HostPort <= 0 {
			continue
		}
		if port.HostPort == sshPort {
			return "application_ssh_port_reserved", fmt.Errorf("host port %d is reserved for server SSH access", sshPort)
		}
		if port.HostPort == agentPort {
			return "application_agent_port_reserved", fmt.Errorf("host port %d is reserved for Panel Agent access", agentPort)
		}
	}
	return "", nil
}

func firewallChannelFailure(code string, steps []controlplane.Step, err error) controlplane.ReconcileResponse {
	message := "firewall control channel verification failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return controlplane.ReconcileResponse{
		ErrorCode: code, ErrorClass: "agent_unavailable", ErrorMessage: message, Retryable: true,
		Steps: append([]controlplane.Step(nil), steps...),
	}
}

func normalizedRuntimeAgentPort(endpoint string) int {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Port() == "" {
		return 9786
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return 9786
	}
	return port
}

func normalizedRuntimeSSHPort(port int) int {
	if port <= 0 || port > 65535 {
		return 22
	}
	return port
}

func runtimeSpecForRequest(req controlplane.ReconcileRequestRPC) (appruntime.Spec, error) {
	var spec appruntime.Spec
	if req.Action != controlplane.ActionApply {
		return spec, nil
	}
	if err := json.Unmarshal(req.RenderedRuntimeSpec, &spec); err != nil {
		return appruntime.Spec{}, err
	}
	if strings.TrimSpace(spec.ApplicationID) == "" {
		spec.ApplicationID = req.ApplicationID
	}
	if strings.TrimSpace(spec.InstanceID) == "" {
		spec.InstanceID = req.InstanceID
	}
	if strings.TrimSpace(spec.ContainerName) == "" {
		spec.ContainerName = runtimeContainerNameByInstance(req.InstanceID)
	}
	if spec.Generation == 0 {
		spec.Generation = req.DesiredGeneration
	}
	if strings.TrimSpace(spec.SpecHash) == "" {
		spec.SpecHash = req.DesiredSpecHash
	}
	return spec, nil
}

func reconcileResponseFromAgent(in agentcontract.RuntimeReconcileResponse) controlplane.ReconcileResponse {
	steps := make([]controlplane.Step, 0, len(in.Steps))
	for _, step := range in.Steps {
		steps = append(steps, controlplane.Step{Name: step.Name, Status: step.Status, Detail: step.Detail})
	}
	return controlplane.ReconcileResponse{
		ObservedState:       in.ObservedState,
		ContainerName:       in.ContainerName,
		ContainerID:         in.ContainerID,
		ObservedGeneration:  in.ObservedGeneration,
		ObservedSpecHash:    in.ObservedSpecHash,
		ObservedImageDigest: in.ObservedImageDigest,
		ObservedAt:          in.ObservedAt,
		Steps:               steps,
		ErrorCode:           in.ErrorCode,
		ErrorClass:          in.ErrorClass,
		ErrorMessage:        in.ErrorMessage,
		ErrorDetail:         in.ErrorDetail,
		Retryable:           in.Retryable,
		RetryAfter:          in.RetryAfter,
	}
}

func failureResponse(code, class string, retryable bool, steps []controlplane.Step, err error) (controlplane.ReconcileResponse, error) {
	message := "runtime reconcile failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return controlplane.ReconcileResponse{ErrorCode: code, ErrorClass: class, ErrorMessage: message, Retryable: retryable, Steps: steps}, err
}

func runtimeContainerNameByInstance(instanceID string) string {
	return "panel-app-" + strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(instanceID)
}

func containsAgentCapability(capabilities []string, wanted string) bool {
	for _, capability := range capabilities {
		if strings.TrimSpace(capability) == wanted {
			return true
		}
	}
	return false
}
