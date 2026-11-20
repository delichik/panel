package applications

import (
	"context"
	"encoding/json"
	"testing"
)

// 对象级“允许自动更新”开关（APP-IMG-003 / 方案 §3.6）：
// - 旧数据/缺省 = true；
// - DTO 保存往返不丢；
// - 更新未提交时保留当前值，旧客户端整笔保存不会把开关静默关掉。
func TestApplicationAutoUpdateEnabledDefaultsTrueAndRoundTrips(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()

	app, err := svc.Create(ctx, SaveInput{Name: "web", Enabled: false, SpecYAML: "name: web\nimage: nginx\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !app.AutoUpdateEnabled {
		t.Fatalf("new application must default autoUpdateEnabled=true, got %#v", app)
	}
	if got := applicationAutoUpdateColumn(t, svc, app.ID); got != 1 {
		t.Fatalf("auto_update_enabled column = %d, want 1", got)
	}

	page, err := svc.ListSummaries(ctx, 1, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || !page.Items[0].AutoUpdateEnabled {
		t.Fatalf("list summary must expose autoUpdateEnabled=true, got %#v", page.Items)
	}

	// 显式关闭并落库。
	off := false
	disabled, err := svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML, AutoUpdateEnabled: &off,
	})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.AutoUpdateEnabled {
		t.Fatalf("explicit false was not persisted, got %#v", disabled)
	}
	if got := applicationAutoUpdateColumn(t, svc, app.ID); got != 0 {
		t.Fatalf("auto_update_enabled column = %d after disable, want 0", got)
	}

	// 旧客户端/旧草稿不带字段（nil）：更新必须保留当前 false，而不是回落 true。
	preserved, err := svc.Update(ctx, app.ID, SaveInput{Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML})
	if err != nil {
		t.Fatal(err)
	}
	if preserved.AutoUpdateEnabled {
		t.Fatalf("omitted autoUpdateEnabled must preserve current false, got %#v", preserved)
	}

	// 重新开启。
	on := true
	enabled, err := svc.Update(ctx, app.ID, SaveInput{
		Name: app.Name, Enabled: false, SpecYAML: app.SpecYAML, AutoUpdateEnabled: &on,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.AutoUpdateEnabled {
		t.Fatalf("explicit true was not persisted, got %#v", enabled)
	}

	// 创建时显式 false 同样生效。
	createdOff, err := svc.Create(ctx, SaveInput{
		Name: "no-auto", Enabled: false, SpecYAML: "name: no-auto\nimage: nginx\n", AutoUpdateEnabled: &off,
	})
	if err != nil {
		t.Fatal(err)
	}
	if createdOff.AutoUpdateEnabled {
		t.Fatalf("create with explicit false = true")
	}
}

func applicationAutoUpdateColumn(t *testing.T, svc *Service, appID string) int {
	t.Helper()
	var value int
	if err := svc.db.QueryRow(`SELECT auto_update_enabled FROM applications WHERE id=?`, appID).Scan(&value); err != nil {
		t.Fatalf("read auto_update_enabled for %s: %v", appID, err)
	}
	return value
}

// 编辑会话 draft 必须显式携带开关；升级前落库的旧 draft 缺少键时保持 nil
// （提交时按“未提交”语义回落/保留），不能被 unmarshal 成 false。
func TestApplicationEditDraftCarriesAutoUpdateEnabled(t *testing.T) {
	draft := saveInputFromApplication(Application{Name: "web", AutoUpdateEnabled: false})
	if draft.AutoUpdateEnabled == nil || *draft.AutoUpdateEnabled {
		t.Fatalf("saveInputFromApplication must carry autoUpdateEnabled=false, got %#v", draft.AutoUpdateEnabled)
	}

	var legacyDraft SaveInput
	if err := json.Unmarshal([]byte(`{"name":"web","enabled":true,"specYaml":"name: web\n"}`), &legacyDraft); err != nil {
		t.Fatal(err)
	}
	if legacyDraft.AutoUpdateEnabled != nil {
		t.Fatalf("legacy draft without autoUpdateEnabled must stay nil, got %#v", legacyDraft.AutoUpdateEnabled)
	}
	normalized := normalizeEditDraft(legacyDraft)
	if normalized.AutoUpdateEnabled != nil {
		t.Fatalf("normalizeEditDraft must not fabricate autoUpdateEnabled, got %#v", normalized.AutoUpdateEnabled)
	}
}
