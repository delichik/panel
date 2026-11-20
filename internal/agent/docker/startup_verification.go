package docker

import (
	"context"
	"fmt"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
)

const containerStartupWindow = 10 * time.Second
const containerStartupPollInterval = 500 * time.Millisecond

// A successful Docker start only acknowledges process creation. Keep the Job
// in verify_running until the same process survives the startup window, so a
// crash cannot become a succeeded Job followed by unbounded new drift Jobs.
func (r *LocalRuntime) verifyContainerStartup(ctx context.Context, req agentcontract.RuntimeReconcileRequest, spec appruntime.Spec, containerID string, allowAlreadyStable bool) (appruntime.InstanceStatus, error) {
	var last appruntime.InstanceStatus
	var firstStartedAt string
	var observedSince time.Time
	for {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		status, err := r.Status(ctx, req.InstanceID, spec.ContainerName, req.ServerID)
		if err != nil {
			return last, err
		}
		last = status
		if err := ctx.Err(); err != nil {
			return last, err
		}
		if status.Status != appruntime.StatusRunning || status.ContainerID != containerID {
			return last, fmt.Errorf("container did not reach running state")
		}
		if observedSince.IsZero() {
			observedSince = time.Now()
			firstStartedAt = status.StartedAt
			// Reconciliation of an unchanged, long-running container need not
			// wait again. A missing/invalid/future timestamp requires observation.
			if startedAt, err := time.Parse(time.RFC3339Nano, status.StartedAt); allowAlreadyStable && err == nil && !startedAt.IsZero() && time.Since(startedAt) >= containerStartupWindow {
				return last, nil
			}
		} else if status.StartedAt != firstStartedAt {
			// Detect an exit/restart between polls even if both samples say running.
			return last, fmt.Errorf("container did not reach running state")
		}
		remaining := containerStartupWindow - time.Since(observedSince)
		if remaining <= 0 {
			return last, nil
		}
		timer := time.NewTimer(min(containerStartupPollInterval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}
