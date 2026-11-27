package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpx "panel/internal/platform/http"
)

func TestSessionReturnsUnauthenticatedWithoutToken(t *testing.T) {
	handler := NewHandler(newTestService(t))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()

	handler.Session(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", envelope.Data)
	}
	if data["authenticated"] != false {
		t.Fatalf("authenticated = %v, want false", data["authenticated"])
	}
}

func TestSessionReturnsAuthenticatedWithValidToken(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	sess, err := service.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	handler := NewHandler(service)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	rec := httptest.NewRecorder()

	handler.Session(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", envelope.Data)
	}
	if data["authenticated"] != true {
		t.Fatalf("authenticated = %v, want true", data["authenticated"])
	}
	if data["username"] != "admin" {
		t.Fatalf("username = %v, want admin", data["username"])
	}
	if data["mustChangePassword"] != true {
		t.Fatalf("mustChangePassword = %v, want true", data["mustChangePassword"])
	}
	permissions, ok := data["permissions"].([]any)
	if !ok || len(permissions) == 0 {
		t.Fatalf("built-in administrator must expose all permissions: %#v", data["permissions"])
	}
	user, ok := data["user"].(map[string]any)
	if !ok || user["isBuiltinAdmin"] != true {
		t.Fatalf("user payload = %#v", data["user"])
	}
}

func TestLoginReturnsSessionResponse(t *testing.T) {
	handler := NewHandler(newTestService(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"admin"}`))
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want object", envelope.Data)
	}
	if data["authenticated"] != true || data["token"] == "" {
		t.Fatalf("login response missing authenticated token: %#v", data)
	}
	if data["username"] != "admin" {
		t.Fatalf("username = %v, want admin", data["username"])
	}
	if data["mustChangePassword"] != true {
		t.Fatalf("mustChangePassword = %v, want true", data["mustChangePassword"])
	}
}

func TestLoginFailureReturnsGenericError(t *testing.T) {
	handler := NewHandler(newTestService(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"not-admin","password":"wrong"}`))
	rec := httptest.NewRecorder()

	handler.Login(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	var envelope httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error == nil {
		t.Fatal("expected error envelope")
	}
	if envelope.Error.Code != "unauthorized" || envelope.Error.Message != "Authentication failed" {
		t.Fatalf("error = %#v, want generic unauthorized authentication failure", envelope.Error)
	}
}

func TestLogoutInvalidatesAuthenticatedSession(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	sess, err := service.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	handler := NewHandler(service)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/auth/logout", service.RequireAuthAllowPasswordChange(http.HandlerFunc(handler.Logout)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if _, ok := service.Validate(ctx, sess.Token); ok {
		t.Fatal("token should be invalid after logout")
	}
}

func TestAccessCredentialBlockedOnSessionOnlyAuthEndpoints(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	admin, err := service.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := service.UpdateAccount(ctx, admin.UserID, AccountUpdate{
		CurrentPassword: "admin",
		Username:        "admin",
		NewPassword:     "new-admin-pass-123",
	}); err != nil {
		t.Fatalf("update account: %v", err)
	}
	admin, err = service.Login(ctx, "admin", "new-admin-pass-123")
	if err != nil {
		t.Fatalf("relogin: %v", err)
	}
	cred, err := service.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
		Name:            "rw",
		ExpiresInSeconds: 900,
		Scope:           "readwrite",
		AllowManagement: true,
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}

	handler := NewHandler(service)
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/auth/logout", service.RequireAuthAllowPasswordChange(service.RejectCredentials(http.HandlerFunc(handler.Logout))))
	mux.Handle("POST /api/v1/auth/account", service.RequireAuthAllowPasswordChange(service.RejectCredentials(http.HandlerFunc(handler.UpdateAccount))))
	mux.Handle("POST /api/v1/auth/jwt-secret", service.RequireAuth(service.RejectCredentials(service.RequirePermission("settings.security", http.HandlerFunc(handler.UpdateJWTSecret)))))

	cases := []struct {
		path string
		body string
	}{
		{"/api/v1/auth/logout", ``},
		{"/api/v1/auth/account", `{"currentPassword":"new-admin-pass-123","newPassword":"another-pass-123"}`},
		{"/api/v1/auth/jwt-secret", `{"jwtSecret":"0123456789abcdef0123456789abcdef"}`},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Authorization", "Bearer "+cred.Token)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403, body=%s", tc.path, rec.Code, rec.Body.String())
		}
		var env httpx.Envelope
		if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
			t.Fatalf("%s: decode: %v", tc.path, err)
		}
		if env.Error == nil || env.Error.Code != "credential_scope_restricted" {
			t.Fatalf("%s: error = %#v, want credential_scope_restricted", tc.path, env.Error)
		}
	}
}

func TestLoginWithTemporaryCredential(t *testing.T) {
	ctx := context.Background()
	service := newTestService(t)
	admin, err := service.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := service.UpdateAccount(ctx, admin.UserID, AccountUpdate{
		CurrentPassword: "admin", Username: "admin", NewPassword: "new-admin-pass-123",
	}); err != nil {
		t.Fatalf("update account: %v", err)
	}
	admin, err = service.Login(ctx, "admin", "new-admin-pass-123")
	if err != nil {
		t.Fatalf("relogin: %v", err)
	}
	cred, err := service.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
		Name: "temp", ExpiresInSeconds: 600, Scope: "readwrite", AllowManagement: false,
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	if cred.Username != "admin" {
		t.Fatalf("credential username = %q, want admin", cred.Username)
	}

	handler := NewHandler(service)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"credential":"`+cred.Token+`"}`))
	rec := httptest.NewRecorder()
	handler.Login(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var env httpx.Envelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := env.Data.(map[string]any)
	if data["authenticated"] != true || data["token"] != cred.Token || data["authKind"] != "credential" {
		t.Fatalf("unexpected login payload: %#v", data)
	}
	if data["expiresAt"] == nil || data["expiresAt"] == "" {
		t.Fatalf("expected expiresAt in credential login payload: %#v", data)
	}
	perms := data["permissions"].([]any)
	for _, p := range perms {
		if p == "access.users.manage" || p == "access.roles.manage" {
			t.Fatalf("allowManagement=false credential should not expose %v", p)
		}
	}

	// Invalid credential fails with generic 401.
	badReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"credential":"pac_invalid"}`))
	badRec := httptest.NewRecorder()
	handler.Login(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid credential status = %d, want 401", badRec.Code)
	}
}
