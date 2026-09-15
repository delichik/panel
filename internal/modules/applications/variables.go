package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	server "panel/internal/modules/servers"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
)

var applicationContainerExpressionPattern = regexp.MustCompile(`\{\{\s*\(\s*index\s+\.applications\s+"([A-Za-z0-9._:-]+)"\s*\)\.containerName\s*\}\}`)

type ApplicationVariableSource interface {
	ApplicationVariables(ctx context.Context, render ApplicationVariableContext) (any, error)
}

type BuiltinVariableResolver interface {
	BuiltinVariables(ctx context.Context, render ApplicationVariableContext) (map[string]any, error)
}

type ApplicationVariableContext struct {
	Application              Application
	Config                   Config
	Server                   *server.Server
	ReferencedApplicationIDs []string
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

func applicationContainerTemplateExpression(applicationID string) string {
	return `{{ (index .applications ` + strconv.Quote(strings.TrimSpace(applicationID)) + `).containerName }}`
}

func referencedApplicationIDs(app Application, files []ApplicationFile) []string {
	seen := map[string]struct{}{}
	collect := func(value string) {
		for _, match := range applicationContainerExpressionPattern.FindAllStringSubmatch(value, -1) {
			if len(match) == 2 {
				seen[match[1]] = struct{}{}
			}
		}
	}
	collect(app.SpecYAML)
	if raw, err := json.Marshal(app.ReverseProxy); err == nil {
		collect(string(raw))
	}
	for _, file := range files {
		if file.Kind == ApplicationFileKindTemplate {
			collect(string(file.Content))
		}
	}
	ids := make([]string, 0, len(seen))
	for applicationID := range seen {
		ids = append(ids, applicationID)
	}
	sort.Strings(ids)
	return ids
}

type applicationReferenceVariableSource struct {
	db *sql.DB
}

type applicationReferenceResolver struct {
	source applicationReferenceVariableSource
}

func (r applicationReferenceResolver) BuiltinVariables(ctx context.Context, render ApplicationVariableContext) (map[string]any, error) {
	value, err := r.source.ApplicationVariables(ctx, render)
	if err != nil {
		return nil, err
	}
	return map[string]any{"applications": value}, nil
}

func (s applicationReferenceVariableSource) ApplicationVariables(ctx context.Context, render ApplicationVariableContext) (any, error) {
	wanted := make(map[string]struct{}, len(render.ReferencedApplicationIDs))
	for _, applicationID := range render.ReferencedApplicationIDs {
		wanted[applicationID] = struct{}{}
	}
	if len(wanted) == 0 {
		return map[string]any{}, nil
	}
	definitions, err := applicationReferenceDefinitions(ctx, s.db)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(wanted))
	for _, definition := range definitions {
		if _, ok := wanted[definition.ResourceID]; !ok {
			continue
		}
		out[definition.ResourceID] = applicationReferenceValue(definition.ResourceID, definition.ResourceName)
	}
	current := render.Application
	if _, ok := wanted[current.ID]; ok && strings.TrimSpace(current.ID) != "" {
		out[current.ID] = applicationReferenceValue(current.ID, current.Name)
	}
	return out, nil
}

func applicationReferenceValue(applicationID, applicationName string) map[string]any {
	app := Application{ID: applicationID, Name: applicationName}
	return map[string]any{
		"id":            applicationID,
		"name":          applicationName,
		"containerName": runtimeContainerName(app),
	}
}

func applicationReferenceDefinitions(ctx context.Context, db *sql.DB) ([]TemplateVariableDefinition, error) {
	if db == nil {
		return []TemplateVariableDefinition{}, nil
	}
	var rows []models.Application
	if err := orm.New(db).From("applications").Select("id", "name").Where("kind<>?", ApplicationKindFacility).And("deletion_requested=0").OrderBy("name ASC", "id ASC").All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]TemplateVariableDefinition, 0, len(rows))
	for _, row := range rows {
		expression := applicationContainerTemplateExpression(row.ID)
		out = append(out, TemplateVariableDefinition{
			Key:                "applications." + row.ID + ".containerName",
			Category:           "application_reference",
			SpecExpression:     expression,
			TemplateExpression: expression,
			ResourceID:         row.ID,
			ResourceName:       row.Name,
		})
	}
	return out, nil
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
