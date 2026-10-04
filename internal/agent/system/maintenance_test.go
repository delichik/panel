package system

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
)

const (
	aptHelperEnv         = "PANEL_TEST_APT_HELPER"
	aptHelperStartedEnv  = "PANEL_TEST_APT_STARTED"
	aptHelperFinishedEnv = "PANEL_TEST_APT_FINISHED"
)

func TestMain(m *testing.M) {
	if os.Getenv(aptHelperEnv) == "1" {
		runAptHelper()
	}
	os.Exit(m.Run())
}

func runAptHelper() {
	_ = os.WriteFile(os.Getenv(aptHelperStartedEnv), []byte("started\n"), 0o644)
	time.Sleep(2 * time.Second)
	_ = os.WriteFile(os.Getenv(aptHelperFinishedEnv), []byte("finished\n"), 0o644)
	os.Exit(0)
}

func TestUpgradePackagesIgnoresRequestCancellation(t *testing.T) {
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fakeApt := filepath.Join(dir, executableName("apt-get"))
	if err := copyFile(exe, fakeApt); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(fakeApt, 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Setenv("PATH", oldPath)
	}()

	started := filepath.Join(dir, "started")
	finished := filepath.Join(dir, "finished")
	envVars := map[string]string{
		aptHelperEnv:         "1",
		aptHelperStartedEnv:  started,
		aptHelperFinishedEnv: finished,
	}
	for key, value := range envVars {
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		for key := range envVars {
			_ = os.Unsetenv(key)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := (LocalCollector{}).UpgradePackages(ctx, agentcontract.PackageUpgradeRequest{All: true})
		result <- err
	}()

	waitForFile(t, started, 5*time.Second)
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("UpgradePackages should survive request cancellation: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("UpgradePackages did not finish after request cancellation")
	}
	waitForFile(t, finished, 2*time.Second)
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
