package tasks

import "testing"

func TestClassifyOutputLevel(t *testing.T) {
	cases := []struct {
		stream string
		text   string
		quiet  bool
		want   string
	}{
		{"stdout", "hello", false, "info"},
		{"system", "stage advanced", false, "info"},
		{"stdout", "hello", true, "debug"},
		// Panel-authored system milestones stay readable even for quiet tasks:
		// they carry operator diagnostics, not remote progress noise.
		{"system", "stage advanced", true, "info"},
		{"system", "privileged access unavailable: sudo denied", true, "info"},
		{"stderr", "boom", false, "error"},
		{"stderr", "[panel] panel_agent_download_ok fetcher=curl rounds=1 bytes=10", false, "debug"},
		{"stdout", "[panel] panel_agent_download_ok fetcher=curl rounds=1 bytes=10", false, "debug"},
		{"stderr", "[panel] panel_agent_download_retry round=1 fetcher=curl status=28", false, "debug"},
		{"stderr", "[panel] panel_agent_fetcher ready fetcher=curl installed=yes", false, "debug"},
		{"stderr", "[panel] panel_agent_download_failed reason=truncated", false, "error"},
		{"stdout", "[panel] panel_agent_docker_failed reason=not_ready", false, "error"},
		{"system", "panel agent download failed (unreachable)", false, "error"},
	}
	for _, tc := range cases {
		if got := classifyOutputLevel(tc.stream, tc.text, tc.quiet); got != tc.want {
			t.Fatalf("classifyOutputLevel(%q,%q,quiet=%v)=%q want %q", tc.stream, tc.text, tc.quiet, got, tc.want)
		}
	}
}

func TestClassifyTaskEventLevel(t *testing.T) {
	cases := []struct {
		eventType string
		severity  string
		quiet     bool
		want      string
	}{
		{"task.created", "info", false, "info"},
		{"task.started", "info", false, "info"},
		{"task.completed", "info", false, "info"},
		{"task.failed", "error", false, "error"},
		{"task.failed", "warning", false, "warn"},
		{"task.cancelled", "warning", false, "warn"},
		// Quiet routine task types: creation/success facts are debug noise...
		{"task.created", "info", true, "debug"},
		{"task.started", "info", true, "debug"},
		{"task.completed", "info", true, "debug"},
		{"task.retried", "info", true, "debug"},
		// ...while failures and cancellations stay visible.
		{"task.failed", "error", true, "error"},
		{"task.failed", "warning", true, "error"},
		{"task.cancelled", "warning", true, "warn"},
	}
	for _, tc := range cases {
		if got := classifyTaskEventLevel(tc.eventType, tc.severity, tc.quiet); got != tc.want {
			t.Fatalf("classifyTaskEventLevel(%q,%q,quiet=%v)=%q want %q", tc.eventType, tc.severity, tc.quiet, got, tc.want)
		}
	}
}
