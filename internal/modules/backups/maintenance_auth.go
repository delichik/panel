package backups

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const maintenanceSessionTTL = 2 * time.Hour

type maintenanceAuthContext string

const (
	maintenanceAuthExport  maintenanceAuthContext = "backup_export"
	maintenanceAuthRestore maintenanceAuthContext = "restore"
)

type maintenanceAuth struct {
	username     string
	passwordHash string
	mu           sync.RWMutex
	sessions     map[string]maintenanceSession
	context      maintenanceAuthContext
	now          func() time.Time
}

type maintenanceSession struct {
	username  string
	expiresAt time.Time
}

func readMaintenanceCredential(ctx context.Context, appDatabase string) (maintenanceCredential, error) {
	if strings.TrimSpace(appDatabase) == "" {
		return maintenanceCredential{}, errors.New("app database path is empty")
	}
	db, err := sql.Open("sqlite", sqliteFileDSN(appDatabase))
	if err != nil {
		return maintenanceCredential{}, err
	}
	defer db.Close()

	var username string
	var passwordHash string
	if err := db.QueryRowContext(ctx, `
		SELECT username, password_hash
		FROM auth_accounts
		WHERE id=?
	`, "admin").Scan(&username, &passwordHash); err != nil {
		return maintenanceCredential{}, err
	}
	return maintenanceCredential{Username: username, PasswordHash: passwordHash}, nil
}

func newMaintenanceAuth(ctx context.Context, authContext maintenanceAuthContext, appDatabase string, fallbacks ...maintenanceCredential) (*maintenanceAuth, error) {
	credential, err := readMaintenanceCredential(ctx, appDatabase)
	if err != nil || !validMaintenanceCredential(credential) {
		for _, fallback := range fallbacks {
			if trustedMaintenanceFallback(fallback) {
				credential = fallback
				err = nil
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if !validMaintenanceCredential(credential) {
		return nil, errors.New("maintenance credential is invalid")
	}
	return newMaintenanceAuthWithCredential(authContext, credential), nil
}

func trustedMaintenanceFallback(credential maintenanceCredential) bool {
	if !validMaintenanceCredential(credential) {
		return false
	}
	return credential.Username != "admin" || bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte("admin")) != nil
}

func newMaintenanceAuthWithCredential(authContext maintenanceAuthContext, credential maintenanceCredential) *maintenanceAuth {
	return &maintenanceAuth{
		username:     credential.Username,
		passwordHash: credential.PasswordHash,
		sessions:     make(map[string]maintenanceSession),
		context:      authContext,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func validMaintenanceCredential(credential maintenanceCredential) bool {
	if strings.TrimSpace(credential.Username) == "" || strings.TrimSpace(credential.PasswordHash) == "" {
		return false
	}
	_, err := bcrypt.Cost([]byte(credential.PasswordHash))
	return err == nil
}

func (a *maintenanceAuth) loginAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Username != a.username || bcrypt.CompareHashAndPassword([]byte(a.passwordHash), []byte(req.Password)) != nil {
		httpx.Error(w, panelerr.Unauthorized("Authentication failed"))
		return
	}
	token, err := randomMaintenanceToken(a.context)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	session := maintenanceSession{username: a.username, expiresAt: a.now().Add(maintenanceSessionTTL)}
	a.mu.Lock()
	a.sessions[token] = session
	a.mu.Unlock()
	httpx.JSON(w, http.StatusOK, a.sessionPayload(token, session))
}

func (a *maintenanceAuth) logoutAPI(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token != "" {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
}

func (a *maintenanceAuth) sessionAPI(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	session, ok := a.validate(token)
	if !ok {
		httpx.JSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	httpx.JSON(w, http.StatusOK, a.sessionPayload(token, session))
}

func (a *maintenanceAuth) require(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.validate(bearerToken(r)); !ok {
			httpx.Error(w, panelerr.Unauthorized("Authentication failed"))
			return
		}
		next(w, r)
	}
}

func (a *maintenanceAuth) validate(token string) (maintenanceSession, bool) {
	if token == "" {
		return maintenanceSession{}, false
	}
	a.mu.RLock()
	session, ok := a.sessions[token]
	a.mu.RUnlock()
	if !ok {
		return maintenanceSession{}, false
	}
	if !strings.HasPrefix(token, maintenanceTokenPrefix(a.context)) || !a.now().Before(session.expiresAt) {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return maintenanceSession{}, false
	}
	return session, true
}

func (a *maintenanceAuth) sessionPayload(token string, session maintenanceSession) map[string]any {
	return map[string]any{
		"authenticated":          true,
		"token":                  token,
		"username":               session.username,
		"passwordChangeRequired": false,
		"authContext":            string(a.context),
		"expiresAt":              session.expiresAt,
	}
}

func bearerToken(r *http.Request) string {
	const bearerPrefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(bearerPrefix) || header[:len(bearerPrefix)] != bearerPrefix {
		return ""
	}
	return header[len(bearerPrefix):]
}

func randomMaintenanceToken(authContext maintenanceAuthContext) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return maintenanceTokenPrefix(authContext) + base64.RawURLEncoding.EncodeToString(b), nil
}

func maintenanceTokenPrefix(authContext maintenanceAuthContext) string {
	if authContext == maintenanceAuthRestore {
		return "mr_"
	}
	return "me_"
}

func sqliteFileDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
}

func redirectMaintenanceRoot(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/" {
		return false
	}
	target := "/maintenance/backup"
	if strings.TrimSpace(r.URL.RawQuery) != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	return true
}

func maintenanceAPINotFound(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	httpx.Error(w, panelerr.NotFound("api route"))
	return true
}
