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
