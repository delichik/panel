package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
)

// AccessCredentialView 是访问凭证的列表投影（不含 token）。
type AccessCredentialView struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
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

func (s *Service) ListAccessCredentials(ctx context.Context, userID string) ([]AccessCredentialView, error) {
	var rows []models.AccessCredential
	if err := orm.New(s.db).From("access_credentials").Where("user_id = ?", userID).OrderBy("created_at DESC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]AccessCredentialView, 0, len(rows))
	for _, row := range rows {
		out = append(out, accessCredentialView(row))
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
	active, err := orm.New(s.db).From("access_credentials").
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Where("expires_at > ?", time.Now().UTC().Format(time.RFC3339Nano)).
		Count(ctx)
	if err != nil {
		return AccessCredentialCreated{}, err
	}
	if active >= credentialMaxActivePerUser {
		return AccessCredentialCreated{}, panelerr.Validation("credential_limit_reached", "Too many active access credentials")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return AccessCredentialCreated{}, err
	}
	token := credentialTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256Hex(token)
	now := time.Now().UTC()
	credential := &models.AccessCredential{
		ID:              id.New("acr"),
		UserID:          user.ID,
		Name:            name,
		TokenHash:       hash,
		TokenPrefix:     token[:12],
		Scope:           scope,
		AllowManagement: input.AllowManagement,
		UserGeneration:  user.Generation,
		ExpiresAt:       now.Add(time.Duration(ttl) * time.Second),
		CreatedAt:       now,
	}
	if err := orm.New(s.db).Insert(ctx, credential); err != nil {
		return AccessCredentialCreated{}, err
	}
	return AccessCredentialCreated{AccessCredentialView: accessCredentialView(*credential), Token: token}, nil
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
	if credential.RevokedAt != nil || !now.Before(credential.ExpiresAt) {
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
	if credential.LastUsedAt == nil || now.Sub(*credential.LastUsedAt) > time.Minute {
		_, _ = orm.RawExec(ctx, s.db, `
			UPDATE access_credentials SET last_used_at = ?
			WHERE id = ? AND (last_used_at IS NULL OR last_used_at < ?)
		`, now.Format(time.RFC3339Nano), credential.ID, now.Add(-time.Minute).Format(time.RFC3339Nano))
	}
	return Session{
		UserID: entry.UserID, Username: entry.Username, DisplayName: entry.DisplayName,
		IsBuiltinAdmin: entry.Builtin, Roles: roles, Permissions: permissions,
		ExpiresAt: credential.ExpiresAt, AuthKind: "credential",
		CredentialID: credential.ID, CredentialScope: credential.Scope,
		AllowManagement: credential.AllowManagement,
	}, true
}

func accessCredentialView(row models.AccessCredential) AccessCredentialView {
	status := "active"
	now := time.Now().UTC()
	if row.RevokedAt != nil {
		status = "revoked"
	} else if !now.Before(row.ExpiresAt) {
		status = "expired"
	}
	return AccessCredentialView{
		ID: row.ID, Name: row.Name, TokenPrefix: row.TokenPrefix,
		Scope: row.Scope, AllowManagement: row.AllowManagement, Status: status,
		ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt, LastUsedAt: row.LastUsedAt, CreatedAt: row.CreatedAt,
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
