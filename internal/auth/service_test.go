package auth

import (
	"testing"

	"panel/internal/config"
)

func TestLoginValidateLogout(t *testing.T) {
	svc := NewService(config.Default())
	sess, err := svc.Login("admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	cookie := svc.CookieValue(sess.ID)
	if _, ok := svc.Validate(cookie); !ok {
		t.Fatal("session should validate")
	}
	svc.Logout(cookie)
	if _, ok := svc.Validate(cookie); ok {
		t.Fatal("session should be invalid after logout")
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	svc := NewService(config.Default())
	if _, err := svc.Login("admin", "wrong"); err == nil {
		t.Fatal("expected unauthorized")
	}
}
