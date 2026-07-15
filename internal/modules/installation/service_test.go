package installation

import (
	"context"
	"testing"

	"panel/internal/platform/config"
	"panel/internal/platform/database"
)

func TestSetHostServerIsSingleton(t *testing.T) {
	cfg := config.Default()
	cfg.DataRoot = t.TempDir()
	cfg.AppDatabase = cfg.DataRoot + "/app.db"
	cfg.LogDatabase = cfg.DataRoot + "/log.db"
	cfg.MetricsDatabase = cfg.DataRoot + "/metrics.db"
	store, err := database.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, id := range []string{"srv-a", "srv-b"} {
		if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,'password','root','now','now')`, "cred-"+id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppDB().ExecContext(ctx, `INSERT INTO servers(id,name,host,port,credential_id,created_at,updated_at) VALUES(?,?,?,22,?,'now','now')`, id, id, "127.0.0.1", "cred-"+id); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(store.AppDB())
	state, err := svc.SetHostServer(ctx, "srv-a")
	if err != nil || state.HostServerID != "srv-a" {
		t.Fatalf("SetHostServer() = %#v, %v", state, err)
	}
	if _, err := svc.SetHostServer(ctx, "srv-b"); err == nil {
		t.Fatal("SetHostServer() should reject replacing the singleton host")
	}
}
