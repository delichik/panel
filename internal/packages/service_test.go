package packages

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/config"
	"panel/internal/credential"
	"panel/internal/linux"
	"panel/internal/server"
	"panel/internal/storage"
	"panel/internal/tasks"
)

func TestPackageServiceBlocksUnsupportedServer(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	credSvc := credential.NewService(store.AppDB(), cfg)
	cred, _ := credSvc.Create(ctx, credential.CreateRequest{Name: "c", Type: credential.TypePassword, Username: "du", Password: "secret"})
	taskSvc := tasks.NewService(store.AppDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	srv, _ := serverSvc.Create(ctx, server.SaveRequest{Name: "s", Host: "h", Port: 22, SSHUsername: "du", CredentialID: cred.ID})
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	if _, err := svc.Refresh(ctx, srv.ID); err == nil {
		t.Fatal("expected unsupported server to be blocked")
	}
}

func TestReplaceUpdates(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AppDB().Exec(`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), nil, nil, nil)
	if err := svc.replaceUpdates(context.Background(), "srv", []linux.PackageUpdate{{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2"}}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Updates) != 1 || list.LastRefreshedAt == nil {
		t.Fatalf("unexpected list: %#v", list)
	}
}
