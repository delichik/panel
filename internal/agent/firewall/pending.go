package firewall

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultPendingTimeout is how long the agent keeps a firewall mutation
// reversible before automatically restoring the previous rules. Panel must
// confirm within this window after verifying fresh SSH and Agent channels.
const DefaultPendingTimeout = 90 * time.Second

const (
	pendingPathEnv     = "PANEL_AGENT_FIREWALL_PENDING_PATH"
	defaultPendingPath = "/var/lib/panel-agent/firewall-pending.json"
	// InverseAllow re-adds a previously deleted allow rule.
	InverseAllow = "allow"
	// InverseDeleteAllow removes a previously added allow rule.
	InverseDeleteAllow = "delete_allow"
)

// InverseOp describes how to undo one firewall mutation.
type InverseOp struct {
	Action   string `json:"action"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	From     string `json:"from,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type pendingState struct {
	Ops       []InverseOp `json:"ops"`
	ExpiresAt time.Time   `json:"expiresAt"`
}

// PendingGuard owns the single in-process rollback timer for UFW mutations.
type PendingGuard struct {
	mu       sync.Mutex
	path     string
	timeout  time.Duration
	runner   commandRunner
	timer    *time.Timer
	ops      []InverseOp
	expires  time.Time
	logf     func(string, ...any)
}

var processPending = newPendingGuard()

func newPendingGuard() *PendingGuard {
	path := strings.TrimSpace(os.Getenv(pendingPathEnv))
	if path == "" {
		path = defaultPendingPath
	}
	return &PendingGuard{
		path:    path,
		timeout: DefaultPendingTimeout,
		runner:  localCommandRunner{},
		logf:    log.Printf,
	}
}

// ArmPending records inverse operations for the latest firewall mutation and
// (re)starts the rollback timer. Callers must already hold mutationMu.
func ArmPending(ops ...InverseOp) {
	processPending.arm(ops...)
}

// ConfirmPending cancels the rollback timer after Panel verifies connectivity.
func ConfirmPending() {
	processPending.confirm()
}

// RollbackPendingNow restores pending rules immediately. Safe to call when no
// pending mutation exists. Errors are logged and never returned to callers so
// shutdown and unrelated flows stay unblocked.
func RollbackPendingNow(ctx context.Context) {
	if err := processPending.rollback(ctx, "manual"); err != nil && processPending.logf != nil {
		processPending.logf("firewall pending rollback failed: %v", err)
	}
}

// RestorePendingOnStartup reloads a persisted pending mutation from disk so an
// agent crash mid-window still rolls back when the timer elapses (or immediately
// when already expired).
func RestorePendingOnStartup() {
	processPending.restoreOnStartup()
}

func (g *PendingGuard) arm(ops ...InverseOp) {
	if g == nil || len(ops) == 0 {
		return
	}
	cleaned := make([]InverseOp, 0, len(ops))
	for _, op := range ops {
		op.Action = strings.TrimSpace(op.Action)
		op.Protocol = strings.ToLower(strings.TrimSpace(op.Protocol))
		if op.Protocol == "" {
			op.Protocol = "tcp"
		}
		if op.Port <= 0 || op.Port > 65535 {
			continue
		}
		if op.Action != InverseAllow && op.Action != InverseDeleteAllow {
			continue
		}
		cleaned = append(cleaned, op)
	}
	if len(cleaned) == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ops = append(g.ops, cleaned...)
	g.expires = time.Now().UTC().Add(g.timeout)
	g.persistLocked()
	g.resetTimerLocked()
	if g.logf != nil {
		g.logf("firewall pending rollback armed (%d ops, expires %s)", len(g.ops), g.expires.Format(time.RFC3339))
	}
}

func (g *PendingGuard) confirm() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.ops) == 0 {
		return
	}
	if g.logf != nil {
		g.logf("firewall pending rollback confirmed (%d ops discarded)", len(g.ops))
	}
	g.clearLocked()
}

func (g *PendingGuard) restoreOnStartup() {
	if g == nil {
		return
	}
	raw, err := os.ReadFile(g.path)
	if err != nil {
		return
	}
	var state pendingState
	if err := json.Unmarshal(raw, &state); err != nil || len(state.Ops) == 0 {
		_ = os.Remove(g.path)
		return
	}
	g.mu.Lock()
	g.ops = state.Ops
	g.expires = state.ExpiresAt.UTC()
	expired := !g.expires.After(time.Now().UTC())
	g.mu.Unlock()
	if expired {
		if err := g.rollback(context.Background(), "startup-expired"); err != nil && g.logf != nil {
			g.logf("firewall pending rollback on startup failed: %v", err)
		}
		return
	}
	g.mu.Lock()
	g.resetTimerLocked()
	g.mu.Unlock()
	if g.logf != nil {
		g.logf("firewall pending rollback restored from disk (%d ops, expires %s)", len(state.Ops), state.ExpiresAt.Format(time.RFC3339))
	}
}

func (g *PendingGuard) rollback(ctx context.Context, reason string) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	ops := append([]InverseOp(nil), g.ops...)
	g.clearLocked()
	g.mu.Unlock()
	if len(ops) == 0 {
		return nil
	}
	if g.logf != nil {
		g.logf("firewall pending rollback starting (%s, %d ops)", reason, len(ops))
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	var first error
	for i := len(ops) - 1; i >= 0; i-- {
		if err := applyInverseOp(ctx, g.runner, ops[i]); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (g *PendingGuard) resetTimerLocked() {
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	delay := time.Until(g.expires)
	if delay < 0 {
		delay = 0
	}
	g.timer = time.AfterFunc(delay, func() {
		if err := g.rollback(context.Background(), "timer"); err != nil && g.logf != nil {
			g.logf("firewall pending timer rollback failed: %v", err)
		}
	})
}

func (g *PendingGuard) clearLocked() {
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.ops = nil
	g.expires = time.Time{}
	_ = os.Remove(g.path)
}

func (g *PendingGuard) persistLocked() {
	if g.path == "" {
		return
	}
	state := pendingState{Ops: append([]InverseOp(nil), g.ops...), ExpiresAt: g.expires}
	raw, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
		if g.logf != nil {
			g.logf("firewall pending persist mkdir failed: %v", err)
		}
		return
	}
	tmp := g.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		if g.logf != nil {
			g.logf("firewall pending persist write failed: %v", err)
		}
		return
	}
	if err := os.Rename(tmp, g.path); err != nil {
		_ = os.Remove(tmp)
		if g.logf != nil {
			g.logf("firewall pending persist rename failed: %v", err)
		}
	}
}

func applyInverseOp(ctx context.Context, runner commandRunner, op InverseOp) error {
	if runner == nil {
		return fmt.Errorf("firewall runner is not configured")
	}
	protocol := op.Protocol
	if protocol == "" {
		protocol = "tcp"
	}
	target := strconv.Itoa(op.Port) + "/" + protocol
	switch op.Action {
	case InverseAllow:
		args := []string{"allow", target}
		if comment := strings.TrimSpace(op.Comment); comment != "" {
			args = append(args, "comment", comment)
		}
		from := strings.TrimSpace(op.From)
		if from != "" && !strings.EqualFold(from, "anywhere") && !strings.HasPrefix(strings.ToLower(from), "anywhere") {
			args = []string{"allow", "from", from, "to", "any", "port", strconv.Itoa(op.Port), "proto", protocol}
			if comment := strings.TrimSpace(op.Comment); comment != "" {
				args = append(args, "comment", comment)
			}
		}
		_, err := runner.Run(ctx, "ufw", args...)
		return err
	case InverseDeleteAllow:
		args := []string{"--force", "delete", "allow", target}
		if comment := strings.TrimSpace(op.Comment); comment != "" {
			args = append(args, "comment", comment)
		}
		_, err := runner.Run(ctx, "ufw", args...)
		return err
	default:
		return fmt.Errorf("unknown firewall inverse action %q", op.Action)
	}
}
