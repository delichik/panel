package contract

import "testing"

func TestCurrentContractIncludesHealthContract(t *testing.T) {
	contract := CurrentContract()
	var health Endpoint
	for _, endpoint := range contract.Endpoints {
		if endpoint.ID == "health" {
			health = endpoint
			break
		}
	}
	if health.ID == "" || health.Method != "GET" || health.Path != "/v1/health" || health.Response == nil {
		t.Fatalf("health endpoint missing from contract: %#v", contract.Endpoints)
	}
	if _, ok := health.Response.Fields["contractHash"]; !ok {
		t.Fatalf("health response contract hash field missing: %#v", health.Response.Fields)
	}
}

func TestGeneratedContractHashMatchesCurrentContract(t *testing.T) {
	if err := ValidateGeneratedHash(); err != nil {
		t.Fatal(err)
	}
	want, err := Hash(CurrentContract())
	if err != nil {
		t.Fatal(err)
	}
	if CurrentHash() != want {
		t.Fatalf("generated contract hash is stale: got %q want %q", CurrentHash(), want)
	}
}
