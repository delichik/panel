package overview

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	metricspkg "panel/internal/modules/observability/metrics"
	serverpkg "panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
)

func TestGetCardsInitializesAndPersistsDefaults(t *testing.T) {
	svc, closeStore := newCardTestService(t)
	defer closeStore()

	first, err := svc.GetCards(context.Background())
	if err != nil {
		t.Fatalf("get cards: %v", err)
	}
	if len(first.Cards) != 6 {
		t.Fatalf("default card count = %d, want 6", len(first.Cards))
	}
	if first.Cards[0].Kind != CardKindCPU || first.Cards[2].Range != "6h" {
		t.Fatalf("unexpected defaults: %#v", first.Cards)
	}

	second, err := svc.GetCards(context.Background())
	if err != nil {
		t.Fatalf("get persisted cards: %v", err)
	}
	if len(second.Cards) != len(first.Cards) || second.Cards[0].ID != first.Cards[0].ID {
		t.Fatalf("defaults were not persisted: first=%#v second=%#v", first, second)
	}
}

func TestUpdateCardsRoundTripsAndPreservesEmptyLayout(t *testing.T) {
	svc, closeStore := newCardTestService(t)
	defer closeStore()

	input := CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-custom",
		Kind:             CardKindNetwork,
		Width:            4,
		Height:           3,
		Range:            "7d",
		NetworkDirection: "rx",
		ServerIDs:        []string{"srv_1", "srv_2"},
	}}}
	if _, err := svc.UpdateCards(context.Background(), input); err != nil {
		t.Fatalf("update cards: %v", err)
	}
	got, err := svc.GetCards(context.Background())
	if err != nil {
		t.Fatalf("get cards: %v", err)
	}
	if len(got.Cards) != 1 || got.Cards[0].ID != "card-custom" || got.Cards[0].ServerIDs[1] != "srv_2" {
		t.Fatalf("unexpected cards: %#v", got.Cards)
	}

	if _, err := svc.UpdateCards(context.Background(), CardConfigurationSet{Cards: []CardConfiguration{}}); err != nil {
		t.Fatalf("clear cards: %v", err)
	}
	got, err = svc.GetCards(context.Background())
	if err != nil {
		t.Fatalf("get empty cards: %v", err)
	}
	if got.Cards == nil || len(got.Cards) != 0 {
		t.Fatalf("empty layout was not preserved: %#v", got.Cards)
	}
}

func TestUpdateCardsRejectsInvalidInput(t *testing.T) {
	svc, closeStore := newCardTestService(t)
	defer closeStore()

	tests := []CardConfigurationSet{
		{Cards: []CardConfiguration{{ID: "", Kind: CardKindCPU, Width: 3, Height: 2, Range: "1h", NetworkDirection: "both", ServerIDs: []string{}}}},
		{Cards: []CardConfiguration{{ID: "x", Kind: "unknown", Width: 3, Height: 2, Range: "1h", NetworkDirection: "both", ServerIDs: []string{}}}},
		{Cards: []CardConfiguration{{ID: "x", Kind: CardKindCPU, Width: 7, Height: 2, Range: "1h", NetworkDirection: "both", ServerIDs: []string{}}}},
		{Cards: []CardConfiguration{{ID: "x", Kind: CardKindCPU, Width: 3, Height: 2, Range: "30d", NetworkDirection: "both", ServerIDs: []string{}}}},
		{Cards: []CardConfiguration{{ID: "x", Kind: CardKindCPU, Width: 3, Height: 2, Range: "1h", NetworkDirection: "invalid", ServerIDs: []string{}}}},
	}
	for i, input := range tests {
		if _, err := svc.UpdateCards(context.Background(), input); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}

func TestGetCardDataReturnsMetricsForConfiguredServers(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()

	input := CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-cpu",
		Kind:             CardKindCPU,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{serverIDs[0]},
	}}}
	if _, err := svc.UpdateCards(context.Background(), input); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	got, err := svc.GetCardData(context.Background(), "card-cpu")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if got.Card.ID != "card-cpu" {
		t.Fatalf("card id = %q, want card-cpu", got.Card.ID)
	}
	if len(got.MetricsByServer) != 1 {
		t.Fatalf("metrics server count = %d, want 1: %#v", len(got.MetricsByServer), got.MetricsByServer)
	}
	series := got.MetricsByServer[serverIDs[0]]
	if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 42 {
		t.Fatalf("unexpected cpu series: %#v", series.CPU)
	}
	if _, ok := got.MetricsByServer[serverIDs[1]]; ok {
		t.Fatalf("unselected server should not be included")
	}
}

func TestGetCardDataExpandsEmptyServerSelection(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()

	input := CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-memory",
		Kind:             CardKindMemory,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{},
	}}}
	if _, err := svc.UpdateCards(context.Background(), input); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	got, err := svc.GetCardData(context.Background(), "card-memory")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if len(got.MetricsByServer) != len(serverIDs) {
		t.Fatalf("metrics server count = %d, want %d", len(got.MetricsByServer), len(serverIDs))
	}
	for _, serverID := range serverIDs {
		if _, ok := got.MetricsByServer[serverID]; !ok {
			t.Fatalf("server %s missing from card data", serverID)
		}
	}
}

func TestGetCardDataSinceReturnsOnlyNewerPoints(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	if _, err := svc.UpdateCards(context.Background(), CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-cpu",
		Kind:             CardKindCPU,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	// 使用整秒时间，避免 since 按秒截断后与采样点边界竞态。
	marker := time.Now().UTC().Truncate(time.Second).Add(time.Second)
	for i, serverID := range serverIDs {
		if err := svc.metrics.Save(context.Background(), linux.MetricsSnapshot{
			ServerID:        serverID,
			Time:            marker,
			CPUUsagePercent: 99 + float64(i),
		}); err != nil {
			t.Fatalf("save newer metrics: %v", err)
		}
	}

	since := marker.Add(-500 * time.Millisecond)
	got, err := svc.GetCardDataSince(context.Background(), "card-cpu", &since)
	if err != nil {
		t.Fatalf("get card data since: %v", err)
	}
	for i, serverID := range serverIDs {
		series := got.MetricsByServer[serverID]
		if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 99+float64(i) {
			t.Fatalf("server %s delta series = %#v, want only the newer point", serverID, series.CPU)
		}
	}

	all, err := svc.GetCardData(context.Background(), "card-cpu")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if len(all.MetricsByServer[serverIDs[0]].CPU) != 2 {
		t.Fatalf("full series length = %d, want 2", len(all.MetricsByServer[serverIDs[0]].CPU))
	}
}
func TestGetCardDataRejectsUnknownCard(t *testing.T) {
	svc, _, closeStore := newCardDataTestService(t)
	defer closeStore()

	if _, err := svc.GetCardData(context.Background(), "missing"); err == nil {
		t.Fatalf("expected missing card error")
	}
}

func newCardTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return NewService(store.AppDB(), nil, nil, nil), func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}
}

func newCardDataTestService(t *testing.T) (*Service, []string, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	now := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if _, err := store.AppDB().ExecContext(context.Background(), `
		INSERT INTO credentials(id, name, type, username, created_at, updated_at)
		VALUES('cred-test', 'Test', 'password', 'root', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert credential: %v", err)
	}
	serverSvc := serverpkg.NewService(store.AppDB(), nil, tasks.NewService(store.LogDB()), serverpkg.WithMetricsDB(store.MetricsDB()))
	first, err := serverSvc.Create(context.Background(), serverpkg.SaveRequest{Name: "Alpha", IPv4: "10.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred-test"})
	if err != nil {
		t.Fatalf("create first server: %v", err)
	}
	second, err := serverSvc.Create(context.Background(), serverpkg.SaveRequest{Name: "Beta", IPv4: "10.0.0.2", Port: 22, SSHUsername: "root", CredentialID: "cred-test"})
	if err != nil {
		t.Fatalf("create second server: %v", err)
	}
	metricsSvc := metricspkg.NewService(store.MetricsDB(), serverSvc, nil)
	for i, serverID := range []string{first.ID, second.ID} {
		if err := metricsSvc.Save(context.Background(), linux.MetricsSnapshot{
			ServerID:           serverID,
			Time:               time.Now().UTC().Add(-time.Duration(i) * time.Minute),
			CPUUsagePercent:    42 + float64(i),
			MemoryUsedBytes:    10,
			MemoryTotalBytes:   20,
			DiskUsedBytes:      30,
			DiskTotalBytes:     40,
			NetworkRxBytesRate: 50,
			NetworkTxBytesRate: 60,
		}); err != nil {
			t.Fatalf("save metrics: %v", err)
		}
	}
	return NewService(store.AppDB(), serverSvc, metricsSvc, nil), []string{first.ID, second.ID}, func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}
}
