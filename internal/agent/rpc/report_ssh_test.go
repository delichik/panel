package rpc

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	agentpb "panel/internal/agent/pb"
	agentsshsessions "panel/internal/agent/sshsessions"
)

func writeSSHRecords(t *testing.T, name string, records ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	var data []byte
	for _, record := range records {
		data = append(data, record...)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sshUTMPRecord(typ int16, pid int32, line, user, host string, sec int64) []byte {
	rec := make([]byte, 384)
	binary.LittleEndian.PutUint16(rec[0:], uint16(typ))
	binary.LittleEndian.PutUint32(rec[4:], uint32(pid))
	copy(rec[8:40], line)
	copy(rec[44:76], user)
	copy(rec[76:332], host)
	binary.LittleEndian.PutUint32(rec[340:], uint32(sec))
	return rec
}

func TestSSHSessionSamplerSendsOnFirstSampleAndChange(t *testing.T) {
	utmp := writeSSHRecords(t, "utmp", sshUTMPRecord(7, 100, "pts/0", "root", "203.0.113.9", 1000))
	hub := newReportHub(okReportCollector{}, nil)
	hub.sshOptions.UTMPPath = utmp
	ch := make(chan *agentpb.AgentReport, 16)
	hub.watchers[1] = &reportWatcher{id: 1, cfg: reportConfig{serverID: "srv-1"}, ch: ch}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.runSSHSessionSampler(ctx)

	first := receiveSSHReport(t, ch)
	if first.SshSessions == nil || len(first.SshSessions.Items) != 1 {
		t.Fatalf("first report = %+v", first)
	}
	if got := first.SshSessions.Items[0]; got.UserName != "root" || got.Tty != "pts/0" || got.Kind != "ssh" {
		t.Fatalf("session = %+v", got)
	}

	// 指纹不变时 2 秒轮询不应重复上报。
	select {
	case report := <-ch:
		t.Fatalf("unexpected duplicate report: %+v", report)
	case <-time.After(2500 * time.Millisecond):
	}

	updated := writeSSHRecords(t, "utmp2",
		sshUTMPRecord(7, 100, "pts/0", "root", "203.0.113.9", 1000),
		sshUTMPRecord(7, 101, "pts/1", "alice", "198.51.100.7", 2000),
	)
	hub.sshOptions.UTMPPath = updated
	report := receiveSSHReport(t, ch)
	if report.SshSessions == nil || len(report.SshSessions.Items) != 2 {
		t.Fatalf("changed report = %+v", report)
	}
}

func TestSSHHistoryBackfillSplitsIntoBatches(t *testing.T) {
	var records [][]byte
	for i := 0; i < 1200; i++ {
		records = append(records, sshUTMPRecord(7, int32(i+1), "pts/0", "u", "", int64(1000+i)))
	}
	wtmp := writeSSHRecords(t, "wtmp", records...)
	hub := newReportHub(okReportCollector{}, nil)
	hub.sshOptions.WTMPPath = wtmp
	ch := make(chan *agentpb.AgentReport, 16)
	hub.watchers[1] = &reportWatcher{id: 1, cfg: reportConfig{serverID: "srv-1"}, ch: ch}

	hub.pushSSHHistory(context.Background(), time.Unix(0, 0))
	batches := 0
	total := 0
	for {
		select {
		case report := <-ch:
			if report.SshSessionHistory == nil {
				continue
			}
			batches++
			total += len(report.SshSessionHistory.Items)
			if len(report.SshSessionHistory.Items) > 500 {
				t.Fatalf("batch size = %d", len(report.SshSessionHistory.Items))
			}
		default:
			if batches == 0 {
				t.Fatal("no batches")
			}
			if batches != 3 || total != 1200 {
				t.Fatalf("batches = %d total = %d, want 3/1200", batches, total)
			}
			return
		}
	}
}

func TestNormalizeReportConfigCarriesSSHSince(t *testing.T) {
	cfg := normalizeReportConfig(&agentpb.AgentReportControl{SshSessionsSinceUnix: 12345})
	if cfg.sshSince != 12345 {
		t.Fatalf("sshSince = %d", cfg.sshSince)
	}
}

func TestSSHSessionSamplerImmediateBroadcastOnLiveWake(t *testing.T) {
	utmp := writeSSHRecords(t, "utmp", sshUTMPRecord(7, 100, "pts/0", "root", "203.0.113.9", 1000))
	hub := newReportHub(okReportCollector{}, nil)
	hub.sshOptions.UTMPPath = utmp
	ch := make(chan *agentpb.AgentReport, 16)
	hub.watchers[1] = &reportWatcher{id: 1, cfg: reportConfig{serverID: "srv-1"}, ch: ch}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.runSSHSessionSampler(ctx)

	_ = receiveSSHReport(t, ch)
	// 指纹未变，但新 watcher 接入触发 sshLiveWake 时应立即收到一次快照。
	hub.notifySSHLiveLocked()
	second := receiveSSHReport(t, ch)
	if second.SshSessions == nil || len(second.SshSessions.Items) != 1 {
		t.Fatalf("wake report = %+v", second)
	}
}

func TestSSHFailureSamplerPushesNewFailuresContinuously(t *testing.T) {
	hub := newReportHub(okReportCollector{}, nil)
	var mu sync.Mutex
	var current []agentsshsessions.Failure
	hub.sshOptions = agentsshsessions.Options{
		BTMPPath: filepath.Join(t.TempDir(), "missing-btmp"),
		JournalReader: func(ctx context.Context, since time.Time) ([]agentsshsessions.Failure, error) {
			mu.Lock()
			defer mu.Unlock()
			out := make([]agentsshsessions.Failure, 0, len(current))
			for _, item := range current {
				if !item.AttemptAt.Before(since) {
					out = append(out, item)
				}
			}
			return out, nil
		},
	}
	ch := make(chan *agentpb.AgentReport, 16)
	hub.watchers[1] = &reportWatcher{id: 1, cfg: reportConfig{serverID: "srv-1"}, ch: ch}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.runSSHFailureSampler(ctx)

	mu.Lock()
	current = append(current, agentsshsessions.Failure{
		UserName:      "root",
		SourceHost:    "198.51.100.77",
		SourceAddress: "198.51.100.77",
		AttemptAt:     time.Now().UTC().Truncate(time.Second),
		PID:           777,
	})
	mu.Unlock()

	report := receiveSSHReport(t, ch)
	if report.SshFailedAttempts == nil || len(report.SshFailedAttempts.Items) != 1 {
		t.Fatalf("failure report = %+v", report)
	}
	if got := report.SshFailedAttempts.Items[0]; got.UserName != "root" || got.SourceAddress != "198.51.100.77" || got.Pid != 777 {
		t.Fatalf("failure item = %+v", got)
	}
}

func receiveSSHReport(t *testing.T, ch chan *agentpb.AgentReport) *agentpb.AgentReport {
	t.Helper()
	select {
	case report := <-ch:
		return report
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSH report")
		return nil
	}
}
