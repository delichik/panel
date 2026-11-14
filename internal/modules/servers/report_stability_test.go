package server

import (
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
)

func TestReportStabilityGradeThresholds(t *testing.T) {
	cases := []struct {
		count int
		want  string
	}{
		{0, agentcontract.ReportStabilityExcellent},
		{1, agentcontract.ReportStabilityGood},
		{2, agentcontract.ReportStabilityGood},
		{3, agentcontract.ReportStabilityFair},
		{5, agentcontract.ReportStabilityFair},
		{6, agentcontract.ReportStabilityPoor},
		{10, agentcontract.ReportStabilityPoor},
		{11, agentcontract.ReportStabilityCritical},
		{40, agentcontract.ReportStabilityCritical},
	}
	for _, tc := range cases {
		if got := reportStabilityGrade(tc.count); got != tc.want {
			t.Fatalf("count=%d: got %q want %q", tc.count, got, tc.want)
		}
	}
}

func TestApplyReportStabilityRecordsOnlyConnectedToDisconnected(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	traits := map[string]string{
		agentcontract.TraitReportStatus: agentcontract.ReportStatusConnected,
	}

	applyReportStability(traits, now, false)
	if traits[agentcontract.TraitReportStability] != agentcontract.ReportStabilityExcellent {
		t.Fatalf("expected excellent before disconnect, got %q", traits[agentcontract.TraitReportStability])
	}
	if traits[agentcontract.TraitReportDisconnectCount1h] != "0" {
		t.Fatalf("expected count 0, got %q", traits[agentcontract.TraitReportDisconnectCount1h])
	}

	applyReportStability(traits, now.Add(time.Minute), true)
	if traits[agentcontract.TraitReportStability] != agentcontract.ReportStabilityGood {
		t.Fatalf("expected good after one disconnect, got %q", traits[agentcontract.TraitReportStability])
	}
	if traits[agentcontract.TraitReportDisconnectCount1h] != "1" {
		t.Fatalf("expected count 1, got %q", traits[agentcontract.TraitReportDisconnectCount1h])
	}

	applyReportStability(traits, now.Add(2*time.Minute), true)
	applyReportStability(traits, now.Add(3*time.Minute), true)
	if traits[agentcontract.TraitReportStability] != agentcontract.ReportStabilityFair {
		t.Fatalf("expected fair after three disconnects, got %q", traits[agentcontract.TraitReportStability])
	}
}

func TestApplyReportStabilityPrunesOutsideOneHour(t *testing.T) {
	base := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	traits := map[string]string{
		agentcontract.TraitReportStatus:           agentcontract.ReportStatusConnected,
		agentcontract.TraitReportDisconnectEvents: serializeDisconnectEvents([]time.Time{base, base.Add(10 * time.Minute)}),
	}

	applyReportStability(traits, base.Add(70*time.Minute), false)
	if traits[agentcontract.TraitReportDisconnectCount1h] != "0" {
		t.Fatalf("expected aged-out count 0, got %q", traits[agentcontract.TraitReportDisconnectCount1h])
	}
	if traits[agentcontract.TraitReportStability] != agentcontract.ReportStabilityExcellent {
		t.Fatalf("expected excellent after prune, got %q", traits[agentcontract.TraitReportStability])
	}
	if _, ok := traits[agentcontract.TraitReportDisconnectEvents]; ok {
		t.Fatalf("expected disconnect events key removed after prune")
	}
}

func TestApplyReportStabilityClearsWhenNeverReported(t *testing.T) {
	traits := map[string]string{}
	applyReportStability(traits, time.Now().UTC(), false)
	if len(traits) != 0 {
		t.Fatalf("expected empty traits for never-reported agent, got %#v", traits)
	}
}
