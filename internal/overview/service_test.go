package overview

import (
	"context"
	"path/filepath"
	"testing"

	"panel/internal/config"
	"panel/internal/storage"
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

func newCardTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
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
