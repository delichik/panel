package overview

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"panel/internal/metrics"
	"panel/internal/packages"
	"panel/internal/panelerr"
	"panel/internal/server"
)

type Service struct {
	db       *sql.DB
	servers  *server.Service
	metrics  *metrics.Service
	packages *packages.Service
}

type Overview struct {
	Servers []ServerSummary `json:"servers"`
}

type ServerSummary struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Host                 string     `json:"host"`
	Supported            bool       `json:"supported"`
	Reachable            bool       `json:"reachable"`
	MetricsFresh         bool       `json:"metricsFresh"`
	PackageUpdateCount   int        `json:"packageUpdateCount"`
	LoadAverage          string     `json:"loadAverage"`
	LastMetricsAt        *time.Time `json:"lastMetricsAt"`
	LastPackageRefreshAt *time.Time `json:"lastPackageRefreshAt"`
}

type CardKind string

const (
	cardConfigurationID = "default"

	CardKindCPU              CardKind = "cpu"
	CardKindMemory           CardKind = "memory"
	CardKindDisk             CardKind = "disk"
	CardKindNetwork          CardKind = "network"
	CardKindPackageUpdates   CardKind = "packageUpdates"
	CardKindContainerUpdates CardKind = "containerUpdates"
	CardKindPlaceholder      CardKind = "placeholder"
)

type CardConfiguration struct {
	ID               string   `json:"id"`
	Kind             CardKind `json:"kind"`
	Width            int      `json:"width"`
	Height           int      `json:"height"`
	Range            string   `json:"range"`
	NetworkDirection string   `json:"networkDirection"`
	ServerIDs        []string `json:"serverIds"`
}

type CardConfigurationSet struct {
	Cards []CardConfiguration `json:"cards"`
}

func NewService(db *sql.DB, servers *server.Service, metrics *metrics.Service, packages *packages.Service) *Service {
	return &Service{db: db, servers: servers, metrics: metrics, packages: packages}
}

func (s *Service) Get(ctx context.Context) (Overview, error) {
	servers, err := s.servers.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	counts, refreshes, err := s.packages.Counts(ctx)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{Servers: []ServerSummary{}}
	for _, srv := range servers {
		lastMetrics, err := s.metrics.LatestAt(ctx, srv.ID)
		if err != nil {
			return Overview{}, err
		}
		load, err := s.metrics.LatestLoad(ctx, srv.ID)
		if err != nil {
			return Overview{}, err
		}
		fresh := lastMetrics != nil && time.Since(*lastMetrics) < 5*time.Minute
		out.Servers = append(out.Servers, ServerSummary{
			ID: srv.ID, Name: srv.Name, Host: srv.Host, Supported: srv.OS.Supported, Reachable: srv.Reachable,
			MetricsFresh: fresh, PackageUpdateCount: counts[srv.ID], LoadAverage: load, LastMetricsAt: lastMetrics, LastPackageRefreshAt: refreshes[srv.ID],
		})
	}
	return out, nil
}

func (s *Service) GetCards(ctx context.Context) (CardConfigurationSet, error) {
	defaults, err := json.Marshal(defaultCards())
	if err != nil {
		return CardConfigurationSet{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO overview_card_configurations(id, cards_json, updated_at)
		VALUES(?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, cardConfigurationID, string(defaults), now); err != nil {
		return CardConfigurationSet{}, err
	}

	var raw string
	if err := s.db.QueryRowContext(ctx, `
		SELECT cards_json FROM overview_card_configurations WHERE id=?
	`, cardConfigurationID).Scan(&raw); err != nil {
		return CardConfigurationSet{}, err
	}
	var cards []CardConfiguration
	if err := json.Unmarshal([]byte(raw), &cards); err != nil {
		return CardConfigurationSet{}, err
	}
	if cards == nil {
		cards = []CardConfiguration{}
	}
	return CardConfigurationSet{Cards: cards}, nil
}

func (s *Service) UpdateCards(ctx context.Context, input CardConfigurationSet) (CardConfigurationSet, error) {
	if input.Cards == nil {
		input.Cards = []CardConfiguration{}
	}
	if err := validateCards(input.Cards); err != nil {
		return CardConfigurationSet{}, err
	}
	raw, err := json.Marshal(input.Cards)
	if err != nil {
		return CardConfigurationSet{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO overview_card_configurations(id, cards_json, updated_at)
		VALUES(?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET cards_json=excluded.cards_json, updated_at=excluded.updated_at
	`, cardConfigurationID, string(raw), now); err != nil {
		return CardConfigurationSet{}, err
	}
	return input, nil
}

func validateCards(cards []CardConfiguration) error {
	if len(cards) > 100 {
		return panelerr.Validation("overview_cards_too_many", "Overview dashboard cannot contain more than 100 cards")
	}
	ids := make(map[string]struct{}, len(cards))
	for _, card := range cards {
		if strings.TrimSpace(card.ID) == "" {
			return panelerr.Validation("overview_card_id_invalid", "Overview card ID is required")
		}
		if _, exists := ids[card.ID]; exists {
			return panelerr.Validation("overview_card_id_duplicate", "Overview card IDs must be unique")
		}
		ids[card.ID] = struct{}{}
		if !validCardKind(card.Kind) {
			return panelerr.Validation("overview_card_kind_invalid", "Overview card kind is invalid")
		}
		if card.Width < 1 || card.Width > 6 || card.Height < 1 || card.Height > 4 {
			return panelerr.Validation("overview_card_size_invalid", "Overview card size is invalid")
		}
		if !validRange(card.Range) {
			return panelerr.Validation("overview_card_range_invalid", "Overview card range is invalid")
		}
		if card.NetworkDirection != "rx" && card.NetworkDirection != "tx" && card.NetworkDirection != "both" {
			return panelerr.Validation("overview_card_network_direction_invalid", "Overview card network direction is invalid")
		}
		serverIDs := make(map[string]struct{}, len(card.ServerIDs))
		for _, serverID := range card.ServerIDs {
			if strings.TrimSpace(serverID) == "" {
				return panelerr.Validation("overview_card_server_id_invalid", "Overview card server IDs cannot be empty")
			}
			if _, exists := serverIDs[serverID]; exists {
				return panelerr.Validation("overview_card_server_id_duplicate", "Overview card server IDs must be unique")
			}
			serverIDs[serverID] = struct{}{}
		}
	}
	return nil
}

func validCardKind(kind CardKind) bool {
	switch kind {
	case CardKindCPU, CardKindMemory, CardKindDisk, CardKindNetwork, CardKindPackageUpdates, CardKindContainerUpdates, CardKindPlaceholder:
		return true
	default:
		return false
	}
}

func validRange(value string) bool {
	return value == "1h" || value == "6h" || value == "1d" || value == "7d"
}

func defaultCards() []CardConfiguration {
	return []CardConfiguration{
		newDefaultCard("card-default-cpu", CardKindCPU, "1h"),
		newDefaultCard("card-default-memory", CardKindMemory, "1h"),
		newDefaultCard("card-default-disk", CardKindDisk, "6h"),
		newDefaultCard("card-default-network", CardKindNetwork, "1h"),
		newDefaultCard("card-default-package-updates", CardKindPackageUpdates, "1d"),
		newDefaultCard("card-default-container-updates", CardKindContainerUpdates, "1d"),
	}
}

func newDefaultCard(id string, kind CardKind, metricRange string) CardConfiguration {
	return CardConfiguration{
		ID:               id,
		Kind:             kind,
		Width:            3,
		Height:           2,
		Range:            metricRange,
		NetworkDirection: "both",
		ServerIDs:        []string{},
	}
}
