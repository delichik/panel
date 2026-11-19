package facilityapps

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

// 入口代理镜像检查/更新（FAC-RP-015）的超时与轮询参数。
const (
	proxyImageRegistryTimeout = 30 * time.Second
	proxyImageAgentTimeout    = 20 * time.Second
	proxyImagePullTimeout     = 5 * time.Minute
	proxyImageRolloutTimeout  = 10 * time.Minute
	proxyImagePollInterval    = 2 * time.Second
)

// proxyImageVersionCommand 是采集运行中入口代理实际版本的命令。结果由 Agent
// 解析为 nginx/x.y.z 并写入 facility_proxy_image_states.image_version。
var proxyImageVersionCommand = []string{"nginx", "-v"}

// updateIntervalSleeper 抽象入口代理滚动更新在相邻网关之间的等待。默认实现
// 是 ctx-aware timer；测试注入即时实现，既能断言间隔又不必真实等待。
type updateIntervalSleeper func(ctx context.Context, d time.Duration) error

// sleepUpdateInterval 是默认等待：0 不等待；ctx 取消时立即返回 ctx.Err()。
func sleepUpdateInterval(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// waitUpdateInterval 使用注入实现（缺省回退到默认 ctx-aware timer）。
func (s *Service) waitUpdateInterval(ctx context.Context, d time.Duration) error {
	if s == nil || s.updateIntervalSleeper == nil {
		return sleepUpdateInterval(ctx, d)
	}
	return s.updateIntervalSleeper(ctx, d)
}

// proxyImageUpdateInterval 把设施设置解析成滚动更新间隔；resolved() 已把越界
// 值收敛到 0..600 秒。
func proxyImageUpdateInterval(cfg ReverseProxyConfig) time.Duration {
	return time.Duration(cfg.Settings.resolved().ImageUpdateIntervalSeconds) * time.Second
}

type proxyImageTaskParams struct {
	ServerIDs []string `json:"serverIds,omitempty"`
	// Rollback/Digest 复用更新任务执行镜像回滚（预拉取 pinned digest → 写目标 →
	// reconcile → 校验）；普通更新任务这两项为空。
	Rollback bool   `json:"rollback,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

// proxyImageUpdateAttempt 描述一次滚动的来源：自动更新需要遵守 pinned 与失败
// 退避，并在失败时按目标 digest 累计退避计数；手动更新/回滚会清零退避。
type proxyImageUpdateAttempt struct {
	Auto bool
	// ExpectedDigest 是本次尝试的目标 digest（自动更新时来自 registry 解析）。
	ExpectedDigest string
}

// proxyImageRollbackTarget 是单个网关解析后的回滚目标。
type proxyImageRollbackTarget struct {
	ServerID string
	Digest   string
}

// registerProxyImageTasks 由 NewService 在 taskSvc 存在时调用。检查任务周期
// 6h（StartPeriodic 驱动）；更新任务只允许内部协调/显式手动触发，因此
// AllowRunNow=false，且不可取消（重建入口代理容器属于破坏性操作）。
func (s *Service) registerProxyImageTasks() {
	proxyImageConcurrencyKeyFn := func(tasks.CreateInput) string { return proxyImageConcurrencyKey }
	s.tasks.MustRegister(tasks.Definition{
		Type:              proxyImageCheckTaskType,
		Summary:           "Checking entrance proxy image updates",
		AllowRunNow:       true,
		AllowRetry:        true,
		ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
		ConcurrencyKey:    proxyImageConcurrencyKeyFn,
		Execute:           s.runProxyImageCheckTask,
		Periodic: &tasks.Periodic{
			CollectInputs: s.collectProxyImageCheckInputs,
		},
	})
	s.tasks.MustRegister(tasks.Definition{
		Type:              proxyImageUpdateTaskType,
		Summary:           "Updating entrance proxy image",
		AllowRunNow:       false,
		AllowRetry:        true,
		DisallowCancel:    true,
		ConcurrencyPolicy: tasks.ConcurrencyCustomKey,
		ConcurrencyKey:    proxyImageConcurrencyKeyFn,
		Execute:           s.runProxyImageUpdateTask,
	})
}

func (s *Service) collectProxyImageCheckInputs(_ context.Context, trigger tasks.PeriodicTrigger) (tasks.CreateBatchInput, bool, error) {
	triggerType := firstNonEmpty(trigger.Type, "scheduler")
	return tasks.CreateBatchInput{
		Type:        proxyImageCheckTaskType,
		TriggerType: triggerType,
		Summary:     "Checking entrance proxy image updates",
		Inputs: []tasks.CreateInput{{
			Type:         proxyImageCheckTaskType,
			ResourceType: "facility_app",
			ResourceID:   ReverseProxyID,
			TriggerType:  triggerType,
			Summary:      "Checking entrance proxy image updates",
		}},
	}, true, nil
}

// CheckReverseProxyImage 接受一次手动镜像检查（后台任务）。
func (s *Service) CheckReverseProxyImage(ctx context.Context) (ProxyImageAction, error) {
	if s == nil || s.tasks == nil {
		return ProxyImageAction{}, panelerr.Validation("facility_proxy_image_unavailable", "entrance proxy image check is unavailable")
	}
	task, _, err := tasks.NewManager(s.tasks).CreateAndRun(ctx, tasks.CreateInput{
		Type:         proxyImageCheckTaskType,
		ResourceType: "facility_app",
		ResourceID:   ReverseProxyID,
		TriggerType:  "user",
		Summary:      "Checking entrance proxy image updates",
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return ProxyImageAction{}, err
	}
	return ProxyImageAction{TaskID: task.ID}, nil
}

// UpdateReverseProxyImage 接受一次手动镜像更新（后台任务，逐节点滚动）。
func (s *Service) UpdateReverseProxyImage(ctx context.Context, in ProxyImageUpdateInput) (ProxyImageAction, error) {
	if s == nil || s.tasks == nil {
		return ProxyImageAction{}, panelerr.Validation("facility_proxy_image_unavailable", "entrance proxy image update is unavailable")
	}
	if !in.Confirm {
		return ProxyImageAction{}, panelerr.Validation("facility_proxy_image_update_confirmation_required", "confirm the entrance proxy image update before it runs")
	}
	params, err := json.Marshal(proxyImageTaskParams{ServerIDs: uniqueSorted(in.ServerIDs)})
	if err != nil {
		return ProxyImageAction{}, err
	}
	task, _, err := tasks.NewManager(s.tasks).CreateAndRun(ctx, tasks.CreateInput{
		Type:         proxyImageUpdateTaskType,
		ResourceType: "facility_app",
		ResourceID:   ReverseProxyID,
		TriggerType:  "user",
		Summary:      "Updating entrance proxy image",
		ParamsJSON:   string(params),
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return ProxyImageAction{}, err
	}
	return ProxyImageAction{TaskID: task.ID}, nil
}

// RollbackReverseProxyImage 接受一次手动镜像回滚（P1）：把每个目标网关固定到
// previous_digest（或显式 digest）并复用逐节点滚动流程 recreate。回滚成功后
// pinned=true，自动更新暂停，直到调用 ResumeReverseProxyImage。没有可回滚目标
// 或节点已固定成功时是幂等空操作（skipped=true），不创建任务。
func (s *Service) RollbackReverseProxyImage(ctx context.Context, in ProxyImageRollbackInput) (ProxyImageAction, error) {
	if !in.Confirm {
		return ProxyImageAction{}, panelerr.Validation("facility_proxy_image_rollback_confirmation_required", "confirm the entrance proxy image rollback before it runs")
	}
	explicitDigest := normalizeDigest(in.Digest)
	if strings.TrimSpace(in.Digest) != "" && !nginxDigestPattern.MatchString(explicitDigest) {
		return ProxyImageAction{}, panelerr.WithDetails(
			panelerr.Validation("facility_proxy_image_digest_invalid", "rollback digest must be a sha256 image digest"),
			map[string]any{"digest": strings.TrimSpace(in.Digest)},
		)
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyImageAction{}, err
	}
	gateways := orderedUniqueStrings(cfg.DeploymentServers)
	targets, err := s.planProxyImageRollback(ctx, gateways, in.ServerIDs, explicitDigest)
	if err != nil {
		return ProxyImageAction{}, err
	}
	if len(targets) == 0 {
		// 无回滚目标或所有节点都已固定：幂等空操作，不创建任务。
		status, statusErr := s.proxyImageStatus(ctx, gateways)
		if statusErr != nil {
			return ProxyImageAction{}, statusErr
		}
		return ProxyImageAction{Skipped: true, Pinned: status.Pinned, Status: status.Status}, nil
	}
	if s.tasks == nil {
		return ProxyImageAction{}, panelerr.Validation("facility_proxy_image_unavailable", "entrance proxy image rollback is unavailable")
	}
	serverIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		serverIDs = append(serverIDs, target.ServerID)
	}
	params, err := json.Marshal(proxyImageTaskParams{ServerIDs: serverIDs, Rollback: true, Digest: explicitDigest})
	if err != nil {
		return ProxyImageAction{}, err
	}
	task, _, err := tasks.NewManager(s.tasks).CreateAndRun(ctx, tasks.CreateInput{
		Type:         proxyImageUpdateTaskType,
		ResourceType: "facility_app",
		ResourceID:   ReverseProxyID,
		TriggerType:  "user",
		Summary:      "Rolling back entrance proxy image",
		ParamsJSON:   string(params),
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return ProxyImageAction{}, err
	}
	return ProxyImageAction{TaskID: task.ID}, nil
}

// ResumeReverseProxyImage 清除 pinned 与自动更新失败退避，把镜像引用恢复到受管
// 标签线，并刷新镜像状态投影。对未被固定/没有失败退避的节点是幂等空操作。
func (s *Service) ResumeReverseProxyImage(ctx context.Context, in ProxyImageResumeInput) (ProxyImageStatus, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyImageStatus{}, err
	}
	gateways := orderedUniqueStrings(cfg.DeploymentServers)
	serverIDs := orderedUniqueStrings(in.ServerIDs)
	if len(serverIDs) == 0 {
		serverIDs = gateways
	} else if err := validateProxyImageGatewaySelection(gateways, serverIDs); err != nil {
		return ProxyImageStatus{}, err
	}
	latestDigest, resolveErr := s.resolveProxyImageDigest(ctx, supportedProxyImage)
	if resolveErr != nil {
		latestDigest = ""
	}
	for _, serverID := range serverIDs {
		state, err := s.loadProxyImageState(ctx, serverID)
		if err != nil {
			return ProxyImageStatus{}, err
		}
		changed := state.Pinned || state.AutoUpdateFailures > 0 || strings.TrimSpace(state.FailedDigest) != "" || digestFromReference(state.Reference) != ""
		if !changed {
			continue
		}
		state.Pinned = false
		resetProxyImageFailureState(&state)
		state.Reference = supportedProxyImage
		state.LastError = ""
		switch {
		case latestDigest == "":
			state.Status = ProxyImageStatusUnknown
		case sameDigest(state.ObservedDigest, latestDigest):
			state.TargetDigest = state.ObservedDigest
			state.Status = ProxyImageStatusUpToDate
		default:
			state.Status = ProxyImageStatusUpdateAvailable
		}
		if err := s.saveProxyImageState(ctx, state); err != nil {
			return ProxyImageStatus{}, err
		}
	}
	status, statusErr := s.proxyImageStatus(ctx, gateways)
	if statusErr != nil {
		return ProxyImageStatus{}, statusErr
	}
	if latestDigest != "" {
		status.LatestDigest = latestDigest
		recomputeProxyImageStatusProjection(&status, latestDigest)
	}
	_ = s.updateProxyImageProjection(ctx, status)
	return status, nil
}

// AutoUpdateReverseProxyImage 是自动更新协调任务（settings/autoupdate）的同步
// 入口：对象级开关关闭 → 直接返回 nil；否则解析 registry digest、逐网关对比
// 实际运行 digest。必须遵守 pinned 与失败退避：固定或退避中的节点会被跳过并
// 记录原因。返回错误代表更新失败（已中止）。
func (s *Service) AutoUpdateReverseProxyImage(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if !s.ProxyImageAutoUpdateEnabled(ctx) {
		return nil
	}
	reporter := s.newProxyImageReporter(ctx, "")
	status, err := s.runProxyImageCheck(ctx, reporter)
	if err != nil {
		return err
	}
	if !status.UpdateAvailable {
		return nil
	}
	states, statesErr := s.loadProxyImageStates(ctx, orderedUniqueStrings(gatewayIDs(status.Servers)))
	if statesErr != nil {
		reporter.logf("load image states for automatic update failed: %v", statesErr)
	}
	serverIDs := []string{}
	for _, item := range status.Servers {
		if item.Status != ProxyImageStatusUpdateAvailable {
			continue
		}
		state := states[item.ServerID]
		if state.Pinned {
			reporter.logf("%s: automatic update skipped: gateway is pinned to %s; resume automatic updates first", item.ServerID, shortDigest(item.TargetDigest))
			continue
		}
		if reason := proxyImageAutoUpdatePauseReason(state, status.LatestDigest); reason != "" {
			reporter.logf("%s: automatic update skipped: %s", item.ServerID, reason)
			continue
		}
		serverIDs = append(serverIDs, item.ServerID)
	}
	if len(serverIDs) == 0 {
		reporter.logf("no gateway eligible for automatic entrance proxy image update")
		return nil
	}
	_, err = s.runProxyImageUpdate(ctx, serverIDs, reporter, proxyImageUpdateAttempt{Auto: true, ExpectedDigest: status.LatestDigest})
	return err
}

// ProxyImageAutoUpdateEnabled 返回设施设置里的对象级自动更新开关（默认开）。
func (s *Service) ProxyImageAutoUpdateEnabled(ctx context.Context) bool {
	if s == nil {
		return true
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return true
	}
	return cfg.Settings.resolved().ImageAutoUpdateEnabled
}

func (s *Service) runProxyImageCheckTask(tc tasks.TaskContext) error {
	if err := s.tasks.Start(tc.Context, tc.Task.ID); err != nil {
		return err
	}
	execCtx := s.tasks.ExecutionContext(tc.Task.ID)
	reporter := s.newProxyImageReporter(execCtx, tc.Task.ID)
	go func() {
		defer s.tasks.FinishExecution(tc.Task.ID)
		status, err := s.runProxyImageCheck(execCtx, reporter)
		if err != nil {
			_ = s.tasks.Fail(execCtx, tc.Task.ID, err)
			return
		}
		_ = s.tasks.Complete(execCtx, tc.Task.ID, "Entrance proxy image checked: "+status.Status)
	}()
	return nil
}

func (s *Service) runProxyImageUpdateTask(tc tasks.TaskContext) error {
	var params proxyImageTaskParams
	_ = json.Unmarshal([]byte(tc.Task.ParamsJSON), &params)
	if err := s.tasks.Start(tc.Context, tc.Task.ID); err != nil {
		return err
	}
	execCtx := s.tasks.ExecutionContext(tc.Task.ID)
	reporter := s.newProxyImageReporter(execCtx, tc.Task.ID)
	go func() {
		defer s.tasks.FinishExecution(tc.Task.ID)
		if params.Rollback {
			status, err := s.runProxyImageRollback(execCtx, params, reporter)
			if err != nil {
				_ = s.tasks.Fail(execCtx, tc.Task.ID, err)
				return
			}
			_ = s.tasks.Complete(execCtx, tc.Task.ID, "Entrance proxy image rolled back: "+status.Status)
			return
		}
		status, err := s.runProxyImageUpdate(execCtx, params.ServerIDs, reporter, proxyImageUpdateAttempt{})
		if err != nil {
			_ = s.tasks.Fail(execCtx, tc.Task.ID, err)
			return
		}
		_ = s.tasks.Complete(execCtx, tc.Task.ID, "Entrance proxy image updated: "+status.Status)
	}()
	return nil
}

type proxyImageReporter struct {
	service *Service
	ctx     context.Context
	taskID  string
}

func (s *Service) newProxyImageReporter(ctx context.Context, taskID string) proxyImageReporter {
	return proxyImageReporter{service: s, ctx: ctx, taskID: strings.TrimSpace(taskID)}
}

func (r proxyImageReporter) advance(stage, message string) {
	if r.service == nil || r.service.tasks == nil || r.taskID == "" {
		return
	}
	_ = r.service.tasks.Advance(r.ctx, r.taskID, stage, message)
}

func (r proxyImageReporter) logf(format string, args ...any) {
	if r.service == nil || r.service.tasks == nil || r.taskID == "" {
		return
	}
	_ = r.service.tasks.AppendLog(r.ctx, r.taskID, "system", fmt.Sprintf(format, args...))
}

// runProxyImageCheck 解析受管标签的 registry digest，并采集每个网关实际运行
// 的镜像 digest，写入 facility_proxy_image_states 与隐藏应用投影列。
// registry 不可达是软失败：保留旧状态，仅记录错误，绝不伪造"最新"。
func (s *Service) runProxyImageCheck(ctx context.Context, reporter proxyImageReporter) (ProxyImageStatus, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyImageStatus{}, err
	}
	gateways := orderedUniqueStrings(cfg.DeploymentServers)
	reference := supportedProxyImage
	now := time.Now().UTC()
	reporter.advance("check_registry", "Resolving entrance proxy image digest")
	if len(gateways) == 0 {
		reporter.logf("no gateway configured; entrance proxy image check skipped")
		status := ProxyImageStatus{Reference: reference, Status: ProxyImageStatusUnknown, Servers: []ProxyImageServerStatus{}}
		_ = s.updateProxyImageProjection(ctx, status)
		return status, nil
	}
	latestDigest, resolveErr := s.resolveProxyImageDigest(ctx, reference)
	if resolveErr != nil {
		reporter.logf("registry digest resolution failed: %v", resolveErr)
		for _, serverID := range gateways {
			state, stateErr := s.loadProxyImageState(ctx, serverID)
			if stateErr != nil {
				continue
			}
			state.Reference = reference
			state.LastError = resolveErr.Error()
			state.CheckedAt = now
			state.UpdatedAt = now
			if strings.TrimSpace(state.Status) == "" {
				state.Status = ProxyImageStatusUnknown
			}
			_ = s.saveProxyImageState(ctx, state)
		}
		status, statusErr := s.proxyImageStatus(ctx, gateways)
		status.LastError = resolveErr.Error()
		status.CheckedAt = &now
		_ = s.updateProxyImageProjection(ctx, status)
		if statusErr != nil {
			return ProxyImageStatus{}, statusErr
		}
		return status, resolveErr
	}
	reporter.logf("registry digest for %s: %s", reference, latestDigest)
	reporter.advance("check_gateways", "Reading running image digest per gateway")
	for _, serverID := range gateways {
		state, stateErr := s.loadProxyImageState(ctx, serverID)
		if stateErr != nil {
			reporter.logf("%s: load image state failed: %v", serverID, stateErr)
			continue
		}
		if strings.TrimSpace(state.Reference) == "" {
			state.Reference = reference
		}
		state.CheckedAt = now
		state.UpdatedAt = now
		observed, observeErr := s.observedProxyImageDigest(ctx, serverID)
		switch {
		case observeErr != nil:
			state.Status = ProxyImageStatusUnknown
			state.LastError = observeErr.Error()
			reporter.logf("%s: run digest unavailable: %v", serverID, observeErr)
		default:
			state.ObservedDigest = observed
			state.LastError = ""
			// 版本采集是附加观测：无法读取时保持空版本并继续完成 digest 检查，
			// 不把一次 exec 失败升级为整节点检查失败。
			if version, versionErr := s.observedProxyImageVersion(ctx, serverID); versionErr != nil {
				state.ImageVersion = ""
				reporter.logf("%s: run image version unavailable: %v", serverID, versionErr)
			} else {
				state.ImageVersion = version
				if version != "" {
					reporter.logf("%s: running nginx version %s", serverID, version)
				}
			}
			if state.Pinned {
				state.Status = ProxyImageStatusPinned
			} else if sameDigest(observed, latestDigest) {
				state.Status = ProxyImageStatusUpToDate
			} else {
				state.Status = ProxyImageStatusUpdateAvailable
			}
			reporter.logf("%s: running=%s latest=%s status=%s", serverID, shortDigest(observed), shortDigest(latestDigest), state.Status)
		}
		if saveErr := s.saveProxyImageState(ctx, state); saveErr != nil {
			reporter.logf("%s: persist image state failed: %v", serverID, saveErr)
		}
	}
	status, statusErr := s.proxyImageStatus(ctx, gateways)
	if statusErr != nil {
		return ProxyImageStatus{}, statusErr
	}
	status.LatestDigest = latestDigest
	_ = s.updateProxyImageProjection(ctx, status)
	return status, nil
}

// runProxyImageUpdate 串行逐网关滚动：预拉取 → 更新该节点目标 digest →
// 触发该节点协调（identity 变化 → recreate）→ 等待 Job 终态 → 校验运行
// digest。任一节点失败即中止并保留其余节点旧版本。attempt 决定失败退避归属：
// 自动更新按目标 digest 累计，手动更新清零。
func (s *Service) runProxyImageUpdate(ctx context.Context, requested []string, reporter proxyImageReporter, attempt proxyImageUpdateAttempt) (ProxyImageStatus, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyImageStatus{}, err
	}
	gateways := orderedUniqueStrings(cfg.DeploymentServers)
	serverIDs := orderedUniqueStrings(requested)
	if len(serverIDs) == 0 {
		serverIDs = gateways
	} else if err := validateProxyImageGatewaySelection(gateways, serverIDs); err != nil {
		return ProxyImageStatus{}, err
	}
	if len(serverIDs) == 0 {
		reporter.logf("no gateway configured; entrance proxy image update skipped")
		return ProxyImageStatus{Reference: supportedProxyImage, Status: ProxyImageStatusUnknown, Servers: []ProxyImageServerStatus{}}, nil
	}
	var firstErr error
	if err := s.rolloutProxyImageGateways(ctx, serverIDs, proxyImageUpdateInterval(cfg), reporter, func(rolloutCtx context.Context, serverID string) error {
		return s.rolloutProxyImageOnServer(rolloutCtx, serverID, attempt, reporter)
	}); err != nil {
		firstErr = err
	}
	status, statusErr := s.proxyImageStatus(ctx, gateways)
	if firstErr != nil {
		status.LastError = firstErr.Error()
	}
	_ = s.updateProxyImageProjection(ctx, status)
	if statusErr != nil {
		// 等待被取消时状态查询通常也会因同一个 ctx 失败；优先返回滚动中断的
		// 原因，避免把"任务被取消"伪装成状态读取错误。
		if firstErr != nil && ctx.Err() != nil {
			return status, firstErr
		}
		return ProxyImageStatus{}, statusErr
	}
	if firstErr != nil {
		return status, firstErr
	}
	return status, nil
}

// rolloutProxyImageGateways 串行逐网关滚动；相邻网关之间按 interval 等待，便于
// 人工观察/灰度。任一节点失败或等待被 ctx 取消都会立即中止，已完成的节点保留
// 新版本，未开始的节点保持旧版本。rollout 由调用方注入，测试可替换为记录实现。
func (s *Service) rolloutProxyImageGateways(ctx context.Context, serverIDs []string, interval time.Duration, reporter proxyImageReporter, rollout func(context.Context, string) error) error {
	for index, serverID := range serverIDs {
		reporter.advance("rollout", fmt.Sprintf("Rolling entrance proxy image on gateway %d/%d", index+1, len(serverIDs)))
		if err := rollout(ctx, serverID); err != nil {
			reporter.logf("%s: rollout failed: %v", serverID, err)
			return err
		}
		reporter.logf("%s: rollout completed", serverID)
		if index >= len(serverIDs)-1 || interval <= 0 {
			continue
		}
		if err := s.waitBetweenProxyImageGateways(ctx, interval, serverID, serverIDs[index+1], reporter); err != nil {
			return err
		}
	}
	return nil
}

// waitBetweenProxyImageGateways 等待两个相邻网关之间的观察窗口。等待被取消
// 时返回包装后的 ctx 错误（errors.Is(err, context.Canceled) 仍成立），让任务
// 以失败结束而不是把半程滚动当成成功。
func (s *Service) waitBetweenProxyImageGateways(ctx context.Context, interval time.Duration, current, next string, reporter proxyImageReporter) error {
	reporter.advance("rollout_wait", fmt.Sprintf("Waiting %ds before rolling gateway %s", int(interval/time.Second), next))
	reporter.logf("waiting %s between gateway %s and %s", interval, current, next)
	if err := s.waitUpdateInterval(ctx, interval); err != nil {
		reporter.logf("wait between gateway %s and %s interrupted: %v", current, next, err)
		return fmt.Errorf("waiting between gateway %s and %s: %w", current, next, err)
	}
	reporter.logf("finished waiting %s; rolling gateway %s", interval, next)
	return nil
}

// runProxyImageRollback 串行把目标网关固定并回滚到旧 digest：预拉取 pinned
// 引用 → 写入 target/previous → 触发该节点 recreate → 校验运行 digest。开始时
// 先固化 pinned，确保自动更新立即暂停（即使回滚中途失败）。
func (s *Service) runProxyImageRollback(ctx context.Context, params proxyImageTaskParams, reporter proxyImageReporter) (ProxyImageStatus, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ProxyImageStatus{}, err
	}
	gateways := orderedUniqueStrings(cfg.DeploymentServers)
	targets, err := s.planProxyImageRollback(ctx, gateways, params.ServerIDs, normalizeDigest(params.Digest))
	if err != nil {
		return ProxyImageStatus{}, err
	}
	status, statusErr := s.proxyImageStatus(ctx, gateways)
	if len(targets) == 0 {
		reporter.logf("no rollback target; entrance proxy image rollback skipped")
		if statusErr != nil {
			return ProxyImageStatus{}, statusErr
		}
		return status, nil
	}
	// 先固化 pinned，避免回滚执行期间自动更新介入。
	for _, target := range targets {
		if err := s.pinProxyImageServer(ctx, target.ServerID); err != nil {
			reporter.logf("%s: pin for rollback failed: %v", target.ServerID, err)
			return status, err
		}
	}
	var firstErr error
	for index, target := range targets {
		reporter.advance("rollback", fmt.Sprintf("Rolling back entrance proxy image on gateway %d/%d", index+1, len(targets)))
		if err := s.rollbackProxyImageOnServer(ctx, target, reporter); err != nil {
			firstErr = err
			reporter.logf("%s: rollback failed: %v", target.ServerID, err)
			break
		}
		reporter.logf("%s: rollback to %s completed", target.ServerID, shortDigest(target.Digest))
	}
	status, statusErr = s.proxyImageStatus(ctx, gateways)
	if firstErr != nil {
		status.LastError = firstErr.Error()
	}
	_ = s.updateProxyImageProjection(ctx, status)
	if statusErr != nil {
		return ProxyImageStatus{}, statusErr
	}
	if firstErr != nil {
		return status, firstErr
	}
	return status, nil
}

// pinProxyImageServer 把单个网关标记为 pinned（自动更新暂停）。
func (s *Service) pinProxyImageServer(ctx context.Context, serverID string) error {
	state, err := s.loadProxyImageState(ctx, serverID)
	if err != nil {
		return err
	}
	if state.Pinned {
		return nil
	}
	state.Pinned = true
	return s.saveProxyImageState(ctx, state)
}

// rollbackProxyImageOnServer 执行单网关回滚：拉取 digest 固定引用 → 写目标 →
// recreate → 校验。成功后 previous=回滚前的 target、observed/target=回滚目标，
// status=pinned；失败保留 pinned 与失败信息，允许重试或恢复自动更新。
func (s *Service) rollbackProxyImageOnServer(ctx context.Context, target proxyImageRollbackTarget, reporter proxyImageReporter) error {
	state, err := s.loadProxyImageState(ctx, target.ServerID)
	if err != nil {
		return err
	}
	targetDigest := normalizeDigest(target.Digest)
	if targetDigest == "" {
		return panelerr.Validation("facility_proxy_image_digest_invalid", "rollback digest must be a sha256 image digest")
	}
	oldTarget := normalizeDigest(state.TargetDigest)
	pinnedReference := proxyImageDigestReference(firstNonEmpty(state.Reference, supportedProxyImage), targetDigest)
	endpoint, err := s.agentEndpoint(ctx, target.ServerID)
	if err != nil {
		return s.failProxyImageRollback(ctx, state, err)
	}
	reporter.logf("%s: pulling pinned image %s", target.ServerID, pinnedReference)
	pullErr := s.executeOnServer(ctx, target.ServerID, func(queueCtx context.Context) error {
		pullCtx, cancel := context.WithTimeout(queueCtx, proxyImagePullTimeout)
		defer cancel()
		return s.agent.DockerImagePull(pullCtx, endpoint, pinnedReference)
	})
	if pullErr != nil {
		return s.failProxyImageRollback(ctx, state, pullErr)
	}
	now := time.Now().UTC()
	// previous 暂时记录回滚目标：失败重试时默认仍解析到同一 digest；成功后
	// 改写成回滚前的 target（保留可追溯的版本关系）。
	state.Reference = pinnedReference
	state.PreviousDigest = targetDigest
	state.TargetDigest = targetDigest
	state.Pinned = true
	state.Status = ProxyImageStatusUpdating
	state.LastError = ""
	state.CheckedAt = now
	resetProxyImageFailureState(&state)
	if err := s.saveProxyImageState(ctx, state); err != nil {
		return err
	}
	reporter.logf("%s: target digest pinned to %s; planning recreate", target.ServerID, shortDigest(targetDigest))
	if err := s.triggerReverseProxyReconcile(ctx, "facility_app", nil, []string{target.ServerID}); err != nil {
		return s.failProxyImageRollback(ctx, state, err)
	}
	expectedHash := s.expectedProxySpecHash(ctx, target.ServerID)
	if err := s.waitForProxyRollout(ctx, target.ServerID, expectedHash, targetDigest); err != nil {
		return s.failProxyImageRollback(ctx, state, err)
	}
	observed, err := s.observedProxyImageDigest(ctx, target.ServerID)
	if err != nil {
		return s.failProxyImageRollback(ctx, state, err)
	}
	state.ObservedDigest = observed
	state.CheckedAt = time.Now().UTC()
	if !sameDigest(observed, targetDigest) {
		return s.failProxyImageRollback(ctx, state, panelerr.Validation("facility_proxy_image_rollback_failed", "gateway is not running the rollback entrance proxy image"))
	}
	state.PreviousDigest = oldTarget
	state.Status = ProxyImageStatusPinned
	state.LastError = ""
	resetProxyImageFailureState(&state)
	return s.saveProxyImageState(ctx, state)
}

// failProxyImageRollback 记录回滚失败并保持 pinned（自动更新仍然暂停）。
func (s *Service) failProxyImageRollback(ctx context.Context, state models.FacilityProxyImageState, err error) error {
	state.Status = ProxyImageStatusFailed
	state.Pinned = true
	state.LastError = err.Error()
	state.CheckedAt = time.Now().UTC()
	state.UpdatedAt = state.CheckedAt
	_ = s.saveProxyImageState(context.WithoutCancel(ctx), state)
	return err
}

// planProxyImageRollback 解析每个目标网关的回滚目标（显式 digest 或该节点的
// previous_digest），并跳过无目标或已固定成功的节点；不写库，供 API 幂等判定
// 与任务执行共用。
func (s *Service) planProxyImageRollback(ctx context.Context, gateways, requested []string, explicitDigest string) ([]proxyImageRollbackTarget, error) {
	serverIDs := orderedUniqueStrings(requested)
	if len(serverIDs) == 0 {
		serverIDs = gateways
	} else if err := validateProxyImageGatewaySelection(gateways, serverIDs); err != nil {
		return nil, err
	}
	targets := make([]proxyImageRollbackTarget, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		state, err := s.loadProxyImageState(ctx, serverID)
		if err != nil {
			return nil, err
		}
		// 已固定成功（pinned）的节点对回滚请求是幂等空操作；failed 允许重试。
		if state.Pinned && state.Status != ProxyImageStatusFailed {
			continue
		}
		digest := explicitDigest
		if digest == "" {
			digest = normalizeDigest(state.PreviousDigest)
		}
		if digest == "" {
			continue
		}
		targets = append(targets, proxyImageRollbackTarget{ServerID: serverID, Digest: digest})
	}
	return targets, nil
}

func (s *Service) rolloutProxyImageOnServer(ctx context.Context, serverID string, attempt proxyImageUpdateAttempt, reporter proxyImageReporter) error {
	state, err := s.loadProxyImageState(ctx, serverID)
	if err != nil {
		return err
	}
	// 自动更新必须遵守 pinned 与失败退避；目标 digest 变化后旧退避不再适用。
	if attempt.Auto {
		if state.Pinned {
			reporter.logf("%s: automatic update skipped: gateway is pinned", serverID)
			return nil
		}
		if reason := proxyImageAutoUpdatePauseReason(state, attempt.ExpectedDigest); reason != "" {
			reporter.logf("%s: automatic update skipped: %s", serverID, reason)
			return nil
		}
		if state.AutoUpdateFailures > 0 && !sameDigest(state.FailedDigest, attempt.ExpectedDigest) {
			resetProxyImageFailureState(&state)
		}
	} else if state.AutoUpdateFailures > 0 || strings.TrimSpace(state.FailedDigest) != "" {
		// 手动更新是显式介入：清零自动更新失败退避。
		resetProxyImageFailureState(&state)
	}
	attemptDigest := normalizeDigest(attempt.ExpectedDigest)
	fail := func(state models.FacilityProxyImageState, err error) error {
		state.Status = ProxyImageStatusFailed
		state.LastError = err.Error()
		state.CheckedAt = time.Now().UTC()
		state.UpdatedAt = state.CheckedAt
		if attempt.Auto {
			recordProxyImageAutoUpdateFailure(&state, firstNonEmpty(attemptDigest, normalizeDigest(state.TargetDigest)))
		}
		_ = s.saveProxyImageState(context.WithoutCancel(ctx), state)
		return err
	}
	reference := strings.TrimSpace(state.Reference)
	if reference == "" || digestFromReference(reference) != "" {
		// 回滚会把引用固定在 digest 上；普通更新/恢复自动更新回到受管标签线。
		reference = supportedProxyImage
	}
	endpoint, err := s.agentEndpoint(ctx, serverID)
	if err != nil {
		return fail(state, err)
	}
	localDigest := ""
	reporter.logf("%s: pulling %s", serverID, reference)
	pullErr := s.executeOnServer(ctx, serverID, func(queueCtx context.Context) error {
		pullCtx, cancel := context.WithTimeout(queueCtx, proxyImagePullTimeout)
		defer cancel()
		if err := s.agent.DockerImagePull(pullCtx, endpoint, reference); err != nil {
			return err
		}
		imagesCtx, imagesCancel := context.WithTimeout(queueCtx, proxyImageAgentTimeout)
		defer imagesCancel()
		images, err := s.agent.DockerImages(imagesCtx, endpoint)
		if err != nil {
			return err
		}
		localDigest = localTagImageDigest(images, reference)
		if localDigest == "" {
			return panelerr.Validation("facility_proxy_image_unavailable", "pulled image digest could not be read from the gateway")
		}
		return nil
	})
	if pullErr != nil {
		return fail(state, pullErr)
	}
	attemptDigest = normalizeDigest(localDigest)
	observed, observeErr := s.observedProxyImageDigest(ctx, serverID)
	if observeErr == nil && sameDigest(observed, localDigest) {
		state.Reference = reference
		state.TargetDigest = localDigest
		state.ObservedDigest = observed
		state.Status = ProxyImageStatusUpToDate
		state.LastError = ""
		state.CheckedAt = time.Now().UTC()
		resetProxyImageFailureState(&state)
		_ = s.saveProxyImageState(ctx, state)
		reporter.logf("%s: already running %s", serverID, shortDigest(localDigest))
		return nil
	}
	state.Reference = reference
	state.PreviousDigest = state.TargetDigest
	state.TargetDigest = localDigest
	state.Status = ProxyImageStatusUpdating
	state.LastError = ""
	state.CheckedAt = time.Now().UTC()
	if err := s.saveProxyImageState(ctx, state); err != nil {
		return err
	}
	reporter.logf("%s: target digest set to %s; planning recreate", serverID, shortDigest(localDigest))
	if err := s.triggerReverseProxyReconcile(ctx, "facility_app", nil, []string{serverID}); err != nil {
		return fail(state, err)
	}
	expectedHash := s.expectedProxySpecHash(ctx, serverID)
	if err := s.waitForProxyRollout(ctx, serverID, expectedHash, localDigest); err != nil {
		return fail(state, err)
	}
	observed, err = s.observedProxyImageDigest(ctx, serverID)
	if err != nil {
		return fail(state, err)
	}
	state.ObservedDigest = observed
	state.CheckedAt = time.Now().UTC()
	if sameDigest(observed, localDigest) {
		state.Status = ProxyImageStatusUpToDate
		state.LastError = ""
	} else {
		return fail(state, panelerr.Validation("facility_proxy_image_rollout_failed", "gateway is not running the target entrance proxy image"))
	}
	// 成功一次即清零失败退避（自动与手动一致）。
	resetProxyImageFailureState(&state)
	return s.saveProxyImageState(ctx, state)
}

// resetProxyImageFailureState 清零自动更新失败退避计数与失败 digest。
func resetProxyImageFailureState(state *models.FacilityProxyImageState) {
	if state == nil {
		return
	}
	state.AutoUpdateFailures = 0
	state.FailedDigest = ""
}

// recordProxyImageAutoUpdateFailure 记录一次自动更新失败：同一 digest 连续失败
// 累加计数，目标变化后从 1 重新计数。
func recordProxyImageAutoUpdateFailure(state *models.FacilityProxyImageState, digest string) {
	if state == nil {
		return
	}
	digest = normalizeDigest(digest)
	if digest == "" {
		state.AutoUpdateFailures++
		return
	}
	if sameDigest(state.FailedDigest, digest) {
		state.AutoUpdateFailures++
		return
	}
	state.FailedDigest = digest
	state.AutoUpdateFailures = 1
}

// proxyImageAutoUpdatePauseReason 返回自动更新因连续失败退避而必须跳过该节点的
// 原因；空字符串表示可以继续。目标 digest 与失败 digest 不同说明目标已变化，
// 旧退避不再适用。
func proxyImageAutoUpdatePauseReason(state models.FacilityProxyImageState, targetDigest string) string {
	if state.AutoUpdateFailures < proxyImageAutoUpdateFailureLimit {
		return ""
	}
	failedDigest := normalizeDigest(state.FailedDigest)
	if failedDigest == "" {
		return ""
	}
	targetDigest = normalizeDigest(targetDigest)
	if targetDigest != "" && !sameDigest(failedDigest, targetDigest) {
		return ""
	}
	return fmt.Sprintf("automatic update paused after %d consecutive failures for digest %s", state.AutoUpdateFailures, shortDigest(failedDigest))
}

// proxyImageDigestReference 由标签引用与 digest 构造 docker 可拉取的固定引用
// （如 nginx@sha256:...）。回滚必须固定引用，否则标签会重新指向最新镜像。
func proxyImageDigestReference(reference, digest string) string {
	repo := strings.TrimSpace(reference)
	if index := strings.LastIndex(repo, "@"); index >= 0 {
		repo = repo[:index]
	}
	if colon := strings.LastIndex(repo, ":"); colon > strings.LastIndex(repo, "/") {
		repo = repo[:colon]
	}
	repo = strings.TrimSuffix(strings.TrimSpace(repo), "/")
	if repo == "" {
		repo = "nginx"
	}
	return repo + "@" + normalizeDigest(digest)
}

// validateProxyImageGatewaySelection 校验请求中的 serverId 都是入口代理网关。
func validateProxyImageGatewaySelection(gateways, serverIDs []string) error {
	allowed := stringSetValues(gateways)
	for _, serverID := range serverIDs {
		if _, ok := allowed[serverID]; !ok {
			return panelerr.WithDetails(panelerr.Validation("facility_proxy_image_server_invalid", "server is not an entrance proxy gateway"), map[string]any{"serverId": serverID})
		}
	}
	return nil
}

// gatewayIDs 提取分网关状态的 serverId 列表。
func gatewayIDs(servers []ProxyImageServerStatus) []string {
	out := make([]string, 0, len(servers))
	for _, item := range servers {
		out = append(out, item.ServerID)
	}
	return out
}

// recomputeProxyImageStatusProjection 在注入新的 latest digest 后重算聚合
// updateAvailable 与整体 status（供恢复自动更新后刷新投影）。
func recomputeProxyImageStatusProjection(status *ProxyImageStatus, latestDigest string) {
	if status == nil {
		return
	}
	statuses := make([]string, 0, len(status.Servers))
	for index := range status.Servers {
		item := &status.Servers[index]
		if strings.TrimSpace(item.TargetDigest) != "" && strings.TrimSpace(item.ObservedDigest) != "" && !sameDigest(item.TargetDigest, item.ObservedDigest) {
			status.UpdateAvailable = true
		}
		statuses = append(statuses, item.Status)
	}
	if latestDigest != "" && !sameDigest(status.TargetDigest, latestDigest) {
		status.UpdateAvailable = true
	}
	status.Status = aggregateProxyImageStatus(statuses, status.UpdateAvailable, status.Pinned)
}

func (s *Service) agentEndpoint(ctx context.Context, serverID string) (string, error) {
	if s == nil || s.servers == nil {
		return "", panelerr.Validation("facility_proxy_image_unavailable", "server registry is unavailable")
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	if endpoint == "" || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return "", panelerr.WithDetails(panelerr.Validation("facility_proxy_image_unavailable", "gateway Agent is unavailable or incompatible"), map[string]any{"serverId": serverID})
	}
	return endpoint, nil
}

// observedProxyImageDigest 采集网关实际运行容器的镜像 digest：
// DockerContainers 定位容器（observed id / panel.application.* 标签 / 容器名），
// 再由容器 ImageID 回查 DockerImages 的 RepoDigests。
func (s *Service) observedProxyImageDigest(ctx context.Context, serverID string) (string, error) {
	endpoint, err := s.agentEndpoint(ctx, serverID)
	if err != nil {
		return "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, proxyImageAgentTimeout)
	defer cancel()
	containers, err := s.agent.DockerContainers(callCtx, endpoint)
	if err != nil {
		return "", err
	}
	container := proxyContainerForServer(containers, s.observedProxyContainerID(ctx, serverID))
	if container == nil {
		return "", panelerr.WithDetails(panelerr.Validation("facility_proxy_image_unavailable", "entrance proxy container was not found on the gateway"), map[string]any{"serverId": serverID})
	}
	if digest := digestFromReference(container.Image); digest != "" {
		return digest, nil
	}
	images, err := s.agent.DockerImages(callCtx, endpoint)
	if err != nil {
		return "", err
	}
	return imageDigestForContainer(container, images), nil
}

// observedProxyImageVersion 采集网关运行中入口代理的实际 nginx 版本：通过
// RuntimeStatus 请求 Agent 在运行容器内执行 `nginx -v`。空版本表示旧 Agent、
// 容器未运行或命令失败；调用方按“未知”展示，不伪造版本。
func (s *Service) observedProxyImageVersion(ctx context.Context, serverID string) (string, error) {
	if s == nil || s.agent == nil {
		return "", panelerr.Validation("facility_proxy_image_unavailable", "gateway Agent client is unavailable")
	}
	endpoint, err := s.agentEndpoint(ctx, serverID)
	if err != nil {
		return "", err
	}
	callCtx, cancel := context.WithTimeout(ctx, proxyImageAgentTimeout)
	defer cancel()
	status, err := s.agent.RuntimeStatusWithVersion(callCtx, endpoint, instanceID(serverID), proxyContainerName, proxyImageVersionCommand)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(status.ImageVersion), nil
}

func (s *Service) observedProxyContainerID(ctx context.Context, serverID string) string {
	if s == nil || s.db == nil {
		return ""
	}
	var containerID string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(observed_container_id,'') FROM application_instances WHERE application_id=? AND server_id=?`, proxyApplicationID, serverID).Scan(&containerID); err != nil {
		return ""
	}
	return strings.TrimSpace(containerID)
}

func proxyContainerForServer(containers []agentcontract.DockerContainer, observedContainerID string) *agentcontract.DockerContainer {
	observedContainerID = strings.TrimSpace(observedContainerID)
	if observedContainerID != "" {
		for index := range containers {
			if sameImageID(containers[index].ID, observedContainerID) {
				return &containers[index]
			}
		}
	}
	for index := range containers {
		labels := containers[index].Labels
		if labels["panel.application.managed"] == "true" && labels["panel.application.id"] == proxyApplicationID {
			return &containers[index]
		}
	}
	for index := range containers {
		for _, name := range containers[index].Names {
			if strings.TrimPrefix(name, "/") == proxyContainerName {
				return &containers[index]
			}
		}
	}
	return nil
}

func imageDigestForContainer(container *agentcontract.DockerContainer, images []agentcontract.DockerImage) string {
	if container == nil {
		return ""
	}
	if digest := digestFromReference(container.Image); digest != "" {
		return digest
	}
	if strings.TrimSpace(container.ImageID) == "" {
		return ""
	}
	for _, image := range images {
		if !sameImageID(image.ID, container.ImageID) {
			continue
		}
		if digest := firstRepoDigest(image.RepoDigests); digest != "" {
			return digest
		}
	}
	return ""
}

// localTagImageDigest 返回本地标签当前指向镜像的内容 digest（RepoDigests）。
func localTagImageDigest(images []agentcontract.DockerImage, reference string) string {
	target := normalizeImageReference(reference)
	for _, image := range images {
		for _, tag := range image.RepoTags {
			if normalizeImageReference(tag) != target {
				continue
			}
			if digest := firstRepoDigest(image.RepoDigests); digest != "" {
				return digest
			}
		}
	}
	return ""
}

func firstRepoDigest(repoDigests []string) string {
	for _, entry := range repoDigests {
		if digest := digestFromReference(entry); digest != "" {
			return digest
		}
	}
	return ""
}

// digestFromReference 从 "repo@sha256:..." / "repo:tag@sha256:..." 中取 digest。
func digestFromReference(value string) string {
	value = strings.TrimSpace(value)
	index := strings.LastIndex(value, "@")
	if index < 0 || index == len(value)-1 {
		return ""
	}
	return strings.TrimSpace(value[index+1:])
}

func normalizeImageReference(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "docker.io/")
	value = strings.TrimPrefix(value, "index.docker.io/")
	return value
}

func sameImageID(left, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	return strings.TrimPrefix(left, "sha256:") == strings.TrimPrefix(right, "sha256:")
}

func normalizeDigest(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if digest := digestFromReference(value); digest != "" {
		return digest
	}
	return value
}

func sameDigest(left, right string) bool {
	left, right = normalizeDigest(left), normalizeDigest(right)
	return left != "" && left == right
}

func shortDigest(value string) string {
	value = normalizeDigest(value)
	if value == "" {
		return "-"
	}
	if len(value) > 19 {
		return value[:19]
	}
	return value
}

func (s *Service) resolveProxyImageDigest(ctx context.Context, reference string) (string, error) {
	if s == nil || s.imageResolver == nil {
		return "", panelerr.Validation("facility_proxy_image_unavailable", "image registry resolver is unavailable")
	}
	resolveCtx, cancel := context.WithTimeout(ctx, proxyImageRegistryTimeout)
	defer cancel()
	resolved, err := s.imageResolver.Resolve(resolveCtx, reference)
	if err != nil {
		return "", err
	}
	digest := strings.TrimSpace(resolved.Digest)
	if digest == "" {
		return "", panelerr.BadGateway("facility_proxy_image_unavailable", "image registry did not return a digest")
	}
	return digest, nil
}

func (s *Service) expectedProxySpecHash(ctx context.Context, serverID string) string {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return ""
	}
	routes, err := s.routesByServer(ctx, []string{serverID})
	if err != nil {
		return ""
	}
	certificates, err := s.reverseProxyCertificates(ctx)
	if err != nil {
		return ""
	}
	spec, err := s.proxySpec(ctx, serverID, cfg, routes[serverID], certificates)
	if err != nil {
		return ""
	}
	return spec.SpecHash
}

func (s *Service) waitForProxyRollout(ctx context.Context, serverID, expectedSpecHash, targetDigest string) error {
	deadline := time.Now().Add(proxyImageRolloutTimeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if state, observedHash, err := s.proxyInstanceState(ctx, serverID); err == nil {
			if state == "running" && (expectedSpecHash == "" || observedHash == expectedSpecHash) {
				return nil
			}
		}
		if status, err := s.latestProxyDeployStatus(ctx, serverID); err == nil {
			switch status {
			case "failed", "cancelled", "blocked":
				return panelerr.Validation("facility_proxy_image_rollout_failed", "entrance proxy recreate job ended with status "+status)
			}
		}
		if time.Now().After(deadline) {
			if digest, err := s.observedProxyImageDigest(ctx, serverID); err == nil && sameDigest(digest, targetDigest) {
				return nil
			}
			return panelerr.Validation("facility_proxy_image_rollout_failed", "timed out waiting for the entrance proxy container to run the target image")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(proxyImagePollInterval):
		}
	}
}

func (s *Service) proxyInstanceState(ctx context.Context, serverID string) (string, string, error) {
	if s == nil || s.db == nil {
		return "", "", sql.ErrNoRows
	}
	var observedState, observedHash string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(observed_state,''),COALESCE(observed_spec_hash,'') FROM application_instances WHERE application_id=? AND server_id=?`, proxyApplicationID, serverID).Scan(&observedState, &observedHash)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(observedState), strings.TrimSpace(observedHash), nil
}

func (s *Service) latestProxyDeployStatus(ctx context.Context, serverID string) (string, error) {
	if s == nil || s.db == nil {
		return "", sql.ErrNoRows
	}
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(status,'') FROM event_runs WHERE type='application_deploy' AND application_id=? AND server_id=? ORDER BY created_at DESC, id DESC LIMIT 1`, proxyApplicationID, serverID).Scan(&status)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(status), nil
}

func (s *Service) proxyImageStatus(ctx context.Context, gateways []string) (ProxyImageStatus, error) {
	gateways = orderedUniqueStrings(gateways)
	status := ProxyImageStatus{
		Reference: supportedProxyImage,
		Status:    ProxyImageStatusUnknown,
		Servers:   make([]ProxyImageServerStatus, 0, len(gateways)),
	}
	if s == nil || s.db == nil {
		return status, nil
	}
	states, err := s.loadProxyImageStates(ctx, gateways)
	if err != nil {
		return status, err
	}
	latestDigest, projectionChecked := s.proxyImageProjection(ctx)
	if latestDigest != "" {
		status.LatestDigest = latestDigest
	}
	if projectionChecked != nil && !projectionChecked.IsZero() {
		status.CheckedAt = projectionChecked
	}
	statuses := make([]string, 0, len(gateways))
	var latestChecked, latestUpdated time.Time
	for _, serverID := range gateways {
		row, ok := states[serverID]
		if !ok {
			row = models.FacilityProxyImageState{ServerID: serverID, Reference: supportedProxyImage, Status: ProxyImageStatusUnknown}
		}
		item := ProxyImageServerStatus{
			ServerID:       serverID,
			Status:         firstNonEmpty(row.Status, ProxyImageStatusUnknown),
			Pinned:         row.Pinned,
			ObservedDigest: row.ObservedDigest,
			TargetDigest:   row.TargetDigest,
			PreviousDigest: row.PreviousDigest,
			ImageVersion:   row.ImageVersion,
			LastError:      row.LastError,
		}
		// 聚合版本取第一个非空网关值作代表；各网关的精确值仍在 servers 中。
		if status.ImageVersion == "" {
			status.ImageVersion = row.ImageVersion
		}
		item.AutoUpdateFailures = row.AutoUpdateFailures
		item.FailedDigest = row.FailedDigest
		if reason := proxyImageAutoUpdatePauseReason(row, latestDigest); reason != "" {
			item.AutoUpdatePaused = true
			item.AutoUpdateSkipReason = reason
			status.AutoUpdatePaused = true
		}
		if !row.CheckedAt.IsZero() {
			checked := row.CheckedAt
			item.CheckedAt = &checked
			if checked.After(latestChecked) {
				latestChecked = checked
			}
		}
		if !row.UpdatedAt.IsZero() && row.UpdatedAt.After(latestUpdated) {
			latestUpdated = row.UpdatedAt
		}
		if reference := strings.TrimSpace(row.Reference); reference != "" {
			status.Reference = reference
		}
		if strings.TrimSpace(row.TargetDigest) != "" && status.TargetDigest == "" {
			status.TargetDigest = row.TargetDigest
		}
		if strings.TrimSpace(row.PreviousDigest) != "" && status.PreviousDigest == "" {
			status.PreviousDigest = row.PreviousDigest
		}
		if row.Pinned {
			status.Pinned = true
		}
		if strings.TrimSpace(row.LastError) != "" && status.LastError == "" {
			status.LastError = row.LastError
		}
		if strings.TrimSpace(row.TargetDigest) != "" && strings.TrimSpace(row.ObservedDigest) != "" && !sameDigest(row.TargetDigest, row.ObservedDigest) {
			status.UpdateAvailable = true
		}
		status.Servers = append(status.Servers, item)
		statuses = append(statuses, item.Status)
	}
	if status.LatestDigest != "" && !sameDigest(status.TargetDigest, status.LatestDigest) {
		status.UpdateAvailable = true
	}
	status.Status = aggregateProxyImageStatus(statuses, status.UpdateAvailable, status.Pinned)
	if !latestChecked.IsZero() {
		status.CheckedAt = &latestChecked
	}
	if !latestUpdated.IsZero() {
		status.UpdatedAt = &latestUpdated
	}
	return status, nil
}

func aggregateProxyImageStatus(statuses []string, updateAvailable, pinned bool) string {
	hasFailed, hasUpdating, hasUnknown := false, false, false
	for _, item := range statuses {
		switch item {
		case ProxyImageStatusFailed:
			hasFailed = true
		case ProxyImageStatusUpdating:
			hasUpdating = true
		case ProxyImageStatusUpdateAvailable:
			updateAvailable = true
		case ProxyImageStatusPinned:
			pinned = true
		case ProxyImageStatusUpToDate:
		default:
			hasUnknown = true
		}
	}
	switch {
	case hasFailed:
		return ProxyImageStatusFailed
	case hasUpdating:
		return ProxyImageStatusUpdating
	case updateAvailable:
		return ProxyImageStatusUpdateAvailable
	case pinned:
		return ProxyImageStatusPinned
	case len(statuses) > 0 && !hasUnknown:
		return ProxyImageStatusUpToDate
	case hasUnknown:
		return ProxyImageStatusUnknown
	default:
		return ProxyImageStatusUnknown
	}
}

func (s *Service) loadProxyImageStates(ctx context.Context, serverIDs []string) (map[string]models.FacilityProxyImageState, error) {
	out := map[string]models.FacilityProxyImageState{}
	if s == nil || s.db == nil || len(serverIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(serverIDs))
	placeholders := make([]string, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		args = append(args, serverID)
		placeholders = append(placeholders, "?")
	}
	var rows []models.FacilityProxyImageState
	if err := orm.New(s.db).From("facility_proxy_image_states").Where("server_id IN ("+strings.Join(placeholders, ",")+")", args...).All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ServerID] = row
	}
	return out, nil
}

func (s *Service) loadProxyImageState(ctx context.Context, serverID string) (models.FacilityProxyImageState, error) {
	state := models.FacilityProxyImageState{ServerID: serverID, Reference: supportedProxyImage, Status: ProxyImageStatusUnknown}
	if s == nil || s.db == nil || strings.TrimSpace(serverID) == "" {
		return state, nil
	}
	var row models.FacilityProxyImageState
	err := orm.New(s.db).From("facility_proxy_image_states").Where("server_id=?", serverID).First(ctx, &row)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if strings.TrimSpace(row.Reference) == "" {
		row.Reference = supportedProxyImage
	}
	if strings.TrimSpace(row.Status) == "" {
		row.Status = ProxyImageStatusUnknown
	}
	return row, nil
}

func (s *Service) saveProxyImageState(ctx context.Context, state models.FacilityProxyImageState) error {
	if s == nil || s.db == nil || strings.TrimSpace(state.ServerID) == "" {
		return nil
	}
	now := time.Now().UTC()
	if state.CheckedAt.IsZero() {
		state.CheckedAt = now
	}
	state.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO facility_proxy_image_states(server_id,reference,target_digest,observed_digest,previous_digest,status,pinned,last_error,checked_at,updated_at,auto_update_failures,failed_digest,image_version)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(server_id) DO UPDATE SET reference=excluded.reference,target_digest=excluded.target_digest,observed_digest=excluded.observed_digest,previous_digest=excluded.previous_digest,status=excluded.status,pinned=excluded.pinned,last_error=excluded.last_error,checked_at=excluded.checked_at,updated_at=excluded.updated_at,auto_update_failures=excluded.auto_update_failures,failed_digest=excluded.failed_digest,image_version=excluded.image_version`,
		state.ServerID, state.Reference, state.TargetDigest, state.ObservedDigest, state.PreviousDigest, state.Status, boolInt(state.Pinned), state.LastError, formatTime(state.CheckedAt), formatTime(state.UpdatedAt), state.AutoUpdateFailures, state.FailedDigest, state.ImageVersion)
	return err
}

// updateProxyImageProjection 把聚合状态写回隐藏应用行，供应用列表/详情使用。
func (s *Service) updateProxyImageProjection(ctx context.Context, status ProxyImageStatus) error {
	if s == nil || s.db == nil {
		return nil
	}
	imageDigest := status.TargetDigest
	for _, item := range status.Servers {
		if strings.TrimSpace(item.ObservedDigest) != "" {
			imageDigest = item.ObservedDigest
			break
		}
	}
	var checkedAt any
	if status.CheckedAt != nil && !status.CheckedAt.IsZero() {
		checkedAt = formatTime(*status.CheckedAt)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE applications SET image_reference=?,image_digest=?,image_latest_digest=?,image_checked_at=?,image_update_available=?,image_last_error=?,updated_at=? WHERE id=?`,
		status.Reference, imageDigest, status.LatestDigest, checkedAt, boolInt(status.UpdateAvailable), status.LastError, formatTime(time.Now().UTC()), proxyApplicationID)
	return err
}

func (s *Service) proxyImageProjection(ctx context.Context) (string, *time.Time) {
	if s == nil || s.db == nil {
		return "", nil
	}
	var latest string
	var checked sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(image_latest_digest,''),image_checked_at FROM applications WHERE id=?`, proxyApplicationID).Scan(&latest, &checked); err != nil {
		return "", nil
	}
	var checkedAt *time.Time
	if checked.Valid {
		if parsed := parseTime(checked.String); !parsed.IsZero() {
			checkedAt = &parsed
		}
	}
	return strings.TrimSpace(latest), checkedAt
}

// orderedUniqueStrings 去重但保留输入顺序（滚动顺序即网关配置顺序）。
func orderedUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
