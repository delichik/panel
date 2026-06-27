package backups

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/argon2"
)

var encryptedMagic = []byte("PANEL-BACKUP-AESGCM-1\n")

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
	writeBytes(&out, header.Salt)
	writeBytes(&out, header.Nonce)
	writeBytes(&out, ciphertext)
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
	salt, err := readBytes(reader)
	if err != nil {
		return nil, err
	}
	nonce, err := readBytes(reader)
	if err != nil {
		return nil, err
	}
	ciphertext, err := readBytes(reader)
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

func writeBytes(w io.Writer, b []byte) {
	_ = binary.Write(w, binary.BigEndian, uint32(len(b)))
	_, _ = w.Write(b)
}

func readBytes(r io.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}
