package contract

import "testing"

func TestMissingAllowsAdditionalOptionalFields(t *testing.T) {
	required := Contract{Endpoints: []Endpoint{{
		ID:     "health",
		Method: "GET",
		Path:   "/v1/health",
		Response: &Schema{Type: "object", Fields: map[string]Schema{
			"status": {Type: "string"},
		}},
	}}}
	actual := required
	actual.Endpoints[0].Response = &Schema{Type: "object", Fields: map[string]Schema{
		"status": {Type: "string"},
		"extra":  {Type: "string", Optional: true},
	}}

	if missing := Missing(required, actual); len(missing) != 0 {
		t.Fatalf("additional fields should remain compatible: %v", missing)
	}
}

func TestMissingDetectsRequiredSchemaRemoval(t *testing.T) {
	required := Contract{Endpoints: []Endpoint{{
		ID:     "create",
		Method: "POST",
		Path:   "/v1/create",
		Request: &Schema{Type: "object", Fields: map[string]Schema{
			"spec": {Type: "object"},
		}},
	}}}
	actual := Contract{Endpoints: []Endpoint{{
		ID:      "create",
		Method:  "POST",
		Path:    "/v1/create",
		Request: &Schema{Type: "object", Fields: map[string]Schema{}},
	}}}

	missing := Missing(required, actual)
	if len(missing) != 1 || missing[0] != "create" {
		t.Fatalf("missing = %v", missing)
	}
}
