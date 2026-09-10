package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	installed bool
	added     string
	numbered  []string
	calls     [][]string
}

func (r *fakeRunner) LookPath(string) (string, error) {
	if !r.installed {
		return "", errors.New("missing")
	}
	return "/usr/sbin/ufw", nil
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	joined := strings.Join(args, " ")
	if joined == "show added" {
		return r.added, nil
	}
	if joined == "status numbered" {
		if len(r.numbered) == 0 {
			return "Status: inactive", nil
		}
		out := r.numbered[0]
		if len(r.numbered) > 1 {
			r.numbered = r.numbered[1:]
		}
		return out, nil
	}
	if len(args) >= 2 && args[0] == "allow" {
		line := "ufw allow " + args[1]
		if len(args) == 4 && args[2] == "comment" {
			line += " comment '" + args[3] + "'"
		}
		r.added = strings.TrimSpace(r.added + "\n" + line)
	}
	if len(args) == 6 && args[0] == "--force" && args[1] == "delete" && args[2] == "allow" && args[4] == "comment" {
		needle := "ufw allow " + args[3] + " comment '" + args[5] + "'"
		lines := strings.Split(r.added, "\n")
		kept := lines[:0]
		for _, line := range lines {
			if strings.TrimSpace(line) != needle {
				kept = append(kept, line)
			}
		}
		r.added = strings.Join(kept, "\n")
	}
	return "", nil
}

func TestEnsureApplicationRulesVerifiesCreatedRule(t *testing.T) {
	runner := &fakeRunner{installed: true}
	manager := newWithRunner(runner)
	if err := manager.EnsureApplicationRules(context.Background(), "app-a", 22, 9786, []Rule{{Port: 5353, Protocol: "udp"}}); err != nil {
		t.Fatal(err)
	}
	showCalls := 0
	for _, call := range runner.calls {
		if strings.Join(call, " ") == "ufw show added" {
			showCalls++
		}
	}
	if showCalls != 2 {
		t.Fatalf("show added calls = %d, want initial read and verification", showCalls)
	}
}

func TestEnsureApplicationRulesRespectsManualAndRejectsOtherOwner(t *testing.T) {
	runner := &fakeRunner{installed: true, added: "ufw allow 8080/tcp\nufw allow 5353/udp comment 'panel:application:other'\n"}
	manager := newWithRunner(runner)
	if err := manager.EnsureApplicationRules(context.Background(), "app-a", 22, 9786, []Rule{{Port: 8080}, {Port: 5353, Protocol: "udp"}}); err == nil || !strings.Contains(err.Error(), "another Panel application") {
		t.Fatalf("expected ownership conflict, got %v", err)
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), "allow 8080") {
			t.Fatalf("manual rule should not be adopted: %#v", runner.calls)
		}
	}
}

func TestEnsureApplicationRulesRequiresInstalledAndProtectsSSH(t *testing.T) {
	manager := newWithRunner(&fakeRunner{})
	if err := manager.EnsureApplicationRules(context.Background(), "app-a", 22, 9786, []Rule{{Port: 8080}}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected not installed, got %v", err)
	}
	manager = newWithRunner(&fakeRunner{installed: true})
	if err := manager.EnsureApplicationRules(context.Background(), "app-a", 22022, 9786, []Rule{{Port: 22022}}); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("expected SSH protection, got %v", err)
	}
	if err := manager.EnsureApplicationRules(context.Background(), "app-a", 22, 10986, []Rule{{Port: 10986}}); err == nil || !strings.Contains(err.Error(), "Agent") {
		t.Fatalf("expected Agent protection, got %v", err)
	}
}

func TestCleanupDeletesOnlyOwnedStaleRules(t *testing.T) {
	runner := &fakeRunner{installed: true, added: strings.Join([]string{
		"ufw allow 8080/tcp comment 'panel:application:app-a'",
		"ufw allow 9090/udp comment 'panel:application:app-a'",
		"ufw allow 7070/tcp comment 'panel:application:other'",
	}, "\n")}
	manager := newWithRunner(runner)
	if err := manager.CleanupApplicationRules(context.Background(), "app-a", 22, 9786, []Rule{{Port: 8080, Protocol: "tcp"}}); err != nil {
		t.Fatal(err)
	}
	joined := ""
	for _, call := range runner.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "ufw --force delete allow 9090/udp comment panel:application:app-a") || strings.Contains(joined, "delete allow 7070") || strings.Contains(joined, "delete allow 8080") {
		t.Fatalf("unexpected calls:\n%s", joined)
	}
}

func TestDeleteRuleFailsClosedForSSHManagedAndChangedRule(t *testing.T) {
	ssh := &fakeRunner{installed: true, numbered: []string{"[ 1] 22022/tcp  ALLOW IN  Anywhere\n"}}
	if err := newWithRunner(ssh).DeleteRule(context.Background(), 1, 22022, 9786); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("expected SSH rejection, got %v", err)
	}
	managed := &fakeRunner{installed: true, numbered: []string{"[ 2] 8080/tcp  ALLOW IN  Anywhere # panel:application:app-a\n"}}
	if err := newWithRunner(managed).DeleteRule(context.Background(), 2, 22, 9786); err == nil || !strings.Contains(err.Error(), "managed") {
		t.Fatalf("expected managed rejection, got %v", err)
	}
	changed := &fakeRunner{installed: true, numbered: []string{
		"[ 3] 8080/tcp  ALLOW IN  Anywhere\n",
		"[ 3] 9090/tcp  ALLOW IN  Anywhere\n",
	}}
	if err := newWithRunner(changed).DeleteRule(context.Background(), 3, 22, 9786); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected changed rejection, got %v", err)
	}
}
