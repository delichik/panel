package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/linux"
)

const (
	aptStubHelperEnv      = "PANEL_TEST_APT_STUB"
	aptStubListOutputEnv  = "PANEL_TEST_APT_STUB_LIST"
	aptStubCacheOutputEnv = "PANEL_TEST_APT_STUB_CACHE"
	aptStubCacheFailEnv   = "PANEL_TEST_APT_STUB_CACHE_FAIL"
	aptStubSimOutputEnv   = "PANEL_TEST_APT_STUB_SIM"
	aptStubSimFailEnv     = "PANEL_TEST_APT_STUB_SIM_FAIL"
	aptStubArgsLogEnv     = "PANEL_TEST_APT_STUB_ARGS_LOG"
	aptStubPrefDumpEnv    = "PANEL_TEST_APT_STUB_PREF_DUMP"
)

// runAptStubHelper 是假 apt 子进程：按 os.Args[0] 区分 apt-get / apt / apt-cache，
// 并记录调用参数与临时 preferences 文件内容以供断言。
func runAptStubHelper() {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	if logPath := os.Getenv(aptStubArgsLogEnv); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(base + " " + strings.Join(os.Args[1:], " ") + "\n")
			_ = f.Close()
		}
	}
	if dumpPath := os.Getenv(aptStubPrefDumpEnv); dumpPath != "" {
		for _, arg := range os.Args[1:] {
			if strings.HasPrefix(arg, "Dir::Etc::Preferences=") {
				prefFile := strings.TrimPrefix(arg, "Dir::Etc::Preferences=")
				if data, err := os.ReadFile(prefFile); err == nil {
					_ = os.WriteFile(dumpPath, data, 0o644)
				}
			}
		}
	}

	switch {
	case strings.Contains(base, "apt-cache"):
		_, _ = os.Stdout.WriteString(os.Getenv(aptStubCacheOutputEnv))
		if os.Getenv(aptStubCacheFailEnv) == "1" {
			os.Exit(1)
		}
	case strings.HasPrefix(base, "apt-get"):
		isSim := false
		for _, arg := range os.Args[1:] {
			if arg == "-s" {
				isSim = true
				break
			}
		}
		if isSim {
			_, _ = os.Stdout.WriteString(os.Getenv(aptStubSimOutputEnv))
			if os.Getenv(aptStubSimFailEnv) == "1" {
				os.Exit(1)
			}
		}
	case strings.HasPrefix(base, "apt"):
		_, _ = os.Stdout.WriteString(os.Getenv(aptStubListOutputEnv))
	}
	os.Exit(0)
}

func installAptStub(t *testing.T) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"apt-get", "apt", "apt-cache"} {
		target := filepath.Join(dir, executableName(name))
		if err := copyFile(exe, target); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prependTestPath(t, dir)
	t.Setenv(aptStubHelperEnv, "1")
}

func prependTestPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const debianGlobalPolicyOutput = `Package files:
 100 /var/lib/dpkg/status
     release a=now
 500 http://mirrors.aliyun.com/debian-security bookworm-security/main amd64 Packages
     release v=12,o=Debian,a=stable-security,n=bookworm-security,l=Debian-Security,c=main,b=amd64
     origin mirrors.aliyun.com
 500 http://mirrors.aliyun.com/debian bookworm-updates/main amd64 Packages
     release v=12-updates,o=Debian,a=stable-updates,n=bookworm-updates,l=Debian,c=main,b=amd64
     origin mirrors.aliyun.com
 500 http://mirrors.aliyun.com/debian bookworm/main amd64 Packages
     release v=12.7,o=Debian,a=stable,n=bookworm,l=Debian,c=main,b=amd64
     origin mirrors.aliyun.com
Pinned packages:
`

const ubuntuGlobalPolicyOutput = `Package files:
 100 /var/lib/dpkg/status
     release a=now
 500 http://mirrors.tuna.tsinghua.edu.cn/ubuntu noble-security/main amd64 Packages
     release v=24.04,o=Ubuntu,a=noble-security,n=noble,l=Ubuntu,c=main,b=amd64
     origin mirrors.tuna.tsinghua.edu.cn
 500 http://mirrors.tuna.tsinghua.edu.cn/ubuntu noble-updates/main amd64 Packages
     release v=24.04,o=Ubuntu,a=noble-updates,n=noble,l=Ubuntu,c=main,b=amd64
     origin mirrors.tuna.tsinghua.edu.cn
 500 https://esm.ubuntu.com/apps/ubuntu noble-apps-security/main amd64 Packages
     release v=24.04,o=UbuntuESMApps,a=noble-apps-security,n=noble,l=UbuntuESMApps,c=main,b=amd64
     origin esm.ubuntu.com
`

func TestParseGlobalAptPolicySecurityReleases(t *testing.T) {
	debReleases := parseGlobalAptPolicySecurityReleases(debianGlobalPolicyOutput)
	if len(debReleases) != 1 {
		t.Fatalf("debian security releases = %#v, want 1", debReleases)
	}
	if debReleases[0].Label != "Debian-Security" || debReleases[0].Archive != "stable-security" {
		t.Fatalf("unexpected debian security release: %#v", debReleases[0])
	}

	ubuReleases := parseGlobalAptPolicySecurityReleases(ubuntuGlobalPolicyOutput)
	if len(ubuReleases) != 2 {
		t.Fatalf("ubuntu security releases = %#v, want 2 (noble-security + esm)", ubuReleases)
	}
}

func TestRenderSecurityPreferencesOrderAndPriority(t *testing.T) {
	releases := parseGlobalAptPolicySecurityReleases(debianGlobalPolicyOutput)
	pref := renderSecurityPreferences(releases)

	secIdx := strings.Index(pref, "Pin: release o=Debian,a=stable-security,n=bookworm-security,l=Debian-Security\nPin-Priority: 500")
	nowIdx := strings.Index(pref, "Pin: release a=now\nPin-Priority: 100")
	catchAllIdx := strings.Index(pref, "Pin: version /.*/\nPin-Priority: 99")

	if secIdx < 0 || nowIdx < 0 || catchAllIdx < 0 {
		t.Fatalf("missing expected pin stanzas in:\n%s", pref)
	}
	// APT general-form 采用 first-match-wins：安全源(500) < 已安装(100) < 非安全兜底(99)。
	if !(secIdx < nowIdx && nowIdx < catchAllIdx) {
		t.Fatalf("invalid pin ordering: sec=%d now=%d catchAll=%d", secIdx, nowIdx, catchAllIdx)
	}
}

func TestParseAptSimulateInst(t *testing.T) {
	simOut := `NOTE: This is only a simulation!
Reading package lists...
Building dependency tree...
The following NEW packages will be installed:
  linux-image-6.8.0-49-generic
The following packages will be upgraded:
  linux-image-generic openssl
Inst linux-image-6.8.0-49-generic (6.8.0-49.49 Ubuntu:24.04/noble-security [amd64])
Inst linux-image-generic [6.8.0-48.48] (6.8.0-49.49 Ubuntu:24.04/noble-security [amd64])
Inst openssl:amd64 [3.0.13-0ubuntu3.1] (3.0.13-0ubuntu3.4 Ubuntu:24.04/noble-security [amd64])
Conf linux-image-6.8.0-49-generic (6.8.0-49.49 Ubuntu:24.04/noble-security [amd64])
`
	got := parseAptSimulateInst(simOut)
	for _, want := range []string{"linux-image-6.8.0-49-generic", "linux-image-generic", "openssl:amd64", "openssl"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("expected %q in parsed Inst set, got %#v", want, got)
		}
	}
	if _, ok := got["curl"]; ok {
		t.Fatalf("unexpected non-security package in Inst set: %#v", got)
	}
}

func TestPackageUpdatesUsesAptPinningSimulation(t *testing.T) {
	installAptStub(t)
	prefDump := filepath.Join(t.TempDir(), "pref.dump")
	t.Setenv(aptStubPrefDumpEnv, prefDump)
	t.Setenv(aptStubCacheOutputEnv, ubuntuGlobalPolicyOutput)
	t.Setenv(aptStubListOutputEnv, strings.Join([]string{
		"Listing...",
		// 即使 apt list 展示的 Candidate 来源是 noble-updates，只要安全 Pin 模拟会升级它，就判定为安全更新。
		"openssl/noble-updates 3.0.13-0ubuntu3.5 amd64 [upgradable from: 3.0.13-0ubuntu3.1]",
		"curl/noble-updates 8.5.0-2ubuntu10.4 amd64 [upgradable from: 8.5.0-2ubuntu10.1]",
	}, "\n"))
	t.Setenv(aptStubSimOutputEnv, "Inst openssl [3.0.13-0ubuntu3.1] (3.0.13-0ubuntu3.4 Ubuntu:24.04/noble-security [amd64])\n")

	updates, err := (LocalCollector{}).PackageUpdates(context.Background())
	if err != nil {
		t.Fatalf("PackageUpdates error: %v", err)
	}
	byName := map[string]linux.PackageUpdate{}
	for _, u := range updates {
		byName[u.Name] = u
	}
	if !byName["openssl"].SecurityKnown || !byName["openssl"].Security {
		t.Fatalf("openssl should be verified security update: %#v", byName["openssl"])
	}
	if !byName["curl"].SecurityKnown || byName["curl"].Security {
		t.Fatalf("curl should be verified regular update: %#v", byName["curl"])
	}
	prefData, err := os.ReadFile(prefDump)
	if err != nil {
		t.Fatalf("expected temporary preferences file to be passed to apt-get -s: %v", err)
	}
	if !strings.Contains(string(prefData), "Pin-Priority: 99") {
		t.Fatalf("unexpected preferences content:\n%s", string(prefData))
	}
}

func TestPackageUpdatesSimulateFailureKeepsUpdatesUnknown(t *testing.T) {
	installAptStub(t)
	t.Setenv(aptStubListOutputEnv, "Listing...\nopenssl/noble-security 3.0.13-0ubuntu3.4 amd64 [upgradable from: 3.0.13-0ubuntu3.1]\n")
	t.Setenv(aptStubSimFailEnv, "1")

	updates, err := (LocalCollector{}).PackageUpdates(context.Background())
	if err != nil {
		t.Fatalf("simulate failure must not fail PackageUpdates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %#v", updates)
	}
	if updates[0].SecurityKnown || updates[0].Security {
		t.Fatalf("failed simulation must leave SecurityKnown=false, Security=false: %#v", updates[0])
	}
}

func TestUpgradePackagesSecurityOnlyUsesPinnedUpgradeWithNewPkgs(t *testing.T) {
	installAptStub(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	t.Setenv(aptStubArgsLogEnv, argsLog)
	t.Setenv(aptStubCacheOutputEnv, debianGlobalPolicyOutput)
	t.Setenv(aptStubSimOutputEnv, "Inst openssl [3.0.11-1~deb12u1] (3.0.11-1~deb12u2 Debian-Security:12/stable-security [amd64])\n")

	if _, err := (LocalCollector{}).UpgradePackages(context.Background(), agentcontract.PackageUpgradeRequest{SecurityOnly: true}); err != nil {
		t.Fatalf("UpgradePackages(SecurityOnly) error: %v", err)
	}
	raw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(raw)
	if !strings.Contains(logText, "update") {
		t.Fatalf("expected apt-get update before security upgrade, got:\n%s", logText)
	}
	if !strings.Contains(logText, "Dir::Etc::PreferencesParts=-") || !strings.Contains(logText, "upgrade --with-new-pkgs -y") {
		t.Fatalf("expected pinned apt-get upgrade --with-new-pkgs -y, got:\n%s", logText)
	}
	if strings.Contains(logText, "dist-upgrade") || strings.Contains(logText, "--only-upgrade") {
		t.Fatalf("security-only upgrade must not use dist-upgrade or --only-upgrade, got:\n%s", logText)
	}
}

func TestUpgradePackagesSecurityOnlyShortCircuitsWhenEmpty(t *testing.T) {
	installAptStub(t)
	argsLog := filepath.Join(t.TempDir(), "args.log")
	t.Setenv(aptStubArgsLogEnv, argsLog)
	t.Setenv(aptStubCacheOutputEnv, debianGlobalPolicyOutput)
	t.Setenv(aptStubSimOutputEnv, "0 upgraded, 0 newly installed, 0 to remove and 5 not upgraded.\n")

	out, err := (LocalCollector{}).UpgradePackages(context.Background(), agentcontract.PackageUpgradeRequest{SecurityOnly: true})
	if err != nil {
		t.Fatalf("UpgradePackages(SecurityOnly) empty error: %v", err)
	}
	if !strings.Contains(out, "No security updates available") {
		t.Fatalf("expected no-op message, got %q", out)
	}
	raw, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "upgrade --with-new-pkgs -y") {
		t.Fatalf("empty security set must not execute write upgrade, got:\n%s", string(raw))
	}
}
