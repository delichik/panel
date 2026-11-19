package facilityapps

import (
	"context"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/applications"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
)

// p1Digest 构造一个形状合法的 sha256 digest（回滚显式 digest 校验要求 64 位十六进制）。
func p1Digest(seed byte) string {
	return "sha256:" + strings.Repeat(string([]byte{seed}), 64)
}

type facilityTestImageResolver struct {
	digest string
	err    error
}

func (f facilityTestImageResolver) Resolve(context.Context, string) (applications.ImageDigestResult, error) {
	if f.err != nil {
		return applications.ImageDigestResult{}, f.err
	}
	return applications.ImageDigestResult{Reference: supportedProxyImage, Digest: f.digest}, nil
}

func seedP1ProxyGateway(t *testing.T, svc *Service) {
	t.Helper()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()
	now := time.Now().UTC()
	if err := orm.Insert(ctx, svc.db, &models.Credential{ID: "cred-p1", Name: "cred-p1", Type: "password", Username: "root", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := orm.Insert(ctx, svc.db, &models.Server{ID: "srv-p1", Name: "srv-p1", Host: "127.0.0.1", Port: 22, CredentialID: "cred-p1", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if _, err := svc.SaveProxyPlane(ctx, ProxyPlaneSaveInput{DeploymentServers: []string{"srv-p1"}}); err != nil {
		t.Fatalf("save proxy plane: %v", err)
	}
}

func TestFacilityProxySettingsP1DefaultsAndValidation(t *testing.T) {
	defaults := defaultFacilityProxySettings()
	if defaults.ServerTokens != ServerTokensOff || defaults.ClientMaxBodySize != "50m" || defaults.TLSMinVersion != TLSMinVersion12 {
		t.Fatalf("P1 defaults = %#v", defaults)
	}
	// 零值设置（旧配置/测试直接构造）必须解析为默认值。
	resolved := FacilityProxySettings{}.resolved()
	if resolved.ServerTokens != ServerTokensOff || resolved.ClientMaxBodySize != "50m" || resolved.TLSMinVersion != TLSMinVersion12 {
		t.Fatalf("zero-value settings resolved = %#v", resolved)
	}
	parsed := parseFacilityProxySettings(`{"serverTokens":"ON","clientMaxBodySize":"128M","tlsMinVersion":"1.3"}`)
	if parsed.ServerTokens != ServerTokensOn || parsed.ClientMaxBodySize != "128m" || parsed.TLSMinVersion != TLSMinVersion13 {
		t.Fatalf("parsed P1 settings = %#v", parsed)
	}
	// 旧库/手工写入的非法值回退默认，避免渲染出非法 nginx 配置。
	invalid := parseFacilityProxySettings(`{"serverTokens":"bogus","clientMaxBodySize":"50 mb","tlsMinVersion":"2.0"}`)
	if invalid.ServerTokens != ServerTokensOff || invalid.ClientMaxBodySize != "50m" || invalid.TLSMinVersion != TLSMinVersion12 {
		t.Fatalf("invalid stored settings must fall back to defaults: %#v", invalid)
	}
	cases := []struct {
		settings FacilityProxySettings
		code     string
	}{
		{FacilityProxySettings{ServerTokens: "bogus"}, "facility_server_tokens_invalid"},
		{FacilityProxySettings{ClientMaxBodySize: "0"}, "facility_client_max_body_size_invalid"},
		{FacilityProxySettings{ClientMaxBodySize: "50mb"}, "facility_client_max_body_size_invalid"},
		{FacilityProxySettings{ClientMaxBodySize: "-1m"}, "facility_client_max_body_size_invalid"},
		{FacilityProxySettings{TLSMinVersion: "1.1"}, "facility_tls_min_version_invalid"},
	}
	for _, tc := range cases {
		if _, err := normalizeFacilityProxySettings(tc.settings); err == nil {
			t.Fatalf("settings %#v must be rejected", tc.settings)
		} else {
			assertFacilityPanelError(t, err, tc.code)
		}
	}
	// 合法 size 归一化为小写并接受常见单位。
	for _, size := range []string{"1m", "512K", "2g", "1024"} {
		normalized, err := normalizeFacilityProxySettings(FacilityProxySettings{ClientMaxBodySize: size})
		if err != nil {
			t.Fatalf("size %q must be accepted: %v", size, err)
		}
		if normalized.ClientMaxBodySize != strings.ToLower(size) {
			t.Fatalf("size %q normalized to %q", size, normalized.ClientMaxBodySize)
		}
	}
}

func TestRenderMainNginxConfigP1Settings(t *testing.T) {
	defaults := renderMainNginxConfig(FacilityProxySettings{})
	for _, want := range []string{"server_tokens off;", "client_max_body_size 50m;", "ssl_protocols TLSv1.2 TLSv1.3;"} {
		if !strings.Contains(defaults, want) {
			t.Fatalf("default main config missing %q:\n%s", want, defaults)
		}
	}
	custom := renderMainNginxConfig(FacilityProxySettings{
		ServerTokens:      ServerTokensOn,
		ClientMaxBodySize: "64m",
		TLSMinVersion:     TLSMinVersion13,
	})
	for _, want := range []string{"server_tokens on;", "client_max_body_size 64m;", "ssl_protocols TLSv1.3;"} {
		if !strings.Contains(custom, want) {
			t.Fatalf("custom main config missing %q:\n%s", want, custom)
		}
	}
	if strings.Contains(custom, "TLSv1.2") {
		t.Fatalf("tls 1.3 minimum must not render TLSv1.2:\n%s", custom)
	}
	// 自定义 http 指令同名覆盖 P1 参数（最高优先级）。
	overridden := renderMainNginxConfig(FacilityProxySettings{
		ClientMaxBodySize:    "64m",
		CustomHTTPDirectives: []string{"client_max_body_size 8m;", "server_tokens on;"},
	})
	if !strings.Contains(overridden, "client_max_body_size 8m;") || !strings.Contains(overridden, "server_tokens on;") {
		t.Fatalf("custom directives must override P1 settings:\n%s", overridden)
	}
	if strings.Contains(overridden, "client_max_body_size 64m;") {
		t.Fatalf("panel-generated size must be replaced by custom directive:\n%s", overridden)
	}
}

func TestSaveProxySettingsPersistsP1Fields(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	svc.reconciler = fakeFacilityReconcileTrigger{}
	ctx := context.Background()

	config, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{
		HTTPPortMode:      HTTPPortModeServe,
		ServerTokens:      ServerTokensOn,
		ClientMaxBodySize: "8m",
		TLSMinVersion:     TLSMinVersion13,
	})
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	if config.Settings.ServerTokens != ServerTokensOn || config.Settings.ClientMaxBodySize != "8m" || config.Settings.TLSMinVersion != TLSMinVersion13 {
		t.Fatalf("saved P1 settings = %#v", config.Settings)
	}
	// GET 投影返回同一份 P1 设置。
	projected, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatalf("get reverse proxy: %v", err)
	}
	if projected.Settings.ServerTokens != ServerTokensOn || projected.Settings.ClientMaxBodySize != "8m" || projected.Settings.TLSMinVersion != TLSMinVersion13 {
		t.Fatalf("projected P1 settings = %#v", projected.Settings)
	}
	// 只提交 P0 字段（P1 空白）时保留现值。
	preserved, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: config.Version, HTTPPortMode: HTTPPortModeRedirect})
	if err != nil {
		t.Fatalf("partial save: %v", err)
	}
	if preserved.Settings.ServerTokens != ServerTokensOn || preserved.Settings.ClientMaxBodySize != "8m" || preserved.Settings.TLSMinVersion != TLSMinVersion13 {
		t.Fatalf("partial save must preserve P1 settings: %#v", preserved.Settings)
	}
	// 非法值返回稳定错误码且不落库。
	if _, err := svc.SaveProxySettings(ctx, ProxySettingsSaveInput{Version: preserved.Version, HTTPPortMode: HTTPPortModeServe, TLSMinVersion: "1.1"}); err == nil {
		t.Fatal("invalid tls minimum must be rejected")
	} else {
		assertFacilityPanelError(t, err, "facility_tls_min_version_invalid")
	}
	after, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Settings.TLSMinVersion != TLSMinVersion13 {
		t.Fatalf("rejected save must not persist: %#v", after.Settings)
	}
}

func TestProxyImageAutoUpdateBackoffHelpers(t *testing.T) {
	target := p1Digest('a')
	other := p1Digest('b')
	var state models.FacilityProxyImageState
	if reason := proxyImageAutoUpdatePauseReason(state, target); reason != "" {
		t.Fatalf("fresh state must not be paused: %q", reason)
	}
	for index := 0; index < proxyImageAutoUpdateFailureLimit-1; index++ {
		recordProxyImageAutoUpdateFailure(&state, target)
	}
	if state.AutoUpdateFailures != proxyImageAutoUpdateFailureLimit-1 || !sameDigest(state.FailedDigest, target) {
		t.Fatalf("failure state = %#v", state)
	}
	if reason := proxyImageAutoUpdatePauseReason(state, target); reason != "" {
		t.Fatalf("below threshold must not pause: %q", reason)
	}
	recordProxyImageAutoUpdateFailure(&state, target)
	if state.AutoUpdateFailures != proxyImageAutoUpdateFailureLimit {
		t.Fatalf("failure count = %d, want %d", state.AutoUpdateFailures, proxyImageAutoUpdateFailureLimit)
	}
	reason := proxyImageAutoUpdatePauseReason(state, target)
	if reason == "" || !strings.Contains(reason, shortDigest(target)) {
		t.Fatalf("threshold must pause with digest reason: %q", reason)
	}
	// 目标 digest 变化后旧退避不再适用。
	if reason := proxyImageAutoUpdatePauseReason(state, other); reason != "" {
		t.Fatalf("new target digest must clear backoff: %q", reason)
	}
	// 同一 digest 连续失败继续累加；换 digest 从 1 重新计数。
	state = models.FacilityProxyImageState{FailedDigest: target, AutoUpdateFailures: 2}
	recordProxyImageAutoUpdateFailure(&state, target)
	if state.AutoUpdateFailures != 3 {
		t.Fatalf("same digest failures = %d, want 3", state.AutoUpdateFailures)
	}
	recordProxyImageAutoUpdateFailure(&state, other)
	if state.AutoUpdateFailures != 1 || !sameDigest(state.FailedDigest, other) {
		t.Fatalf("new digest must reset counter: %#v", state)
	}
	resetProxyImageFailureState(&state)
	if state.AutoUpdateFailures != 0 || state.FailedDigest != "" {
		t.Fatalf("reset state = %#v", state)
	}
}

func TestProxyImageStatusProjectsPinnedAndBackoff(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	seedP1ProxyGateway(t, svc)
	ctx := context.Background()

	failedDigest := p1Digest('c')
	state, err := svc.loadProxyImageState(ctx, "srv-p1")
	if err != nil {
		t.Fatal(err)
	}
	state.Status = ProxyImageStatusFailed
	state.Pinned = true
	state.TargetDigest = failedDigest
	state.ObservedDigest = failedDigest
	state.AutoUpdateFailures = proxyImageAutoUpdateFailureLimit
	state.FailedDigest = failedDigest
	if err := svc.saveProxyImageState(ctx, state); err != nil {
		t.Fatalf("save image state: %v", err)
	}
	status, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatalf("get reverse proxy: %v", err)
	}
	if !status.Image.Pinned || !status.Image.AutoUpdatePaused {
		t.Fatalf("aggregate image status = %#v", status.Image)
	}
	if len(status.Image.Servers) != 1 {
		t.Fatalf("servers = %#v", status.Image.Servers)
	}
	item := status.Image.Servers[0]
	if !item.Pinned || !item.AutoUpdatePaused || !strings.Contains(item.AutoUpdateSkipReason, shortDigest(failedDigest)) {
		t.Fatalf("per-gateway image state = %#v", item)
	}
	if item.AutoUpdateFailures != proxyImageAutoUpdateFailureLimit {
		t.Fatalf("autoUpdateFailures = %d", item.AutoUpdateFailures)
	}
	if !sameDigest(item.FailedDigest, failedDigest) {
		t.Fatalf("failedDigest = %q, want %q", item.FailedDigest, failedDigest)
	}
	// 恢复自动更新后 pinned 与退避清零，并重新投影 latest digest。
	resolverLatest := p1Digest('d')
	svc.imageResolver = facilityTestImageResolver{digest: resolverLatest}
	resumed, err := svc.ResumeReverseProxyImage(ctx, ProxyImageResumeInput{})
	if err != nil {
		t.Fatalf("resume auto update: %v", err)
	}
	if resumed.LatestDigest != resolverLatest {
		t.Fatalf("resumed latest digest = %q", resumed.LatestDigest)
	}
	reloaded, err := svc.loadProxyImageState(ctx, "srv-p1")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Pinned || reloaded.AutoUpdateFailures != 0 || reloaded.FailedDigest != "" {
		t.Fatalf("resume must clear pinned/backoff: %#v", reloaded)
	}
	if reloaded.Reference != supportedProxyImage {
		t.Fatalf("resume reference = %q, want managed tag", reloaded.Reference)
	}
	if reloaded.Status != ProxyImageStatusUpdateAvailable {
		t.Fatalf("resume status = %q, want update_available", reloaded.Status)
	}
}

func TestRollbackReverseProxyImageValidationAndIdempotency(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	seedP1ProxyGateway(t, svc)
	ctx := context.Background()

	if _, err := svc.RollbackReverseProxyImage(ctx, ProxyImageRollbackInput{}); err == nil {
		t.Fatal("rollback without confirm must be rejected")
	} else {
		assertFacilityPanelError(t, err, "facility_proxy_image_rollback_confirmation_required")
	}
	if _, err := svc.RollbackReverseProxyImage(ctx, ProxyImageRollbackInput{Confirm: true, Digest: "sha256:short"}); err == nil {
		t.Fatal("malformed digest must be rejected")
	} else {
		assertFacilityPanelError(t, err, "facility_proxy_image_digest_invalid")
	}
	if _, err := svc.RollbackReverseProxyImage(ctx, ProxyImageRollbackInput{Confirm: true, ServerIDs: []string{"not-a-gateway"}}); err == nil {
		t.Fatal("non-gateway server must be rejected")
	} else {
		assertFacilityPanelError(t, err, "facility_proxy_image_server_invalid")
	}

	// 无 previous_digest 且未显式指定 digest：幂等空操作，不创建任务。
	action, err := svc.RollbackReverseProxyImage(ctx, ProxyImageRollbackInput{Confirm: true})
	if err != nil {
		t.Fatalf("rollback without target: %v", err)
	}
	if !action.Skipped || action.TaskID != "" {
		t.Fatalf("rollback without target action = %#v", action)
	}

	// 有回滚目标但任务服务不可用：明确报不可用而不是静默成功。
	target := p1Digest('e')
	state, err := svc.loadProxyImageState(ctx, "srv-p1")
	if err != nil {
		t.Fatal(err)
	}
	state.PreviousDigest = target
	if err := svc.saveProxyImageState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RollbackReverseProxyImage(ctx, ProxyImageRollbackInput{Confirm: true}); err == nil {
		t.Fatal("rollback with target but no task service must fail")
	} else {
		assertFacilityPanelError(t, err, "facility_proxy_image_unavailable")
	}

	// 已固定成功（pinned）的节点对回滚请求是幂等空操作。
	state.Pinned = true
	state.Status = ProxyImageStatusPinned
	if err := svc.saveProxyImageState(ctx, state); err != nil {
		t.Fatal(err)
	}
	action, err = svc.RollbackReverseProxyImage(ctx, ProxyImageRollbackInput{Confirm: true})
	if err != nil {
		t.Fatalf("pinned idempotent rollback: %v", err)
	}
	if !action.Skipped || !action.Pinned {
		t.Fatalf("pinned rollback action = %#v", action)
	}

	// 失败状态允许重试：plan 仍解析出回滚目标。
	state.Status = ProxyImageStatusFailed
	if err := svc.saveProxyImageState(ctx, state); err != nil {
		t.Fatal(err)
	}
	cfg, err := svc.loadConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	targets, err := svc.planProxyImageRollback(ctx, cfg.DeploymentServers, nil, "")
	if err != nil {
		t.Fatalf("plan rollback: %v", err)
	}
	if len(targets) != 1 || targets[0].ServerID != "srv-p1" || !sameDigest(targets[0].Digest, target) {
		t.Fatalf("rollback plan = %#v", targets)
	}
	// 显式 digest 优先于 previous_digest。
	other := p1Digest('f')
	targets, err = svc.planProxyImageRollback(ctx, cfg.DeploymentServers, nil, other)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || !sameDigest(targets[0].Digest, other) {
		t.Fatalf("explicit digest rollback plan = %#v", targets)
	}
}

func TestProxyImageDigestReference(t *testing.T) {
	for _, tc := range []struct {
		reference string
		digest    string
		want      string
	}{
		{supportedProxyImage, p1Digest('a'), "nginx@" + p1Digest('a')},
		{"registry.example.com:5000/team/nginx:1.28-alpine", p1Digest('b'), "registry.example.com:5000/team/nginx@" + p1Digest('b')},
		{"nginx@" + p1Digest('c'), p1Digest('d'), "nginx@" + p1Digest('d')},
	} {
		if got := proxyImageDigestReference(tc.reference, tc.digest); got != tc.want {
			t.Fatalf("reference %q -> %q, want %q", tc.reference, got, tc.want)
		}
	}
}
