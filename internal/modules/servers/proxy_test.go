package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/secrets"
	sshx "panel/internal/platform/ssh"
)

// newProxyTestFixture 构造带 secretstore 的服务器服务，用于代理模式（SRV-PROXY-*）
// 的持久化、校验、解析与派生状态测试。
func newProxyTestFixture(t *testing.T) (*Service, *storage.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	secrets, err := secretstore.Open(cfg, store.AppDB())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred_1','c','password','du','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	taskSvc := tasks.NewService(store.AppDB())
	svc := newServerServiceForTest(store, nil, taskSvc, WithSecretStore(secrets))
	return svc, store
}

func createProxyTestServer(t *testing.T, svc *Service, name, ipv4 string, req SaveRequest) Server {
	t.Helper()
	req.Name = name
	req.IPv4 = ipv4
	req.Port = 22
	req.SSHUsername = "du"
	req.CredentialID = "cred_1"
	srv, err := svc.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("create server %s: %v", name, err)
	}
	return srv
}

func errorCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var domain *panelerr.Error
	if !errors.As(err, &domain) {
		t.Fatalf("expected typed panel error, got %T: %v", err, err)
	}
	return domain.Code
}

func TestSocks5PasswordIsEncryptedAndNeverExposed(t *testing.T) {
	svc, store := newProxyTestFixture(t)
	srv := createProxyTestServer(t, svc, "socks", "192.0.2.11", SaveRequest{
		ProxyMode:      sshx.ProxyModeSocks5,
		Socks5Address:  "127.0.0.1:1080",
		Socks5Username: "proxy-user",
		Socks5Password: "proxy-secret",
	})

	var ciphertext string
	if err := store.AppDB().QueryRow(`SELECT socks5_password_ciphertext FROM servers WHERE id=?`, srv.ID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(ciphertext) == "" {
		t.Fatal("expected stored socks5 password ciphertext")
	}
	if ciphertext == "proxy-secret" {
		t.Fatal("socks5 password must not be stored in plaintext")
	}

	got, err := svc.Get(context.Background(), srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProxyMode != sshx.ProxyModeSocks5 || got.Socks5Address != "127.0.0.1:1080" || got.Socks5Username != "proxy-user" {
		t.Fatalf("unexpected proxy fields: %#v", got)
	}
	if !got.Socks5PasswordSet {
		t.Fatal("expected socks5PasswordSet to be derived as true")
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "proxy-secret") || strings.Contains(string(raw), ciphertext) {
		t.Fatalf("server DTO leaked socks5 secret: %s", raw)
	}
	if strings.Contains(string(raw), "socks5Password\"") {
		t.Fatalf("server DTO must not expose a write-only socks5Password field: %s", raw)
	}
}

func TestSocks5PasswordBlankOnUpdateKeepsStoredCiphertext(t *testing.T) {
	svc, store := newProxyTestFixture(t)
	srv := createProxyTestServer(t, svc, "socks", "192.0.2.12", SaveRequest{
		ProxyMode:      sshx.ProxyModeSocks5,
		Socks5Address:  "127.0.0.1:1080",
		Socks5Username: "proxy-user",
		Socks5Password: "proxy-secret",
	})
	readCipher := func() string {
		var value string
		if err := store.AppDB().QueryRow(`SELECT socks5_password_ciphertext FROM servers WHERE id=?`, srv.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := readCipher()
	_, err := svc.Update(context.Background(), srv.ID, SaveRequest{
		Name: "socks", IPv4: "192.0.2.12", Port: 22, SSHUsername: "du", CredentialID: "cred_1",
		ProxyMode: sshx.ProxyModeSocks5, Socks5Address: "127.0.0.1:1080", Socks5Username: "proxy-user",
	})
	if err != nil {
		t.Fatalf("update with blank password: %v", err)
	}
	if after := readCipher(); after != before {
		t.Fatalf("blank password must keep stored ciphertext: before=%q after=%q", before, after)
	}
}

func TestSwitchToDirectClearsProxyConfiguration(t *testing.T) {
	svc, store := newProxyTestFixture(t)
	srv := createProxyTestServer(t, svc, "socks", "192.0.2.13", SaveRequest{
		ProxyMode:      sshx.ProxyModeSocks5,
		Socks5Address:  "127.0.0.1:1080",
		Socks5Username: "proxy-user",
		Socks5Password: "proxy-secret",
	})
	if _, err := svc.Update(context.Background(), srv.ID, SaveRequest{
		Name: "socks", IPv4: "192.0.2.13", Port: 22, SSHUsername: "du", CredentialID: "cred_1",
		ProxyMode: sshx.ProxyModeDirect,
	}); err != nil {
		t.Fatal(err)
	}
	var mode, address, username, ciphertext string
	if err := store.AppDB().QueryRow(`SELECT proxy_mode,COALESCE(socks5_address,''),COALESCE(socks5_username,''),COALESCE(socks5_password_ciphertext,'') FROM servers WHERE id=?`, srv.ID).
		Scan(&mode, &address, &username, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if mode != sshx.ProxyModeDirect || address != "" || username != "" || ciphertext != "" {
		t.Fatalf("direct mode must clear proxy config and secret: mode=%q address=%q username=%q ciphertext=%q", mode, address, username, ciphertext)
	}
}

func TestProxySaveValidation(t *testing.T) {
	svc, _ := newProxyTestFixture(t)
	base := SaveRequest{Name: "s", IPv4: "192.0.2.20", Port: 22, SSHUsername: "du", CredentialID: "cred_1"}

	cases := []struct {
		name string
		req  SaveRequest
		code string
	}{
		{"invalid mode", SaveRequest{Name: "s", IPv4: "192.0.2.21", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: "weird"}, "server_proxy_mode_invalid"},
		{"tunnel required", SaveRequest{Name: "s", IPv4: "192.0.2.22", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSSHTunnel}, "server_proxy_tunnel_required"},
		{"tunnel missing", SaveRequest{Name: "s", IPv4: "192.0.2.23", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: "srv_missing"}, "server_proxy_tunnel_missing"},
		{"socks5 address required", SaveRequest{Name: "s", IPv4: "192.0.2.24", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSocks5}, "server_proxy_socks5_address_required"},
		{"socks5 address invalid", SaveRequest{Name: "s", IPv4: "192.0.2.25", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSocks5, Socks5Address: "not-an-address"}, "server_proxy_socks5_address_invalid"},
		{"socks5 password needs username", SaveRequest{Name: "s", IPv4: "192.0.2.26", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSocks5, Socks5Address: "127.0.0.1:1080", Socks5Password: "p"}, "server_proxy_socks5_username_required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), tc.req); errorCode(t, err) != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
		})
	}
	if _, err := svc.Create(context.Background(), base); err != nil {
		t.Fatalf("direct create: %v", err)
	}
}

func TestSSHTunnelValidationRejectsSelfMissingCycleAndTooDeep(t *testing.T) {
	svc, _ := newProxyTestFixture(t)
	direct := createProxyTestServer(t, svc, "direct", "192.0.2.30", SaveRequest{})
	selfReq := SaveRequest{Name: "self", IPv4: "192.0.2.31", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: direct.ID}
	self, err := svc.Create(context.Background(), selfReq)
	if err != nil {
		t.Fatal(err)
	}
	// 自引用。
	if _, err := svc.Update(context.Background(), self.ID, SaveRequest{Name: "self", IPv4: "192.0.2.31", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: self.ID}); errorCode(t, err) != "server_proxy_tunnel_self" {
		t.Fatalf("expected self tunnel rejection, got %v", err)
	}
	// A -> B -> A 成环。
	a := createProxyTestServer(t, svc, "a", "192.0.2.32", SaveRequest{ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: direct.ID})
	b := createProxyTestServer(t, svc, "b", "192.0.2.33", SaveRequest{ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: a.ID})
	if _, err := svc.Update(context.Background(), direct.ID, SaveRequest{Name: "direct", IPv4: "192.0.2.30", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: b.ID}); errorCode(t, err) != "server_proxy_cycle" {
		t.Fatalf("expected cycle rejection, got %v", err)
	}
	// 8 层允许、9 层拒绝。
	previous := direct
	var chain [9]Server
	for i := 0; i < 8; i++ {
		chain[i] = createProxyTestServer(t, svc, "chain", "192.0.2.4"+string(rune('0'+i)), SaveRequest{ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: previous.ID})
		previous = chain[i]
	}
	if _, err := svc.Create(context.Background(), SaveRequest{Name: "too-deep", IPv4: "192.0.2.60", Port: 22, SSHUsername: "du", CredentialID: "cred_1", ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: previous.ID}); errorCode(t, err) != "server_proxy_chain_too_deep" {
		t.Fatalf("expected chain-too-deep rejection, got %v", err)
	}
}

func TestDeleteTunnelServerMarksDependentProxyInvalid(t *testing.T) {
	svc, _ := newProxyTestFixture(t)
	tunnel := createProxyTestServer(t, svc, "tunnel", "192.0.2.70", SaveRequest{})
	dependent := createProxyTestServer(t, svc, "dependent", "192.0.2.71", SaveRequest{ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: tunnel.ID})
	if err := svc.Delete(context.Background(), tunnel.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), dependent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ProxyInvalid || got.ProxyError != "tunnel_missing" {
		t.Fatalf("expected derived tunnel_missing state, got invalid=%v code=%q", got.ProxyInvalid, got.ProxyError)
	}
	if got.TunnelServerID != tunnel.ID {
		t.Fatalf("delete must not rewrite dependent config: got tunnelServerId=%q want %q", got.TunnelServerID, tunnel.ID)
	}
	summaries, err := svc.ListSummaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range summaries {
		if item.ID == dependent.ID {
			found = true
			if !item.ProxyInvalid || item.ProxyError != "tunnel_missing" {
				t.Fatalf("summary must expose derived proxy invalid state, got %#v", item)
			}
		}
	}
	if !found {
		t.Fatal("expected dependent server in summaries")
	}
}

func TestResolveProxyChainOrdersHopsAndDecryptsSocks5Password(t *testing.T) {
	svc, _ := newProxyTestFixture(t)
	direct := createProxyTestServer(t, svc, "direct", "192.0.2.80", SaveRequest{})
	socks := createProxyTestServer(t, svc, "socks", "192.0.2.81", SaveRequest{
		ProxyMode: sshx.ProxyModeSocks5, Socks5Address: "127.0.0.1:1080", Socks5Username: "u", Socks5Password: "secret",
	})
	tunnel := createProxyTestServer(t, svc, "tunnel", "192.0.2.82", SaveRequest{ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: socks.ID})
	hops, err := svc.ResolveProxyChain(context.Background(), tunnel.ID, sshx.ProxySpec{Mode: sshx.ProxyModeSSHTunnel, TunnelServerID: socks.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(hops) != 2 {
		t.Fatalf("expected socks5 + ssh hops, got %#v", hops)
	}
	if hops[0].Kind != sshx.ProxyHopKindSocks5 || hops[0].Socks5Address != "127.0.0.1:1080" || hops[0].Socks5Password != "secret" {
		t.Fatalf("unexpected socks5 hop: %#v", hops[0])
	}
	if hops[1].Kind != sshx.ProxyHopKindSSH || hops[1].Host != "192.0.2.81" || hops[1].Port != 22 || hops[1].CredentialID != "cred_1" {
		t.Fatalf("unexpected ssh hop: %#v", hops[1])
	}

	// 一个更深的链：top -> tunnel -> direct，hop 顺序为最内侧隧道到最外层。
	top := createProxyTestServer(t, svc, "top", "192.0.2.83", SaveRequest{ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: tunnel.ID})
	deep, err := svc.ResolveProxyChain(context.Background(), top.ID, sshx.ProxySpec{Mode: sshx.ProxyModeSSHTunnel, TunnelServerID: tunnel.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(deep) != 3 || deep[0].Kind != sshx.ProxyHopKindSocks5 || deep[1].Host != "192.0.2.81" || deep[2].Host != "192.0.2.82" {
		t.Fatalf("unexpected deep chain: %#v", deep)
	}
	_ = direct
}

func TestResolveProxyChainRejectsCorruptedCycle(t *testing.T) {
	svc, store := newProxyTestFixture(t)
	a := createProxyTestServer(t, svc, "a", "192.0.2.90", SaveRequest{})
	b := createProxyTestServer(t, svc, "b", "192.0.2.91", SaveRequest{})
	// 绕过保存校验直接写入成环数据，模拟手工改库/旧数据。
	if _, err := store.AppDB().Exec(`UPDATE servers SET proxy_mode='ssh_tunnel',tunnel_server_id=? WHERE id=?`, b.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppDB().Exec(`UPDATE servers SET proxy_mode='ssh_tunnel',tunnel_server_id=? WHERE id=?`, a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	_, err := svc.ResolveProxyChain(context.Background(), a.ID, sshx.ProxySpec{Mode: sshx.ProxyModeSSHTunnel, TunnelServerID: b.ID})
	if errorCode(t, err) != "server_proxy_cycle" {
		t.Fatalf("expected cycle error, got %v", err)
	}
	got, err := svc.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ProxyInvalid || got.ProxyError != "cycle" {
		t.Fatalf("expected derived cycle invalid state, got %#v", got.ProxyError)
	}
}

func TestProbeValidationRejectsSelfTunnel(t *testing.T) {
	svc, _ := newProxyTestFixture(t)
	srv := createProxyTestServer(t, svc, "s", "192.0.2.95", SaveRequest{})
	svc.exec = &proxyDialRecordingExecutor{}
	_, err := svc.ProbeConnectivity(context.Background(), SaveRequest{
		IPv4: "192.0.2.95", Port: 22, SSHUsername: "du", CredentialID: "cred_1",
		ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: srv.ID, ProbeServerID: srv.ID,
	})
	if errorCode(t, err) != "server_proxy_tunnel_self" {
		t.Fatalf("expected probe self-tunnel rejection, got %v", err)
	}
}

type proxyDialRecordingExecutor struct {
	serverID string
	spec     sshx.ProxySpec
	address  string
	network  string
}

func (f *proxyDialRecordingExecutor) Exec(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, errors.New("not used")
}

func (f *proxyDialRecordingExecutor) ExecSudo(context.Context, sshx.Target, sshx.CommandSpec) (sshx.CommandResult, error) {
	return sshx.CommandResult{}, errors.New("not used")
}

func (f *proxyDialRecordingExecutor) Upload(context.Context, sshx.Target, sshx.UploadSpec) error {
	return errors.New("not used")
}

func (f *proxyDialRecordingExecutor) Download(context.Context, sshx.Target, sshx.DownloadSpec) error {
	return errors.New("not used")
}

func (f *proxyDialRecordingExecutor) DialThroughProxy(_ context.Context, network, address, serverID string, spec sshx.ProxySpec) (net.Conn, error) {
	f.network, f.address, f.serverID, f.spec = network, address, serverID, spec
	client, server := net.Pipe()
	_ = server.Close()
	return client, nil
}

func TestDialContextMatchesAgentAddressAndUsesProxyChain(t *testing.T) {
	svc, store := newProxyTestFixture(t)
	recorder := &proxyDialRecordingExecutor{}
	svc.exec = recorder
	tunnel := createProxyTestServer(t, svc, "tunnel", "192.0.2.100", SaveRequest{})
	srv := createProxyTestServer(t, svc, "target", "192.0.2.101", SaveRequest{
		ProxyMode: sshx.ProxyModeSSHTunnel, TunnelServerID: tunnel.ID,
	})
	traits := `{"agent.url":"http://192.0.2.101:9786","agent.enabled":"true"}`
	if _, err := store.AppDB().Exec(`UPDATE servers SET traits=? WHERE id=?`, traits, srv.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := svc.DialContext(context.Background(), "tcp", "192.0.2.101:9786")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if recorder.serverID != srv.ID || recorder.address != "192.0.2.101:9786" {
		t.Fatalf("unexpected proxy dial: serverID=%q address=%q", recorder.serverID, recorder.address)
	}
	if recorder.spec.Mode != sshx.ProxyModeSSHTunnel || recorder.spec.TunnelServerID != tunnel.ID {
		t.Fatalf("unexpected proxy spec: %#v", recorder.spec)
	}
	// 未知 agent 地址（例如非本 Panel 管理的端点）回落直连，不经过代理。
	direct := &proxyDialRecordingExecutor{}
	svc.exec = direct
	if conn, err := svc.DialContext(context.Background(), "tcp", "127.0.0.1:1"); err == nil {
		_ = conn.Close()
		t.Fatal("expected direct dial to fail in tests")
	}
	if direct.network != "" {
		t.Fatal("unknown agent address must not use the proxy dialer")
	}
}

func TestProxyChainPreservesOrderedHopsBound(t *testing.T) {
	// 解析层保护：手工构造超过 hop 上限的链时，解析必须失败而不是无限展开。
	svc, store := newProxyTestFixture(t)
	ids := make([]string, 0, sshx.MaxProxyChainHops+2)
	for i := 0; i < sshx.MaxProxyChainHops+2; i++ {
		ip := "192.0.3." + itoaForTest(i+1)
		srv := createProxyTestServer(t, svc, "chain", ip, SaveRequest{})
		ids = append(ids, srv.ID)
	}
	for i := 1; i < len(ids); i++ {
		if _, err := store.AppDB().Exec(`UPDATE servers SET proxy_mode='ssh_tunnel',tunnel_server_id=? WHERE id=?`, ids[i-1], ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	_, err := svc.ResolveProxyChain(context.Background(), ids[len(ids)-1], sshx.ProxySpec{Mode: sshx.ProxyModeSSHTunnel, TunnelServerID: ids[len(ids)-2]})
	if errorCode(t, err) != "server_proxy_chain_too_deep" {
		t.Fatalf("expected chain-too-deep, got %v", err)
	}
}

func itoaForTest(value int) string {
	if value < 10 {
		return string(rune('0' + value))
	}
	return "1" + string(rune('0'+value-10))
}

var _ = time.Second
var _ = sql.ErrNoRows
