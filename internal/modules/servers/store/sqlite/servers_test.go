package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"panel/internal/modules/servers/domain"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
)

func TestServerRepositoryPersistsCRUDAndJSONFields(t *testing.T) {
	repo, db, closeStore := newServerRepositoryTestStore(t)
	defer closeStore()
	ctx := context.Background()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	first := domain.Server{
		ID:           "srv-alpha",
		Name:         "Alpha",
		Host:         "10.0.0.10",
		Port:         22,
		SSHUsername:  "root",
		CredentialID: "cred-1",
		DockerHost:   "unix:///var/run/docker.sock",
		Traits: map[string]string{
			"agent.status": "compatible",
			"sys.cpu":      "Ryzen",
		},
		Variables: map[string]string{
			"APP_ENV": "prod",
			"PORT":    "8080",
		},
		Notes:     "first server",
		CreatedAt: created,
		UpdatedAt: created,
	}
	second := first
	second.ID = "srv-beta"
	second.Name = "Beta"
	second.Host = "10.0.0.11"
	second.CreatedAt = created.Add(time.Minute)
	second.UpdatedAt = second.CreatedAt

	if err := repo.Insert(ctx, first); err != nil {
		t.Fatalf("insert first server: %v", err)
	}
	if err := repo.Insert(ctx, second); err != nil {
		t.Fatalf("insert second server: %v", err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if _, ok := serverByID(list, "srv-alpha"); !ok {
		t.Fatalf("list did not include srv-alpha: %#v", list)
	}
	if _, ok := serverByID(list, "srv-beta"); !ok {
		t.Fatalf("list did not include srv-beta: %#v", list)
	}

	ids, err := repo.ListIDs(ctx)
	if err != nil {
		t.Fatalf("list server ids: %v", err)
	}
	if !sameStringSet(ids, []string{"srv-alpha", "srv-beta"}) {
		t.Fatalf("ids = %#v, want srv-alpha and srv-beta", ids)
	}

	got, err := repo.Get(ctx, "srv-alpha")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	assertServerCore(t, got, first)
	if got.Privilege.Mode != "none" || got.Privilege.Privileged {
		t.Fatalf("default privilege = %#v, want mode none and not privileged", got.Privilege)
	}

	updated := got
	updated.Name = "Alpha Renamed"
	updated.Host = "10.0.0.20"
	updated.Port = 2222
	updated.SSHUsername = "deployer"
	updated.DockerHost = "tcp://127.0.0.1:2375"
	updated.Traits = map[string]string{
		"agent.status": "unavailable",
		"custom.role":  "edge",
	}
	updated.Variables = map[string]string{
		"APP_ENV": "staging",
		"SECRET":  "from-variable-store",
	}
	updated.Notes = "updated notes"
	updated.UpdatedAt = created.Add(2 * time.Hour)
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("update server: %v", err)
	}
	got, err = repo.Get(ctx, "srv-alpha")
	if err != nil {
		t.Fatalf("get updated server: %v", err)
	}
	assertServerCore(t, got, updated)

	if err := repo.Delete(ctx, "srv-beta"); err != nil {
		t.Fatalf("delete server: %v", err)
	}
	if _, err := repo.Get(ctx, "srv-beta"); !isPanelNotFound(err) {
		t.Fatalf("get deleted server error = %v, want not found", err)
	}
	if err := repo.Delete(ctx, "srv-beta"); !isPanelNotFound(err) {
		t.Fatalf("delete missing server error = %v, want not found", err)
	}

	list, err = repo.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != "srv-alpha" {
		t.Fatalf("list after delete = %#v, want only srv-alpha", list)
	}

	var rawTraits, rawVariables string
	if err := db.QueryRowContext(ctx, `SELECT traits,variables_json FROM servers WHERE id=?`, "srv-alpha").Scan(&rawTraits, &rawVariables); err != nil {
		t.Fatalf("read persisted json columns: %v", err)
	}
	if rawTraits == "" || rawTraits == "{}" || rawVariables == "" || rawVariables == "{}" {
		t.Fatalf("expected non-empty persisted traits and variables, traits=%q variables=%q", rawTraits, rawVariables)
	}
}

func TestServerRepositorySummariesExposeAgentURL(t *testing.T) {
	repo, db, closeStore := newServerRepositoryTestStore(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	traits := `{"agent.enabled":"true","agent.url":"https://10.0.0.1:9786","agent.status":"compatible","sys.ufw_supported":"true","sys.ufw_installed":"false","custom.role":"edge"}`
	if _, err := db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"srv-agent", "Agent Host", "10.0.0.1", 22, "root", "cred-1", "unix:///var/run/docker.sock", traits, "{}", now, now); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	items, err := repo.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("summary count = %d, want 1", len(items))
	}
	if items[0].CredentialID != "cred-1" {
		t.Fatalf("summary credentialId = %q, want cred-1", items[0].CredentialID)
	}
	got := items[0].Traits
	if got["agent.enabled"] != "true" || got["agent.url"] != "https://10.0.0.1:9786" || got["agent.status"] != "compatible" || got["sys.ufw_supported"] != "true" {
		t.Fatalf("summary traits = %#v, want agent traits and ufw flags", got)
	}
	if _, ok := got["custom.role"]; ok {
		t.Fatalf("summary traits = %#v, want unrelated traits omitted", got)
	}

	page, err := repo.ListSummaryPage(ctx, 1, 50, "")
	if err != nil {
		t.Fatalf("list summary page: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("summary page = %#v, want one item", page)
	}
	if page.Items[0].CredentialID != "cred-1" {
		t.Fatalf("summary page credentialId = %q, want cred-1", page.Items[0].CredentialID)
	}
	got = page.Items[0].Traits
	if got["agent.url"] != "https://10.0.0.1:9786" || got["agent.status"] != "compatible" {
		t.Fatalf("summary page traits = %#v, want agent.url and agent.status", got)
	}
}

func TestServerRepositoryPrivilegeCompatibilityAndNotFound(t *testing.T) {
	repo, db, closeStore := newServerRepositoryTestStore(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC).Format(time.RFC3339Nano)

	rows := []struct {
		id              string
		mode            string
		wantPrivileged  bool
		wantDefaultMode bool
	}{
		{id: "srv-root", mode: "root", wantPrivileged: true},
		{id: "srv-sudo", mode: "passwordless_sudo", wantPrivileged: true},
		{id: "srv-none", mode: "none", wantPrivileged: false},
		{id: "srv-legacy-empty", mode: "", wantPrivileged: false, wantDefaultMode: true},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,privilege_mode,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, row.id, "127.0.0.1", 22, "root", "cred-1", "unix:///var/run/docker.sock", "{}", "{}", row.mode, now, now); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	for _, row := range rows {
		got, err := repo.Get(ctx, row.id)
		if err != nil {
			t.Fatalf("get %s: %v", row.id, err)
		}
		wantMode := row.mode
		if row.wantDefaultMode {
			wantMode = "none"
		}
		if got.Privilege.Mode != wantMode || got.Privilege.Privileged != row.wantPrivileged {
			t.Fatalf("%s privilege = %#v, want mode %q privileged %v", row.id, got.Privilege, wantMode, row.wantPrivileged)
		}
	}

	if _, err := repo.Get(ctx, "missing"); !isPanelNotFound(err) {
		t.Fatalf("get missing error = %v, want not found", err)
	}
	missingUpdate := domain.Server{ID: "missing", UpdatedAt: time.Now().UTC()}
	if err := repo.Update(ctx, missingUpdate); !isPanelNotFound(err) {
		t.Fatalf("update missing error = %v, want not found", err)
	}
}

func newServerRepositoryTestStore(t *testing.T) (*ServerRepository, *sql.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		"cred-1", "credential", "password", "root", now, now); err != nil {
		_ = store.Close()
		t.Fatalf("insert credential: %v", err)
	}
	return NewServerRepository(store.AppDB()), store.AppDB(), func() { _ = store.Close() }
}

func assertServerCore(t *testing.T, got, want domain.Server) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || got.Host != want.Host || got.Port != want.Port ||
		got.SSHUsername != want.SSHUsername || got.CredentialID != want.CredentialID ||
		got.DockerHost != want.DockerHost || got.Notes != want.Notes {
		t.Fatalf("server core = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(got.Traits, want.Traits) {
		t.Fatalf("traits = %#v, want %#v", got.Traits, want.Traits)
	}
	if !reflect.DeepEqual(got.Variables, want.Variables) {
		t.Fatalf("variables = %#v, want %#v", got.Variables, want.Variables)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("timestamps = %s/%s, want %s/%s", got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

func serverByID(items []domain.Server, id string) (domain.Server, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return domain.Server{}, false
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := map[string]int{}
	for _, value := range got {
		counts[value]++
	}
	for _, value := range want {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func isPanelNotFound(err error) bool {
	var panel *panelerr.Error
	return errors.As(err, &panel) && panel.HTTPStatus == 404
}

func TestServerRepositorySummariesNormalizeLegacyPrivilege(t *testing.T) {
	repo, db, closeStore := newServerRepositoryTestStore(t)
	defer closeStore()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Legacy row: privilege_mode=none persisted alongside sudo_passwordless=1.
	if _, err := db.ExecContext(ctx, `INSERT INTO servers(id,name,host,port,ssh_username,credential_id,docker_host,traits,variables_json,reachable,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"srv-legacy-sudo", "Legacy", "127.0.0.1", 22, "root", "cred-1", "unix:///var/run/docker.sock", "{}", "{}", 1, 1, "none", now, now); err != nil {
		t.Fatalf("insert legacy server: %v", err)
	}

	items, err := repo.ListSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("summary count = %d, want 1", len(items))
	}
	if items[0].Privilege.Mode != "passwordless_sudo" || !items[0].Privilege.Privileged {
		t.Fatalf("legacy summary privilege = %#v, want passwordless_sudo and privileged", items[0].Privilege)
	}

	page, err := repo.ListSummaryPage(ctx, 1, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("summary page = %#v, want one item", page)
	}
	if page.Items[0].Privilege.Mode != "passwordless_sudo" || !page.Items[0].Privilege.Privileged {
		t.Fatalf("legacy summary page privilege = %#v, want passwordless_sudo and privileged", page.Items[0].Privilege)
	}
}