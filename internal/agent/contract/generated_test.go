package contract

import "testing"

func TestCurrentContractIncludesHealthContract(t *testing.T) {
	contract := CurrentContract()
	var health Method
	for _, method := range contract.Methods {
		if method.ID == "Health" {
			health = method
			break
		}
	}
	if health.ID == "" || health.Service != agentService || health.RPC != "Health" || health.Response == nil {
		t.Fatalf("health method missing from contract: %#v", contract.Methods)
	}
	if health.Response.Type != "panel.agent.v1.HealthResponse" {
		t.Fatalf("health response type mismatch: %#v", health.Response)
	}
	if contract.ProtoFile == nil || contract.ProtoFile.GetName() != "agent.proto" {
		t.Fatalf("proto descriptor missing from contract: %#v", contract.ProtoFile)
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
