package docker

import (
	"context"
	"regexp"
	"strings"
	"time"

	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	// runtimeStatusVersionTimeout bounds the optional version command executed by
	// RuntimeStatus. `nginx -v` finishes in milliseconds; the bound keeps a
	// misbehaving image or a hung exec from delaying a status query.
	runtimeStatusVersionTimeout = 5 * time.Second
	// maxImageVersionLength bounds the reported version token so a pathological
	// command output cannot inflate the status response.
	maxImageVersionLength = 64
)

// imageVersionPattern extracts the semantic version token from version command
// output such as "nginx version: nginx/1.28.0". Only nginx-prefixed versions are
// accepted so unrelated path text cannot be mistaken for a version.
var imageVersionPattern = regexp.MustCompile(`(?i)\bnginx/([0-9]+(?:\.[0-9]+){1,3})`)

// runningContainerImageVersion runs one version command inside a running
// container and returns the parsed version, or "" when the command is missing,
// fails, exits non-zero or produces no recognizable output. Observation failures
// are logged at debug level and never surface as status errors.
func (r *LocalRuntime) runningContainerImageVersion(ctx context.Context, containerName string, command []string) string {
	if r == nil || r.client == nil || len(command) == 0 || strings.TrimSpace(containerName) == "" {
		return ""
	}
	probeCtx, cancel := context.WithTimeout(ctx, runtimeStatusVersionTimeout)
	defer cancel()
	result, err := r.client.execContainer(probeCtx, containerName, command)
	if err != nil {
		logging.From(ctx).Debug("runtime status image version command failed",
			zap.String("container_name", containerName),
			zap.Error(err))
		return ""
	}
	if result.ExitCode != 0 {
		return ""
	}
	return parseImageVersion(result.Output)
}

// parseImageVersion returns the nginx/x.y.z version token from command output
// (stdout and stderr are captured together by docker exec). It returns "" when
// the output carries no recognizable nginx version.
func parseImageVersion(output string) string {
	match := imageVersionPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return ""
	}
	version := strings.TrimSpace(match[1])
	if len(version) > maxImageVersionLength {
		return ""
	}
	return version
}
