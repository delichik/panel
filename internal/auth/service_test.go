package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/config"
	"panel/internal/settings"
	"panel/internal/storage"
)

func TestLoginValidate(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("expected jwt token")
	}
	claims, ok := svc.verifyJWT(sess.Token)
	if !ok {
		t.Fatal("token should verify")
	}
	if claims.Subject == "admin" {
		t.Fatal("token subject should not expose configured admin username")
	}
	if _, ok := svc.Validate(ctx, sess.Token); !ok {
		t.Fatal("token should validate")
	}
}

func TestValidateRejectsInvalidToken(t *testing.T) {
	svc := newTestService(t)
	if _, ok := svc.Validate(context.Background(), "not-a-token"); ok {
		t.Fatal("invalid token should not validate")
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Login(context.Background(), "admin", "wrong"); err == nil || err.Error() != "Authentication failed" {
		t.Fatal("expected unauthorized")
	}
}

func TestLoginRejectsBadUsernameWithSameMessage(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Login(context.Background(), "not-admin", "admin"); err == nil || err.Error() != "Authentication failed" {
		t.Fatal("expected generic unauthorized")
	}
}

func TestLogoutInvalidatesExistingTokens(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	first, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login first token: %v", err)
	}
	if err := svc.Logout(ctx); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, ok := svc.Validate(ctx, first.Token); ok {
		t.Fatal("token issued before logout should not validate")
	}
	second, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login second token: %v", err)
	}
	if second.Token == first.Token {
		t.Fatal("token should change after logout rotates nonce")
	}
	if _, ok := svc.Validate(ctx, second.Token); !ok {
		t.Fatal("token issued after logout should validate")
	}
}

func TestLoginUsesRuntimeTokenExpiration(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	if _, err := svc.db.ExecContext(ctx, `
		INSERT INTO runtime_settings(key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, settings.RuntimeSettingTokenExpiration, settings.TokenExpirationNever, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("set token expiration: %v", err)
	}

	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	claims, ok := svc.verifyJWT(sess.Token)
	if !ok {
		t.Fatal("token should verify")
	}
	if claims.ExpiresAt != 0 || !sess.ExpiresAt.IsZero() {
		t.Fatalf("never-expiring token should not include exp: claims=%d session=%v", claims.ExpiresAt, sess.ExpiresAt)
	}
	if _, ok := svc.Validate(ctx, sess.Token); !ok {
		t.Fatal("never-expiring token should validate")
	}
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	nonce, err := svc.currentTokenNonce(ctx)
	if err != nil {
		t.Fatalf("current token nonce: %v", err)
	}
	token, err := svc.signJWT(tokenSubject, nonce, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	if _, ok := svc.Validate(ctx, token); ok {
		t.Fatal("expired token should not validate")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc, err := NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return svc
}
