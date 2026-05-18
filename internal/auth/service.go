package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panel/internal/config"
	"panel/internal/id"
	"panel/internal/panelerr"
)

type Service struct {
	cfg      config.Config
	mu       sync.Mutex
	sessions map[string]Session
}

type Session struct {
	ID        string
	Username  string
	ExpiresAt time.Time
}

func NewService(cfg config.Config) *Service {
	return &Service{cfg: cfg, sessions: map[string]Session{}}
}

func (s *Service) Login(username, password string) (Session, error) {
	if username != s.cfg.AdminUsername {
		return Session{}, panelerr.Unauthorized("Invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(s.cfg.AdminPasswordHash), []byte(password)); err != nil {
		return Session{}, panelerr.Unauthorized("Invalid username or password")
	}
	sess := Session{ID: id.New("sess"), Username: username, ExpiresAt: time.Now().UTC().Add(24 * time.Hour)}
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	return sess, nil
}

func (s *Service) Validate(cookie string) (Session, bool) {
	sessionID, ok := s.verifyCookie(cookie)
	if !ok {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok || time.Now().UTC().After(sess.ExpiresAt) {
		delete(s.sessions, sessionID)
		return Session{}, false
	}
	return sess, true
}

func (s *Service) Logout(cookie string) {
	sessionID, ok := s.verifyCookie(cookie)
	if !ok {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

func (s *Service) CookieValue(sessionID string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.SessionSecret))
	mac.Write([]byte(sessionID))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return sessionID + "." + sig
}

func (s *Service) verifyCookie(cookie string) (string, bool) {
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 || parts[0] == "" {
		return "", false
	}
	expected := s.CookieValue(parts[0])
	return parts[0], hmac.Equal([]byte(cookie), []byte(expected))
}
