package applications

import (
	"context"
	"strings"
)

type ApplicationVariableSource interface {
	ApplicationVariables(ctx context.Context) (any, error)
}

type BuiltinVariableResolver interface {
	BuiltinVariables(ctx context.Context) (map[string]any, error)
}

type ApplicationVariableRegistry struct {
	sources map[string]ApplicationVariableSource
}

func NewApplicationVariableRegistry() *ApplicationVariableRegistry {
	return &ApplicationVariableRegistry{sources: map[string]ApplicationVariableSource{}}
}

func (r *ApplicationVariableRegistry) Register(key string, source ApplicationVariableSource) {
	key = strings.TrimSpace(key)
	if key == "" || source == nil {
		return
	}
	r.sources[key] = source
}

func (r *ApplicationVariableRegistry) BuiltinVariables(ctx context.Context) (map[string]any, error) {
	if r == nil {
		return nil, nil
	}
	out := map[string]any{}
	for key, source := range r.sources {
		value, err := source.ApplicationVariables(ctx)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}
