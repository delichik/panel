package autoupdate

import (
	"context"
	"strings"
	"sync"
	"time"

	"panel/internal/modules/settings"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

// Service 是设置驱动的自动更新协调器：每 30 秒读取一次热更新设置，命中每日
// 计划时间窗口且当日尚未运行过时，创建一个全局互斥的 auto_update_run 任务。
type Service struct {
	tasks    *tasks.Service
	settings SettingsStore

	servers           ServerLister
	applications      ApplicationLister
	applicationUpdate ApplicationUpgrader
	imageRefresh      ImageRefresher
	packages          PackageMaintainer
	reverseProxy      ReverseProxyImageUpdater

	now            func() time.Time
	tickInterval   time.Duration
	scheduleWindow time.Duration
	waitPoll       time.Duration
	refreshWait    time.Duration
	appsWait       time.Duration
	proxyWait      time.Duration
	packagesWait   time.Duration
}

type Option func(*Service)

func WithServerLister(lister ServerLister) Option {
	return func(s *Service) { s.servers = lister }
}

func WithApplicationLister(lister ApplicationLister) Option {
	return func(s *Service) { s.applications = lister }
}

func WithApplicationUpgrader(upgrader ApplicationUpgrader) Option {
	return func(s *Service) { s.applicationUpdate = upgrader }
}

func WithImageRefresher(refresher ImageRefresher) Option {
	return func(s *Service) { s.imageRefresh = refresher }
}

func WithPackageMaintainer(maintainer PackageMaintainer) Option {
	return func(s *Service) { s.packages = maintainer }
}

func WithReverseProxyImageUpdater(updater ReverseProxyImageUpdater) Option {
	return func(s *Service) { s.reverseProxy = updater }
}

// WithClock 注入时钟（测试窗口/去重语义）。
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithTickInterval 注入调度 tick 间隔（测试用；生产保持 30s）。
func WithTickInterval(interval time.Duration) Option {
	return func(s *Service) { s.tickInterval = interval }
}

// WithScheduleWindow 注入计划时间后的容忍窗口。
func WithScheduleWindow(window time.Duration) Option {
	return func(s *Service) { s.scheduleWindow = window }
}

// WithPhaseTimeouts 调整各阶段的等待上限（测试用）。
func WithPhaseTimeouts(refresh, apps, reverseProxy, packages time.Duration) Option {
	return func(s *Service) {
		s.refreshWait = refresh
		s.appsWait = apps
		s.proxyWait = reverseProxy
		s.packagesWait = packages
	}
}

// WithWaitPollInterval 调整等待子任务时的轮询间隔。
func WithWaitPollInterval(interval time.Duration) Option {
	return func(s *Service) { s.waitPoll = interval }
}

func NewService(taskSvc *tasks.Service, settingsStore SettingsStore, options ...Option) *Service {
	s := &Service{
		tasks:          taskSvc,
		settings:       settingsStore,
		now:            time.Now,
		tickInterval:   DefaultTickInterval,
		scheduleWindow: DefaultScheduleWindow,
		waitPoll:       defaultWaitPollInterval,
		refreshWait:    defaultRefreshTimeout,
		appsWait:       defaultAppsTimeout,
		proxyWait:      defaultReverseProxyTimeout,
		packagesWait:   defaultPackagesTimeout,
	}
	for _, option := range options {
		option(s)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.tickInterval <= 0 {
		s.tickInterval = DefaultTickInterval
	}
	if s.scheduleWindow <= 0 {
		s.scheduleWindow = DefaultScheduleWindow
	}
	if s.waitPoll <= 0 {
		s.waitPoll = defaultWaitPollInterval
	}
	if s.refreshWait <= 0 {
		s.refreshWait = defaultRefreshTimeout
	}
	if s.appsWait <= 0 {
		s.appsWait = defaultAppsTimeout
	}
	if s.proxyWait <= 0 {
		s.proxyWait = defaultReverseProxyTimeout
	}
	if s.packagesWait <= 0 {
		s.packagesWait = defaultPackagesTimeout
	}
	return s
}

// RegisterTasks 注册 auto_update_run。AllowRunNow=false：自动更新只能由调度器
// 按计划触发；DisallowCancel=true：长任务不允许从事件页取消；全局互斥兜底
// 防止同一时刻出现两次自动更新。
func (s *Service) RegisterTasks(taskSvc *tasks.Service) {
	if taskSvc == nil {
		return
	}
	if s.tasks == nil {
		s.tasks = taskSvc
	}
	taskSvc.MustRegister(tasks.Definition{
		Type:              TaskTypeAutoUpdateRun,
		Summary:           "Automatic updates",
		AllowRunNow:       false,
		AllowRetry:        true,
		DisallowCancel:    true,
		ConcurrencyPolicy: tasks.ConcurrencyGlobalExclusive,
		Execute:           s.RunAutoUpdateTask,
	})
}

// Loop 拥有自动更新调度 goroutine；Wait 用于关闭时等待退出。
type Loop struct {
	service *Service
	wg      sync.WaitGroup
}

func (l *Loop) Wait() {
	if l != nil {
		l.wg.Wait()
	}
}

// StartPeriodic 启动设置驱动的 30s tick 调度循环。设置热更新：每次 tick 都通过
// SettingsStore.Runtime() 读取最新开关与执行时间，修改后下一次 tick 生效。
func (s *Service) StartPeriodic(ctx context.Context) *Loop {
	if s == nil || s.tasks == nil || s.settings == nil {
		return nil
	}
	loop := &Loop{service: s}
	loop.wg.Add(1)
	go func() {
		defer loop.wg.Done()
		loop.run(ctx)
	}()
	return loop
}

func (l *Loop) run(ctx context.Context) {
	ticker := time.NewTicker(l.service.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx := logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "autoupdate"})
			if _, _, err := l.service.tick(tickCtx); err != nil {
				logging.From(tickCtx).Warn("automatic update trigger failed", zap.Error(err))
			}
		}
	}
}

// tick 采样一次设置并决定是否创建协调任务。返回创建的任务与是否新建（false
// 表示已存在运行中的协调任务而被并发键去重）。
func (s *Service) tick(ctx context.Context) (tasks.Task, bool, error) {
	rt := s.settings.Runtime().AutoUpdate
	now := s.now()
	if !shouldTrigger(rt, now, s.scheduleWindow) {
		return tasks.Task{}, false, nil
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         TaskTypeAutoUpdateRun,
		ResourceType: "system",
		ResourceID:   "automatic-updates",
		TriggerType:  triggerTypeScheduler,
		Summary:      "Automatic updates",
		ParamsJSON:   paramsFor(rt, triggerTypeScheduler),
	}, tasks.Trigger{Type: triggerTypeScheduler, Periodic: true})
	if err != nil || !created {
		return task, created, err
	}
	s.dispatch(task)
	return task, true, nil
}

// RunAutoUpdateNow 手动「立即运行一次」（SET-RUN-023）：跳过计划时间窗口与当日
// 去重，但仍走同一个全局互斥的 auto_update_run 任务。已有活跃任务（含调度触发
// 的运行）时幂等返回其 taskId，不重复派发。rt 是设置服务解析后的本次有效设置
// （可含临时覆盖），写入 ParamsJSON 供执行阶段使用而不落库。
func (s *Service) RunAutoUpdateNow(ctx context.Context, rt settings.RuntimeAutoUpdateSettings) (string, error) {
	if s == nil || s.tasks == nil {
		return "", panelerr.Validation("task_service_unavailable", "Task service is unavailable")
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         TaskTypeAutoUpdateRun,
		ResourceType: "system",
		ResourceID:   "automatic-updates",
		TriggerType:  triggerTypeUser,
		Summary:      "Automatic updates",
		ParamsJSON:   paramsFor(rt, triggerTypeManual),
	}, tasks.Trigger{Type: triggerTypeUser, Manual: true})
	if err != nil {
		return "", err
	}
	if created {
		s.dispatch(task)
	}
	return task.ID, nil
}

// dispatch 在后台运行任务；与各模块 CreateAndRun 的调度方式一致，任务创建
// 成功即返回，避免 tick 被长任务阻塞。
func (s *Service) dispatch(task tasks.Task) {
	if s.tasks == nil || strings.TrimSpace(task.ID) == "" {
		return
	}
	go func() {
		defer s.tasks.FinishExecution(task.ID)
		if err := tasks.NewManager(s.tasks).Run(context.Background(), task); err != nil {
			logging.From(logging.Background("autoupdate")).Warn("automatic update task failed to run", zap.String("task_id", task.ID), zap.Error(err))
		}
	}()
}

// shouldTrigger 实现触发条件：至少一个开关开启 && 本地时间处于计划时间窗口
// （错过不补跑）&& 当日（Panel 本地日期）尚无运行记录。
func shouldTrigger(rt settings.RuntimeAutoUpdateSettings, now time.Time, window time.Duration) bool {
	if !rt.AppsEnabled && !rt.PackagesEnabled {
		return false
	}
	if ranOnLocalDay(rt.LastRunAt, now) {
		return false
	}
	return inScheduleWindow(rt.ScheduleTime, now, window)
}

// inScheduleWindow 判断 now 是否落在计划 HH:MM 之后 window 长度的窗口内。
// 只看当日，不回看前一天：计划时间错过整个窗口后当天不再补跑。
func inScheduleWindow(scheduleTime string, now time.Time, window time.Duration) bool {
	hour, minute, ok := settings.ParseAutoUpdateScheduleTime(scheduleTime)
	if !ok {
		return false
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	delta := now.Sub(target)
	return delta >= 0 && delta <= window
}

// ranOnLocalDay 报告 lastRunAt 是否落在 now 的本地日期。解析失败的旧值视为
// 没有记录（宁可多跑一次，也不因脏数据永久跳过）。
func ranOnLocalDay(lastRunAt string, now time.Time) bool {
	trimmed := strings.TrimSpace(lastRunAt)
	if trimmed == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		if parsed, err = time.Parse(time.RFC3339, trimmed); err != nil {
			return false
		}
	}
	local := parsed.In(now.Location())
	return local.Year() == now.Year() && local.YearDay() == now.YearDay()
}
