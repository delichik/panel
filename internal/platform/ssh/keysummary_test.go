package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSummarizePrivateKeyEd25519(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "deploy@example")
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := SummarizePrivateKey(pem.EncodeToMemory(block), "")
	if !ok {
		t.Fatal("expected key summary")
	}
	if summary.Algorithm != "ED25519" || summary.Bits != 256 {
		t.Fatalf("summary = %#v, want ED25519/256", summary)
	}
	if !strings.HasPrefix(summary.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q, want SHA256 prefix", summary.Fingerprint)
	}
	if summary.Comment != "deploy@example" {
		t.Fatalf("comment = %q, want deploy@example", summary.Comment)
	}
}

func TestSummarizePrivateKeyRSA(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(key, "rsa@example")
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := SummarizePrivateKey(pem.EncodeToMemory(block), "")
	if !ok {
		t.Fatal("expected key summary")
	}
	if summary.Algorithm != "RSA" || summary.Bits != 2048 {
		t.Fatalf("summary = %#v, want RSA/2048", summary)
	}
	if summary.Comment != "rsa@example" {
		t.Fatalf("comment = %q, want rsa@example", summary.Comment)
	}
}

func TestSummarizePrivateKeyEncryptedHidesCommentButKeepsFingerprint(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "secret@example", []byte("passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := SummarizePrivateKey(pem.EncodeToMemory(block), "passphrase")
	if !ok {
		t.Fatal("expected key summary")
	}
	if summary.Fingerprint == "" || summary.Algorithm != "ED25519" {
		t.Fatalf("summary = %#v, want fingerprint and algorithm", summary)
	}
	if summary.Comment != "" {
		t.Fatalf("comment = %q, want empty for encrypted key", summary.Comment)
	}
	if _, ok := SummarizePrivateKey(pem.EncodeToMemory(block), "wrong"); ok {
		t.Fatal("expected parse failure with wrong passphrase")
	}
}

func TestSummarizePrivateKeyRejectsGarbage(t *testing.T) {
	if _, ok := SummarizePrivateKey([]byte("not a key"), ""); ok {
		t.Fatal("expected parse failure")
	}
}
