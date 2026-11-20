package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRequiresExplicitMode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != exitUsage {
		t.Fatalf("no args: got exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "--srv") {
		t.Fatalf("usage should mention --srv, got:\n%s", stderr.String())
	}
}

func TestRunRejectsUnknownArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--bogus"}, &stdout, &stderr); code != exitUsage {
		t.Fatalf("unknown arg: got exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("--help: got exit %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout.String(), "--srv") {
		t.Fatalf("help should mention --srv, got:\n%s", stdout.String())
	}
}
