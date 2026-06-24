package applications

import (
	"context"
	"strconv"
	"strings"

	server "panel/internal/modules/servers"
)

type ApplicationVariableSource interface {
	ApplicationVariables(ctx context.Context, render ApplicationVariableContext) (any, error)
}

type BuiltinVariableResolver interface {
	BuiltinVariables(ctx context.Context, render ApplicationVariableContext) (map[string]any, error)
}

type ApplicationVariableContext struct {
	Application Application
	Config      Config
	Server      *server.Server
}

type ApplicationVariableRegistry struct {
	sources map[string]ApplicationVariableSource
}

func NewApplicationVariableRegistry() *ApplicationVariableRegistry {
	registry := &ApplicationVariableRegistry{sources: map[string]ApplicationVariableSource{}}
	registry.Register("app", appVariableSource{})
	registry.Register("server", serverVariableSource{})
	return registry
}

func (r *ApplicationVariableRegistry) Register(key string, source ApplicationVariableSource) {
	key = strings.TrimSpace(key)
	if key == "" || source == nil {
		return
	}
	r.sources[key] = source
}

func (r *ApplicationVariableRegistry) BuiltinVariables(ctx context.Context, render ApplicationVariableContext) (map[string]any, error) {
	if r == nil {
		return nil, nil
	}
	out := map[string]any{}
	for key, source := range r.sources {
		value, err := source.ApplicationVariables(ctx, render)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

type appVariableSource struct{}

func (appVariableSource) ApplicationVariables(ctx context.Context, render ApplicationVariableContext) (any, error) {
	app := render.Application
	namespace := strings.TrimSpace(firstNonEmpty(app.Namespace, render.Config.Namespace))
	deploymentMode := strings.TrimSpace(app.DeploymentMode)
	if deploymentMode == "" {
		deploymentMode = DeploymentModeAll
	}
	return map[string]any{
		"id":             app.ID,
		"name":           app.Name,
		"namespace":      namespace,
		"generation":     app.Generation,
		"deploymentMode": deploymentMode,
	}, nil
}

type serverVariableSource struct{}

func (serverVariableSource) ApplicationVariables(ctx context.Context, render ApplicationVariableContext) (any, error) {
	if render.Server == nil {
		return map[string]any{
			"id":          "",
			"name":        "",
			"host":        "",
			"sshHost":     "",
			"sshPort":     0,
			"sshPortText": "",
			"sshUsername": "",
			"variables":   map[string]any{},
		}, nil
	}
	srv := *render.Server
	variables := map[string]any{}
	for key, value := range srv.Variables {
		variables[key] = value
	}
	return map[string]any{
		"id":          srv.ID,
		"name":        srv.Name,
		"host":        srv.Host,
		"sshHost":     srv.Host,
		"sshPort":     srv.Port,
		"sshPortText": strconv.Itoa(srv.Port),
		"sshUsername": srv.SSHUsername,
		"variables":   variables,
	}, nil
}
