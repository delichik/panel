package applications

import (
	"context"
	"io"
	"strings"

	panelerr "panel/internal/platform/errors"
)

type InternalFileInfo struct {
	Name string
	Mode string
	Size int64
}

type InternalFileSource interface {
	InternalFileCatalog(ctx context.Context) ([]PanelFileDefinition, error)
	OpenInternalFile(ctx context.Context, source string) (io.ReadCloser, InternalFileInfo, error)
}

type InternalFileProvider interface {
	InternalFileCatalog(ctx context.Context) ([]PanelFileDefinition, error)
	OpenInternalFile(ctx context.Context, source string) (io.ReadCloser, InternalFileInfo, error)
}

type InternalFileRegistry struct {
	resolvers map[string]InternalFileSource
}

func NewInternalFileRegistry() *InternalFileRegistry {
	return &InternalFileRegistry{resolvers: map[string]InternalFileSource{}}
}

func (r *InternalFileRegistry) Register(scheme string, resolver InternalFileSource) {
	scheme = strings.TrimSpace(scheme)
	if scheme == "" || resolver == nil {
		return
	}
	r.resolvers[scheme] = resolver
}

func (r *InternalFileRegistry) InternalFileCatalog(ctx context.Context) ([]PanelFileDefinition, error) {
	if r == nil {
		return nil, nil
	}
	out := []PanelFileDefinition{}
	for _, resolver := range r.resolvers {
		items, err := resolver.InternalFileCatalog(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return out, nil
}

func (r *InternalFileRegistry) OpenInternalFile(ctx context.Context, source string) (io.ReadCloser, InternalFileInfo, error) {
	if r == nil {
		return nil, InternalFileInfo{}, panelerr.NotFound("internal file")
	}
	resolver := r.resolvers[internalFileScheme(source)]
	if resolver == nil {
		return nil, InternalFileInfo{}, panelerr.Validation("panel_file_source_invalid", "Panel file source is invalid")
	}
	return resolver.OpenInternalFile(ctx, source)
}

func internalFileScheme(source string) string {
	source = strings.TrimSpace(source)
	idx := strings.Index(source, ":")
	if idx < 0 {
		return ""
	}
	return source[:idx]
}
