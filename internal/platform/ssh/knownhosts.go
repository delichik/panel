package sshx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

var (
	// ErrHostKeyMismatch identifies a rejected connection whose host key does
	// not match the key recorded for the same host:port on a previous connection.
	ErrHostKeyMismatch = errors.New("ssh host key mismatch")
	// ErrHostKeyVerification identifies failures reading or persisting the
	// known_hosts store itself; connections fail closed rather than silently
	// downgrading to insecure behavior.
	ErrHostKeyVerification = errors.New("ssh host key verification failed")
)

// knownHostsStore persists verified host public keys in a known_hosts-style
// file (one "<identity> <keytype> <base64>" line per entry) and answers
// trust-on-first-use lookups. It is safe for concurrent use.
type knownHostsStore struct {
	path string
	mu   sync.Mutex
	keys map[string]ssh.PublicKey
}

func newKnownHostsStore(path string) *knownHostsStore {
	return &knownHostsStore{path: path, keys: make(map[string]ssh.PublicKey)}
}

// verify checks the presented host key against the stored key for identity.
// It records and persists the key on first use, returns nil when the stored
// key matches, and returns an error wrapping ErrHostKeyMismatch when a
// previously recorded key no longer matches.
func (s *knownHostsStore) verify(identity string, key ssh.PublicKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return fmt.Errorf("%w: %v", ErrHostKeyVerification, err)
	}
	stored, ok := s.keys[identity]
	if !ok {
		s.keys[identity] = key
		if err := s.saveLocked(); err != nil {
			delete(s.keys, identity)
			return fmt.Errorf("%w: persist host key for %s: %v", ErrHostKeyVerification, identity, err)
		}
		return nil
	}
	if publicKeysEqual(stored, key) {
		return nil
	}
	return fmt.Errorf("%w: host key for %s has changed (stored %s, presented %s)",
		ErrHostKeyMismatch, identity, ssh.FingerprintSHA256(stored), ssh.FingerprintSHA256(key))
}

// Replace overwrites the stored public key for identity with key. It is used
// by the explicit "trust new host key" flow after an administrator confirms
// that a changed key is expected. The write goes through the same locked
// load-then-save path as verify and is atomic (temp file + rename).
func (s *knownHostsStore) Replace(identity string, key ssh.PublicKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return fmt.Errorf("%w: %v", ErrHostKeyVerification, err)
	}
	s.keys[identity] = key
	if err := s.saveLocked(); err != nil {
		delete(s.keys, identity)
		return fmt.Errorf("%w: persist host key for %s: %v", ErrHostKeyVerification, identity, err)
	}
	return nil
}

func (s *knownHostsStore) loadLocked() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read known hosts file %s: %w", s.path, err)
	}
	keys := make(map[string]ssh.PublicKey, 8)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[1:], " ")))
		if err != nil {
			continue
		}
		keys[fields[0]] = pub
	}
	s.keys = keys
	return nil
}

func (s *knownHostsStore) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var b strings.Builder
	for identity, key := range s.keys {
		fmt.Fprintf(&b, "%s %s", identity, string(ssh.MarshalAuthorizedKey(key)))
	}
	tmp, err := os.CreateTemp(dir, ".known_hosts-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func publicKeysEqual(a, b ssh.PublicKey) bool {
	return a != nil && b != nil && a.Type() == b.Type() && bytes.Equal(a.Marshal(), b.Marshal())
}

// defaultKnownHostsPath returns the known_hosts file location used when no
// explicit WithKnownHosts option is provided. It follows PANEL_DATA_ROOT when
// set and otherwise falls back to the default "data" data root.
func defaultKnownHostsPath() string {
	dataRoot := os.Getenv("PANEL_DATA_ROOT")
	if dataRoot == "" {
		dataRoot = "data"
	}
	return filepath.Join(dataRoot, "known_hosts")
}
