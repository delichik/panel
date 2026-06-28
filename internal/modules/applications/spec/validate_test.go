package appspec

import "testing"

func TestValidateRejectsInvalidName(t *testing.T) {
	issues := Validate(Spec{Name: "Bad_Name", Image: "nginx"})
	if !hasIssue(issues, "name") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsMissingImage(t *testing.T) {
	issues := Validate(Spec{Name: "web"})
	if !hasIssue(issues, "image") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsCommandWithMultipleItems(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Command: []string{"nginx", "-g", "daemon off;"}})
	if !hasIssue(issues, "command") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestNormalizeDropsBlankCommandItems(t *testing.T) {
	spec := Normalize(Spec{Name: "web", Image: "nginx", Command: []string{" "}, Args: []string{"", " --debug "}})
	if len(spec.Command) != 0 {
		t.Fatalf("command = %#v", spec.Command)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "--debug" {
		t.Fatalf("args = %#v", spec.Args)
	}
}

func TestNormalizeDefaultsRestartPolicyToNo(t *testing.T) {
	spec := Normalize(Spec{Name: "web", Image: "nginx"})
	if spec.Restart.Policy != "no" {
		t.Fatalf("restart policy = %q, want no", spec.Restart.Policy)
	}
}

func TestValidateRejectsInvalidPortRange(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Ports: []Port{{Label: "http", To: 70000}}})
	if !hasIssue(issues, "ports[0].to") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsCheckReferencingMissingPort(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Checks: []Check{{Name: "http", Type: "http", Port: "admin"}}})
	if !hasIssue(issues, "checks[0].port") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsServiceReferencingMissingPort(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Services: []Service{{Name: "web", Port: "admin"}}})
	if !hasIssue(issues, "services[0].port") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsRelativeVolumeTarget(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Volumes: []Volume{{Source: "data", Target: "var/www"}}})
	if !hasIssue(issues, "volumes[0].target") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsInvalidMountSource(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Mounts: []Mount{{Type: "file", Source: "../secret", Target: "/etc/secret"}}})
	if !hasIssue(issues, "mounts[0].source") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsInvalidRestartPolicy(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", Restart: Restart{Policy: "sometimes"}})
	if !hasIssue(issues, "restart.policy") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsPortsWithHostNetwork(t *testing.T) {
	issues := Validate(Spec{Name: "web", Image: "nginx", NetworkMode: "host", Ports: []Port{{Label: "http", To: 80}}})
	if !hasIssue(issues, "ports") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestDecodeYAMLReturnsIssueForMalformedYAML(t *testing.T) {
	_, issues := DecodeYAML("name: [")
	if !hasIssue(issues, "specYaml") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateAcceptsKeyAssetPanelFileSource(t *testing.T) {
	issues := Validate(Spec{
		Name:  "web",
		Image: "nginx",
		Mounts: []Mount{{
			Type:   "panel_file",
			Source: "key_asset:key_1:ssh_public_key",
			Target: "/etc/ssh/key.pub",
		}},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateAcceptsCertificatePanelFileSource(t *testing.T) {
	issues := Validate(Spec{
		Name:  "web",
		Image: "nginx",
		Mounts: []Mount{{
			Type:   "panel_file",
			Source: "certificate:cert_1:private_key",
			Target: "/etc/ssl/private/key.pem",
		}},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidateRejectsInvalidCertificatePanelFileKind(t *testing.T) {
	issues := Validate(Spec{
		Name:  "web",
		Image: "nginx",
		Mounts: []Mount{{
			Type:   "panel_file",
			Source: "certificate:cert_1:public_key",
			Target: "/etc/ssl/public.pem",
		}},
	})
	if !hasIssue(issues, "mounts[0].source") {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestValidatePersistentMountPermissions(t *testing.T) {
	negative := -1
	issues := Validate(Spec{Name: "web", Image: "nginx", Mounts: []Mount{{Type: "persistent", Source: "data", Target: "/data", UID: &negative, Mode: "bad"}}})
	if !hasIssue(issues, "mounts[0].uid") || !hasIssue(issues, "mounts[0].mode") {
		t.Fatalf("issues = %#v", issues)
	}

	uid := 1000
	issues = Validate(Spec{Name: "web", Image: "nginx", Mounts: []Mount{{Type: "host", Source: "/srv/data", Target: "/data", UID: &uid}}})
	if !hasIssue(issues, "mounts[0]") {
		t.Fatalf("issues = %#v", issues)
	}

	issues = Validate(Spec{Name: "web", Image: "nginx", Mounts: []Mount{{Type: "persistent", Source: "data", Target: "/data", UID: &uid, Mode: "0755"}}})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
}

func hasIssue(issues []Issue, field string) bool {
	for _, issue := range issues {
		if issue.Field == field {
			return true
		}
	}
	return false
}
