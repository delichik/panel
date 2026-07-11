package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/modules/settings"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
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
	if !sess.PasswordChangeRequired {
		t.Fatal("seeded admin account should require a password change")
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
	runtime := svc.runtime.Runtime()
	_, err := svc.runtime.Update(ctx, settings.RuntimeUpdate{
		MetricsRetentionDays:             runtime.MetricsRetentionDays,
		MetricsCollectionIntervalSeconds: runtime.MetricsCollectionIntervalSeconds,
		CleanupSchedule:                  runtime.CleanupSchedule,
		TokenExpiration:                  settings.TokenExpirationNever,
		Language:                         runtime.Language,
		RemoteCommandTimeoutSeconds:      runtime.RemoteCommandTimeoutSeconds,
		Certificates:                     &runtime.Certificates,
	})
	if err != nil {
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

func TestUpdateAccountClearsPasswordChangeAndRotatesJWTSecret(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	first, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	oldSecret := svc.runtime.JWTSecret()
	updated, err := svc.UpdateAccount(ctx, AccountUpdate{
		CurrentPassword: "admin",
		Username:        "root",
		NewPassword:     "new-admin-password",
	})
	if err != nil {
		t.Fatalf("update account: %v", err)
	}
	if updated.Username != "root" || updated.PasswordChangeRequired {
		t.Fatalf("unexpected updated session: %#v", updated)
	}
	if svc.runtime.JWTSecret() == oldSecret {
		t.Fatal("password change should rotate jwt secret")
	}
	if _, ok := svc.Validate(ctx, first.Token); ok {
		t.Fatal("token signed with previous secret should not validate")
	}
	if _, ok := svc.Validate(ctx, updated.Token); !ok {
		t.Fatal("new token should validate")
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
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatalf("new settings service: %v", err)
	}
	svc, err := NewService(store.AppDB(), cfg, settingsSvc)
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return svc
}
