package appearance

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"panel/internal/platform/i18n"
)

// newTestHandlerMux 构造带中间件计数的完整路由表（5 条认证 + 1 条公开）。
func newTestHandlerMux(t *testing.T) (*http.ServeMux, *Service, *int) {
	t.Helper()
	svc, _ := newTestService(t)
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	authCalls := 0
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authCalls++
			next.ServeHTTP(w, r)
		})
	})
	handler.RegisterPublicRoutes(mux)
	return mux, svc, &authCalls
}

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%s)", err, recorder.Body.String())
	}
	return env
}

func dataMap(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	env := decodeEnvelope(t, recorder)
	var data map[string]any
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v (%s)", err, recorder.Body.String())
	}
	return data
}

func backgroundMap(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	background, ok := data["background"].(map[string]any)
	if !ok {
		t.Fatalf("background = %#v", data["background"])
	}
	return background
}

func multipartUploadBody(t *testing.T, fieldName, filename string, content []byte, includeFile bool) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if includeFile {
		part, err := writer.CreateFormFile(fieldName, filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write form file: %v", err)
		}
	} else {
		if err := writer.WriteField("note", "no file here"); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

// TestHandlerRoutesHappyPath 覆盖 GET/PUT/POST/DELETE/content 与公开接口的
// 状态码、字段名和认证中间件调用。
func TestHandlerRoutesHappyPath(t *testing.T) {
	mux, _, authCalls := newTestHandlerMux(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/appearance", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d (%s)", rec.Code, rec.Body.String())
	}
	if *authCalls != 1 {
		t.Fatalf("GET must pass auth middleware, calls = %d", *authCalls)
	}
	data := dataMap(t, rec)
	if data["themeMode"] != ThemeModeSystem || data["mica"] != true || data["accent"] != AccentBlue {
		t.Fatalf("GET data = %#v", data)
	}
	if data["initialized"] != false {
		t.Fatalf("initialized = %#v", data["initialized"])
	}
	if _, ok := data["thumbDataUrl"]; !ok {
		t.Fatalf("thumbDataUrl must be present (null): %#v", data)
	}
	bg := backgroundMap(t, data)
	for _, key := range []string{"kind", "presetId", "assetId", "revision", "hasImage"} {
		if _, ok := bg[key]; !ok {
			t.Fatalf("background missing %q: %#v", key, bg)
		}
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/settings/appearance",
		strings.NewReader(`{"themeMode":"dark","mica":false,"accent":"teal","background":{"kind":"preset","presetId":"mist"}}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%s)", rec.Code, rec.Body.String())
	}
	updated := dataMap(t, rec)
	if updated["themeMode"] != ThemeModeDark || updated["mica"] != false || updated["accent"] != AccentTeal || updated["initialized"] != true {
		t.Fatalf("PUT data = %#v", updated)
	}
	if backgroundMap(t, updated)["kind"] != BackgroundKindPreset || backgroundMap(t, updated)["presetId"] != PresetMist {
		t.Fatalf("PUT background = %#v", backgroundMap(t, updated))
	}

	// 上传 multipart（用户文件名必须被忽略）。
	jpegBytes := encodeTestJPEG(t, 480, 270)
	body, contentType := multipartUploadBody(t, "file", `..\evil name.jpg`, jpegBytes, true)
	rec = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/appearance/background", body)
	request.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload status = %d (%s)", rec.Code, rec.Body.String())
	}
	uploaded := dataMap(t, rec)
	uploadedBackground := backgroundMap(t, uploaded)
	if uploadedBackground["kind"] != BackgroundKindImage || uploadedBackground["hasImage"] != true || uploadedBackground["assetId"] == nil {
		t.Fatalf("upload background = %#v", uploadedBackground)
	}
	if uploaded["thumbDataUrl"] == nil {
		t.Fatalf("upload thumbDataUrl = %#v", uploaded["thumbDataUrl"])
	}

	// content?variant=full/thumb 的响应头与字节。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/appearance/background/content?variant=full", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("content status = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != "inline" {
		t.Fatalf("content-disposition = %q", got)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("ETag must be set")
	}
	fullBytes := rec.Body.Bytes()
	if decoded, err := jpeg.Decode(bytes.NewReader(fullBytes)); err != nil {
		t.Fatalf("decode served image: %v", err)
	} else if decoded.Bounds().Dx() > maxBackgroundEdge {
		t.Fatalf("served master width = %d", decoded.Bounds().Dx())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/appearance/background/content?variant=thumb", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
		t.Fatalf("thumb status = %d len=%d", rec.Code, rec.Body.Len())
	}

	// 公开接口不需要认证中间件。
	authBefore := *authCalls
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/public-appearance", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public status = %d (%s)", rec.Code, rec.Body.String())
	}
	if *authCalls != authBefore {
		t.Fatal("public-appearance must not run auth middleware")
	}
	public := dataMap(t, rec)
	publicBackground := backgroundMap(t, public)
	if _, leaked := publicBackground["assetId"]; leaked {
		t.Fatal("public-appearance must not expose assetId")
	}
	for _, key := range []string{"kind", "presetId", "hasImage"} {
		if _, ok := publicBackground[key]; !ok {
			t.Fatalf("public background missing %q: %#v", key, publicBackground)
		}
	}
	if public["thumbDataUrl"] == nil {
		t.Fatalf("public image kind must carry thumbnail: %#v", public)
	}

	// DELETE 幂等删除并返回最新外观。
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/settings/appearance/background", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d (%s)", rec.Code, rec.Body.String())
	}
	deleted := dataMap(t, rec)
	if backgroundMap(t, deleted)["kind"] != BackgroundKindNone || backgroundMap(t, deleted)["hasImage"] != false {
		t.Fatalf("delete background = %#v", backgroundMap(t, deleted))
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/settings/appearance/background", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second delete status = %d", rec.Code)
	}
}

// TestHandlerErrorMapping 覆盖稳定错误码、HTTP 状态与中文映射。
func TestHandlerErrorMapping(t *testing.T) {
	i18n.SetDefaultLocale(i18n.LocaleSimplifiedChinese)
	t.Cleanup(func() { i18n.SetDefaultLocale(i18n.LocaleEnglish) })
	mux, _, _ := newTestHandlerMux(t)

	assertError := func(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode, wantMessage string) {
		t.Helper()
		if recorder.Code != wantStatus {
			t.Fatalf("status = %d, want %d (%s)", recorder.Code, wantStatus, recorder.Body.String())
		}
		env := decodeEnvelope(t, recorder)
		if env.Error == nil || env.Error.Code != wantCode {
			t.Fatalf("error = %#v, want code %q", env.Error, wantCode)
		}
		if wantMessage != "" && env.Error.Message != wantMessage {
			t.Fatalf("error message = %q, want %q", env.Error.Message, wantMessage)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/settings/appearance", strings.NewReader(`{"themeMode":"neon","accent":"blue"}`)))
	assertError(t, rec, http.StatusUnprocessableEntity, "invalid_theme_mode", "主题模式必须为 system、light 或 dark")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/settings/appearance", strings.NewReader(`{"themeMode":"system","accent":"cyan"}`)))
	assertError(t, rec, http.StatusUnprocessableEntity, "invalid_appearance_accent", "")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/settings/appearance", strings.NewReader(`{"themeMode":"system","accent":"blue","background":{"kind":"image"}}`)))
	assertError(t, rec, http.StatusUnprocessableEntity, "background_required", "")

	body, contentType := multipartUploadBody(t, "file", "bg.jpg", nil, false)
	rec = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/settings/appearance/background", body)
	request.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(rec, request)
	assertError(t, rec, http.StatusUnprocessableEntity, "background_required", "缺少背景图片")

	body, contentType = multipartUploadBody(t, "file", "bg.gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), true)
	rec = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/appearance/background", body)
	request.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(rec, request)
	assertError(t, rec, http.StatusUnsupportedMediaType, "background_unsupported_type", "背景图片只支持 JPEG 或 PNG 格式")

	// 超出显式 MaxBytesReader 上限（10 MiB body）。
	huge, hugeType := multipartUploadBody(t, "file", "bg.jpg", make([]byte, 10<<20), true)
	rec = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/appearance/background", huge)
	request.Header.Set("Content-Type", hugeType)
	mux.ServeHTTP(rec, request)
	assertError(t, rec, http.StatusRequestEntityTooLarge, "background_too_large", "背景图片不能超过 8 MiB")

	// 8 MiB + 1 字节（在解析上限内，由服务端 LimitReader 判定）。
	justOver := encodeTestJPEG(t, 16, 16)
	justOver = append(justOver, make([]byte, MaxBackgroundUploadBytes+1-len(justOver))...)
	body, contentType = multipartUploadBody(t, "file", "bg.jpg", justOver, true)
	rec = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/settings/appearance/background", body)
	request.Header.Set("Content-Type", contentType)
	mux.ServeHTTP(rec, request)
	assertError(t, rec, http.StatusRequestEntityTooLarge, "background_too_large", "")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/appearance/background/content?variant=huge", nil))
	assertError(t, rec, http.StatusBadRequest, "bad_request", "")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/appearance/background/content", nil))
	assertError(t, rec, http.StatusNotFound, "not_found", "")

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/settings/appearance", strings.NewReader(`{bad json`)))
	assertError(t, rec, http.StatusBadRequest, "bad_request", "")
}
