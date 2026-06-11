package remoteops

import (
	"strings"
	"testing"
)

func TestAPTInstallScriptValidatesPackages(t *testing.T) {
	script, err := APTInstallScript([]string{"ufw", "docker.io"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "apt_get install -y ufw docker.io") {
		t.Fatalf("unexpected install script:\n%s", script)
	}
	if _, err := APTInstallScript([]string{"bad;name"}); err == nil {
		t.Fatal("expected invalid package name")
	}
}

func TestUFWAllowScriptBuildsPortRules(t *testing.T) {
	script, err := UFWAllowScript([]UFWRule{
		{Port: 22, Protocol: "tcp"},
		{Port: 53, Protocol: "udp", From: "10.0.0.0/8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ufw allow 22/tcp", "ufw allow from '10.0.0.0/8' to any port 53 proto udp"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func TestUFWEnableScriptAllowsSSHBeforeEnable(t *testing.T) {
	script, err := UFWEnableScript(22022)
	if err != nil {
		t.Fatal(err)
	}
	allowIndex := strings.Index(script, "ufw allow 22022/tcp")
	enableIndex := strings.Index(script, "ufw --force enable")
	if allowIndex < 0 || enableIndex < 0 || allowIndex >= enableIndex {
		t.Fatalf("expected SSH allow before UFW enable:\n%s", script)
	}
}

func TestRestartScriptSchedulesDetachedRestart(t *testing.T) {
	script := RestartScript()
	for _, want := range []string{
		"sleep 1; systemctl reboot",
		"sleep 1; shutdown -r now",
		">/dev/null 2>&1 &",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("restart script missing %q:\n%s", want, script)
		}
	}
}

func TestParseUFWStatus(t *testing.T) {
	status := ParseUFWStatus(`panel_ufw_installed=true
Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
panel_ufw_numbered_begin
Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80/tcp                     ALLOW IN    Anywhere
[ 3] 53/udp                     ALLOW IN    10.0.0.0/8`)

	if !status.Installed || !status.Active || status.Status != "active" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if status.Default != "deny (incoming), allow (outgoing), disabled (routed)" {
		t.Fatalf("unexpected default policy: %q", status.Default)
	}
	if len(status.Rules) != 3 || status.Rules[2].Number != 3 || status.Rules[2].From != "10.0.0.0/8" {
		t.Fatalf("unexpected rules: %#v", status.Rules)
	}
}

func TestWriteFileSudoScriptRequiresAbsolutePath(t *testing.T) {
	if _, err := WriteFileSudoScript("relative.conf", []byte("x"), "0644"); err == nil {
		t.Fatal("expected absolute path validation")
	}
	script, err := WriteFileSudoScript("/etc/panel/example.conf", []byte("hello"), "0640")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "install -d -m 0755 '/etc/panel'") || !strings.Contains(script, "install -m 0640") {
		t.Fatalf("unexpected write script:\n%s", script)
	}
}
