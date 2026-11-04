package tasks

import "strings"

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
