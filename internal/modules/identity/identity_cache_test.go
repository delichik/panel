package auth

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/modules/settings"
	"panel/internal/platform/cache"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/database/orm"
)

func newTestServiceWithCache(t *testing.T) (*Service, *cache.Cache) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.AdminPassword = "admin"

	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cacheSvc, err := cache.New(cache.Config{})
	if err != nil {
		_ = store.Close()
		t.Fatalf("new cache: %v", err)
	}
	t.Cleanup(func() {
		_ = cacheSvc.Close()
		_ = store.Close()
	})

	settingsSvc, err := settings.NewService(store.AppDB(), cfg)
	if err != nil {
		t.Fatalf("new settings service: %v", err)
	}
	svc, err := NewService(store.AppDB(), cfg, settingsSvc, WithCache(cacheSvc))
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}
	return svc, cacheSvc
}

func hasPermission(permissions []string, want string) bool {
	for _, key := range permissions {
		if key == want {
			return true
		}
	}
	return false
}

func TestValidateUsesSessionCacheAndGenerationInvalidatesIt(t *testing.T) {
	ctx := context.Background()
	svc, cacheSvc := newTestServiceWithCache(t)

	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, ok := svc.Validate(ctx, sess.Token); !ok {
			t.Fatalf("validate %d: token should be valid", i)
		}
	}
	stats := cacheSvc.Stats()
	if stats.Hits == 0 || stats.Entries == 0 {
		t.Fatalf("cache stats = %+v, want hits and entries", stats)
	}

	if err := svc.BumpUserGeneration(ctx, sess.UserID); err != nil {
		t.Fatalf("bump user generation: %v", err)
	}
	if _, ok := svc.Validate(ctx, sess.Token); ok {
		t.Fatal("token must be rejected immediately after the user generation changes")
	}
}

func TestRoleChangeInvalidatesCachedPermissions(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestServiceWithCache(t)
	admin, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}
	userID := createTestUser(t, svc, "reader", "reader-password", "servers.view")
	userSess, err := svc.Login(ctx, "reader", "reader-password")
	if err != nil {
		t.Fatalf("login reader: %v", err)
	}
	validated, ok := svc.Validate(ctx, userSess.Token)
	if !ok {
		t.Fatal("reader token should be valid")
	}
	if !hasPermission(validated.Permissions, "servers.view") {
		t.Fatalf("permissions = %v, want servers.view", validated.Permissions)
	}

	var roleIDs []string
	if err := orm.New(svc.db).From("user_roles").Where("user_id = ?", userID).Pluck(ctx, "role_id", &roleIDs); err != nil {
		t.Fatalf("load role ids: %v", err)
	}
	if len(roleIDs) != 1 {
		t.Fatalf("role ids = %v, want exactly one", roleIDs)
	}
	empty := []string{}
	if _, err := svc.UpdateUser(ctx, admin.UserID, userID, UserUpdateInput{RoleIDs: &empty}); err != nil {
		t.Fatalf("remove role: %v", err)
	}

	validated, ok = svc.Validate(ctx, userSess.Token)
	if !ok {
		t.Fatal("token should remain valid (only permissions changed)")
	}
	if hasPermission(validated.Permissions, "servers.view") {
		t.Fatalf("permissions = %v, want servers.view removed immediately", validated.Permissions)
	}
}

func TestJWTSecretRotationInvalidatesCachedKeyGeneration(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestServiceWithCache(t)
	sess, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, ok := svc.Validate(ctx, sess.Token); !ok {
		t.Fatal("token should validate before rotation")
	}
	if err := svc.UpdateJWTSecret(ctx, "replacement-jwt-secret-32b"); err != nil {
		t.Fatalf("rotate secret: %v", err)
	}
	if _, ok := svc.Validate(ctx, sess.Token); ok {
		t.Fatal("old token must be rejected immediately after key rotation")
	}
	again, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login after rotation: %v", err)
	}
	if _, ok := svc.Validate(ctx, again.Token); !ok {
		t.Fatal("new token should validate")
	}
}

func TestCredentialValidationUsesSessionCacheAndStaysRevocable(t *testing.T) {
	ctx := context.Background()
	svc, cacheSvc := newTestServiceWithCache(t)
	admin, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	created, err := svc.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
		Name: "ci", ExpiresInSeconds: 120, Scope: "readwrite",
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	before := cacheSvc.Stats().Hits
	for i := 0; i < 3; i++ {
		if _, ok := svc.Validate(ctx, created.Token); !ok {
			t.Fatalf("validate credential %d: should be valid", i)
		}
	}
	if cacheSvc.Stats().Hits <= before {
		t.Fatal("credential validation should reuse the cached session projection")
	}

	if err := svc.RevokeAccessCredential(ctx, admin.UserID, true, created.ID); err != nil {
		t.Fatalf("revoke credential: %v", err)
	}
	if _, ok := svc.Validate(ctx, created.Token); ok {
		t.Fatal("revoked credential must be rejected immediately")
	}
}
