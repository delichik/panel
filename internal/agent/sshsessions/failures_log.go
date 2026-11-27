package sshsessions

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	journalQueryTimeout = 10 * time.Second
	maxAuthLogBytes     = 16 << 20
)

var (
	// 匹配 OpenSSH 各类认证失败日志（含 Debian/Ubuntu 下 sshd/sshd-session/sshd-auth）。
	reFailedMethod = regexp.MustCompile(`^Failed (?:password|publickey|keyboard-interactive(?:/pam)?|gssapi-with-mic|hostbased) for (?:invalid user )?(\S+) from (\S+) port \d+`)
	reInvalidUser  = regexp.MustCompile(`^Invalid user (\S*) from (\S+)`)
	reNotAllowed   = regexp.MustCompile(`^User (\S+) from (\S+) not allowed because `)
	reRootRefused  = regexp.MustCompile(`^ROOT LOGIN REFUSED FROM (\S+)`)
	reMaxAttempts  = regexp.MustCompile(`maximum authentication attempts exceeded for (?:invalid user )?(\S+) from (\S+)`)
	rePreauthClose = regexp.MustCompile(`^(?:Connection closed by|Disconnected from) (?:authenticating|invalid) user (\S+) (\S+) port \d+ \[preauth\]`)

	// auth.log 传统 syslog 行：Nov 24 11:02:03 host sshd[1234]: msg
	reBSDAuthLine = regexp.MustCompile(`^([A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})\s+\S+\s+(?:sshd|sshd-session|sshd-auth)\[(\d+)\]:\s+(.*)$`)
	// auth.log ISO8601 行（Debian 12 / Ubuntu 24.04 rsyslog 默认）：2026-11-24T11:02:03.123456+00:00 host sshd[1234]: msg
	reISOAuthLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\S+)\s+\S+\s+(?:sshd|sshd-session|sshd-auth)\[(\d+)\]:\s+(.*)$`)
)

// ReadAllFailures 合并 btmp 与 sshd 认证日志（优先 systemd journal，回退 auth.log）
// 中的失败登录尝试，按 (user, source, unix_sec, pid) 去重并按时间升序返回。
// Debian/Ubuntu 下 OpenSSH 默认不把远程 SSH 失败写入 /var/log/btmp，只写 journal/auth.log，
// 因此需要多源合并才能准确捕获所有登录失败。
func ReadAllFailures(ctx context.Context, opts Options, since time.Time) (Failures, error) {
	limit := opts.MaxItems
	if limit <= 0 {
		limit = DefaultMaxItems
	}
	var merged []Failure
	var errs []error
	succeeded := false

	if btmp, err := ReadFailures(opts, since); err == nil {
		succeeded = true
		merged = append(merged, btmp.Items...)
	} else {
		errs = append(errs, err)
	}

	journalOK := false
	if opts.JournalReader != nil {
		if items, err := opts.JournalReader(ctx, since); err == nil {
			succeeded = true
			journalOK = true
			merged = append(merged, items...)
		} else {
			errs = append(errs, err)
		}
	} else if !opts.DisableJournal {
		if items, err := readJournalFailures(ctx, since); err == nil {
			succeeded = true
			journalOK = true
			merged = append(merged, items...)
		} else {
			errs = append(errs, err)
		}
	}

	// journal 不可用或调用方显式指定了 AuthLogPath 时读取 auth.log。
	if !journalOK || opts.AuthLogPath != "" {
		if items, err := readAuthLogFailures(opts, since); err == nil {
			succeeded = true
			merged = append(merged, items...)
		} else if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	if !succeeded && len(errs) > 0 {
		return Failures{}, errs[0]
	}

	deduped := deduplicateFailures(merged)
	sort.SliceStable(deduped, func(i, j int) bool { return deduped[i].AttemptAt.Before(deduped[j].AttemptAt) })
	truncated := false
	if len(deduped) > limit {
		deduped = deduped[len(deduped)-limit:]
		truncated = true
	}
	return Failures{Items: deduped, Truncated: truncated}, nil
}

func deduplicateFailures(items []Failure) []Failure {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]Failure, 0, len(items))
	for _, item := range items {
		source := item.SourceAddress
		if source == "" {
			source = item.SourceHost
		}
		key := fmt.Sprintf("%s|%s|%d|%d", item.UserName, source, item.AttemptAt.UTC().Unix(), item.PID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

type journalEntry struct {
	RealtimeTimestamp string `json:"__REALTIME_TIMESTAMP"`
	PID               string `json:"_PID"`
	Message           any    `json:"MESSAGE"`
}

func readJournalFailures(ctx context.Context, since time.Time) ([]Failure, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, journalQueryTimeout)
	defer cancel()

	args := []string{"--no-pager", "--output=json", "-t", "sshd", "-t", "sshd-session", "-t", "sshd-auth"}
	if !since.IsZero() {
		args = append(args, fmt.Sprintf("--since=@%d", since.UTC().Unix()))
	}
	cmd := exec.CommandContext(queryCtx, "journalctl", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("journalctl ssh failures: %w", err)
	}
	return parseJournalJSON(&stdout, since), nil
}

func parseJournalJSON(r io.Reader, since time.Time) []Failure {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []Failure
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry journalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		msg, ok := entry.Message.(string)
		if !ok || msg == "" {
			continue
		}
		user, source, matched := parseSSHDMessage(msg)
		if !matched {
			continue
		}
		usec, err := strconv.ParseInt(entry.RealtimeTimestamp, 10, 64)
		if err != nil || usec <= 0 {
			continue
		}
		at := time.Unix(usec/1_000_000, 0).UTC()
		if !since.IsZero() && at.Before(since) {
			continue
		}
		pid, _ := strconv.Atoi(entry.PID)
		addr := resolveAddress(source, "")
		out = append(out, Failure{
			UserName:      user,
			SourceHost:    source,
			SourceAddress: addr,
			AttemptAt:     at,
			PID:           pid,
		})
	}
	return out
}

func readAuthLogFailures(opts Options, since time.Time) ([]Failure, error) {
	main := opts.AuthLogPath
	if main == "" {
		main = DefaultAuthLogPath
	}
	rotated := main + ".1"
	paths := []string{main}
	if _, err := os.Stat(rotated); err == nil {
		paths = []string{rotated, main}
	}
	now := time.Now().UTC()
	var out []Failure
	for _, path := range paths {
		items, err := parseAuthLogFile(path, since, now)
		if err != nil {
			if path == rotated && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if path == main {
				return nil, err
			}
			continue
		}
		out = append(out, items...)
	}
	return out, nil
}

func parseAuthLogFile(path string, since, now time.Time) ([]Failure, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxAuthLogBytes {
		if _, err := file.Seek(info.Size()-maxAuthLogBytes, io.SeekStart); err != nil {
			return nil, err
		}
	}
	scanner := bufio.NewScanner(io.LimitReader(file, maxAuthLogBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)
	var out []Failure
	for scanner.Scan() {
		line := scanner.Text()
		at, pid, msg, ok := parseAuthLogLine(line, now)
		if !ok {
			continue
		}
		if !since.IsZero() && at.Before(since) {
			continue
		}
		user, source, matched := parseSSHDMessage(msg)
		if !matched {
			continue
		}
		out = append(out, Failure{
			UserName:      user,
			SourceHost:    source,
			SourceAddress: resolveAddress(source, ""),
			AttemptAt:     at.Truncate(time.Second),
			PID:           pid,
		})
	}
	return out, scanner.Err()
}

func parseAuthLogLine(line string, now time.Time) (time.Time, int, string, bool) {
	if m := reISOAuthLine.FindStringSubmatch(line); m != nil {
		at, err := time.Parse(time.RFC3339Nano, m[1])
		if err != nil {
			return time.Time{}, 0, "", false
		}
		pid, _ := strconv.Atoi(m[2])
		return at.UTC(), pid, m[3], true
	}
	if m := reBSDAuthLine.FindStringSubmatch(line); m != nil {
		parsed, err := time.ParseInLocation("Jan _2 15:04:05", m[1], now.Location())
		if err != nil {
			return time.Time{}, 0, "", false
		}
		at := time.Date(now.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, now.Location())
		// 处理跨年日志（例如 1 月读到 12 月末的记录）。
		if at.After(now.Add(24 * time.Hour)) {
			at = at.AddDate(-1, 0, 0)
		}
		pid, _ := strconv.Atoi(m[2])
		return at.UTC(), pid, m[3], true
	}
	return time.Time{}, 0, "", false
}

// parseSSHDMessage 从单条 sshd 日志正文中提取失败尝试的用户名与来源地址。
func parseSSHDMessage(msg string) (user, source string, ok bool) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", "", false
	}
	if m := reFailedMethod.FindStringSubmatch(msg); m != nil {
		return m[1], cleanSource(m[2]), true
	}
	if m := reInvalidUser.FindStringSubmatch(msg); m != nil {
		return m[1], cleanSource(m[2]), true
	}
	if m := reNotAllowed.FindStringSubmatch(msg); m != nil {
		return m[1], cleanSource(m[2]), true
	}
	if m := reRootRefused.FindStringSubmatch(msg); m != nil {
		return "root", cleanSource(m[1]), true
	}
	if m := reMaxAttempts.FindStringSubmatch(msg); m != nil {
		return m[1], cleanSource(m[2]), true
	}
	if m := rePreauthClose.FindStringSubmatch(msg); m != nil {
		return m[1], cleanSource(m[2]), true
	}
	return "", "", false
}

func cleanSource(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "[]")
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}
