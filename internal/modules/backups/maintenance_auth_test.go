package backups

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMaintenanceLoginRateLimitLocksAfterRepeatedFailures(t *testing.T) {
	auth := testMaintenanceAuth(t, maintenanceAuthExport)
	now := time.Now().UTC()
	auth.now = func() time.Time { return now }
	handler := auth.loginAPI

	postLogin := func(password string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"`+password+`"}`))
		req.RemoteAddr = "203.0.113.10:12345"
		handler(rec, req)
		return rec
	}

	for i := 0; i < maintenanceLoginMaxAttempts; i++ {
		if rec := postLogin("wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}
	// Correct password is still rejected while locked out.
	if rec := postLogin("password"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked out: status = %d, want 429; body=%s", rec.Code, rec.Body.String())
	}
	// After the lockout expires the correct password succeeds.
	now = now.Add(maintenanceLoginLockout + time.Second)
	if rec := postLogin("password"); rec.Code != http.StatusOK {
		t.Fatalf("after lockout: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Success reset the counter: one more failure must not lock again.
	if rec := postLogin("wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after reset: status = %d, want 401", rec.Code)
	}
}

func TestMaintenanceAuthSweepsExpiredSessions(t *testing.T) {
	auth := testMaintenanceAuth(t, maintenanceAuthExport)
	now := time.Now().UTC()
	auth.now = func() time.Time { return now }
	token := maintenanceTokenPrefix(auth.context) + "expired-token"
	auth.mu.Lock()
	auth.sessions[token] = maintenanceSession{username: auth.username, expiresAt: now.Add(-time.Minute)}
	auth.mu.Unlock()

	if _, ok := auth.validate(token); ok {
		t.Fatal("expired session should be rejected")
	}
	auth.mu.RLock()
	_, stillPresent := auth.sessions[token]
	auth.mu.RUnlock()
	if stillPresent {
		t.Fatal("expired session should be removed on validate")
	}
}
