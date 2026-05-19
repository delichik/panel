package templatex

import (
	"bytes"
	"context"
	"strings"
	"text/template"

	"panel/internal/panelerr"
)

type Renderer interface {
	Render(ctx context.Context, source string, data map[string]any) (string, error)
}

type GoRenderer struct{}

func NewGoRenderer() GoRenderer { return GoRenderer{} }

func (GoRenderer) Render(ctx context.Context, source string, data map[string]any) (string, error) {
	tpl, err := template.New("resource").Option("missingkey=error").Parse(source)
	if err != nil {
		return "", panelerr.Validation("template_parse_failed", err.Error())
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", panelerr.Validation("template_render_failed", cleanTemplateError(err.Error()))
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return buf.String(), nil
	}
}

func cleanTemplateError(message string) string {
	message = strings.ReplaceAll(message, "map has no entry for key", "missing variable")
	return message
}
