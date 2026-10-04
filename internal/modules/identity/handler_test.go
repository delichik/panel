package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"panel/internal/platform/http"
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
	if data["passwordChangeRequired"] != true {
		t.Fatalf("passwordChangeRequired = %v, want true", data["passwordChangeRequired"])
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
	if data["passwordChangeRequired"] != true {
		t.Fatalf("passwordChangeRequired = %v, want true", data["passwordChangeRequired"])
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	rec := httptest.NewRecorder()

	handler.Logout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if _, ok := service.Validate(ctx, sess.Token); ok {
		t.Fatal("token should be invalid after logout")
	}
}
