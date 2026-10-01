package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
)

// Exit codes the fetch and install scripts use to report a classified failure.
// Only the fetch codes below request a fallback; everything else fails the
// deployment, because a truncated transfer, a corrupt archive or a checksum
// mismatch is a data problem that a slow retry path must not paper over.
const (
	agentFetchExitNoFetcher   = 42
	agentFetchExitUnreachable = 43
	agentFetchExitHTTPStatus  = 44
	agentFetchExitTruncated   = 45

	agentInstallExitDecompress  = 46
	agentInstallExitChecksum    = 47
	agentInstallExitToolMissing = 48
)

// agentDeployFailureClass names why the HTTP download path failed. It decides
// whether the deploy task falls back to the SSH upload.
type agentDeployFailureClass string

const (
	agentDeployFailureUnclassified agentDeployFailureClass = "unclassified"
	agentDeployFailureNoFetcher    agentDeployFailureClass = "no_fetcher"
	agentDeployFailureUnreachable  agentDeployFailureClass = "unreachable"
	agentDeployFailureHTTPStatus   agentDeployFailureClass = "http_status"
	agentDeployFailureTruncated    agentDeployFailureClass = "truncated"
	agentDeployFailureArchive      agentDeployFailureClass = "decompress_failed"
	agentDeployFailureChecksum     agentDeployFailureClass = "checksum_mismatch"
	agentDeployFailureToolMissing  agentDeployFailureClass = "tool_missing"
	agentDeployFailureTimeout      agentDeployFailureClass = "timeout"
)

// fallsBackToSSHUpload reports whether a failure class may be retried over the
// SSH upload path. Connection failures, HTTP error responses and a missing
// fetcher are transport reachability problems, so SSH is a real alternative;
// data problems are not.
func (c agentDeployFailureClass) fallsBackToSSHUpload() bool {
	switch c {
	case agentDeployFailureNoFetcher, agentDeployFailureUnreachable, agentDeployFailureHTTPStatus:
		return true
	default:
		return false
	}
}

func agentDeployFailureClassFromExitCode(exitCode int) (agentDeployFailureClass, bool) {
	switch exitCode {
	case agentFetchExitNoFetcher:
		return agentDeployFailureNoFetcher, true
	case agentFetchExitUnreachable:
		return agentDeployFailureUnreachable, true
	case agentFetchExitHTTPStatus:
		return agentDeployFailureHTTPStatus, true
	case agentFetchExitTruncated:
		return agentDeployFailureTruncated, true
	case agentInstallExitDecompress:
		return agentDeployFailureArchive, true
	case agentInstallExitChecksum:
		return agentDeployFailureChecksum, true
	case agentInstallExitToolMissing:
		return agentDeployFailureToolMissing, true
	default:
		return agentDeployFailureUnclassified, false
	}
}

// agentDeployFailureClassFromResult classifies a failed remote step. A transport
// error that never reached a shell is reported as unclassified: the fallback
// would travel over the same broken connection, so the deployment should surface
// the original error instead.
func agentDeployFailureClassFromResult(result sshx.CommandResult, err error) agentDeployFailureClass {
	if result.TimedOut || errors.Is(err, context.DeadlineExceeded) {
		return agentDeployFailureTimeout
	}
	if class, ok := agentDeployFailureClassFromExitCode(result.ExitCode); ok {
		return class
	}
	return agentDeployFailureUnclassified
}

// agentBundleFetchScript downloads the compressed agent bundle on the target
// host. It is a separate step from installation so the Panel can classify a
// failed fetch and decide about the SSH fallback without re-running an
// installer that may already have stopped the running agent.
func agentBundleFetchScript(downloadURL, archivePath string, settings AgentDeliverySettings, targetTimeout time.Duration, maxBytes int64) string {
	seconds := agentTransferSeconds(targetTimeout)
	connectSeconds := agentTransferSeconds(agentDownloadConnectTimeout)
	if agentDownloadConnectTimeout >= targetTimeout {
		connectSeconds = strconv.Itoa(int(targetTimeout/time.Second) / 2)
	}
	curlTLS := " -k"
	wgetTLS := " --no-check-certificate"
	if settings.VerifyTLS {
		curlTLS = ""
		wgetTLS = ""
	}
	// curl bounds itself with --max-time. wget has no total-time option, so it
	// is only usable together with `timeout`; without a total bound the panel
	// side would fire first and the failure could not be classified, which is
	// what decides the SSH fallback.
	curlCommand := "curl -f -sS -L" + curlTLS + " --retry 3 --retry-delay 2 --connect-timeout " + connectSeconds + " --max-time " + seconds
	if maxBytes > 0 {
		curlCommand += " --max-filesize " + strconv.FormatInt(maxBytes, 10)
	}
	curlCommand += ` -o "$archive" "$url"`
	wgetCommand := "timeout " + seconds + " wget -q" + wgetTLS + " --tries=1 --timeout=" + connectSeconds + ` -O "$archive" "$url"`
	return strings.Join([]string{
		"set -u",
		"url=" + remoteops.ShellQuote(downloadURL),
		"archive=" + remoteops.ShellQuote(archivePath),
		`rm -f "$archive"`,
		`if command -v curl >/dev/null 2>&1; then`,
		`  fetcher=curl`,
		`elif command -v wget >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1; then`,
		`  fetcher=wget`,
		`else`,
		`  echo "[panel] panel_agent_download_failed reason=no_bounded_fetcher" >&2`,
		`  exit ` + strconv.Itoa(agentFetchExitNoFetcher),
		`fi`,
		`status=0`,
		`if [ "$fetcher" = "curl" ]; then`,
		`  ` + curlCommand + ` || status=$?`,
		`else`,
		`  ` + wgetCommand + ` || status=$?`,
		`fi`,
		`if [ "$status" -ne 0 ]; then`,
		`  if [ -s "$archive" ]; then`,
		`    echo "[panel] panel_agent_download_failed reason=truncated fetcher=$fetcher status=$status" >&2`,
		`    rm -f "$archive"`,
		`    exit ` + strconv.Itoa(agentFetchExitTruncated),
		`  fi`,
		`  rm -f "$archive"`,
		`  case "$fetcher:$status" in`,
		`    curl:22|wget:8) reason=http_status; code=` + strconv.Itoa(agentFetchExitHTTPStatus) + ` ;;`,
		`    curl:18) reason=truncated; code=` + strconv.Itoa(agentFetchExitTruncated) + ` ;;`,
		`    curl:6|curl:7|curl:28|wget:4) reason=unreachable; code=` + strconv.Itoa(agentFetchExitUnreachable) + ` ;;`,
		// Anything else (an unexpected fetcher status, a 404 answered by a
		// non-curl client, an oversized response rejected by --max-filesize)
		// means the URL did not deliver a usable archive; the SSH upload is a
		// genuine alternative, so it falls back.
		`    *) reason=unreachable; code=` + strconv.Itoa(agentFetchExitUnreachable) + ` ;;`,
		`  esac`,
		`  echo "[panel] panel_agent_download_failed reason=$reason fetcher=$fetcher status=$status" >&2`,
		`  exit "$code"`,
		`fi`,
		`if [ ! -s "$archive" ]; then`,
		`  echo "[panel] panel_agent_download_failed reason=truncated fetcher=$fetcher status=empty" >&2`,
		`  rm -f "$archive"`,
		`  exit ` + strconv.Itoa(agentFetchExitTruncated),
		`fi`,
		`echo "[panel] panel_agent_download_ok fetcher=$fetcher bytes=$(wc -c < "$archive" | tr -d ' ')" >&2`,
	}, "\n")
}

// agentFetcherAPTTimeoutSeconds bounds each apt invocation of the fetcher setup
// step. The setup runs at most two of them (update, install), so its worst case
// stays strictly inside agentFetcherSetupTimeout.
const agentFetcherAPTTimeoutSeconds = 120

// agentFetcherSetupTimeout bounds the best-effort step that makes a download
// fetcher available on the target. It exists only to choose between two
// transports, so a timeout or any other failure falls back to the SSH upload
// instead of failing the deployment.
const agentFetcherSetupTimeout = 5 * time.Minute

// agentFetcherSetupScript installs curl on a target that has no usable bundle
// fetcher. It runs only for distributions the Panel knows how to drive, and only
// after a download attempt already reported agentFetchExitNoFetcher.
//
// The step is best effort: every failure path exits non-zero with a reason on
// stderr and the caller falls back to the SSH upload, so a target that cannot
// install anything still gets its agent.
func agentFetcherSetupScript() string {
	return strings.Join([]string{
		strings.TrimRight(remoteops.APTInstallPrelude(agentFetcherAPTTimeoutSeconds), "\n"),
		`echo "[panel] panel_agent_fetcher installing curl" >&2`,
		`if ! apt_get update; then`,
		`  echo "[panel] panel_agent_fetcher missing reason=apt_update_failed" >&2`,
		`  exit 1`,
		`fi`,
		`if ! apt_get install -y --no-install-recommends curl; then`,
		`  echo "[panel] panel_agent_fetcher missing reason=apt_install_failed" >&2`,
		`  exit 1`,
		`fi`,
		`if ! command -v curl >/dev/null 2>&1; then`,
		`  echo "[panel] panel_agent_fetcher missing reason=install_no_effect" >&2`,
		`  exit 1`,
		`fi`,
		`echo "[panel] panel_agent_fetcher ready fetcher=curl installed=yes" >&2`,
	}, "\n")
}

// RegisterPublicRoutes registers the unauthenticated agent bundle download
// endpoints. Targets have no Panel session, and a per-server token would give
// every server a different URL, which would defeat CDN reuse. The artifact is
// not a secret: it is distributed to every managed host and is a build product
// of a public repository.
//
// Both patterns are written as string literals on purpose. The artifact file
// name being a literal rather than a path parameter is what keeps request data
// out of the filesystem, and the public route manifest test reads these
// literals straight out of this file so the served path cannot change silently.
func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /agent/{version}/{platform}/panel-agent.gz", h.AgentBundle)
	// Every other path under the prefix is a miss, so an unknown artifact never
	// falls through to the SPA catch-all and answers 200 with index.html.
	mux.HandleFunc("GET /agent/", h.AgentBundleNotFound)
}

func (h *Handler) AgentBundle(w http.ResponseWriter, r *http.Request) {
	artifact, ok := lookupAgentBundle(r.PathValue("version"), r.PathValue("platform"))
	if !ok {
		h.AgentBundleNotFound(w, r)
		return
	}
	file, err := os.Open(artifact.Archive)
	if err != nil {
		h.AgentBundleNotFound(w, r)
		return
	}
	defer file.Close()
	headers := w.Header()
	headers.Set("Content-Type", "application/gzip")
	// The archive URL carries the build version, so it can be cached forever:
	// a new build produces a new URL instead of a stale entry under this one.
	headers.Set("Cache-Control", "public, max-age=31536000, immutable")
	headers.Set("ETag", `"`+artifact.SHA256+`"`)
	headers.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", artifact.ModTime, file)
}

func (h *Handler) AgentBundleNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.Error(w, "agent bundle not found", http.StatusNotFound)
}

// agentDownloadHost is used for task log lines so an operator can see which
// address a deployment actually tried.
func agentDownloadHost(downloadURL string) string {
	parsed, err := url.Parse(downloadURL)
	if err != nil || parsed.Host == "" {
		return downloadURL
	}
	return parsed.Host
}

func agentDownloadFailureError(class agentDeployFailureClass) error {
	return panelerr.BadGateway("agent_download_failed", "Panel agent download failed ("+string(class)+")")
}
