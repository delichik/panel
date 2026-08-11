package backups

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"golang.org/x/crypto/argon2"
)

var encryptedMagic = []byte("PANEL-BACKUP-AESGCM-1\n")

// maxEncryptedSegmentBytes is the largest length-prefixed segment the backup
// format can represent (the prefix is a uint32). readBytes uses it together
// with the remaining input size so a crafted header cannot force a huge
// allocation from a small file.
const maxEncryptedSegmentBytes = uint64(math.MaxUint32)

type encryptedHeader struct {
	Salt  []byte
	Nonce []byte
}

func encryptBytes(plain []byte, password string) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password is required")
	}
	header := encryptedHeader{
		Salt:  make([]byte, 16),
		Nonce: make([]byte, 12),
	}
	if _, err := rand.Read(header.Salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(header.Nonce); err != nil {
		return nil, err
	}
	aead, err := backupAEAD(password, header.Salt)
	if err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, header.Nonce, plain, encryptedMagic)
	var out bytes.Buffer
	out.Write(encryptedMagic)
	if err := writeBytes(&out, header.Salt); err != nil {
		return nil, err
	}
	if err := writeBytes(&out, header.Nonce); err != nil {
		return nil, err
	}
	if err := writeBytes(&out, ciphertext); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decryptBytes(raw []byte, password string) ([]byte, error) {
	if !bytes.HasPrefix(raw, encryptedMagic) {
		return raw, nil
	}
	if password == "" {
		return nil, errPasswordRequired
	}
	reader := bytes.NewReader(raw[len(encryptedMagic):])
	salt, err := readBytes(reader, uint64(reader.Len()))
	if err != nil {
		return nil, err
	}
	nonce, err := readBytes(reader, uint64(reader.Len()))
	if err != nil {
		return nil, err
	}
	ciphertext, err := readBytes(reader, uint64(reader.Len()))
	if err != nil {
		return nil, err
	}
	aead, err := backupAEAD(password, salt)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, ciphertext, encryptedMagic)
	if err != nil {
		return nil, errPasswordInvalid
	}
	return plain, nil
}

func isEncryptedBackup(raw []byte) bool {
	return bytes.HasPrefix(raw, encryptedMagic)
}

func backupAEAD(password string, salt []byte) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// writeBytes writes a uint32 length prefix followed by the bytes. It returns
// an error instead of truncating the length when b exceeds the uint32 range.
func writeBytes(w io.Writer, b []byte) error {
	if uint64(len(b)) > maxEncryptedSegmentBytes {
		return fmt.Errorf("backup encrypted segment too large: %d bytes exceeds uint32 length prefix", len(b))
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(b))); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

// readBytes reads a uint32 length prefix and returns the segment. max bounds
// the allocation: a malformed header claiming a huge size is rejected before
// memory is allocated, instead of attempting a multi-gigabyte make.
func readBytes(r io.Reader, max uint64) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	if uint64(size) > max {
		return nil, fmt.Errorf("backup encrypted segment size %d exceeds limit %d", size, max)
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}
