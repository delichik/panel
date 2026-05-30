package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panel/internal/config"
	"panel/internal/panelerr"
)

type Service struct {
	cfg config.Config
}

type Session struct {
	Username  string
	Token     string
	ExpiresAt time.Time
}

func NewService(cfg config.Config) *Service {
	return &Service{cfg: cfg}
}

func (s *Service) Login(username, password string) (Session, error) {
	if username != s.cfg.AdminUsername {
		return Session{}, panelerr.Unauthorized("Invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPasswordHash), []byte(password)); err != nil {
		return Session{}, panelerr.Unauthorized("Invalid username or password")
	}
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	token, err := s.signJWT(username, expiresAt)
	if err != nil {
		return Session{}, err
	}
	return Session{Username: username, Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) Validate(token string) (Session, bool) {
	claims, ok := s.verifyJWT(token)
	if !ok || claims.Subject != s.cfg.AdminUsername || time.Now().UTC().After(time.Unix(claims.ExpiresAt, 0)) {
		return Session{}, false
	}
	return Session{Username: claims.Subject, Token: token, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC()}, true
}

func (s *Service) signJWT(username string, expiresAt time.Time) (string, error) {
	header, err := encodeJWTPart(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := encodeJWTPart(jwtClaims{Subject: username, ExpiresAt: expiresAt.Unix()})
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

func encodeJWTPart(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type jwtClaims struct {
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
}
