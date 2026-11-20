package server

import (
	"context"
	"testing"
)

// TestDeleteServerCleansMetricsFacilityReferences covers FAC-MET-007: deleting a
// server prunes it from the selected metrics collection node list, bumps the
// facility version, and the existing metrics DB cleanup still removes its
// historical rows (OBS-MET-006).
func TestDeleteServerCleansMetricsFacilityReferences(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	svc.SetMetricsDB(store.MetricsDB())
	ctx := context.Background()
	removed, err := svc.Create(ctx, SaveRequest{Name: "removed", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := svc.Create(ctx, SaveRequest{Name: "kept", IPv4: "127.0.0.2", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at)
VALUES('metrics','selected',?,7,60,'daily',1,'2026-01-01T00:00:00Z')`,
		`["`+removed.ID+`","`+kept.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	// 历史指标行在独立 MetricsDB；删除服务器后仍必须被清理。
	for _, serverID := range []string{removed.ID, kept.ID} {
		if _, err := store.MetricsDB().Exec(`INSERT INTO metrics_snapshots(server_id,time,cpu_usage_percent,memory_used_bytes,memory_total_bytes,disk_used_bytes,disk_total_bytes,network_rx_bps,network_tx_bps)
VALUES(?,?,0,0,0,0,0,0,0)`, serverID, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.Delete(ctx, removed.ID); err != nil {
		t.Fatal(err)
	}

	var rawIDs string
	var version int
	if err := store.AppDB().QueryRow(`SELECT server_ids_json,version FROM metrics_facility_configs WHERE id='metrics'`).Scan(&rawIDs, &version); err != nil {
		t.Fatal(err)
	}
	if rawIDs != `["`+kept.ID+`"]` {
		t.Fatalf("expected the metrics collection node list to be pruned, got %s", rawIDs)
	}
	if version != 2 {
		t.Fatalf("expected the metrics facility version to increment, got %d", version)
	}
	var removedRows int
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, removed.ID).Scan(&removedRows); err != nil {
		t.Fatal(err)
	}
	if removedRows != 0 {
		t.Fatalf("metrics rows for the deleted server must be removed, got %d", removedRows)
	}
	var keptRows int
	if err := store.MetricsDB().QueryRow(`SELECT COUNT(*) FROM metrics_snapshots WHERE server_id=?`, kept.ID).Scan(&keptRows); err != nil {
		t.Fatal(err)
	}
	if keptRows != 1 {
		t.Fatalf("metrics rows for the kept server must survive, got %d", keptRows)
	}
}

// TestDeleteServerLeavesMetricsFacilityAllModeAlone verifies that all-mode
// configs are not rewritten and that a config which does not list the deleted
// server is left untouched (no version bump).
func TestDeleteServerLeavesMetricsFacilityAllModeAlone(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	ctx := context.Background()
	removed, err := svc.Create(ctx, SaveRequest{Name: "removed", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := svc.Create(ctx, SaveRequest{Name: "kept", IPv4: "127.0.0.2", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at)
VALUES('metrics','all','[]',7,60,'daily',3,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, removed.ID); err != nil {
		t.Fatal(err)
	}

	var mode, rawIDs string
	var version int
	if err := store.AppDB().QueryRow(`SELECT mode,server_ids_json,version FROM metrics_facility_configs WHERE id='metrics'`).Scan(&mode, &rawIDs, &version); err != nil {
		t.Fatal(err)
	}
	if mode != "all" || rawIDs != "[]" || version != 3 {
		t.Fatalf("all-mode metrics config must stay unchanged: mode=%s ids=%s version=%d", mode, rawIDs, version)
	}

	// selected 但列表里没有该节点的配置同样是幂等的：不递增版本。
	if _, err := store.AppDB().Exec(`UPDATE metrics_facility_configs SET mode='selected',server_ids_json=?,version=5 WHERE id='metrics'`, `["`+kept.ID+`"]`); err != nil {
		t.Fatal(err)
	}
	other, err := svc.Create(ctx, SaveRequest{Name: "other", IPv4: "127.0.0.3", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AppDB().QueryRow(`SELECT server_ids_json,version FROM metrics_facility_configs WHERE id='metrics'`).Scan(&rawIDs, &version); err != nil {
		t.Fatal(err)
	}
	if rawIDs != `["`+kept.ID+`"]` || version != 5 {
		t.Fatalf("idempotent prune must not bump version: ids=%s version=%d", rawIDs, version)
	}
}

// TestDeleteServerFailsWhenMetricsFacilityPruneFails verifies the prune runs in
// the same transaction as the server delete: an unreadable config aborts the
// whole delete instead of leaving a dangling server ID behind.
func TestDeleteServerFailsWhenMetricsFacilityPruneFails(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	ctx := context.Background()
	removed, err := svc.Create(ctx, SaveRequest{Name: "removed", IPv4: "127.0.0.1", Port: 22, SSHUsername: "du", CredentialID: "cred_1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO metrics_facility_configs(id,mode,server_ids_json,retention_days,collection_interval_seconds,cleanup_schedule,version,updated_at)
VALUES('metrics','selected','not-json',7,60,'daily',1,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, removed.ID); err == nil {
		t.Fatal("expected the delete to fail when the metrics facility config cannot be parsed")
	}

	var count int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM servers WHERE id=?`, removed.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("server delete must roll back together with the failed metrics facility prune")
	}
}
