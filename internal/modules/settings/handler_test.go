package settings

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPublicBrandingReturnsOnlyPublicFields(t *testing.T) {
	svc := newTestService(t)
	svc.mu.Lock()
	svc.rt.Branding = RuntimeBrandingSettings{
		LoginTitle:    "Operations",
		LoginSubtitle: "Manage infrastructure",
	}
	svc.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/public-branding", nil)
	NewHandler(svc).PublicBranding(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var response struct {
		Data RuntimeBrandingSettings `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Data.LoginTitle != "Operations" || response.Data.LoginSubtitle != "Manage infrastructure" {
		t.Fatalf("unexpected public branding: %#v", response.Data)
	}
}

// runtimeEnvelope 是 API 统一信封在设置测试中的最小表示。
type runtimeEnvelope struct {
	Data  RuntimeSettings `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeRuntimeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) runtimeEnvelope {
	t.Helper()
	var envelope runtimeEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	return envelope
}

// runtimeSettingsJSONRoundTrip 把服务端快照投影为 JSON 可见形态，便于和 HTTP
// 响应（只含可序列化字段）直接比较。
func runtimeSettingsJSONRoundTrip(t *testing.T, settings RuntimeSettings) RuntimeSettings {
	t.Helper()
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var projected RuntimeSettings
	if err := json.Unmarshal(raw, &projected); err != nil {
		t.Fatal(err)
	}
	return projected
}

// assertCompleteRuntimeSettings 断言分区写入的响应仍是完整的 RuntimeSettings，
// 而不是只回显本分区（SET-RUN-022）。
func assertCompleteRuntimeSettings(t *testing.T, settings RuntimeSettings) {
	t.Helper()
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"listenAddress",
		"appDatabase",
		"metricsDatabase",
		"dataRoot",
		"containerReportIntervalSeconds",
		"tokenExpiration",
		"language",
		"logLevel",
		"remoteCommandTimeoutSeconds",
		"reconcileTraceEnabled",
		"branding",
		"certificates",
		"panel",
		"agent",
		"jwtSecretConfigured",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("runtime settings response is missing %q: %s", key, raw)
		}
	}
}

// TestRuntimePartitionHandlersApplyOnlyTheirSection 验证五个分区端点各自只改变
// 本分区字段、返回完整 RuntimeSettings，且回填后的内存快照与持久化一致。
func TestRuntimePartitionHandlersApplyOnlyTheirSection(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		body    string
		handler func(*Handler, http.ResponseWriter, *http.Request)
		expect  func(RuntimeSettings) RuntimeSettings
	}{
		{
			name:    "general",
			path:    "/api/v1/settings/runtime/general",
			body:    `{"containerReportIntervalSeconds":9,"language":"zh-CN","logLevel":"debug","reconcileTraceEnabled":false}`,
			handler: (*Handler).UpdateGeneral,
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.ContainerReportIntervalSeconds = 9
				base.Language = "zh-CN"
				base.LogLevel = "debug"
				base.ReconcileTraceEnabled = false
				return base
			},
		},
		{
			name:    "security",
			path:    "/api/v1/settings/runtime/security",
			body:    `{"tokenExpiration":"30d","remoteCommandTimeoutSeconds":90}`,
			handler: (*Handler).UpdateSecurity,
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.TokenExpiration = TokenExpiration30Days
				base.RemoteCommandTimeoutSeconds = 90
				return base
			},
		},
		{
			name:    "certificates",
			path:    "/api/v1/settings/runtime/certificates",
			body:    `{"panel":{"domain":"panel2.example.test","tlsCertificateId":""},"certificates":{"email":"ops@example.test","dnsPropagationDelaySeconds":60}}`,
			handler: (*Handler).UpdateCertificates,
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Panel = RuntimePanelSettings{Domain: "panel2.example.test"}
				base.Certificates = RuntimeCertificateSettings{Email: "ops@example.test", DNSPropagationDelaySeconds: 60}
				return base
			},
		},
		{
			name:    "agent",
			path:    "/api/v1/settings/runtime/agent",
			body:    `{"downloadBaseUrl":"https://cdn.example.test","downloadVerifyTls":false,"transferTimeoutSeconds":120}`,
			handler: (*Handler).UpdateAgent,
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Agent = RuntimeAgentSettings{DownloadBaseURL: "https://cdn.example.test", DownloadVerifyTLS: false, TransferTimeoutSeconds: 120}
				return base
			},
		},
		{
			name:    "branding",
			path:    "/api/v1/settings/runtime/branding",
			body:    `{"loginTitle":"Seamark Ops","loginSubtitle":"Infrastructure control"}`,
			handler: (*Handler).UpdateBranding,
			expect: func(base RuntimeSettings) RuntimeSettings {
				base.Branding = RuntimeBrandingSettings{LoginTitle: "Seamark Ops", LoginSubtitle: "Infrastructure control"}
				return base
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			tc.handler(NewHandler(svc), rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			envelope := decodeRuntimeEnvelope(t, rec)
			if envelope.Error != nil {
				t.Fatalf("unexpected error response: %#v", envelope.Error)
			}
			want := tc.expect(baseline)
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
		})
	}
}

// TestRuntimePartitionHandlersRejectInvalidValues 验证非法值返回 422 与既有稳定
// 错误码，且内存与持久化中的旧值都不变（SET-RUN-005：失败不得部分生效）。
func TestRuntimePartitionHandlersRejectInvalidValues(t *testing.T) {
	longTitle, err := json.Marshal(strings.Repeat("a", 81))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		path    string
		body    string
		handler func(*Handler, http.ResponseWriter, *http.Request)
		code    string
	}{
		{
			name:    "general invalid language",
			path:    "/api/v1/settings/runtime/general",
			body:    `{"language":"fr"}`,
			handler: (*Handler).UpdateGeneral,
			code:    "invalid_language",
		},
		{
			name:    "general invalid log level",
			path:    "/api/v1/settings/runtime/general",
			body:    `{"logLevel":"verbose"}`,
			handler: (*Handler).UpdateGeneral,
			code:    "invalid_log_level",
		},
		{
			name:    "security invalid token expiration",
			path:    "/api/v1/settings/runtime/security",
			body:    `{"tokenExpiration":"2h"}`,
			handler: (*Handler).UpdateSecurity,
			code:    "invalid_token_expiration",
		},
		{
			name:    "security invalid remote timeout",
			path:    "/api/v1/settings/runtime/security",
			body:    `{"remoteCommandTimeoutSeconds":-1}`,
			handler: (*Handler).UpdateSecurity,
			code:    "invalid_remote_command_timeout",
		},
		{
			name:    "certificates invalid panel domain",
			path:    "/api/v1/settings/runtime/certificates",
			body:    `{"panel":{"domain":"panel.example.test/path"}}`,
			handler: (*Handler).UpdateCertificates,
			code:    "invalid_panel_domain",
		},
		{
			name:    "agent invalid base url",
			path:    "/api/v1/settings/runtime/agent",
			body:    `{"downloadBaseUrl":"ftp://agent.example.test"}`,
			handler: (*Handler).UpdateAgent,
			code:    "invalid_agent_download_base_url",
		},
		{
			name:    "branding title too long",
			path:    "/api/v1/settings/runtime/branding",
			body:    fmt.Sprintf(`{"loginTitle":%s}`, longTitle),
			handler: (*Handler).UpdateBranding,
			code:    "invalid_branding_login_title",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseline := newSeededRuntimeService(t)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			tc.handler(NewHandler(svc), rec, req)

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

// TestRuntimePartitionRoutesRegistered 验证五条分区 PUT 与既有 GET 均已注册，
// 且整体 PUT /api/v1/settings/runtime 已删除（同路径只剩 GET，PUT 返回 405）。
func TestRuntimePartitionRoutesRegistered(t *testing.T) {
	svc := newTestService(t)
	mux := http.NewServeMux()
	NewHandler(svc).RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })

	for _, path := range []string{
		"/api/v1/settings/runtime",
		"/api/v1/settings/runtime/general",
		"/api/v1/settings/runtime/security",
		"/api/v1/settings/runtime/certificates",
		"/api/v1/settings/runtime/agent",
		"/api/v1/settings/runtime/branding",
	} {
		method := http.MethodPut
		var body io.Reader = strings.NewReader(`{}`)
		if path == "/api/v1/settings/runtime" {
			method, body = http.MethodGet, nil
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want 200; body = %s", method, path, rec.Code, rec.Body.String())
		}
	}

	// 整体 PUT 已随分区拆分删除：不得再接受整套 runtime 写入。
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/settings/runtime", strings.NewReader(`{}`)))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /api/v1/settings/runtime status = %d, want 405 (route removed)", rec.Code)
	}
}
