package server

import (
	"context"
	"strings"
	"testing"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
)

func createTailscaleServer(t *testing.T, svc *Service) Server {
	t.Helper()
	srv, err := svc.Create(context.Background(), SaveRequest{
		Name:                        "node-a",
		IPv4:                        "203.0.113.10",
		Port:                        22,
		SSHUsername:                 "du",
		CredentialID:                "cred_1",
		DockerHost:                  agentcontract.DefaultDockerHost,
		TailscaleEnabled:            true,
		TailscalePreferAgent:        true,
		TailscalePreferInterconnect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestCreatePersistsTailscaleIntent(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	srv := createTailscaleServer(t, svc)
	if !srv.TailscaleEnabled || !srv.TailscalePreferAgent || !srv.TailscalePreferInterconnect {
		t.Fatalf("tailscale intent = %#v", srv)
	}
	reloaded, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.TailscaleEnabled || !reloaded.TailscalePreferAgent || !reloaded.TailscalePreferInterconnect {
		t.Fatalf("persisted tailscale intent = %#v", reloaded)
	}
}

func TestUpdateForcesPreferenceOffWhenTailscaleDisabled(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	srv := createTailscaleServer(t, svc)
	updated, err := svc.Update(context.Background(), srv.ID, SaveRequest{
		Name:                        "node-a",
		IPv4:                        "203.0.113.10",
		Port:                        22,
		SSHUsername:                 "du",
		CredentialID:                "cred_1",
		DockerHost:                  agentcontract.DefaultDockerHost,
		TailscaleEnabled:            false,
		TailscalePreferAgent:        true,
		TailscalePreferInterconnect: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.TailscaleEnabled || updated.TailscalePreferAgent || updated.TailscalePreferInterconnect {
		t.Fatalf("disabling tailscale must clear both preferences: %#v", updated)
	}
}

func TestAgentCertificateIncludesTailnetAddressWhenPreferred(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	assets, err := agentsecurity.EnsureTLSAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	srv := createTailscaleServer(t, svc)
	traits := `{"tailscale.ipv4":"100.64.0.7","tailscale.ipv6":"fd7a:115c:a1e0::7"}`
	if _, err := store.AppDB().Exec(`UPDATE servers SET traits=? WHERE id=?`, traits, srv.ID); err != nil {
		t.Fatal(err)
	}

	bundle, err := svc.IssueAgentCertificate(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 公钥束仍然以规范地址为 Agent URL；tailnet 地址只进入证书 SAN。
	if bundle.AgentURL != "https://203.0.113.10:9786" {
		t.Fatalf("agent URL = %q", bundle.AgentURL)
	}
	hosts := splitTraitList(mustGetServer(t, svc, srv.ID).Traits[agentcontract.TraitCertificateHosts])
	if !containsStringValue(hosts, "203.0.113.10") || !containsStringValue(hosts, "100.64.0.7") {
		t.Fatalf("certificate hosts = %#v, want both the canonical and the tailnet address", hosts)
	}
}

func TestAgentCertificateSkipsTailnetAddressWithoutPreference(t *testing.T) {
	svc, _, store := testServerService(t, nil)
	assets, err := agentsecurity.EnsureTLSAssets(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentTLSAssets(assets)
	srv, err := svc.Create(context.Background(), SaveRequest{
		Name:             "node-b",
		IPv4:             "203.0.113.11",
		Port:             22,
		SSHUsername:      "du",
		CredentialID:     "cred_1",
		DockerHost:       agentcontract.DefaultDockerHost,
		TailscaleEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE servers SET traits=? WHERE id=?`, `{"tailscale.ipv4":"100.64.0.8"}`, srv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IssueAgentCertificate(context.Background(), srv.ID); err != nil {
		t.Fatal(err)
	}
	hosts := splitTraitList(mustGetServer(t, svc, srv.ID).Traits[agentcontract.TraitCertificateHosts])
	if containsStringValue(hosts, "100.64.0.8") {
		t.Fatalf("certificate hosts = %#v, want no tailnet address without the preference", hosts)
	}
}

func TestApplyTailscaleReportWritesObservation(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	srv := createTailscaleServer(t, svc)
	status := agentcontract.TailscaleStatus{
		Installed: true, Running: true, LoggedIn: true,
		Hostname: "node-a", IPv4: "100.64.0.7", IPv6: "fd7a:115c:a1e0::7", Version: "1.80.0", BackendState: "Running",
	}
	if err := svc.ApplyTailscaleReport(context.Background(), srv.ID, status); err != nil {
		t.Fatal(err)
	}
	observed := mustGetServer(t, svc, srv.ID).Traits
	if observed[agentcontract.TraitTailscaleStatus] != agentcontract.TailscaleStatusRunning {
		t.Fatalf("status trait = %q", observed[agentcontract.TraitTailscaleStatus])
	}
	if observed[agentcontract.TraitTailscaleIPv4] != "100.64.0.7" || observed[agentcontract.TraitTailscaleIPv6] != "fd7a:115c:a1e0::7" {
		t.Fatalf("address traits = %#v", observed)
	}
	if observed[agentcontract.TraitTailscaleUpdatedAt] == "" {
		t.Fatal("observation must record when it was taken")
	}
}

func TestApplyTailscaleReportIgnoresDisabledNode(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	srv, err := svc.Create(context.Background(), SaveRequest{
		Name:         "node-c",
		IPv4:         "203.0.113.12",
		Port:         22,
		SSHUsername:  "du",
		CredentialID: "cred_1",
		DockerHost:   agentcontract.DefaultDockerHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 节点自行运行 tailscale 不是 Panel 的托管对象：未启用的节点不得产生观测。
	if err := svc.ApplyTailscaleReport(context.Background(), srv.ID, agentcontract.TailscaleStatus{Installed: true, LoggedIn: true, IPv4: "100.64.0.9"}); err != nil {
		t.Fatal(err)
	}
	if got := mustGetServer(t, svc, srv.ID).Traits[agentcontract.TraitTailscaleIPv4]; got != "" {
		t.Fatalf("tailscale.ipv4 = %q, want no observation for a node without the intent", got)
	}
}

func TestApplyTailscaleReportMarksCertificateRefreshOnAddressChange(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	srv := createTailscaleServer(t, svc)
	svc.markAgentStatus(context.Background(), srv.ID, agentcontract.StatusCompatible, "1.80.0", "")
	// 证书当前只为规范地址签发。
	if err := svc.recordAgentCertificateHosts(context.Background(), mustGetServer(t, svc, srv.ID), []string{"203.0.113.10"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApplyTailscaleReport(context.Background(), srv.ID, agentcontract.TailscaleStatus{Installed: true, LoggedIn: true, IPv4: "100.64.0.7"}); err != nil {
		t.Fatal(err)
	}
	observed := mustGetServer(t, svc, srv.ID).Traits
	if observed[agentcontract.TraitStatus] != agentcontract.StatusIncompatible {
		t.Fatalf("agent status = %q, want an explicit certificate refresh request", observed[agentcontract.TraitStatus])
	}
	if !strings.Contains(observed[agentcontract.TraitLastError], "certificate refresh") {
		t.Fatalf("agent last error = %q", observed[agentcontract.TraitLastError])
	}
}

func TestApplyTailscaleReportSkipsCertificateRefreshWithoutPreference(t *testing.T) {
	svc := testServiceForCertificateRefreshTest(t)
	srv, err := svc.Create(context.Background(), SaveRequest{
		Name:             "node-d",
		IPv4:             "203.0.113.13",
		Port:             22,
		SSHUsername:      "du",
		CredentialID:     "cred_1",
		DockerHost:       agentcontract.DefaultDockerHost,
		TailscaleEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.markAgentStatus(context.Background(), srv.ID, agentcontract.StatusCompatible, "1.80.0", ""); err != nil {
		t.Fatal(err)
	}
	before := mustGetServer(t, svc, srv.ID).Traits[agentcontract.TraitStatus]
	if before != agentcontract.StatusCompatible {
		t.Fatalf("precondition failed: agent status = %q", before)
	}
	if err := svc.ApplyTailscaleReport(context.Background(), srv.ID, agentcontract.TailscaleStatus{Installed: true, LoggedIn: true, IPv4: "100.64.0.10"}); err != nil {
		t.Fatal(err)
	}
	if after := mustGetServer(t, svc, srv.ID).Traits[agentcontract.TraitStatus]; after != before {
		t.Fatalf("agent status = %q, want %q: without the preference the certificate needs no tailnet SAN", after, before)
	}
}

func TestTailscaleApplyTaskReportsMissingAgentSupport(t *testing.T) {
	svc, _, _ := testServerService(t, nil)
	srv := createTailscaleServer(t, svc)
	svc.SetAgentClient(&serverFakeAgentClient{health: agentHealth(agentcontract.Version)})
	err := svc.runTailscaleApply(context.Background(), "task-1", srv.ID)
	if err == nil {
		t.Fatal("expected the apply to fail when no agent endpoint is configured")
	}
}

func mustGetServer(t *testing.T, svc *Service, serverID string) Server {
	t.Helper()
	srv, err := svc.Get(context.Background(), serverID)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func testServiceForCertificateRefreshTest(t *testing.T) *Service {
	t.Helper()
	svc, _, _ := testServerService(t, nil)
	return svc
}

func containsStringValue(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
