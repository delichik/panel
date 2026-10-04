package server

import (
	"context"
	"sort"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentendpoint "panel/internal/agent/endpoint"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// tailscaleApplyTaskType 是节点侧 tailscale 收敛任务：安装软件包、加入
// tailnet、写回观测态或按用户意图退出 tailnet。
const tailscaleApplyTaskType = "server_tailscale_apply"

// tailscaleResourceType 是任务中心展示用的资源类型。
const tailscaleResourceType = "server"

// TailscaleSettings 是节点侧加入 tailnet 所需的全局配置，由设置模块提供。
type TailscaleSettings struct {
	AuthKey string
	Tags    []string
}

// WithTailscaleSettings 注入全局 tailscale 配置提供者。未注入时节点无法首次
// 加入 tailnet，任务会给出明确失败而不是静默跳过。
func WithTailscaleSettings(provider func() TailscaleSettings) Option {
	return func(s *Service) { s.tailscaleSettings = provider }
}

// ApplyTailscale 为用户显式操作创建或复用节点 tailscale 收敛任务。与 Agent
// 部署一致：响应只代表任务已被接受并持久化（GOV-DOD-007）。
func (s *Service) ApplyTailscale(ctx context.Context, serverID string) (tasks.Task, error) {
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if !srv.TailscaleEnabled {
		return tasks.Task{}, panelerr.Validation("tailscale_not_enabled", "Enable Tailscale on this server before applying it")
	}
	return s.ensureTailscaleApplyTask(ctx, srv, "user", true)
}

// ensureTailscaleApplyTask 创建或复用收敛任务。run 为真时立即开始执行。
func (s *Service) ensureTailscaleApplyTask(ctx context.Context, srv Server, triggeredBy string, run bool) (tasks.Task, error) {
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         tailscaleApplyTaskType,
		ServerID:     srv.ID,
		ResourceType: tailscaleResourceType,
		ResourceID:   srv.ID,
		TriggeredBy:  triggeredBy,
		Summary:      "Applying Tailscale configuration for " + srv.Name,
	}, tasks.Trigger{Type: triggeredBy, Manual: triggeredBy == "user", Periodic: triggeredBy != "user"})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created && task.Status == tasks.StatusRunning {
		return task, nil
	}
	if run && !created {
		if task, err = s.tasks.RunNow(ctx, task.ID); err != nil {
			return tasks.Task{}, err
		}
	}
	if run {
		if err := s.startTailscaleApplyTask(task, srv); err != nil {
			return tasks.Task{}, err
		}
		task, _ = s.tasks.Get(ctx, task.ID)
	}
	return task, nil
}

func (s *Service) startTailscaleApplyTask(task tasks.Task, srv Server) error {
	if err := s.tasks.Start(context.Background(), task.ID); err != nil {
		return err
	}
	go s.runTailscaleApply(s.tasks.ExecutionContext(task.ID), task.ID, srv.ID)
	return nil
}

// RunTailscaleApplyTask 是任务执行体。
func (s *Service) RunTailscaleApplyTask(tc tasks.TaskContext) error {
	return s.runTailscaleApply(tc.Context, tc.Task.ID, tc.Task.ServerID)
}

func (s *Service) runTailscaleApply(ctx context.Context, taskID, serverID string) error {
	defer s.tasks.FinishExecution(taskID)
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return err
	}
	client, ok := s.agent.(agentcontract.TailscaleClient)
	if !ok {
		return s.failTailscaleApply(ctx, taskID, serverID, panelerr.Validation("tailscale_agent_unsupported", "Agent does not support Tailscale management; upgrade the agent on this server"))
	}
	endpoint, ok := agentURL(srv)
	if !ok {
		return s.failTailscaleApply(ctx, taskID, serverID, panelerr.Validation("agent_required", "Agent is required for Tailscale management"))
	}
	if err := s.requireTailscaleCapability(ctx, srv, endpoint); err != nil {
		return s.failTailscaleApply(ctx, taskID, serverID, err)
	}
	_ = s.tasks.Advance(ctx, taskID, "applying", "applying tailscale configuration on the node")

	if !srv.TailscaleEnabled {
		// 关闭意图：让节点退出 tailnet 连接但保留软件包与节点身份，
		// 使再次启用无需重新申请节点密钥。
		_ = s.tasks.Advance(ctx, taskID, "disabling", "disconnecting the node from the tailnet")
		status, err := client.TailscaleDisable(ctx, endpoint)
		if err != nil {
			return s.failTailscaleApply(ctx, taskID, serverID, err)
		}
		if err := s.recordTailscaleObservation(ctx, srv, status); err != nil {
			return err
		}
		_ = s.tasks.Complete(ctx, taskID, "Tailscale disconnected on "+srv.Name)
		return nil
	}

	settings := s.tailscaleConfig()
	_ = s.tasks.Advance(ctx, taskID, "joining", "joining the tailnet")
	status, err := client.TailscaleConfigure(ctx, endpoint, agentcontract.TailscaleConfigureRequest{
		AuthKey: settings.AuthKey,
		Tags:    append([]string(nil), settings.Tags...),
	})
	if err != nil {
		return s.failTailscaleApply(ctx, taskID, serverID, err)
	}
	if err := s.recordTailscaleObservation(ctx, srv, status); err != nil {
		return err
	}
	if !status.LoggedIn {
		message := firstNonEmpty(status.LastError, "node did not join the tailnet")
		return s.failTailscaleApply(ctx, taskID, serverID, panelerr.Validation("tailscale_join_failed", message))
	}
	_ = s.tasks.Complete(ctx, taskID, "Tailscale is active on "+srv.Name)
	return nil
}

// requireTailscaleCapability 按 AGT-STATE-004 只让该操作失败：缺少能力的旧
// Agent 需要升级，而不是重装节点。
func (s *Service) requireTailscaleCapability(ctx context.Context, srv Server, endpoint string) error {
	health, err := s.agent.Health(ctx, endpoint)
	if err != nil {
		return err
	}
	if !hasAgentCapability(health.Capabilities, agentcontract.CapabilityTailscale) {
		return panelerr.Validation("tailscale_agent_unsupported", "Agent does not support Tailscale management; upgrade the agent on this server")
	}
	return nil
}

func (s *Service) failTailscaleApply(ctx context.Context, taskID, serverID string, cause error) error {
	_ = s.markTailscaleFailure(ctx, serverID, cause)
	_ = s.tasks.Fail(ctx, taskID, cause)
	return cause
}

func (s *Service) tailscaleConfig() TailscaleSettings {
	if s.tailscaleSettings == nil {
		return TailscaleSettings{}
	}
	return s.tailscaleSettings()
}

// reconcileTailscaleIntent 在用户保存节点开关后立即收敛一次。任务失败只记录
// 状态，不影响保存结果本身。
func (s *Service) reconcileTailscaleIntent(ctx context.Context, serverID string, enabled bool) {
	if s.tasks == nil {
		return
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return
	}
	if enabled && strings.TrimSpace(srv.Traits[agentcontract.TraitEnabled]) != "true" {
		// 节点还没有可用的 Agent：先由部署流程接管，tailscale 任务在 Agent
		// 就绪后由状态收敛再触发。
		_ = s.markTailscalePending(ctx, srv.ID)
		return
	}
	if !enabled && !isTailscaleObserved(srv) {
		return
	}
	if _, err := s.ensureTailscaleApplyTask(ctx, srv, "system", true); err != nil {
		logging.L().Warn("queue tailscale apply failed", zap.String("server_id", serverID), zap.Error(err))
	}
}

// ApplyTailscaleReport 写入节点上报的 tailscale 观测态。旧 Agent 不上报该
// 字段时调用方不会进入这里，因此已有观测不会被空报告覆盖。
func (s *Service) ApplyTailscaleReport(ctx context.Context, serverID string, status agentcontract.TailscaleStatus) error {
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return err
	}
	if !srv.TailscaleEnabled {
		// 用户没有为该节点启用 tailscale：节点自行运行 tailscale 不是 Panel
		// 的托管对象，不得据此创建观测态或地址。
		return nil
	}
	if !status.Installed {
		if err := s.markTailscaleStatus(ctx, srv.ID, agentcontract.TailscaleStatusUnsupported, "tailscale is not installed on this node"); err != nil {
			return err
		}
		if err := s.clearTailscaleAddress(ctx, srv); err != nil {
			return err
		}
		if agentendpoint.TailnetAddress(srv.Traits) != "" {
			// 地址失效同样会改变互联链路，必须让设施重算。
			s.notifyInterconnectChange(ctx, srv.ID)
		}
		return nil
	}
	previous := agentendpoint.TailnetAddress(srv.Traits)
	if err := s.recordTailscaleObservation(ctx, srv, status); err != nil {
		return err
	}
	if err := s.ensureTailscaleCertificateHosts(ctx, srv, status); err != nil {
		return err
	}
	if previous != agentendpoint.TailnetAddress(map[string]string{
		agentcontract.TraitTailscaleIPv4: status.IPv4,
		agentcontract.TraitTailscaleIPv6: status.IPv6,
	}) {
		s.notifyInterconnectChange(ctx, srv.ID)
	}
	return nil
}

// mergeTailscaleTraits 在既有 traits 之上写入 tailscale 观测。saveServerTraits
// 是整体替换，因此必须先读后写，否则会把 agent.* 等系统 trait 一并清空。
func (s *Service) mergeTailscaleTraits(ctx context.Context, serverID string, values map[string]string) error {
	traits, err := s.loadServerTraits(ctx, serverID)
	if err != nil {
		return err
	}
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			delete(traits, key)
			continue
		}
		traits[key] = value
	}
	return s.saveServerTraits(ctx, serverID, traits)
}

// recordTailscaleObservation 把节点事实写入 traits，并保持状态语义稳定：
// 只有 LoggedIn 才是 running，有错误文本时为 error，其余为 pending。
func (s *Service) recordTailscaleObservation(ctx context.Context, srv Server, status agentcontract.TailscaleStatus) error {
	state := agentcontract.TailscaleStatusPending
	switch {
	case status.LoggedIn:
		state = agentcontract.TailscaleStatusRunning
	case status.LastError != "":
		state = agentcontract.TailscaleStatusError
	}
	return s.mergeTailscaleTraits(ctx, srv.ID, map[string]string{
		agentcontract.TraitTailscaleStatus:    state,
		agentcontract.TraitTailscaleIPv4:      strings.TrimSpace(status.IPv4),
		agentcontract.TraitTailscaleIPv6:      strings.TrimSpace(status.IPv6),
		agentcontract.TraitTailscaleHostname:  strings.TrimSpace(status.Hostname),
		agentcontract.TraitTailscaleVersion:   strings.TrimSpace(status.Version),
		agentcontract.TraitTailscaleLastError: strings.TrimSpace(status.LastError),
		agentcontract.TraitTailscaleUpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Service) markTailscalePending(ctx context.Context, serverID string) error {
	return s.mergeTailscaleTraits(ctx, serverID, map[string]string{agentcontract.TraitTailscaleStatus: agentcontract.TailscaleStatusPending})
}

func (s *Service) markTailscaleStatus(ctx context.Context, serverID, state, message string) error {
	return s.mergeTailscaleTraits(ctx, serverID, map[string]string{
		agentcontract.TraitTailscaleStatus:    state,
		agentcontract.TraitTailscaleLastError: strings.TrimSpace(message),
		agentcontract.TraitTailscaleUpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Service) markTailscaleFailure(ctx context.Context, serverID string, cause error) error {
	if cause == nil {
		return nil
	}
	return s.markTailscaleStatus(ctx, serverID, agentcontract.TailscaleStatusError, cause.Error())
}

// clearTailscaleAddress 在节点未安装或已关闭时清除地址，避免互联与界面继续
// 使用一个已经失效的 tailnet 地址。
func (s *Service) clearTailscaleAddress(ctx context.Context, srv Server) error {
	return s.mergeTailscaleTraits(ctx, srv.ID, map[string]string{
		agentcontract.TraitTailscaleIPv4: "",
		agentcontract.TraitTailscaleIPv6: "",
	})
}

// agentCertificateHosts 返回节点证书必须覆盖的地址集合。启用“优先使用
// tailscale 地址连接 agent”时，tailnet 地址也在其中：gRPC 客户端按拨号目标
// 校验主机名，缺少该地址会让 mTLS 失败。
func agentCertificateHosts(srv Server) []string {
	hosts := []string{strings.TrimSpace(srv.Host)}
	if srv.TailscaleEnabled && srv.TailscalePreferAgent {
		if address := agentendpoint.TailnetAddress(srv.Traits); address != "" {
			hosts = append(hosts, address)
		}
	}
	out := make([]string, 0, len(hosts))
	seen := map[string]struct{}{}
	for _, host := range hosts {
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		out = append(out, host)
	}
	sort.Strings(out)
	return out
}

// ensureTailscaleCertificateHosts 在节点 tailnet 地址变化后发现证书 SAN 集合
// 不再匹配时，标记节点需要刷新证书。复用既有的 Agent 部署通道：版本与二进制
// 未变时该通道只重写证书与配置（AGT-DEP-003），不会重传二进制。
func (s *Service) ensureTailscaleCertificateHosts(ctx context.Context, srv Server, status agentcontract.TailscaleStatus) error {
	if !srv.TailscaleEnabled || !srv.TailscalePreferAgent {
		return nil
	}
	current, err := s.Get(ctx, srv.ID)
	if err != nil {
		return err
	}
	desired := agentCertificateHosts(current)
	recorded := splitTraitList(current.Traits[agentcontract.TraitCertificateHosts])
	if sameStringSet(desired, recorded) {
		return nil
	}
	if err := s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", "tailscale address changed; agent certificate refresh required"); err != nil {
		return err
	}
	return nil
}

// recordAgentCertificateHosts 记录本次签发的 SAN 集合，供后续比较是否需要重签。
// 这里同样必须先读后写：证书 trait 与 agent.* 状态共存。
func (s *Service) recordAgentCertificateHosts(ctx context.Context, srv Server, hosts []string) error {
	return s.mergeTailscaleTraits(ctx, srv.ID, map[string]string{agentcontract.TraitCertificateHosts: strings.Join(hosts, ",")})
}

func splitTraitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

// isTailscaleObserved 报告该节点是否已有 tailscale 观测态。
func isTailscaleObserved(srv Server) bool {
	for _, key := range []string{
		agentcontract.TraitTailscaleStatus,
		agentcontract.TraitTailscaleIPv4,
		agentcontract.TraitTailscaleIPv6,
	} {
		if strings.TrimSpace(srv.Traits[key]) != "" {
			return true
		}
	}
	return false
}
