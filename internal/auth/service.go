package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panel/internal/config"
	"panel/internal/panelerr"
	"panel/internal/settings"
)

type Service struct {
	cfg config.Config
	db  *sql.DB
}

type Session struct {
	Username  string
	Token     string
	ExpiresAt time.Time
}

const (
	tokenNonceStateKey = "token_nonce"
	tokenSubject       = "panel-admin"
)

func NewService(db *sql.DB, cfg config.Config) (*Service, error) {
	s := &Service{cfg: cfg, db: db}
	if err := s.ensureTokenNonce(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	passwordMatches := bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPasswordHash), []byte(password)) == nil
	if username != s.cfg.AdminUsername || !passwordMatches {
		return Session{}, panelerr.Unauthorized("Authentication failed")
	}
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
	return Session{Username: username, Token: token, ExpiresAt: expiresAt}, nil
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
	var expiresAt time.Time
	if claims.ExpiresAt > 0 {
		expiresAt = time.Unix(claims.ExpiresAt, 0).UTC()
	}
	return Session{Username: s.cfg.AdminUsername, Token: token, ExpiresAt: expiresAt}, true
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
	mac := hmac.New(sha256.New, []byte(s.cfg.JWTSecret))
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
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
	err := s.db.QueryRowContext(ctx, `SELECT value FROM auth_state WHERE key=?`, tokenNonceStateKey).Scan(&nonce)
	return nonce, err
}

func (s *Service) rotateTokenNonce(ctx context.Context) error {
	nonce, err := randomTokenNonce()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
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
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key=?`, settings.RuntimeSettingTokenExpiration).Scan(&value)
	if err == sql.ErrNoRows {
		value = settings.DefaultTokenExpiration
	} else if err != nil {
		return 0, err
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

func encodeJWTPart(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type jwtClaims struct {
	Subject    string `json:"sub"`
	TokenNonce string `json:"nonce"`
	ExpiresAt  int64  `json:"exp,omitempty"`
}
