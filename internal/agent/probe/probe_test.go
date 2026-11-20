package probe

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const iputilsOutput = `PING 10.0.0.2 (10.0.0.2) 56(84) bytes of data.
64 bytes from 10.0.0.2: icmp_seq=1 ttl=64 time=0.045 ms
64 bytes from 10.0.0.2: icmp_seq=2 ttl=64 time=0.050 ms
64 bytes from 10.0.0.2: icmp_seq=3 ttl=64 time=0.061 ms

--- 10.0.0.2 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2042ms
rtt min/avg/max/mdev = 0.045/0.052/0.061/0.008 ms
`

const busyboxOutput = `PING 10.0.0.3 (10.0.0.3): 56 data bytes
64 bytes from 10.0.0.3: seq=0 ttl=64 time=0.320 ms
64 bytes from 10.0.0.3: seq=1 ttl=64 time=0.400 ms

--- 10.0.0.3 ping statistics ---
2 packets transmitted, 2 packets received, 0% packet loss
round-trip min/avg/max = 0.320/0.360/0.400 ms
`

func TestParsePingOutputIPUtils(t *testing.T) {
	stats, err := ParsePingOutput(iputilsOutput)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sent != 3 || stats.Received != 3 || stats.PacketLossPercent != 0 {
		t.Fatalf("unexpected iputils stats: %#v", stats)
	}
	if stats.LatencyMS != 0.052 {
		t.Fatalf("latency = %v, want 0.052", stats.LatencyMS)
	}
}

func TestParsePingOutputBusybox(t *testing.T) {
	stats, err := ParsePingOutput(busyboxOutput)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sent != 2 || stats.Received != 2 || stats.PacketLossPercent != 0 {
		t.Fatalf("unexpected busybox stats: %#v", stats)
	}
	if stats.LatencyMS != 0.360 {
		t.Fatalf("latency = %v, want 0.360", stats.LatencyMS)
	}
}

func TestParsePingOutputPartialLoss(t *testing.T) {
	output := `--- 10.0.0.4 ping statistics ---
3 packets transmitted, 2 received, 33% packet loss, time 2042ms
rtt min/avg/max/mdev = 0.100/0.200/0.300/0.100 ms
`
	stats, err := ParsePingOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Sent != 3 || stats.Received != 2 || stats.PacketLossPercent != 33 || stats.LatencyMS != 0.2 {
		t.Fatalf("unexpected partial loss stats: %#v", stats)
	}
}

func TestParsePingOutputComputesLossWithoutExplicitLine(t *testing.T) {
	output := "3 packets transmitted, 1 received\n"
	stats, err := ParsePingOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PacketLossPercent < 66.6 || stats.PacketLossPercent > 66.7 {
		t.Fatalf("computed loss = %v, want 66.67", stats.PacketLossPercent)
	}
}

func TestParsePingOutputAveragesReplyTimesWhenRTTSummaryMissing(t *testing.T) {
	output := `PING 10.0.0.5 (10.0.0.5) 56(84) bytes of data.
64 bytes from 10.0.0.5: icmp_seq=1 ttl=64 time=1.0 ms
64 bytes from 10.0.0.5: icmp_seq=2 ttl=64 time=3.0 ms

--- 10.0.0.5 ping statistics ---
2 packets transmitted, 2 received, 0% packet loss
`
	stats, err := ParsePingOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if stats.LatencyMS != 2 {
		t.Fatalf("latency = %v, want 2", stats.LatencyMS)
	}
}

func TestParsePingOutputRejectsMissingStatistics(t *testing.T) {
	if _, err := ParsePingOutput("ping: usage error: Destination address required"); err == nil {
		t.Fatal("expected an error for output without packet statistics")
	}
}

func TestRunUsesInjectedRunnerAndKeepsTargetOrder(t *testing.T) {
	targets := []Target{
		{ServerID: "s1", Address: "10.0.0.1"},
		{ServerID: "s2", Address: "10.0.0.2"},
		{ServerID: "s3", Address: "10.0.0.3"},
	}
	runner := func(_ context.Context, address string) (string, error) {
		if address == "10.0.0.2" {
			return "", errors.New("ping: command not found")
		}
		return strings.ReplaceAll(iputilsOutput, "10.0.0.2", address), nil
	}
	results := Run(context.Background(), targets, Options{}, runner)
	if len(results) != len(targets) {
		t.Fatalf("Run returned %d results, want %d", len(results), len(targets))
	}
	if results[0].TargetServerID != "s1" || results[1].TargetServerID != "s2" || results[2].TargetServerID != "s3" {
		t.Fatalf("result order does not match targets: %#v", results)
	}
	if results[0].LatencyMS != 0.052 || results[0].TargetAddress != "10.0.0.1" {
		t.Fatalf("unexpected first result: %#v", results[0])
	}
	if results[1].Error == "" {
		t.Fatal("expected an error for the failing target")
	}
}

func TestRunReportsAllLostTargetsWithError(t *testing.T) {
	output := `--- 10.0.0.9 ping statistics ---
3 packets transmitted, 0 received, 100% packet loss, time 2042ms
`
	runner := func(context.Context, string) (string, error) {
		return output, errors.New("exit status 1")
	}
	results := Run(context.Background(), []Target{{ServerID: "s9", Address: "10.0.0.9"}}, Options{}, runner)
	if len(results) != 1 {
		t.Fatalf("unexpected results: %#v", results)
	}
	if results[0].Sent != 3 || results[0].Received != 0 || results[0].PacketLossPercent != 100 {
		t.Fatalf("unexpected all-lost stats: %#v", results[0])
	}
	if results[0].Error == "" {
		t.Fatal("expected an error when no reply was received")
	}
}

func TestRunCapsConcurrency(t *testing.T) {
	var active, maxActive atomic.Int32
	runner := func(context.Context, string) (string, error) {
		current := active.Add(1)
		for {
			observed := maxActive.Load()
			if current <= observed || maxActive.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return iputilsOutput, nil
	}
	targets := make([]Target, 20)
	for index := range targets {
		targets[index] = Target{ServerID: fmt.Sprintf("s%d", index), Address: fmt.Sprintf("10.0.1.%d", index+1)}
	}
	Run(context.Background(), targets, Options{}, runner)
	if got := maxActive.Load(); got > MaxConcurrency {
		t.Fatalf("max concurrent probes = %d, want at most %d", got, MaxConcurrency)
	}
}

func TestOptionsNormalizeDefaultsAndClamps(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Options
		want Options
	}{
		{name: "zero falls back to defaults", in: Options{}, want: Options{Count: DefaultCount, TimeoutSeconds: DefaultTimeoutSeconds}},
		{name: "negative falls back to defaults", in: Options{Count: -2, TimeoutSeconds: -3}, want: Options{Count: DefaultCount, TimeoutSeconds: DefaultTimeoutSeconds}},
		{name: "custom values kept", in: Options{Count: 5, TimeoutSeconds: 4}, want: Options{Count: 5, TimeoutSeconds: 4}},
		{name: "count clamped high", in: Options{Count: 99, TimeoutSeconds: 2}, want: Options{Count: MaxCount, TimeoutSeconds: 2}},
		{name: "timeout clamped high", in: Options{Count: 3, TimeoutSeconds: 99}, want: Options{Count: 3, TimeoutSeconds: MaxTimeoutSeconds}},
		{name: "single values kept at bounds", in: Options{Count: 1, TimeoutSeconds: 1}, want: Options{Count: 1, TimeoutSeconds: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Normalize(); got != tc.want {
				t.Fatalf("Normalize(%+v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestOptionsTargetTimeoutFollowsPingBudget(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want time.Duration
	}{
		// 默认 3 包 * 2 秒 + 2 秒余量。
		{name: "defaults", opts: Options{}, want: 8 * time.Second},
		{name: "custom", opts: Options{Count: 5, TimeoutSeconds: 4}, want: 22 * time.Second},
		// 最小组合 1*1+2=3 秒，正好落在下限。
		{name: "minimal combination", opts: Options{Count: 1, TimeoutSeconds: 1}, want: 3 * time.Second},
		// 越界输入先夹紧再计算。
		{name: "clamped", opts: Options{Count: 50, TimeoutSeconds: 50}, want: 102 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.targetTimeout(); got != tc.want {
				t.Fatalf("targetTimeout(%+v) = %s, want %s", tc.opts, got, tc.want)
			}
		})
	}
}

func TestRunUsesConfiguredPerTargetTimeout(t *testing.T) {
	runner := func(ctx context.Context, _ string) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("probe runner context must carry a deadline")
			return "", errors.New("missing deadline")
		}
		// 5 包 * 4 秒 + 2 秒余量 = 22 秒；允许极小的时间流逝误差。
		if remaining := time.Until(deadline); remaining <= 21*time.Second || remaining > 22*time.Second {
			t.Errorf("per-target timeout = %s, want ~22s", remaining)
		}
		return iputilsOutput, nil
	}
	Run(context.Background(), []Target{{ServerID: "s1", Address: "10.0.0.1"}}, Options{Count: 5, TimeoutSeconds: 4}, runner)
}

func TestPingArgsBuildsIPv4AndIPv6Commands(t *testing.T) {
	defaults := Options{}
	wantV4 := []string{"-n", "-c", "3", "-W", "2", "10.0.0.1"}
	if got := pingArgs("10.0.0.1", defaults); !reflect.DeepEqual(got, wantV4) {
		t.Fatalf("pingArgs(ipv4) = %#v, want %#v", got, wantV4)
	}
	wantV6 := []string{"-6", "-n", "-c", "3", "-W", "2", "fd00::1"}
	if got := pingArgs("fd00::1", defaults); !reflect.DeepEqual(got, wantV6) {
		t.Fatalf("pingArgs(ipv6) = %#v, want %#v", got, wantV6)
	}
	custom := Options{Count: 5, TimeoutSeconds: 4}
	wantCustom := []string{"-n", "-c", "5", "-W", "4", "10.0.0.2"}
	if got := pingArgs("10.0.0.2", custom); !reflect.DeepEqual(got, wantCustom) {
		t.Fatalf("pingArgs(custom) = %#v, want %#v", got, wantCustom)
	}
	clamped := Options{Count: 99, TimeoutSeconds: 0}
	wantClamped := []string{"-n", "-c", "10", "-W", "2", "10.0.0.3"}
	if got := pingArgs("10.0.0.3", clamped); !reflect.DeepEqual(got, wantClamped) {
		t.Fatalf("pingArgs(clamped) = %#v, want %#v", got, wantClamped)
	}
}
