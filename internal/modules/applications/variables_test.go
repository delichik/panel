package applications

import (
	"context"
	"testing"
)

func TestApplicationVariableRegistryRegistersRootKeys(t *testing.T) {
	registry := NewApplicationVariableRegistry()
	registry.Register("certs", fakeVariableSource{value: map[string]any{"web": "tls"}})
	registry.Register("custom", fakeVariableSource{value: "value"})

	vars, err := registry.BuiltinVariables(context.Background(), ApplicationVariableContext{})
	if err != nil {
		t.Fatal(err)
	}
	if vars["custom"] != "value" {
		t.Fatalf("custom variable = %#v", vars["custom"])
	}
	certs, ok := vars["certs"].(map[string]any)
	if !ok || certs["web"] != "tls" {
		t.Fatalf("certs variable = %#v", vars["certs"])
	}
}

type fakeVariableSource struct {
	value any
	err   error
}

func (f fakeVariableSource) ApplicationVariables(ctx context.Context, render ApplicationVariableContext) (any, error) {
	return f.value, f.err
}
