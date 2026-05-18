package credential

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"panel/internal/config"
	"panel/internal/id"
	"panel/internal/panelerr"
)

type Service struct {
	db       *sql.DB
	dataRoot string
}

func NewService(db *sql.DB, cfg config.Config) *Service {
	return &Service{db: db, dataRoot: cfg.DataRoot}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (Credential, error) {
	if err := validateSave(req.Name, req.Type, req.Username, req.Password, req.PrivateKey, true); err != nil {
		return Credential{}, err
	}
	now := time.Now().UTC()
	cred := Credential{ID: id.New("cred"), Name: req.Name, Type: req.Type, Username: req.Username, CreatedAt: now, UpdatedAt: now}
	privateKeyPath := ""
	if req.Type == TypePrivateKey {
		keysDir := filepath.Join(s.dataRoot, "keys")
		if err := os.MkdirAll(keysDir, 0700); err != nil {
			return Credential{}, err
		}
		privateKeyPath = filepath.Join(keysDir, cred.ID+".key")
		if err := os.WriteFile(privateKeyPath, []byte(req.PrivateKey), 0600); err != nil {
			return Credential{}, err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO credentials(id,name,type,username,password_secret,private_key_path,passphrase_secret,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		cred.ID, cred.Name, cred.Type, cred.Username, req.Password, privateKeyPath, req.Passphrase, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
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
	privateKeyPath := ""
	if old.Type == TypePrivateKey {
		_ = s.db.QueryRowContext(ctx, `SELECT private_key_path FROM credentials WHERE id=?`, credentialID).Scan(&privateKeyPath)
	}
	if req.Type == TypePassword {
		if req.Password != "" {
			password = req.Password
		}
		passphrase = ""
		if privateKeyPath != "" {
			_ = os.Remove(privateKeyPath)
			privateKeyPath = ""
		}
	} else {
		password = ""
		passphrase = req.Passphrase
		keysDir := filepath.Join(s.dataRoot, "keys")
		if err := os.MkdirAll(keysDir, 0700); err != nil {
			return Credential{}, err
		}
		if privateKeyPath == "" {
			privateKeyPath = filepath.Join(keysDir, credentialID+".key")
		}
		if req.PrivateKey != "" {
			if err := os.WriteFile(privateKeyPath, []byte(req.PrivateKey), 0600); err != nil {
				return Credential{}, err
			}
		}
	}
	res, err := s.db.ExecContext(ctx, `UPDATE credentials SET name=?,type=?,username=?,password_secret=?,private_key_path=?,passphrase_secret=?,updated_at=? WHERE id=?`,
		req.Name, req.Type, req.Username, password, privateKeyPath, passphrase, now.Format(time.RFC3339Nano), credentialID)
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
	var keyPath string
	err := s.db.QueryRowContext(ctx, `SELECT id,type,username,password_secret,private_key_path,passphrase_secret FROM credentials WHERE id=?`, credentialID).
		Scan(&r.ID, &r.Type, &r.Username, &r.Password, &keyPath, &r.Passphrase)
	if err == sql.ErrNoRows {
		return ResolvedCredential{}, panelerr.NotFound("credential")
	}
	if err != nil {
		return ResolvedCredential{}, err
	}
	if r.Type == TypePrivateKey {
		b, err := os.ReadFile(keyPath)
		if err != nil {
			return ResolvedCredential{}, err
		}
		r.PrivateKey = b
	}
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
	var keyPath string
	_ = s.db.QueryRowContext(ctx, `SELECT private_key_path FROM credentials WHERE id=?`, credentialID).Scan(&keyPath)
	res, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE id=?`, credentialID)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return panelerr.NotFound("credential")
	}
	if keyPath != "" {
		_ = os.Remove(keyPath)
	}
	return nil
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
