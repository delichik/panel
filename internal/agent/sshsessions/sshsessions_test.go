package sshsessions

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ipv4Addr(a, b, c, d byte) [16]byte {
	var out [16]byte
	out[0], out[1], out[2], out[3] = a, b, c, d
	return out
}

func ipv6Addr(s string) [16]byte {
	var out [16]byte
	copy(out[:], net.ParseIP(s).To16())
	return out
}

func utmpRecord(typ int16, pid int32, line, user, host string, sec int64, addr [16]byte) []byte {
	rec := make([]byte, recordSize)
	binary.LittleEndian.PutUint16(rec[0:], uint16(typ))
	binary.LittleEndian.PutUint32(rec[4:], uint32(pid))
	copy(rec[8:40], line)
	copy(rec[44:76], user)
	copy(rec[76:332], host)
	binary.LittleEndian.PutUint32(rec[336:], 0)
	binary.LittleEndian.PutUint32(rec[340:], uint32(sec))
	binary.LittleEndian.PutUint32(rec[344:], 0)
	copy(rec[348:364], addr[:])
	return rec
}

func writeBinary(t *testing.T, name string, parts ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	var data []byte
	for _, part := range parts {
		data = append(data, part...)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSessionsParsesUTMP(t *testing.T) {
	v4 := ipv4Addr(203, 0, 113, 9)
	v6 := ipv6Addr("2001:db8::42")
	path := writeBinary(t, "utmp",
		utmpRecord(userProcess, 100, "pts/0", "root", "203.0.113.9", 1000, v4),
		utmpRecord(userProcess, 101, "tty1", "ops", "", 900, [16]byte{}),
		utmpRecord(userProcess, 103, "pts/2", "v6user", "2001:db8::42", 1050, v6),
		utmpRecord(deadProcess, 100, "pts/0", "root", "203.0.113.9", 1100, v4),
		utmpRecord(userProcess, 102, "pts/0", "root", "203.0.113.9", 1200, v4),
	)
	items, err := ReadSessions(Options{UTMPPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("sessions = %d, want 3: %+v", len(items), items)
	}
	byTTY := map[string]Session{}
	for _, item := range items {
		byTTY[item.TTY] = item
	}
	ssh := byTTY["pts/0"]
	if ssh.UserName != "root" || ssh.Kind != "ssh" || ssh.PID != 100 {
		t.Fatalf("ssh session = %+v", ssh)
	}
	if !ssh.LoginAt.Equal(time.Unix(1000, 0)) {
		t.Fatalf("login time = %v, want earliest", ssh.LoginAt)
	}
	if ssh.SourceAddress != "203.0.113.9" {
		t.Fatalf("source address = %q, want 203.0.113.9", ssh.SourceAddress)
	}
	if got := byTTY["pts/2"]; got.SourceAddress != "2001:db8::42" || got.Kind != "ssh" {
		t.Fatalf("ipv6 session = %+v", got)
	}
	local := byTTY["tty1"]
	if local.Kind != "local" || local.SourceAddress != "" {
		t.Fatalf("local session = %+v", local)
	}
}

func TestReadHistoryPairsLoginAndLogout(t *testing.T) {
	main := writeBinary(t, "wtmp",
		utmpRecord(userProcess, 200, "pts/1", "alice", "host-a", 2000, ipv4Addr(192, 0, 2, 5)),
		utmpRecord(deadProcess, 200, "pts/1", "", "", 2600, [16]byte{}),
		utmpRecord(userProcess, 201, "pts/2", "bob", "", 2100, [16]byte{}),
		utmpRecord(userProcess, 202, "pts/3", "old", "host-b", 100, [16]byte{}),
		utmpRecord(deadProcess, 202, "pts/3", "", "", 200, [16]byte{}),
	)
	rotated := writeBinary(t, "wtmp.1",
		utmpRecord(userProcess, 199, "pts/0", "carol", "host-c", 1900, [16]byte{}),
		utmpRecord(deadProcess, 199, "pts/0", "", "", 1950, [16]byte{}),
	)
	history, err := ReadHistory(Options{WTMPPath: main, WTMPRotatedPath: rotated}, time.Unix(1500, 0))
	if err != nil {
		t.Fatal(err)
	}
	if history.Truncated {
		t.Fatal("history should not be truncated")
	}
	got := map[string]HistoryItem{}
	for _, item := range history.Items {
		got[item.TTY] = item
	}
	if len(got) != 3 {
		t.Fatalf("history items = %d, want 3: %+v", len(got), history.Items)
	}
	if item := got["pts/1"]; !item.LogoutAt.Equal(time.Unix(2600, 0)) || item.SourceAddress != "192.0.2.5" {
		t.Fatalf("paired item = %+v", item)
	}
	if item := got["pts/2"]; !item.LogoutAt.IsZero() {
		t.Fatalf("unpaired item logout = %v", item.LogoutAt)
	}
	if item := got["pts/0"]; !item.LoginAt.Equal(time.Unix(1900, 0)) || !item.LogoutAt.Equal(time.Unix(1950, 0)) {
		t.Fatalf("rotated item = %+v", item)
	}
	if _, ok := got["pts/3"]; ok {
		t.Fatal("item older than since must be skipped")
	}
}

func TestReadHistoryFallbackPairingAndBootTime(t *testing.T) {
	main := writeBinary(t, "wtmp",
		// 登录早于 since，但登出于 since 之后，且 DEAD_PROCESS pid=0。
		utmpRecord(userProcess, 500, "pts/0", "alice", "192.0.2.10", 1000, ipv4Addr(192, 0, 2, 10)),
		utmpRecord(deadProcess, 0, "pts/0", "", "", 2200, [16]byte{}),
		// 未写 DEAD_PROCESS 就遇到 BOOT_TIME，应按重启时间关闭。
		utmpRecord(userProcess, 501, "pts/1", "bob", "192.0.2.11", 2300, ipv4Addr(192, 0, 2, 11)),
		utmpRecord(bootTime, 0, "~", "reboot", "", 2500, [16]byte{}),
		// 同 tty 被新会话复用，旧会话应按新登录时间关闭。
		utmpRecord(userProcess, 502, "pts/2", "carol", "192.0.2.12", 2600, ipv4Addr(192, 0, 2, 12)),
		utmpRecord(userProcess, 503, "pts/2", "dave", "192.0.2.13", 2700, ipv4Addr(192, 0, 2, 13)),
	)
	history, err := ReadHistory(Options{WTMPPath: main}, time.Unix(2000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Items) != 4 {
		t.Fatalf("items = %d, want 4: %+v", len(history.Items), history.Items)
	}
	byUser := map[string]HistoryItem{}
	for _, item := range history.Items {
		byUser[item.UserName] = item
	}
	if !byUser["alice"].LogoutAt.Equal(time.Unix(2200, 0)) {
		t.Fatalf("alice logout = %v, want 2200", byUser["alice"].LogoutAt)
	}
	if !byUser["bob"].LogoutAt.Equal(time.Unix(2500, 0)) {
		t.Fatalf("bob logout = %v, want 2500 (bootTime)", byUser["bob"].LogoutAt)
	}
	if !byUser["carol"].LogoutAt.Equal(time.Unix(2700, 0)) {
		t.Fatalf("carol logout = %v, want 2700 (tty reuse)", byUser["carol"].LogoutAt)
	}
	if !byUser["dave"].LogoutAt.IsZero() {
		t.Fatalf("dave should remain open, got %v", byUser["dave"].LogoutAt)
	}
}

func TestReadFailuresKeepsLoginAndUserProcessAndRotated(t *testing.T) {
	main := writeBinary(t, "btmp",
		// 真实 Linux sshd/PAM 写 LOGIN_PROCESS(6) 到 btmp。
		utmpRecord(loginProcess, 300, "ssh:notty", "admin", "198.51.100.7", 3000, [16]byte{}),
		utmpRecord(userProcess, 301, "ssh:notty", "admin", "198.51.100.7", 3000, ipv4Addr(198, 51, 100, 7)),
		utmpRecord(loginProcess, 302, "ssh:notty", "root", "198.51.100.8", 2999, [16]byte{}),
	)
	rotated := writeBinary(t, "btmp.1",
		utmpRecord(loginProcess, 299, "ssh:notty", "ops", "198.51.100.9", 3001, [16]byte{}),
	)
	failures, err := ReadFailures(Options{BTMPPath: main, BTMPRotatedPath: rotated}, time.Unix(3000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(failures.Items) != 3 || failures.Truncated {
		t.Fatalf("failures = %+v", failures)
	}
	if failures.Items[0].SourceAddress != "198.51.100.7" {
		t.Fatalf("fallback IP from host failed: %+v", failures.Items[0])
	}
	if failures.Items[0].PID == failures.Items[1].PID {
		t.Fatal("same-second attempts must keep distinct pid")
	}
}

func TestParseJournalAndAuthLogFailuresAndMerge(t *testing.T) {
	// 1. Journal JSON parsing
	journalData := strings.Join([]string{
		`{"__REALTIME_TIMESTAMP":"1700000100000000","_PID":"4001","MESSAGE":"Failed password for invalid user ghost from 203.0.113.50 port 51234 ssh2"}`,
		`{"__REALTIME_TIMESTAMP":"1700000100000000","_PID":"4001","MESSAGE":"Invalid user ghost from 203.0.113.50 port 51234"}`,
		`{"__REALTIME_TIMESTAMP":"1700000105000000","_PID":"4002","MESSAGE":"Failed publickey for root from 2001:db8::9 port 44321 ssh2: ED25519 SHA256:xxx"}`,
		`{"__REALTIME_TIMESTAMP":"1700000110000000","_PID":"4003","MESSAGE":"Connection closed by authenticating user deploy 198.51.100.22 port 33210 [preauth]"}`,
		`{"__REALTIME_TIMESTAMP":"1700000115000000","_PID":"4004","MESSAGE":"Accepted publickey for root from 198.51.100.22 port 33211 ssh2"}`,
	}, "\n")
	parsed := parseJournalJSON(strings.NewReader(journalData), time.Unix(1700000000, 0))
	if len(parsed) != 4 {
		t.Fatalf("raw parsed journal = %d, want 4: %+v", len(parsed), parsed)
	}

	// 2. Auth.log parsing (ISO8601 + BSD syslog)
	authPath := filepath.Join(t.TempDir(), "auth.log")
	authContent := strings.Join([]string{
		"2026-11-24T10:00:01.123456+00:00 srv sshd-session[5001]: Failed password for root from 192.0.2.88 port 50000 ssh2",
		"2026-11-24T10:00:02.000000+00:00 srv sshd[5002]: User test from 192.0.2.89 not allowed because not listed in AllowUsers",
		"2026-11-24T10:00:03.000000+00:00 srv sshd[5003]: ROOT LOGIN REFUSED FROM 192.0.2.90",
		"2026-11-24T10:00:04.000000+00:00 srv sshd[5004]: error: maximum authentication attempts exceeded for invalid user badguy from 192.0.2.91 port 41111 ssh2 [preauth]",
	}, "\n")
	if err := os.WriteFile(authPath, []byte(authContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// 3. Unified ReadAllFailures merges and deduplicates same-second same-pid journal entries
	btmpPath := writeBinary(t, "btmp",
		utmpRecord(loginProcess, 4001, "ssh:notty", "ghost", "203.0.113.50", 1700000100, ipv4Addr(203, 0, 113, 50)),
	)
	res, err := ReadAllFailures(context.Background(), Options{
		BTMPPath:    btmpPath,
		AuthLogPath: authPath,
		JournalReader: func(ctx context.Context, since time.Time) ([]Failure, error) {
			return parsed, nil
		},
	}, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	// btmp(4001) + journal(4001 x2) deduplicate to 1, plus 4002, 4003, plus 4 auth.log entries = 7 total.
	if len(res.Items) != 7 {
		t.Fatalf("merged failures = %d, want 7: %+v", len(res.Items), res.Items)
	}
}

func TestReadSessionsMissingFileReturnsError(t *testing.T) {
	if _, err := ReadSessions(Options{UTMPPath: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("expected error for missing utmp")
	}
}

func TestReadHistoryTruncatesToMaxItems(t *testing.T) {
	var parts [][]byte
	for i := 0; i < 5; i++ {
		parts = append(parts, utmpRecord(userProcess, int32(400+i), "pts/0", "u", "", int64(1000+i), [16]byte{}))
	}
	path := writeBinary(t, "wtmp", parts...)
	history, err := ReadHistory(Options{WTMPPath: path, MaxItems: 3}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !history.Truncated || len(history.Items) != 3 {
		t.Fatalf("history = %+v", history)
	}
	if history.Items[0].LoginAt.Unix() != 1002 {
		t.Fatalf("truncation should keep newest items, got first=%v", history.Items[0].LoginAt)
	}
}
