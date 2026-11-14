package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/platform/buildinfo"
)

// newAgentBundleRoot writes a bundle shaped like the one the image build
// produces and points agentBundleRoot at it. It returns the checksum of each
// platform's uncompressed payload.
func newAgentBundleRoot(t *testing.T, payloads map[string][]byte) map[string]string {
	t.Helper()
	root := t.TempDir()
	checksums := make(map[string]string, len(payloads))
	for platform, payload := range payloads {
		dir := filepath.Join(root, platform)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		var archive bytes.Buffer
		writer := gzip.NewWriter(&archive)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, agentBundleArchiveName), archive.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(payload)
		checksum := fmt.Sprintf("%x", sum[:])
		if err := os.WriteFile(filepath.Join(dir, agentBundleChecksumName), []byte(checksum+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		checksums[platform] = checksum
	}
	withAgentBundleRoot(t, root)
	return checksums
}

func TestAgentBundleVersionSegmentFollowsBuildVersion(t *testing.T) {
	original := buildinfo.Version
	t.Cleanup(func() { buildinfo.Version = original })
	checksum := strings.Repeat("a1", 32)
	contentAddressed := "dev-" + checksum[:12]

	cases := []struct {
		version string
		want    string
	}{
		{"v1.4.2", "v1.4.2"},
		{"v1.4.2.20260101120000", "v1.4.2.20260101120000"},
		{"dev", contentAddressed},
		{"", contentAddressed},
		// A version that cannot appear in a URL segment must not reach the path.
		{"bad/version", contentAddressed},
		{"../escape", contentAddressed},
	}
	for _, testCase := range cases {
		buildinfo.Version = testCase.version
		if got := agentBundleVersionSegment(checksum); got != testCase.want {
			t.Fatalf("version %q: expected segment %q, got %q", testCase.version, testCase.want, got)
		}
	}
}

func TestLookupAgentBundleServesOnlyFixedPlatforms(t *testing.T) {
	checksums := newAgentBundleRoot(t, map[string][]byte{
		"linux-amd64": []byte("amd64 payload"),
		"linux-arm64": []byte("arm64 payload"),
	})
	amd64 := agentBundleVersionSegment(checksums["linux-amd64"])
	arm64 := agentBundleVersionSegment(checksums["linux-arm64"])

	if artifact, ok := lookupAgentBundle(amd64, "linux-amd64"); !ok || artifact.SHA256 != checksums["linux-amd64"] {
		t.Fatalf("expected linux-amd64 artifact, got ok=%v artifact=%#v", ok, artifact)
	}
	if artifact, ok := lookupAgentBundle(arm64, "linux-arm64"); !ok || artifact.SHA256 != checksums["linux-arm64"] {
		t.Fatalf("expected linux-arm64 artifact, got ok=%v artifact=%#v", ok, artifact)
	}
	// A version the panel is not serving must not be rewritten into the version
	// it is serving.
	if _, ok := lookupAgentBundle("v0.0.1", "linux-amd64"); ok {
		t.Fatal("expected unknown version to be reported as not found")
	}
	// Only enumerated platforms resolve, so traversal never reaches the disk.
	for _, platform := range []string{"linux-386", "windows-amd64", "..", "../linux-amd64", "linux-amd64/../..", ""} {
		if _, ok := lookupAgentBundle(amd64, platform); ok {
			t.Fatalf("expected platform %q to be rejected", platform)
		}
	}
	for _, version := range []string{"", "..", "../..", amd64 + "/..", "a/../b"} {
		if _, ok := lookupAgentBundle(version, "linux-amd64"); ok {
			t.Fatalf("expected version %q to be rejected", version)
		}
	}
}

func TestLookupAgentBundleRejectsMalformedChecksumFile(t *testing.T) {
	checksums := newAgentBundleRoot(t, map[string][]byte{"linux-amd64": []byte("payload")})
	checksumPath := filepath.Join(agentBundleRoot, "linux-amd64", agentBundleChecksumName)
	// The checksum is interpolated into a remote shell script, so anything that
	// is not canonical lowercase hex must disqualify the artifact entirely.
	for _, malformed := range []string{"", "not-a-hash", strings.Repeat("a", 63), strings.Repeat("A", 64) + "; rm -rf /"} {
		if err := os.WriteFile(checksumPath, []byte(malformed), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := lookupAgentBundleArtifact("linux-amd64"); ok {
			t.Fatalf("expected checksum %q to be rejected", malformed)
		}
	}
	if err := os.WriteFile(checksumPath, []byte(checksums["linux-amd64"]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupAgentBundleArtifact("linux-amd64"); !ok {
		t.Fatal("expected the restored checksum to be accepted")
	}
}

func TestLookupAgentBundleArtifactRequiresArchiveAndChecksum(t *testing.T) {
	newAgentBundleRoot(t, map[string][]byte{"linux-amd64": []byte("payload")})
	if err := os.Remove(filepath.Join(agentBundleRoot, "linux-amd64", agentBundleChecksumName)); err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupAgentBundleArtifact("linux-amd64"); ok {
		t.Fatal("expected a missing checksum file to disqualify the artifact")
	}
}

func TestAgentDownloadURLRequiresABareOrigin(t *testing.T) {
	artifact := agentBundleArtifact{Platform: "linux-amd64", Version: "v1.4.2"}
	suffix := "/agent/v1.4.2/linux-amd64/" + agentBundleArchiveName

	accepted := map[string]string{
		"https://panel.example.test":      "https://panel.example.test" + suffix,
		"https://panel.example.test/":     "https://panel.example.test" + suffix,
		" http://panel.example.test:8443": "http://panel.example.test:8443" + suffix,
	}
	for base, want := range accepted {
		got, ok := agentDownloadURL(base, artifact)
		if !ok {
			t.Fatalf("expected %q to produce a download URL", base)
		}
		if got != want {
			t.Fatalf("base %q: expected %q, got %q", base, want, got)
		}
	}
	for _, base := range []string{
		"",
		"panel.example.test",
		"ftp://panel.example.test",
		"https://panel.example.test/prefix",
		"https://panel.example.test?a=b",
		"https://panel.example.test#fragment",
		"https://user:secret@panel.example.test",
	} {
		if _, ok := agentDownloadURL(base, artifact); ok {
			t.Fatalf("expected base %q to be rejected", base)
		}
	}
}

func TestAgentTransferTimeoutIsBoundedAndTargetIsShorter(t *testing.T) {
	if got := NormalizeAgentTransferTimeout(0); got != agentTransferTimeoutDefault {
		t.Fatalf("expected default timeout, got %s", got)
	}
	if got := NormalizeAgentTransferTimeout(time.Second); got != agentTransferTimeoutMin {
		t.Fatalf("expected the lower bound, got %s", got)
	}
	if got := NormalizeAgentTransferTimeout(24 * time.Hour); got != agentTransferTimeoutMax {
		t.Fatalf("expected the upper bound, got %s", got)
	}
	// The target-side fetcher must always give up first, otherwise the panel
	// would only see a bare remote timeout and could not classify the failure.
	for _, panelBound := range []time.Duration{60 * time.Second, 120 * time.Second, agentTransferTimeoutDefault, time.Hour} {
		budget := agentDownloadTargetBudget(panelBound)
		if budget >= panelBound {
			t.Fatalf("target budget %s must be shorter than the panel bound %s", budget, panelBound)
		}
		// The bound only holds if the whole loop, including the delays between
		// rounds, fits. Asserting a helper's arithmetic alone is exactly how an
		// earlier revision shipped with curl --retry quietly multiplying the
		// worst case past the panel bound: curl grants every retry its own
		// --max-time, so the script's real cost was never the computed one.
		worstCase := time.Duration(agentDownloadRounds)*agentDownloadRoundTimeout(budget) +
			time.Duration(agentDownloadRounds-1)*agentDownloadRetryDelay
		if worstCase >= panelBound {
			t.Fatalf("worst case %s (%d rounds plus delays) must stay strictly inside the panel bound %s", worstCase, agentDownloadRounds, panelBound)
		}
	}
}
