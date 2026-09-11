package ports

import (
	"context"

	"panel/internal/modules/servers/domain"
	httpx "panel/internal/platform/http"
)

type ServerRepository interface {
	List(context.Context) ([]domain.Server, error)
	ListSummaries(context.Context) ([]domain.ServerSummary, error)
	ListSummaryPage(context.Context, int, int, string) (httpx.ListPage[domain.ServerSummary], error)
	Get(context.Context, string) (domain.Server, error)
	ListIDs(context.Context) ([]string, error)
	Insert(context.Context, domain.Server) error
	Update(context.Context, domain.Server) error
	Delete(context.Context, string) error
}
