package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panel/internal/modules/identity/permissioncatalog"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
	id "panel/internal/platform/identity"
)

// UserView 是用户管理接口的响应投影。
type UserView struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"displayName"`
	Status             string     `json:"status"`
	Builtin            bool       `json:"builtin"`
	MustChangePassword bool       `json:"mustChangePassword"`
	Roles              []RoleRef  `json:"roles"`
	LastLoginAt        *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

type UserCreateInput struct {
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Password    string   `json:"password"`
	RoleIDs     []string `json:"roleIds"`
}

type UserUpdateInput struct {
	DisplayName *string   `json:"displayName"`
	Status      *string   `json:"status"`
	RoleIDs     *[]string `json:"roleIds"`
}

type RoleView struct {
	ID             string   `json:"id"`
	Key            string   `json:"key,omitempty"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Builtin        bool     `json:"builtin"`
	PermissionKeys []string `json:"permissionKeys"`
	UserCount      int64    `json:"userCount"`
	CreatedAt      time.Time `json:"createdAt"`
}

type RoleSaveInput struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permissionKeys"`
}

func (s *Service) PermissionCatalog() map[string]any {
	return map[string]any{
		"groups":      permissioncatalog.Groups(),
		"permissions": permissioncatalog.Permissions(),
	}
}

func (s *Service) ListUsers(ctx context.Context, page, pageSize int, query, status string) (httpx.ListPage[UserView], error) {
	q := orm.New(s.db).From("users")
	if term := strings.TrimSpace(query); term != "" {
		like := orm.LikeEscaped(term)
		q.AndLike("username", like).OrLike("display_name", like)
	}
	if value := strings.TrimSpace(status); value != "" {
		q.And("status = ?", value)
	}
	total, err := q.Count(ctx)
	if err != nil {
		return httpx.ListPage[UserView]{}, err
	}
	var rows []models.User
	if err := q.OrderBy("created_at ASC", "id ASC").Limit(pageSize).Offset((page - 1) * pageSize).All(ctx, &rows); err != nil {
		return httpx.ListPage[UserView]{}, err
	}
	rolesByUser, err := s.rolesByUserIDs(ctx, userIDs(rows))
	if err != nil {
		return httpx.ListPage[UserView]{}, err
	}
	items := make([]UserView, 0, len(rows))
	for _, row := range rows {
		items = append(items, userView(row, rolesByUser[row.ID]))
	}
	return httpx.ListPage[UserView]{Items: items, Total: int(total), Page: page, PageSize: pageSize}, nil
}

func (s *Service) GetUser(ctx context.Context, userID string) (UserView, error) {
	var row models.User
	if err := orm.New(s.db).From("users").Where("id = ?", userID).First(ctx, &row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserView{}, panelerr.NotFound("user")
		}
		return UserView{}, err
	}
	roles, err := s.rolesForUser(ctx, row.ID)
	if err != nil {
		return UserView{}, err
	}
	return userView(row, roles), nil
}

func (s *Service) CreateUser(ctx context.Context, input UserCreateInput) (UserView, error) {
	username, err := normalizeUsername(input.Username)
	if err != nil {
		return UserView{}, err
	}
	if err := validatePassword(input.Password); err != nil {
		return UserView{}, err
	}
	roleIDs, err := s.normalizeRoleIDs(ctx, input.RoleIDs)
	if err != nil {
		return UserView{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return UserView{}, err
	}
	now := time.Now().UTC()
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = username
	}
	user := &models.User{
		ID: id.New("usr"), Username: username, DisplayName: displayName,
		PasswordHash: string(hash), Status: "active", Generation: 1,
		MustChangePassword: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := ensureUsernameAvailable(ctx, tx, username, ""); err != nil {
			return err
		}
		if err := orm.New(tx).Insert(ctx, user); err != nil {
			return err
		}
		for _, roleID := range roleIDs {
			if err := orm.New(tx).Insert(ctx, &models.UserRole{UserID: user.ID, RoleID: roleID}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return UserView{}, err
	}
	s.invalidateIdentityCaches()
	return s.GetUser(ctx, user.ID)
}

func (s *Service) UpdateUser(ctx context.Context, actorID, userID string, input UserUpdateInput) (UserView, error) {
	var user models.User
	if err := orm.New(s.db).From("users").Where("id = ?", userID).First(ctx, &user); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserView{}, panelerr.NotFound("user")
		}
		return UserView{}, err
	}
	if user.Builtin {
		return UserView{}, panelerr.Forbidden("builtin_admin_immutable", "Built-in administrator cannot be modified")
	}
	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		if status != "active" && status != "disabled" {
			return UserView{}, panelerr.Validation("invalid_user_status", "Status must be active or disabled")
		}
		if status == "disabled" && userID == actorID {
			return UserView{}, panelerr.Validation("cannot_disable_self", "You cannot disable your own account")
		}
	}
	var roleIDs []string
	if input.RoleIDs != nil {
		normalized, err := s.normalizeRoleIDs(ctx, *input.RoleIDs)
		if err != nil {
			return UserView{}, err
		}
		roleIDs = normalized
	}
	now := time.Now().UTC()
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		updates := map[string]any{"updated_at": now.Format(time.RFC3339Nano)}
		if input.DisplayName != nil {
			displayName := strings.TrimSpace(*input.DisplayName)
			if displayName == "" {
				return panelerr.Validation("display_name_required", "Display name is required")
			}
			updates["display_name"] = displayName
		}
		if input.Status != nil {
			status := strings.TrimSpace(*input.Status)
			updates["status"] = status
			if status == "disabled" {
				updates["generation"] = user.Generation + 1
			}
		}
		if err := orm.New(tx).From("users").Where("id = ?", userID).UpdateColumns(ctx, updates); err != nil {
			return err
		}
		if input.RoleIDs != nil {
			if err := orm.New(tx).From("user_roles").Where("user_id = ?", userID).Delete(ctx); err != nil {
				return err
			}
			for _, roleID := range roleIDs {
				if err := orm.New(tx).Insert(ctx, &models.UserRole{UserID: userID, RoleID: roleID}); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return UserView{}, err
	}
	s.invalidateIdentityCaches()
	return s.GetUser(ctx, userID)
}

func (s *Service) DeleteUser(ctx context.Context, actorID, userID string) error {
	var user models.User
	if err := orm.New(s.db).From("users").Where("id = ?", userID).First(ctx, &user); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return panelerr.NotFound("user")
		}
		return err
	}
	if user.Builtin {
		return panelerr.Forbidden("builtin_admin_immutable", "Built-in administrator cannot be deleted")
	}
	if userID == actorID {
		return panelerr.Validation("cannot_delete_self", "You cannot delete your own account")
	}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := orm.RawExec(ctx, tx, `DELETE FROM access_credentials WHERE user_id = ?`, userID); err != nil {
			return err
		}
		if err := orm.New(tx).From("user_roles").Where("user_id = ?", userID).Delete(ctx); err != nil {
			return err
		}
		return orm.New(tx).From("users").Where("id = ?", userID).Delete(ctx)
	}); err != nil {
		return err
	}
	s.invalidateIdentityCaches()
	return nil
}

func (s *Service) ResetUserPassword(ctx context.Context, userID, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	var user models.User
	if err := orm.New(s.db).From("users").Where("id = ?", userID).First(ctx, &user); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return panelerr.NotFound("user")
		}
		return err
	}
	if user.Builtin {
		return panelerr.Forbidden("builtin_admin_immutable", "Built-in administrator password can only be changed by itself")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	err = orm.New(s.db).From("users").Where("id = ?", userID).UpdateColumns(ctx, map[string]any{
		"password_hash":        string(hash),
		"must_change_password": 1,
		"generation":           user.Generation + 1,
		"updated_at":           time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	s.invalidateIdentityCaches()
	return nil
}

func (s *Service) ListRoles(ctx context.Context) ([]RoleView, error) {
	var rows []models.Role
	if err := orm.New(s.db).From("roles").OrderBy("builtin DESC", "name ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	perms, err := s.permissionsByRoleIDs(ctx, roleIDs(rows))
	if err != nil {
		return nil, err
	}
	counts, err := s.userCountsByRole(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleView, 0, len(rows))
	for _, row := range rows {
		out = append(out, RoleView{
			ID: row.ID, Key: row.Key, Name: row.Name, Description: row.Description,
			Builtin: row.Builtin, PermissionKeys: perms[row.ID], UserCount: counts[row.ID], CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func (s *Service) GetRole(ctx context.Context, roleID string) (RoleView, error) {
	var row models.Role
	if err := orm.New(s.db).From("roles").Where("id = ?", roleID).First(ctx, &row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RoleView{}, panelerr.NotFound("role")
		}
		return RoleView{}, err
	}
	perms, err := s.permissionsByRoleIDs(ctx, []string{roleID})
	if err != nil {
		return RoleView{}, err
	}
	counts, err := s.userCountsByRole(ctx)
	if err != nil {
		return RoleView{}, err
	}
	return RoleView{
		ID: row.ID, Key: row.Key, Name: row.Name, Description: row.Description,
		Builtin: row.Builtin, PermissionKeys: perms[row.ID], UserCount: counts[row.ID], CreatedAt: row.CreatedAt,
	}, nil
}

func (s *Service) CreateRole(ctx context.Context, input RoleSaveInput) (RoleView, error) {
	name, permissions, err := normalizeRoleInput(input)
	if err != nil {
		return RoleView{}, err
	}
	now := time.Now().UTC()
	role := &models.Role{ID: id.New("role"), Name: name, CreatedAt: now, UpdatedAt: now}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).Insert(ctx, role); err != nil {
			return err
		}
		return insertRolePermissions(ctx, tx, role.ID, permissions)
	}); err != nil {
		return RoleView{}, err
	}
	s.invalidateIdentityCaches()
	return s.GetRole(ctx, role.ID)
}

func (s *Service) UpdateRole(ctx context.Context, roleID string, input RoleSaveInput) (RoleView, error) {
	var role models.Role
	if err := orm.New(s.db).From("roles").Where("id = ?", roleID).First(ctx, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RoleView{}, panelerr.NotFound("role")
		}
		return RoleView{}, err
	}
	if role.Builtin {
		return RoleView{}, panelerr.Forbidden("builtin_role_immutable", "Built-in role cannot be modified")
	}
	name, permissions, err := normalizeRoleInput(input)
	if err != nil {
		return RoleView{}, err
	}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).From("roles").Where("id = ?", roleID).UpdateColumns(ctx, map[string]any{
			"name": name, "description": strings.TrimSpace(input.Description),
			"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		if err := orm.New(tx).From("role_permissions").Where("role_id = ?", roleID).Delete(ctx); err != nil {
			return err
		}
		return insertRolePermissions(ctx, tx, roleID, permissions)
	}); err != nil {
		return RoleView{}, err
	}
	s.invalidateIdentityCaches()
	return s.GetRole(ctx, roleID)
}

func (s *Service) DeleteRole(ctx context.Context, roleID string) error {
	var role models.Role
	if err := orm.New(s.db).From("roles").Where("id = ?", roleID).First(ctx, &role); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return panelerr.NotFound("role")
		}
		return err
	}
	if role.Builtin {
		return panelerr.Forbidden("builtin_role_immutable", "Built-in role cannot be deleted")
	}
	count, err := orm.New(s.db).From("user_roles").Where("role_id = ?", roleID).Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return panelerr.Conflict("role_in_use", "Role is still assigned to users")
	}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).From("role_permissions").Where("role_id = ?", roleID).Delete(ctx); err != nil {
			return err
		}
		return orm.New(tx).From("roles").Where("id = ?", roleID).Delete(ctx)
	}); err != nil {
		return err
	}
	s.invalidateIdentityCaches()
	return nil
}

func (s *Service) normalizeRoleIDs(ctx context.Context, ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, raw := range ids {
		roleID := strings.TrimSpace(raw)
		if roleID == "" {
			continue
		}
		if _, ok := seen[roleID]; ok {
			continue
		}
		var role models.Role
		if err := orm.New(s.db).From("roles").Where("id = ?", roleID).First(ctx, &role); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, panelerr.Validation("role_not_found", "Role not found: "+roleID)
			}
			return nil, err
		}
		if role.Builtin {
			return nil, panelerr.Validation("builtin_role_not_assignable", "The administrator role is reserved for the built-in administrator")
		}
		seen[roleID] = struct{}{}
		out = append(out, roleID)
	}
	return out, nil
}

func normalizeRoleInput(input RoleSaveInput) (string, []string, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 80 {
		return "", nil, panelerr.Validation("invalid_role_name", "Role name must be 1..80 characters")
	}
	expanded, ok := permissioncatalog.Expand(input.PermissionKeys)
	if !ok {
		return "", nil, panelerr.Validation("unknown_permission", "Role contains an unknown permission")
	}
	for _, key := range expanded {
		if !permissioncatalog.LookupGrantable(key) {
			return "", nil, panelerr.Validation("permission_not_grantable", "Permission is reserved for the built-in administrator: "+key)
		}
	}
	if len(expanded) == 0 {
		return "", nil, panelerr.Validation("role_permissions_required", "Select at least one permission")
	}
	sort.Strings(expanded)
	return name, expanded, nil
}

func insertRolePermissions(ctx context.Context, tx *sql.Tx, roleID string, keys []string) error {
	for _, key := range keys {
		if err := orm.New(tx).Insert(ctx, &models.RolePermission{RoleID: roleID, PermissionKey: key}); err != nil {
			return err
		}
	}
	return nil
}

func ensureUsernameAvailable(ctx context.Context, db orm.Executor, username, exceptID string) error {
	q := orm.New(db).From("users").Where("username = ? COLLATE NOCASE", username)
	if exceptID != "" {
		q.Where("id <> ?", exceptID)
	}
	count, err := q.Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return panelerr.Conflict("username_taken", "Username is already in use")
	}
	return nil
}

func userView(row models.User, roles []RoleRef) UserView {
	return UserView{
		ID: row.ID, Username: row.Username, DisplayName: row.DisplayName,
		Status: row.Status, Builtin: row.Builtin, MustChangePassword: row.MustChangePassword,
		Roles: roles, LastLoginAt: row.LastLoginAt, CreatedAt: row.CreatedAt,
	}
}

func userIDs(rows []models.User) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

func roleIDs(rows []models.Role) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

func (s *Service) rolesByUserIDs(ctx context.Context, ids []string) (map[string][]RoleRef, error) {
	out := map[string][]RoleRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := orm.Raw(ctx, s.db, `
		SELECT ur.user_id, r.id, r.key, r.name
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		ORDER BY r.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		var role RoleRef
		if err := rows.Scan(&userID, &role.ID, &role.Key, &role.Name); err != nil {
			return nil, err
		}
		out[userID] = append(out[userID], role)
	}
	return out, rows.Err()
}

func (s *Service) permissionsByRoleIDs(ctx context.Context, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := orm.Raw(ctx, s.db, `SELECT role_id, permission_key FROM role_permissions ORDER BY permission_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var roleID, key string
		if err := rows.Scan(&roleID, &key); err != nil {
			return nil, err
		}
		out[roleID] = append(out[roleID], key)
	}
	return out, rows.Err()
}

func (s *Service) userCountsByRole(ctx context.Context) (map[string]int64, error) {
	rows, err := orm.Raw(ctx, s.db, `SELECT role_id, COUNT(*) FROM user_roles GROUP BY role_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var roleID string
		var count int64
		if err := rows.Scan(&roleID, &count); err != nil {
			return nil, err
		}
		out[roleID] = count
	}
	return out, rows.Err()
}

// LookupGrantable 由权限目录实现；这里保留一层便于未来扩展。
func (s *Service) requireGrantable(key string) error {
	if !permissioncatalog.LookupGrantable(key) {
		return panelerr.Validation("permission_not_grantable", fmt.Sprintf("Permission is reserved for the built-in administrator: %s", key))
	}
	return nil
}
