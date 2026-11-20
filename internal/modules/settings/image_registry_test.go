package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	panelerr "panel/internal/platform/errors"
)

// TestRuntimeImageRegistryDefaultsAndSeeds 验证新库默认值：官方仓库（endpoint
// 为空）与 https 协议，且默认值已播种到 runtime_settings。
func TestRuntimeImageRegistryDefaultsAndSeeds(t *testing.T) {
	svc := newTestService(t)
	got := svc.Runtime().ImageRegistry
	want := RuntimeImageRegistrySettings{Endpoint: "", Scheme: ImageRegistrySchemeHTTPS}
	if got != want {
		t.Fatalf("default image registry settings = %#v, want %#v", got, want)
	}
	for key, value := range map[string]string{
		RuntimeSettingImageRegistryEndpoint: "",
		RuntimeSettingImageRegistryScheme:   ImageRegistrySchemeHTTPS,
	} {
		var stored string
		if err := svc.db.QueryRow(`SELECT value FROM runtime_settings WHERE key=?`, key).Scan(&stored); err != nil {
			t.Fatalf("runtime_settings[%s] not seeded: %v", key, err)
		}
		if stored != value {
			t.Fatalf("runtime_settings[%s] = %q, want %q", key, stored, value)
		}
	}
}

// TestRuntimeImageRegistryPersistsExplicitHTTP 验证显式 http、endpoint 的 trim/
// 小写归一，以及跨重启持久化。
func TestRuntimeImageRegistryPersistsExplicitHTTP(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	got, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{
		Endpoint: stringPtr("  Harbor.Example.Test:5000  "),
		Scheme:   stringPtr("HTTP"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := RuntimeImageRegistrySettings{Endpoint: "harbor.example.test:5000", Scheme: ImageRegistrySchemeHTTP}
	if got.ImageRegistry != want {
		t.Fatalf("image registry settings = %#v, want %#v", got.ImageRegistry, want)
	}
	if svc.Runtime().ImageRegistry != want {
		t.Fatalf("in-memory image registry settings = %#v, want %#v", svc.Runtime().ImageRegistry, want)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Runtime().ImageRegistry != want {
		t.Fatalf("persisted image registry settings = %#v, want %#v", reloaded.Runtime().ImageRegistry, want)
	}
}

// TestRuntimeImageRegistryAcceptsValidEndpoints 锁定裸 host[:port] 的合法形态，
// 包括官方仓库空串、IPv4、单标签主机名与方括号 IPv6。
func TestRuntimeImageRegistryAcceptsValidEndpoints(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"registry.example.test":     "registry.example.test",
		"registry.example.test:443": "registry.example.test:443",
		"10.0.0.1:5000":             "10.0.0.1:5000",
		"localhost":                 "localhost",
		"[::1]:5000":                "[::1]:5000",
	}
	for input, want := range cases {
		ctx := context.Background()
		svc := newTestService(t)
		got, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{Endpoint: stringPtr(input)})
		if err != nil {
			t.Fatalf("endpoint %q was rejected: %v", input, err)
		}
		if got.ImageRegistry.Endpoint != want {
			t.Fatalf("endpoint %q normalized to %q, want %q", input, got.ImageRegistry.Endpoint, want)
		}
	}
}

// TestRuntimeImageRegistryOmittedFieldsKeepCurrent 验证分区负载的「省略 = 保留」：
// 只改 endpoint 时 scheme 不动，只改 scheme 时 endpoint 不动；scheme 显式空串
// 视为省略。
func TestRuntimeImageRegistryOmittedFieldsKeepCurrent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{
		Endpoint: stringPtr("registry.example.test:5000"),
		Scheme:   stringPtr(ImageRegistrySchemeHTTP),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{Endpoint: stringPtr("mirror.example.test")})
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageRegistry.Endpoint != "mirror.example.test" || got.ImageRegistry.Scheme != ImageRegistrySchemeHTTP {
		t.Fatalf("omitted scheme changed: %#v", got.ImageRegistry)
	}

	// 显式空串仍视为省略，避免前端清空控件把 scheme 清零。
	got, err = svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{Scheme: stringPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageRegistry.Endpoint != "mirror.example.test" || got.ImageRegistry.Scheme != ImageRegistrySchemeHTTP {
		t.Fatalf("empty scheme must keep current values: %#v", got.ImageRegistry)
	}

	got, err = svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{Scheme: stringPtr("https")})
	if err != nil {
		t.Fatal(err)
	}
	if got.ImageRegistry.Endpoint != "mirror.example.test" || got.ImageRegistry.Scheme != ImageRegistrySchemeHTTPS {
		t.Fatalf("omitted endpoint changed: %#v", got.ImageRegistry)
	}
}

// TestRuntimeImageRegistryExplicitEmptyEndpointClearsToOfficial 验证显式空串与
// 省略的区别：显式空 endpoint 表示回到官方仓库，并真正落库。
func TestRuntimeImageRegistryExplicitEmptyEndpointClearsToOfficial(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{
		Endpoint: stringPtr("registry.example.test:5000"),
		Scheme:   stringPtr(ImageRegistrySchemeHTTP),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{Endpoint: stringPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	want := RuntimeImageRegistrySettings{Endpoint: "", Scheme: ImageRegistrySchemeHTTP}
	if got.ImageRegistry != want {
		t.Fatalf("cleared image registry settings = %#v, want %#v", got.ImageRegistry, want)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Runtime().ImageRegistry != want {
		t.Fatalf("cleared image registry settings were not persisted: %#v", reloaded.Runtime().ImageRegistry)
	}
}

// TestRuntimeImageRegistryRejectsInvalidEndpoints 验证 scheme/path/query/
// userinfo/空白/引号/反斜杠/端口越界等非法 endpoint 返回稳定 422 错误码，且
// 失败整笔不生效。
func TestRuntimeImageRegistryRejectsInvalidEndpoints(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
	}{
		{name: "with scheme", endpoint: "https://registry.example.test"},
		{name: "with path", endpoint: "registry.example.test/v2"},
		{name: "with query", endpoint: "registry.example.test?mirror=1"},
		{name: "with userinfo", endpoint: "user:secret@registry.example.test"},
		{name: "with whitespace", endpoint: "registry.example.test /v2"},
		{name: "with quotes", endpoint: `"registry.example.test"`},
		{name: "with backslash", endpoint: `registry.example.test\v2`},
		{name: "port out of range", endpoint: "registry.example.test:70000"},
		{name: "port zero", endpoint: "registry.example.test:0"},
		{name: "empty host", endpoint: ":5000"},
		{name: "bare ipv6", endpoint: "2001:db8::1"},
		{name: "invalid hostname", endpoint: "reg_istry.example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			_, err := svc.UpdateImageRegistry(context.Background(), RuntimeImageRegistryUpdate{Endpoint: stringPtr(tc.endpoint)})
			if err == nil {
				t.Fatalf("expected endpoint %q to be rejected", tc.endpoint)
			}
			var domainErr *panelerr.Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("error is not a domain error: %v", err)
			}
			if domainErr.Code != "invalid_image_registry_endpoint" || domainErr.HTTPStatus != http.StatusUnprocessableEntity {
				t.Fatalf("error = %#v, want code invalid_image_registry_endpoint with 422", domainErr)
			}
			if !reflect.DeepEqual(svc.Runtime(), baseline) {
				t.Fatalf("rejected update changed runtime snapshot: %#v, want %#v", svc.Runtime(), baseline)
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

// TestRuntimeImageRegistryRejectsInvalidScheme 验证 scheme 仅接受 http/https，
// 错误码与 422 稳定，且失败不改变快照。
func TestRuntimeImageRegistryRejectsInvalidScheme(t *testing.T) {
	for _, scheme := range []string{"ftp", "http+https", "tls"} {
		t.Run(scheme, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			_, err := svc.UpdateImageRegistry(context.Background(), RuntimeImageRegistryUpdate{Scheme: stringPtr(scheme)})
			if err == nil {
				t.Fatal("expected a validation error")
			}
			var domainErr *panelerr.Error
			if !errors.As(err, &domainErr) {
				t.Fatalf("error is not a domain error: %v", err)
			}
			if domainErr.Code != "invalid_image_registry_scheme" || domainErr.HTTPStatus != http.StatusUnprocessableEntity {
				t.Fatalf("error = %#v, want code invalid_image_registry_scheme with 422", domainErr)
			}
			if !reflect.DeepEqual(svc.Runtime(), baseline) {
				t.Fatalf("rejected update changed runtime snapshot: %#v, want %#v", svc.Runtime(), baseline)
			}
		})
	}
}

// TestRuntimeImageRegistrySurvivesOtherPartitionSaves 验证普通分区保存不会清掉
// 镜像仓库源分组（runtimeValues 全量写入时必须带上该分组的当前值）。
func TestRuntimeImageRegistrySurvivesOtherPartitionSaves(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	want := RuntimeImageRegistrySettings{
		Endpoint: "registry.example.test:5000",
		Scheme:   ImageRegistrySchemeHTTP,
	}
	if _, err := svc.UpdateImageRegistry(ctx, RuntimeImageRegistryUpdate{
		Endpoint: stringPtr(want.Endpoint),
		Scheme:   stringPtr(want.Scheme),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateGeneral(ctx, RuntimeGeneralUpdate{LogLevel: "warn"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Runtime().ImageRegistry; got != want {
		t.Fatalf("general partition save clobbered image registry settings: %#v", got)
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Runtime().ImageRegistry; got != want {
		t.Fatalf("persisted image registry settings = %#v, want %#v", got, want)
	}
}

// TestRuntimeImageRegistryOldDatabaseSeedsDefaults 模拟旧库缺少新键：启动时
// ensureDefaultRuntimeSettings 必须播种默认值。
func TestRuntimeImageRegistryOldDatabaseSeedsDefaults(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.db.Exec(`DELETE FROM runtime_settings WHERE key IN (?,?)`,
		RuntimeSettingImageRegistryEndpoint, RuntimeSettingImageRegistryScheme); err != nil {
		t.Fatal(err)
	}
	upgraded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := RuntimeImageRegistrySettings{Endpoint: "", Scheme: ImageRegistrySchemeHTTPS}
	if got := upgraded.Runtime().ImageRegistry; got != want {
		t.Fatalf("old database defaults = %#v, want %#v", got, want)
	}
	for key, value := range map[string]string{
		RuntimeSettingImageRegistryEndpoint: "",
		RuntimeSettingImageRegistryScheme:   ImageRegistrySchemeHTTPS,
	} {
		var stored string
		if err := upgraded.db.QueryRow(`SELECT value FROM runtime_settings WHERE key=?`, key).Scan(&stored); err != nil {
			t.Fatalf("runtime_settings[%s] was not seeded on upgrade: %v", key, err)
		}
		if stored != value {
			t.Fatalf("runtime_settings[%s] = %q, want %q", key, stored, value)
		}
	}
}

// TestRuntimeImageRegistryNormalizesDirtyLegacyValues 验证旧库/手工改库的非法
// 值静默回退到默认值而不是阻止启动（与自动更新分区一致）。
func TestRuntimeImageRegistryNormalizesDirtyLegacyValues(t *testing.T) {
	svc := newTestService(t)
	for key, value := range map[string]string{
		RuntimeSettingImageRegistryEndpoint: "https://bad.example.test/v2",
		RuntimeSettingImageRegistryScheme:   "ftp",
	} {
		if _, err := svc.db.Exec(`INSERT INTO runtime_settings(key,value,updated_at) VALUES(?,?,strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatalf("invalid legacy image registry values must not block startup: %v", err)
	}
	want := RuntimeImageRegistrySettings{Endpoint: "", Scheme: ImageRegistrySchemeHTTPS}
	if got := reloaded.Runtime().ImageRegistry; got != want {
		t.Fatalf("invalid legacy values were not normalized: %#v", got)
	}
}

// TestRuntimeImageRegistryPartitionHandler 验证分区端点：返回完整
// RuntimeSettings、只改 imageRegistry、并写入持久化。
func TestRuntimeImageRegistryPartitionHandler(t *testing.T) {
	svc, baseline := newSeededRuntimeService(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/runtime/image-registry",
		strings.NewReader(`{"endpoint":"registry.example.test:5000","scheme":"http"}`))
	NewHandler(svc).UpdateImageRegistry(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	envelope := decodeRuntimeEnvelope(t, rec)
	if envelope.Error != nil {
		t.Fatalf("unexpected error response: %#v", envelope.Error)
	}
	want := baseline
	want.ImageRegistry = RuntimeImageRegistrySettings{Endpoint: "registry.example.test:5000", Scheme: ImageRegistrySchemeHTTP}
	if !reflect.DeepEqual(envelope.Data, runtimeSettingsJSONRoundTrip(t, want)) {
		t.Fatalf("response settings = %#v, want %#v", envelope.Data, want)
	}
	if !reflect.DeepEqual(svc.Runtime(), want) {
		t.Fatalf("in-memory settings = %#v, want %#v", svc.Runtime(), want)
	}
	assertCompleteRuntimeSettings(t, envelope.Data)
	reloaded, err := NewService(svc.db, svc.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Runtime(), want) {
		t.Fatalf("persisted settings = %#v, want %#v", reloaded.Runtime(), want)
	}
}

// TestRuntimeImageRegistryPartitionHandlerRejectsInvalidValues 验证 handler 层
// 沿用统一的 422 错误信封与稳定错误码。
func TestRuntimeImageRegistryPartitionHandlerRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{name: "invalid endpoint", body: `{"endpoint":"https://registry.example.test"}`, code: "invalid_image_registry_endpoint"},
		{name: "invalid scheme", body: `{"scheme":"ftp"}`, code: "invalid_image_registry_scheme"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/runtime/image-registry", strings.NewReader(tc.body))
			NewHandler(svc).UpdateImageRegistry(rec, req)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
			}
			envelope := decodeRuntimeEnvelope(t, rec)
			if envelope.Error == nil || envelope.Error.Code != tc.code {
				t.Fatalf("error = %#v, want code %q", envelope.Error, tc.code)
			}
			if !reflect.DeepEqual(svc.Runtime(), baseline) {
				t.Fatalf("rejected update changed the runtime snapshot: %#v, want %#v", svc.Runtime(), baseline)
			}
		})
	}
}

// TestRuntimeImageRegistryJSONShape 锁定 GET /runtime 的 imageRegistry 分组形态：
// endpoint 与 scheme 两个字段始终存在（不是 omitempty）。
func TestRuntimeImageRegistryJSONShape(t *testing.T) {
	svc := newTestService(t)
	raw, err := json.Marshal(svc.Runtime())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	group, ok := decoded["imageRegistry"]
	if !ok {
		t.Fatalf("runtime settings JSON is missing imageRegistry: %s", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(group, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"endpoint", "scheme"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("imageRegistry JSON is missing %q: %s", key, group)
		}
	}
	var settings struct {
		ImageRegistry RuntimeImageRegistrySettings `json:"imageRegistry"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.ImageRegistry.Scheme != ImageRegistrySchemeHTTPS || settings.ImageRegistry.Endpoint != "" {
		t.Fatalf("unexpected imageRegistry defaults: %#v", settings.ImageRegistry)
	}
}
