package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	appruntime "panel/internal/modules/applications/runtime"
)

const startupLogBytes = 8 << 10
const startupLogRunes = 3072
const startupLogTimeout = 2 * time.Second
const startupLogTimeSkew = 2 * time.Second

var startupPrivateKey = regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----.*?(?:-----END (?:RSA |EC |OPENSSH |ENCRYPTED )?PRIVATE KEY-----|$)`)
var startupCertificatePEM = regexp.MustCompile(`(?s)-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----`)
var startupSecretAssignment = regexp.MustCompile(`(?i)((?:password|passwd|passphrase|private[_-]?key|(?:access[_-]?|refresh[_-]?)?token|api[_-]?key|authorization|secret)["']?\s*[=:]\s*)("[^"\r\n]*"?|'[^'\r\n]*'?|[^\s,;]+)`)
var startupSensitiveLine = regexp.MustCompile(`(?i)(password|passwd|passphrase|private[_-]?key|(?:access[_-]?|refresh[_-]?)?token|api[_-]?key|authorization|secret)\s*[=:]|-----BEGIN |[A-Za-z0-9+/=]{80,}`)

// Failure evidence is best effort: one bounded request, scoped by immutable
// container ID and this process's actual start/finish timestamps. Never read
// by name. When evidence cannot be collected, always explain why — silent
// omission leaves only exitCode and is useless for troubleshooting.
func (r *LocalRuntime) startupFailureLogs(ctx context.Context, status appruntime.InstanceStatus, expectedID string, spec appruntime.Spec) string {
	expectedID = strings.TrimSpace(expectedID)
	if expectedID == "" {
		return "\ncontainerLogs: unavailable (expected container id missing)"
	}
	observedID := strings.TrimSpace(status.ContainerID)
	if observedID == "" {
		return "\ncontainerLogs: unavailable (observed container id missing)"
	}
	if !containerIDEqual(observedID, expectedID) {
		return "\ncontainerLogs: unavailable (container id mismatch observed=" + shortContainerID(observedID) + " expected=" + shortContainerID(expectedID) + ")"
	}
	if status.Status != appruntime.StatusFailed && status.Status != appruntime.StatusStopped && status.ExitCode == 0 {
		return "\ncontainerLogs: unavailable (status=" + firstNonEmpty(status.Status, "unknown") + ")"
	}
	started, startErr := time.Parse(time.RFC3339Nano, status.StartedAt)
	finished, finishErr := time.Parse(time.RFC3339Nano, status.FinishedAt)
	if startErr != nil || finishErr != nil || started.IsZero() || finished.Before(started) {
		return "\ncontainerLogs: unavailable (run timestamps missing)"
	}
	logctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), startupLogTimeout)
	defer cancel()
	// Absorb small Docker clock skew so a sub-second crash still yields logs.
	since := started.Add(-startupLogTimeSkew)
	until := finished.Add(startupLogTimeSkew)
	query := url.Values{"stdout": {"true"}, "stderr": {"true"}, "tail": {"50"}, "since": {since.Format(time.RFC3339Nano)}, "until": {until.Format(time.RFC3339Nano)}}
	req, err := r.client.newRequest(logctx, http.MethodGet, "/containers/"+url.PathEscape(expectedID)+"/logs?"+query.Encode(), nil)
	if err != nil {
		return "\ncontainerLogs: unavailable (build request failed)"
	}
	res, err := r.client.client.Do(req)
	if err != nil {
		return "\ncontainerLogs: unavailable (docker request failed)"
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Sprintf("\ncontainerLogs: unavailable (docker status %d)", res.StatusCode)
	}
	text, truncated, err := readStartupLogs(res.Body)
	if err != nil {
		return "\ncontainerLogs: unavailable (decode failed)"
	}
	if truncated {
		// A cut line can contain half a credential that exact redaction cannot
		// recognize. Retain complete lines only.
		if lastNewline := strings.LastIndexByte(text, '\n'); lastNewline >= 0 {
			text = text[:lastNewline+1]
		} else {
			text = ""
		}
	}
	text = redactStartupLogs(text, spec)
	runes := []rune(text)
	if len(runes) > startupLogRunes {
		text, truncated = string(runes[:startupLogRunes]), true
	}
	if strings.TrimSpace(text) == "" && !truncated {
		return "\ncontainerLogs: empty"
	}
	if truncated {
		text += "\n[truncated]"
	}
	return "\ncontainerLogs:\n" + text
}

func containerIDEqual(a, b string) bool {
	if a == b {
		return true
	}
	// Docker create/inspect may surface full or truncated IDs depending on path.
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func shortContainerID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func readStartupLogs(src io.Reader) (string, bool, error) {
	// Bound the wire response as well as decoded output, including malformed
	// zero-length frames. Closing the HTTP body stops further transfer.
	r := bufio.NewReader(io.LimitReader(src, 32<<10))
	header, err := r.Peek(8)
	framed := err == nil && header[0] <= 2 && header[1] == 0 && header[2] == 0 && header[3] == 0
	if !framed {
		b, err := io.ReadAll(io.LimitReader(r, startupLogBytes+1))
		return string(b[:min(len(b), startupLogBytes)]), len(b) > startupLogBytes, err
	}
	var out strings.Builder
	for {
		var h [8]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			if err == io.EOF {
				return out.String(), false, nil
			}
			return "", false, err
		}
		if h[0] > 2 || h[1] != 0 || h[2] != 0 || h[3] != 0 {
			return "", false, fmt.Errorf("invalid Docker log frame")
		}
		size := int64(binary.BigEndian.Uint32(h[4:]))
		remaining := int64(startupLogBytes - out.Len())
		if _, err := io.CopyN(&out, r, min(size, remaining)); err != nil {
			return "", false, err
		}
		if size >= remaining {
			return out.String(), true, nil
		}
	}
}

func redactStartupLogs(text string, spec appruntime.Spec) string {
	text = startupPrivateKey.ReplaceAllString(text, "[REDACTED PRIVATE KEY]")
	text = startupCertificatePEM.ReplaceAllString(text, "[REDACTED CERTIFICATE]")
	var values []string
	for _, value := range spec.Env {
		if value != "" {
			values = append(values, value)
		}
	}
	for _, file := range spec.Files {
		if len(file.Content) == 0 {
			continue
		}
		content := string(file.Content)
		// Full-file match covers secrets embedded in managed payloads without
		// erasing every short nginx/config directive from process errors.
		values = append(values, content)
		for _, block := range startupPrivateKey.FindAllString(content, -1) {
			values = append(values, block)
		}
		for _, block := range startupCertificatePEM.FindAllString(content, -1) {
			values = append(values, block)
		}
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if len(line) >= 8 && startupSensitiveLine.MatchString(line) {
				values = append(values, line)
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		text = strings.ReplaceAll(text, value, "[REDACTED]")
	}
	text = startupSecretAssignment.ReplaceAllString(text, "${1}[REDACTED]")
	text = containerDiagnosticBearer.ReplaceAllString(text, "Bearer [REDACTED]")
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, "�"))
}
