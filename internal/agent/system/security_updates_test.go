package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"panel/internal/platform/linux"
)

const (
	unattendedHelperEnv       = "PANEL_TEST_UNATTENDED_HELPER"
	unattendedHelperOutputEnv = "PANEL_TEST_UNATTENDED_OUTPUT"
	unattendedHelperFailEnv   = "PANEL_TEST_UNATTENDED_FAIL"
)

// runUnattendedUpgradeHelper 是假 unattended-upgrade 子进程：把预设输出写到
// stdout，并按预设退出码退出。TestMain 在子进程中调用它。
func runUnattendedUpgradeHelper() {
	_, _ = os.Stdout.WriteString(os.Getenv(unattendedHelperOutputEnv))
	if os.Getenv(unattendedHelperFailEnv) == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func installFakeUnattendedUpgrade(t *testing.T, dir string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, executableName("unattended-upgrade"))
	if err := copyFile(exe, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
}

func prependTestPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestParseUnattendedUpgradeDryRunSingleLine 覆盖较早版本把包名放在标记同一行
// 的输出格式。
func TestParseUnattendedUpgradeDryRunSingleLine(t *testing.T) {
	out := "Checking available free disk space\nPackages that will be upgraded: openssl libssl3\n"
	security, known := parseUnattendedUpgradeDryRun(out, nil)
	if !known {
		t.Fatal("expected recognized dry-run output to be authoritative")
	}
	assertSecuritySet(t, security, "openssl", "libssl3")
}

// TestParseUnattendedUpgradeDryRunIndentedList 覆盖标记行后跟缩进包名列表的
// 输出格式，并确认列表在非缩进行处结束。
func TestParseUnattendedUpgradeDryRunIndentedList(t *testing.T) {
	out := "Packages that will be upgraded: \n  libc6:amd64\n\topenssl\nWriting dpkg log to /var/log/unattended-upgrades/unattended-upgrades-dpkg.log\n"
	security, known := parseUnattendedUpgradeDryRun(out, nil)
	if !known {
		t.Fatal("expected recognized dry-run output to be authoritative")
	}
	assertSecuritySet(t, security, "libc6", "openssl")
	if _, ok := security["Writing"]; ok {
		t.Fatalf("log lines after the package list must not be parsed as packages: %#v", security)
	}
}

// TestParseUnattendedUpgradeDryRunPlainList 覆盖标记行后每行一个包名且未缩进
// 的格式，同时确认后续日志行不会被当包名收进集合。
func TestParseUnattendedUpgradeDryRunPlainList(t *testing.T) {
	out := "Packages that will be upgraded: \nlibc6:amd64\nopenssl\nWriting dpkg log to /var/log/unattended-upgrades/unattended-upgrades-dpkg.log\n"
	security, known := parseUnattendedUpgradeDryRun(out, nil)
	if !known {
		t.Fatal("expected recognized dry-run output to be authoritative")
	}
	assertSecuritySet(t, security, "libc6", "openssl")
}

// TestParseUnattendedUpgradeDryRunNoPackages 覆盖确定“没有安全更新”的输出：
// 这是已知结论（空集合），不是不可判定。
func TestParseUnattendedUpgradeDryRunNoPackages(t *testing.T) {
	out := "No packages found that can be upgraded unattended and no pending auto-removals\n"
	security, known := parseUnattendedUpgradeDryRun(out, nil)
	if !known {
		t.Fatal("no-packages output must still be authoritative")
	}
	if len(security) != 0 {
		t.Fatalf("no-packages output must produce an empty set, got %#v", security)
	}
}

func TestParseUnattendedUpgradeDryRunUnrecognizedOutputIsUnknown(t *testing.T) {
	security, known := parseUnattendedUpgradeDryRun("something unexpected\n", nil)
	if known || security != nil {
		t.Fatalf("unrecognized output must be unknown, got known=%v set=%#v", known, security)
	}
}

func TestParseUnattendedUpgradeDryRunCommandFailureIsUnknown(t *testing.T) {
	security, known := parseUnattendedUpgradeDryRun("Packages that will be upgraded: openssl\n", errors.New("exit status 1"))
	if known || security != nil {
		t.Fatalf("failed command must be unknown, got known=%v set=%#v", known, security)
	}
}

// TestClassifyPackageSecurityUnknownKeepsZeroValues 锁定不可判定时不得把包
// 标成“非安全”：Security 与 SecurityKnown 都保持 false，由 Panel 回退启发式。
func TestClassifyPackageSecurityUnknownKeepsZeroValues(t *testing.T) {
	updates := []linux.PackageUpdate{{Name: "openssl", Source: "jammy-security"}}
	classifyPackageSecurity(updates, nil, false)
	if updates[0].Security || updates[0].SecurityKnown {
		t.Fatalf("unknown classification must keep zero values: %#v", updates[0])
	}
}

// TestClassifyPackageSecurityKnown 覆盖权威判定的两种情况：命中集合为安全，
// 未命中为“已知非安全”（即使 source 看起来像安全源）。
func TestClassifyPackageSecurityKnown(t *testing.T) {
	updates := []linux.PackageUpdate{
		{Name: "openssl", Source: "jammy-updates"},
		{Name: "curl", Source: "jammy-security"},
		{Name: "libc6", Source: ""},
	}
	classifyPackageSecurity(updates, map[string]struct{}{"openssl": {}}, true)
	if !updates[0].SecurityKnown || !updates[0].Security {
		t.Fatalf("authoritative security package not marked: %#v", updates[0])
	}
	for _, update := range updates[1:] {
		if !update.SecurityKnown {
			t.Fatalf("authoritative non-security package must be known: %#v", update)
		}
		if update.Security {
			t.Fatalf("authoritative non-security package must not be security: %#v", update)
		}
	}

	empty := []linux.PackageUpdate{{Name: "openssl", Source: "jammy-security"}}
	classifyPackageSecurity(empty, map[string]struct{}{}, true)
	if !empty[0].SecurityKnown || empty[0].Security {
		t.Fatalf("known empty set must mark packages as known non-security: %#v", empty[0])
	}
}

// TestUnattendedUpgradeSecurityPackagesExecutesDryRun 通过假可执行文件验证
// Agent 真的执行 `unattended-upgrade --dry-run` 并解析其输出。
func TestUnattendedUpgradeSecurityPackagesExecutesDryRun(t *testing.T) {
	dir := t.TempDir()
	installFakeUnattendedUpgrade(t, dir)
	prependTestPath(t, dir)
	t.Setenv(unattendedHelperEnv, "1")
	t.Setenv(unattendedHelperOutputEnv, "Packages that will be upgraded: \n  openssl:amd64\n  libc6\n")

	security, known := unattendedUpgradeSecurityPackages(context.Background())
	if !known {
		t.Fatal("expected dry-run output to be authoritative")
	}
	assertSecuritySet(t, security, "openssl", "libc6")
}

// TestUnattendedUpgradeSecurityPackagesFailureIsUnknown 验证命令退出失败时
// 只是不可判定，不会返回错误。
func TestUnattendedUpgradeSecurityPackagesFailureIsUnknown(t *testing.T) {
	dir := t.TempDir()
	installFakeUnattendedUpgrade(t, dir)
	prependTestPath(t, dir)
	t.Setenv(unattendedHelperEnv, "1")
	t.Setenv(unattendedHelperFailEnv, "1")
	t.Setenv(unattendedHelperOutputEnv, "Packages that will be upgraded: openssl\n")

	if security, known := unattendedUpgradeSecurityPackages(context.Background()); known || security != nil {
		t.Fatalf("failed dry-run must be unknown, got known=%v set=%#v", known, security)
	}
}

// TestUnattendedUpgradeSecurityPackagesMissingCommandIsUnknown 验证命令不存在
// 时按不可判定处理。
func TestUnattendedUpgradeSecurityPackagesMissingCommandIsUnknown(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if security, known := unattendedUpgradeSecurityPackages(context.Background()); known || security != nil {
		t.Fatalf("missing dry-run command must be unknown, got known=%v set=%#v", known, security)
	}
}

func assertSecuritySet(t *testing.T, security map[string]struct{}, want ...string) {
	t.Helper()
	if len(security) != len(want) {
		t.Fatalf("security set = %#v, want %#v", security, want)
	}
	for _, name := range want {
		if _, ok := security[name]; !ok {
			t.Fatalf("security set = %#v, missing %q", security, name)
		}
	}
}
