package applications

import (
	"testing"

	panelerr "panel/internal/platform/errors"
)

func TestNormalizeAnyAccessConfigOriginPriority(t *testing.T) {
	origins := []string{"srv-a", "srv-b", "srv-c"}

	t.Run("derives primary from user-arranged priority", func(t *testing.T) {
		cfg, err := NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:        true,
			Strategy:       AnyAccessStrategyPrimaryBackup,
			OriginPriority: []string{"srv-c", "srv-a", "srv-b"},
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.PrimaryOriginServerID != "srv-c" {
			t.Fatalf("expected primary srv-c, got %q", cfg.PrimaryOriginServerID)
		}
		if len(cfg.OriginPriority) != 3 || cfg.OriginPriority[0] != "srv-c" || cfg.OriginPriority[2] != "srv-b" {
			t.Fatalf("expected priority order to be preserved, got %v", cfg.OriginPriority)
		}
	})

	t.Run("falls back to explicit primary when no priority is provided", func(t *testing.T) {
		cfg, err := NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:               true,
			Strategy:              AnyAccessStrategyPrimaryBackup,
			PrimaryOriginServerID: "srv-b",
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.PrimaryOriginServerID != "srv-b" {
			t.Fatalf("expected primary srv-b, got %q", cfg.PrimaryOriginServerID)
		}
		if cfg.OriginPriority != nil {
			t.Fatalf("expected no origin priority, got %v", cfg.OriginPriority)
		}
	})

	t.Run("projects a stale priority onto the current origin set", func(t *testing.T) {
		// A server was removed from the gateway/deployment set: it is dropped,
		// the remaining stored order is preserved and the new server is
		// appended, so saves and automatic reconciles keep working instead of
		// failing on the stale priority.
		cfg, err := NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:        true,
			Strategy:       AnyAccessStrategyPrimaryBackup,
			OriginPriority: []string{"srv-a", "srv-b"},
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.PrimaryOriginServerID != "srv-a" {
			t.Fatalf("expected primary srv-a, got %q", cfg.PrimaryOriginServerID)
		}
		if len(cfg.OriginPriority) != 3 || cfg.OriginPriority[0] != "srv-a" || cfg.OriginPriority[1] != "srv-b" || cfg.OriginPriority[2] != "srv-c" {
			t.Fatalf("expected projected priority [srv-a srv-b srv-c], got %v", cfg.OriginPriority)
		}

		cfg, err = NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:        true,
			Strategy:       AnyAccessStrategyPrimaryBackup,
			OriginPriority: []string{"srv-a", "srv-b", "srv-x"},
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.OriginPriority) != 3 || cfg.OriginPriority[2] != "srv-c" {
			t.Fatalf("expected stale srv-x to be dropped and srv-c appended, got %v", cfg.OriginPriority)
		}
	})

	t.Run("collapses duplicate entries in the priority", func(t *testing.T) {
		cfg, err := NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:        true,
			Strategy:       AnyAccessStrategyPrimaryBackup,
			OriginPriority: []string{"srv-a", "srv-a", "srv-b", "srv-c"},
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(cfg.OriginPriority) != 3 || cfg.OriginPriority[0] != "srv-a" {
			t.Fatalf("expected duplicates collapsed, got %v", cfg.OriginPriority)
		}
	})

	t.Run("clears priority for non-primary-backup strategies", func(t *testing.T) {
		cfg, err := NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:        true,
			Strategy:       AnyAccessStrategyRoundRobin,
			OriginPriority: []string{"srv-a", "srv-b", "srv-c"},
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.PrimaryOriginServerID != "" || cfg.OriginPriority != nil {
			t.Fatalf("expected primary and priority to be cleared, got %q %v", cfg.PrimaryOriginServerID, cfg.OriginPriority)
		}
	})

	t.Run("clears priority when AnyAccess is disabled", func(t *testing.T) {
		cfg, err := NormalizeAnyAccessConfig(AnyAccessConfig{
			Enabled:        false,
			Strategy:       AnyAccessStrategyPrimaryBackup,
			OriginPriority: []string{"srv-a", "srv-b", "srv-c"},
		}, origins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.PrimaryOriginServerID != "" || cfg.OriginPriority != nil {
			t.Fatalf("expected primary and priority to be cleared, got %q %v", cfg.PrimaryOriginServerID, cfg.OriginPriority)
		}
	})
}

func TestNormalizeReverseProxyRulesIgnoresClientOrigins(t *testing.T) {
	rules, err := normalizeReverseProxyRules([]ReverseProxyRule{{
		Domain:          "api.example.test",
		TargetPort:      8080,
		OriginServerIDs: []string{"srv-x"},
		AnyAccess: AnyAccessConfig{
			Enabled:               true,
			Strategy:              AnyAccessStrategyPrimaryBackup,
			PrimaryOriginServerID: "srv-x",
			OriginPriority:        []string{"srv-x"},
		},
		Paths: []ReverseProxyPath{{Path: "/"}},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if len(rules[0].OriginServerIDs) != 0 {
		t.Fatalf("expected client-provided origin servers to be ignored, got %v", rules[0].OriginServerIDs)
	}
	if rules[0].AnyAccess.PrimaryOriginServerID != "" {
		t.Fatalf("expected client-provided primary origin to be ignored, got %q", rules[0].AnyAccess.PrimaryOriginServerID)
	}
	if len(rules[0].AnyAccess.OriginPriority) != 1 || rules[0].AnyAccess.OriginPriority[0] != "srv-x" {
		t.Fatalf("expected origin priority to be preserved, got %v", rules[0].AnyAccess.OriginPriority)
	}
}

func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", code)
	}
	perr, ok := err.(*panelerr.Error)
	if !ok {
		t.Fatalf("expected *panelerr.Error, got %T: %v", err, err)
	}
	if perr.Code != code {
		t.Fatalf("expected code %q, got %q", code, perr.Code)
	}
}
