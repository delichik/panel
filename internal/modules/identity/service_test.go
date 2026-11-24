package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/modules/settings"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	id "panel/internal/platform/identity"

	"golang.org/x/crypto/bcrypt"
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
	if !sess.MustChangePassword {
		t.Fatal("seeded admin account should require a password change")
	}
	if !sess.IsBuiltinAdmin {
		t.Fatal("seeded admin should be the built-in administrator")
	}
	claims, ok := svc.verifyJWT(sess.Token)
	if !ok {
		t.Fatal("token should verify")
	}
	if claims.Subject == "admin" {
		t.Fatal("token subject should not expose the username")
	}
	if claims.KeyGeneration == 0 || claims.UserGeneration == 0 {
		t.Fatal("token should carry both generations")
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

func TestLogoutInvalidatesOnlyThatUsersTokens(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	first, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	userID := createTestUser(t, svc, "operator", "operator-password", "servers.view")
	other, err := svc.Login(ctx, "operator", "operator-password")
	if err != nil {
		t.Fatalf("login second user: %v", err)
	}
	if err := svc.Logout(ctx, first.UserID); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, ok := svc.Validate(ctx, first.Token); ok {
		t.Fatal("token issued before logout should not validate")
	}
	if _, ok := svc.Validate(ctx, other.Token); !ok {
		t.Fatal("other user's token must stay valid")
	}
	if other.UserID == "" || userID == "" {
		t.Fatal("expected user ids")
	}
}

func TestJWTGenerationsBothMustMatch(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	user, err := svc.accountByID(ctx, sess.UserID)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	keyGen, err := svc.keyGeneration(ctx)
	if err != nil {
		t.Fatalf("key generation: %v", err)
	}

	wrongKey, err := svc.signJWT(user.ID, keyGen+1, user.Generation, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, ok := svc.Validate(ctx, wrongKey); ok {
		t.Fatal("token with wrong key generation must not validate")
	}
	wrongUser, err := svc.signJWT(user.ID, keyGen, user.Generation+1, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, ok := svc.Validate(ctx, wrongUser); ok {
		t.Fatal("token with wrong user generation must not validate")
	}
}

func TestBumpKeyGenerationInvalidatesAllTokens(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.UpdateJWTSecret(ctx, "replacement-jwt-secret-32b"); err != nil {
		t.Fatalf("update jwt secret: %v", err)
	}
	if _, ok := svc.Validate(ctx, sess.Token); ok {
		t.Fatal("old token must be invalidated by key generation")
	}
	again, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login after rotation: %v", err)
	}
	if _, ok := svc.Validate(ctx, again.Token); !ok {
		t.Fatal("new session should validate")
	}
}

func TestPermissionResolutionExpandsDependencies(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	createTestUser(t, svc, "app-reader", "reader-password", "applications.view")
	sess, err := svc.Login(ctx, "app-reader", "reader-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	got := map[string]bool{}
	for _, key := range sess.Permissions {
		got[key] = true
	}
	if !got["applications.view"] || !got["servers.view"] {
		t.Fatalf("expected dependency-closed permissions, got %v", sess.Permissions)
	}
	if got["applications.edit"] {
		t.Fatalf("unexpected write permission: %v", sess.Permissions)
	}
}

func TestLoginUsesRuntimeTokenExpiration(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	runtime := svc.runtime.Runtime()
	_, err := svc.runtime.Update(ctx, settings.RuntimeUpdate{
		TokenExpiration:             settings.TokenExpirationNever,
		Language:                    runtime.Language,
		RemoteCommandTimeoutSeconds: runtime.RemoteCommandTimeoutSeconds,
		Certificates:                &runtime.Certificates,
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

func TestUpdateAccountClearsFirstLoginAndRequiresRelogin(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	first, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	result, err := svc.UpdateAccount(ctx, first.UserID, AccountUpdate{
		CurrentPassword: "admin",
		Username:        "root",
		NewPassword:     "new-admin-password",
	})
	if err != nil {
		t.Fatalf("update account: %v", err)
	}
	if !result.MustRelogin {
		t.Fatal("account update must require a re-login")
	}
	if _, ok := svc.Validate(ctx, first.Token); ok {
		t.Fatal("token issued before password change should not validate")
	}
	updated, err := svc.Login(ctx, "root", "new-admin-password")
	if err != nil {
		t.Fatalf("login with new credentials: %v", err)
	}
	if updated.MustChangePassword {
		t.Fatal("first-login flag should be cleared")
	}
	if updated.Username != "root" {
		t.Fatalf("username = %q", updated.Username)
	}
}

func TestUpdateJWTSecretRequiresRelogin(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	first, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := svc.UpdateJWTSecret(ctx, "replacement-jwt-secret-32b"); err != nil {
		t.Fatalf("update jwt secret: %v", err)
	}
	if _, ok := svc.Validate(ctx, first.Token); ok {
		t.Fatal("old token must be invalidated")
	}
	if _, err := svc.Login(ctx, "admin", "admin"); err != nil {
		t.Fatalf("login after rotation: %v", err)
	}
}

func TestValidateRejectsExpiredToken(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	user, err := svc.accountByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	keyGen, err := svc.keyGeneration(ctx)
	if err != nil {
		t.Fatalf("key generation: %v", err)
	}
	token, err := svc.signJWT(user.ID, keyGen, user.Generation, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	if _, ok := svc.Validate(ctx, token); ok {
		t.Fatal("expired token should not validate")
	}
}

func TestSetupRequiredUntilInitialAdminExists(t *testing.T) {
	ctx := context.Background()
	svc := newTestServiceWithoutAdmin(t)
	required, err := svc.SetupRequired(ctx)
	if err != nil || !required {
		t.Fatalf("setup required = %v, err = %v", required, err)
	}
	if _, err := svc.Login(ctx, "admin", "admin"); err == nil {
		t.Fatal("login must fail before initialization")
	}
	if _, err := svc.CreateInitialAdmin(ctx, "admin", "admin"); err != nil {
		t.Fatalf("create initial admin: %v", err)
	}
	required, err = svc.SetupRequired(ctx)
	if err != nil || required {
		t.Fatalf("setup required after create = %v, err = %v", required, err)
	}
	if _, err := svc.CreateInitialAdmin(ctx, "other", "other-password"); err == nil || err.Error() != "Panel is already initialized" {
		t.Fatalf("expected setup_completed, got %v", err)
	}
	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login after setup: %v", err)
	}
	if !sess.MustChangePassword {
		t.Fatal("initial admin must change password on first login")
	}
}

func TestLoginRateLimitLocksAfterRepeatedFailures(t *testing.T) {
	svc := newTestService(t)
	ip := "10.0.0.1"
	for i := 0; i < loginMaxFailures; i++ {
		if _, err := svc.LoginFrom(context.Background(), "admin", "wrong", ip); err == nil || err.Error() != "Authentication failed" {
			t.Fatalf("attempt %d: expected unauthorized, got %v", i+1, err)
		}
	}
	if _, err := svc.LoginFrom(context.Background(), "admin", "admin", ip); err == nil || err.Error() != "Too many login attempts; try again later" {
		t.Fatalf("expected rate-limited login after repeated failures, got %v", err)
	}
}

func TestLoginRateLimitIsPerIPAndUsername(t *testing.T) {
	svc := newTestService(t)
	for i := 0; i < loginMaxFailures; i++ {
		if _, err := svc.LoginFrom(context.Background(), "admin", "wrong", "10.0.0.2"); err == nil {
			t.Fatal("expected unauthorized")
		}
	}
	if _, err := svc.LoginFrom(context.Background(), "admin", "admin", "10.0.0.3"); err != nil {
		t.Fatalf("different IP should not be blocked: %v", err)
	}
	if _, err := svc.LoginFrom(context.Background(), "not-admin", "admin", "10.0.0.2"); err == nil || err.Error() != "Authentication failed" {
		t.Fatalf("different username should still reach auth check: %v", err)
	}
}

func TestLoginRateLimitResetsAfterSuccess(t *testing.T) {
	svc := newTestService(t)
	ip := "10.0.0.4"
	for i := 0; i < loginMaxFailures-1; i++ {
		if _, err := svc.LoginFrom(context.Background(), "admin", "wrong", ip); err == nil {
			t.Fatal("expected unauthorized")
		}
	}
	if _, err := svc.LoginFrom(context.Background(), "admin", "admin", ip); err != nil {
		t.Fatalf("login after failures should succeed: %v", err)
	}
	for i := 0; i < loginMaxFailures-1; i++ {
		if _, err := svc.LoginFrom(context.Background(), "admin", "wrong", ip); err == nil {
			t.Fatal("expected unauthorized")
		}
	}
	if _, err := svc.LoginFrom(context.Background(), "admin", "admin", ip); err != nil {
		t.Fatalf("login should still succeed below threshold: %v", err)
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWithConfig(t, func(cfg *config.Config) {
		cfg.AdminPassword = "admin"
	})
}

func newTestServiceWithoutAdmin(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWithConfig(t, func(cfg *config.Config) {
		cfg.AdminPassword = ""
		cfg.AdminPasswordHash = ""
	})
}

func newTestServiceWithConfig(t *testing.T, mutate func(*config.Config)) *Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	mutate(&cfg)
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

func createTestUser(t *testing.T, svc *Service, username, password string, permissionKeys ...string) string {
	t.Helper()
	ctx := context.Background()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	roleID := id.New("role")
	now := time.Now().UTC()
	if err := orm.New(svc.db).Insert(ctx, &models.Role{
		ID: roleID, Name: "Test role", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("insert role: %v", err)
	}
	for _, key := range permissionKeys {
		if err := orm.New(svc.db).Insert(ctx, &models.RolePermission{RoleID: roleID, PermissionKey: key}); err != nil {
			t.Fatalf("insert permission: %v", err)
		}
	}
	userID := id.New("usr")
	if err := orm.New(svc.db).Insert(ctx, &models.User{
		ID: userID, Username: username, DisplayName: username, PasswordHash: string(hash),
		Status: "active", Generation: 1, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := orm.New(svc.db).Insert(ctx, &models.UserRole{UserID: userID, RoleID: roleID}); err != nil {
		t.Fatalf("insert user role: %v", err)
	}
	return userID
}

func TestLoginRateLimitNormalizesUsernameCaseAndWhitespace(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	variants := []string{"admin", "Admin", "ADMIN", " admin ", "\tAdMiN"}
	for i, variant := range variants {
		if _, err := svc.LoginFrom(ctx, variant, "wrong-password", "10.0.0.9"); err == nil {
			t.Fatalf("attempt %d (%q): expected failure", i, variant)
		}
	}
	if _, err := svc.LoginFrom(ctx, "admin", "admin", "10.0.0.9"); err == nil || err.Error() != "Too many login attempts; try again later" {
		t.Fatalf("expected rate limit after case/whitespace variants, got %v", err)
	}
}

func TestLoginRateLimiterPruneKeepsActiveUnlockedAttempts(t *testing.T) {
	limiter := newLoginRateLimiter()
	now := time.Now().UTC()
	key := loginRateLimitKey("10.0.0.1", "admin")
	for i := 0; i < 4; i++ {
		limiter.recordFailure(key, now)
	}
	limiter.mu.Lock()
	limiter.pruneLocked(now.Add(time.Minute))
	remaining := limiter.attempts[key]
	limiter.mu.Unlock()
	if remaining == nil || remaining.failures != 4 {
		t.Fatalf("active unlocked attempt was pruned: %#v", remaining)
	}
}

func TestRolesAndPermissionsExpandsKnownKeysWhenUnknownKeyPresent(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	createTestUser(t, svc, "legacy", "legacy-password", "legacy.removed.permission", "applications.edit")
	sess, err := svc.Login(ctx, "legacy", "legacy-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	for _, want := range []string{"applications.edit", "applications.view", "servers.manage", "servers.view"} {
		if !HasPermission(sess.Permissions, want) {
			t.Fatalf("permissions %v missing expanded dependency %q", sess.Permissions, want)
		}
	}
}

func TestCreateInitialAdminRejectsSecondSetup(t *testing.T) {
	ctx := context.Background()
	svc := newTestServiceWithoutAdmin(t)
	if _, err := svc.CreateInitialAdmin(ctx, "admin", "admin"); err != nil {
		t.Fatalf("first setup: %v", err)
	}
	if _, err := svc.CreateInitialAdmin(ctx, "admin2", "admin2"); err == nil {
		t.Fatal("second setup must fail with setup_completed")
	}
}
