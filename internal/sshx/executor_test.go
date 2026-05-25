package sshx

import (
	"bytes"
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

func TestStreamWriterEmitsLinesAndFlushesPartialLine(t *testing.T) {
	var buf bytes.Buffer
	var lines []string
	writer := newStreamWriter(&buf, func(line string) {
		lines = append(lines, line)
	})

	if _, err := writer.Write([]byte("one\ntwo\rthree")); err != nil {
		t.Fatal(err)
	}
	writer.Flush()

	if buf.String() != "one\ntwo\rthree" {
		t.Fatalf("unexpected buffered output: %q", buf.String())
	}
	want := []string{"one", "two", "three"}
	if len(lines) != len(want) {
		t.Fatalf("expected lines %#v, got %#v", want, lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}
