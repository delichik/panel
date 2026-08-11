package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentdocker "panel/internal/agent/docker"
	agentpb "panel/internal/agent/pb"
	"panel/internal/platform/linux"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const reportCollectionTimeout = 30 * time.Second

type reportConfig struct {
	serverID          string
	metricsInterval   time.Duration
	containerInterval time.Duration
}

// reportCollector is the subset of agentsystem.LocalCollector used by the
// report hub; it exists so tests can inject failing collectors.
type reportCollector interface {
	MetricsSnapshot(ctx context.Context, serverID string) (linux.MetricsSnapshot, error)
	PackageUpdates(ctx context.Context) ([]linux.PackageUpdate, error)
}

type reportHub struct {
	collector reportCollector
	runtime   *agentdocker.LocalRuntime

	mu                sync.Mutex
	watchers          map[int]*reportWatcher
	nextWatcherID     int
	running           bool
	wake              chan struct{}
	lastContainerHash string
	collectMu         sync.Mutex
}

type reportWatcher struct {
	id  int
	cfg reportConfig
	ch  chan *agentpb.AgentReport
}

type reportWatcherSnapshot struct {
	id  int
	cfg reportConfig
	ch  chan *agentpb.AgentReport
}

func newReportHub(collector reportCollector, runtime *agentdocker.LocalRuntime) *reportHub {
	return &reportHub{
		collector: collector,
		runtime:   runtime,
		watchers:  map[int]*reportWatcher{},
		wake:      make(chan struct{}, 1),
	}
}

func (h *Handler) Report(stream agentpb.AgentReportService_ReportServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	watcher := h.reports.add(normalizeReportConfig(first))
	defer h.reports.remove(watcher.id)

	errCh := make(chan error, 1)
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			h.reports.update(watcher.id, normalizeReportConfig(msg))
		}
	}()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case err := <-errCh:
			return err
		case report := <-watcher.ch:
			if err := stream.Send(report); err != nil {
				return err
			}
		}
	}
}

func (h *reportHub) add(cfg reportConfig) *reportWatcher {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextWatcherID++
	watcher := &reportWatcher{id: h.nextWatcherID, cfg: cfg, ch: make(chan *agentpb.AgentReport, 16)}
	h.watchers[watcher.id] = watcher
	if !h.running {
		h.running = true
		go h.run()
	}
	h.notifyLocked()
	return watcher
}

func (h *reportHub) update(id int, cfg reportConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	watcher, ok := h.watchers[id]
	if !ok {
		return
	}
	if cfg.serverID != "" {
		watcher.cfg.serverID = cfg.serverID
	}
	if cfg.metricsInterval > 0 {
		watcher.cfg.metricsInterval = cfg.metricsInterval
	}
	if cfg.containerInterval > 0 {
		watcher.cfg.containerInterval = cfg.containerInterval
	}
	h.notifyLocked()
}

func (h *reportHub) remove(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.watchers, id)
	h.notifyLocked()
}

func (h *reportHub) isRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func (h *reportHub) notifyLocked() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *reportHub) run() {
	eventCtx, stopEvents := context.WithCancel(context.Background())
	defer stopEvents()
	go h.watchContainerEvents(eventCtx)
	go h.watchImageEvents(eventCtx)
	go h.watchPackageEvents(eventCtx)
	for {
		watchers := h.snapshot()
		if len(watchers) == 0 {
			if h.tryStop() {
				return
			}
			continue
		}
		dueAt, ok := nextReportDue(time.Now().UTC(), watchers)
		if !ok {
			h.waitForChange()
			continue
		}
		wait := dueAt.Sub(time.Now().UTC())
		if wait < 0 {
			wait = 0
		}
		if !h.waitUntilDue(wait) {
			continue
		}
		h.collectAndBroadcast(dueAt, false, "")
	}
}

// tryStop atomically marks the hub stopped only when no watchers remain. It
// closes the race where the last watcher is removed while a new watcher is
// added: the new watcher either finds the hub still running or starts a new
// run loop, so it can never be left without a scheduler.
func (h *reportHub) tryStop() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.watchers) > 0 {
		return false
	}
	h.running = false
	return true
}

func (h *reportHub) waitForChange() {
	<-h.wake
}

func (h *reportHub) waitUntilDue(wait time.Duration) bool {
	if wait <= 0 {
		select {
		case <-h.wake:
		default:
		}
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-h.wake:
		return false
	}
}

func (h *reportHub) snapshot() []reportWatcherSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]reportWatcherSnapshot, 0, len(h.watchers))
	for _, watcher := range h.watchers {
		out = append(out, reportWatcherSnapshot{id: watcher.id, cfg: watcher.cfg, ch: watcher.ch})
	}
	return out
}

func (h *reportHub) watchContainerEvents(ctx context.Context) {
	if h.runtime == nil {
		return
	}
	backoff := time.Second
	for ctx.Err() == nil {
		events, errs := h.runtime.ContainerEvents(ctx)
		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				h.collectAndBroadcast(time.Now().UTC().Truncate(time.Second), true, "container_change")
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err != nil && ctx.Err() == nil {
					logging.L().Warn("agent report container event watch failed", zap.Error(err))
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

const (
	imagePushMinInterval   = 30 * time.Second
	packagePushMinInterval = 10 * time.Minute
)

func (h *reportHub) watchImageEvents(ctx context.Context) {
	if h.runtime == nil {
		return
	}
	backoff := time.Second
	var lastPush time.Time
	for ctx.Err() == nil {
		events, errs := h.runtime.ImageEvents(ctx)
		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if time.Since(lastPush) < imagePushMinInterval {
					continue
				}
				lastPush = time.Now()
				h.pushImages(ctx, "image_change")
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err != nil && ctx.Err() == nil {
					logging.L().Warn("agent report image event watch failed", zap.Error(err))
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

const packageStatusPath = "/var/lib/dpkg/status"

func (h *reportHub) watchPackageEvents(ctx context.Context) {
	var lastMod time.Time
	var lastPush time.Time
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		info, err := os.Stat(packageStatusPath)
		if err != nil {
			continue
		}
		mod := info.ModTime().UTC().Truncate(time.Second)
		if lastMod.IsZero() {
			lastMod = mod
			continue
		}
		if !mod.After(lastMod) {
			continue
		}
		lastMod = mod
		if time.Since(lastPush) < packagePushMinInterval {
			continue
		}
		lastPush = time.Now()
		h.pushPackageUpdates(ctx)
	}
}

func (h *reportHub) pushImages(ctx context.Context, reason string) {
	pushCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	items, err := h.runtime.Images(pushCtx)
	if err != nil {
		logging.L().Warn("agent report image push failed", zap.Error(err))
		return
	}
	out := make([]*agentpb.DockerImage, 0, len(items))
	for _, item := range items {
		out = append(out, pbDockerImage(item))
	}
	h.broadcastReport(&agentpb.AgentReport{
		SampleAt: timestamppb.New(time.Now().UTC().Truncate(time.Second)),
		Reason:   reason,
		Images:   &agentpb.DockerImagesResponse{Items: out},
	})
}

func (h *reportHub) pushPackageUpdates(ctx context.Context) {
	pushCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	items, err := h.collector.PackageUpdates(pushCtx)
	if err != nil {
		logging.L().Warn("agent report package push failed", zap.Error(err))
		return
	}
	h.broadcastReport(&agentpb.AgentReport{
		SampleAt:       timestamppb.New(time.Now().UTC().Truncate(time.Second)),
		Reason:         "package_change",
		PackageUpdates: &agentpb.PackageUpdatesResponse{Items: pbPackageUpdates(items)},
	})
}

func (h *reportHub) broadcastReport(report *agentpb.AgentReport) {
	if report == nil {
		return
	}
	watchers := h.snapshot()
	for _, watcher := range watchers {
		if watcher.cfg.serverID == "" {
			continue
		}
		sendReport(watcher.ch, report)
	}
}

func (h *reportHub) collectAndBroadcast(sampleAt time.Time, forceContainers bool, forcedReason string) {
	h.collectMu.Lock()
	defer h.collectMu.Unlock()
	watchers := h.snapshot()
	targets := make([]reportWatcherSnapshot, 0, len(watchers))
	var collectMetrics, collectContainers bool
	serverID := ""
	for _, watcher := range watchers {
		if watcher.cfg.serverID == "" {
			continue
		}
		metricsDue := !forceContainers && reportIntervalDue(sampleAt, watcher.cfg.metricsInterval)
		containersDue := reportIntervalDue(sampleAt, watcher.cfg.containerInterval)
		if forceContainers {
			containersDue = watcher.cfg.containerInterval > 0
		}
		if !metricsDue && !containersDue {
			continue
		}
		if serverID == "" {
			serverID = watcher.cfg.serverID
		}
		collectMetrics = collectMetrics || metricsDue
		collectContainers = collectContainers || containersDue
		targets = append(targets, watcher)
	}
	if len(targets) == 0 || serverID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), reportCollectionTimeout)
	defer cancel()
	var metrics *agentpb.MetricsSnapshotResponse
	if collectMetrics {
		snap, err := h.collector.MetricsSnapshot(ctx, serverID)
		if err == nil {
			metrics = pbSnapshot(snap)
		} else {
			// Keep the field nil so the panel never overwrites a previously
			// collected snapshot with an empty one.
			logging.L().Warn("agent report metrics collection failed", zap.String("server_id", serverID), zap.Error(err))
		}
	}

	var containers *agentpb.DockerContainersResponse
	reason := "scheduled"
	if forcedReason != "" {
		reason = forcedReason
	}
	if collectContainers {
		items, hash, err := h.reportContainers(ctx)
		if err == nil {
			containers = &agentpb.DockerContainersResponse{Items: items}
			h.mu.Lock()
			if forcedReason == "" && h.lastContainerHash != "" && hash != h.lastContainerHash {
				reason = "container_change"
			}
			h.lastContainerHash = hash
			h.mu.Unlock()
		} else {
			// Keep the field nil so the panel never clears the last known
			// container snapshot on a transient collection failure.
			logging.L().Warn("agent report container collection failed", zap.Error(err))
		}
	}

	for _, target := range targets {
		report := &agentpb.AgentReport{SampleAt: timestamppb.New(sampleAt), Reason: reason}
		if !forceContainers && reportIntervalDue(sampleAt, target.cfg.metricsInterval) {
			report.Metrics = metrics
		}
		if (forceContainers && target.cfg.containerInterval > 0) || (!forceContainers && reportIntervalDue(sampleAt, target.cfg.containerInterval)) {
			report.Containers = containers
		}
		if report.Metrics == nil && report.Containers == nil {
			continue
		}
		sendReport(target.ch, report)
	}
}

func sendReport(ch chan *agentpb.AgentReport, report *agentpb.AgentReport) {
	select {
	case ch <- report:
	default:
		select {
		case <-ch:
		default:
		}
		ch <- report
	}
}

func normalizeReportConfig(msg *agentpb.AgentReportControl) reportConfig {
	if msg == nil {
		return reportConfig{}
	}
	out := reportConfig{serverID: msg.ServerId}
	if msg.Config != nil {
		out.metricsInterval = time.Duration(msg.Config.MetricsIntervalSeconds) * time.Second
		out.containerInterval = time.Duration(msg.Config.ContainersIntervalSeconds) * time.Second
	}
	if out.metricsInterval < time.Second {
		out.metricsInterval = 0
	}
	if out.containerInterval < time.Second {
		out.containerInterval = 0
	}
	return out
}

func (h *reportHub) reportContainers(ctx context.Context) ([]*agentpb.DockerContainer, string, error) {
	if h.runtime == nil {
		return nil, "", errors.New("runtime is not configured")
	}
	items, err := h.runtime.Containers(ctx)
	if err != nil {
		return nil, "", err
	}
	out := make([]*agentpb.DockerContainer, 0, len(items))
	hash := sha256.New()
	for _, item := range items {
		out = append(out, pbDockerContainerSlim(item))
		hashDockerContainer(hash, item)
	}
	return out, hex.EncodeToString(hash.Sum(nil)), nil
}

// hashDockerContainer folds the container fields that matter for change
// detection into a streaming hash, avoiding a full JSON marshal of the list.
// Managed-file drift labels are excluded so a drift transition does not
// masquerade as a container change.
func hashDockerContainer(h io.Writer, c agentcontract.DockerContainer) {
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00", c.ID, c.Image, c.State, c.Status)
	for _, name := range c.Names {
		fmt.Fprintf(h, "%s\x00", name)
	}
	h.Write([]byte{0xff})
	for _, port := range c.Ports {
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%s\x00", port.IP, port.PrivatePort, port.PublicPort, port.Type)
	}
	h.Write([]byte{0xfe})
	if len(c.Labels) > 0 {
		keys := make([]string, 0, len(c.Labels))
		for key := range c.Labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if strings.HasPrefix(key, "panel.application.managed_files.") {
				continue
			}
			fmt.Fprintf(h, "%s=%s\x00", key, c.Labels[key])
		}
	}
	h.Write([]byte{0xfd})
}

func nextReportDue(now time.Time, watchers []reportWatcherSnapshot) (time.Time, bool) {
	var out time.Time
	for _, watcher := range watchers {
		if watcher.cfg.serverID == "" {
			continue
		}
		if watcher.cfg.metricsInterval > 0 {
			out = minOptionalTime(out, nextAligned(now, watcher.cfg.metricsInterval))
		}
		if watcher.cfg.containerInterval > 0 {
			out = minOptionalTime(out, nextAligned(now, watcher.cfg.containerInterval))
		}
	}
	if out.IsZero() {
		return time.Time{}, false
	}
	return out, true
}

func reportIntervalDue(sampleAt time.Time, interval time.Duration) bool {
	seconds := int64(interval / time.Second)
	return seconds > 0 && sampleAt.UTC().Unix()%seconds == 0
}

func nextAligned(now time.Time, interval time.Duration) time.Time {
	now = now.UTC()
	seconds := int64(interval / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	unix := now.Unix()
	next := ((unix / seconds) + 1) * seconds
	if now.Nanosecond() == 0 && unix%seconds == 0 {
		next = unix
	}
	return time.Unix(next, 0).UTC()
}

func minOptionalTime(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func minTime(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}
