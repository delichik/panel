package credential

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
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
	err = orm.New(s.db).Insert(ctx, &models.Credential{
		ID: cred.ID, Name: cred.Name, Type: cred.Type, Username: cred.Username,
		SecretCiphertext: ciphertext, CreatedAt: now, UpdatedAt: now,
	})
	return cred, err
}

func (s *Service) List(ctx context.Context) ([]Credential, error) {
	var rows []models.Credential
	if err := orm.New(s.db).From("credentials").OrderBy("created_at DESC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]Credential, 0, len(rows))
	for i := range rows {
		out = append(out, toDomainCredential(rows[i]))
	}
	return out, nil
}

func (s *Service) ListPage(ctx context.Context, page, pageSize int, query string) (httpx.ListPage[Credential], error) {
	q := orm.New(s.db).From("credentials")
	if query != "" {
		term := orm.LikeEscaped(query)
		q.AndLike("name", term).OrLike("username", term)
	}
	total, err := q.Count(ctx)
	if err != nil {
		return httpx.ListPage[Credential]{}, err
	}
	var rows []models.Credential
	if err := q.OrderBy("created_at DESC", "id ASC").Limit(pageSize).Offset((page-1)*pageSize).All(ctx, &rows); err != nil {
		return httpx.ListPage[Credential]{}, err
	}
	items := make([]Credential, 0, len(rows))
	for i := range rows {
		items = append(items, toDomainCredential(rows[i]))
	}
	return httpx.ListPage[Credential]{Items: items, Total: int(total), Page: page, PageSize: pageSize}, nil
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
	res, err := orm.RawExec(ctx, s.db, `UPDATE credentials SET name=?,type=?,username=?,secret_ciphertext=?,password_secret='',private_key_path='',passphrase_secret='',updated_at=? WHERE id=?`,
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
	var row models.Credential
	err := orm.New(s.db).From("credentials").Where("id=?", credentialID).First(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, panelerr.NotFound("credential")
	}
	if err != nil {
		return Credential{}, err
	}
	return toDomainCredential(row), nil
}

func (s *Service) Resolve(ctx context.Context, credentialID string) (ResolvedCredential, error) {
	var row models.Credential
	err := orm.New(s.db).From("credentials").Where("id=?", credentialID).First(ctx, &row)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedCredential{}, panelerr.NotFound("credential")
	}
	if err != nil {
		return ResolvedCredential{}, err
	}
	secret, err := s.decrypt(row.ID, row.Type, row.SecretCiphertext)
	if err != nil {
		return ResolvedCredential{}, err
	}
	r := ResolvedCredential{ID: row.ID, Type: row.Type, Username: row.Username}
	r.Password = secret.Password
	r.PrivateKey = []byte(secret.PrivateKey)
	r.Passphrase = secret.Passphrase
	return r, nil
}

func (s *Service) Delete(ctx context.Context, credentialID string) error {
	count, err := orm.New(s.db).From("servers").Where("credential_id=?", credentialID).Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return panelerr.Conflict("credential_in_use", "Credential is still used by one or more servers")
	}
	res, err := orm.RawExec(ctx, s.db, `DELETE FROM credentials WHERE id=?`, credentialID)
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
	var rows []models.Credential
	if err := orm.New(s.db).From("credentials").All(ctx, &rows); err != nil {
		return err
	}
	for _, item := range rows {
		if err := s.migrateLegacySecret(ctx, item.ID, item.Type, item.SecretCiphertext, item.PasswordSecret, item.PrivateKeyPath, item.PassphraseSecret); err != nil {
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
		if err := orm.New(s.db).From("credentials").Where("id=?", credentialID).UpdateColumns(ctx, map[string]any{
			"secret_ciphertext": ciphertext,
			"updated_at":        time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
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
		return orm.New(s.db).From("credentials").Where("id=?", credentialID).UpdateColumns(ctx, map[string]any{
			"password_secret":   "",
			"private_key_path":  "",
			"passphrase_secret": "",
			"updated_at":        time.Now().UTC().Format(time.RFC3339Nano),
		})
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

func toDomainCredential(m models.Credential) Credential {
	return Credential{ID: m.ID, Name: m.Name, Type: m.Type, Username: m.Username, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}
