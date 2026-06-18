package panel

import (
	"context"
	"testing"
)

func TestApplicationCertificateBridgeRejectsUseBeforeInitialization(t *testing.T) {
	bridge := &applicationCertificateBridge{}
	if _, err := bridge.BuiltinVariables(context.Background()); err == nil {
		t.Fatal("expected uninitialized certificate bridge error")
	}
	if _, err := bridge.RedeployEnabledApplications(context.Background()); err == nil {
		t.Fatal("expected uninitialized application bridge error")
	}
}

func TestApplicationContainerBridgeRejectsUseBeforeInitialization(t *testing.T) {
	bridge := &applicationContainerBridge{}
	if err := bridge.Execute(context.Background(), "srv", func(context.Context) error { return nil }); err == nil {
		t.Fatal("expected uninitialized container bridge error")
	}
	if _, err := bridge.List(context.Background()); err == nil {
		t.Fatal("expected uninitialized application bridge error")
	}
}
