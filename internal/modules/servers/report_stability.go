package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
)

const (
	reportStabilityWindow     = time.Hour
	reportDisconnectEventsCap = 64
)

// applyReportStability refreshes the rolling 1h disconnect window and derived
// grade on agent.report.* traits. When recordDisconnect is true, now is appended
// as a disconnect transition (connected → disconnected only).
func applyReportStability(traits map[string]string, now time.Time, recordDisconnect bool) {
	if traits == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	events := parseDisconnectEvents(traits[agentcontract.TraitReportDisconnectEvents])
	cutoff := now.Add(-reportStabilityWindow)
	filtered := make([]time.Time, 0, len(events)+1)
	for _, at := range events {
		if !at.Before(cutoff) {
			filtered = append(filtered, at)
		}
	}
	if recordDisconnect {
		filtered = append(filtered, now)
	}
	if len(filtered) > reportDisconnectEventsCap {
		filtered = filtered[len(filtered)-reportDisconnectEventsCap:]
	}

	status := strings.TrimSpace(traits[agentcontract.TraitReportStatus])
	if status == "" && len(filtered) == 0 {
		delete(traits, agentcontract.TraitReportDisconnectEvents)
		delete(traits, agentcontract.TraitReportDisconnectCount1h)
		delete(traits, agentcontract.TraitReportStability)
		return
	}

	count := len(filtered)
	if len(filtered) == 0 {
		delete(traits, agentcontract.TraitReportDisconnectEvents)
	} else {
		traits[agentcontract.TraitReportDisconnectEvents] = serializeDisconnectEvents(filtered)
	}
	traits[agentcontract.TraitReportDisconnectCount1h] = strconv.Itoa(count)
	traits[agentcontract.TraitReportStability] = reportStabilityGrade(count)
}

func reportStabilityGrade(count int) string {
	switch {
	case count <= 0:
		return agentcontract.ReportStabilityExcellent
	case count <= 2:
		return agentcontract.ReportStabilityGood
	case count <= 5:
		return agentcontract.ReportStabilityFair
	case count <= 10:
		return agentcontract.ReportStabilityPoor
	default:
		return agentcontract.ReportStabilityCritical
	}
}

func parseDisconnectEvents(raw string) []time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var unixSeconds []int64
	if err := json.Unmarshal([]byte(raw), &unixSeconds); err != nil {
		return nil
	}
	out := make([]time.Time, 0, len(unixSeconds))
	for _, sec := range unixSeconds {
		if sec <= 0 {
			continue
		}
		out = append(out, time.Unix(sec, 0).UTC())
	}
	return out
}

func serializeDisconnectEvents(events []time.Time) string {
	secs := make([]int64, 0, len(events))
	for _, at := range events {
		if at.IsZero() {
			continue
		}
		secs = append(secs, at.UTC().Unix())
	}
	raw, err := json.Marshal(secs)
	if err != nil {
		return "[]"
	}
	return string(raw)
}
