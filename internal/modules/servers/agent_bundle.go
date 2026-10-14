package server

import (
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"panel/internal/platform/buildinfo"
	"panel/internal/platform/linux/remoteops"
)

// Agent bundles are prepared at image build time: a gzip-compressed binary plus
// the SHA-256 of the *uncompressed* binary next to it. The Panel never
// compresses or hashes an agent binary at runtime, so serving the archive is a
// plain file read and the checksum handed to a target host is always the one
// the build produced.
const (
	agentBundleArchiveName  = "panel-agent.gz"
	agentBundleChecksumName = "panel-agent.sha256"
)

// agentRemoteTmpPrefix is the fixed /tmp staging prefix for one deploy task.
// The archive keeps the .gz suffix so the remote install script can hand it to
// gzip -dc unchanged.
const agentRemoteTmpPrefix = "/tmp/panel-agent-"

func agentRemoteTmpArchivePath(taskID string) string {
	return agentRemoteTmpPrefix + taskID + ".gz"
}

func agentRemoteTmpBinaryPath(taskID string) string {
	return agentRemoteTmpPrefix + taskID
}

// agentDownloadPathPrefix is the public path prefix the agent bundle is served
// under. It is deliberately outside /api so that CDN rules which bypass caching
// for API traffic do not apply to the artifact.
const agentDownloadPathPrefix = "/agent/"

// agentBundlePlatforms is the complete set of platforms the image ships. Every
// bundle lookup enumerates this fixed list; request data is only ever compared
// against it and never takes part in building a filesystem path.
var agentBundlePlatforms = []string{"linux-amd64", "linux-arm64"}

var (
	agentBundleChecksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	agentBundleSegmentPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

const (
	agentTransferTimeoutDefault = 300 * time.Second
	agentTransferTimeoutMin     = 60 * time.Second
	agentTransferTimeoutMax     = 3600 * time.Second
	// agentDownloadTargetReserve keeps the target-side fetch budget strictly
	// below the Panel-side bound. The fetch script must always get the chance to
	// exit with a classified reason: if the Panel bound fired first the task
	// would only see a bare remote timeout and could not tell "cannot connect"
	// (fall back to the SSH upload) from "truncated mid-transfer" (must not).
	agentDownloadTargetReserve = 30 * time.Second
	// agentDownloadRounds is how many bounded attempts one download may take.
	// Each attempt resumes the previous one, so a link that is merely slower than
	// a single round still converges instead of restarting from zero. This is
	// what replaces curl --retry, which must not be used here: curl applies
	// --max-time per attempt, so a retry multiplies the target-side worst case
	// and silently breaks the bound below.
	agentDownloadRounds     = 3
	agentDownloadRetryDelay = 2 * time.Second
	// agentDownloadConnectTimeout is the connect-phase limit of one attempt.
	agentDownloadConnectTimeout = 15 * time.Second
)

// agentBundleArtifact is one servable agent bundle: the compressed archive, the
// precomputed checksum of its uncompressed payload, and the cache segment the
// download path is built from.
type agentBundleArtifact struct {
	Platform string
	Version  string
	SHA256   string
	Archive  string
	Size     int64
	ModTime  time.Time
}

// AgentDeliverySettings is the runtime configuration for agent binary delivery.
// It is supplied by the settings module through a provider so the servers
// module does not depend on it directly.
type AgentDeliverySettings struct {
	DownloadBaseURL string
	VerifyTLS       bool
	TransferTimeout time.Duration
}

// WithAgentDeliverySettings wires the runtime-provided delivery settings.
func WithAgentDeliverySettings(provider func() AgentDeliverySettings) Option {
	return func(s *Service) { s.agentDeliverySettings = provider }
}

func (s *Service) agentDelivery() AgentDeliverySettings {
	settings := AgentDeliverySettings{TransferTimeout: agentTransferTimeoutDefault}
	if s.agentDeliverySettings != nil {
		settings = s.agentDeliverySettings()
	}
	if settings.TransferTimeout <= 0 {
		settings.TransferTimeout = agentTransferTimeoutDefault
	}
	return settings
}

// NormalizeAgentTransferTimeout clamps a configured transfer timeout into the
// supported range. A zero value means "use the default".
func NormalizeAgentTransferTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return agentTransferTimeoutDefault
	}
	if timeout < agentTransferTimeoutMin {
		return agentTransferTimeoutMin
	}
	if timeout > agentTransferTimeoutMax {
		return agentTransferTimeoutMax
	}
	return timeout
}

// agentBundleArchivePathForPlatform and agentBundleChecksumPathForPlatform
// build the fixed on-disk location of one platform's bundle. The caller must
// pass a platform taken from agentBundlePlatforms, never request data.
func agentBundleArchivePathForPlatform(platform string) string {
	return path.Join(agentBundleRoot, strings.TrimSpace(platform), agentBundleArchiveName)
}

func agentBundleChecksumPathForPlatform(platform string) string {
	return path.Join(agentBundleRoot, strings.TrimSpace(platform), agentBundleChecksumName)
}

// agentBundleVersionSegment derives the cache segment used in the download
// path. Release builds keep their build version, which the release pipeline
// makes unique per build; a build that carries no distinguishable version does
// not, so it falls back to a content-addressed segment instead of letting a CDN
// pin a stale binary under a reused URL.
func agentBundleVersionSegment(checksum string) string {
	version := strings.TrimSpace(buildinfo.NormalizedVersion())
	if version != "" && version != "dev" && agentBundleSegmentPattern.MatchString(version) {
		return version
	}
	short := checksum
	if len(short) > 12 {
		short = short[:12]
	}
	return "dev-" + short
}

// lookupAgentBundleArtifact resolves the bundle of one platform from the fixed
// platform list. It is the only place that reads the bundle off disk, and the
// path it builds comes from agentBundleRoot plus a platform this function
// validated itself.
func lookupAgentBundleArtifact(platform string) (agentBundleArtifact, bool) {
	platform = strings.TrimSpace(platform)
	if !isAgentBundlePlatform(platform) {
		return agentBundleArtifact{}, false
	}
	archive := agentBundleArchivePathForPlatform(platform)
	info, err := os.Stat(archive)
	if err != nil || info.IsDir() {
		return agentBundleArtifact{}, false
	}
	checksumBytes, err := os.ReadFile(agentBundleChecksumPathForPlatform(platform))
	if err != nil {
		return agentBundleArtifact{}, false
	}
	// The checksum travels into a remote shell script, so it is accepted only in
	// its exact canonical form.
	checksum := strings.ToLower(strings.TrimSpace(string(checksumBytes)))
	if !agentBundleChecksumPattern.MatchString(checksum) {
		return agentBundleArtifact{}, false
	}
	return agentBundleArtifact{
		Platform: platform,
		Version:  agentBundleVersionSegment(checksum),
		SHA256:   checksum,
		Archive:  archive,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
	}, true
}

func isAgentBundlePlatform(platform string) bool {
	for _, candidate := range agentBundlePlatforms {
		if candidate == platform {
			return true
		}
	}
	return false
}

// isSafeAgentBundleSegment reports whether a request-supplied path segment is
// shaped like a bundle key. Segments that are not are rejected before any
// comparison so traversal attempts never reach the lookup table.
func isSafeAgentBundleSegment(segment string) bool {
	if strings.Contains(segment, "..") || strings.ContainsAny(segment, `/\`) {
		return false
	}
	return agentBundleSegmentPattern.MatchString(segment)
}

// lookupAgentBundle resolves a request-supplied (version, platform) pair against
// the fixed bundle table. Both segments are used only as comparison keys, and a
// miss is reported as not found instead of being rewritten into the version the
// Panel happens to be serving.
func lookupAgentBundle(version, platform string) (agentBundleArtifact, bool) {
	if !isSafeAgentBundleSegment(version) || !isSafeAgentBundleSegment(platform) {
		return agentBundleArtifact{}, false
	}
	artifact, ok := lookupAgentBundleArtifact(platform)
	if !ok || artifact.Version != version {
		return agentBundleArtifact{}, false
	}
	return artifact, true
}

// agentDownloadURL builds the public download URL of one artifact. It returns
// false when HTTP delivery is disabled or the configured base URL is not usable,
// in which case the deploy task keeps using the SSH upload path.
func agentDownloadURL(baseURL string, artifact agentBundleArtifact) (string, bool) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", false
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	// Path, query, fragment and userinfo are rejected by the settings
	// validation; re-checking here keeps a malformed value from silently
	// producing a download URL that points somewhere unintended.
	if parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return "", false
	}
	return base + agentDownloadPathPrefix + artifact.Version + "/" + artifact.Platform + "/" + agentBundleArchiveName, true
}

// agentDownloadTargetBudget is the total wall clock the target-side fetch may
// take. It is strictly shorter than the Panel-side bound so the script always
// finishes and reports a classified reason.
func agentDownloadTargetBudget(transferTimeout time.Duration) time.Duration {
	budget := transferTimeout - agentDownloadTargetReserve
	if budget < 20*time.Second {
		budget = 20 * time.Second
	}
	return budget
}

// agentDownloadRoundTimeout is the per-attempt limit. The whole point of the
// budget arithmetic is that rounds x round + the delays between them stays
// inside agentDownloadTargetBudget, which in turn stays inside the Panel-side
// bound. curl's --retry cannot be used for this: it grants every retry a fresh
// --max-time, so the worst case becomes unbounded in practice.
func agentDownloadRoundTimeout(budget time.Duration) time.Duration {
	delays := time.Duration(agentDownloadRounds-1) * agentDownloadRetryDelay
	round := (budget - delays) / agentDownloadRounds
	if round < 5*time.Second {
		round = 5 * time.Second
	}
	return round
}

// agentBundleInstallScript verifies the downloaded archive and installs it. It
// is shared by the HTTP download path and the SSH upload path, so both paths
// decompress and checksum the bundle before anything is installed.
//
// Exit codes are part of the contract with agentDeployFailureFromResult: a
// missing tool, a corrupt archive and a checksum mismatch must fail the
// deployment outright rather than silently falling back to the SSH upload.
func agentBundleInstallScript(archivePath, binaryPath, expectedSHA256 string) string {
	archive := remoteops.ShellQuote(archivePath)
	binary := remoteops.ShellQuote(binaryPath)
	return strings.Join([]string{
		"set -eu",
		`if ! command -v systemctl >/dev/null 2>&1; then`,
		`  echo "[panel] systemd is required to manage panel-agent" >&2`,
		`  exit 1`,
		`fi`,
		`if ! command -v gzip >/dev/null 2>&1; then`,
		`  echo "[panel] panel_agent_install_failed reason=tool_missing tool=gzip" >&2`,
		`  exit 48`,
		`fi`,
		`if ! command -v sha256sum >/dev/null 2>&1; then`,
		`  echo "[panel] panel_agent_install_failed reason=tool_missing tool=sha256sum" >&2`,
		`  exit 48`,
		`fi`,
		"archive=" + archive,
		"binary=" + binary,
		`trap 'rm -f "$archive" "$binary"' EXIT`,
		`if ! gzip -dc "$archive" > "$binary"; then`,
		`  echo "[panel] panel_agent_install_failed reason=decompress_failed" >&2`,
		`  exit 46`,
		`fi`,
		`actual="$(sha256sum "$binary" | awk '{print $1}')"`,
		`if [ "$actual" != ` + remoteops.ShellQuote(expectedSHA256) + ` ]; then`,
		`  echo "[panel] panel_agent_install_failed reason=checksum_mismatch expected=` + expectedSHA256 + ` actual=$actual" >&2`,
		`  exit 47`,
		`fi`,
		`rm -f "$archive"`,
		"systemctl stop panel-agent.service >/dev/null 2>&1 || true",
		`if command -v pkill >/dev/null 2>&1; then`,
		`  pkill -x panel-agent >/dev/null 2>&1 || true`,
		`  pkill -f '^/usr/local/bin/panel-agent($| )' >/dev/null 2>&1 || true`,
		`fi`,
		`install -m 0755 "$binary" ` + remoteops.ShellQuote(agentRemoteBinaryPath),
		`rm -f "$binary"`,
		`trap - EXIT`,
		agentServiceStartScript(),
	}, "\n")
}

func agentTransferSeconds(timeout time.Duration) string {
	return strconv.Itoa(int(timeout / time.Second))
}
