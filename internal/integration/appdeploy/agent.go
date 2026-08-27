// Package appdeploy 提供剧本驱动的应用部署集成测试台：mock agent 按剧本
// 报告/响应，测试通过真实的 applications.Service + orchestrator.Controller
// 驱动完整流程（触发→planner→Job→RuntimeReconcile→观测→收敛/重试/恢复）。
package appdeploy

import (
	"context"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
)

// ScriptKey 定位一条 agent 剧本。Call 是 1 起的调用序号（按
// application+server+action 计数），0 表示“任意次数”兜底。
type ScriptKey struct {
	ApplicationID string
	ServerID      string
	Action        string
	Call          int
}

type ScriptedResponse struct {
	// Success 为 true 时返回成功观测快照；false 时返回结构化错误。
	Success             bool
	ObservedState       string
	ContainerID         string
	ContainerName       string
	ObservedGeneration  int
	ObservedSpecHash    string
	ObservedImageDigest string
	Steps               []agentcontract.RuntimeReconcileStep
	ErrorCode           string
	ErrorClass          string
	ErrorMessage        string
	Retryable           bool
	RetryAfter          time.Duration
	Delay               time.Duration // 响应前阻塞（模拟慢 agent/租约过期）
	HangUntilDeadline   bool          // 阻塞到 ctx 结束（模拟 RPC 超时）
}

// CallRecord 记录一次 RuntimeReconcile 调用，供剧本断言。
type CallRecord struct {
	ApplicationID string
	ServerID      string
	Action        string
	ExecutionID   string
	Attempt       int
	Request       agentcontract.RuntimeReconcileRequest
	Responded     bool
}

type ScriptedAgent struct {
	mu        sync.Mutex
	script    map[ScriptKey]ScriptedResponse
	defaults  []ScriptedResponse // 顺序取用；用尽后取最后一条
	calls     map[ScriptKey]int  // 调用计数
	records   []CallRecord
	reconcile func(ctx context.Context, baseURL string, req agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error)
	stopErr   error
}

func NewScriptedAgent() *ScriptedAgent {
	return &ScriptedAgent{
		script: map[ScriptKey]ScriptedResponse{},
		calls:  map[ScriptKey]int{},
		reconcile: func(ctx context.Context, baseURL string, req agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error) {
			state := appruntime.StatusRunning
			switch req.Action {
			case "stop":
				state = appruntime.StatusStopped
			case "purge":
				state = appruntime.StatusMissing
			}
			return agentcontract.RuntimeReconcileResponse{
				ObservedState:      state,
				ContainerName:      req.Spec.ContainerName,
				ContainerID:        "container-" + req.InstanceID,
				ObservedGeneration: req.DesiredGeneration,
				ObservedSpecHash:   req.DesiredSpecHash,
				ObservedAt:         time.Now().UTC(),
			}, nil
		},
	}
}

// Script 注册一条剧本；Call=0 表示该键任意次数的兜底。
func (a *ScriptedAgent) Script(appID, serverID, action string, call int, resp ScriptedResponse) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.script[ScriptKey{ApplicationID: appID, ServerID: serverID, Action: action, Call: call}] = resp
}

// SetReconcile 覆盖默认 RuntimeReconcile 实现（用于自定义行为）。
func (a *ScriptedAgent) SetReconcile(fn func(ctx context.Context, baseURL string, req agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reconcile = fn
}

// Records 返回全部 RuntimeReconcile 调用记录（按调用顺序）。
func (a *ScriptedAgent) Records() []CallRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]CallRecord, len(a.records))
	copy(out, a.records)
	return out
}

func (a *ScriptedAgent) RecordCount(appID, serverID, action string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls[ScriptKey{ApplicationID: appID, ServerID: serverID, Action: action}]
}

// RuntimeReconcile 按剧本响应。这是 orchestrator 唯一调用的部署 RPC。
func (a *ScriptedAgent) RuntimeReconcile(ctx context.Context, baseURL string, req agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error) {
	a.mu.Lock()
	key := ScriptKey{ApplicationID: req.ApplicationID, ServerID: req.ServerID, Action: req.Action}
	a.calls[key]++
	call := a.calls[key]
	resp, ok := a.script[ScriptKey{ApplicationID: req.ApplicationID, ServerID: req.ServerID, Action: req.Action, Call: call}]
	if !ok {
		resp, ok = a.script[ScriptKey{ApplicationID: req.ApplicationID, ServerID: req.ServerID, Action: req.Action, Call: 0}]
	}
	record := CallRecord{
		ApplicationID: req.ApplicationID,
		ServerID:      req.ServerID,
		Action:        req.Action,
		ExecutionID:   req.ExecutionID,
		Attempt:       call,
		Request:       req,
	}
	a.records = append(a.records, record)
	reconcile := a.reconcile
	a.mu.Unlock()

	if !ok {
		// 没有命中剧本：走默认实现（按请求回显期望态，模拟真实收敛）。
		return reconcile(ctx, baseURL, req)
	}
	if resp.HangUntilDeadline {
		<-ctx.Done()
		return agentcontract.RuntimeReconcileResponse{}, ctx.Err()
	}
	if resp.Delay > 0 {
		select {
		case <-time.After(resp.Delay):
		case <-ctx.Done():
			return agentcontract.RuntimeReconcileResponse{}, ctx.Err()
		}
	}
	a.mu.Lock()
	record.Responded = true
	a.records[len(a.records)-1] = record
	a.mu.Unlock()

	if resp.Success {
		observed := resp.ObservedState
		if observed == "" {
			observed = appruntime.StatusRunning
		}
		return agentcontract.RuntimeReconcileResponse{
			ObservedState:       observed,
			ContainerName:       resp.ContainerName,
			ContainerID:         resp.ContainerID,
			ObservedGeneration:  resp.ObservedGeneration,
			ObservedSpecHash:    resp.ObservedSpecHash,
			ObservedImageDigest: resp.ObservedImageDigest,
			ObservedAt:          time.Now().UTC(),
			Steps:               resp.Steps,
		}, nil
	}
	return agentcontract.RuntimeReconcileResponse{
		ErrorCode:    resp.ErrorCode,
		ErrorClass:   resp.ErrorClass,
		ErrorMessage: resp.ErrorMessage,
		Retryable:    resp.Retryable,
		RetryAfter:   resp.RetryAfter,
	}, nil
}

func (a *ScriptedAgent) Health(ctx context.Context, baseURL string) (agentcontract.HealthResponse, error) {
	return agentcontract.HealthResponse{
		Status:       "ok",
		Time:         time.Now().UTC().Format(time.RFC3339),
		Version:      "0.0.0",
		Capabilities: append([]string{"runtime-reconcile"}, agentcontract.RequiredCapabilities...),
	}, nil
}

// 以下为旧 primitive API 的兜底实现：部署场景不会走到，仅保持接口完整。

func (a *ScriptedAgent) RuntimeWriteFiles(ctx context.Context, baseURL string, req agentcontract.RuntimeWriteFilesRequest) error {
	return nil
}
func (a *ScriptedAgent) RuntimeReload(ctx context.Context, baseURL string, req agentcontract.RuntimeReloadRequest) (agentcontract.RuntimeReloadResponse, error) {
	return agentcontract.RuntimeReloadResponse{Reloaded: true}, nil
}
func (a *ScriptedAgent) RuntimeCreateContainer(ctx context.Context, baseURL string, req agentcontract.RuntimeCreateContainerRequest) (agentcontract.RuntimeCreateContainerResponse, error) {
	return agentcontract.RuntimeCreateContainerResponse{ContainerID: "container-" + req.Spec.InstanceID}, nil
}
func (a *ScriptedAgent) RuntimeStop(ctx context.Context, baseURL string, req agentcontract.RuntimeStopRequest) (agentcontract.RuntimeInstanceResponse, error) {
	if a.stopErr != nil {
		return agentcontract.RuntimeInstanceResponse{}, a.stopErr
	}
	return agentcontract.RuntimeInstanceResponse{InstanceID: req.InstanceID, ContainerName: req.ContainerName, Status: appruntime.StatusStopped, ObservedAt: time.Now().UTC()}, nil
}
func (a *ScriptedAgent) RuntimeStatus(ctx context.Context, baseURL, instanceID, containerName string) (agentcontract.RuntimeStatusResponse, error) {
	return agentcontract.RuntimeStatusResponse{InstanceStatus: appruntime.InstanceStatus{InstanceID: instanceID, ContainerName: containerName, Status: appruntime.StatusRunning, ObservedAt: time.Now().UTC()}}, nil
}
func (a *ScriptedAgent) RuntimeLogs(ctx context.Context, baseURL, instanceID, containerName string, tail int) (agentcontract.RuntimeLogsResponse, error) {
	return agentcontract.RuntimeLogsResponse{InstanceID: instanceID, Logs: ""}, nil
}
func (a *ScriptedAgent) RuntimePersistentArchive(ctx context.Context, baseURL, applicationID string) (agentcontract.RuntimePersistentArchiveResponse, error) {
	return agentcontract.RuntimePersistentArchiveResponse{}, nil
}
func (a *ScriptedAgent) RuntimePersistentRestore(ctx context.Context, baseURL, applicationID string, content []byte) (agentcontract.RuntimePersistentRestoreResponse, error) {
	return agentcontract.RuntimePersistentRestoreResponse{ApplicationID: applicationID, Restored: true}, nil
}
func (a *ScriptedAgent) DockerImagePull(ctx context.Context, baseURL, reference string) error {
	return nil
}
func (a *ScriptedAgent) DockerContainerDelete(ctx context.Context, baseURL, id string) error {
	return nil
}
func (a *ScriptedAgent) DockerContainerAction(ctx context.Context, baseURL, id, action string) error {
	return nil
}
