package contract

import "testing"

func TestValidateGeneratedHashRejectsEmptyValue(t *testing.T) {
	if err := validateGeneratedHash(""); err == nil {
		t.Fatal("expected empty generated hash to fail validation")
	}
}

func TestHashIsStableAcrossMapInsertionOrder(t *testing.T) {
	first := Contract{Methods: []Method{{
		ID:      "health",
		Service: "panel.agent.v1.AgentService",
		RPC:     "Health",
		Response: &Schema{Type: "object", Fields: map[string]Schema{
			"status": {Type: "string"},
			"extra":  {Type: "string", Optional: true},
		}},
	}}}
	second := Contract{Methods: []Method{{
		ID:      "health",
		Service: "panel.agent.v1.AgentService",
		RPC:     "Health",
		Response: &Schema{Type: "object", Fields: map[string]Schema{
			"extra":  {Type: "string", Optional: true},
			"status": {Type: "string"},
		}},
	}}}

	firstHash, err := Hash(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := Hash(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("map insertion order changed hash: first=%q second=%q", firstHash, secondHash)
	}
}

func TestHashChangesWhenContractChanges(t *testing.T) {
	original := Contract{Methods: []Method{{
		ID:      "create",
		Service: "panel.agent.v1.AgentService",
		RPC:     "Create",
		Request: &Schema{Type: "object", Fields: map[string]Schema{
			"spec": {Type: "object"},
		}},
	}}}
	changed := Contract{Methods: []Method{{
		ID:      "create",
		Service: "panel.agent.v1.AgentService",
		RPC:     "Create",
		Request: &Schema{Type: "object", Fields: map[string]Schema{}},
	}}}

	originalHash, err := Hash(original)
	if err != nil {
		t.Fatal(err)
	}
	changedHash, err := Hash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if originalHash == changedHash {
		t.Fatalf("contract change did not change hash: %q", originalHash)
	}
}
