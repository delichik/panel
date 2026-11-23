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

func (h *reportHub) sshSinceSnapshot() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sshSince
}

// runSSHSessionSampler 周期性读取 utmp：首次立即上报，之后仅在会话指纹变化
// 或达到心跳间隔时上报，避免每 2 秒把相同快照推给 Panel。
func (h *reportHub) runSSHSessionSampler(ctx context.Context) {
	ctx = logging.With(ctx, logging.Context{StreamID: logging.NewStreamID(), Module: "agent"})
	var lastFingerprint string
	var lastSent time.Time
	var lastErr string
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
			if fingerprint != lastFingerprint || now.Sub(lastSent) >= sessionHeartbeatInterval {
				h.broadcastSSHSnapshot(items, now)
				lastFingerprint = fingerprint
				lastSent = now
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(sessionPollInterval):
		}
	}
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

// runSSHBackfill 在启动和 Panel 下发新游标时回填 wtmp/btmp。游标来自安全库
// 的 MAX(login_at)/MAX(attempt_at) 减 1 小时，重复区间靠确定性主键幂等。
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
	failures, err := agentsshsessions.ReadFailures(h.sshOptions, since)
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
	for start := 0; start < len(items); start += backfillBatch {
		end := minInt(start+backfillBatch, len(items))
		batch := agentcontract.SSHFailures{
			Items:     items[start:end],
			Since:     since,
			Truncated: failures.Truncated && end == len(items),
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
