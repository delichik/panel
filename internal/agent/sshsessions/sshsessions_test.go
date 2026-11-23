package sshsessions

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func utmpRecord(typ int16, pid int32, line, user, host string, sec int64, addr [4]uint32) []byte {
	rec := make([]byte, recordSize)
	binary.LittleEndian.PutUint16(rec[0:], uint16(typ))
	binary.LittleEndian.PutUint32(rec[4:], uint32(pid))
	copy(rec[8:40], line)
	copy(rec[44:76], user)
	copy(rec[76:332], host)
	binary.LittleEndian.PutUint32(rec[336:], 0)
	binary.LittleEndian.PutUint32(rec[340:], uint32(sec))
	binary.LittleEndian.PutUint32(rec[344:], 0)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(rec[348+i*4:], addr[i])
	}
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
	ipv4 := [4]uint32{0, 0, 0, binary.LittleEndian.Uint32([]byte{203, 0, 113, 9})}
	path := writeBinary(t, "utmp",
		utmpRecord(userProcess, 100, "pts/0", "root", "203.0.113.9", 1000, ipv4),
		utmpRecord(userProcess, 101, "tty1", "ops", "", 900, [4]uint32{}),
		utmpRecord(deadProcess, 100, "pts/0", "root", "203.0.113.9", 1100, ipv4),
		utmpRecord(userProcess, 102, "pts/0", "root", "203.0.113.9", 1200, ipv4),
	)
	items, err := ReadSessions(Options{UTMPPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("sessions = %d, want 2: %+v", len(items), items)
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
		t.Fatalf("source address = %q", ssh.SourceAddress)
	}
	local := byTTY["tty1"]
	if local.Kind != "local" || local.SourceAddress != "" {
		t.Fatalf("local session = %+v", local)
	}
}

func TestReadHistoryPairsLoginAndLogout(t *testing.T) {
	main := writeBinary(t, "wtmp",
		utmpRecord(userProcess, 200, "pts/1", "alice", "host-a", 2000, [4]uint32{0, 0, 0, binary.LittleEndian.Uint32([]byte{192, 0, 2, 5})}),
		utmpRecord(deadProcess, 200, "pts/1", "", "", 2600, [4]uint32{}),
		utmpRecord(userProcess, 201, "pts/2", "bob", "", 2100, [4]uint32{}),
		utmpRecord(userProcess, 202, "pts/3", "old", "host-b", 100, [4]uint32{}),
	)
	rotated := writeBinary(t, "wtmp.1",
		utmpRecord(userProcess, 199, "pts/0", "carol", "host-c", 1900, [4]uint32{}),
		utmpRecord(deadProcess, 199, "pts/0", "", "", 1950, [4]uint32{}),
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

func TestReadFailuresKeepsEveryAttempt(t *testing.T) {
	path := writeBinary(t, "btmp",
		utmpRecord(userProcess, 300, "ssh:notty", "admin", "198.51.100.7", 3000, [4]uint32{}),
		utmpRecord(userProcess, 301, "ssh:notty", "admin", "198.51.100.7", 3000, [4]uint32{}),
		utmpRecord(userProcess, 302, "ssh:notty", "root", "198.51.100.8", 2999, [4]uint32{}),
	)
	failures, err := ReadFailures(Options{BTMPPath: path}, time.Unix(3000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(failures.Items) != 2 || failures.Truncated {
		t.Fatalf("failures = %+v", failures)
	}
	if failures.Items[0].PID == failures.Items[1].PID {
		t.Fatal("same-second attempts must keep distinct pid")
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
		parts = append(parts, utmpRecord(userProcess, int32(400+i), "pts/0", "u", "", int64(1000+i), [4]uint32{}))
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
