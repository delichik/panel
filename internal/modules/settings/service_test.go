package settings

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/reconciletrace"
)

type failingPanelTLSProvider struct{ err error }

func (p failingPanelTLSProvider) AssetType(context.Context, string) (string, error) {
	return "tls_certificate", nil
}

func (p failingPanelTLSProvider) ReadFile(context.Context, string, string) ([]byte, string, error) {
	return nil, "", p.err
}

func (p failingPanelTLSProvider) SyncPanelTLS(context.Context, string, string) error { return p.err }

func newTestService(t *testing.T) *Service {
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
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRuntimeSettingsUpdatePersists(t *testing.T) {
	svc := newTestService(t)
	got, err := svc.Update(context.Background(), RuntimeUpdate{
		ContainerReportIntervalSeconds: 3,
		TokenExpiration:                TokenExpiration5Days,
		Language:                       "zh-CN",
		LogLevel:                       "debug",
		RemoteCommandTimeoutSeconds:    45,
		Branding:                       &RuntimeBrandingSettings{LoginTitle: "Operations", LoginSubtitle: "Manage infrastructure"},
		Certificates:                   &RuntimeCertificateSettings{Email: "admin@example.com", DNSPropagationDelaySeconds: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContainerReportIntervalSeconds != 3 || got.TokenExpiration != TokenExpiration5Days || got.Language != "zh-CN" || got.LogLevel != "debug" || got.RemoteCommandTimeoutSeconds != 45 || got.Branding.LoginTitle != "Operations" || got.Branding.LoginSubtitle != "Manage infrastructure" || got.Certificates.Email != "admin@example.com" || got.Certificates.DNSPropagationDelaySeconds != 10 {
		t.Fatalf("unexpected runtime settings: %#v", got)
	}
	if got := svc.Runtime(); got.ContainerReportIntervalSeconds != 3 {
		t.Fatalf("runtime cache did not update: %#v", got)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Runtime().Branding != got.Branding {
		t.Fatalf("branding settings were not persisted: %#v", reloaded.Runtime().Branding)
	}
	if reloaded.Runtime().LogLevel != "debug" {
		t.Fatalf("log level was not persisted: %q", reloaded.Runtime().LogLevel)
	}
}

func TestRuntimeSettingsDoesNotPersistPanelSelectionWhenTLSActivationFails(t *testing.T) {
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
	svc, err := NewService(store.AppDB(), cfg, WithTLSAssetProvider(failingPanelTLSProvider{err: errors.New("write fixed certificate")}))
	if err != nil {
		t.Fatal(err)
	}
	current := svc.Runtime()
	_, err = svc.Update(context.Background(), RuntimeUpdate{
		ContainerReportIntervalSeconds: current.ContainerReportIntervalSeconds,
		TokenExpiration:                current.TokenExpiration,
		Language:                       current.Language,
		LogLevel:                       current.LogLevel,
		RemoteCommandTimeoutSeconds:    current.RemoteCommandTimeoutSeconds,
		Panel:                          &RuntimePanelSettings{Domain: "panel.example.test"},
	})
	if err == nil {
		t.Fatal("expected Panel TLS activation failure")
	}
	if got := svc.Runtime().Panel; got != current.Panel {
		t.Fatalf("runtime Panel settings changed despite activation failure: %#v", got)
	}
	var domain string
	if err := store.AppDB().QueryRow(`SELECT value FROM runtime_settings WHERE key='panel.domain'`).Scan(&domain); err != nil {
		t.Fatal(err)
	}
	if domain != current.Panel.Domain {
		t.Fatalf("persisted Panel domain = %q, want %q", domain, current.Panel.Domain)
	}
}

// TestRuntimeSettingsReconcileTracePersists 验证协调追踪开关的持久化与开关
// 同步：更新后内存缓存、重新加载后的值，以及 reconciletrace 包的运行时状态。
func TestRuntimeSettingsReconcileTracePersists(t *testing.T) {
	svc := newTestService(t)
	enabled := true
	got, err := svc.Update(context.Background(), RuntimeUpdate{
		ContainerReportIntervalSeconds: 30,
		TokenExpiration:                TokenExpiration1Day,
		Language:                       "en",
		LogLevel:                       "info",
		RemoteCommandTimeoutSeconds:    30,
		ReconcileTraceEnabled:          &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReconcileTraceEnabled {
		t.Fatalf("reconcile trace was not enabled: %#v", got)
	}
	if !reconciletrace.Enabled() {
		t.Fatal("reconcile trace switch was not applied to the tracing package")
	}
	// 未传该字段时保持当前值（*bool 语义）。
	got, err = svc.Update(context.Background(), RuntimeUpdate{
		ContainerReportIntervalSeconds: 30,
		TokenExpiration:                TokenExpiration1Day,
		Language:                       "en",
		LogLevel:                       "info",
		RemoteCommandTimeoutSeconds:    30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReconcileTraceEnabled {
		t.Fatalf("reconcile trace should keep its current value when not provided: %#v", got)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.Runtime().ReconcileTraceEnabled {
		t.Fatalf("reconcile trace was not persisted: %#v", reloaded.Runtime())
	}
	if !reconciletrace.Enabled() {
		t.Fatal("reconcile trace switch was not restored after reload")
	}
}

func TestRuntimeSettingsSetJWTSecretPersists(t *testing.T) {
	svc := newTestService(t)
	got, err := svc.SetJWTSecret(context.Background(), "a-new-long-jwt-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.JWTSecret != "a-new-long-jwt-secret" || !got.JWTSecretConfigured {
		t.Fatalf("unexpected jwt state: %#v", got)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.JWTSecret() != "a-new-long-jwt-secret" {
		t.Fatalf("jwt secret was not persisted: %q", reloaded.JWTSecret())
	}
}

func TestRuntimeSettingsFirstStartRandomizesDefaultJWTSecret(t *testing.T) {
	svc := newTestService(t)
	if svc.JWTSecret() == DefaultJWTSecret {
		t.Fatal("default jwt secret should be randomized on first startup")
	}
	if !svc.Runtime().JWTSecretConfigured {
		t.Fatal("randomized jwt secret should be marked as configured")
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.JWTSecret() != svc.JWTSecret() {
		t.Fatalf("randomized jwt secret should persist across reloads: %q != %q", reloaded.JWTSecret(), svc.JWTSecret())
	}
}

func TestRuntimeSettingsRotatesLegacyDefaultJWTSecret(t *testing.T) {
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
	// 模拟旧版本安装：默认密钥已被旧代码固化进 runtime_settings。
	if _, err := store.AppDB().Exec(`INSERT INTO runtime_settings(key, value, updated_at) VALUES('jwtSecret', ?, 'now')`, DefaultJWTSecret); err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if svc.JWTSecret() == DefaultJWTSecret {
		t.Fatal("legacy default jwt secret should be rotated on startup")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// 再次启动不应再次轮换（幂等），且与首次轮换值一致。
	store2, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	svc2, err := NewService(store2.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if svc2.JWTSecret() != svc.JWTSecret() {
		t.Fatalf("rotated jwt secret should be stable across restarts: %q != %q", svc2.JWTSecret(), svc.JWTSecret())
	}
}

func TestRuntimeSettingsPreservesExplicitConfigJWTSecret(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.JWTSecret = "explicit-configured-secret-value-123"
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := svc.JWTSecret(); got != "explicit-configured-secret-value-123" {
		t.Fatalf("explicit jwt secret not preserved: %q", got)
	}
}
func TestRuntimeSettingsRejectInvalidLanguage(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		TokenExpiration: DefaultTokenExpiration,
		Language:        "fr",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectInvalidLogLevel(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		TokenExpiration: DefaultTokenExpiration,
		Language:        "en",
		LogLevel:        "verbose",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectInvalidTokenExpiration(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		TokenExpiration: "2h",
		Language:        "en",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeSettingsRejectLongBranding(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Update(context.Background(), RuntimeUpdate{
		TokenExpiration: DefaultTokenExpiration,
		Language:        "en",
		Branding:        &RuntimeBrandingSettings{LoginTitle: strings.Repeat("a", 81)},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func validAgentRuntimeUpdate(agent *RuntimeAgentSettings) RuntimeUpdate {
	return RuntimeUpdate{
		TokenExpiration: DefaultTokenExpiration,
		Language:        "en",
		Agent:           agent,
	}
}

func TestRuntimeSettingsAgentDeliveryDefaultsToDisabled(t *testing.T) {
	svc := newTestService(t)
	agent := svc.Runtime().Agent
	// An empty base URL is what keeps the SSH upload the only delivery path, so
	// a Panel that never opts in behaves exactly as before.
	if agent.DownloadBaseURL != "" {
		t.Fatalf("expected HTTP delivery to be disabled by default, got %q", agent.DownloadBaseURL)
	}
	if agent.DownloadVerifyTLS {
		t.Fatal("expected certificate verification to default to off")
	}
	if agent.TransferTimeoutSeconds != DefaultAgentTransferTimeoutSeconds {
		t.Fatalf("expected the default transfer timeout, got %d", agent.TransferTimeoutSeconds)
	}
}

func TestRuntimeSettingsAgentDeliveryPersistsAndNormalizes(t *testing.T) {
	svc := newTestService(t)
	updated, err := svc.Update(context.Background(), validAgentRuntimeUpdate(&RuntimeAgentSettings{
		DownloadBaseURL:        "  https://panel.example.test/  ",
		DownloadVerifyTLS:      true,
		TransferTimeoutSeconds: 300,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Agent.DownloadBaseURL != "https://panel.example.test" {
		t.Fatalf("expected a normalized origin, got %q", updated.Agent.DownloadBaseURL)
	}
	if !updated.Agent.DownloadVerifyTLS {
		t.Fatal("expected certificate verification to be kept")
	}
	if updated.Agent.TransferTimeoutSeconds != 300 {
		t.Fatalf("expected the configured timeout, got %d", updated.Agent.TransferTimeoutSeconds)
	}
	if got := svc.Runtime().Agent; got != updated.Agent {
		t.Fatalf("expected the in-memory settings to match, got %#v", got)
	}

	// Leaving the timeout unset keeps the stored value instead of clamping it to
	// zero, which would be a validation error.
	kept, err := svc.Update(context.Background(), validAgentRuntimeUpdate(&RuntimeAgentSettings{
		DownloadBaseURL: "https://panel.example.test",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if kept.Agent.TransferTimeoutSeconds != 300 {
		t.Fatalf("expected the previous timeout to be kept, got %d", kept.Agent.TransferTimeoutSeconds)
	}
}

func TestRuntimeSettingsRejectInvalidAgentDownloadBaseURL(t *testing.T) {
	svc := newTestService(t)
	// Quoting and shell metacharacters are rejected outright because the value
	// is embedded in a remote shell command, and a path would change which
	// artifact the Panel serves.
	for _, baseURL := range []string{
		"panel.example.test",
		"ftp://panel.example.test",
		"https://panel.example.test/prefix",
		"https://panel.example.test?a=b",
		"https://panel.example.test#fragment",
		"https://user:secret@panel.example.test",
		"https://panel.example.test; rm -rf /",
		"https://panel.example.test/$(id)",
		"https://panel.example.test/`id`",
		"https://panel.example.test/\"x\"",
		"https://panel.example.test/ x",
	} {
		if _, err := svc.Update(context.Background(), validAgentRuntimeUpdate(&RuntimeAgentSettings{DownloadBaseURL: baseURL})); err == nil {
			t.Fatalf("expected base URL %q to be rejected", baseURL)
		}
	}
}

func TestRuntimeSettingsRejectOutOfRangeAgentTransferTimeout(t *testing.T) {
	svc := newTestService(t)
	for _, seconds := range []int{1, MinAgentTransferTimeoutSeconds - 1, MaxAgentTransferTimeoutSeconds + 1} {
		agent := &RuntimeAgentSettings{TransferTimeoutSeconds: seconds}
		if _, err := svc.Update(context.Background(), validAgentRuntimeUpdate(agent)); err == nil {
			t.Fatalf("expected transfer timeout %d to be rejected", seconds)
		}
	}
	for _, seconds := range []int{MinAgentTransferTimeoutSeconds, MaxAgentTransferTimeoutSeconds} {
		agent := &RuntimeAgentSettings{TransferTimeoutSeconds: seconds}
		if _, err := svc.Update(context.Background(), validAgentRuntimeUpdate(agent)); err != nil {
			t.Fatalf("expected transfer timeout %d to be accepted: %v", seconds, err)
		}
	}
}

// newSeededRuntimeService 构造一个各分区均为非默认值的设置服务，供分区写入测试
// 断言“只改本分区、其余保持”。基线同时覆盖常规、安全、证书、Agent、品牌与
// Tailscale，避免零值让断言失去区分度。
func newSeededRuntimeService(t *testing.T) (*Service, RuntimeSettings) {
	t.Helper()
	svc := newTestService(t)
	reconcileTraceEnabled := true
	if _, err := svc.Update(context.Background(), RuntimeUpdate{
		ContainerReportIntervalSeconds: 7,
		TokenExpiration:                TokenExpiration5Days,
		Language:                       "en",
		LogLevel:                       "info",
		RemoteCommandTimeoutSeconds:    45,
		ReconcileTraceEnabled:          &reconcileTraceEnabled,
		Branding:                       &RuntimeBrandingSettings{LoginTitle: "Operations", LoginSubtitle: "Manage infrastructure"},
		Certificates:                   &RuntimeCertificateSettings{Email: "admin@example.test", DNSPropagationDelaySeconds: 15},
		Agent:                          &RuntimeAgentSettings{DownloadBaseURL: "https://agent.example.test", DownloadVerifyTLS: true, TransferTimeoutSeconds: 600},
		Tailscale:                      &RuntimeTailscaleUpdate{AuthKey: "tskey-auth-baseline", Tags: []string{"tag:web"}},
	}); err != nil {
		t.Fatal(err)
	}
	return svc, svc.Runtime()
}

// TestRuntimePartitionUpdatesOnlyTouchTheirSection 验证每个分区 Service 方法在
// 完整候选快照上只替换本分区字段，并返回与持久化一致的完整 RuntimeSettings。
func TestRuntimePartitionUpdatesOnlyTouchTheirSection(t *testing.T) {
	cases := []struct {
		name   string
		apply  func(context.Context, *Service) (RuntimeSettings, error)
		expect func(RuntimeSettings) RuntimeSettings
	}{
		{
			name: "general",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				reconcileTraceEnabled := false
				return svc.UpdateGeneral(ctx, RuntimeGeneralUpdate{
					ContainerReportIntervalSeconds: 9,
					Language:                       "zh-CN",
					LogLevel:                       "debug",
					ReconcileTraceEnabled:          &reconcileTraceEnabled,
				})
			},
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.ContainerReportIntervalSeconds = 9
				base.Language = "zh-CN"
				base.LogLevel = "debug"
				base.ReconcileTraceEnabled = false
				return base
			},
		},
		{
			name: "security",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateSecurity(ctx, RuntimeSecurityUpdate{
					TokenExpiration:             TokenExpiration30Days,
					RemoteCommandTimeoutSeconds: 90,
				})
			},
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.TokenExpiration = TokenExpiration30Days
				base.RemoteCommandTimeoutSeconds = 90
				return base
			},
		},
		{
			name: "certificates",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateCertificates(ctx, RuntimeCertificatesUpdate{
					Panel:        &RuntimePanelSettings{Domain: "panel2.example.test"},
					Certificates: &RuntimeCertificateSettings{Email: "ops@example.test", DNSPropagationDelaySeconds: 60},
				})
			},
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Panel = RuntimePanelSettings{Domain: "panel2.example.test"}
				base.Certificates = RuntimeCertificateSettings{Email: "ops@example.test", DNSPropagationDelaySeconds: 60}
				return base
			},
		},
		{
			name: "agent",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateAgent(ctx, RuntimeAgentSettings{
					DownloadBaseURL:        "https://cdn.example.test",
					DownloadVerifyTLS:      false,
					TransferTimeoutSeconds: 120,
				})
			},
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Agent = RuntimeAgentSettings{DownloadBaseURL: "https://cdn.example.test", DownloadVerifyTLS: false, TransferTimeoutSeconds: 120}
				return base
			},
		},
		{
			name: "branding",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateBranding(ctx, RuntimeBrandingSettings{LoginTitle: "Seamark Ops", LoginSubtitle: "Infrastructure control"})
			},
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Branding = RuntimeBrandingSettings{LoginTitle: "Seamark Ops", LoginSubtitle: "Infrastructure control"}
				return base
			},
		},
		{
			name: "tailscale",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateTailscale(ctx, RuntimeTailscaleUpdate{Tags: []string{"tag:db"}})
			},
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Tailscale.Tags = []string{"tag:db"}
				return base
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			got, err := tc.apply(context.Background(), svc)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.expect(baseline)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("update returned %#v, want %#v", got, want)
			}
			if !reflect.DeepEqual(svc.Runtime(), want) {
				t.Fatalf("in-memory settings = %#v, want %#v", svc.Runtime(), want)
			}
			// 分区写入返回值必须仍是完整 RuntimeSettings，而不是只含本分区。
			if got.TokenExpiration == "" || got.Panel.Domain == "" || got.Agent.TransferTimeoutSeconds < MinAgentTransferTimeoutSeconds || !got.JWTSecretConfigured {
				t.Fatalf("partition update returned incomplete settings: %#v", got)
			}
			reloaded, err := NewService(svc.db, svc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reloaded.Runtime(), want) {
				t.Fatalf("persisted settings = %#v, want %#v", reloaded.Runtime(), want)
			}
		})
	}
}

// TestRuntimePartitionUpdateKeepsOmittedFields 验证分区负载内省略的字段保持现值：
// 自动保存可能只改动一个控件，不得把同分区的其它字段清零。
func TestRuntimePartitionUpdateKeepsOmittedFields(t *testing.T) {
	ctx := context.Background()
	svc, baseline := newSeededRuntimeService(t)

	got, err := svc.UpdateGeneral(ctx, RuntimeGeneralUpdate{LogLevel: "warn"})
	if err != nil {
		t.Fatal(err)
	}
	if got.LogLevel != "warn" {
		t.Fatalf("log level = %q", got.LogLevel)
	}
	if got.ContainerReportIntervalSeconds != baseline.ContainerReportIntervalSeconds || got.Language != baseline.Language || got.ReconcileTraceEnabled != baseline.ReconcileTraceEnabled {
		t.Fatalf("omitted general fields changed: %#v", got)
	}

	got, err = svc.UpdateAgent(ctx, RuntimeAgentSettings{DownloadBaseURL: "https://cdn.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent.TransferTimeoutSeconds != baseline.Agent.TransferTimeoutSeconds {
		t.Fatalf("omitted agent transfer timeout changed: %#v", got.Agent)
	}

	got, err = svc.UpdateCertificates(ctx, RuntimeCertificatesUpdate{
		Certificates: &RuntimeCertificateSettings{Email: "ops@example.test", DNSPropagationDelaySeconds: 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Panel != baseline.Panel {
		t.Fatalf("omitted panel group changed: %#v", got.Panel)
	}
	if got.Certificates.Email != "ops@example.test" {
		t.Fatalf("certificate settings were not applied: %#v", got.Certificates)
	}
}

// TestRuntimePartitionUpdateValidationErrorsAreStable 验证分区方法沿用既有稳定
// 错误码与 422 状态，且失败不改变内存快照或持久化值（SET-RUN-005）。
func TestRuntimePartitionUpdateValidationErrorsAreStable(t *testing.T) {
	cases := []struct {
		name  string
		apply func(context.Context, *Service) (RuntimeSettings, error)
		code  string
	}{
		{
			name: "general",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateGeneral(ctx, RuntimeGeneralUpdate{Language: "fr"})
			},
			code: "invalid_language",
		},
		{
			name: "security",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateSecurity(ctx, RuntimeSecurityUpdate{TokenExpiration: "2h"})
			},
			code: "invalid_token_expiration",
		},
		{
			name: "certificates",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateCertificates(ctx, RuntimeCertificatesUpdate{Panel: &RuntimePanelSettings{Domain: "bad domain"}})
			},
			code: "invalid_panel_domain",
		},
		{
			name: "agent",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateAgent(ctx, RuntimeAgentSettings{DownloadBaseURL: "ftp://agent.example.test"})
			},
			code: "invalid_agent_download_base_url",
		},
		{
			name: "branding",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateBranding(ctx, RuntimeBrandingSettings{LoginTitle: strings.Repeat("a", 81)})
			},
			code: "invalid_branding_login_title",
		},
		{
			name: "tailscale",
			apply: func(ctx context.Context, svc *Service) (RuntimeSettings, error) {
				return svc.UpdateTailscale(ctx, RuntimeTailscaleUpdate{Tags: []string{"web"}})
			},
			code: "invalid_tailscale_tag",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			_, err := tc.apply(context.Background(), svc)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			var domainErr *panelerr.Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("error is not a domain error: %v", err)
			}
			if domainErr.Code != tc.code || domainErr.HTTPStatus != http.StatusUnprocessableEntity {
				t.Fatalf("error = %#v, want code %q with 422", domainErr, tc.code)
			}
			if !reflect.DeepEqual(svc.Runtime(), baseline) {
				t.Fatalf("rejected update changed the runtime snapshot: %#v, want %#v", svc.Runtime(), baseline)
			}
			reloaded, err := NewService(svc.db, svc.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(reloaded.Runtime(), baseline) {
				t.Fatalf("rejected update changed persistent settings: %#v, want %#v", reloaded.Runtime(), baseline)
			}
		})
	}
}
