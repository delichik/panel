package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"panel/internal/modules/identity/permissioncatalog"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
)

const (
	credentialTokenPrefix       = "pac_"
	credentialDefaultTTLSeconds = 900
	credentialMinTTLSeconds     = 60
	credentialMaxTTLSeconds     = 1800
	credentialMaxActivePerUser  = 10
	credentialNameMaxLength     = 64
	credentialCleanupInterval   = 5 * time.Minute
	credentialRateLimitUserKey  = ":credential:"
)

// AccessCredentialView 是临时访问凭据的列表投影（不含 token）。
type AccessCredentialView struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Username        string     `json:"username"`
	DisplayName     string     `json:"displayName"`
	TokenPrefix     string     `json:"tokenPrefix"`
	Scope           string     `json:"scope"`
	AllowManagement bool       `json:"allowManagement"`
	Status          string     `json:"status"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	RevokedAt       *time.Time `json:"revokedAt,omitempty"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

// AccessCredentialCreated 只在创建响应中返回一次明文 token。
type AccessCredentialCreated struct {
	AccessCredentialView
	Token string `json:"token"`
}

type AccessCredentialCreateInput struct {
	Name            string `json:"name"`
	ExpiresInSeconds int   `json:"expiresInSeconds"`
	Scope           string `json:"scope"`
	AllowManagement bool   `json:"allowManagement"`
}

// CleanupExpiredCredentials removes temporary access credentials that have expired.
func (s *Service) CleanupExpiredCredentials(ctx context.Context) (int64, error) {
	res, err := orm.RawExec(ctx, s.db,
		`DELETE FROM access_credentials WHERE expires_at <= ?`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// StartPeriodic cleans up expired temporary access credentials at startup and periodically.
func (s *Service) StartPeriodic(ctx context.Context) {
	_, _ = s.CleanupExpiredCredentials(ctx)
	go func() {
		ticker := time.NewTicker(credentialCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.CleanupExpiredCredentials(ctx)
			}
		}
	}()
}

// LoginWithCredentialFrom authenticates a browser/API login request using a temporary access credential.
func (s *Service) LoginWithCredentialFrom(ctx context.Context, rawToken, clientIP string) (Session, error) {
	key := loginRateLimitKey(clientIP, credentialRateLimitUserKey)
	now := time.Now().UTC()
	if !s.limiter.allow(key, now) {
		return Session{}, panelerr.New(http.StatusTooManyRequests, "login_rate_limited", "Too many login attempts; try again later")
	}
	token := strings.TrimSpace(rawToken)
	sess, ok := s.validateCredential(ctx, token)
	if !ok || sess.MustChangePassword {
		s.limiter.recordFailure(key, time.Now().UTC())
		return Session{}, panelerr.Unauthorized("Authentication failed")
	}
	s.limiter.recordSuccess(key)
	return sess, nil
}

func (s *Service) ListAccessCredentials(ctx context.Context, userID string) ([]AccessCredentialView, error) {
	user, err := s.accountByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	_, _ = orm.RawExec(ctx, s.db,
		`DELETE FROM access_credentials WHERE user_id = ? AND expires_at <= ?`,
		userID, time.Now().UTC().Format(time.RFC3339Nano))
	var rows []models.AccessCredential
	if err := orm.New(s.db).From("access_credentials").Where("user_id = ?", userID).OrderBy("created_at DESC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]AccessCredentialView, 0, len(rows))
	for _, row := range rows {
		out = append(out, accessCredentialView(row, user))
	}
	return out, nil
}

func (s *Service) CreateAccessCredential(ctx context.Context, userID string, input AccessCredentialCreateInput) (AccessCredentialCreated, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > credentialNameMaxLength {
		return AccessCredentialCreated{}, panelerr.Validation("invalid_credential_name", "Credential name must be 1..64 characters")
	}
	ttl := input.ExpiresInSeconds
	if ttl == 0 {
		ttl = credentialDefaultTTLSeconds
	}
	if ttl < credentialMinTTLSeconds || ttl > credentialMaxTTLSeconds {
		return AccessCredentialCreated{}, panelerr.Validation("invalid_credential_expiry", "Credential expiry must be between 60 and 1800 seconds")
	}
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = "readonly"
	}
	if scope != "readonly" && scope != "readwrite" {
		return AccessCredentialCreated{}, panelerr.Validation("invalid_credential_scope", "Credential scope must be readonly or readwrite")
	}
	if scope == "readonly" && input.AllowManagement {
		return AccessCredentialCreated{}, panelerr.Validation("credential_management_requires_write_scope", "Management access requires the readwrite scope")
	}
	user, err := s.accountByID(ctx, userID)
	if err != nil {
		return AccessCredentialCreated{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return AccessCredentialCreated{}, err
	}
	token := credentialTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256Hex(token)
	var credential *models.AccessCredential
	if err := orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		var currentGen int64
		if err := orm.New(tx).From("users").Where("id = ?", user.ID).Select("generation").ScanValue(ctx, &currentGen); err != nil {
			return err
		}
		now := time.Now().UTC()
		_, _ = orm.RawExec(ctx, tx,
			`DELETE FROM access_credentials WHERE user_id = ? AND expires_at <= ?`,
			userID, now.Format(time.RFC3339Nano))
		active, err := orm.New(tx).From("access_credentials").
			Where("user_id = ?", userID).
			Where("user_generation = ?", currentGen).
			Where("revoked_at IS NULL").
			Where("expires_at > ?", now.Format(time.RFC3339Nano)).
			Count(ctx)
		if err != nil {
			return err
		}
		if active >= credentialMaxActivePerUser {
			return panelerr.Validation("credential_limit_reached", "Too many active access credentials")
		}
		credential = &models.AccessCredential{
			ID:              id.New("acr"),
			UserID:          user.ID,
			Name:            name,
			TokenHash:       hash,
			TokenPrefix:     token[:12],
			Scope:           scope,
			AllowManagement: input.AllowManagement,
			UserGeneration:  currentGen,
			ExpiresAt:       now.Add(time.Duration(ttl) * time.Second),
			CreatedAt:       now,
		}
		return orm.New(tx).Insert(ctx, credential)
	}); err != nil {
		return AccessCredentialCreated{}, err
	}
	user.Generation = credential.UserGeneration
	return AccessCredentialCreated{AccessCredentialView: accessCredentialView(*credential, user), Token: token}, nil
}

func (s *Service) RevokeAccessCredential(ctx context.Context, userID string, canManageOthers bool, credentialID string) error {
	var credential models.AccessCredential
	if err := orm.New(s.db).From("access_credentials").Where("id = ?", credentialID).First(ctx, &credential); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return panelerr.NotFound("access credential")
		}
		return err
	}
	if credential.UserID != userID && !canManageOthers {
		return panelerr.Forbidden("permission_denied", "Permission denied")
	}
	if credential.RevokedAt != nil {
		return nil
	}
	now := time.Now().UTC()
	return orm.New(s.db).From("access_credentials").Where("id = ?", credentialID).UpdateColumns(ctx, map[string]any{
		"revoked_at": now.Format(time.RFC3339Nano),
		"revoked_by": userID,
	})
}

func (s *Service) validateCredential(ctx context.Context, token string) (Session, bool) {
	if !strings.HasPrefix(token, credentialTokenPrefix) {
		return Session{}, false
	}
	var credential models.AccessCredential
	if err := orm.New(s.db).From("access_credentials").Where("token_hash = ?", sha256Hex(token)).First(ctx, &credential); err != nil {
		return Session{}, false
	}
	now := time.Now().UTC()
	if !now.Before(credential.ExpiresAt) {
		_, _ = orm.RawExec(ctx, s.db, `DELETE FROM access_credentials WHERE id = ?`, credential.ID)
		return Session{}, false
	}
	if credential.RevokedAt != nil {
		return Session{}, false
	}
	entry, err := s.resolveSession(ctx, credential.UserID)
	if err != nil || entry.Status != "active" || credential.UserGeneration != entry.Generation {
		return Session{}, false
	}
	roles, permissions := entry.Roles, entry.Permissions
	if credential.Scope == "readonly" {
		permissions = permissioncatalog.FilterReadOnly(permissions)
	}
	if !credential.AllowManagement {
		permissions = filterOutManagementPermissions(permissions)
	}
	if credential.LastUsedAt == nil || now.Sub(*credential.LastUsedAt) > time.Minute {
		s.scheduleCredentialLastUsed(credential.ID, now)
	}
	return Session{
		UserID: entry.UserID, Username: entry.Username, DisplayName: entry.DisplayName,
		IsBuiltinAdmin: entry.Builtin, Roles: roles, Permissions: permissions,
		Token: token, ExpiresAt: credential.ExpiresAt, MustChangePassword: entry.MustChangePassword,
		AuthKind: "credential", CredentialID: credential.ID, CredentialScope: credential.Scope,
		AllowManagement: credential.AllowManagement,
	}, true
}

func filterOutManagementPermissions(permissions []string) []string {
	out := make([]string, 0, len(permissions))
	for _, key := range permissions {
		if key == "access.users.manage" || key == "access.roles.manage" {
			continue
		}
		out = append(out, key)
	}
	return out
}

func (s *Service) scheduleCredentialLastUsed(credentialID string, now time.Time) {
	if s == nil || s.db == nil || credentialID == "" {
		return
	}
	s.lastUsedMu.Lock()
	if s.lastUsedSeen == nil {
		s.lastUsedSeen = make(map[string]time.Time, 16)
	}
	if last, ok := s.lastUsedSeen[credentialID]; ok && now.Sub(last) < time.Minute {
		s.lastUsedMu.Unlock()
		return
	}
	if len(s.lastUsedSeen) >= 1024 {
		for k, v := range s.lastUsedSeen {
			if now.Sub(v) >= time.Minute {
				delete(s.lastUsedSeen, k)
			}
		}
	}
	s.lastUsedSeen[credentialID] = now
	if s.lastUsedPending == nil {
		s.lastUsedPending = make(map[string]time.Time, 16)
	}
	s.lastUsedPending[credentialID] = now
	startWorker := !s.lastUsedRunning
	if startWorker {
		s.lastUsedRunning = true
	}
	s.lastUsedMu.Unlock()

	if startWorker {
		go s.flushCredentialLastUsed()
	}
}

func (s *Service) flushCredentialLastUsed() {
	for {
		s.lastUsedMu.Lock()
		if len(s.lastUsedPending) == 0 {
			s.lastUsedRunning = false
			s.lastUsedMu.Unlock()
			return
		}
		batch := s.lastUsedPending
		s.lastUsedPending = make(map[string]time.Time, 16)
		s.lastUsedMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = orm.WithTx(ctx, s.db, func(tx *sql.Tx) error {
			for credID, usedAt := range batch {
				ts := usedAt.UTC().Format(time.RFC3339Nano)
				cutoff := usedAt.UTC().Add(-time.Minute).Format(time.RFC3339Nano)
				_, _ = orm.RawExec(ctx, tx, `
					UPDATE access_credentials SET last_used_at = ?
					WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)
				`, ts, credID, cutoff)
			}
			return nil
		})
		cancel()
	}
}

func accessCredentialView(row models.AccessCredential, owner account) AccessCredentialView {
	status := "active"
	now := time.Now().UTC()
	if row.RevokedAt != nil || row.UserGeneration != owner.Generation {
		status = "revoked"
	} else if !now.Before(row.ExpiresAt) {
		status = "expired"
	}
	return AccessCredentialView{
		ID: row.ID, Name: row.Name, Username: owner.Username, DisplayName: owner.DisplayName,
		TokenPrefix: row.TokenPrefix, Scope: row.Scope, AllowManagement: row.AllowManagement,
		Status: status, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt,
		LastUsedAt: row.LastUsedAt, CreatedAt: row.CreatedAt,
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
