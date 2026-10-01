package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
)

func testAgentDelivery(baseURL string, verifyTLS bool) AgentDeliverySettings {
	return AgentDeliverySettings{
		DownloadBaseURL: baseURL,
		VerifyTLS:       verifyTLS,
		TransferTimeout: agentTransferTimeoutDefault,
	}
}

func TestAgentBundleHandlerServesTheFixedArtifact(t *testing.T) {
	checksums := newAgentBundleRoot(t, map[string][]byte{"linux-amd64": []byte("payload")})
	version := agentBundleVersionSegment(checksums["linux-amd64"])
	mux := http.NewServeMux()
	NewHandler(nil).RegisterPublicRoutes(mux)

	path := agentDownloadPathPrefix + version + "/linux-amd64/" + agentBundleArchiveName
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/gzip" {
		t.Fatalf("unexpected content type %q", got)
	}
	// The URL carries the build version, so it may be cached forever.
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected cache control %q", got)
	}
	if got := recorder.Header().Get("ETag"); got != `"`+checksums["linux-amd64"]+`"` {
		t.Fatalf("unexpected etag %q", got)
	}
	if got := recorder.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("expected range support, got %q", got)
	}
	if got := recorder.Header().Get("Set-Cookie"); got != "" {
		t.Fatalf("the artifact endpoint must not set cookies, got %q", got)
	}
	if got := recorder.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("the archive must be served as a plain file, got content-encoding %q", got)
	}
	if recorder.Body.Len() == 0 {
		t.Fatal("expected the archive body")
	}
}

func TestAgentBundleHandlerSupportsRangeRequests(t *testing.T) {
	checksums := newAgentBundleRoot(t, map[string][]byte{"linux-amd64": []byte("payload")})
	version := agentBundleVersionSegment(checksums["linux-amd64"])
	mux := http.NewServeMux()
	NewHandler(nil).RegisterPublicRoutes(mux)

	request := httptest.NewRequest(http.MethodGet, agentDownloadPathPrefix+version+"/linux-amd64/"+agentBundleArchiveName, nil)
	request.Header.Set("Range", "bytes=0-3")
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 for a range request, got %d", recorder.Code)
	}
	if recorder.Body.Len() != 4 {
		t.Fatalf("expected 4 bytes, got %d", recorder.Body.Len())
	}
}

func TestAgentBundleHandlerRejectsEverythingElse(t *testing.T) {
	checksums := newAgentBundleRoot(t, map[string][]byte{
		"linux-amd64": []byte("payload"),
		"linux-arm64": []byte("arm payload"),
	})
	version := agentBundleVersionSegment(checksums["linux-amd64"])
	mux := http.NewServeMux()
	NewHandler(nil).RegisterPublicRoutes(mux)

	missing := []string{
		// Unknown version must not be rewritten into the served version.
		agentDownloadPathPrefix + "v0.0.1/linux-amd64/" + agentBundleArchiveName,
		// Only enumerated platforms resolve.
		agentDownloadPathPrefix + version + "/linux-386/" + agentBundleArchiveName,
		agentDownloadPathPrefix + version + "/../linux-amd64/" + agentBundleArchiveName,
		// Traversal and other artifacts stay 404 instead of reaching the SPA.
		"/agent/",
		"/agent/anything",
		agentDownloadPathPrefix + version + "/linux-amd64/panel-agent",
		agentDownloadPathPrefix + version + "/linux-amd64/panel-agent.sha256",
	}
	for _, path := range missing {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		// ServeMux sanitizes a traversal path into a redirect instead of routing
		// it; what matters is that no artifact is ever served.
		if recorder.Code == http.StatusOK {
			t.Fatalf("path %q: expected the artifact not to be served, got %d", path, recorder.Code)
		}
		if recorder.Code != http.StatusNotFound {
			continue
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("path %q: expected a non-cacheable miss, got cache-control %q", path, got)
		}
	}
}

func TestAgentBundleFetchScriptHonoursTLSAndTimeouts(t *testing.T) {
	archive := "/tmp/panel-agent-task.gz"
	downloadURL := "https://panel.example.test/agent/v1.4.2/linux-amd64/" + agentBundleArchiveName

	insecure := agentBundleFetchScript(downloadURL, archive, testAgentDelivery("https://panel.example.test", false), agentDownloadTargetTimeout(120*time.Second), 2048)
	for _, want := range []string{
		"https://panel.example.test/agent/v1.4.2/linux-amd64/panel-agent.gz",
		"curl -f -sS -L -k",
		"wget -q --no-check-certificate",
		"--max-time 100",
		"--max-filesize 2048",
		`archive='/tmp/panel-agent-task.gz'`,
		"exit 42",
		"code=43",
		"code=44",
		"code=45",
		`exit "$code"`,
		"reason=no_bounded_fetcher",
		"command -v wget >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1",
		"timeout 100 wget -q --no-check-certificate",
		"reason=truncated",
		"curl:22|wget:8) reason=http_status",
		"curl:18) reason=truncated",
		"curl:6|curl:7|curl:28|wget:4) reason=unreachable",
	} {
		if !strings.Contains(insecure, want) {
			t.Fatalf("expected fetch script to contain %q, got:\n%s", want, insecure)
		}
	}

	secure := agentBundleFetchScript(downloadURL, archive, testAgentDelivery("https://panel.example.test", true), agentDownloadTargetTimeout(120*time.Second), 2048)
	if strings.Contains(secure, " -k ") || strings.Contains(secure, "--no-check-certificate") {
		t.Fatalf("expected certificate verification to be kept when enabled, got:\n%s", secure)
	}

	// Without a known size the cap is omitted rather than sent as zero.
	uncapped := agentBundleFetchScript(downloadURL, archive, testAgentDelivery("https://panel.example.test", false), agentDownloadTargetTimeout(120*time.Second), 0)
	if strings.Contains(uncapped, "--max-filesize") {
		t.Fatalf("expected no file size cap, got:\n%s", uncapped)
	}
}

func TestAgentDeployFailureClassDecidesFallback(t *testing.T) {
	fallback := map[agentDeployFailureClass]bool{
		agentDeployFailureNoFetcher:    true,
		agentDeployFailureUnreachable:  true,
		agentDeployFailureHTTPStatus:   true,
		agentDeployFailureTruncated:    false,
		agentDeployFailureArchive:      false,
		agentDeployFailureChecksum:     false,
		agentDeployFailureToolMissing:  false,
		agentDeployFailureTimeout:      false,
		agentDeployFailureUnclassified: false,
	}
	for class, want := range fallback {
		if got := class.fallsBackToSSHUpload(); got != want {
			t.Fatalf("class %q: expected fallback=%v, got %v", class, want, got)
		}
	}

	exitCodes := map[int]agentDeployFailureClass{
		agentFetchExitNoFetcher:     agentDeployFailureNoFetcher,
		agentFetchExitUnreachable:   agentDeployFailureUnreachable,
		agentFetchExitHTTPStatus:    agentDeployFailureHTTPStatus,
		agentFetchExitTruncated:     agentDeployFailureTruncated,
		agentInstallExitDecompress:  agentDeployFailureArchive,
		agentInstallExitChecksum:    agentDeployFailureChecksum,
		agentInstallExitToolMissing: agentDeployFailureToolMissing,
	}
	for code, want := range exitCodes {
		got := agentDeployFailureClassFromResult(sshx.CommandResult{ExitCode: code}, errString("remote command failed"))
		if got != want {
			t.Fatalf("exit code %d: expected class %q, got %q", code, want, got)
		}
	}

	// A transport error that never reached a shell cannot be classified, and the
	// fallback would travel over the same broken connection.
	if got := agentDeployFailureClassFromResult(sshx.CommandResult{}, errString("ssh_connection_failed")); got != agentDeployFailureUnclassified {
		t.Fatalf("expected an unclassified transport error, got %q", got)
	}
	if got := agentDeployFailureClassFromResult(sshx.CommandResult{TimedOut: true}, errString("timeout")); got != agentDeployFailureTimeout {
		t.Fatalf("expected a timeout class, got %q", got)
	}
}

func TestAgentFetcherSetupScriptStaysInsideItsBound(t *testing.T) {
	script := agentFetcherSetupScript()
	for _, want := range []string{
		"export DEBIAN_FRONTEND=noninteractive",
		// The apt budget is the shared prelude's, parameterised so this step can
		// keep its own bound.
		"panel_timeout 120 apt-get",
		"-o Dpkg::Options::=--force-confdef",
		`apt_get update`,
		`apt_get install -y --no-install-recommends curl`,
		`if ! command -v curl >/dev/null 2>&1; then`,
		"reason=apt_update_failed",
		"reason=apt_install_failed",
		"reason=install_no_effect",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("expected fetcher setup script to contain %q, got:\n%s", want, script)
		}
	}

	// The setup runs at most two apt invocations, and the panel-side bound must
	// outlast both so the script can finish and report instead of being cut off.
	targetWorstCase := 2 * agentFetcherAPTTimeoutSeconds * time.Second
	if agentFetcherSetupTimeout <= targetWorstCase {
		t.Fatalf("panel bound %s must exceed the target worst case %s", agentFetcherSetupTimeout, targetWorstCase)
	}
}

func TestDeliverAgentBundleRepairsMissingFetcher(t *testing.T) {
	debian := linux.OSRelease{ID: "debian", VersionID: "13"}
	alpine := linux.OSRelease{ID: "alpine", VersionID: "3.20"}

	cases := []struct {
		name string
		srv  Server
		// fetchExit is the first download attempt; retryExit the one after a
		// successful fetcher install.
		fetchExit    int
		retryExit    int
		installFails bool
		wantUploads  int
		wantInstalls int
		wantFailed   bool
	}{
		{name: "download succeeds", srv: Server{OS: debian}, fetchExit: 0, wantUploads: 0},
		{
			name: "missing fetcher is repaired and the download is retried",
			srv:  Server{OS: debian}, fetchExit: agentFetchExitNoFetcher, retryExit: 0,
			wantUploads: 0, wantInstalls: 1,
		},
		{
			name: "repaired target that still has no fetcher falls back",
			srv:  Server{OS: debian}, fetchExit: agentFetchExitNoFetcher, retryExit: agentFetchExitNoFetcher,
			wantUploads: 1, wantInstalls: 1,
		},
		{
			name: "failed install falls back",
			srv:  Server{OS: debian}, fetchExit: agentFetchExitNoFetcher, installFails: true,
			wantUploads: 1, wantInstalls: 1,
		},
		{
			// Only distributions the Panel can drive are touched, so an
			// unsupported target keeps the previous behaviour.
			name: "unsupported distribution is never installed on",
			srv:  Server{OS: alpine}, fetchExit: agentFetchExitNoFetcher,
			wantUploads: 1, wantInstalls: 0,
		},
		{
			name: "truncated transfer fails without a repair attempt",
			srv:  Server{OS: debian}, fetchExit: agentFetchExitTruncated,
			wantUploads: 0, wantInstalls: 0, wantFailed: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			exec := &trackingAgentDeployExec{agentArchFakeExec: agentArchFakeExec{arch: "x86_64"}}
			svc, _, _ := testServerService(t, exec)
			svc.agentDeliverySettings = func() AgentDeliverySettings {
				return testAgentDelivery("https://panel.example.test", false)
			}
			attempts := 0
			exec.onSudo = func(command string) (sshx.CommandResult, error) {
				switch {
				case strings.Contains(command, "panel_agent_fetcher installing curl"):
					if testCase.installFails {
						return sshx.CommandResult{ExitCode: 1}, errString("remote command failed")
					}
					return sshx.CommandResult{ExitCode: 0}, nil
				case strings.Contains(command, agentDownloadPathPrefix):
					attempts++
					code := testCase.fetchExit
					if attempts > 1 {
						code = testCase.retryExit
					}
					if code == 0 {
						return sshx.CommandResult{ExitCode: 0}, nil
					}
					return sshx.CommandResult{ExitCode: code}, errString("remote command failed")
				default:
					return sshx.CommandResult{ExitCode: 0}, nil
				}
			}

			srv := testCase.srv
			srv.ID = "srv_deliver"
			srv.Host = "127.0.0.1"
			srv.Port = 22
			srv.CredentialID = "cred_1"
			artifact := agentBundleArtifact{
				Platform: "linux-amd64",
				Version:  "v1.4.2",
				SHA256:   strings.Repeat("ab", 32),
				Archive:  "/app/panel-agents/linux-amd64/" + agentBundleArchiveName,
				Size:     2048,
			}

			err := svc.deliverAgentBundle(context.Background(), "task_deliver", srv, remoteops.Runner{Exec: exec}, artifact)
			if testCase.wantFailed {
				if err == nil {
					t.Fatal("expected the delivery to fail")
				}
			} else if err != nil {
				t.Fatalf("unexpected delivery error: %v", err)
			}
			if exec.uploads != testCase.wantUploads {
				t.Fatalf("expected %d uploads, got %d", testCase.wantUploads, exec.uploads)
			}
			installs := strings.Count(strings.Join(exec.sudoCommands, "\n"), "panel_agent_fetcher installing curl")
			if installs != testCase.wantInstalls {
				t.Fatalf("expected %d fetcher install attempts, got %d", testCase.wantInstalls, installs)
			}
		})
	}
}

func TestAgentDeployDownloadFailureClassification(t *testing.T) {
	traits := map[string]string{
		agentcontract.TraitEnabled: "true",
		agentcontract.TraitURL:     "https://127.0.0.1:9786",
		agentcontract.TraitStatus:  agentcontract.StatusIncompatible,
		agentcontract.TraitVersion: "0.1.0",
	}
	cases := []struct {
		name        string
		baseURL     string
		fetchExit   int
		wantUploads int
		wantFailed  bool
	}{
		{name: "download succeeds", baseURL: "https://panel.example.test", fetchExit: 0, wantUploads: 0},
		{name: "unreachable falls back", baseURL: "https://panel.example.test", fetchExit: agentFetchExitUnreachable, wantUploads: 1},
		{name: "http status falls back", baseURL: "https://panel.example.test", fetchExit: agentFetchExitHTTPStatus, wantUploads: 1},
		{name: "missing fetcher falls back", baseURL: "https://panel.example.test", fetchExit: agentFetchExitNoFetcher, wantUploads: 1},
		{name: "truncated transfer fails without fallback", baseURL: "https://panel.example.test", fetchExit: agentFetchExitTruncated, wantUploads: 0, wantFailed: true},
		{name: "http delivery disabled", baseURL: "", fetchExit: 0, wantUploads: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			svc, taskSvc, serverID, exec, agent := newDeployTestService(t, traits)
			agent.health = agentHealth(agentcontract.Version)
			svc.agentDeliverySettings = func() AgentDeliverySettings {
				return testAgentDelivery(testCase.baseURL, false)
			}
			newAgentBundleRoot(t, map[string][]byte{"linux-amd64": []byte("payload")})
			exec.onSudo = func(command string) (sshx.CommandResult, error) {
				if strings.Contains(command, agentDownloadPathPrefix) {
					if testCase.fetchExit == 0 {
						return sshx.CommandResult{ExitCode: 0}, nil
					}
					return sshx.CommandResult{ExitCode: testCase.fetchExit}, errString("remote command failed")
				}
				return sshx.CommandResult{ExitCode: 0}, nil
			}

			task, err := svc.DeployAgent(context.Background(), serverID)
			if err != nil {
				t.Fatal(err)
			}
			finished := waitDeployTaskTerminal(t, taskSvc, task.ID)
			if testCase.wantFailed {
				if finished.Status != tasks.StatusFailed {
					t.Fatalf("expected a failed task, got %#v", finished)
				}
			} else if finished.Status != tasks.StatusCompleted {
				t.Fatalf("expected a completed task, got %#v (%s)", finished, finished.Error)
			}
			if exec.uploads != testCase.wantUploads {
				t.Fatalf("expected %d uploads, got %d", testCase.wantUploads, exec.uploads)
			}
			if exec.uploads > 0 {
				// The SSH fallback carries the compressed archive too.
				if !strings.HasSuffix(exec.uploadSpec.LocalPath, agentBundleArchiveName) {
					t.Fatalf("expected the compressed archive to be uploaded, got %q", exec.uploadSpec.LocalPath)
				}
				if exec.uploadSpec.Timeout != agentTransferTimeoutDefault {
					t.Fatalf("expected the transfer timeout on the upload, got %s", exec.uploadSpec.Timeout)
				}
			}
		})
	}
}
