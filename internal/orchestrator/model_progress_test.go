package orchestrator

import "testing"

func TestApplyReadyToSucceed(t *testing.T) {
	if !ApplyReadyToSucceed(ActionStop, []Step{{Name: "stop", Status: "succeeded"}}) {
		t.Fatal("stop should succeed without verify_running")
	}
	if !ApplyReadyToSucceed(ActionApply, nil) {
		t.Fatal("empty trail should allow legacy succeed")
	}
	if !ApplyReadyToSucceed(ActionApply, []Step{{Name: "apply", Status: "succeeded"}}) {
		t.Fatal("stub apply step without verify_running should allow")
	}
	if ApplyReadyToSucceed(ActionApply, []Step{{Name: "verify_running", Status: "running"}}) {
		t.Fatal("in-flight verify_running must block")
	}
	if !ApplyReadyToSucceed(ActionApply, []Step{
		{Name: "ensure_image", Status: "succeeded"},
		{Name: "verify_running", Status: "succeeded"},
	}) {
		t.Fatal("successful verify_running must allow")
	}
}

func TestUncertaintyNeedsAttention(t *testing.T) {
	if !UncertaintyNeedsAttention("uncertainty", "") {
		t.Fatal("uncertainty without stage needs attention")
	}
	if UncertaintyNeedsAttention("uncertainty", "ensure_image") {
		t.Fatal("projected progress is normal converging")
	}
	if UncertaintyNeedsAttention("", "ensure_image") {
		t.Fatal("non-uncertainty must not need attention")
	}
}
