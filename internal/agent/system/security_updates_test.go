package system

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/platform/linux"
)

const (
	aptStubHelperEnv      = "PANEL_TEST_APT_STUB"
	aptStubListOutputEnv  = "PANEL_TEST_APT_STUB_LIST"
	aptStubCacheOutputEnv = "PANEL_TEST_APT_STUB_CACHE"
	aptStubCacheFailEnv   = "PANEL_TEST_APT_STUB_CACHE_FAIL"
)

// runAptStubHelper 是假 apt 子进程：按 os.Args[0] 的可执行文件名区分
// apt-get/apt（可升级列表）与 apt-cache（安全元数据）。TestMain 在子进程中调用。
func runAptStubHelper() {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	switch {
	case strings.Contains(base, "apt-cache"):
		_, _ = os.Stdout.WriteString(os.Getenv(aptStubCacheOutputEnv))
		if os.Getenv(aptStubCacheFailEnv) == "1" {
			os.Exit(1)
		}
	case strings.HasPrefix(base, "apt-get"):
		// apt-get update：成功且无输出即可。
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

// useAptSecurityFixtures 把 Agent 读取的 unattended-upgrades 配置与 os-release
// 指向测试目录，避免测试依赖宿主机真实 apt 配置。
func useAptSecurityFixtures(t *testing.T, allowedOrigins, osRelease string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "50unattended-upgrades")
	if err := os.WriteFile(configPath, []byte(allowedOrigins), 0o644); err != nil {
		t.Fatal(err)
	}
	osReleasePath := filepath.Join(dir, "os-release")
	if err := os.WriteFile(osReleasePath, []byte(osRelease), 0o644); err != nil {
		t.Fatal(err)
	}
	oldGlob, oldOSRelease := aptSecurityAllowedOriginsGlob, aptSecurityOSReleasePath
	aptSecurityAllowedOriginsGlob = filepath.Join(dir, "*unattended*")
	aptSecurityOSReleasePath = osReleasePath
	t.Cleanup(func() {
		aptSecurityAllowedOriginsGlob = oldGlob
		aptSecurityOSReleasePath = oldOSRelease
	})
}

const ubuntuPolicyOutput = `openssl:
  Installed: 3.0.13-0ubuntu3.1
  Candidate: 3.0.13-0ubuntu3.4
  Version table:
     3.0.13-0ubuntu3.4 500
        500 http://security.ubuntu.com/ubuntu noble-security/main amd64 Packages
     3.0.13-0ubuntu3.2 500
        500 http://archive.ubuntu.com/ubuntu noble-updates/main amd64 Packages
     3.0.13-0ubuntu3.1 100
        100 /var/lib/dpkg/status
libssl3:
  Installed: 3.0.13-0ubuntu3.4
  Candidate: 3.0.13-0ubuntu3.5
  Version table:
     3.0.13-0ubuntu3.5 500
        500 http://archive.ubuntu.com/ubuntu noble-updates/main amd64 Packages
     3.0.13-0ubuntu3.4 500
        500 http://security.ubuntu.com/ubuntu noble-security/main amd64 Packages
        100 /var/lib/dpkg/status
libc6:
  Installed: 2.39-0ubuntu8
  Candidate: (none)
  Version table:
     2.39-0ubuntu8 100
        100 /var/lib/dpkg/status
`

const debianPolicyOutput = `tcpdump:
  Installed: 4.99.3-1
  Candidate: 4.99.3-1+deb12u1
  Version table:
     4.99.3-1+deb12u1 500
        500 http://deb.debian.org/debian-security bookworm-security/main amd64 Packages
         release o=Debian,a=stable-security,n=bookworm-security,l=Debian-Security,c=main,b=amd64
vim:
  Installed: 9.0.1378-2
  Candidate: 9.0.1650-1~deb12u1
  Version table:
     9.0.1650-1~deb12u1 500
        500 http://deb.debian.org/debian bookworm/main amd64 Packages
     9.0.1378-2 500
        500 http://security.debian.org/debian-security bookworm-security/main amd64 Packages
         release o=Debian,a=stable-security,n=bookworm-security,l=Debian-Security,c=main,b=amd64
        100 /var/lib/dpkg/status
`

const ubuntuAllowedOrigins = `// Unattended-Upgrade::Allowed-Origins {
Unattended-Upgrade::Allowed-Origins {
	"${distro_id}:${distro_codename}";
	"${distro_id}:${distro_codename}-security";
	"${distro_id}ESMApps:${distro_codename}-apps-security";
	"${distro_id}ESM:${distro_codename}-infra-security";
//	"${distro_id}:${distro_codename}-updates";
};
`

const ubuntuOSRelease = `NAME="Ubuntu"
ID=ubuntu
VERSION_CODENAME=noble
UBUNTU_CODENAME=noble
`

// TestParseAptCachePolicyKeepsOnlyCandidateSources 锁定解析语义：只取
// Candidate 版本的来源，多版本包不能把 installed/旧安全版本的来源算进来。
func TestParseAptCachePolicyKeepsOnlyCandidateSources(t *testing.T) {
	candidates := parseAptCachePolicy(ubuntuPolicyOutput)
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want openssl and libssl3 only", candidates)
	}
	openssl, ok := candidates["openssl"]
	if !ok {
		t.Fatalf("openssl candidate missing: %#v", candidates)
	}
	if openssl.Version != "3.0.13-0ubuntu3.4" {
		t.Fatalf("openssl candidate version = %q", openssl.Version)
	}
	if len(openssl.Sources) != 1 || openssl.Sources[0].Suite != "noble-security" || openssl.Sources[0].Component != "main" {
		t.Fatalf("openssl candidate sources = %#v", openssl.Sources)
	}
	libssl3, ok := candidates["libssl3"]
	if !ok {
		t.Fatalf("libssl3 candidate missing: %#v", candidates)
	}
	if len(libssl3.Sources) != 1 || libssl3.Sources[0].Suite != "noble-updates" {
		t.Fatalf("libssl3 must be classified by Candidate (%s), got sources %#v", libssl3.Version, libssl3.Sources)
	}
	if _, ok := candidates["libc6"]; ok {
		t.Fatalf("package without candidate must be omitted: %#v", candidates["libc6"])
	}
}

// TestParseAptCachePolicyParsesReleaseLines 覆盖 Debian 输出中 release
// o=...,a=...,n=...,l=... 行的解析。
func TestParseAptCachePolicyParsesReleaseLines(t *testing.T) {
	candidates := parseAptCachePolicy(debianPolicyOutput)
	tcpdump := candidates["tcpdump"]
	if len(tcpdump.Sources) != 1 {
		t.Fatalf("tcpdump sources = %#v", tcpdump.Sources)
	}
	release := tcpdump.Sources[0].Release
	if release.Origin != "Debian" || release.Archive != "stable-security" || release.Codename != "bookworm-security" || release.Label != "Debian-Security" {
		t.Fatalf("tcpdump release = %#v", release)
	}
	if tcpdump.Sources[0].Suite != "bookworm-security" {
		t.Fatalf("tcpdump suite = %q", tcpdump.Sources[0].Suite)
	}
}

// TestParseAptCachePolicySecurityWithAllowedOrigins 验证 Allowed-Origins 命中
// 安全归档才算安全，未命中的 Candidate（updates 源）是“已知非安全”。
func TestParseAptCachePolicySecurityWithAllowedOrigins(t *testing.T) {
	origins := securityOriginPatterns(parseAllowedOrigins(ubuntuAllowedOrigins, parseOSRelease(ubuntuOSRelease)))
	if len(origins) == 0 {
		t.Fatal("expected security allowed origins")
	}
	infos := parseAptCachePolicySecurity(ubuntuPolicyOutput, origins)
	assertSecurityInfo(t, infos, "openssl", true, true)
	assertSecurityInfo(t, infos, "libssl3", false, true)
	if info, ok := infos["libc6"]; ok && info.Known {
		t.Fatalf("libc6 has no candidate and must stay unknown: %#v", info)
	}
}

// TestParseAptCachePolicySecurityAllowedOriginsMismatch 验证允许 origin 存在但
// Candidate 不在其中时是确定的“非安全”，而不是未知。
func TestParseAptCachePolicySecurityAllowedOriginsMismatch(t *testing.T) {
	origins := securityOriginPatterns(parseAllowedOrigins(`Unattended-Upgrade::Allowed-Origins { "Ubuntu:noble-security"; };`, parseOSRelease(ubuntuOSRelease)))
	infos := parseAptCachePolicySecurity(ubuntuPolicyOutput, origins)
	assertSecurityInfo(t, infos, "libssl3", false, true)
}

// TestParseAptCachePolicySecurityFallbackMetadata 验证没有可用 Allowed-Origins
// 时只按 apt 元数据中的安全归档命名/官方安全站点判定。
func TestParseAptCachePolicySecurityFallbackMetadata(t *testing.T) {
	infos := parseAptCachePolicySecurity(ubuntuPolicyOutput, nil)
	assertSecurityInfo(t, infos, "openssl", true, true)
	assertSecurityInfo(t, infos, "libssl3", false, true)

	debian := parseAptCachePolicySecurity(debianPolicyOutput, nil)
	assertSecurityInfo(t, debian, "tcpdump", true, true)
	assertSecurityInfo(t, debian, "vim", false, true)
}

// TestParseAptCachePolicySecurityESMFallback 验证官方 ESM 站点 + 安全 suite 的
// 回退判定（Ubuntu Pro noble-apps-security）。
func TestParseAptCachePolicySecurityESMFallback(t *testing.T) {
	output := `esm-apps:
  Installed: 1.0
  Candidate: 1.1
  Version table:
     1.1 500
        500 https://esm.ubuntu.com/apps/ubuntu noble-apps-security/main amd64 Packages
`
	infos := parseAptCachePolicySecurity(output, nil)
	assertSecurityInfo(t, infos, "esm-apps", true, true)
}

// TestParseAptCachePolicySecurityThirdPartyIsKnownNonSecurity 验证可解析的第三方
// 来源是“已知非安全”，而没有任何可解析归档的本地来源是未知。
func TestParseAptCachePolicySecurityThirdPartyIsKnownNonSecurity(t *testing.T) {
	output := `vendor:
  Installed: 1.0
  Candidate: 1.1
  Version table:
     1.1 500
        500 https://packages.example.com/stable stable/main amd64 Packages
local:
  Installed: 1.0
  Candidate: 1.1
  Version table:
     1.1 100
        100 /var/lib/dpkg/status
`
	infos := parseAptCachePolicySecurity(output, nil)
	assertSecurityInfo(t, infos, "vendor", false, true)
	if info, ok := infos["local"]; ok && info.Known {
		t.Fatalf("local source without archive metadata must stay unknown: %#v", info)
	}
}

// TestParseAllowedOriginsExpandsDistroVariables 覆盖变量展开与“只保留 security
// 归档规则”的过滤（updates/proposed 不算安全）。
func TestParseAllowedOriginsExpandsDistroVariables(t *testing.T) {
	osRelease := parseOSRelease(ubuntuOSRelease)
	patterns := parseAllowedOrigins(ubuntuAllowedOrigins, osRelease)
	if len(patterns) != 4 {
		t.Fatalf("parsed patterns = %#v, want 4", patterns)
	}
	security := securityOriginPatterns(patterns)
	if len(security) != 3 {
		t.Fatalf("security patterns = %#v, want 3", security)
	}
	for _, pattern := range security {
		if !strings.Contains(strings.ToLower(pattern.Archive), "security") {
			t.Fatalf("non-security pattern leaked into security set: %#v", pattern)
		}
		if pattern.Origin == "ubuntu" && pattern.Archive == "noble-security" {
			continue
		}
		if pattern.Archive == "noble-apps-security" || pattern.Archive == "noble-infra-security" {
			continue
		}
		t.Fatalf("unexpected security pattern: %#v", pattern)
	}
}

// TestParseAllowedOriginsSupportsOriginsPattern 覆盖 Debian 2.x 的
// Unattended-Upgrade::Origins-Pattern（o=...,n=...,l=... 形式）。
func TestParseAllowedOriginsSupportsOriginsPattern(t *testing.T) {
	text := `Unattended-Upgrade::Origins-Pattern {
        "origin=Debian,codename=${distro_codename},label=Debian";
        "origin=Debian,codename=${distro_codename},label=Debian-Security";
        "origin=Debian,codename=${distro_codename}-security,label=Debian-Security";
};
`
	osRelease := parseOSRelease("ID=debian\nVERSION_CODENAME=bookworm\n")
	security := securityOriginPatterns(parseAllowedOrigins(text, osRelease))
	if len(security) != 2 {
		t.Fatalf("security patterns = %#v, want 2", security)
	}
	infos := parseAptCachePolicySecurity(debianPolicyOutput, security)
	assertSecurityInfo(t, infos, "tcpdump", true, true)
	assertSecurityInfo(t, infos, "vim", false, true)
}

// TestParseAllowedOriginsSupportsDoubleColonBlock 覆盖 Allowed-Origins:: 块的
// 兼容写法。
func TestParseAllowedOriginsSupportsDoubleColonBlock(t *testing.T) {
	text := `Unattended-Upgrade::Allowed-Origins:: {
        "Ubuntu:noble-security";
};
`
	patterns := parseAllowedOrigins(text, nil)
	if len(patterns) != 1 || patterns[0].Origin != "Ubuntu" || patterns[0].Archive != "noble-security" {
		t.Fatalf("patterns = %#v", patterns)
	}
}

// TestParseAllowedOriginsIgnoresCommentsAndUnknownVariables 覆盖注释剔除与变量
// 无法展开时丢弃规则（避免用永不匹配的伪策略把安全包误判成非安全）。
func TestParseAllowedOriginsIgnoresCommentsAndUnknownVariables(t *testing.T) {
	text := `// Unattended-Upgrade::Allowed-Origins { "Ubuntu:never"; };
Unattended-Upgrade::Allowed-Origins {
    // "Ubuntu:noble-security";
    "${distro_id}:${distro_codename}-security";
    "Ubuntu:noble-security"; // 行内注释
};
`
	patterns := parseAllowedOrigins(text, map[string]string{})
	if len(patterns) != 1 || patterns[0].Archive != "noble-security" {
		t.Fatalf("patterns = %#v, want only the literal production rules", patterns)
	}
}

// TestLoadAllowedOriginsMissingConfigIsEmpty 验证配置文件缺失时不报错、无策略。
func TestLoadAllowedOriginsMissingConfigIsEmpty(t *testing.T) {
	patterns := loadAllowedOrigins([]string{filepath.Join(t.TempDir(), "missing")}, parseOSRelease(ubuntuOSRelease))
	if len(patterns) != 0 {
		t.Fatalf("missing config must yield no patterns, got %#v", patterns)
	}
}

// TestBatchPackageNames 验证大批量包名分批，避免 apt-cache policy 参数过长。
func TestBatchPackageNames(t *testing.T) {
	names := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		names = append(names, strings.Repeat("p", 60))
	}
	batches := batchPackageNames(names)
	if len(batches) < 2 {
		t.Fatalf("expected multiple batches, got %d", len(batches))
	}
	total := 0
	for _, batch := range batches {
		total += len(batch)
		if len(batch) > aptCachePolicyMaxArgs {
			t.Fatalf("batch too large: %d", len(batch))
		}
	}
	if total != len(names) {
		t.Fatalf("batched %d names, want %d", total, len(names))
	}
}

// TestPackageUpdatesUsesAptMetadataSecurity 端到端验证 PackageUpdates 用
// apt-cache policy + Allowed-Origins 判定，而不是包列表里的 Source 字符串。
func TestPackageUpdatesUsesAptMetadataSecurity(t *testing.T) {
	useAptSecurityFixtures(t, ubuntuAllowedOrigins, ubuntuOSRelease)
	installAptStub(t)
	t.Setenv(aptStubListOutputEnv, `Listing... Done
openssl/noble-updates 3.0.13-0ubuntu3.4 amd64 [upgradable from: 3.0.13-0ubuntu3.1]
curl/noble-security 8.5.0-2ubuntu10.3 amd64 [upgradable from: 8.5.0-2ubuntu10.1]
vendor/apps 1.1 amd64 [upgradable from: 1.0]
`)
	t.Setenv(aptStubCacheOutputEnv, ubuntuPolicyOutput+`curl:
  Installed: 8.5.0-2ubuntu10.1
  Candidate: 8.5.0-2ubuntu10.3
  Version table:
     8.5.0-2ubuntu10.3 500
        500 http://archive.ubuntu.com/ubuntu noble-updates/main amd64 Packages
vendor:
  Installed: 1.0
  Candidate: 1.1
  Version table:
     1.1 500
        500 https://packages.example.com/stable stable/main amd64 Packages
`)

	updates, err := (LocalCollector{}).PackageUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]linux.PackageUpdate{}
	for _, update := range updates {
		byName[update.Name] = update
	}
	if got := byName["openssl"]; !got.SecurityKnown || !got.Security {
		t.Fatalf("openssl must be security from Candidate source, got %#v", got)
	}
	if got := byName["curl"]; !got.SecurityKnown || got.Security {
		t.Fatalf("curl must be known non-security despite security-looking Source %q: %#v", got.Source, got)
	}
	if got := byName["vendor"]; !got.SecurityKnown || got.Security {
		t.Fatalf("vendor must be known non-security, got %#v", got)
	}
}

// TestPackageUpdatesAptCacheFailureKeepsUpdates 验证 apt-cache policy 失败时
// 可升级列表仍然成功返回，只是安全判定未知。
func TestPackageUpdatesAptCacheFailureKeepsUpdates(t *testing.T) {
	useAptSecurityFixtures(t, ubuntuAllowedOrigins, ubuntuOSRelease)
	installAptStub(t)
	t.Setenv(aptStubListOutputEnv, "Listing... Done\nopenssl/noble-security 3.0.13-0ubuntu3.4 amd64 [upgradable from: 3.0.13-0ubuntu3.1]\n")
	t.Setenv(aptStubCacheFailEnv, "1")

	updates, err := (LocalCollector{}).PackageUpdates(context.Background())
	if err != nil {
		t.Fatalf("apt-cache failure must not fail PackageUpdates: %v", err)
	}
	if len(updates) != 1 || updates[0].Name != "openssl" {
		t.Fatalf("updates = %#v", updates)
	}
	if updates[0].Security || updates[0].SecurityKnown {
		t.Fatalf("failed apt-cache policy must stay unknown, got %#v", updates[0])
	}
}

// TestPackageUpdatesUnparsableAptCacheKeepsUnknown 验证解析不出 Candidate 来源时
// 保持未知，而不是根据 Source 猜测。
func TestPackageUpdatesUnparsableAptCacheKeepsUnknown(t *testing.T) {
	useAptSecurityFixtures(t, ubuntuAllowedOrigins, ubuntuOSRelease)
	installAptStub(t)
	t.Setenv(aptStubListOutputEnv, "Listing... Done\nopenssl/noble-security 3.0.13-0ubuntu3.4 amd64 [upgradable from: 3.0.13-0ubuntu3.1]\n")
	t.Setenv(aptStubCacheOutputEnv, "something unexpected\n")

	updates, err := (LocalCollector{}).PackageUpdates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].SecurityKnown || updates[0].Security {
		t.Fatalf("unparsable apt-cache output must stay unknown, got %#v", updates)
	}
}

func assertSecurityInfo(t *testing.T, infos map[string]securityInfo, name string, wantSecurity, wantKnown bool) {
	t.Helper()
	info, ok := infos[name]
	if !ok {
		t.Fatalf("%s missing from security info: %#v", name, infos)
	}
	if info.Security != wantSecurity || info.Known != wantKnown {
		t.Fatalf("%s security=%v known=%v, want %v/%v", name, info.Security, info.Known, wantSecurity, wantKnown)
	}
}
