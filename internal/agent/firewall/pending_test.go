package firewall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptedRunner struct {
	calls []string
}

func (r *scriptedRunner) LookPath(name string) (string, error) { return name, nil }
func (r *scriptedRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

func TestPendingGuardConfirmCancelsRollback(t *testing.T) {
	dir := t.TempDir()
	runner := &scriptedRunner{}
	guard := &PendingGuard{
		path:    filepath.Join(dir, "pending.json"),
		timeout: time.Hour,
		runner:  runner,
		logf:    func(string, ...any) {},
	}
	guard.arm(InverseOp{Action: InverseAllow, Port: 8080, Protocol: "tcp"})
	if _, err := os.Stat(guard.path); err != nil {
		t.Fatalf("pending file missing: %v", err)
	}
	guard.confirm()
	if _, err := os.Stat(guard.path); !os.IsNotExist(err) {
		t.Fatalf("pending file should be removed after confirm, err=%v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("confirm must not run ufw, calls=%v", runner.calls)
	}
}

func TestPendingGuardTimerRollsBack(t *testing.T) {
	dir := t.TempDir()
	runner := &scriptedRunner{}
	guard := &PendingGuard{
		path:    filepath.Join(dir, "pending.json"),
		timeout: 20 * time.Millisecond,
		runner:  runner,
		logf:    func(string, ...any) {},
	}
	guard.arm(InverseOp{Action: InverseAllow, Port: 8080, Protocol: "tcp", Comment: "panel:application:app"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(runner.calls) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.calls[0], "ufw allow 8080/tcp comment panel:application:app") {
		t.Fatalf("rollback calls = %#v", runner.calls)
	}
}
