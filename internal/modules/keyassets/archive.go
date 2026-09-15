package keyassets

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"

	panelerr "panel/internal/platform/errors"
)

const (
	archiveVersion = 1
	archiveAlg     = "xchacha20poly1305"
	archiveKDF     = "argon2id"
)

type archiveHeader struct {
	Version     int    `json:"version"`
	Algorithm   string `json:"algorithm"`
	KDF         string `json:"kdf"`
	Time        uint32 `json:"time"`
	MemoryKiB   uint32 `json:"memoryKiB"`
	Parallelism uint8  `json:"parallelism"`
	Salt        string `json:"salt"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
}

type archivePayload struct {
	Assets []archiveAsset `json:"assets"`
}

type archiveAsset struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Name           string         `json:"name"`
	ParentAssetID  string         `json:"parentAssetId,omitempty"`
	Algorithm      string         `json:"algorithm"`
	KeySize        int            `json:"keySize,omitempty"`
	CommonName     string         `json:"commonName,omitempty"`
	DNSNames       []string       `json:"dnsNames,omitempty"`
	IPAddresses    []string       `json:"ipAddresses,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CertificatePEM string         `json:"certificatePem,omitempty"`
	PrivateKeyPEM  string         `json:"privateKeyPem"`
	PublicKey      string         `json:"publicKey,omitempty"`
}

func encryptArchive(password string, payload archivePayload) ([]byte, error) {
	if len(password) < 12 {
		return nil, panelerr.Validation("key_asset_archive_password_invalid", "Archive password must be at least 12 characters")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	header := archiveHeader{
		Version:     archiveVersion,
		Algorithm:   archiveAlg,
		KDF:         archiveKDF,
		Time:        3,
		MemoryKiB:   64 * 1024,
		Parallelism: 4,
	}
	salt := make([]byte, 16)
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, header.Time, header.MemoryKiB, header.Parallelism, 32)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, raw, []byte("panel-key-assets"))
	header.Salt = base64.StdEncoding.EncodeToString(salt)
	header.Nonce = base64.StdEncoding.EncodeToString(nonce)
	header.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	return json.Marshal(header)
}

func decryptArchive(archiveBytes []byte, password string) (archivePayload, error) {
	var header archiveHeader
	if err := json.Unmarshal(archiveBytes, &header); err != nil {
		return archivePayload{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive is invalid")
	}
	if header.Version != archiveVersion {
		return archivePayload{}, panelerr.Validation("key_asset_archive_version_unsupported", "Key asset archive version is unsupported")
	}
	if header.Algorithm != archiveAlg {
		return archivePayload{}, panelerr.Validation("key_asset_archive_version_unsupported", "Key asset archive algorithm is unsupported")
	}
	if header.KDF != archiveKDF {
		return archivePayload{}, panelerr.Validation("key_asset_archive_kdf_invalid", "Key asset archive KDF is unsupported")
	}
	if header.Time == 0 || header.Time > 10 || header.MemoryKiB == 0 || header.MemoryKiB > 256*1024 || header.Parallelism == 0 || header.Parallelism > 8 {
		return archivePayload{}, panelerr.Validation("key_asset_archive_kdf_invalid", "Key asset archive KDF parameters are invalid")
	}
	salt, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header.Salt))
	if err != nil || len(salt) != 16 {
		return archivePayload{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive salt is invalid")
	}
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header.Nonce))
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return archivePayload{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive nonce is invalid")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header.Ciphertext))
	if err != nil {
		return archivePayload{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive ciphertext is invalid")
	}
	key := argon2.IDKey([]byte(password), salt, header.Time, header.MemoryKiB, header.Parallelism, 32)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return archivePayload{}, err
	}
	raw, err := aead.Open(nil, nonce, ciphertext, []byte("panel-key-assets"))
	if err != nil {
		return archivePayload{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive password is invalid or the archive was tampered with")
	}
	var payload archivePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return archivePayload{}, panelerr.Validation("key_asset_archive_tampered", "Key asset archive payload is invalid")
	}
	return payload, nil
}
