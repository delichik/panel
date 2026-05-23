package placement

import "testing"

func TestSchedulePrefersExistingThenStableNameAndRejectsDependencyNodeMismatch(t *testing.T) {
	nodes := []Node{{
		ID: "srv_b", Name: "b", Reachable: true, OSSupported: true,
		Capabilities: Capabilities{Docker: true, Compose: true, Include: true},
		Traits:       map[string]string{"role": "web"},
	}, {
		ID: "srv_a", Name: "a", Reachable: true, OSSupported: true,
		Capabilities: Capabilities{Docker: true, Compose: true, Include: true},
		Traits:       map[string]string{"role": "web"},
	}}
	svc := ServiceSpec{ID: "api", Selector: map[string]string{"role": "web"}, PortClaims: []int{8080}}
	got, err := ChooseNode(svc, nodes, State{ExistingNodeID: "srv_b"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Node.ID != "srv_b" {
		t.Fatalf("expected existing node preference, got %#v", got.Node)
	}
	got, err = ChooseNode(svc, nodes, State{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Node.ID != "srv_a" {
		t.Fatalf("expected stable name/id sort, got %#v", got.Node)
	}
	_, err = ChooseNode(ServiceSpec{ID: "api", Dependencies: []DependencyPlacement{{ServiceID: "db", NodeID: "srv_b"}}}, nodes[1:], State{})
	if err == nil {
		t.Fatal("expected dependency co-location conflict")
	}
	_, err = ChooseNode(svc, nodes, State{ManagedPortClaims: []PortClaim{{NodeID: "srv_a", Port: 8080}}})
	if err != nil {
		t.Fatalf("port conflict on first node should choose second node: %v", err)
	}
}
