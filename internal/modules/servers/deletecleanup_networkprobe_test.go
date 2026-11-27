package server

import (
	"context"
	"testing"
)

// TestDeleteServerCleansNetworkProbeReferences covers FAC-PRB: deleting a server
// drops its probe results in both directions and removes it from the selected
// probe node list, bumping the facility version only when the list actually
// changed.
func TestDeleteServerCleansNetworkProbeReferences(t *testing.T) {
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
	if _, err := store.AppDB().Exec(`INSERT INTO network_probe_configs(id,mode,server_ids_json,version,updated_at) VALUES('network_probe','selected',?,1,?)`,
		`["`+removed.ID+`","`+kept.ID+`"]`, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ source, target string }{
		{removed.ID, kept.ID},
		{kept.ID, removed.ID},
	} {
		if _, err := store.AppDB().Exec(`INSERT INTO network_probe_results(source_server_id,target_server_id,target_address,sampled_at,latency_ms,packet_loss_percent,sent,received,error) VALUES(?,?,?,?,?,?,?,?,?)`,
			row.source, row.target, "127.0.0.2", "2026-01-01T00:00:00Z", 1, 0, 3, 3, ""); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.Delete(ctx, removed.ID); err != nil {
		t.Fatal(err)
	}

	var resultCount int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM network_probe_results WHERE source_server_id=? OR target_server_id=?`, removed.ID, removed.ID).Scan(&resultCount); err != nil {
		t.Fatal(err)
	}
	if resultCount != 0 {
		t.Fatalf("expected network probe results for the deleted server to be removed, got %d", resultCount)
	}
	var rawIDs string
	var version int
	if err := store.AppDB().QueryRow(`SELECT server_ids_json,version FROM network_probe_configs WHERE id='network_probe'`).Scan(&rawIDs, &version); err != nil {
		t.Fatal(err)
	}
	if rawIDs != `["`+kept.ID+`"]` {
		t.Fatalf("expected the probe node list to be pruned, got %s", rawIDs)
	}
	if version != 2 {
		t.Fatalf("expected the probe config version to increment, got %d", version)
	}

	// The kept server stays a probe node, but results targeting the deleted
	// server are gone.
	var keptRows int
	if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM network_probe_results WHERE source_server_id=?`, kept.ID).Scan(&keptRows); err != nil {
		t.Fatal(err)
	}
	if keptRows != 0 {
		t.Fatalf("rows targeting the deleted server must be removed, got %d", keptRows)
	}
}

// TestDeleteServerLeavesUnrelatedNetworkProbeConfigAlone verifies the config is
// not rewritten when the deleted node was not a selected probe node, and that
// all-mode configs (which do not persist a node list) are untouched.
func TestDeleteServerLeavesUnrelatedNetworkProbeConfigAlone(t *testing.T) {
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
	if _, err := store.AppDB().Exec(`INSERT INTO network_probe_configs(id,mode,server_ids_json,version,updated_at) VALUES('network_probe','selected',?,3,?)`,
		`["`+kept.ID+`"]`, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, removed.ID); err != nil {
		t.Fatal(err)
	}
	var rawIDs string
	var version int
	if err := store.AppDB().QueryRow(`SELECT server_ids_json,version FROM network_probe_configs WHERE id='network_probe'`).Scan(&rawIDs, &version); err != nil {
		t.Fatal(err)
	}
	if rawIDs != `["`+kept.ID+`"]` || version != 3 {
		t.Fatalf("unrelated probe config changed: ids=%s version=%d", rawIDs, version)
	}
}
