package autoupdate

import (
	"context"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/servers"
	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// runSummary 汇总一次自动更新的结果，用于任务摘要与失败判定。
type runSummary struct {
	appsConsidered int
	appsDisabled   int
	appsUpdated    int
	serversTotal   int
	serversUpdated int
	failures       []string
}

// RunAutoUpdateTask 是 auto_update_run 的执行入口（TASK-PER-003）：
//  1. 执行时重新读取设置（以最新快照为准；两开关都关直接完成 disabled）。
//  2. 有界刷新应用镜像 digest、节点镜像与软件包缓存（最佳努力，超时或部分
//     失败只记日志）。应用镜像刷新不可省略，否则应用的 ImageUpdateAvailable
//     永远停留在上次检查结果，应用阶段筛不到任何更新。
//  3. 应用更新阶段（appsEnabled）：用户应用镜像（按对象开关过滤）+ 入口代理镜像
//     逐节点滚动；入口代理失败不阻断后续软件包阶段。
//  4. 软件包更新阶段（packagesEnabled）：逐服务器串行执行 security/all。
//  5. 汇总：任一阶段/服务器失败 → 任务失败（允许重试）；否则完成。
func (s *Service) RunAutoUpdateTask(tc tasks.TaskContext) error {
	ctx, task := tc.Context, tc.Task
	if s.tasks == nil {
		return fmt.Errorf("automatic update task service is unavailable")
	}
	if s.settings == nil {
		return fmt.Errorf("automatic update settings store is unavailable")
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return err
	}
	runAt := s.now()
	rt, manual := s.resolveRunSettings(task)
	if manual {
		_ = s.tasks.AppendLog(ctx, task.ID, "system", "Automatic updates started manually (trigger: manual)")
	} else {
		// 只有调度触发的运行才写 lastAutoRunAt（当日去重依据）；手动触发只写
		// lastRunAt（展示），否则用户手动测试一次会吞掉当天的计划运行。
		s.recordAutoRun(ctx, task.ID, runAt)
	}
	if !rt.AppsEnabled && !rt.PackagesEnabled {
		s.recordLastRun(ctx, task.ID, runAt, RunStatusDisabled, "Automatic updates are disabled; both switches are off")
		_ = s.tasks.AppendLog(ctx, task.ID, "system", "Automatic updates are disabled")
		return s.tasks.Complete(ctx, task.ID, "Automatic updates are disabled")
	}
	// 开始即写运行态：即使进程在长任务中途重启，当日去重与"最近一次运行"
	// 也仍然可见（任务历史只保留 24 小时）。
	s.recordLastRun(ctx, task.ID, runAt, RunStatusRunning, "")
	summary := s.runPhases(ctx, task, rt)
	finalStatus := RunStatusCompleted
	text := summaryText(summary, rt)
	if len(summary.failures) > 0 {
		finalStatus = RunStatusFailed
		text = failureSummaryText(summary)
	}
	s.recordLastRun(ctx, task.ID, runAt, finalStatus, text)
	if len(summary.failures) > 0 {
		err := fmt.Errorf("automatic updates finished with %d failure(s): %s", len(summary.failures), strings.Join(summary.failures, "; "))
		_ = s.tasks.Fail(ctx, task.ID, err)
		return err
	}
	return s.tasks.Complete(ctx, task.ID, text)
}

// resolveRunSettings 解析本次运行使用的有效设置。调度触发以执行时的 DB 快照
// 为准（设置热更新，§3.3）；手动触发使用创建任务时持久化在 ParamsJSON 中的
// 有效设置，使「立即运行一次」的可选临时覆盖只作用于该次运行，且不落库。
func (s *Service) resolveRunSettings(task tasks.Task) (settings.RuntimeAutoUpdateSettings, bool) {
	rt := s.settings.Runtime().AutoUpdate
	params, ok := parseParams(task.ParamsJSON)
	if !ok || params.Trigger != triggerTypeManual {
		return rt, false
	}
	rt.AppsEnabled = params.AppsEnabled
	rt.PackagesEnabled = params.PackagesEnabled
	if mode := strings.TrimSpace(params.PackagesMode); mode != "" {
		rt.PackagesMode = mode
	}
	return rt, true
}

func summaryText(summary runSummary, rt settings.RuntimeAutoUpdateSettings) string {
	parts := []string{"Automatic updates completed"}
	if rt.AppsEnabled {
		parts = append(parts, fmt.Sprintf("%d application image update(s) planned", summary.appsUpdated))
	}
	if rt.PackagesEnabled {
		mode := "security"
		if rt.PackagesMode == settings.AutoUpdatePackagesModeAll {
			mode = "all"
		}
		parts = append(parts, fmt.Sprintf("%d server(s) processed (%s)", summary.serversUpdated, mode))
	}
	if summary.appsDisabled > 0 {
		parts = append(parts, fmt.Sprintf("%d application(s) skipped by their auto-update switch", summary.appsDisabled))
	}
	text := strings.Join(parts, "; ")
	return truncateSummary(text)
}

// failureSummaryText 生成失败的只读摘要（lastRunSummary），与任务错误信息同源，
// 但裁剪到 240 字符以适配设置页展示。
func failureSummaryText(summary runSummary) string {
	text := fmt.Sprintf("Automatic updates failed with %d failure(s): %s", len(summary.failures), strings.Join(summary.failures, "; "))
	return truncateSummary(text)
}

func truncateSummary(text string) string {
	if runes := []rune(text); len(runes) > 240 {
		return string(runes[:240])
	}
	return text
}

func (s *Service) runPhases(ctx context.Context, task tasks.Task, rt settings.RuntimeAutoUpdateSettings) runSummary {
	var summary runSummary
	_ = s.tasks.Advance(ctx, task.ID, "refreshing", "refreshing image and package update caches")
	s.runRefreshPhase(ctx, task, rt)
	if rt.AppsEnabled {
		_ = s.tasks.Advance(ctx, task.ID, "updating-applications", "updating application images")
		s.runApplicationPhase(ctx, task, &summary)
		s.runReverseProxyPhase(ctx, task, &summary)
	} else {
		_ = s.tasks.AppendLog(ctx, task.ID, "system", "Skipping application updates: the automatic application update switch is off")
	}
	if rt.PackagesEnabled {
		_ = s.tasks.Advance(ctx, task.ID, "updating-packages", "updating server packages")
		s.runPackagePhase(ctx, task, rt, &summary)
	} else {
		_ = s.tasks.AppendLog(ctx, task.ID, "system", "Skipping package updates: the automatic package update switch is off")
	}
	return summary
}

// runRefreshPhase 触发应用镜像 digest 与所有可用节点的镜像/软件包刷新，并做
// 有界等待。刷新只是最佳努力：任一对象失败或整体超时都只记日志，后续阶段使用
// 缓存数据继续。
//
// 应用镜像刷新不可省略：applications.ImageUpdateAvailable 只由
// application_image_check 任务更新，不先刷新的话应用阶段永远筛不到可用更新。
func (s *Service) runRefreshPhase(ctx context.Context, task tasks.Task, rt settings.RuntimeAutoUpdateSettings) {
	taskIDs := make([]string, 0, 4)
	// 应用镜像 digest 刷新放在最前面：只依赖应用清单，不依赖节点遍历；仅在
	// 应用自动更新开启时需要（关闭时创建检查任务纯属浪费）。
	if rt.AppsEnabled && s.applicationImages != nil {
		refreshed, err := s.applicationImages.RefreshImageUpdates(ctx)
		if err != nil {
			s.logTask(ctx, task.ID, "Application image update check skipped: "+err.Error())
		} else {
			for _, taskID := range refreshed {
				if trimmed := strings.TrimSpace(taskID); trimmed != "" {
					taskIDs = append(taskIDs, trimmed)
				}
			}
		}
	}
	servers := s.candidateServers(ctx, task)
	for _, srv := range servers {
		if s.imageRefresh != nil && srv.Reachable && agentCompatible(srv) {
			refreshTask, err := s.imageRefresh.RefreshImages(ctx, srv.ID, triggerTypeScheduler, "")
			if err != nil {
				s.logTask(ctx, task.ID, "Image update check skipped for server "+srv.Name+": "+err.Error())
				continue
			}
			if strings.TrimSpace(refreshTask.ID) != "" {
				taskIDs = append(taskIDs, refreshTask.ID)
			}
		}
		if s.packages != nil && srv.OS.Supported && agentCompatible(srv) {
			result, err := s.packages.Refresh(ctx, srv.ID, triggerTypeScheduler)
			if err != nil {
				s.logTask(ctx, task.ID, "Package update check skipped for server "+srv.Name+": "+err.Error())
				continue
			}
			if strings.TrimSpace(result.TaskID) != "" {
				taskIDs = append(taskIDs, result.TaskID)
			}
		}
	}
	if len(taskIDs) == 0 {
		return
	}
	if !s.waitForTasks(ctx, taskIDs, s.refreshWait) {
		s.logTask(ctx, task.ID, fmt.Sprintf("Update checks did not finish within %s; continuing with cached data", s.refreshWait))
	}
}

// runApplicationPhase 只把 Enabled && AutoUpdateEnabled && ImageUpdateAvailable 的
// 应用交给升级器；对象开关关闭只跳过自动更新，不影响手动更新。
func (s *Service) runApplicationPhase(ctx context.Context, task tasks.Task, summary *runSummary) {
	if s.applications == nil || s.applicationUpdate == nil {
		s.logTask(ctx, task.ID, "Application image auto-update is unavailable; skipping")
		return
	}
	apps, err := s.applications.List(ctx)
	if err != nil {
		summary.failures = append(summary.failures, "listing applications: "+err.Error())
		s.logTask(ctx, task.ID, "Listing applications for automatic updates failed: "+err.Error())
		return
	}
	summary.appsConsidered = len(apps)
	ids := make([]string, 0, len(apps))
	for _, app := range apps {
		if !app.AutoUpdateEnabled {
			summary.appsDisabled++
			continue
		}
		if app.Enabled && app.ImageUpdateAvailable {
			ids = append(ids, app.ID)
		}
	}
	if summary.appsDisabled > 0 {
		s.logTask(ctx, task.ID, fmt.Sprintf("%d application(s) skipped because their automatic update switch is off", summary.appsDisabled))
	}
	if len(ids) == 0 {
		s.logTask(ctx, task.ID, "No application images need an automatic update")
		return
	}
	upgradeTask, err := s.applicationUpdate.UpgradeApplications(ctx, ids, false)
	if err != nil {
		summary.failures = append(summary.failures, "updating application images: "+err.Error())
		s.logTask(ctx, task.ID, "Planning application image updates failed: "+err.Error())
		return
	}
	summary.appsUpdated = len(ids)
	s.logTask(ctx, task.ID, fmt.Sprintf("Planned image updates for %d application(s); deployment continues asynchronously", len(ids)))
	if strings.TrimSpace(upgradeTask.ID) == "" {
		return
	}
	if !s.waitForTasks(ctx, []string{upgradeTask.ID}, s.appsWait) {
		summary.failures = append(summary.failures, fmt.Sprintf("application image updates did not finish within %s", s.appsWait))
		return
	}
	latest, err := s.tasks.Get(ctx, upgradeTask.ID)
	if err != nil {
		summary.failures = append(summary.failures, "reading application image update task: "+err.Error())
		return
	}
	if latest.Status != tasks.StatusCompleted {
		summary.failures = append(summary.failures, fmt.Sprintf("application image updates ended with %s: %s", latest.Status, latest.Error))
	}
}

// runReverseProxyPhase 调用入口代理的同步检查+滚动更新；失败只记入汇总，
// 不阻断后续软件包阶段。
func (s *Service) runReverseProxyPhase(ctx context.Context, task tasks.Task, summary *runSummary) {
	if s.reverseProxy == nil {
		s.logTask(ctx, task.ID, "Reverse proxy image auto-update is unavailable; skipping")
		return
	}
	proxyCtx, cancel := context.WithTimeout(ctx, s.proxyWait)
	defer cancel()
	if err := s.reverseProxy.AutoUpdateReverseProxyImage(proxyCtx); err != nil {
		summary.failures = append(summary.failures, "reverse proxy image: "+err.Error())
		s.logTask(ctx, task.ID, "Reverse proxy image auto-update failed: "+err.Error())
		return
	}
	s.logTask(ctx, task.ID, "Reverse proxy image auto-update finished")
}

// runPackagePhase 逐服务器串行：单台失败记录后继续下一台，绝不因一台失败跳过
// 其余服务器。security 模式使用 package_upgrade_security（实时安全分类）。
func (s *Service) runPackagePhase(ctx context.Context, task tasks.Task, rt settings.RuntimeAutoUpdateSettings, summary *runSummary) {
	if s.packages == nil {
		s.logTask(ctx, task.ID, "Package auto-update is unavailable; skipping")
		return
	}
	mode := rt.PackagesMode
	if mode != settings.AutoUpdatePackagesModeAll {
		mode = settings.AutoUpdatePackagesModeSecurity
	}
	servers := s.candidateServers(ctx, task)
	eligible := 0
	for _, srv := range servers {
		if !srv.Reachable || !srv.OS.Supported || !agentCompatible(srv) {
			continue
		}
		eligible++
		var upgradeTask tasks.Task
		var err error
		if mode == settings.AutoUpdatePackagesModeAll {
			upgradeTask, err = s.packages.UpgradeAll(ctx, srv.ID, triggerTypeScheduler)
		} else {
			upgradeTask, err = s.packages.UpgradeSecurity(ctx, srv.ID, triggerTypeScheduler)
		}
		if err != nil {
			summary.failures = append(summary.failures, fmt.Sprintf("server %s package update: %v", srv.Name, err))
			s.logTask(ctx, task.ID, fmt.Sprintf("Package update failed for server %s: %v", srv.Name, err))
			continue
		}
		if strings.TrimSpace(upgradeTask.ID) == "" {
			continue
		}
		// 串行：等待本节点任务终态后才处理下一台服务器。
		if !s.waitForTasks(ctx, []string{upgradeTask.ID}, s.packagesWait) {
			summary.failures = append(summary.failures, fmt.Sprintf("server %s package update did not finish within %s", srv.Name, s.packagesWait))
			continue
		}
		latest, err := s.tasks.Get(ctx, upgradeTask.ID)
		if err != nil {
			summary.failures = append(summary.failures, fmt.Sprintf("server %s package update: %v", srv.Name, err))
			continue
		}
		if latest.Status == tasks.StatusCompleted {
			summary.serversUpdated++
			continue
		}
		detail := strings.TrimSpace(latest.Error)
		if detail == "" {
			detail = latest.Status
		}
		summary.failures = append(summary.failures, fmt.Sprintf("server %s package update ended with %s", srv.Name, detail))
	}
	summary.serversTotal = eligible
	if eligible == 0 {
		s.logTask(ctx, task.ID, "No eligible server for automatic package updates")
	}
}

func (s *Service) candidateServers(ctx context.Context, task tasks.Task) []server.Server {
	if s.servers == nil {
		return nil
	}
	servers, err := s.servers.List(ctx)
	if err != nil {
		s.logTask(ctx, task.ID, "Listing servers for automatic updates failed: "+err.Error())
		return nil
	}
	return servers
}

// waitForTasks 轮询等待一批任务进入终态；超时返回 false（调用方自行记录）。
// failed_retryable 视为本次已结束：重试由任务框架独立推进，协调任务不等待。
func (s *Service) waitForTasks(ctx context.Context, taskIDs []string, timeout time.Duration) bool {
	pending := make(map[string]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		if strings.TrimSpace(taskID) != "" {
			pending[taskID] = struct{}{}
		}
	}
	if len(pending) == 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(s.waitPoll)
	defer ticker.Stop()
	for {
		for taskID := range pending {
			latest, err := s.tasks.Get(ctx, taskID)
			if err != nil {
				delete(pending, taskID)
				continue
			}
			if taskFinished(latest.Status) {
				delete(pending, taskID)
			}
		}
		if len(pending) == 0 {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func taskFinished(status string) bool {
	switch status {
	case tasks.StatusCompleted, tasks.StatusFailed, tasks.StatusFailedRetryable, tasks.StatusBlocked, tasks.StatusCancelled:
		return true
	default:
		return false
	}
}

func (s *Service) recordLastRun(ctx context.Context, taskID string, runAt time.Time, status, summary string) {
	if s.settings == nil {
		return
	}
	err := s.settings.RecordAutoUpdateLastRun(context.WithoutCancel(ctx), settings.RuntimeAutoUpdateRunState{
		LastRunAt:      runAt,
		LastRunStatus:  status,
		LastRunTaskID:  taskID,
		LastRunSummary: summary,
	})
	if err != nil {
		// 运行态写失败不影响自动更新主流程（已执行的更新不可回滚）。
		logging.From(ctx).Warn("failed to persist automatic update run state", zap.String("task_id", taskID), zap.Error(err))
	}
}

// recordAutoRun 记录「调度自动运行」时间，作为跨重启的当日去重依据。手动运行
// 绝不调用本方法；写失败同样只记日志，不阻断更新本身。
func (s *Service) recordAutoRun(ctx context.Context, taskID string, at time.Time) {
	if s.settings == nil {
		return
	}
	if err := s.settings.RecordAutoUpdateAutoRun(context.WithoutCancel(ctx), at); err != nil {
		logging.From(ctx).Warn("failed to persist automatic update auto-run time", zap.String("task_id", taskID), zap.Error(err))
	}
}

func (s *Service) logTask(ctx context.Context, taskID, message string) {
	if s.tasks == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	_ = s.tasks.AppendLog(ctx, taskID, "system", message)
}

func agentCompatible(srv server.Server) bool {
	return srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusCompatible &&
		strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) != ""
}
