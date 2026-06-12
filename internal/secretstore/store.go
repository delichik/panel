package secretstore

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"

	"panel/internal/config"
	"panel/internal/panelerr"
)

const (
	MasterKeyEnvVar = "PANEL_KEY_ASSETS_MASTER_KEY"
	envelopeVersion = 1
	envelopeAlg     = "xchacha20poly1305"
)

type Store struct {
	masterKey [32]byte
}

type envelope struct {
	Version    int    `json:"v"`
	Algorithm  string `json:"alg"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func Open(cfg config.Config, db *sql.DB) (*Store, error) {
	filePath := MasterKeyPath(cfg.DataRoot)
	fileKey, fileExists, err := loadFileKey(filePath)
	if err != nil {
		return nil, err
	}
	envKey, envExists, err := loadEnvKey()
	if err != nil {
		return nil, err
	}
	if envExists && fileExists && envKey != fileKey {
		return nil, panelerr.Validation("key_asset_master_key_invalid", "Key asset master key from environment does not match the on-disk master key")
	}
	switch {
	case envExists:
		return &Store{masterKey: envKey}, nil
	case fileExists:
		return &Store{masterKey: fileKey}, nil
	}
	hasEncrypted, err := hasEncryptedAssets(db)
	if err != nil {
		return nil, err
	}
	if hasEncrypted {
		return nil, panelerr.Validation("key_asset_master_key_missing", "Encrypted secrets exist but the master key is missing")
	}
	generated, err := generateKey()
	if err != nil {
		return nil, err
	}
	if err := persistFileKey(filePath, generated); err != nil {
		return nil, err
	}
	return &Store{masterKey: generated}, nil
}

func MasterKeyPath(dataRoot string) string {
	return filepath.Join(dataRoot, "secrets", "key-assets-master.key")
}

func (s *Store) Encrypt(assetID, assetType, field string, plaintext []byte) (string, error) {
	aead, err := chacha20poly1305.NewX(s.masterKey[:])
	if err != nil {
		return "", err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	blob := aead.Seal(nil, nonce, plaintext, associatedData(assetID, assetType, field))
	env := envelope{
		Version:    envelopeVersion,
		Algorithm:  envelopeAlg,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(blob),
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Store) Decrypt(assetID, assetType, field, wrapped string) ([]byte, error) {
	var env envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(wrapped)), &env); err != nil {
		return nil, panelerr.Validation("key_asset_master_key_invalid", "Key asset ciphertext is invalid")
	}
	if env.Version != envelopeVersion || env.Algorithm != envelopeAlg {
		return nil, panelerr.Validation("key_asset_master_key_invalid", "Key asset ciphertext uses an unsupported encryption format")
	}
	nonce, err := base64.StdEncoding.DecodeString(env.Nonce)
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, panelerr.Validation("key_asset_master_key_invalid", "Key asset ciphertext nonce is invalid")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, panelerr.Validation("key_asset_master_key_invalid", "Key asset ciphertext is invalid")
	}
	aead, err := chacha20poly1305.NewX(s.masterKey[:])
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, associatedData(assetID, assetType, field))
	if err != nil {
		return nil, panelerr.Validation("key_asset_master_key_invalid", "Key asset master key is invalid for the stored ciphertext")
	}
	return plaintext, nil
}

func associatedData(assetID, assetType, field string) []byte {
	return []byte(assetID + "\x00" + assetType + "\x00" + field)
}

func loadEnvKey() ([32]byte, bool, error) {
	value := strings.TrimSpace(os.Getenv(MasterKeyEnvVar))
	if value == "" {
		return [32]byte{}, false, nil
	}
	key, err := decodeMasterKey(value)
	if err != nil {
		return [32]byte{}, false, err
	}
	return key, true, nil
}

func loadFileKey(path string) ([32]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return [32]byte{}, false, nil
		}
		return [32]byte{}, false, err
	}
	key, err := decodeMasterKey(strings.TrimSpace(string(raw)))
	if err != nil {
		return [32]byte{}, false, err
	}
	return key, true, nil
}

func decodeMasterKey(value string) ([32]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) != 32 {
		return [32]byte{}, panelerr.Validation("key_asset_master_key_invalid", "Key asset master key must be a base64-encoded 32-byte value")
	}
	var key [32]byte
	copy(key[:], raw)
	return key, nil
}

func generateKey() ([32]byte, error) {
	var key [32]byte
	_, err := rand.Read(key[:])
	return key, err
}

func persistFileKey(path string, key [32]byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key[:])+"\n"), 0o600)
}

func hasEncryptedAssets(db *sql.DB) (bool, error) {
	if db == nil {
		return false, nil
	}
	keyAssetsEncrypted, err := encryptedValuesExist(db, "key_assets", "private_key_ciphertext")
	if err != nil {
		return false, err
	}
	if keyAssetsEncrypted {
		return true, nil
	}
	return encryptedValuesExist(db, "dns_domains", "provider_secret_ciphertext")
}

func encryptedValuesExist(db *sql.DB, table, column string) (bool, error) {
	var columnCount int
	query := `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`
	if err := db.QueryRow(query, table, column).Scan(&columnCount); err != nil {
		return false, err
	}
	if columnCount == 0 {
		return false, nil
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE TRIM(COALESCE(` + column + `, '')) <> ''`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
