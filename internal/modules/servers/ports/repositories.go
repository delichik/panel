package ports

import (
	"context"

	"panel/internal/modules/servers/domain"
)

type ServerRepository interface {
	List(context.Context) ([]domain.Server, error)
	Get(context.Context, string) (domain.Server, error)
	ListIDs(context.Context) ([]string, error)
	Insert(context.Context, domain.Server) error
	Update(context.Context, domain.Server) error
	Delete(context.Context, string) error
}
