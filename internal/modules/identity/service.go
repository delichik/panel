package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panel/internal/modules/settings"
	"panel/internal/platform/config"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

type Service struct {
	cfg     config.Config
	db      *sql.DB
	runtime *settings.Service
	limiter *loginRateLimiter
}

type Session struct {
	Username               string
	Token                  string
	ExpiresAt              time.Time
	PasswordChangeRequired bool
}

type AccountUpdate struct {
	CurrentPassword string `json:"currentPassword"`
	Username        string `json:"username"`
	NewPassword     string `json:"newPassword"`
}

const (
	tokenNonceStateKey = "token_nonce"
	tokenSubject       = "panel-admin"
	adminAccountID     = "admin"
	minPasswordLength  = 8
)

func NewService(db *sql.DB, cfg config.Config, runtime *settings.Service) (*Service, error) {
	s := &Service{cfg: cfg, db: db, runtime: runtime, limiter: newLoginRateLimiter()}
	if err := s.ensureAdminAccount(context.Background()); err != nil {
		return nil, err
	}
	if err := s.ensureTokenNonce(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
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
	account, err := s.currentAdminAccount(ctx)
	if err != nil {
		return Session{}, err
	}
	passwordMatches := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)) == nil
	if username != account.Username || !passwordMatches {
		s.limiter.recordFailure(key, time.Now().UTC())
		return Session{}, panelerr.Unauthorized("Authentication failed")
	}
	s.limiter.recordSuccess(key)
	return s.issueSession(ctx, account)
}

func (s *Service) Logout(ctx context.Context) error {
	return s.rotateTokenNonce(ctx)
}

func (s *Service) Validate(ctx context.Context, token string) (Session, bool) {
	claims, ok := s.verifyJWT(token)
	if !ok || claims.Subject != tokenSubject || claims.TokenNonce == "" || tokenExpired(claims.ExpiresAt) {
		return Session{}, false
	}
	nonce, err := s.currentTokenNonce(ctx)
	if err != nil || nonce != claims.TokenNonce {
		return Session{}, false
	}
	account, err := s.currentAdminAccount(ctx)
	if err != nil {
		return Session{}, false
	}
	var expiresAt time.Time
	if claims.ExpiresAt > 0 {
		expiresAt = time.Unix(claims.ExpiresAt, 0).UTC()
	}
	return Session{Username: account.Username, Token: token, ExpiresAt: expiresAt, PasswordChangeRequired: account.PasswordChangeRequired}, true
}

func (s *Service) UpdateAccount(ctx context.Context, input AccountUpdate) (Session, error) {
	account, err := s.currentAdminAccount(ctx)
	if err != nil {
		return Session{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(input.CurrentPassword)) != nil {
		return Session{}, panelerr.Unauthorized("Authentication failed")
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return Session{}, panelerr.Validation("admin_username_required", "Username is required")
	}
	passwordChanging := input.NewPassword != ""
	if account.PasswordChangeRequired && !passwordChanging {
		return Session{}, panelerr.Validation("admin_password_change_required", "Password must be changed before continuing")
	}
	nextHash := account.PasswordHash
	if passwordChanging {
		if len(input.NewPassword) < minPasswordLength {
			return Session{}, panelerr.Validation("admin_password_too_short", "Password must be at least 8 characters")
		}
		if bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(input.NewPassword)) == nil {
			return Session{}, panelerr.Validation("admin_password_unchanged", "New password must be different from the current password")
		}
		rawHash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			return Session{}, err
		}
		nextHash = string(rawHash)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := orm.New(s.db).From("auth_accounts").Where("id = ?", adminAccountID).UpdateColumns(ctx, map[string]any{
		"username": username, "password_hash": nextHash, "password_change_required": 0, "updated_at": now,
	}); err != nil {
		return Session{}, err
	}
	if passwordChanging {
		secret, err := randomJWTSecret()
		if err != nil {
			return Session{}, err
		}
		if _, err := s.runtime.SetJWTSecret(ctx, secret); err != nil {
			return Session{}, err
		}
		if err := s.rotateTokenNonce(ctx); err != nil {
			return Session{}, err
		}
	}
	account.Username = username
	account.PasswordHash = nextHash
	account.PasswordChangeRequired = false
	return s.issueSession(ctx, account)
}

func (s *Service) UpdateJWTSecret(ctx context.Context, secret string) (Session, error) {
	if _, err := s.runtime.SetJWTSecret(ctx, secret); err != nil {
		return Session{}, err
	}
	if err := s.rotateTokenNonce(ctx); err != nil {
		return Session{}, err
	}
	account, err := s.currentAdminAccount(ctx)
	if err != nil {
		return Session{}, err
	}
	return s.issueSession(ctx, account)
}

func (s *Service) issueSession(ctx context.Context, account adminAccount) (Session, error) {
	nonce, err := s.currentTokenNonce(ctx)
	if err != nil {
		return Session{}, err
	}
	expiresAt, err := s.nextExpiresAt(ctx)
	if err != nil {
		return Session{}, err
	}
	token, err := s.signJWT(tokenSubject, nonce, expiresAt)
	if err != nil {
		return Session{}, err
	}
	return Session{Username: account.Username, Token: token, ExpiresAt: expiresAt, PasswordChangeRequired: account.PasswordChangeRequired}, nil
}

func (s *Service) signJWT(username, nonce string, expiresAt time.Time) (string, error) {
	header, err := encodeJWTPart(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims := jwtClaims{Subject: username, TokenNonce: nonce}
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

func (s *Service) ensureAdminAccount(ctx context.Context) error {
	exists, err := orm.New(s.db).From("auth_accounts").Where("id = ?", adminAccountID).Count(ctx)
	if err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	username := strings.TrimSpace(s.cfg.AdminUsername)
	if username == "" {
		username = "admin"
	}
	passwordHash := strings.TrimSpace(s.cfg.AdminPasswordHash)
	if passwordHash == "" {
		rawHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		passwordHash = string(rawHash)
	}
	now := time.Now().UTC()
	err = orm.New(s.db).Insert(ctx, &models.AuthAccount{
		ID: adminAccountID, Username: username, PasswordHash: passwordHash,
		PasswordChangeRequired: true, CreatedAt: now, UpdatedAt: now,
	})
	return err
}

func (s *Service) currentAdminAccount(ctx context.Context) (adminAccount, error) {
	var account models.AuthAccount
	err := orm.New(s.db).From("auth_accounts").Where("id = ?", adminAccountID).First(ctx, &account)
	if err != nil {
		return adminAccount{}, err
	}
	return adminAccount{
		Username:               account.Username,
		PasswordHash:           account.PasswordHash,
		PasswordChangeRequired: account.PasswordChangeRequired,
	}, nil
}

func (s *Service) ensureTokenNonce(ctx context.Context) error {
	nonce, err := s.currentTokenNonce(ctx)
	if err == nil && nonce != "" {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	return s.rotateTokenNonce(ctx)
}

func (s *Service) currentTokenNonce(ctx context.Context) (string, error) {
	var nonce string
	err := orm.New(s.db).From("auth_state").Where("key = ?", tokenNonceStateKey).Select("value").ScanValue(ctx, &nonce)
	return nonce, err
}

func (s *Service) rotateTokenNonce(ctx context.Context) error {
	nonce, err := randomTokenNonce()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = orm.RawExec(ctx, s.db, `
		INSERT INTO auth_state(key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, tokenNonceStateKey, nonce, now)
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
		if err == sql.ErrNoRows {
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

func randomTokenNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomJWTSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func encodeJWTPart(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type adminAccount struct {
	Username               string
	PasswordHash           string
	PasswordChangeRequired bool
}

type jwtClaims struct {
	Subject    string `json:"sub"`
	TokenNonce string `json:"nonce"`
	ExpiresAt  int64  `json:"exp,omitempty"`
}
