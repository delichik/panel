package overview

import (
	"context"
	"time"

	"panel/internal/metrics"
	"panel/internal/packages"
	"panel/internal/server"
)

type Service struct {
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

func NewService(servers *server.Service, metrics *metrics.Service, packages *packages.Service) *Service {
	return &Service{servers: servers, metrics: metrics, packages: packages}
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
