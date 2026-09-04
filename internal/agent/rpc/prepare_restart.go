package rpc

import (
	"sync"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentpb "panel/internal/agent/pb"
)

// packageUpgradeTracker tracks package upgrades started through the agent.
// PrepareRestart holds the panel connection while an upgrade is in flight so
// the agent is not stopped or restarted mid-transaction.
type packageUpgradeTracker struct {
	mu     sync.Mutex
	active int
	done   chan struct{}
}

func (t *packageUpgradeTracker) begin() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active == 0 {
		t.done = make(chan struct{})
	}
	t.active++
}

func (t *packageUpgradeTracker) end() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active == 0 {
		return
	}
	t.active--
	if t.active == 0 && t.done != nil {
		close(t.done)
		t.done = nil
	}
}

// wait reports whether a package upgrade is active and, when active, returns
// the channel that closes once the current batch of upgrades finishes.
func (t *packageUpgradeTracker) wait() (done <-chan struct{}, active bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.active == 0 {
		return nil, false
	}
	return t.done, true
}

// PrepareRestart lets the panel ask whether it is safe to stop and restart the
// agent. While the agent is upgrading system packages it streams "holdon" once
// per second; as soon as the upgrade finishes it streams "ready" and closes the
// stream. If the panel cancels the connection first, the handler returns
// without sending "ready".
func (h *Handler) PrepareRestart(_ *agentpb.Empty, stream agentpb.AgentService_PrepareRestartServer) error {
	ctx := stream.Context()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		done, active := h.upgrades.wait()
		if !active {
			return stream.Send(&agentpb.PrepareRestartResponse{State: agentcontract.PrepareRestartStateReady})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			// The upgrade batch finished; re-check in case another upgrade
			// started before the previous one completed.
		case <-ticker.C:
			if err := stream.Send(&agentpb.PrepareRestartResponse{State: agentcontract.PrepareRestartStateHoldOn}); err != nil {
				return err
			}
		}
	}
}
