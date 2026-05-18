package templatex

import "context"

type Renderer interface {
	Render(ctx context.Context, source string, data map[string]any) (string, error)
}
