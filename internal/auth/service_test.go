package auth

import (
	"testing"

	"panel/internal/config"
)

func TestLoginValidate(t *testing.T) {
	svc := NewService(config.Default())
	sess, err := svc.Login("admin", "admin")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if sess.Token == "" {
		t.Fatal("expected jwt token")
	}
	if _, ok := svc.Validate(sess.Token); !ok {
		t.Fatal("token should validate")
	}
}

func TestValidateRejectsInvalidToken(t *testing.T) {
	svc := NewService(config.Default())
	if _, ok := svc.Validate("not-a-token"); ok {
		t.Fatal("invalid token should not validate")
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	svc := NewService(config.Default())
	if _, err := svc.Login("admin", "wrong"); err == nil {
		t.Fatal("expected unauthorized")
	}
}
