package credential

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	id "panel/internal/platform/identity"
	"panel/internal/platform/secrets"
)

type Service struct {
	db      *sql.DB
	secrets *secretstore.Store
}

const credentialSecretField = "ssh_credential"

type storedSecret struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

func NewService(db *sql.DB, secrets *secretstore.Store) *Service {
	return &Service{db: db, secrets: secrets}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (Credential, error) {
	if err := validateSave(req.Name, req.Type, req.Username, req.Password, req.PrivateKey, true); err != nil {
		return Credential{}, err
	}
	now := time.Now().UTC()
	cred := Credential{ID: id.New("cred"), Name: req.Name, Type: req.Type, Username: req.Username, CreatedAt: now, UpdatedAt: now}
	ciphertext, err := s.encrypt(cred.ID, cred.Type, storedSecret{
		Password: req.Password, PrivateKey: req.PrivateKey, Passphrase: req.Passphrase,
	})
	if err != nil {
		return Credential{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO credentials(id,name,type,username,secret_ciphertext,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		cred.ID, cred.Name, cred.Type, cred.Username, ciphertext, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return cred, err
}

func (s *Service) List(ctx context.Context) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,type,username,created_at,updated_at FROM credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) Update(ctx context.Context, credentialID string, req UpdateRequest) (Credential, error) {
	if err := validateSave(req.Name, req.Type, req.Username, req.Password, req.PrivateKey, false); err != nil {
		return Credential{}, err
	}
	old, err := s.Resolve(ctx, credentialID)
	if err != nil {
		return Credential{}, err
	}
	if old.Type != req.Type {
		if req.Type == TypePassword && req.Password == "" {
			return Credential{}, panelerr.Validation("credential_password_required", "Password credential requires a password when changing type")
		}
		if req.Type == TypePrivateKey && req.PrivateKey == "" {
			return Credential{}, panelerr.Validation("credential_private_key_required", "Private key credential requires a private key when changing type")
		}
	}
	now := time.Now().UTC()
	password := old.Password
	passphrase := old.Passphrase
	privateKey := string(old.PrivateKey)
	if req.Type == TypePassword {
		if req.Password != "" {
			password = req.Password
		}
		passphrase = ""
		privateKey = ""
	} else {
		password = ""
		passphrase = req.Passphrase
		if req.PrivateKey != "" {
			privateKey = req.PrivateKey
		}
	}
	ciphertext, err := s.encrypt(credentialID, req.Type, storedSecret{
		Password: password, PrivateKey: privateKey, Passphrase: passphrase,
	})
	if err != nil {
		return Credential{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE credentials SET name=?,type=?,username=?,secret_ciphertext=?,password_secret='',private_key_path='',passphrase_secret='',updated_at=? WHERE id=?`,
		req.Name, req.Type, req.Username, ciphertext, now.Format(time.RFC3339Nano), credentialID)
	if err != nil {
		return Credential{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Credential{}, panelerr.NotFound("credential")
	}
	return s.Get(ctx, credentialID)
}

func (s *Service) Get(ctx context.Context, credentialID string) (Credential, error) {
	c, err := scanCredential(s.db.QueryRowContext(ctx, `SELECT id,name,type,username,created_at,updated_at FROM credentials WHERE id=?`, credentialID))
	if err == sql.ErrNoRows {
		return Credential{}, panelerr.NotFound("credential")
	}
	return c, err
}

func (s *Service) Resolve(ctx context.Context, credentialID string) (ResolvedCredential, error) {
	var r ResolvedCredential
	var ciphertext string
	err := s.db.QueryRowContext(ctx, `SELECT id,type,username,secret_ciphertext FROM credentials WHERE id=?`, credentialID).
		Scan(&r.ID, &r.Type, &r.Username, &ciphertext)
	if err == sql.ErrNoRows {
		return ResolvedCredential{}, panelerr.NotFound("credential")
	}
	if err != nil {
		return ResolvedCredential{}, err
	}
	secret, err := s.decrypt(r.ID, r.Type, ciphertext)
	if err != nil {
		return ResolvedCredential{}, err
	}
	r.Password = secret.Password
	r.PrivateKey = []byte(secret.PrivateKey)
	r.Passphrase = secret.Passphrase
	return r, nil
}

func (s *Service) Delete(ctx context.Context, credentialID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE credential_id=?`, credentialID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return panelerr.Conflict("credential_in_use", "Credential is still used by one or more servers")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE id=?`, credentialID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("credential")
	}
	return nil
}

func (s *Service) EnsureLegacySecretsMigrated(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,secret_ciphertext,password_secret,private_key_path,passphrase_secret FROM credentials`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type legacyCredential struct {
		id, typ, ciphertext, password, keyPath, passphrase string
	}
	var credentials []legacyCredential
	for rows.Next() {
		var item legacyCredential
		if err := rows.Scan(&item.id, &item.typ, &item.ciphertext, &item.password, &item.keyPath, &item.passphrase); err != nil {
			return err
		}
		credentials = append(credentials, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range credentials {
		if err := s.migrateLegacySecret(ctx, item.id, item.typ, item.ciphertext, item.password, item.keyPath, item.passphrase); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) migrateLegacySecret(ctx context.Context, credentialID, typ, ciphertext, password, keyPath, passphrase string) error {
	hasLegacy := password != "" || keyPath != "" || passphrase != ""
	if ciphertext == "" {
		if !hasLegacy {
			return panelerr.Validation("credential_secret_missing", "Credential secret is missing")
		}
		var privateKey string
		if keyPath != "" {
			raw, err := os.ReadFile(keyPath)
			if err != nil {
				return err
			}
			privateKey = string(raw)
		}
		var err error
		ciphertext, err = s.encrypt(credentialID, typ, storedSecret{
			Password: password, PrivateKey: privateKey, Passphrase: passphrase,
		})
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE credentials SET secret_ciphertext=?,updated_at=? WHERE id=?`,
			ciphertext, time.Now().UTC().Format(time.RFC3339Nano), credentialID); err != nil {
			return err
		}
	}
	if _, err := s.decrypt(credentialID, typ, ciphertext); err != nil {
		return err
	}
	if keyPath != "" {
		if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if hasLegacy {
		_, err := s.db.ExecContext(ctx, `UPDATE credentials SET password_secret='',private_key_path='',passphrase_secret='',updated_at=? WHERE id=?`,
			time.Now().UTC().Format(time.RFC3339Nano), credentialID)
		return err
	}
	return nil
}

func (s *Service) encrypt(credentialID, typ string, secret storedSecret) (string, error) {
	if s.secrets == nil {
		return "", panelerr.Validation("credential_secret_store_missing", "Credential secret store is unavailable")
	}
	raw, err := json.Marshal(secret)
	if err != nil {
		return "", err
	}
	return s.secrets.Encrypt(credentialID, typ, credentialSecretField, raw)
}

func (s *Service) decrypt(credentialID, typ, ciphertext string) (storedSecret, error) {
	if s.secrets == nil {
		return storedSecret{}, panelerr.Validation("credential_secret_store_missing", "Credential secret store is unavailable")
	}
	raw, err := s.secrets.Decrypt(credentialID, typ, credentialSecretField, ciphertext)
	if err != nil {
		return storedSecret{}, err
	}
	var secret storedSecret
	if err := json.Unmarshal(raw, &secret); err != nil {
		return storedSecret{}, panelerr.Validation("credential_secret_invalid", "Credential secret is invalid")
	}
	return secret, nil
}

func validateSave(name, typ, username, password, privateKey string, requireSecret bool) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(username) == "" {
		return panelerr.Validation("credential_invalid", "Credential name and username are required")
	}
	if typ != TypePassword && typ != TypePrivateKey {
		return panelerr.Validation("credential_type_invalid", "Credential type must be password or private_key")
	}
	if requireSecret && typ == TypePassword && password == "" {
		return panelerr.Validation("credential_password_required", "Password credential requires a password")
	}
	if requireSecret && typ == TypePrivateKey && privateKey == "" {
		return panelerr.Validation("credential_private_key_required", "Private key credential requires a private key")
	}
	return nil
}

type credScanner interface{ Scan(dest ...any) error }

func scanCredential(row credScanner) (Credential, error) {
	var c Credential
	var created, updated string
	if err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Username, &created, &updated); err != nil {
		return Credential{}, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, nil
}
