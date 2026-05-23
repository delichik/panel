package containerrender

import (
	"strings"
	"testing"
)

func TestRenderWrapsServiceBodyAndInjectsSystemLabels(t *testing.T) {
	out, err := Render(Input{
		ServiceID:          "svc_1",
		ServiceName:        "api",
		NodeID:             "srv_1",
		Generation:         3,
		SpecRevision:       "rev",
		RootDir:            "/opt/panel/container-services",
		ComposeProjectName: "panel_managed",
		ComposeServiceYAML: "image: nginx\ncommand: '{{ .variables.CMD }}'\nvolumes:\n  - '{{ .service.data_dir }}:/data'\n",
		Variables:          map[string]string{"CMD": "nginx"},
		Files: []FileInput{{
			Path:    "nginx/default.conf",
			Kind:    FileKindTemplate,
			Content: []byte("server_name {{ .service.name }};"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"services:",
		"api:",
		"nginx",
		"panel.managed: \"true\"",
		"panel.service.id: svc_1",
		"panel.service.name: api",
		"panel.service.generation: \"3\"",
		"panel.service.spec_revision: rev",
		"panel.project: panel_managed",
		"panel.node.id: srv_1",
	} {
		if !strings.Contains(out.OverrideYAML, fragment) && !strings.Contains(out.ComposeYAML, fragment) {
			t.Fatalf("rendered artifacts missing %q\ncompose:\n%s\noverride:\n%s", fragment, out.ComposeYAML, out.OverrideYAML)
		}
	}
	if out.Manifest.Service.CurrentDir != "/opt/panel/container-services/api/current" || out.Manifest.Service.DataDir != "/opt/panel/container-services/api/data" {
		t.Fatalf("unexpected manifest paths: %#v", out.Manifest.Service)
	}
	if len(out.Files) != 1 || out.Files[0].RemotePath != "/opt/panel/container-services/api/current/files/nginx/default.conf" || string(out.Files[0].Content) != "server_name api;" {
		t.Fatalf("unexpected rendered files: %#v", out.Files)
	}
}

func TestRenderFailsOnMissingVariable(t *testing.T) {
	_, err := Render(Input{
		ServiceID:          "svc_1",
		ServiceName:        "api",
		NodeID:             "srv_1",
		Generation:         1,
		SpecRevision:       "rev",
		RootDir:            "/opt/panel/container-services",
		ComposeProjectName: "panel_managed",
		ComposeServiceYAML: "image: '{{ .variables.IMAGE }}'\n",
		Variables:          map[string]string{},
	})
	if err == nil {
		t.Fatal("expected missingkey error")
	}
}
