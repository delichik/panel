package docker

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
)

func TestManagedArchiveLimitsRejectCountDepthSizeAndRatio(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		count      int
		extracted  int64
		compressed int64
	}{
		{name: "count", path: "a", count: managedArchiveMaxFiles + 1, extracted: 1, compressed: 1},
		{name: "depth", path: strings.Repeat("a/", managedArchiveMaxDepth) + "x", count: 1, extracted: 1, compressed: 1},
		{name: "size", path: "a", count: 1, extracted: managedArchiveMaxExtracted + 1, compressed: managedArchiveMaxExtracted},
		{name: "ratio", path: "a", count: 1, extracted: 2 << 20, compressed: 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateManagedArchiveLimits(tc.path, tc.count, tc.extracted, tc.compressed); err == nil {
				t.Fatal("expected managed archive limit error")
			}
		})
	}
}

func TestManagedArchiveRejectsEmptyDirectoryFlood(t *testing.T) {
	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)
	for i := 0; i <= managedArchiveMaxFiles; i++ {
		header := &zip.FileHeader{Name: fmt.Sprintf("d%05d/", i), Method: zip.Store}
		if _, err := zw.CreateHeader(header); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw.Bytes()), int64(raw.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extractManagedZip(zr, t.TempDir(), int64(raw.Len()), managedArchiveOwner{}); err == nil {
		t.Fatal("expected entry limit error")
	}
}

func TestManagedTarRejectsEmptyDirectoryFlood(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := 0; i <= managedArchiveMaxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("d%05d/", i), Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractManagedTar(tar.NewReader(bytes.NewReader(raw.Bytes())), t.TempDir(), int64(raw.Len()), managedArchiveOwner{}); err == nil {
		t.Fatal("expected entry limit error")
	}
}

func TestManagedTarRejectsIgnoredHeaderFlood(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := 0; i <= managedArchiveMaxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("l%05d", i), Typeflag: tar.TypeSymlink, Linkname: "target", Mode: 0o777}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractManagedTar(tar.NewReader(bytes.NewReader(raw.Bytes())), t.TempDir(), int64(raw.Len()), managedArchiveOwner{}); err == nil {
		t.Fatal("expected entry limit error")
	}
}

func TestDockerAPIClientPullImagePassesExplicitLatestTag(t *testing.T) {
	latest := "latest"
	v1 := "v1"
	cases := []struct {
		name      string
		image     string
		fromImage string
		tag       *string
	}{
		{name: "single name defaults latest", image: "nginx", fromImage: "nginx", tag: &latest},
		{name: "repository defaults latest", image: "team/web", fromImage: "team/web", tag: &latest},
		{name: "explicit tag", image: "team/web:v1", fromImage: "team/web", tag: &v1},
		{name: "registry port defaults latest", image: "registry.example.com:5000/team/web", fromImage: "registry.example.com:5000/team/web", tag: &latest},
		{name: "registry port explicit tag", image: "registry.example.com:5000/team/web:v1", fromImage: "registry.example.com:5000/team/web", tag: &v1},
		{name: "digest reference preserved", image: "team/web@sha256:abc123", fromImage: "team/web@sha256:abc123"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries := make(chan url.Values, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/images/create" {
					t.Errorf("path = %s, want /images/create", r.URL.Path)
				}
				queries <- r.URL.Query()
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			client := &dockerAPIClient{host: server.URL, pullClient: server.Client()}
			if err := client.pullImage(context.Background(), tc.image); err != nil {
				t.Fatalf("pullImage() error = %v", err)
			}

			query := <-queries
			if got := query.Get("fromImage"); got != tc.fromImage {
				t.Fatalf("fromImage = %q, want %q", got, tc.fromImage)
			}
			gotTags, hasTag := query["tag"]
			if tc.tag == nil {
				if hasTag {
					t.Fatalf("tag query = %#v, want absent", gotTags)
				}
				return
			}
			if !hasTag || len(gotTags) != 1 || gotTags[0] != *tc.tag {
				t.Fatalf("tag query = %#v, want %q", gotTags, *tc.tag)
			}
		})
	}
}

func TestDockerAPIClientCreateContainerOmitsRestartPolicy(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/containers/create" {
			t.Errorf("path = %s, want /containers/create", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"container-1"}`))
	}))
	defer server.Close()

	client := &dockerAPIClient{host: server.URL, client: server.Client()}
	_, err := client.createContainer(context.Background(), appruntime.Spec{
		Image:         "nginx:1.27",
		ContainerName: "panel-web",
		Restart:       appruntime.Restart{Policy: "always"},
	})
	if err != nil {
		t.Fatalf("createContainer() error = %v", err)
	}

	body := <-bodies
	hostConfig, ok := body["HostConfig"].(map[string]any)
	if !ok {
		t.Fatalf("HostConfig = %#v", body["HostConfig"])
	}
	if _, ok := hostConfig["RestartPolicy"]; ok {
		t.Fatalf("RestartPolicy was sent in create payload: %#v", hostConfig["RestartPolicy"])
	}
}

func TestWriteManagedFilesRemovesStaleManagedFiles(t *testing.T) {
	runtime := &LocalRuntime{root: t.TempDir()}
	spec := appruntime.Spec{ApplicationID: "app", InstanceID: "instance", Files: []appruntime.ManagedFile{
		{Path: "nginx/nginx.conf", Content: []byte("first"), Mode: "0644"},
		{Path: "nginx/conf.d/old.conf", Content: []byte("old"), Mode: "0644"},
	}}
	if err := runtime.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	spec.Files = []appruntime.ManagedFile{{Path: "nginx/nginx.conf", Content: []byte("second"), Mode: "0644"}}
	if err := runtime.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	oldPath, err := safeRuntimePath(runtime.root, "app", "instance", "files", "nginx/conf.d/old.conf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale managed file still exists: %v", err)
	}
	mainPath, _ := safeRuntimePath(runtime.root, "app", "instance", "files", "nginx/nginx.conf")
	content, err := os.ReadFile(mainPath)
	if err != nil || string(content) != "second" {
		t.Fatalf("managed file = %q, %v", content, err)
	}
}

func TestAppliedStateMatchesContainerIdentity(t *testing.T) {
	state := appliedState{ContainerID: "container-1", ContainerName: "panel-nginx", Generation: 2, SpecHash: "hash"}
	if !state.matches(agentcontract.DockerContainer{ID: "container-1", Names: []string{"/panel-nginx"}}) {
		t.Fatal("matching applied state was rejected")
	}
	if state.matches(agentcontract.DockerContainer{ID: "container-2", Names: []string{"/panel-nginx"}}) {
		t.Fatal("applied state from another container was accepted")
	}
}

func TestDockerAPIClientCreateContainerSendsCapAdd(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Id":"container-1"}`))
	}))
	defer server.Close()

	client := &dockerAPIClient{host: server.URL, client: server.Client()}
	_, err := client.createContainer(context.Background(), appruntime.Spec{
		Image:         "nginx:1.27",
		ContainerName: "panel-web",
		CapAdd:        []string{"NET_ADMIN", "SYS_TIME"},
	})
	if err != nil {
		t.Fatalf("createContainer() error = %v", err)
	}

	body := <-bodies
	hostConfig, ok := body["HostConfig"].(map[string]any)
	if !ok {
		t.Fatalf("HostConfig = %#v", body["HostConfig"])
	}
	got, ok := hostConfig["CapAdd"].([]any)
	if !ok || len(got) != 2 || got[0] != "NET_ADMIN" || got[1] != "SYS_TIME" {
		t.Fatalf("CapAdd = %#v", hostConfig["CapAdd"])
	}
}

func TestLocalRuntimeStatusReportsMissingWhenContainerNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/containers/panel-web/json" {
			t.Errorf("path = %s, want /containers/panel-web/json", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container"}`))
	}))
	defer server.Close()

	r := &LocalRuntime{client: &dockerAPIClient{host: server.URL, client: server.Client()}}
	status, err := r.Status(context.Background(), "app-1-server-1", "panel-web", "server-1")
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != appruntime.StatusMissing || status.DesiredState != appruntime.DesiredRunning {
		t.Fatalf("status = %#v", status)
	}
}

func TestPreparePersistentMountsCreatesManagedDirectory(t *testing.T) {
	root := t.TempDir()
	appID := "app-1"
	source := filepath.Join(root, appID, "persistent", "data", "logs")
	r := &LocalRuntime{root: root}
	spec := appruntime.Spec{
		ApplicationID: appID,
		Mounts: []appruntime.Mount{{
			Type:   "persistent",
			Source: source,
			Target: "/opt/data/logs",
			Mode:   "0755",
		}},
	}

	if err := r.preparePersistentMounts(spec); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("persistent mount source is not a directory: %s", source)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestWriteManagedFilesAppliesFileMode(t *testing.T) {
	root := t.TempDir()
	r := &LocalRuntime{root: root}
	target := filepath.Join(root, "app-1", "instances", "app-1-srv-a", "files", "bin", "start.sh")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := appruntime.Spec{
		ApplicationID: "app-1",
		InstanceID:    "app-1-srv-a",
		Files: []appruntime.ManagedFile{{
			Path:    "bin/start.sh",
			Content: []byte("#!/bin/sh\n"),
			Mode:    "0755",
		}},
	}

	if err := r.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestWriteManagedFilesMakesDirectoriesTraversable(t *testing.T) {
	root := t.TempDir()
	r := &LocalRuntime{root: root}
	parent := filepath.Join(root, "app-1", "instances", "app-1-srv-a", "files", "static-assets", "asset-1")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	spec := appruntime.Spec{
		ApplicationID: "app-1",
		InstanceID:    "app-1-srv-a",
		Files: []appruntime.ManagedFile{{
			Path:    "static-assets/asset-1/index.html",
			Content: []byte("ok"),
			Mode:    "0644",
		}},
	}

	if err := r.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("parent mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestWriteManagedArchiveKeepsArchiveAndOverwritesExtractedFiles(t *testing.T) {
	root := t.TempDir()
	r := &LocalRuntime{root: root}
	content := testZipArchive(t, map[string]string{"index.html": "<h1>ok</h1>"})
	spec := appruntime.Spec{
		ApplicationID: "app-1",
		InstanceID:    "app-1-srv-a",
		Files: []appruntime.ManagedFile{{
			Kind:    appruntime.ManagedFileKindArchive,
			Path:    "public",
			Content: content,
		}},
	}

	if err := r.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "app-1", "instances", "app-1-srv-a", "archives", "public.archive")
	extractedPath := filepath.Join(root, "app-1", "instances", "app-1-srv-a", "files", "public", "index.html")
	if got, err := os.ReadFile(archivePath); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("archive content = %q err=%v", got, err)
	}
	if got, err := os.ReadFile(extractedPath); err != nil || string(got) != "<h1>ok</h1>" {
		t.Fatalf("extracted content = %q err=%v", got, err)
	}
	if hash, drifted, err := r.managedFilesDrift("app-1", "app-1-srv-a"); err != nil || hash == "" || drifted {
		t.Fatalf("managed files should be healthy after write: hash=%q drifted=%v err=%v", hash, drifted, err)
	}

	if err := os.WriteFile(archivePath, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extractedPath, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, drifted, err := r.managedFilesDrift("app-1", "app-1-srv-a"); err != nil || !drifted {
		t.Fatalf("managed files should drift after node-side changes: drifted=%v err=%v", drifted, err)
	}
	if err := r.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(archivePath); err != nil || !bytes.Equal(got, content) {
		t.Fatalf("rewritten archive content = %q err=%v", got, err)
	}
	if got, err := os.ReadFile(extractedPath); err != nil || string(got) != "<h1>ok</h1>" {
		t.Fatalf("restored extracted content = %q err=%v", got, err)
	}
	if _, drifted, err := r.managedFilesDrift("app-1", "app-1-srv-a"); err != nil || drifted {
		t.Fatalf("managed files should be healthy after rewrite: drifted=%v err=%v", drifted, err)
	}
}

func testZipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		writer, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPreparePersistentMountsRejectsEscapedDirectory(t *testing.T) {
	root := t.TempDir()
	r := &LocalRuntime{root: root}
	err := r.preparePersistentMounts(appruntime.Spec{
		ApplicationID: "app-1",
		Mounts: []appruntime.Mount{{
			Type:   "persistent",
			Source: filepath.Join(root, "other", "data"),
			Target: "/data",
		}},
	})
	if err == nil {
		t.Fatal("expected escaped persistent mount path to be rejected")
	}
}

func TestManagedFilesDriftUsesFingerprintCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("managed-file mode semantics differ on Windows")
	}
	root := t.TempDir()
	r := &LocalRuntime{root: root}
	spec := appruntime.Spec{
		ApplicationID: "app-1",
		InstanceID:    "app-1-srv-a",
		Files: []appruntime.ManagedFile{
			{Path: "bin/start.sh", Content: []byte("#!/bin/sh\n"), Mode: "0755"},
			{Kind: appruntime.ManagedFileKindArchive, Path: "public", Content: testZipArchive(t, map[string]string{"index.html": "<h1>ok</h1>"})},
		},
	}
	if err := r.writeManagedFiles(spec); err != nil {
		t.Fatal(err)
	}
	if _, drifted, err := r.managedFilesDrift("app-1", "app-1-srv-a"); err != nil || drifted {
		t.Fatalf("expected healthy after write: drifted=%v err=%v", drifted, err)
	}
	cachePath := filepath.Join(root, "app-1", "instances", "app-1-srv-a", "state", managedFingerprintsPath)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("fingerprint cache should exist after first drift check: %v", err)
	}
	// A metadata-only touch must not count as drift and must refresh the cache.
	target := filepath.Join(root, "app-1", "instances", "app-1-srv-a", "files", "bin", "start.sh")
	now := time.Now()
	if err := os.Chtimes(target, now, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, drifted, err := r.managedFilesDrift("app-1", "app-1-srv-a"); err != nil || drifted {
		t.Fatalf("touch should not count as drift: drifted=%v err=%v", drifted, err)
	}
	// A content change must still be detected.
	if err := os.WriteFile(target, []byte("changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, drifted, err := r.managedFilesDrift("app-1", "app-1-srv-a"); err != nil || !drifted {
		t.Fatalf("content change should be detected: drifted=%v err=%v", drifted, err)
	}
}

func TestDecodeDockerLogsReaderStreamsFrames(t *testing.T) {
	var raw bytes.Buffer
	for _, payload := range [][]byte{[]byte("hello"), []byte("world")} {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
		raw.Write(header)
		raw.Write(payload)
	}
	got, err := decodeDockerLogsReader(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != "helloworld" {
		t.Fatalf("decoded = %q, want %q", got, "helloworld")
	}
}

func TestDecodeDockerLogsReaderStopsOnOversizedFrame(t *testing.T) {
	var raw bytes.Buffer
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[4:8], uint32(maxDockerLogFrameBytes+1))
	raw.Write(header)
	raw.Write(make([]byte, 16))
	got, err := decodeDockerLogsReader(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("decoded = %q, want empty when frame exceeds per-frame cap", got)
	}
}

func TestDecodeDockerLogsReaderReturnsPartialOnTruncatedFrame(t *testing.T) {
	var raw bytes.Buffer
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[4:8], 100)
	raw.Write(header)
	raw.Write([]byte("short"))
	got, err := decodeDockerLogsReader(bytes.NewReader(raw.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got != "short" {
		t.Fatalf("decoded = %q, want %q", got, "short")
	}
}

func TestRestorePersistentArchiveSwapsAtomicallyAndPreservesOldOnFailure(t *testing.T) {
	root := t.TempDir()
	r := &LocalRuntime{root: root}
	ctx := context.Background()
	dir := filepath.Join(root, "app-1", "persistent")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	archive := testZipArchive(t, map[string]string{"new.txt": "hello", "sub/a.txt": "nested"})
	if err := r.RestorePersistentArchive(ctx, "app-1", archive); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file still present after restore: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("new file = %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "a.txt")); err != nil {
		t.Fatalf("nested file missing: %v", err)
	}

	// An archive with an escaping path fails validation and must keep the
	// previously restored tree intact.
	bad := testZipArchive(t, map[string]string{"../escape.txt": "x"})
	if err := r.RestorePersistentArchive(ctx, "app-1", bad); err == nil {
		t.Fatal("expected escaping archive to be rejected")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("existing tree was damaged by failed restore: %q err=%v", got, err)
	}

	// Cancellation before extraction must also preserve the directory.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.RestorePersistentArchive(cancelled, "app-1", archive); err == nil {
		t.Fatal("expected cancelled restore to fail")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("existing tree was damaged by cancelled restore: %q err=%v", got, err)
	}
}