package applications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
	controlplane "panel/internal/orchestrator"
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
		if !containsAgentCapability(health.Capabilities, "runtime-reconcile") {
			return controlplane.ReconcileResponse{ErrorCode: "agent_capability_missing", ErrorClass: "agent_unavailable", ErrorMessage: "agent does not support runtime-reconcile", Retryable: true}, nil
		}
	}

	// A runtime call must finish before the default three-minute lease expires.
	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	if client, ok := r.service.runtimeClient.(AgentRuntimeReconcileClient); ok {
		spec, err := runtimeSpecForRequest(req)
		if err != nil {
			return failureResponse("invalid_spec", "invalid_spec", false, nil, err)
		}
		response, err := client.RuntimeReconcile(runCtx, endpoint, agentcontract.RuntimeReconcileRequest{
			JobID: req.JobID, ExecutionID: req.ExecutionID, ApplicationID: req.ApplicationID, InstanceID: req.InstanceID,
			ServerID: req.ServerID, Action: req.Action, DesiredGeneration: req.DesiredGeneration, DesiredSpecHash: req.DesiredSpecHash,
			DesiredRevisionID: req.DesiredRevisionID, Spec: spec, RemoveData: req.RemoveData, PreviousContainerName: req.PreviousContainerName,
		})
		return reconcileResponseFromAgent(response), err
	}

	switch req.Action {
	case controlplane.ActionApply:
		return r.apply(runCtx, endpoint, req)
	case controlplane.ActionStop, controlplane.ActionPurge:
		return r.stop(runCtx, endpoint, req)
	default:
		return controlplane.ReconcileResponse{ErrorCode: "invalid_action", ErrorClass: "configuration", ErrorMessage: "unsupported runtime action", Retryable: false}, nil
	}
}

func (r *serviceRuntimeReconciler) apply(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC) (controlplane.ReconcileResponse, error) {
	spec, err := runtimeSpecForRequest(req)
	if err != nil {
		return failureResponse("invalid_spec", "invalid_spec", false, nil, err)
	}

	if spec.ApplicationID != req.ApplicationID || spec.InstanceID != req.InstanceID {
		return controlplane.ReconcileResponse{ErrorCode: "invalid_spec_identity", ErrorClass: "invalid_spec", ErrorMessage: "runtime spec identity does not match job", Retryable: false}, nil
	}

	steps := []controlplane.Step{{Name: "write_files", Status: "running"}}
	if err := r.service.runtimeClient.RuntimeWriteFiles(ctx, endpoint, agentcontract.RuntimeWriteFilesRequest{Spec: spec}); err != nil {
		return failureResponse("write_files_failed", classifyRuntimeError(ctx, err), true, steps, err)
	}
	steps[0].Status = "succeeded"

	steps = append(steps, controlplane.Step{Name: "create_container", Status: "running"})
	created, err := r.service.runtimeClient.RuntimeCreateContainer(ctx, endpoint, agentcontract.RuntimeCreateContainerRequest{ServerID: req.ServerID, Spec: spec})
	if err != nil {
		return failureResponse("create_container_failed", classifyRuntimeError(ctx, err), true, steps, err)
	}
	steps[len(steps)-1].Status = "succeeded"

	steps = append(steps, controlplane.Step{Name: "verify_running", Status: "running"})
	status, err := r.service.runtimeClient.RuntimeStatus(ctx, endpoint, req.InstanceID, spec.ContainerName)
	if err != nil {
		return failureResponse("verification_failed", classifyRuntimeError(ctx, err), true, steps, err)
	}
	if status.Status != appruntime.StatusRunning {
		err := fmt.Errorf("container status is %s", firstNonEmpty(status.Status, "unknown"))
		return failureResponse("container_not_running", "container_start_failed", true, steps, err)
	}
	steps[len(steps)-1].Status = "succeeded"

	containerID := firstNonEmpty(status.ContainerID, created.ContainerID)
	imageDigest := ""
	if strings.Contains(status.Image, "@sha256:") {
		imageDigest = status.Image[strings.Index(status.Image, "@sha256:")+1:]
	}
	return controlplane.ReconcileResponse{
		ObservedState:       controlplane.ObservedRunning,
		ContainerName:       firstNonEmpty(status.ContainerName, spec.ContainerName),
		ContainerID:         containerID,
		ObservedGeneration:  req.DesiredGeneration,
		ObservedSpecHash:    req.DesiredSpecHash,
		ObservedImageDigest: imageDigest,
		ObservedAt:          status.ObservedAt,
		Steps:               steps,
	}, nil
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

func (r *serviceRuntimeReconciler) stop(ctx context.Context, endpoint string, req controlplane.ReconcileRequestRPC) (controlplane.ReconcileResponse, error) {
	result, err := r.service.runtimeClient.RuntimeStop(ctx, endpoint, agentcontract.RuntimeStopRequest{
		ApplicationID:         req.ApplicationID,
		InstanceID:            req.InstanceID,
		ContainerName:         req.PreviousContainerName,
		Purge:                 req.Action == controlplane.ActionPurge,
		RemoveApplicationData: req.RemoveData,
	})
	if err != nil {
		return failureResponse("stop_failed", classifyRuntimeError(ctx, err), true, []controlplane.Step{{Name: req.Action, Status: "failed"}}, err)
	}
	state := controlplane.ObservedStopped
	if req.Action == controlplane.ActionPurge {
		state = controlplane.ObservedMissing
	}
	if result.Error != "" {
		return failureResponse("stop_failed", "runtime", true, []controlplane.Step{{Name: req.Action, Status: "failed"}}, errors.New(result.Error))
	}
	return controlplane.ReconcileResponse{
		ObservedState:      state,
		ContainerName:      result.ContainerName,
		ContainerID:        result.ContainerID,
		ObservedGeneration: 0,
		ObservedAt:         result.ObservedAt,
		Steps:              []controlplane.Step{{Name: req.Action, Status: "succeeded"}},
	}, nil
}

func failureResponse(code, class string, retryable bool, steps []controlplane.Step, err error) (controlplane.ReconcileResponse, error) {
	message := "runtime reconcile failed"
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	return controlplane.ReconcileResponse{ErrorCode: code, ErrorClass: class, ErrorMessage: message, Retryable: retryable, Steps: steps}, err
}

func classifyRuntimeError(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "agent_unavailable"
	}
	return "runtime"
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
