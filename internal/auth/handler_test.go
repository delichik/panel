package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"panel/internal/config"
	"panel/internal/httpx"
)

func TestSessionReturnsUnauthenticatedWithoutToken(t *testing.T) {
	handler := NewHandler(NewService(config.Default()))
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
	service := NewService(config.Default())
	sess, err := service.Login("admin", "admin")
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
}
