package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentpb "panel/internal/agent/pb"
	agentsshsessions "panel/internal/agent/sshsessions"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *reportHub) notifySSHLocked() {
	select {
	case h.sshWake <- struct{}{}:
	default:
	}
}

func (h *reportHub) notifySSHLiveLocked() {
	if h.sshLiveWake == nil {
		return
	}
	select {
	case h.sshLiveWake <- struct{}{}:
	default:
	}
}

func (h *reportHub) sshSinceSnapshot() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sshSince
}

// runSSHSessionSampler 周期性读取 utmp：首次或新 watcher 接入时立即上报，之后
// 在会话指纹变化或达到心跳间隔时上报；检测到上下线变化时同步做一次近 5 分钟的
// wtmp 增量补齐，以捕获短会话与精确登出时间。
func (h *reportHub) runSSHSessionSampler(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	var lastFingerprint string
	var lastSent time.Time
	var lastErr string
	force := true
	for {
		items, err := agentsshsessions.ReadSessions(h.sshOptions)
		if err != nil {
			if message := err.Error(); message != lastErr && ctx.Err() == nil {
				logging.From(ctx).Warn("agent SSH session sampling failed", zap.Error(err))
				lastErr = message
			}
		} else {
			lastErr = ""
			fingerprint := sshSessionFingerprint(items)
			now := time.Now().UTC()
			changed := lastFingerprint != "" && fingerprint != lastFingerprint
			if force || fingerprint != lastFingerprint || now.Sub(lastSent) >= sessionHeartbeatInterval {
				h.broadcastSSHSnapshot(items, now)
				if changed {
					// 上下线变化时立即增量读取近期 wtmp，补齐短会话与精确退出时间。
					h.pushSSHHistory(ctx, now.Add(-5*time.Minute))
				}
				lastFingerprint = fingerprint
				lastSent = now
				force = false
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-h.sshLiveWake:
			force = true
		case <-time.After(sessionPollInterval):
		}
	}
}

// runSSHFailureSampler 持续轮询 btmp 与 sshd 认证日志（journal/auth.log），
// 在检测到新的登录失败尝试时立即上报，解决此前仅在首次回填上报失败的问题。
func (h *reportHub) runSSHFailureSampler(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	cursor := time.Now().UTC().Truncate(time.Second)
	seen := map[string]time.Time{}
	var lastErr string
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(failurePollInterval):
		}
		since := cursor.Add(-failurePollOverlap)
		failures, err := agentsshsessions.ReadAllFailures(ctx, h.sshOptions, since)
		if err != nil {
			if message := err.Error(); message != lastErr && ctx.Err() == nil {
				logging.From(ctx).Warn("agent SSH failure sampling failed", zap.Error(err))
				lastErr = message
			}
			continue
		}
		lastErr = ""
		now := time.Now().UTC()
		var fresh []agentcontract.SSHFailure
		maxAt := cursor
		for _, item := range failures.Items {
			key := sshFailureKey(item)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = now
			if item.AttemptAt.After(maxAt) {
				maxAt = item.AttemptAt
			}
			fresh = append(fresh, agentcontract.SSHFailure{
				UserName:      item.UserName,
				SourceHost:    item.SourceHost,
				SourceAddress: item.SourceAddress,
				AttemptAt:     item.AttemptAt,
				PID:           item.PID,
			})
		}
		if len(fresh) > 0 {
			h.sendSSHFailureBatches(fresh, since, failures.Truncated)
			cursor = maxAt.UTC().Truncate(time.Second)
		}
		// 清理超过 2 分钟的去重缓存，保持内存有界。
		cutoff := now.Add(-2 * time.Minute)
		for k, ts := range seen {
			if ts.Before(cutoff) {
				delete(seen, k)
			}
		}
	}
}

func sshFailureKey(item agentsshsessions.Failure) string {
	source := item.SourceAddress
	if source == "" {
		source = item.SourceHost
	}
	return fmt.Sprintf("%s|%s|%d|%d", item.UserName, source, item.AttemptAt.UTC().Unix(), item.PID)
}

func sshSessionFingerprint(items []agentsshsessions.Session) string {
	hash := sha256.New()
	for _, item := range items {
		fmt.Fprintf(hash, "%s|%s|%d|%d|%s|%s|%s\n", item.UserName, item.TTY, item.PID, item.LoginAt.Unix(), item.SourceAddress, item.Kind, item.SourceHost)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (h *reportHub) broadcastSSHSnapshot(items []agentsshsessions.Session, sampledAt time.Time) {
	snapshot := agentcontract.SSHSnapshot{
		Items:     make([]agentcontract.SSHSession, 0, len(items)),
		SampledAt: sampledAt.Truncate(time.Second),
	}
	for _, item := range items {
		snapshot.Items = append(snapshot.Items, agentcontract.SSHSession{
			UserName:      item.UserName,
			TTY:           item.TTY,
			SourceHost:    item.SourceHost,
			SourceAddress: item.SourceAddress,
			LoginAt:       item.LoginAt,
			PID:           item.PID,
			Kind:          item.Kind,
		})
	}
	h.broadcastReport(&agentpb.AgentReport{
		SampleAt:    timestamppb.New(sampledAt.Truncate(time.Second)),
		Reason:      "ssh_sessions",
		SshSessions: pbSSHSnapshot(snapshot),
	})
}

// runSSHBackfill 在启动和 Panel 下发新游标时回填 wtmp 与失败尝试。游标来自安全库
// 的历史记录减 1 小时，重复区间靠确定性主键幂等。回填结束后触发一次在线快照，
// 保证未配对的旧 wtmp 记录不会短暂显示为在线。
func (h *reportHub) runSSHBackfill(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	for {
		since := h.sshSinceSnapshot()
		if since <= 0 {
			since = time.Now().Add(-backfillMaxAge).Unix()
		}
		sinceAt := time.Unix(since, 0)
		h.pushSSHHistory(ctx, sinceAt)
		h.pushSSHFailures(ctx, sinceAt)
		h.notifySSHLiveLocked()
		select {
		case <-ctx.Done():
			return
		case <-h.sshWake:
		}
	}
}

func (h *reportHub) pushSSHHistory(ctx context.Context, since time.Time) {
	history, err := agentsshsessions.ReadHistory(h.sshOptions, since)
	if err != nil {
		if ctx.Err() == nil {
			logging.From(ctx).Warn("agent SSH history backfill failed", zap.Error(err))
		}
		return
	}
	items := make([]agentcontract.SSHSessionHistoryItem, 0, len(history.Items))
	for _, item := range history.Items {
		items = append(items, agentcontract.SSHSessionHistoryItem{
			SSHSession: agentcontract.SSHSession{
				UserName:      item.UserName,
				TTY:           item.TTY,
				SourceHost:    item.SourceHost,
				SourceAddress: item.SourceAddress,
				LoginAt:       item.LoginAt,
				PID:           item.PID,
				Kind:          item.Kind,
			},
			LogoutAt: item.LogoutAt,
		})
	}
	h.sendSSHHistoryBatches(items, since, history.Truncated)
}

func (h *reportHub) sendSSHHistoryBatches(items []agentcontract.SSHSessionHistoryItem, since time.Time, truncated bool) {
	for start := 0; start < len(items); start += backfillBatch {
		end := minInt(start+backfillBatch, len(items))
		batch := agentcontract.SSHSessionHistory{
			Items:     items[start:end],
			Since:     since,
			Truncated: truncated && end == len(items),
		}
		h.broadcastReport(&agentpb.AgentReport{
			SampleAt:          timestamppb.New(time.Now().UTC().Truncate(time.Second)),
			Reason:            "ssh_session_history",
			SshSessionHistory: pbSSHHistory(batch),
		})
	}
}

func (h *reportHub) pushSSHFailures(ctx context.Context, since time.Time) {
	failures, err := agentsshsessions.ReadAllFailures(ctx, h.sshOptions, since)
	if err != nil {
		if ctx.Err() == nil {
			logging.From(ctx).Warn("agent SSH failure backfill failed", zap.Error(err))
		}
		return
	}
	items := make([]agentcontract.SSHFailure, 0, len(failures.Items))
	for _, item := range failures.Items {
		items = append(items, agentcontract.SSHFailure{
			UserName:      item.UserName,
			SourceHost:    item.SourceHost,
			SourceAddress: item.SourceAddress,
			AttemptAt:     item.AttemptAt,
			PID:           item.PID,
		})
	}
	h.sendSSHFailureBatches(items, since, failures.Truncated)
}

func (h *reportHub) sendSSHFailureBatches(items []agentcontract.SSHFailure, since time.Time, truncated bool) {
	for start := 0; start < len(items); start += backfillBatch {
		end := minInt(start+backfillBatch, len(items))
		batch := agentcontract.SSHFailures{
			Items:     items[start:end],
			Since:     since,
			Truncated: truncated && end == len(items),
		}
		h.broadcastReport(&agentpb.AgentReport{
			SampleAt:          timestamppb.New(time.Now().UTC().Truncate(time.Second)),
			Reason:            "ssh_failed_attempts",
			SshFailedAttempts: pbSSHFailures(batch),
		})
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
