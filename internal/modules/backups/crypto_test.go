package backups

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestReadBytesRejectsSizeOverLimit(t *testing.T) {
	var buf bytes.Buffer
	if err := writeBytes(&buf, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := readBytes(bytes.NewReader(buf.Bytes()), 4); err == nil {
		t.Fatal("expected size-over-limit error")
	} else if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected readable limit error, got %v", err)
	}
}

func TestDecryptBytesRejectsOversizedSegmentBeforeAllocation(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(encryptedMagic)
	if err := writeBytes(&buf, make([]byte, 16)); err != nil {
		t.Fatal(err)
	}
	if err := writeBytes(&buf, make([]byte, 12)); err != nil {
		t.Fatal(err)
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], math.MaxUint32)
	buf.Write(size[:])
	buf.WriteString("tiny")

	// A header claiming a ~4 GiB ciphertext segment with only a few bytes
	// remaining must be rejected before any large allocation is attempted.
	if _, err := decryptBytes(buf.Bytes(), "password"); err == nil {
		t.Fatal("expected oversized ciphertext segment to be rejected")
	} else if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected readable limit error, got %v", err)
	}
}

type failWriter struct{}

func (failWriter) Write(p []byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteBytesPropagatesWriteErrors(t *testing.T) {
	if err := writeBytes(failWriter{}, []byte("x")); err == nil {
		t.Fatal("expected write error to propagate")
	}
}
