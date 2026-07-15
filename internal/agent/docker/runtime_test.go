package docker

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	agentcontract "panel/internal/agent/contract"
	appruntime "panel/internal/modules/applications/runtime"
)

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
