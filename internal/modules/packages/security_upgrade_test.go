package packages

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
)

// newSecurityUpgradeTestService 构造带一个受支持/免密 sudo 节点的包服务，
// 并注册真实任务定义（安全升级任务由 svc.RegisterTasks 提供）。
func newSecurityUpgradeTestService(t *testing.T) (*Service, *tasks.Service, *sql.DB) {
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
	statements := []string{
		`INSERT INTO credentials(id,name,type,username,created_at,updated_at) VALUES('cred','c','password','du','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`,
		`INSERT INTO servers(id,name,host,port,ssh_username,credential_id,os_id,os_version_id,os_supported,sudo_passwordless,privilege_mode,created_at,updated_at) VALUES('srv','s','h',22,'du','cred','debian','12',1,1,'passwordless_sudo','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := store.AppDB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	taskSvc := tasks.NewService(store.AppDB())
	serverSvc := server.NewService(store.AppDB(), nil, taskSvc)
	svc := NewService(store.AppDB(), serverSvc, nil, taskSvc)
	svc.RegisterTasks(taskSvc)
	return svc, taskSvc, store.AppDB()
}

// securityTestAdapter 可记录升级调用并可阻塞首次升级（维护互斥测试）。
type securityTestAdapter struct {
	mu            sync.Mutex
	listCalls     int
	initial       []linux.PackageUpdate
	remaining     []linux.PackageUpdate
	selectedNames []string
	selectedCalls int
	allCalls      int
	block         bool
	entered       chan struct{}
	release       chan struct{}
	enteredOnce   sync.Once
}

func (a *securityTestAdapter) ListUpgradeable(context.Context, sshx.RemoteExecutor, sshx.Target) ([]linux.PackageUpdate, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listCalls++
	if a.listCalls == 1 {
		return append([]linux.PackageUpdate{}, a.initial...), nil
	}
	return append([]linux.PackageUpdate{}, a.remaining...), nil
}

func (a *securityTestAdapter) UpgradeSelected(_ context.Context, _ sshx.RemoteExecutor, _ sshx.Target, names []string, _ linux.LogSink) error {
	if a.block {
		a.enteredOnce.Do(func() { close(a.entered) })
		<-a.release
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.selectedCalls++
	a.selectedNames = append([]string{}, names...)
	return nil
}

func (a *securityTestAdapter) UpgradeAll(context.Context, sshx.RemoteExecutor, sshx.Target, linux.LogSink) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.allCalls++
	return nil
}

// TestSecurityPackageSelectionOnlyKnownSecurity 锁定选择语义：只有 Agent 权威
// 判定 SecurityKnown 且 Security 的包才升级；未知包（哪怕 Source 像安全源）
// 一律跳过，绝不启发式猜测。
func TestSecurityPackageSelectionOnlyKnownSecurity(t *testing.T) {
	updates := []linux.PackageUpdate{
		{Name: "openssl", Source: "jammy-updates", Security: true, SecurityKnown: true},
		{Name: "curl", Source: "jammy-security", Security: false, SecurityKnown: true},
		{Name: "libc6", Source: "jammy-security"}, // 未知，source 看起来像安全源
		{Name: "bash", Source: "jammy-updates"},   // 未知
	}
	names, unknown := securityPackageSelection(updates)
	wantNames := []string{"openssl"}
	if len(names) != len(wantNames) || names[0] != wantNames[0] {
		t.Fatalf("security names = %#v, want %#v", names, wantNames)
	}
	wantUnknown := []string{"libc6", "bash"}
	if len(unknown) != len(wantUnknown) || unknown[0] != wantUnknown[0] || unknown[1] != wantUnknown[1] {
		t.Fatalf("unknown names = %#v, want %#v", unknown, wantUnknown)
	}
	if names, _ := securityPackageSelection(nil); len(names) != 0 {
		t.Fatalf("empty input must yield no names, got %#v", names)
	}
	if names, _ := securityPackageSelection([]linux.PackageUpdate{{Name: "openssl", Source: "jammy-security"}}); len(names) != 0 {
		t.Fatalf("unknown packages must not be selected, got %#v", names)
	}
}

// TestListUsesPersistedSecurityAuthority 验证 List 原样透传持久化的权威判定：
// 已知非安全的安全源包展示为非安全，未知包既不是安全也不猜测。
func TestListUsesPersistedSecurityAuthority(t *testing.T) {
	svc, _, _ := newSecurityUpgradeTestService(t)
	updates := []linux.PackageUpdate{
		{Name: "authoritative_secure", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: true, SecurityKnown: true},
		{Name: "authoritative_plain", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: false, SecurityKnown: true},
		{Name: "unknown_security_source", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
		{Name: "unknown_plain", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates"},
		// 防御历史脏行：未知状态下即便残留 security=1 也不得展示为安全。
		{Name: "stale_security_bit", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: true},
	}
	if err := svc.replaceUpdates(context.Background(), "srv", updates); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ security, known bool }{
		"authoritative_secure":    {security: true, known: true},
		"authoritative_plain":     {security: false, known: true},
		"unknown_security_source": {security: false, known: false},
		"unknown_plain":           {security: false, known: false},
		"stale_security_bit":      {security: false, known: false},
	}
	if len(list.Updates) != len(want) {
		t.Fatalf("updates = %#v, want %d rows", list.Updates, len(want))
	}
	for _, update := range list.Updates {
		expected, ok := want[update.Name]
		if !ok {
			t.Fatalf("unexpected package %q in list", update.Name)
		}
		if update.Security != expected.security || update.SecurityKnown != expected.known {
			t.Fatalf("update %q security=%v known=%v, want %v/%v (source %q)",
				update.Name, update.Security, update.SecurityKnown, expected.security, expected.known, update.Source)
		}
		delete(want, update.Name)
	}
	if len(want) != 0 {
		t.Fatalf("missing packages in list: %#v", want)
	}

	// API 字段形状：每个更新对象都必须显式带 security 与 securityKnown 布尔字段。
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Updates []map[string]any `json:"updates"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Updates) != len(updates) {
		t.Fatalf("json updates = %#v, want %d items", payload.Updates, len(updates))
	}
	for _, item := range payload.Updates {
		for _, key := range []string{"security", "securityKnown"} {
			value, ok := item[key]
			if !ok {
				t.Fatalf("json update %v is missing %s field: %s", item["name"], key, raw)
			}
			if _, ok := value.(bool); !ok {
				t.Fatalf("json update %v %s = %#v, want bool", item["name"], key, value)
			}
		}
	}
}

// TestSecurityTaskAndListShareClassification 保证列表 Security 标记与
// package_upgrade_security 实际选择的包集合完全一致，防止两处判定漂移。
func TestSecurityTaskAndListShareClassification(t *testing.T) {
	svc, _, _ := newSecurityUpgradeTestService(t)
	updates := []linux.PackageUpdate{
		{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: true, SecurityKnown: true},
		{Name: "curl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: true, SecurityKnown: true},
		{Name: "nginx", InstalledVersion: "1", CandidateVersion: "2", Source: "stable-security", Security: false, SecurityKnown: true},
		{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: false, SecurityKnown: true},
		{Name: "bash", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
	}
	if err := svc.replaceUpdates(context.Background(), "srv", updates); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	fromList := make([]string, 0, len(updates))
	for _, update := range list.Updates {
		if update.Security {
			fromList = append(fromList, update.Name)
		}
	}
	fromTask, _ := securityPackageSelection(updates)
	sort.Strings(fromList)
	sort.Strings(fromTask)
	if len(fromList) != len(fromTask) {
		t.Fatalf("list security packages = %#v, task security packages = %#v", fromList, fromTask)
	}
	for i := range fromList {
		if fromList[i] != fromTask[i] {
			t.Fatalf("list security packages = %#v, task security packages = %#v", fromList, fromTask)
		}
	}
}

// TestUpgradeSecurityUpgradesOnlyKnownSecurityPackages 验证只升级 Agent 明确
// 判定为安全的包；未知包跳过，任务继续完成。
func TestUpgradeSecurityUpgradesOnlyKnownSecurityPackages(t *testing.T) {
	svc, taskSvc, db := newSecurityUpgradeTestService(t)
	adapter := &securityTestAdapter{
		initial: []linux.PackageUpdate{
			{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: true, SecurityKnown: true},
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
			{Name: "curl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: false, SecurityKnown: true},
			{Name: "nginx", InstalledVersion: "1", CandidateVersion: "2", Source: "stable-security", Security: true, SecurityKnown: true},
		},
		remaining: []linux.PackageUpdate{
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
		},
	}
	svc.adapter = adapter

	task, err := svc.UpgradeSecurity(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)

	adapter.mu.Lock()
	selectedNames := append([]string{}, adapter.selectedNames...)
	selectedCalls, allCalls := adapter.selectedCalls, adapter.allCalls
	adapter.mu.Unlock()
	if selectedCalls != 1 || allCalls != 0 {
		t.Fatalf("expected one names upgrade and no upgrade-all, got selected=%d all=%d", selectedCalls, allCalls)
	}
	wantNames := []string{"openssl", "nginx"}
	if len(selectedNames) != len(wantNames) || selectedNames[0] != wantNames[0] || selectedNames[1] != wantNames[1] {
		t.Fatalf("upgraded names = %#v, want %#v", selectedNames, wantNames)
	}

	// 升级后缓存刷新为剩余（未知）更新。
	var cached []string
	rows, err := db.Query(`SELECT name FROM package_updates WHERE server_id='srv' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		cached = append(cached, name)
	}
	if len(cached) != 1 || cached[0] != "libc6" {
		t.Fatalf("package cache after security upgrade = %#v, want [libc6]", cached)
	}
}

// TestUpgradeSecurityNoSecurityUpdatesDoesNotUpgradeAll 验证没有已知安全更新时
// 直接完成，绝不退化为全部升级（未知包同样不升级）。
func TestUpgradeSecurityNoSecurityUpdatesDoesNotUpgradeAll(t *testing.T) {
	svc, taskSvc, db := newSecurityUpgradeTestService(t)
	adapter := &securityTestAdapter{
		initial: []linux.PackageUpdate{
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: false, SecurityKnown: true},
			{Name: "bash", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
		},
		remaining: []linux.PackageUpdate{
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: false, SecurityKnown: true},
			{Name: "bash", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
		},
	}
	svc.adapter = adapter

	task, err := svc.UpgradeSecurity(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)

	latest, err := taskSvc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Summary != "No security updates available" {
		t.Fatalf("summary = %q, want no-security message", latest.Summary)
	}
	adapter.mu.Lock()
	selectedCalls, allCalls := adapter.selectedCalls, adapter.allCalls
	adapter.mu.Unlock()
	if selectedCalls != 0 || allCalls != 0 {
		t.Fatalf("empty security set must not upgrade anything, got selected=%d all=%d", selectedCalls, allCalls)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM package_updates WHERE server_id='srv'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("package cache after no-op security upgrade = %d rows, want 2", count)
	}
}

// TestUpgradeSecuritySharesMaintenanceMutex 验证安全升级复用 per-server 维护互斥：
// 已有升级进行中时第二个任务记录可重试失败，且不触发任何升级副作用。
func TestUpgradeSecuritySharesMaintenanceMutex(t *testing.T) {
	svc, taskSvc, _ := newSecurityUpgradeTestService(t)
	adapter := &securityTestAdapter{
		initial: []linux.PackageUpdate{
			{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: true, SecurityKnown: true},
		},
		remaining: []linux.PackageUpdate{},
		block:     true,
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	svc.adapter = adapter

	first, err := svc.UpgradeSecurity(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-adapter.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first security upgrade did not start")
	}
	second, err := svc.UpgradeSecurity(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, second.ID, tasks.StatusFailedRetryable)
	secondTask, err := taskSvc.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secondTask.Error, "maintenance") {
		t.Fatalf("expected maintenance conflict, got %#v", secondTask)
	}
	adapter.mu.Lock()
	selectedCalls := adapter.selectedCalls
	adapter.mu.Unlock()
	if selectedCalls != 0 {
		t.Fatalf("blocked task must not invoke a second upgrade, got %d selected upgrades", selectedCalls)
	}
	close(adapter.release)
	waitForTaskStatus(t, taskSvc, first.ID, tasks.StatusCompleted)
}

// TestSaveReportedUpdatesFromLegacyAgentStaysUnknown 模拟旧 Agent 推送的包快照
// （不带 security/security_known 字段）：缓存 security_known=0，列表与安全升级
// 任务都保持 unknown，不按 source 猜测升级。
func TestSaveReportedUpdatesFromLegacyAgentStaysUnknown(t *testing.T) {
	svc, _, db := newSecurityUpgradeTestService(t)
	legacy := []linux.PackageUpdate{
		{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
		{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates"},
	}
	if err := svc.SaveReportedUpdates(context.Background(), "srv", legacy); err != nil {
		t.Fatal(err)
	}
	var known int
	if err := db.QueryRow(`SELECT COUNT(*) FROM package_updates WHERE server_id='srv' AND security_known=1`).Scan(&known); err != nil {
		t.Fatal(err)
	}
	if known != 0 {
		t.Fatalf("legacy agent snapshot must not be persisted as authoritative, got %d known rows", known)
	}

	list, err := svc.List(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	for _, update := range list.Updates {
		if update.SecurityKnown {
			t.Fatalf("legacy agent update %q must be unknown, got known=true", update.Name)
		}
		if update.Security {
			t.Fatalf("legacy agent update %q must not be guessed as security", update.Name)
		}
	}

	if names, _ := securityPackageSelection(legacy); len(names) != 0 {
		t.Fatalf("legacy agent task selection must stay empty, got %#v", names)
	}
}

// TestUpgradeSecuritySkipsUnknownPackages 验证未知判定不被启发式升级，已知安全
// 包照常升级，任务不因未知包失败。
func TestUpgradeSecuritySkipsUnknownPackages(t *testing.T) {
	svc, taskSvc, _ := newSecurityUpgradeTestService(t)
	adapter := &securityTestAdapter{
		initial: []linux.PackageUpdate{
			// 权威判定为安全，但 source 看起来不是安全源。
			{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: true, SecurityKnown: true},
			// 权威判定为非安全，即使 source 看起来像安全源也不得升级。
			{Name: "curl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: false, SecurityKnown: true},
			// 未知：source 命中 security 也不得启发式升级。
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security"},
			// 未知：非安全源同样跳过。
			{Name: "bash", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates"},
		},
		remaining: []linux.PackageUpdate{},
	}
	svc.adapter = adapter

	task, err := svc.UpgradeSecurity(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)

	adapter.mu.Lock()
	selectedNames := append([]string{}, adapter.selectedNames...)
	selectedCalls, allCalls := adapter.selectedCalls, adapter.allCalls
	adapter.mu.Unlock()
	if selectedCalls != 1 || allCalls != 0 {
		t.Fatalf("expected one names upgrade and no upgrade-all, got selected=%d all=%d", selectedCalls, allCalls)
	}
	want := []string{"openssl"}
	if len(selectedNames) != len(want) || selectedNames[0] != want[0] {
		t.Fatalf("upgraded names = %#v, want %#v", selectedNames, want)
	}
}

// TestUpgradeSecurityAuthoritativeEmptySetDoesNotUpgradeAll 验证权威判定为
// “已知没有安全更新”（即使 source 启发式会认为有）时任务直接完成，绝不升级。
func TestUpgradeSecurityAuthoritativeEmptySetDoesNotUpgradeAll(t *testing.T) {
	svc, taskSvc, _ := newSecurityUpgradeTestService(t)
	adapter := &securityTestAdapter{
		initial: []linux.PackageUpdate{
			{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: false, SecurityKnown: true},
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: false, SecurityKnown: true},
		},
		remaining: []linux.PackageUpdate{
			{Name: "openssl", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-security", Security: false, SecurityKnown: true},
			{Name: "libc6", InstalledVersion: "1", CandidateVersion: "2", Source: "jammy-updates", Security: false, SecurityKnown: true},
		},
	}
	svc.adapter = adapter

	task, err := svc.UpgradeSecurity(context.Background(), "srv")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)
	adapter.mu.Lock()
	selectedCalls, allCalls := adapter.selectedCalls, adapter.allCalls
	adapter.mu.Unlock()
	if selectedCalls != 0 || allCalls != 0 {
		t.Fatalf("authoritative empty security set must not upgrade anything, got selected=%d all=%d", selectedCalls, allCalls)
	}
}

type fakeSecurityAgent struct {
	mu           sync.Mutex
	requests     []agentcontract.PackageUpgradeRequest
	upgradeOut   string
	remaining    []linux.PackageUpdate
	listCalls    int
}

func (f *fakeSecurityAgent) PackageUpdates(context.Context, string) ([]linux.PackageUpdate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	return append([]linux.PackageUpdate(nil), f.remaining...), nil
}

func (f *fakeSecurityAgent) UpgradePackages(_ context.Context, _ string, req agentcontract.PackageUpgradeRequest) (agentcontract.CommandResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	return agentcontract.CommandResponse{Output: f.upgradeOut}, nil
}

func (*fakeSecurityAgent) UFWAllow(context.Context, string, agentcontract.UFWAllowRequest) (remoteops.UFWStatus, error) {
	return remoteops.UFWStatus{}, nil
}
func (*fakeSecurityAgent) UFWDelete(context.Context, string, agentcontract.UFWDeleteRequest) (remoteops.UFWStatus, error) {
	return remoteops.UFWStatus{}, nil
}
func (*fakeSecurityAgent) UFWConfirmPending(context.Context, string) error { return nil }
func (*fakeSecurityAgent) Fail2BanStatus(context.Context, string) (agentcontract.Fail2BanStatusResponse, error) {
	return agentcontract.Fail2BanStatusResponse{}, nil
}
func (*fakeSecurityAgent) ApplyFail2Ban(context.Context, string, agentcontract.Fail2BanApplyRequest) (agentcontract.Fail2BanStatusResponse, error) {
	return agentcontract.Fail2BanStatusResponse{}, nil
}
func (*fakeSecurityAgent) ReleaseFail2Ban(context.Context, string) (agentcontract.Fail2BanStatusResponse, error) {
	return agentcontract.Fail2BanStatusResponse{}, nil
}
func (*fakeSecurityAgent) RestartSystem(context.Context, string) error { return nil }

// TestUpgradeSecurityWithAgentDelegatesToNativeSecurityOnly 验证生产 Agent 路径下
// package_upgrade_security 直接发送 SecurityOnly=true（不传包名列表、不设 All=true），
// 并在完成后刷新包缓存。
func TestUpgradeSecurityWithAgentDelegatesToNativeSecurityOnly(t *testing.T) {
	svc, taskSvc, db := newSecurityUpgradeTestService(t)
	traitsJSON := `{"agent.status":"compatible","agent.url":"https://127.0.0.1:9786"}`
	if _, err := db.Exec(`UPDATE servers SET traits = ? WHERE id = 'srv'`, traitsJSON); err != nil {
		t.Fatal(err)
	}
	fakeAgent := &fakeSecurityAgent{
		upgradeOut: "Upgrading openssl...\n",
		remaining: []linux.PackageUpdate{
			{Name: "curl", InstalledVersion: "1", CandidateVersion: "2", Source: "bookworm-updates", Security: false, SecurityKnown: true},
		},
	}
	svc.agent = fakeAgent

	task, err := svc.UpgradeSecurity(context.Background(), "srv", "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	waitForTaskStatus(t, taskSvc, task.ID, tasks.StatusCompleted)

	fakeAgent.mu.Lock()
	reqs := append([]agentcontract.PackageUpgradeRequest(nil), fakeAgent.requests...)
	listCalls := fakeAgent.listCalls
	fakeAgent.mu.Unlock()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 UpgradePackages call, got %d", len(reqs))
	}
	if !reqs[0].SecurityOnly || reqs[0].All || len(reqs[0].Names) != 0 {
		t.Fatalf("expected {SecurityOnly:true, All:false, Names:nil}, got %#v", reqs[0])
	}
	if listCalls != 1 {
		t.Fatalf("expected 1 post-upgrade cache refresh, got %d", listCalls)
	}
}
