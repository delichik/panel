package auth

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"panel/internal/modules/identity/permissioncatalog"
)

func TestEveryAPIRouteResolvesToPermission(t *testing.T) {
	root := repositoryRoot(t)
	files := []string{filepath.Join(root, "internal", "bootstrap", "panel", "app.go")}
	if err := filepath.WalkDir(filepath.Join(root, "internal", "modules"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "routes.go" {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	public := map[string]bool{
		"POST /api/v1/auth/login":                true,
		"GET /api/v1/setup/status":               true,
		"POST /api/v1/setup":                     true,
		"GET /api/v1/settings/public-branding":   true,
		"GET /api/v1/settings/public-appearance": true,
	}
	patterns := routePatterns(t, files)
	sort.Strings(patterns)
	for _, pattern := range patterns {
		if !strings.HasPrefix(pattern, "GET /api/") && !strings.HasPrefix(pattern, "POST /api/") &&
			!strings.HasPrefix(pattern, "PUT /api/") && !strings.HasPrefix(pattern, "PATCH /api/") &&
			!strings.HasPrefix(pattern, "DELETE /api/") {
			continue
		}
		if public[pattern] {
			continue
		}
		method, path, _ := strings.Cut(pattern, " ")
		key, ok := RequiredPermission(method, path)
		if !ok {
			t.Errorf("route %q has no permission mapping", pattern)
			continue
		}
		if key != "" {
			if _, exists := permissioncatalog.Lookup(key); !exists {
				t.Errorf("route %q maps to unknown permission %q", pattern, key)
			}
		}
	}
}

func TestCatalogDependenciesAreValid(t *testing.T) {
	for _, permission := range permissioncatalog.Permissions() {
		expanded, ok := permissioncatalog.Expand([]string{permission.Key})
		if !ok {
			t.Fatalf("permission %q has invalid dependency", permission.Key)
		}
		for _, key := range expanded {
			if permission.Mode == permissioncatalog.ModeRead && !permissioncatalog.IsReadOnly(key) {
				t.Fatalf("read-only permission %q depends on write permission %q", permission.Key, key)
			}
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func routePatterns(t *testing.T, files []string) []string {
	t.Helper()
	found := map[string]struct{}{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Handle" && selector.Sel.Name != "HandleFunc") {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			pattern, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			found[pattern] = struct{}{}
			return true
		})
	}
	patterns := make([]string, 0, len(found))
	for pattern := range found {
		patterns = append(patterns, pattern)
	}
	return patterns
}

func TestRoleAndUserLifecycle(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	role, err := svc.CreateRole(ctx, RoleSaveInput{Name: "Ops", PermissionKeys: []string{"applications.edit"}})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	for _, key := range []string{"applications.edit", "applications.view", "servers.manage", "servers.view"} {
		if !contains(role.PermissionKeys, key) {
			t.Fatalf("role permissions missing dependency %q: %v", key, role.PermissionKeys)
		}
	}
	user, err := svc.CreateUser(ctx, UserCreateInput{Username: "alice", Password: "alice-password", RoleIDs: []string{role.ID}})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if !user.MustChangePassword {
		t.Fatal("new user must change password on first login")
	}
	sess, err := svc.Login(ctx, "alice", "alice-password")
	if err != nil {
		t.Fatalf("login created user: %v", err)
	}
	if !sess.MustChangePassword || !contains(sess.Permissions, "servers.view") {
		t.Fatalf("unexpected session: %#v", sess)
	}
	if _, err := svc.CreateRole(ctx, RoleSaveInput{Name: "Bad", PermissionKeys: []string{"access.users.manage"}}); err == nil {
		t.Fatal("admin-only permission must not be grantable")
	}
	if err := svc.DeleteRole(ctx, role.ID); err == nil || err.Error() != "Role is still assigned to users" {
		t.Fatalf("expected role_in_use, got %v", err)
	}
	if err := svc.DeleteUser(ctx, sess.UserID, sess.UserID); err == nil {
		t.Fatal("deleting self must be rejected")
	}
	builtin, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("builtin login: %v", err)
	}
	if _, err := svc.UpdateUser(ctx, builtin.UserID, builtin.UserID, UserUpdateInput{}); err == nil {
		t.Fatal("built-in administrator must be immutable")
	}
}

func TestAccessCredentialLifecycle(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	createTestUser(t, svc, "reader", "reader-password", "servers.view", "servers.manage")
	sess, err := svc.Login(ctx, "reader", "reader-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	sess, err = finishFirstLogin(t, svc, sess)
	if err != nil {
		t.Fatalf("finish first login: %v", err)
	}
	created, err := svc.CreateAccessCredential(ctx, sess.UserID, AccessCredentialCreateInput{Name: "ci", ExpiresInSeconds: 900, Scope: "readonly"})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	if created.Token == "" || !strings.HasPrefix(created.Token, "pac_") {
		t.Fatalf("unexpected token: %q", created.Token)
	}
	credSess, ok := svc.Validate(ctx, created.Token)
	if !ok || credSess.AuthKind != "credential" || credSess.CredentialScope != "readonly" {
		t.Fatalf("credential session = %#v, ok=%v", credSess, ok)
	}
	if contains(credSess.Permissions, "servers.manage") {
		t.Fatalf("readonly credential must not carry write permissions: %v", credSess.Permissions)
	}
	if _, err := svc.CreateAccessCredential(ctx, sess.UserID, AccessCredentialCreateInput{Name: "bad", ExpiresInSeconds: 900, Scope: "readonly", AllowManagement: true}); err == nil {
		t.Fatal("readonly credential must not allow management")
	}
	if _, err := svc.CreateAccessCredential(ctx, sess.UserID, AccessCredentialCreateInput{Name: "long", ExpiresInSeconds: 1801}); err == nil {
		t.Fatal("expiry above 30 minutes must be rejected")
	}
	if err := svc.RevokeAccessCredential(ctx, sess.UserID, false, created.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := svc.Validate(ctx, created.Token); ok {
		t.Fatal("revoked credential must not validate")
	}
	if err := svc.BumpUserGeneration(ctx, sess.UserID); err != nil {
		t.Fatalf("bump generation: %v", err)
	}
	second, err := svc.CreateAccessCredential(ctx, sess.UserID, AccessCredentialCreateInput{Name: "ci2", ExpiresInSeconds: 900, Scope: "readwrite"})
	if err != nil {
		t.Fatalf("create second credential: %v", err)
	}
	if err := svc.BumpUserGeneration(ctx, sess.UserID); err != nil {
		t.Fatalf("bump generation again: %v", err)
	}
	if _, ok := svc.Validate(ctx, second.Token); ok {
		t.Fatal("user generation bump must invalidate credentials")
	}
}

func finishFirstLogin(t *testing.T, svc *Service, sess Session) (Session, error) {
	t.Helper()
	ctx := context.Background()
	if !sess.MustChangePassword {
		return sess, nil
	}
	if _, err := svc.UpdateAccount(ctx, sess.UserID, AccountUpdate{
		CurrentPassword: "reader-password",
		Username:        sess.Username,
		NewPassword:     "reader-password-2",
	}); err != nil {
		return Session{}, err
	}
	return svc.Login(ctx, sess.Username, "reader-password-2")
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestUserRoleAndCredentialBugfixes(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	admin, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login admin: %v", err)
	}

	// 1. CreateRole preserves description and ListRoles/GetRole populate builtin permissionKeys.
	role, err := svc.CreateRole(ctx, RoleSaveInput{
		Name:           "Auditor",
		Description:    "Read-only audit role",
		PermissionKeys: []string{"events.view"},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	if role.Description != "Read-only audit role" {
		t.Fatalf("role description = %q, want %q", role.Description, "Read-only audit role")
	}
	roles, err := svc.ListRoles(ctx)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	foundBuiltin := false
	for _, item := range roles {
		if item.Builtin {
			foundBuiltin = true
			if len(item.PermissionKeys) != len(permissioncatalog.AllKeys()) {
				t.Fatalf("builtin role permissionKeys len = %d, want %d", len(item.PermissionKeys), len(permissioncatalog.AllKeys()))
			}
		}
	}
	if !foundBuiltin {
		t.Fatal("expected built-in administrator role")
	}

	// 2. User without roles serializes non-nil empty slice and ListUsers respects query + status precedence.
	u1, err := svc.CreateUser(ctx, UserCreateInput{Username: "alpha_user", DisplayName: "Alpha", Password: "password-1234"})
	if err != nil {
		t.Fatalf("create u1: %v", err)
	}
	if u1.Roles == nil || len(u1.Roles) != 0 {
		t.Fatalf("expected non-nil empty roles slice, got %#v", u1.Roles)
	}
	disabled := "disabled"
	if _, err := svc.UpdateUser(ctx, admin.UserID, u1.ID, UserUpdateInput{Status: &disabled}); err != nil {
		t.Fatalf("disable u1: %v", err)
	}
	page, err := svc.ListUsers(ctx, 1, 20, "alpha", "active")
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("disabled user matched query+active filter: %#v", page.Items)
	}

	// 3. ForceUserLogout returns 404 for unknown user.
	if err := svc.ForceUserLogout(ctx, "usr_nonexistent"); err == nil {
		t.Fatal("expected not found for unknown user")
	}

	// 4. Generation bump marks previous credentials revoked in list and frees active quota.
	for i := 0; i < credentialMaxActivePerUser; i++ {
		if _, err := svc.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
			Name: "batch", ExpiresInSeconds: 600, Scope: "readonly",
		}); err != nil {
			t.Fatalf("create cred %d: %v", i, err)
		}
	}
	if err := svc.BumpUserGeneration(ctx, admin.UserID); err != nil {
		t.Fatalf("bump generation: %v", err)
	}
	creds, err := svc.ListAccessCredentials(ctx, admin.UserID)
	if err != nil {
		t.Fatalf("list credentials: %v", err)
	}
	for _, c := range creds {
		if c.Status != "revoked" {
			t.Fatalf("credential %s status = %q after generation bump, want revoked", c.ID, c.Status)
		}
	}
	if _, err := svc.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
		Name: "after-bump", ExpiresInSeconds: 600, Scope: "readonly",
	}); err != nil {
		t.Fatalf("create credential after generation bump should succeed: %v", err)
	}
}

func TestExpiredAccessCredentialsAreAutomaticallyDeleted(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)
	admin, err := svc.Login(ctx, "admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	created, err := svc.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
		Name: "short", ExpiresInSeconds: 60, Scope: "readonly",
	})
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	// Backdate expiry into the past.
	if _, err := svc.db.ExecContext(ctx,
		`UPDATE access_credentials SET expires_at = ? WHERE id = ?`,
		"2020-01-01T00:00:00Z", created.ID); err != nil {
		t.Fatalf("backdate expires_at: %v", err)
	}
	// Listing should automatically delete expired credentials.
	list, err := svc.ListAccessCredentials(ctx, admin.UserID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected expired credential to be deleted on list, got %#v", list)
	}
	var remaining int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_credentials WHERE id = ?`, created.ID).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expired credential row still in database: %d", remaining)
	}

	// CleanupExpiredCredentials also removes expired rows globally.
	second, err := svc.CreateAccessCredential(ctx, admin.UserID, AccessCredentialCreateInput{
		Name: "short2", ExpiresInSeconds: 60, Scope: "readonly",
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := svc.db.ExecContext(ctx,
		`UPDATE access_credentials SET expires_at = ? WHERE id = ?`,
		"2020-01-01T00:00:00Z", second.ID); err != nil {
		t.Fatalf("backdate second: %v", err)
	}
	deleted, err := svc.CleanupExpiredCredentials(ctx)
	if err != nil || deleted != 1 {
		t.Fatalf("CleanupExpiredCredentials = (%d, %v), want (1, nil)", deleted, err)
	}
}
