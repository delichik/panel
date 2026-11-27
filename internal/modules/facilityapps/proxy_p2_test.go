package facilityapps

import (
	"context"
	"errors"
	"testing"
	"time"
)

func intPtr(value int) *int { return &value }

func TestFacilityProxySettingsImageUpdateIntervalDefaultsAndParsing(t *testing.T) {
	defaults := defaultFacilityProxySettings()
	if defaults.ImageUpdateIntervalSeconds != 0 {
		t.Fatalf("default image update interval = %d, want 0", defaults.ImageUpdateIntervalSeconds)
	}
	if parsed := parseFacilityProxySettings("{}"); parsed.ImageUpdateIntervalSeconds != 0 {
		t.Fatalf("empty document interval = %d, want 0", parsed.ImageUpdateIntervalSeconds)
	}
	if parsed := parseFacilityProxySettings(`{"imageUpdateIntervalSeconds":30}`); parsed.ImageUpdateIntervalSeconds != 30 {
		t.Fatalf("parsed interval = %d, want 30", parsed.ImageUpdateIntervalSeconds)
	}
	// 旧库脏值/越界值在解析与 resolved 时收敛到 0..600。
	if parsed := parseFacilityProxySettings(`{"imageUpdateIntervalSeconds":9999}`); parsed.ImageUpdateIntervalSeconds != maxImageUpdateIntervalSeconds {
		t.Fatalf("over-max stored interval = %d, want %d", parsed.ImageUpdateIntervalSeconds, maxImageUpdateIntervalSeconds)
	}
	if parsed := parseFacilityProxySettings(`{"imageUpdateIntervalSeconds":-5}`); parsed.ImageUpdateIntervalSeconds != 0 {
		t.Fatalf("negative stored interval = %d, want 0", parsed.ImageUpdateIntervalSeconds)
	}
	if resolved := (FacilityProxySettings{ImageUpdateIntervalSeconds: -1}).resolved(); resolved.ImageUpdateIntervalSeconds != 0 {
		t.Fatalf("resolved negative interval = %d, want 0", resolved.ImageUpdateIntervalSeconds)
	}
	if resolved := (FacilityProxySettings{ImageUpdateIntervalSeconds: 601}).resolved(); resolved.ImageUpdateIntervalSeconds != maxImageUpdateIntervalSeconds {
		t.Fatalf("resolved over-max interval = %d, want %d", resolved.ImageUpdateIntervalSeconds, maxImageUpdateIntervalSeconds)
	}
}

func TestNormalizeFacilityProxySettingsImageUpdateInterval(t *testing.T) {
	for _, value := range []int{0, 1, 30, maxImageUpdateIntervalSeconds} {
		normalized, err := normalizeFacilityProxySettings(FacilityProxySettings{ImageUpdateIntervalSeconds: value})
		if err != nil {
			t.Fatalf("interval %d must be accepted: %v", value, err)
		}
		if normalized.ImageUpdateIntervalSeconds != value {
			t.Fatalf("interval %d normalized to %d", value, normalized.ImageUpdateIntervalSeconds)
		}
	}
	for _, value := range []int{-1, maxImageUpdateIntervalSeconds + 1} {
		_, err := normalizeFacilityProxySettings(FacilityProxySettings{ImageUpdateIntervalSeconds: value})
		if err == nil {
			t.Fatalf("interval %d must be rejected", value)
		}
		assertFacilityPanelError(t, err, "facility_image_update_interval_invalid")
	}
}

func TestSaveProxySettingsImageUpdateIntervalRoundTrip(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()

	config, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{
		HTTPPortMode:               HTTPPortModeServe,
		ImageUpdateIntervalSeconds: intPtr(45),
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if config.Settings.ImageUpdateIntervalSeconds != 45 {
		t.Fatalf("saved interval = %d, want 45", config.Settings.ImageUpdateIntervalSeconds)
	}
	projected, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatalf("get reverse proxy: %v", err)
	}
	if projected.Settings.ImageUpdateIntervalSeconds != 45 {
		t.Fatalf("projected interval = %d, want 45", projected.Settings.ImageUpdateIntervalSeconds)
	}

	// 未提交（nil）保留现值。
	preserved, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: config.Version, HTTPPortMode: HTTPPortModeServe})
	if err != nil {
		t.Fatalf("partial save: %v", err)
	}
	if preserved.Settings.ImageUpdateIntervalSeconds != 45 {
		t.Fatalf("partial save interval = %d, want preserved 45", preserved.Settings.ImageUpdateIntervalSeconds)
	}

	// 显式 0 关闭等待。
	disabled, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: preserved.Version, HTTPPortMode: HTTPPortModeServe, ImageUpdateIntervalSeconds: intPtr(0)})
	if err != nil {
		t.Fatalf("explicit zero save: %v", err)
	}
	if disabled.Settings.ImageUpdateIntervalSeconds != 0 {
		t.Fatalf("explicit zero interval = %d, want 0", disabled.Settings.ImageUpdateIntervalSeconds)
	}

	// 越界值稳定报错且不落库。
	if _, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: disabled.Version, HTTPPortMode: HTTPPortModeServe, ImageUpdateIntervalSeconds: intPtr(601)}); err == nil {
		t.Fatal("interval 601 must be rejected")
	} else {
		assertFacilityPanelError(t, err, "facility_image_update_interval_invalid")
	}
	after, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Settings.ImageUpdateIntervalSeconds != 0 {
		t.Fatalf("rejected save must not persist: %d", after.Settings.ImageUpdateIntervalSeconds)
	}
}

func TestProxyImageUpdateIntervalFromSettings(t *testing.T) {
	if got := proxyImageUpdateInterval(ReverseProxyConfig{}); got != 0 {
		t.Fatalf("zero settings interval = %s, want 0", got)
	}
	if got := proxyImageUpdateInterval(ReverseProxyConfig{Settings: FacilityProxySettings{ImageUpdateIntervalSeconds: 45}}); got != 45*time.Second {
		t.Fatalf("interval = %s, want 45s", got)
	}
	// 未归一化的越界值按上限 600 秒处理，避免滚动任务被拖成小时级。
	if got := proxyImageUpdateInterval(ReverseProxyConfig{Settings: FacilityProxySettings{ImageUpdateIntervalSeconds: 9999}}); got != maxImageUpdateIntervalSeconds*time.Second {
		t.Fatalf("clamped interval = %s, want %ds", got, maxImageUpdateIntervalSeconds)
	}
}

type proxyImageRolloutEvent struct {
	kind   string // rollout | sleep
	server string
	delay  time.Duration
}

func TestRolloutProxyImageGatewaysWaitsBetweenAdjacentGateways(t *testing.T) {
	recordSleeper := func(events *[]proxyImageRolloutEvent) func(context.Context, time.Duration) error {
		return func(_ context.Context, d time.Duration) error {
			*events = append(*events, proxyImageRolloutEvent{kind: "sleep", delay: d})
			return nil
		}
	}
	rolloutRecorder := func(events *[]proxyImageRolloutEvent) func(context.Context, string) error {
		return func(_ context.Context, serverID string) error {
			*events = append(*events, proxyImageRolloutEvent{kind: "rollout", server: serverID})
			return nil
		}
	}

	events := []proxyImageRolloutEvent{}
	svc := &Service{updateIntervalSleeper: recordSleeper(&events)}
	reporter := proxyImageReporter{}
	if err := svc.rolloutProxyImageGateways(context.Background(), []string{"srv-a", "srv-b", "srv-c"}, 2*time.Second, reporter, rolloutRecorder(&events)); err != nil {
		t.Fatalf("rollout gateways: %v", err)
	}
	want := []proxyImageRolloutEvent{
		{kind: "rollout", server: "srv-a"},
		{kind: "sleep", delay: 2 * time.Second},
		{kind: "rollout", server: "srv-b"},
		{kind: "sleep", delay: 2 * time.Second},
		{kind: "rollout", server: "srv-c"},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	for index := range want {
		if events[index].kind != want[index].kind {
			t.Fatalf("event %d = %#v, want %#v", index, events[index], want[index])
		}
		if events[index].kind == "rollout" && events[index].server != want[index].server {
			t.Fatalf("rollout order = %#v, want %#v", events, want)
		}
		if events[index].kind == "sleep" && events[index].delay != want[index].delay {
			t.Fatalf("sleep %d = %s, want %s", index, events[index].delay, want[index].delay)
		}
	}

	// 间隔 0 不等待。
	zeroEvents := []proxyImageRolloutEvent{}
	svc = &Service{updateIntervalSleeper: recordSleeper(&zeroEvents)}
	if err := svc.rolloutProxyImageGateways(context.Background(), []string{"srv-a", "srv-b"}, 0, reporter, rolloutRecorder(&zeroEvents)); err != nil {
		t.Fatalf("zero interval rollout: %v", err)
	}
	if len(zeroEvents) != 2 {
		t.Fatalf("zero interval events = %#v, want two rollouts without sleep", zeroEvents)
	}
}

func TestRolloutProxyImageGatewaysStopsOnFailure(t *testing.T) {
	events := []proxyImageRolloutEvent{}
	svc := &Service{updateIntervalSleeper: func(_ context.Context, d time.Duration) error {
		events = append(events, proxyImageRolloutEvent{kind: "sleep", delay: d})
		return nil
	}}
	rolloutErr := errors.New("rollout failed")
	err := svc.rolloutProxyImageGateways(context.Background(), []string{"srv-a", "srv-b", "srv-c"}, time.Second, proxyImageReporter{}, func(_ context.Context, serverID string) error {
		events = append(events, proxyImageRolloutEvent{kind: "rollout", server: serverID})
		if serverID == "srv-b" {
			return rolloutErr
		}
		return nil
	})
	if !errors.Is(err, rolloutErr) {
		t.Fatalf("rollout error = %v, want %v", err, rolloutErr)
	}
	// srv-a 完成 → 等待 → srv-b 失败；srv-c 未被滚动，失败后也不再等待。
	want := []proxyImageRolloutEvent{
		{kind: "rollout", server: "srv-a"},
		{kind: "sleep", delay: time.Second},
		{kind: "rollout", server: "srv-b"},
	}
	if len(events) != len(want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
	for index := range want {
		if events[index].kind != want[index].kind || events[index].server != want[index].server {
			t.Fatalf("events = %#v, want %#v", events, want)
		}
	}
}

func TestRolloutProxyImageGatewaysWaitHonorsContextCancellation(t *testing.T) {
	// 不注入 sleeper：走默认 ctx-aware 等待，取消的 ctx 应立即返回而不真实等待。
	svc := &Service{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rolled := []string{}
	err := svc.rolloutProxyImageGateways(ctx, []string{"srv-a", "srv-b"}, time.Hour, proxyImageReporter{}, func(_ context.Context, serverID string) error {
		rolled = append(rolled, serverID)
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context.Canceled", err)
	}
	if len(rolled) != 1 || rolled[0] != "srv-a" {
		t.Fatalf("rolled = %#v, want only srv-a before cancellation", rolled)
	}
}
