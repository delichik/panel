package tasks

import (
	"strings"

	"panel/internal/modules/runtimeevents"
)

// classifyOutputLevel maps a task output line to a JSONL level.
// stderr is not automatically error: remote scripts often print progress on
// stderr. Failures stay error. Noisy progress is debug — and with the default
// process log level (info), Debug is not written to JSONL or events at all.
func classifyOutputLevel(stream, text string, quiet bool) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if isFailureOutput(lower) {
		return "error"
	}
	if isNoisyProgressOutput(lower) {
		return "debug"
	}
	if stream == "stderr" {
		return "error"
	}
	if quiet {
		// Quiet routine tasks demote remote stdout/progress to debug. Panel-authored
		// "system" milestones stay visible at info: they carry operator diagnostics
		// such as "privileged access unavailable" that must remain readable in the
		// task log even when the routine lifecycle facts are debug.
		if stream == "system" {
			return "info"
		}
		return "debug"
	}
	return "info"
}

func isFailureOutput(lower string) bool {
	if lower == "" {
		return false
	}
	markers := []string{
		"_failed",
		" failed",
		"failure",
		"error:",
		"fatal:",
		"panic:",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isNoisyProgressOutput(lower string) bool {
	if lower == "" {
		return false
	}
	// Remote agent deploy / transfer progress markers.
	markers := []string{
		"panel_agent_download_ok",
		"panel_agent_download_retry",
		"panel_agent_download_resume_unsupported",
		"panel_agent_fetcher installing",
		"panel_agent_fetcher ready",
		"panel_agent_docker installed",
		"panel_agent_docker starting",
		"panel_agent_docker_ready",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// classifyTaskEventLevel maps a task lifecycle event to the unified events/logs
// level (TASK-REG-003). Quiet task creation and success transitions are routine
// background facts, so they are recorded at debug level and therefore never
// reach the user-visible notification channel; failures and retryable/blocked
// terminal states stay visible as error.
func classifyTaskEventLevel(eventType, severity string, quiet bool) string {
	level := "info"
	switch severity {
	case runtimeevents.SeverityError:
		level = "error"
	case runtimeevents.SeverityWarning:
		level = "warn"
	}
	if !quiet {
		return level
	}
	if eventType == runtimeevents.EventTaskFailed {
		// Failed, retryable-failed and blocked lifecycles must remain visible
		// even for routine task types.
		return "error"
	}
	if severity == runtimeevents.SeverityInfo {
		return "debug"
	}
	return level
}
