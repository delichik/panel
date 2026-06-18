package agent

import "testing"

func TestCurrentContractIncludesHealthContract(t *testing.T) {
	contract := CurrentContract()
	var health ContractEndpoint
	for _, endpoint := range contract.Endpoints {
		if endpoint.ID == "health" {
			health = endpoint
			break
		}
	}
	if health.ID == "" || health.Method != "GET" || health.Path != "/v1/health" || health.Response == nil {
		t.Fatalf("health endpoint missing from contract: %#v", contract.Endpoints)
	}
	if _, ok := health.Response.Fields["contract"]; !ok {
		t.Fatalf("health response contract field missing: %#v", health.Response.Fields)
	}
}

func TestMissingContractEndpointsAllowsAdditionalFields(t *testing.T) {
	contract := CurrentContract()
	for i := range contract.Endpoints {
		if contract.Endpoints[i].ID != "health" || contract.Endpoints[i].Response == nil {
			continue
		}
		contract.Endpoints[i].Response.Fields["extra"] = Schema{Type: "string", Optional: true}
	}
	if missing := MissingContractEndpoints(contract); len(missing) != 0 {
		t.Fatalf("additional fields should remain compatible, missing=%v", missing)
	}
}

func TestMissingContractEndpointsDetectsSchemaChanges(t *testing.T) {
	contract := CurrentContract()
	for i := range contract.Endpoints {
		if contract.Endpoints[i].ID != "runtime-create-container" || contract.Endpoints[i].Request == nil {
			continue
		}
		delete(contract.Endpoints[i].Request.Fields, "spec")
	}
	missing := MissingContractEndpoints(contract)
	if len(missing) != 1 || missing[0] != "runtime-create-container" {
		t.Fatalf("expected runtime-create-container incompatibility, got %v", missing)
	}
}
