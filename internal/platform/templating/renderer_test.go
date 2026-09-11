package templatex

import (
	"context"
	"strings"
	"testing"
)

func TestGoRendererRequiresVariables(t *testing.T) {
	_, err := NewGoRenderer().Render(context.Background(), "hello {{ .missing }}", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "missing variable") {
		t.Fatalf("expected missing variable error, got %v", err)
	}
}

func TestGoRendererRendersValues(t *testing.T) {
	got, err := NewGoRenderer().Render(context.Background(), "hello {{ .name }}", map[string]any{"name": "panel"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello panel" {
		t.Fatalf("unexpected render output: %q", got)
	}
}
