package settings

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/tailscale"
)

// fakeTailscaleController 是 panel-init 控制面的替身，记录收敛请求次数。
type fakeTailscaleController struct {
	supported bool
	status    tailscale.Status
	applyErr  error
	applies   int
}

func (f *fakeTailscaleController) Supported() bool { return f.supported }

func (f *fakeTailscaleController) Apply(context.Context) (tailscale.Status, error) {
	f.applies++
	if f.applyErr != nil {
		return tailscale.Status{}, f.applyErr
	}
	return f.status, nil
}

func (f *fakeTailscaleController) Status(context.Context) (tailscale.Status, error) {
	return f.status, nil
}

func TestRuntimeSettingsNeverExposeTailscaleAuthKey(t *testing.T) {
	controller := &fakeTailscaleController{supported: true}
	svc := newTailscaleTestService(t, controller)
	if _, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		ContainerReportIntervalSeconds:   30,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  "1d",
		Language:                         "en",
		LogLevel:                         "info",
		RemoteCommandTimeoutSeconds:      30,
		Tailscale:                        &RuntimeTailscaleUpdate{AuthKey: "tskey-auth-secret", Tags: []string{"tag:web"}},
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(svc.Runtime())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "tskey-auth-secret") {
		t.Fatalf("runtime settings leaked the tailscale auth key: %s", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	group, ok := decoded["tailscale"].(map[string]any)
	if !ok {
		t.Fatalf("runtime settings must expose a tailscale group: %s", raw)
	}
	if group["authKeyConfigured"] != true {
		t.Fatalf("authKeyConfigured = %v", group["authKeyConfigured"])
	}
	if _, exists := group["authKey"]; exists {
		t.Fatal("the auth key field must not be serialized")
	}

	// 期望态配置文件是 panel-init 的唯一输入，必须与设置一致。
	cfg, err := tailscale.ReadConfig(svc.cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.AuthKey != "tskey-auth-secret" || len(cfg.Tags) != 1 || cfg.Tags[0] != "tag:web" {
		t.Fatalf("tailscale config = %#v", cfg)
	}
	if controller.applies == 0 {
		t.Fatal("saving the tailscale group must request a container reconcile")
	}
}

func TestRuntimeSettingsKeepExistingTailscaleAuthKey(t *testing.T) {
	controller := &fakeTailscaleController{supported: true}
	svc := newTailscaleTestService(t, controller)
	ctx := context.Background()
	base := RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		ContainerReportIntervalSeconds:   30,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  "1d",
		Language:                         "en",
		LogLevel:                         "info",
		RemoteCommandTimeoutSeconds:      30,
	}
	first := base
	first.Tailscale = &RuntimeTailscaleUpdate{AuthKey: "tskey-auth-secret"}
	if _, err := svc.Update(ctx, first); err != nil {
		t.Fatal(err)
	}

	// 只改 tag 不得清空密钥。
	second := base
	second.Tailscale = &RuntimeTailscaleUpdate{Tags: []string{"tag:db"}}
	if _, err := svc.Update(ctx, second); err != nil {
		t.Fatal(err)
	}
	if _, tags := svc.TailscaleNodeCredentials(); len(tags) != 1 || tags[0] != "tag:db" {
		t.Fatalf("tags = %#v", tags)
	}
	key, _ := svc.TailscaleNodeCredentials()
	if key != "tskey-auth-secret" {
		t.Fatalf("auth key = %q, want the stored key to survive a tag-only update", key)
	}

	// 显式清除才移除密钥，并让容器期望态回到关闭。
	third := base
	third.Tailscale = &RuntimeTailscaleUpdate{ClearAuthKey: true}
	settings, err := svc.Update(ctx, third)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Tailscale.AuthKeyConfigured {
		t.Fatal("clearAuthKey must clear the configured flag")
	}
	if key, _ := svc.TailscaleNodeCredentials(); key != "" {
		t.Fatalf("auth key = %q, want it cleared", key)
	}
	cfg, err := tailscale.ReadConfig(svc.cfg.DataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatalf("cleared key must disable the container expectation: %#v", cfg)
	}
}

func TestRuntimeSettingsRejectInvalidTailscaleInput(t *testing.T) {
	base := RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		ContainerReportIntervalSeconds:   30,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  "1d",
		Language:                         "en",
		LogLevel:                         "info",
		RemoteCommandTimeoutSeconds:      30,
	}
	cases := map[string]RuntimeTailscaleUpdate{
		"key without prefix": {AuthKey: "not-a-key"},
		"tag without prefix": {AuthKey: "tskey-auth-x", Tags: []string{"web"}},
		"tag with space":     {AuthKey: "tskey-auth-x", Tags: []string{"tag:web app"}},
		"tag with underscore": {AuthKey: "tskey-auth-x", Tags: []string{"tag:web_app"}},
	}
	for name, group := range cases {
		t.Run(name, func(t *testing.T) {
			svc := newTailscaleTestService(t, &fakeTailscaleController{supported: true})
			input := base
			group := group
			input.Tailscale = &group
			if _, err := svc.Update(context.Background(), input); err == nil {
				t.Fatal("expected validation to reject the input")
			}
			if key, _ := svc.TailscaleNodeCredentials(); key != "" {
				t.Fatalf("rejected input must not persist a key, got %q", key)
			}
		})
	}
}

func TestRuntimeSettingsNormalizeTailscaleTags(t *testing.T) {
	svc := newTailscaleTestService(t, &fakeTailscaleController{supported: true})
	settings, err := svc.Update(context.Background(), RuntimeUpdate{
		MetricsRetentionDays:             7,
		MetricsCollectionIntervalSeconds: 60,
		ContainerReportIntervalSeconds:   30,
		CleanupSchedule:                  "daily",
		TokenExpiration:                  "1d",
		Language:                         "en",
		LogLevel:                         "info",
		RemoteCommandTimeoutSeconds:      30,
		// tag 大小写不敏感：统一规范化为小写并去重、排序。
		Tailscale: &RuntimeTailscaleUpdate{AuthKey: "tskey-auth-x", Tags: []string{" tag:Web ", "tag:web", "tag:db"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tags := settings.Tailscale.Tags
	if len(tags) != 2 || tags[0] != "tag:db" || tags[1] != "tag:web" {
		t.Fatalf("tags = %#v", tags)
	}
}

func TestContainerTailscaleUnavailableWithoutPanelInit(t *testing.T) {
	svc := newTailscaleTestService(t, &fakeTailscaleController{supported: false})
	state := svc.RefreshTailscaleContainer(context.Background())
	if state.Available {
		t.Fatalf("state = %#v, want unavailable when panel-init does not supervise the panel", state)
	}
	if _, err := svc.ApplyTailscaleContainer(context.Background()); err == nil {
		t.Fatal("apply must fail explicitly when the container tailscale cannot be managed")
	}
}

func TestContainerStateReflectsLiveTailscale(t *testing.T) {
	controller := &fakeTailscaleController{
		supported: true,
		status:    tailscale.Status{Available: true, Running: true, LoggedIn: true, Hostname: "seamark-panel", IPv4: "100.64.0.2"},
	}
	svc := newTailscaleTestService(t, controller)
	state := svc.RefreshTailscaleContainer(context.Background())
	if !state.Available || !state.LoggedIn || state.IPv4 != "100.64.0.2" {
		t.Fatalf("state = %#v", state)
	}
	if !tailscale.PanelReady() {
		t.Fatal("a logged-in panel must be marked tailnet ready")
	}
	t.Cleanup(func() { tailscale.SetPanelReady(false) })

	controller.status.LoggedIn = false
	controller.status.Running = false
	svc.RefreshTailscaleContainer(context.Background())
	if tailscale.PanelReady() {
		t.Fatal("panel readiness must be cleared when tailscaled stops")
	}
}

// newTailscaleTestService 构造一个使用替身控制面的设置服务。容器内 tailscale
// 的期望态配置文件写在测试临时目录中，因此所有断言都不触碰真实 tailscaled。
func newTailscaleTestService(t *testing.T, controller tailscale.InitController) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := NewService(store.AppDB(), cfg, WithTailscaleController(controller))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tailscale.SetPanelReady(false) })
	return svc
}
