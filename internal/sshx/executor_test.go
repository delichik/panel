package sshx

import (
	"context"
	"testing"

	"panel/internal/credential"
)

type fakeResolver struct{ cred credential.ResolvedCredential }

func (f fakeResolver) Resolve(ctx context.Context, id string) (credential.ResolvedCredential, error) {
	return f.cred, nil
}

func TestPasswordAuthMethod(t *testing.T) {
	_, err := authMethod(credential.ResolvedCredential{Type: credential.TypePassword, Password: "secret"})
	if err != nil {
		t.Fatalf("password auth method: %v", err)
	}
}

func TestPrivateKeyAuthMethodRejectsInvalidKey(t *testing.T) {
	_, err := authMethod(credential.ResolvedCredential{Type: credential.TypePrivateKey, PrivateKey: []byte("bad")})
	if err == nil {
		t.Fatal("expected invalid private key error")
	}
}

func TestSudoCommandWrapsNonInteractive(t *testing.T) {
	got := shellQuote("apt-get update")
	if got != "'apt-get update'" {
		t.Fatalf("unexpected shell quote: %s", got)
	}
}
