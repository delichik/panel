package overview

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"panel/internal/modules/networkprobe"
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
	if len(series.Memory) != 0 || len(series.DiskIO) != 0 || len(series.Network) != 0 || len(series.Load) != 0 || series.DiskCurrent != nil {
		t.Fatalf("metric card must only materialize its own series: %#v", series)
	}
	if got.BucketSeconds != 10 {
		t.Fatalf("bucket seconds = %d, want 10 for one server on 1h", got.BucketSeconds)
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

func TestGetCardDataSinceRecomputesOpenBucket(t *testing.T) {
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

	// 使用整秒、且晚于种子数据至少一个桶的时间，保证增量只覆盖 marker 所在桶。
	marker := time.Now().UTC().Truncate(time.Second).Add(30 * time.Second)
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
	bucketStart := time.Unix(marker.Unix()/int64(got.BucketSeconds)*int64(got.BucketSeconds), 0).UTC()
	for i, serverID := range serverIDs {
		series := got.MetricsByServer[serverID]
		if len(series.CPU) != 1 || series.CPU[0].UsagePercent != 99+float64(i) {
			t.Fatalf("server %s delta series = %#v, want the recomputed open bucket", serverID, series.CPU)
		}
		if !series.CPU[0].Time.Equal(bucketStart) {
			t.Fatalf("server %s bucket time = %s, want %s", serverID, series.CPU[0].Time, bucketStart)
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

func TestGetCardDataAveragesSamplesWithinOneBucket(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	if _, err := svc.UpdateCards(context.Background(), CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-cpu",
		Kind:             CardKindCPU,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{serverIDs[0]},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	base := time.Now().UTC().Truncate(time.Second).Add(-30 * time.Minute)
	bucketStart := time.Unix(base.Unix()/10*10, 0).UTC()
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: serverIDs[0], Time: bucketStart, CPUUsagePercent: 10},
		{ServerID: serverIDs[0], Time: bucketStart.Add(4 * time.Second), CPUUsagePercent: 30},
	} {
		if err := svc.metrics.Save(context.Background(), snap); err != nil {
			t.Fatalf("save metrics: %v", err)
		}
	}

	got, err := svc.GetCardData(context.Background(), "card-cpu")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if got.BucketSeconds != 10 {
		t.Fatalf("bucket seconds = %d, want 10", got.BucketSeconds)
	}
	for _, point := range got.MetricsByServer[serverIDs[0]].CPU {
		if point.Time.Equal(bucketStart) {
			if point.UsagePercent != 20 {
				t.Fatalf("bucket average = %v, want 20", point.UsagePercent)
			}
			return
		}
	}
	t.Fatalf("bucket %s missing from %#v", bucketStart, got.MetricsByServer[serverIDs[0]].CPU)
}

func TestGetCardDataDiskCardReturnsCurrentValueOnly(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-disk",
		Kind:             CardKindDisk,
		Width:            2,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{serverIDs[0]},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	// 最近一次有效磁盘采样之后还有未采样磁盘的行：磁盘卡必须展示最后有效行。
	validAt := time.Now().UTC().Truncate(time.Second).Add(2 * time.Second)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: serverIDs[0], Time: validAt, DiskUsedBytes: 70, DiskTotalBytes: 140},
		{ServerID: serverIDs[0], Time: validAt.Add(time.Second)},
	} {
		if err := svc.metrics.Save(ctx, snap); err != nil {
			t.Fatalf("save metrics: %v", err)
		}
	}

	got, err := svc.GetCardData(ctx, "card-disk")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if got.BucketSeconds == 0 {
		t.Fatalf("disk card must still report bucket seconds")
	}
	series := got.MetricsByServer[serverIDs[0]]
	if series.DiskCurrent == nil {
		t.Fatalf("disk card diskCurrent = nil, want current disk sample")
	}
	if series.DiskCurrent.UsedBytes != 70 || series.DiskCurrent.TotalBytes != 140 {
		t.Fatalf("unexpected diskCurrent: %#v", series.DiskCurrent)
	}
	if len(series.CPU) != 0 || len(series.Memory) != 0 || len(series.DiskIO) != 0 || len(series.Network) != 0 || len(series.Load) != 0 {
		t.Fatalf("disk card must only materialize diskCurrent: %#v", series)
	}
}

func TestGetCardDataDiskIOCardProjectsReadWriteSeries(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-disk-io",
		Kind:             CardKindDiskIO,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{serverIDs[0]},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	base := time.Unix(time.Now().Unix()/10*10, 0).UTC().Add(-30 * time.Second)
	for _, snap := range []linux.MetricsSnapshot{
		{ServerID: serverIDs[0], Time: base, DiskIOAvailable: true, DiskReadBytesRate: 100, DiskWriteBytesRate: 200},
		{ServerID: serverIDs[0], Time: base.Add(10 * time.Second), DiskIOAvailable: true, DiskReadBytesRate: 300, DiskWriteBytesRate: 400},
	} {
		if err := svc.metrics.Save(ctx, snap); err != nil {
			t.Fatalf("save metrics: %v", err)
		}
	}

	got, err := svc.GetCardData(ctx, "card-disk-io")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	series := got.MetricsByServer[serverIDs[0]]
	if len(series.DiskIO) != 2 {
		t.Fatalf("diskIo buckets = %d, want 2: %#v", len(series.DiskIO), series.DiskIO)
	}
	if !series.DiskIO[0].Time.Equal(base) || series.DiskIO[0].ReadBytesPerSecond != 100 || series.DiskIO[0].WriteBytesPerSecond != 200 {
		t.Fatalf("unexpected first diskIo bucket: %#v", series.DiskIO[0])
	}
	if !series.DiskIO[1].Time.Equal(base.Add(10*time.Second)) || series.DiskIO[1].ReadBytesPerSecond != 300 || series.DiskIO[1].WriteBytesPerSecond != 400 {
		t.Fatalf("unexpected second diskIo bucket: %#v", series.DiskIO[1])
	}
	if series.DiskCurrent != nil || len(series.CPU) != 0 || len(series.Memory) != 0 || len(series.Network) != 0 || len(series.Load) != 0 {
		t.Fatalf("diskIO card must only materialize diskIo: %#v", series)
	}
}

func TestUpdateCardsAcceptsDiskIOAndNetworkProbeKinds(t *testing.T) {
	svc, closeStore := newCardTestService(t)
	defer closeStore()
	ctx := context.Background()
	cards := []CardConfiguration{
		{ID: "card-disk-io", Kind: CardKindDiskIO, Width: 2, Height: 2, Range: "1h", NetworkDirection: "both", ServerIDs: []string{}},
		{ID: "card-network-probe", Kind: CardKindNetworkProbe, Width: 3, Height: 2, Range: "1h", NetworkDirection: "both", ServerIDs: []string{}},
	}
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: cards}); err != nil {
		t.Fatalf("update cards with new kinds: %v", err)
	}
	got, err := svc.GetCards(ctx)
	if err != nil {
		t.Fatalf("get cards: %v", err)
	}
	if len(got.Cards) != 2 || got.Cards[0].Kind != CardKindDiskIO || got.Cards[1].Kind != CardKindNetworkProbe {
		t.Fatalf("new card kinds were not persisted: %#v", got.Cards)
	}
}

func TestPlanBucketSecondsKeepsPointCountsBounded(t *testing.T) {
	tests := []struct {
		rng     string
		servers int
		want    int
	}{
		{"1h", 1, 10},
		{"1h", 100, 90},
		{"6h", 1, 60},
		{"1d", 1, 240},
		{"7d", 1, 1800},
		{"7d", 100, 21600},
		{"bogus", 1, 0},
	}
	for _, tc := range tests {
		if got := planBucketSeconds(tc.rng, tc.servers); got != tc.want {
			t.Fatalf("planBucketSeconds(%q, %d) = %d, want %d", tc.rng, tc.servers, got, tc.want)
		}
	}
}

func TestGetCardDataRejectsUnknownCard(t *testing.T) {
	svc, _, closeStore := newCardDataTestService(t)
	defer closeStore()

	if _, err := svc.GetCardData(context.Background(), "missing"); err == nil {
		t.Fatalf("expected missing card error")
	}
}

func TestGetCardDataNetworkProbeCardReturnsProviderData(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-probe",
		Kind:             CardKindNetworkProbe,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}
	updatedAt := time.Now().UTC()
	provider := &fakeProbeCardProvider{card: networkprobe.CardData{
		Nodes: []networkprobe.Node{
			{ServerID: serverIDs[0], Name: "Alpha", Address: "10.0.0.1"},
			{ServerID: serverIDs[1], Name: "Beta", Address: "10.0.0.2"},
		},
		Results: []networkprobe.Result{{
			SourceServerID: serverIDs[0], TargetServerID: serverIDs[1], TargetAddress: "10.0.0.2",
			LatencyMS: 12.5, PacketLossPercent: 0, Sent: 3, Received: 3, SampledAt: updatedAt,
		}},
		UpdatedAt: &updatedAt,
	}}
	svc.probes = provider

	got, err := svc.GetCardData(ctx, "card-probe")
	if err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if got.Probes == nil {
		t.Fatal("networkProbe card must return probes")
	}
	if len(got.Probes.Nodes) != 2 || got.Probes.Nodes[1].ServerID != serverIDs[1] {
		t.Fatalf("unexpected probe nodes: %#v", got.Probes.Nodes)
	}
	if len(got.Probes.Results) != 1 || got.Probes.Results[0].LatencyMS != 12.5 || got.Probes.Results[0].SourceServerID != serverIDs[0] {
		t.Fatalf("unexpected probe results: %#v", got.Probes.Results)
	}
	if got.BucketSeconds != 0 {
		t.Fatalf("networkProbe card bucketSeconds = %d, want 0", got.BucketSeconds)
	}
	if len(got.MetricsByServer) != 0 {
		t.Fatalf("networkProbe card must not materialize metrics: %#v", got.MetricsByServer)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if len(provider.gotServerIDs) != 0 {
		t.Fatalf("empty server selection must pass no filter, got %#v", provider.gotServerIDs)
	}
}

func TestGetCardDataNetworkProbeCardPassesSelectedServers(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-probe",
		Kind:             CardKindNetworkProbe,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{serverIDs[1]},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}
	provider := &fakeProbeCardProvider{}
	svc.probes = provider

	if _, err := svc.GetCardData(ctx, "card-probe"); err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if len(provider.gotServerIDs) != 1 || provider.gotServerIDs[0] != serverIDs[1] {
		t.Fatalf("provider serverIDs = %#v, want the card selection", provider.gotServerIDs)
	}
}

func TestGetCardDataNetworkProbeWithoutProviderAndProviderError(t *testing.T) {
	svc, _, closeStore := newCardDataTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-probe",
		Kind:             CardKindNetworkProbe,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}

	// 未注入 provider：返回空 probes 而不是错误。
	got, err := svc.GetCardData(ctx, "card-probe")
	if err != nil {
		t.Fatalf("get card data without provider: %v", err)
	}
	if got.Probes != nil {
		t.Fatalf("probes = %#v, want nil without provider", got.Probes)
	}

	// provider 失败必须向上返回错误，不得静默展示空数据。
	svc.probes = &fakeProbeCardProvider{err: errors.New("probe unavailable")}
	if _, err := svc.GetCardData(ctx, "card-probe"); err == nil {
		t.Fatal("expected provider error to propagate")
	}
}

func TestGetCardDataMetricCardsDoNotCallProbeProvider(t *testing.T) {
	svc, serverIDs, closeStore := newCardDataTestService(t)
	defer closeStore()
	ctx := context.Background()
	if _, err := svc.UpdateCards(ctx, CardConfigurationSet{Cards: []CardConfiguration{{
		ID:               "card-cpu",
		Kind:             CardKindCPU,
		Width:            3,
		Height:           2,
		Range:            "1h",
		NetworkDirection: "both",
		ServerIDs:        []string{serverIDs[0]},
	}}}); err != nil {
		t.Fatalf("update cards: %v", err)
	}
	provider := &fakeProbeCardProvider{}
	svc.probes = provider
	if _, err := svc.GetCardData(ctx, "card-cpu"); err != nil {
		t.Fatalf("get card data: %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("metric card must not call the probe provider, calls = %d", provider.calls)
	}
}

type fakeProbeCardProvider struct {
	card         networkprobe.CardData
	err          error
	gotServerIDs []string
	calls        int
}

func (f *fakeProbeCardProvider) CardData(_ context.Context, serverIDs []string) (networkprobe.CardData, error) {
	f.calls++
	f.gotServerIDs = append([]string{}, serverIDs...)
	return f.card, f.err
}

func newCardTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
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
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
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
	serverSvc := serverpkg.NewService(store.AppDB(), nil, tasks.NewService(store.AppDB()), serverpkg.WithMetricsDB(store.MetricsDB()))
	first, err := serverSvc.Create(context.Background(), serverpkg.SaveRequest{Name: "Alpha", IPv4: "10.0.0.1", Port: 22, SSHUsername: "root", CredentialID: "cred-test"})
	if err != nil {
		t.Fatalf("create first server: %v", err)
	}
	second, err := serverSvc.Create(context.Background(), serverpkg.SaveRequest{Name: "Beta", IPv4: "10.0.0.2", Port: 22, SSHUsername: "root", CredentialID: "cred-test"})
	if err != nil {
		t.Fatalf("create second server: %v", err)
	}
	metricsSvc := metricspkg.NewService(store.MetricsDB(), serverSvc)
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
