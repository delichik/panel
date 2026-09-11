package certs

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/acme"
)

func TestEnsureACMEAccountRecoversExistingAccount(t *testing.T) {
	want := &acme.Account{URI: "https://acme.example/acct/1"}
	client := &fakeACMEAccountClient{
		registerErr: acme.ErrAccountAlreadyExists,
		account:     want,
	}

	got, err := ensureACMEAccount(context.Background(), client, &acme.Account{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want || client.registerCalls != 1 || client.getRegCalls != 1 {
		t.Fatalf("account=%#v registerCalls=%d getRegCalls=%d", got, client.registerCalls, client.getRegCalls)
	}
}

func TestEnsureACMEAccountReturnsRegistrationFailure(t *testing.T) {
	wantErr := errors.New("registration unavailable")
	client := &fakeACMEAccountClient{registerErr: wantErr}

	if _, err := ensureACMEAccount(context.Background(), client, &acme.Account{}); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v want=%v", err, wantErr)
	}
	if client.getRegCalls != 0 {
		t.Fatalf("unexpected GetReg calls: %d", client.getRegCalls)
	}
}

type fakeACMEAccountClient struct {
	account       *acme.Account
	registerErr   error
	getRegErr     error
	registerCalls int
	getRegCalls   int
}

func (f *fakeACMEAccountClient) Register(context.Context, *acme.Account, func(string) bool) (*acme.Account, error) {
	f.registerCalls++
	return f.account, f.registerErr
}

func (f *fakeACMEAccountClient) GetReg(context.Context, string) (*acme.Account, error) {
	f.getRegCalls++
	return f.account, f.getRegErr
}
