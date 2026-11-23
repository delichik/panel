package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panel/internal/modules/identity/permissioncatalog"
	"panel/internal/modules/settings"
	"panel/internal/platform/config"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

const (
	authStateKeyGeneration = "jwt_key_generation"
	builtinRoleKey         = "administrator"
	builtinRoleID          = "role_administrator"
	minPasswordLength      = 8
	maxPasswordBytes       = 72
	minUsernameLength      = 3
	maxUsernameLength      = 64
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Service 负责账户、会话与权限解析。
type Service struct {
	cfg       config.Config
	db        *sql.DB
	runtime   *settings.Service
	limiter   *loginRateLimiter
	dummyHash string
}

// Session 是认证后的调用者上下文。权限为实时解析结果。
type Session struct {
	UserID             string
	Username           string
	DisplayName        string
	IsBuiltinAdmin     bool
	Roles              []RoleRef
	Permissions        []string
	Token              string
	ExpiresAt          time.Time
	MustChangePassword bool
	AuthKind           string
	CredentialID       string
	CredentialScope    string
	AllowManagement    bool
}

// RoleRef 是会话中展示用的角色摘要。
type RoleRef struct {
	ID   string `json:"id"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name"`
}

// AccountUpdate 是当前用户自助修改资料的输入。
type AccountUpdate struct {
	CurrentPassword string `json:"currentPassword"`
	Username        string `json:"username"`
	DisplayName     string `json:"displayName"`
	NewPassword     string `json:"newPassword"`
}

// AccountResult 表示资料修改后必须重新登录。
type AccountResult struct {
	MustRelogin bool `json:"mustRelogin"`
}

type account struct {
	ID                 string
	Username           string
	DisplayName        string
	PasswordHash       string
	Status             string
	Builtin            bool
	Generation         int64
	MustChangePassword bool
	CreatedAt          time.Time
}

func NewService(db *sql.DB, cfg config.Config, runtime *settings.Service) (*Service, error) {
	s := &Service{cfg: cfg, db: db, runtime: runtime, limiter: newLoginRateLimiter()}
	hash, err := bcrypt.GenerateFromPassword([]byte("panel-no-such-user"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	s.dummyHash = string(hash)
	if err := s.ensureAuthState(context.Background()); err != nil {
		return nil, err
	}
	if _, err := s.ensureBuiltinRole(context.Background()); err != nil {
		return nil, err
	}
	if err := s.ensureBootstrapAdmin(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// SetupRequired 报告当前是否处于首次运行初始化模式（没有任何用户）。
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	count, err := orm.New(s.db).From("users").Count(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *Service) ensureAuthState(ctx context.Context) error {
	var value string
	err := orm.New(s.db).From("auth_state").Where("key = ?", authStateKeyGeneration).Select("value").ScanValue(ctx, &value)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = orm.RawExec(ctx, s.db, `
		INSERT INTO auth_state(key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO NOTHING
	`, authStateKeyGeneration, "1", time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Service) ensureBuiltinRole(ctx context.Context) (string, error) {
	var roleID string
	err := orm.New(s.db).From("roles").Where("key = ?", builtinRoleKey).Select("id").ScanValue(ctx, &roleID)
	if err == nil {
		return roleID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	now := time.Now().UTC()
	role := &models.Role{
		ID:          builtinRoleID,
		Key:         builtinRoleKey,
		Name:        "Administrator",
		Description: "Built-in administrator with all permissions",
		Builtin:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := orm.New(s.db).Insert(ctx, role); err != nil {
		// A concurrent start may have created it first.
		if lookupErr := orm.New(s.db).From("roles").Where("key = ?", builtinRoleKey).Select("id").ScanValue(ctx, &roleID); lookupErr == nil {
			return roleID, nil
		}
		return "", err
	}
	return role.ID, nil
}

func (s *Service) ensureBootstrapAdmin(ctx context.Context) error {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}
	password := strings.TrimSpace(s.cfg.AdminPassword)
	hash := strings.TrimSpace(s.cfg.AdminPasswordHash)
	var passwordHash string
	switch {
	case password != "":
		if len(password) > maxPasswordBytes {
			return fmt.Errorf("invalid admin password: longer than %d bytes", maxPasswordBytes)
		}
		raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		passwordHash = string(raw)
	case hash != "":
		if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			return fmt.Errorf("invalid admin password hash")
		}
		passwordHash = hash
	default:
		// No bootstrap credentials: the initialization UI must create the
		// built-in administrator.
		return nil
	}
	username := strings.TrimSpace(s.cfg.AdminUsername)
	if username == "" {
		username = "admin"
	}
	displayName := strings.TrimSpace(s.cfg.AdminDisplayName)
	if displayName == "" {
		displayName = "Administrator"
	}
	_, err = s.createBuiltinAdmin(ctx, username, displayName, passwordHash)
	return err
}

func (s *Service) createBuiltinAdmin(ctx context.Context, username, displayName, passwordHash string) (account, error) {
	username, err := normalizeUsername(username)
	if err != nil {
		return account{}, err
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = "Administrator"
	}
	roleID, err := s.ensureBuiltinRole(ctx)
	if err != nil {
		return account{}, err
	}
	now := time.Now().UTC()
	user := &models.User{
		ID:                 id.New("usr"),
		Username:           username,
		DisplayName:        displayName,
		PasswordHash:       passwordHash,
		Status:             "active",
		Builtin:            true,
		Generation:         1,
		MustChangePassword: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := orm.New(tx).Insert(ctx, user); err != nil {
			return err
		}
		return orm.New(tx).Insert(ctx, &models.UserRole{UserID: user.ID, RoleID: roleID})
	}); err != nil {
		return account{}, err
	}
	return account{
		ID: user.ID, Username: user.Username, DisplayName: user.DisplayName,
		PasswordHash: user.PasswordHash, Status: user.Status, Builtin: true,
		Generation: user.Generation, MustChangePassword: true, CreatedAt: now,
	}, nil
}

// CreateInitialAdmin 处理首次运行初始化界面提交。
func (s *Service) CreateInitialAdmin(ctx context.Context, username, password string) (account, error) {
	required, err := s.SetupRequired(ctx)
	if err != nil {
		return account{}, err
	}
	if !required {
		return account{}, panelerr.Conflict("setup_completed", "Panel is already initialized")
	}
	if err := validateInitialPassword(password); err != nil {
		return account{}, err
	}
	raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return account{}, err
	}
	return s.createBuiltinAdmin(ctx, username, "Administrator", string(raw))
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	return s.login(ctx, username, password, "")
}

// LoginFrom is Login with a client IP for per-source brute-force throttling.
func (s *Service) LoginFrom(ctx context.Context, username, password, clientIP string) (Session, error) {
	return s.login(ctx, username, password, clientIP)
}

func (s *Service) login(ctx context.Context, username, password, clientIP string) (Session, error) {
	key := loginRateLimitKey(clientIP, username)
	now := time.Now().UTC()
	if !s.limiter.allow(key, now) {
		return Session{}, panelerr.New(http.StatusTooManyRequests, "login_rate_limited", "Too many login attempts; try again later")
	}
	username = strings.TrimSpace(username)
	user, err := s.accountByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_ = bcrypt.CompareHashAndPassword([]byte(s.dummyHash), []byte(password))
			s.limiter.recordFailure(key, time.Now().UTC())
			return Session{}, panelerr.Unauthorized("Authentication failed")
		}
		return Session{}, err
	}
	matches := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
	if user.Status != "active" || !matches {
		s.limiter.recordFailure(key, time.Now().UTC())
		return Session{}, panelerr.Unauthorized("Authentication failed")
	}
	s.limiter.recordSuccess(key)
	if err := orm.New(s.db).From("users").Where("id = ?", user.ID).UpdateColumns(ctx, map[string]any{
		"last_login_at": time.Now().UTC().Format(time.RFC3339Nano), "updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return Session{}, err
	}
	return s.issueSession(ctx, user)
}

func (s *Service) Logout(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return nil
	}
	return s.BumpUserGeneration(ctx, userID)
}

// Validate 校验 JWT 签名、kind、过期时间与双代数，或校验访问凭证。
func (s *Service) Validate(ctx context.Context, token string) (Session, bool) {
	if strings.HasPrefix(token, credentialTokenPrefix) {
		return s.validateCredential(ctx, token)
	}
	claims, ok := s.verifyJWT(token)
	if !ok || claims.Subject == "" || claims.Kind != "session" || tokenExpired(claims.ExpiresAt) {
		return Session{}, false
	}
	keyGen, err := s.keyGeneration(ctx)
	if err != nil || claims.KeyGeneration != keyGen {
		return Session{}, false
	}
	user, err := s.accountByID(ctx, claims.Subject)
	if err != nil || user.Status != "active" || user.Generation != claims.UserGeneration {
		return Session{}, false
	}
	roles, permissions, err := s.rolesAndPermissions(ctx, user)
	if err != nil {
		return Session{}, false
	}
	var expiresAt time.Time
	if claims.ExpiresAt > 0 {
		expiresAt = time.Unix(claims.ExpiresAt, 0).UTC()
	}
	return Session{
		UserID:             user.ID,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		IsBuiltinAdmin:     user.Builtin,
		Roles:              roles,
		Permissions:        permissions,
		Token:              token,
		ExpiresAt:          expiresAt,
		MustChangePassword: user.MustChangePassword,
		AuthKind:           "session",
	}, true
}

// UpdateAccount 修改当前用户资料；成功后递增代数并强制重新登录。
func (s *Service) UpdateAccount(ctx context.Context, userID string, input AccountUpdate) (AccountResult, error) {
	user, err := s.accountByID(ctx, userID)
	if err != nil {
		return AccountResult{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.CurrentPassword)) != nil {
		return AccountResult{}, panelerr.Unauthorized("Authentication failed")
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = user.Username
	}
	normalized, err := normalizeUsername(username)
	if err != nil {
		return AccountResult{}, err
	}
	if user.Builtin && !user.MustChangePassword && normalized != user.Username {
		return AccountResult{}, panelerr.Forbidden("builtin_username_locked", "Built-in administrator username cannot be changed after first login")
	}
	if normalized != user.Username {
		count, err := orm.New(s.db).From("users").Where("username = ? COLLATE NOCASE", normalized).Where("id <> ?", user.ID).Count(ctx)
		if err != nil {
			return AccountResult{}, err
		}
		if count > 0 {
			return AccountResult{}, panelerr.Validation("username_taken", "Username is already in use")
		}
	}
	passwordChanging := input.NewPassword != ""
	if user.MustChangePassword && !passwordChanging {
		return AccountResult{}, panelerr.Validation("password_change_required", "Password must be changed before continuing")
	}
	nextHash := user.PasswordHash
	if passwordChanging {
		if err := validatePassword(input.NewPassword); err != nil {
			return AccountResult{}, err
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.NewPassword)) == nil {
			return AccountResult{}, panelerr.Validation("admin_password_unchanged", "New password must be different from the current password")
		}
		raw, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return AccountResult{}, err
		}
		nextHash = string(raw)
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = user.DisplayName
	}
	now := time.Now().UTC()
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		return orm.New(tx).From("users").Where("id = ?", user.ID).UpdateColumns(ctx, map[string]any{
			"username":             normalized,
			"display_name":         displayName,
			"password_hash":        nextHash,
			"must_change_password": 0,
			"generation":           user.Generation + 1,
			"updated_at":           now.Format(time.RFC3339Nano),
		})
	}); err != nil {
		return AccountResult{}, err
	}
	return AccountResult{MustRelogin: true}, nil
}

// UpdateJWTSecret 轮换签名密钥并递增全局密钥代数。
func (s *Service) UpdateJWTSecret(ctx context.Context, secret string) error {
	secret = strings.TrimSpace(secret)
	if err := settings.ValidateJWTSecret(secret); err != nil {
		return err
	}
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.runtime.PersistJWTSecretTx(ctx, tx, secret); err != nil {
			return err
		}
		return s.bumpKeyGenerationTx(ctx, tx)
	}); err != nil {
		return err
	}
	s.runtime.ApplyJWTSecret(secret)
	return nil
}

// BumpUserGeneration 吊销某用户的全部 JWT 与访问凭证。
func (s *Service) BumpUserGeneration(ctx context.Context, userID string) error {
	_, err := orm.RawExec(ctx, s.db,
		`UPDATE users SET generation = generation + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), userID)
	return err
}

func (s *Service) issueSession(ctx context.Context, user account) (Session, error) {
	keyGen, err := s.keyGeneration(ctx)
	if err != nil {
		return Session{}, err
	}
	expiresAt, err := s.nextExpiresAt(ctx)
	if err != nil {
		return Session{}, err
	}
	token, err := s.signJWT(user.ID, keyGen, user.Generation, expiresAt)
	if err != nil {
		return Session{}, err
	}
	roles, permissions, err := s.rolesAndPermissions(ctx, user)
	if err != nil {
		return Session{}, err
	}
	return Session{
		UserID:             user.ID,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		IsBuiltinAdmin:     user.Builtin,
		Roles:              roles,
		Permissions:        permissions,
		Token:              token,
		ExpiresAt:          expiresAt,
		MustChangePassword: user.MustChangePassword,
		AuthKind:           "session",
	}, nil
}

func (s *Service) signJWT(userID string, keyGen, userGen int64, expiresAt time.Time) (string, error) {
	header, err := encodeJWTPart(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims := jwtClaims{Subject: userID, Kind: "session", KeyGeneration: keyGen, UserGeneration: userGen}
	if !expiresAt.IsZero() {
		claims.ExpiresAt = expiresAt.Unix()
	}
	payload, err := encodeJWTPart(claims)
	if err != nil {
		return "", err
	}
	signingInput := header + "." + payload
	return signingInput + "." + s.sign(signingInput), nil
}

func (s *Service) verifyJWT(token string) (jwtClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, false
	}
	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(s.sign(signingInput))) {
		return jwtClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, false
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}, false
	}
	return claims, true
}

func (s *Service) sign(value string) string {
	mac := hmac.New(sha256.New, []byte(s.jwtSecret()))
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) jwtSecret() string {
	if s.runtime != nil {
		return s.runtime.JWTSecret()
	}
	if strings.TrimSpace(s.cfg.JWTSecret) != "" {
		return s.cfg.JWTSecret
	}
	return settings.DefaultJWTSecret
}

func (s *Service) accountByID(ctx context.Context, userID string) (account, error) {
	var user models.User
	if err := orm.New(s.db).From("users").Where("id = ?", userID).First(ctx, &user); err != nil {
		return account{}, err
	}
	return toAccount(user), nil
}

func (s *Service) accountByUsername(ctx context.Context, username string) (account, error) {
	var user models.User
	if err := orm.New(s.db).From("users").Where("username = ? COLLATE NOCASE", username).First(ctx, &user); err != nil {
		return account{}, err
	}
	return toAccount(user), nil
}

func toAccount(user models.User) account {
	return account{
		ID:                 user.ID,
		Username:           user.Username,
		DisplayName:        user.DisplayName,
		PasswordHash:       user.PasswordHash,
		Status:             user.Status,
		Builtin:            user.Builtin,
		Generation:         user.Generation,
		MustChangePassword: user.MustChangePassword,
		CreatedAt:          user.CreatedAt,
	}
}

func (s *Service) rolesAndPermissions(ctx context.Context, user account) ([]RoleRef, []string, error) {
	if user.Builtin {
		roles, err := s.rolesForUser(ctx, user.ID)
		if err != nil {
			return nil, nil, err
		}
		return roles, permissioncatalog.AllKeys(), nil
	}
	roles, err := s.rolesForUser(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	var keys []string
	if len(roles) > 0 {
		rows, err := orm.Raw(ctx, s.db, `
			SELECT DISTINCT rp.permission_key
			FROM user_roles ur
			JOIN role_permissions rp ON rp.role_id = ur.role_id
			WHERE ur.user_id = ?
			ORDER BY rp.permission_key
		`, user.ID)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return nil, nil, err
			}
			keys = append(keys, key)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
	}
	expanded, ok := permissioncatalog.Expand(keys)
	if !ok {
		// Defensive: a role may reference a removed permission. Keep the known
		// subset instead of locking the user out entirely.
		expanded = keys
	}
	return roles, expanded, nil
}

func (s *Service) rolesForUser(ctx context.Context, userID string) ([]RoleRef, error) {
	rows, err := orm.Raw(ctx, s.db, `
		SELECT r.id, r.key, r.name
		FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = ?
		ORDER BY r.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []RoleRef{}
	for rows.Next() {
		var role RoleRef
		if err := rows.Scan(&role.ID, &role.Key, &role.Name); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *Service) keyGeneration(ctx context.Context) (int64, error) {
	var value string
	if err := orm.New(s.db).From("auth_state").Where("key = ?", authStateKeyGeneration).Select("value").ScanValue(ctx, &value); err != nil {
		return 0, err
	}
	var generation int64
	if _, err := fmt.Sscan(strings.TrimSpace(value), &generation); err != nil || generation <= 0 {
		return 1, nil
	}
	return generation, nil
}

func (s *Service) bumpKeyGenerationTx(ctx context.Context, tx *sql.Tx) error {
	_, err := orm.RawExec(ctx, tx, `
		UPDATE auth_state
		SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT), updated_at = ?
		WHERE key = ?
	`, time.Now().UTC().Format(time.RFC3339Nano), authStateKeyGeneration)
	return err
}

func (s *Service) nextExpiresAt(ctx context.Context) (time.Time, error) {
	duration, err := s.tokenExpirationDuration(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if duration == 0 {
		return time.Time{}, nil
	}
	return time.Now().UTC().Add(duration), nil
}

func (s *Service) tokenExpirationDuration(ctx context.Context) (time.Duration, error) {
	value := settings.DefaultTokenExpiration
	if s.runtime != nil {
		value = s.runtime.Runtime().TokenExpiration
	} else {
		err := orm.New(s.db).From("runtime_settings").Where("key = ?", settings.RuntimeSettingTokenExpiration).Select("value").ScanValue(ctx, &value)
		if errors.Is(err, sql.ErrNoRows) {
			value = settings.DefaultTokenExpiration
		} else if err != nil {
			return 0, err
		}
	}
	duration, ok := settings.TokenExpirationDuration(value)
	if !ok {
		duration, _ = settings.TokenExpirationDuration(settings.DefaultTokenExpiration)
	}
	return duration, nil
}

func tokenExpired(expiresAt int64) bool {
	return expiresAt > 0 && time.Now().UTC().After(time.Unix(expiresAt, 0))
}

func normalizeUsername(value string) (string, error) {
	username := strings.TrimSpace(value)
	if username == "" {
		return "", panelerr.Validation("admin_username_required", "Username is required")
	}
	if len(username) < minUsernameLength || len(username) > maxUsernameLength {
		return "", panelerr.Validation("invalid_username", fmt.Sprintf("Username must be %d..%d characters", minUsernameLength, maxUsernameLength))
	}
	if !usernamePattern.MatchString(username) {
		return "", panelerr.Validation("invalid_username", "Username may only contain letters, digits, dot, underscore and hyphen")
	}
	return username, nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return panelerr.Validation("admin_password_too_short", "Password must be at least 8 characters")
	}
	if len(password) > maxPasswordBytes {
		return panelerr.Validation("admin_password_too_long", "Password must be at most 72 bytes")
	}
	return nil
}

// validateInitialPassword 允许旧版默认的短初始化密码（如 admin），首次登录
// 会被强制改成符合常规策略的密码。
func validateInitialPassword(password string) error {
	if strings.TrimSpace(password) == "" {
		return panelerr.Validation("admin_password_required", "Password is required")
	}
	if len(password) > maxPasswordBytes {
		return panelerr.Validation("admin_password_too_long", "Password must be at most 72 bytes")
	}
	return nil
}

func encodeJWTPart(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type jwtClaims struct {
	Subject        string `json:"sub"`
	Kind           string `json:"kind"`
	KeyGeneration  int64  `json:"kg"`
	UserGeneration int64  `json:"ug"`
	ExpiresAt      int64  `json:"exp,omitempty"`
}
