package facilityapps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"panel/internal/modules/applications"
	"panel/internal/modules/tasks"
	storage "panel/internal/platform/database"
)

// configRecordReconcileTrigger records every config-plane/proxy-plane reconcile
// trigger and can be made to fail on demand. FAC-RP-009/FAC-RP-010 require the
// trigger payload (affected gateways only) to be observable and apply failures to
// stay non-fatal for the already committed record.
type configRecordReconcileTrigger struct {
	mu       sync.Mutex
	triggers []tasks.PeriodicTrigger
	err      error
}

func (r *configRecordReconcileTrigger) TriggerApplicationReconcile(_ context.Context, trigger tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggers = append(r.triggers, trigger)
	if r.err != nil {
		return tasks.Task{}, false, r.err
	}
	return tasks.Task{ID: "config-record-operation"}, false, nil
}

func (r *configRecordReconcileTrigger) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

func (r *configRecordReconcileTrigger) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggers = nil
	r.err = nil
}

func (r *configRecordReconcileTrigger) snapshot() []tasks.PeriodicTrigger {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]tasks.PeriodicTrigger(nil), r.triggers...)
}

func configRecordTriggerPayload(t *testing.T, trigger tasks.PeriodicTrigger) map[string]any {
	t.Helper()
	payload, ok := trigger.Payload.(map[string]any)
	if !ok {
		t.Fatalf("trigger payload = %#v, want map[string]any", trigger.Payload)
	}
	return payload
}

func configRecordTriggerServerIDs(t *testing.T, trigger tasks.PeriodicTrigger) []string {
	t.Helper()
	payload := configRecordTriggerPayload(t, trigger)
	values, ok := payload["serverIds"].([]string)
	if !ok {
		t.Fatalf("trigger serverIds = %#v, want []string", payload["serverIds"])
	}
	return append([]string(nil), values...)
}

func newConfigRecordsTestService(t *testing.T) (*Service, *configRecordReconcileTrigger) {
	t.Helper()
	svc, _, closeStore := newFacilityEditTestService(t)
	t.Cleanup(closeStore)
	trigger := &configRecordReconcileTrigger{}
	svc.reconciler = trigger
	return svc, trigger
}

func saveConfigRecordGateways(t *testing.T, svc *Service, serverIDs ...string) {
	t.Helper()
	if _, err := svc.SaveProxyPlane(context.Background(), ProxyPlaneSaveInput{DeploymentServers: serverIDs}); err != nil {
		t.Fatalf("save proxy plane %v: %v", serverIDs, err)
	}
}

type configRecordAssetFixture struct {
	Name    string
	Content string
}

func configRecordDraftForTest(recordID string, origins []string, anyAccess applications.AnyAccessConfig, asset *configRecordAssetFixture) FacilityRouteDomain {
	path := FacilityRoutePath{Path: "/", RuleType: StaticRuleRedirect, RedirectURL: "https://" + recordID + "/", RedirectCode: 302}
	if asset != nil {
		path = FacilityRoutePath{Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceUploadedFile, AssetName: asset.Name}
	}
	return FacilityRouteDomain{
		Domain:          recordID,
		OriginServerIDs: append([]string(nil), origins...),
		AnyAccess:       anyAccess,
		Paths:           []FacilityRoutePath{path},
	}
}

func commitConfigRecordForTest(t *testing.T, svc *Service, key string, draft FacilityRouteDomain, assets []configRecordAssetFixture) FacilityEditCommitResult {
	t.Helper()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: draft.Domain, Draft: &draft})
	if err != nil {
		t.Fatalf("begin %s edit session: %v", draft.Domain, err)
	}
	for index, asset := range assets {
		operationID := key + "-put-" + strconv.Itoa(index)
		session, err = svc.PutFacilityEditAsset(ctx, session.ID, asset.Name, operationID, FacilityEditAssetInput{
			Revision:          session.Revision,
			ClientOperationID: operationID,
			Name:              asset.Name,
			Kind:              StaticSourceUploadedFile,
			ContentMode:       "text",
			FileName:          "index.html",
			Content:           []byte(asset.Content),
		})
		if err != nil {
			t.Fatalf("upload %s asset %q: %v", draft.Domain, asset.Name, err)
		}
	}
	preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatalf("preview %s edit session: %v", draft.Domain, err)
	}
	result, err := svc.CommitFacilityEditSession(ctx, session.ID, key, CommitFacilityEditSessionInput{
		Revision:            session.Revision,
		BaseResourceVersion: session.BaseResourceVersion.Value,
		PreviewToken:        preview.Token.Value,
	})
	if err != nil {
		t.Fatalf("commit %s edit session: %v", draft.Domain, err)
	}
	return result
}

func seedConfigRecordForTest(t *testing.T, svc *Service, recordID string, origins []string, anyAccess applications.AnyAccessConfig, asset *configRecordAssetFixture) ConfigRecord {
	t.Helper()
	draft := configRecordDraftForTest(recordID, origins, anyAccess, asset)
	var assets []configRecordAssetFixture
	if asset != nil {
		assets = []configRecordAssetFixture{*asset}
	}
	return commitConfigRecordForTest(t, svc, "seed-"+recordID, draft, assets).Record
}

func mustGetConfigRecord(t *testing.T, svc *Service, recordID string) ConfigRecord {
	t.Helper()
	record, err := svc.GetConfigRecord(context.Background(), recordID)
	if err != nil {
		t.Fatalf("get config record %s: %v", recordID, err)
	}
	return record
}

func configRecordAssetID(t *testing.T, record ConfigRecord, name string) string {
	t.Helper()
	for _, asset := range record.StaticAssets {
		if asset.Name == name {
			return asset.ID
		}
	}
	t.Fatalf("record %s has no static asset %q: %#v", record.RecordID, name, record.StaticAssets)
	return ""
}

func assertConfigRecordAssetContent(t *testing.T, svc *Service, record ConfigRecord, name, want string) string {
	t.Helper()
	assetID := configRecordAssetID(t, record, name)
	content, err := os.ReadFile(filepath.Join(svc.staticAssetContentDir(assetID), "index.html"))
	if err != nil {
		t.Fatalf("read record %s asset %q content: %v", record.RecordID, name, err)
	}
	if string(content) != want {
		t.Fatalf("record %s asset %q content = %q, want %q", record.RecordID, name, string(content), want)
	}
	return assetID
}

// TestFacilityConfigRecordVersionsAreIsolated covers FAC-RP-001/FAC-RP-004:
// every reverse_proxy_routes row is an independent configuration record with its
// own version/CAS. Committing one record must not bump another record, and an
// edit session is bound to exactly one recordId (base version included).
func TestFacilityConfigRecordVersionsAreIsolated(t *testing.T) {
	svc, _ := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a", "srv-b")

	recordA := seedConfigRecordForTest(t, svc, "a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	recordB := seedConfigRecordForTest(t, svc, "b.example.test", []string{"srv-b"}, applications.AnyAccessConfig{}, nil)
	if recordA.Version != 1 || recordB.Version != 1 {
		t.Fatalf("initial record versions = %d/%d, want 1/1", recordA.Version, recordB.Version)
	}

	// 提交 A 的第二版只推进 A 的 version。
	draft := configRecordDraftForTest("a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	draft.Paths[0].RedirectURL = "https://a.example.test/v2/"
	result := commitConfigRecordForTest(t, svc, "update-a", draft, nil)
	if result.Record.Version != 2 {
		t.Fatalf("committed record version = %d, want 2", result.Record.Version)
	}
	if got := mustGetConfigRecord(t, svc, "b.example.test").Version; got != 1 {
		t.Fatalf("unrelated record version = %d after committing a, want 1", got)
	}

	// 会话 base version / draft domain 必须绑定各自记录。
	sessionA, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: "a.example.test"})
	if err != nil {
		t.Fatalf("begin a edit session: %v", err)
	}
	if sessionA.RecordID != "a.example.test" || sessionA.Draft.Domain != "a.example.test" {
		t.Fatalf("session a record binding = %q/%q", sessionA.RecordID, sessionA.Draft.Domain)
	}
	if sessionA.BaseResourceVersion.Value != "2" {
		t.Fatalf("session a base version = %q, want 2", sessionA.BaseResourceVersion.Value)
	}
	sessionB, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: "b.example.test"})
	if err != nil {
		t.Fatalf("begin b edit session: %v", err)
	}
	if sessionB.BaseResourceVersion.Value != "1" {
		t.Fatalf("session b base version = %q, want 1", sessionB.BaseResourceVersion.Value)
	}

	// 用另一条记录的 version 作为 base 必须被记录级 CAS 拒绝。
	_, err = svc.PatchFacilityEditSession(ctx, sessionA.ID, PatchFacilityEditSessionInput{
		Revision:            sessionA.Revision,
		BaseResourceVersion: "1",
		Draft:               draft,
	})
	assertFacilityPanelError(t, err, "resource_version_conflict")

	// 使用本记录当前 base version 可以正常 rebase。
	rebased, err := svc.PatchFacilityEditSession(ctx, sessionA.ID, PatchFacilityEditSessionInput{
		Revision:            sessionA.Revision,
		BaseResourceVersion: "2",
		Draft:               draft,
	})
	if err != nil {
		t.Fatalf("rebase a edit session: %v", err)
	}
	if rebased.BaseResourceVersion.Value != "2" || rebased.Draft.Domain != "a.example.test" {
		t.Fatalf("rebased session = base %q domain %q", rebased.BaseResourceVersion.Value, rebased.Draft.Domain)
	}
}

// TestFacilityConfigRecordCommitCASOnlyBlocksThatRecord covers FAC-RP-004:
// two sessions based on the same record version race for the same record; only
// one wins, the loser sees resource_version_conflict, and unrelated records keep
// their versions untouched.
func TestFacilityConfigRecordCommitCASOnlyBlocksThatRecord(t *testing.T) {
	svc, _ := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a")
	seedConfigRecordForTest(t, svc, "cas.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	seedConfigRecordForTest(t, svc, "other.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	planeVersionBefore := mustLoadPlaneVersion(t, svc, ctx)

	draftOne := configRecordDraftForTest("cas.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	draftOne.Paths[0].RedirectURL = "https://one.example.test/"
	draftTwo := configRecordDraftForTest("cas.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	draftTwo.Paths[0].RedirectURL = "https://two.example.test/"

	type candidate struct {
		session FacilityEditSession
		preview FacilityEditPreviewResult
	}
	candidates := make([]candidate, 0, 2)
	for _, draft := range []FacilityRouteDomain{draftOne, draftTwo} {
		session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: "cas.example.test", Draft: &draft})
		if err != nil {
			t.Fatalf("begin cas edit session: %v", err)
		}
		preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
		if err != nil {
			t.Fatalf("preview cas edit session: %v", err)
		}
		candidates = append(candidates, candidate{session: session, preview: preview})
	}

	start := make(chan struct{})
	errs := make(chan error, len(candidates))
	var wg sync.WaitGroup
	for index, item := range candidates {
		wg.Add(1)
		go func(index int, item candidate) {
			defer wg.Done()
			<-start
			_, commitErr := svc.CommitFacilityEditSession(ctx, item.session.ID, "cas-commit-"+strconv.Itoa(index), CommitFacilityEditSessionInput{
				Revision:            item.session.Revision,
				BaseResourceVersion: item.session.BaseResourceVersion.Value,
				PreviewToken:        item.preview.Token.Value,
			})
			errs <- commitErr
		}(index, item)
	}
	close(start)
	wg.Wait()
	close(errs)

	successes, conflicts := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case facilityPanelErrorCode(err) == "resource_version_conflict":
			conflicts++
		default:
			t.Fatalf("unexpected commit error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent record commits: successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
	if got := mustGetConfigRecord(t, svc, "cas.example.test").Version; got != 2 {
		t.Fatalf("raced record version = %d, want 2", got)
	}
	if got := mustGetConfigRecord(t, svc, "other.example.test").Version; got != 1 {
		t.Fatalf("unrelated record version = %d after race, want 1", got)
	}
	if got := mustLoadPlaneVersion(t, svc, ctx); got != planeVersionBefore {
		t.Fatalf("proxy plane version = %d after record commit, want %d", got, planeVersionBefore)
	}
}

func mustLoadPlaneVersion(t *testing.T, svc *Service, ctx context.Context) int {
	t.Helper()
	cfg, err := svc.loadConfig(ctx)
	if err != nil {
		t.Fatalf("load proxy plane config: %v", err)
	}
	return cfg.Version
}

// TestFacilityConfigRecordPrivateAssetsAreIsolated covers FAC-RP-003:
// assets are private to one record, the same asset name in two records must not
// overwrite or resolve to each other, committed downloads must be scoped by
// recordId, and a record cannot reference/delete another record's asset.
func TestFacilityConfigRecordPrivateAssetsAreIsolated(t *testing.T) {
	svc, _ := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a")

	seedConfigRecordForTest(t, svc, "a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, &configRecordAssetFixture{Name: "site", Content: "site-a"})
	seedConfigRecordForTest(t, svc, "b.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, &configRecordAssetFixture{Name: "site", Content: "site-b"})
	// B 增加一个仅属于自己的私有资产。
	draftB := configRecordDraftForTest("b.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, &configRecordAssetFixture{Name: "site"})
	commitConfigRecordForTest(t, svc, "add-only-b", draftB, []configRecordAssetFixture{{Name: "only-b", Content: "b-only"}})

	recordA := mustGetConfigRecord(t, svc, "a.example.test")
	recordB := mustGetConfigRecord(t, svc, "b.example.test")
	if recordA.Version != 1 || recordB.Version != 2 {
		t.Fatalf("record versions = %d/%d, want 1/2", recordA.Version, recordB.Version)
	}
	assetIDForA := assertConfigRecordAssetContent(t, svc, recordA, "site", "site-a")
	assetIDForB := assertConfigRecordAssetContent(t, svc, recordB, "site", "site-b")
	if assetIDForA == assetIDForB {
		t.Fatalf("same asset name in two records resolved to one physical asset %q", assetIDForA)
	}

	// 记录作用域下载：同名资产各自解析到本记录私有内容。
	downloadA, err := svc.GetStaticAssetDownloadForRecord(ctx, "a.example.test", "site")
	if err != nil {
		t.Fatalf("scoped download a/site: %v", err)
	}
	assertFacilityDownloadFile(t, downloadA, "site-a")
	downloadB, err := svc.GetStaticAssetDownloadForRecord(ctx, "b.example.test", "site")
	if err != nil {
		t.Fatalf("scoped download b/site: %v", err)
	}
	assertFacilityDownloadFile(t, downloadB, "site-b")

	// 不带 recordId 的已归属资产下载必须拒绝，避免跨记录歧义。
	_, err = svc.GetStaticAssetDownload(ctx, "site")
	assertFacilityPanelError(t, err, "facility_record_id_required")

	// 跨记录资产名不得解析：A 记录看不到 B 的私有资产。
	if _, err := svc.GetStaticAssetDownloadForRecord(ctx, "a.example.test", "only-b"); !isPanelNotFound(err) {
		t.Fatalf("cross-record asset lookup error = %v, want not found", err)
	}

	// HTTP handler 同样必须按 recordId 作用域下载。
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	getAsset := func(assetName, recordID string) *httptest.ResponseRecorder {
		t.Helper()
		target := "/api/v1/facility-apps/reverse-proxy/static-assets/" + url.PathEscape(assetName) + "/content"
		if recordID != "" {
			target += "?recordId=" + url.QueryEscape(recordID)
		}
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		return recorder
	}
	if recorder := getAsset("site", "a.example.test"); recorder.Code != http.StatusOK || recorder.Body.String() != "site-a" {
		t.Fatalf("scoped handler a/site status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder := getAsset("site", "b.example.test"); recorder.Code != http.StatusOK || recorder.Body.String() != "site-b" {
		t.Fatalf("scoped handler b/site status=%d body=%q", recorder.Code, recorder.Body.String())
	}
	if recorder := getAsset("site", ""); recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unscoped committed asset download status=%d, want 422", recorder.Code)
	}
	if recorder := getAsset("only-b", "a.example.test"); recorder.Code != http.StatusNotFound {
		t.Fatalf("cross-record handler lookup status=%d, want 404", recorder.Code)
	}

	// 单人记录编辑会话不能引用另一记录的同名私有资产。
	draftA := FacilityRouteDomain{
		Domain:          "a.example.test",
		OriginServerIDs: []string{"srv-a"},
		Paths: []FacilityRoutePath{
			{Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceUploadedFile, AssetName: "site"},
			{Path: "/only", RuleType: StaticRuleStatic, SourceType: StaticSourceUploadedFile, AssetName: "only-b"},
		},
	}
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: "a.example.test", Draft: &draftA})
	if err != nil {
		t.Fatalf("begin a edit session for cross-record draft: %v", err)
	}
	validation, err := svc.ValidateFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatalf("validate cross-record draft: %v", err)
	}
	if len(validation.Diagnostics) != 1 ||
		validation.Diagnostics[0].Code != "facility_static_asset_referenced_after_delete" ||
		validation.Diagnostics[0].Details["assetName"] != "only-b" {
		t.Fatalf("cross-record reference diagnostics = %#v, want one referenced_after_delete for only-b", validation.Diagnostics)
	}

	// 上传本记录自己的同名资产后引用成立。
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "only-b", "a-put-only-b", FacilityEditAssetInput{
		Revision:          session.Revision,
		ClientOperationID: "a-put-only-b",
		Name:              "only-b",
		Kind:              StaticSourceUploadedFile,
		ContentMode:       "text",
		FileName:          "index.html",
		Content:           []byte("a-only-b"),
	})
	if err != nil {
		t.Fatalf("upload a/only-b: %v", err)
	}
	validation, err = svc.ValidateFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatalf("validate after uploading private asset: %v", err)
	}
	if len(validation.Diagnostics) != 0 {
		t.Fatalf("diagnostics after uploading private asset = %#v, want none", validation.Diagnostics)
	}

	// 仍被本记录草稿引用时删除必须被拒绝。
	_, err = svc.DeleteFacilityEditAsset(ctx, session.ID, "only-b", "a-delete-only-b", FacilityEditMutationInput{Revision: session.Revision, ClientOperationID: "a-delete-only-b"})
	assertFacilityPanelError(t, err, "facility_static_asset_in_use")

	// B 的私有同内容资产不受 A 的编辑会话影响。
	downloadBOnly, err := svc.GetStaticAssetDownloadForRecord(ctx, "b.example.test", "only-b")
	if err != nil {
		t.Fatalf("download b/only-b after a edit: %v", err)
	}
	assertFacilityDownloadFile(t, downloadBOnly, "b-only")
}

// TestFacilityProxyPlaneAndRecordWritesKeepVersionsIsolated covers FAC-RP-001:
// saving the proxy plane only advances facility_app_configs.version, never any
// configuration record version; committing/deleting one record never advances
// another record (or the proxy plane).
func TestFacilityProxyPlaneAndRecordWritesKeepVersionsIsolated(t *testing.T) {
	svc, _ := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a", "srv-b")
	seedConfigRecordForTest(t, svc, "plane-a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	seedConfigRecordForTest(t, svc, "plane-b.example.test", []string{"srv-b"}, applications.AnyAccessConfig{}, nil)
	if got := mustLoadPlaneVersion(t, svc, ctx); got != 1 {
		t.Fatalf("initial proxy plane version = %d, want 1", got)
	}

	// 代理平面保存：只推进 facility_app_configs.version，记录 version 不变。
	if _, err := svc.SaveProxyPlane(ctx, ProxyPlaneSaveInput{DeploymentServers: []string{"srv-a", "srv-b", "srv-c"}}); err != nil {
		t.Fatalf("second proxy plane save: %v", err)
	}
	if got := mustLoadPlaneVersion(t, svc, ctx); got != 2 {
		t.Fatalf("proxy plane version = %d after save, want 2", got)
	}
	if got := mustGetConfigRecord(t, svc, "plane-a.example.test").Version; got != 1 {
		t.Fatalf("record a version = %d after proxy plane save, want 1", got)
	}
	if got := mustGetConfigRecord(t, svc, "plane-b.example.test").Version; got != 1 {
		t.Fatalf("record b version = %d after proxy plane save, want 1", got)
	}

	// 配置记录 commit：只推进本记录 version。
	draftA := configRecordDraftForTest("plane-a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	draftA.Paths[0].RedirectURL = "https://plane-a.example.test/v2/"
	commitConfigRecordForTest(t, svc, "plane-update-a", draftA, nil)
	if got := mustGetConfigRecord(t, svc, "plane-a.example.test").Version; got != 2 {
		t.Fatalf("record a version = %d after commit, want 2", got)
	}
	if got := mustGetConfigRecord(t, svc, "plane-b.example.test").Version; got != 1 {
		t.Fatalf("record b version = %d after committing a, want 1", got)
	}
	if got := mustLoadPlaneVersion(t, svc, ctx); got != 2 {
		t.Fatalf("proxy plane version = %d after record commit, want 2", got)
	}

	// 配置记录 delete：只移除本记录，不触碰其它记录或代理平面 version。
	if err := svc.DeleteConfigRecord(ctx, "plane-a.example.test", 2); err != nil {
		t.Fatalf("delete config record a: %v", err)
	}
	if _, err := svc.GetConfigRecord(ctx, "plane-a.example.test"); !isPanelNotFound(err) {
		t.Fatalf("deleted record lookup error = %v, want not found", err)
	}
	if got := mustGetConfigRecord(t, svc, "plane-b.example.test").Version; got != 1 {
		t.Fatalf("record b version = %d after deleting a, want 1", got)
	}
	if got := mustLoadPlaneVersion(t, svc, ctx); got != 2 {
		t.Fatalf("proxy plane version = %d after record delete, want 2", got)
	}
}

// TestApplyConfigRecordReloadsOnlyAffectedGateways covers FAC-RP-010:
// a single-record apply only coordinates the gateways covered by that record's
// origins ∪ relays; AnyAccess with an empty relay list falls back to every
// non-origin gateway.
func TestApplyConfigRecordReloadsOnlyAffectedGateways(t *testing.T) {
	svc, trigger := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a", "srv-b", "srv-c", "srv-d")
	seedConfigRecordForTest(t, svc, "origin-a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	seedConfigRecordForTest(t, svc, "relay-b.example.test", []string{"srv-b"}, applications.AnyAccessConfig{
		Enabled:        true,
		Strategy:       applications.AnyAccessStrategyRoundRobin,
		RelayServerIDs: []string{"srv-d"},
	}, nil)
	seedConfigRecordForTest(t, svc, "any-c.example.test", []string{"srv-c"}, applications.AnyAccessConfig{
		Enabled:  true,
		Strategy: applications.AnyAccessStrategyRoundRobin,
	}, nil)
	trigger.reset()

	for _, tc := range []struct {
		recordID string
		want     []string
	}{
		{recordID: "origin-a.example.test", want: []string{"srv-a"}},
		{recordID: "relay-b.example.test", want: []string{"srv-b", "srv-d"}},
		{recordID: "any-c.example.test", want: []string{"srv-a", "srv-b", "srv-c", "srv-d"}},
	} {
		before := len(trigger.snapshot())
		if err := svc.ApplyConfigRecord(ctx, tc.recordID); err != nil {
			t.Fatalf("apply config record %s: %v", tc.recordID, err)
		}
		triggers := trigger.snapshot()
		if len(triggers) != before+1 {
			t.Fatalf("apply %s triggered %d reconciles, want 1", tc.recordID, len(triggers)-before)
		}
		last := triggers[len(triggers)-1]
		if last.Type != "facility_app" || last.Manual || last.TriggerResourceID != proxyApplicationID {
			t.Fatalf("apply %s trigger = %#v, want facility_app config-plane trigger", tc.recordID, last)
		}
		payload := configRecordTriggerPayload(t, last)
		if force, _ := payload["force"].(bool); force {
			t.Fatalf("apply %s must not force a full re-render: payload=%#v", tc.recordID, payload)
		}
		if reason, _ := payload["reason"].(string); reason != "reverse_proxy_changed" {
			t.Fatalf("apply %s reason = %q, want reverse_proxy_changed", tc.recordID, reason)
		}
		if stopServers, _ := payload["stopServers"].([]string); len(stopServers) != 0 {
			t.Fatalf("apply %s stopServers = %#v, want none", tc.recordID, stopServers)
		}
		if got := configRecordTriggerServerIDs(t, last); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("apply %s affected gateways = %#v, want %#v", tc.recordID, got, tc.want)
		}
	}
}

// TestApplyConfigRecordFailureDoesNotNegateCommittedRecord covers FAC-RP-005/
// FAC-RP-010 failure semantics: committing the record persists it first; a
// failing config-plane apply afterwards must not roll the committed record back
// (stable error only, never a bare 500 that negates the commit).
func TestApplyConfigRecordFailureDoesNotNegateCommittedRecord(t *testing.T) {
	svc, trigger := newConfigRecordsTestService(t)
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a")
	trigger.reset()
	trigger.fail(errors.New("config-plane reconcile backend unavailable"))

	draft := configRecordDraftForTest("apply-failure.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, nil)
	result := commitConfigRecordForTest(t, svc, "commit-apply-failure", draft, nil)
	if result.Record.Version != 1 {
		t.Fatalf("committed record version = %d, want 1", result.Record.Version)
	}
	if result.ApplyRequested || !result.ApplyNeeded {
		t.Fatalf("commit result apply flags = requested:%v needed:%v, want false/true", result.ApplyRequested, result.ApplyNeeded)
	}
	if !reflect.DeepEqual(result.ApplyServerIDs, []string{"srv-a"}) {
		t.Fatalf("commit apply server ids = %#v, want [srv-a]", result.ApplyServerIDs)
	}
	if triggers := trigger.snapshot(); len(triggers) != 0 {
		t.Fatalf("commit must only persist and return applyNeeded, triggered %d reconciles", len(triggers))
	}

	err := svc.ApplyConfigRecord(ctx, "apply-failure.example.test")
	assertFacilityPanelError(t, err, "facility_config_apply_failed")

	// apply 失败后记录仍持久化且内容未回滚。
	stored := mustGetConfigRecord(t, svc, "apply-failure.example.test")
	if stored.Version != 1 {
		t.Fatalf("stored record version = %d after failed apply, want 1", stored.Version)
	}
	if stored.Domain.Paths[0].RedirectURL != draft.Paths[0].RedirectURL {
		t.Fatalf("stored redirect = %q after failed apply, want %q", stored.Domain.Paths[0].RedirectURL, draft.Paths[0].RedirectURL)
	}
	records, err := svc.ListConfigRecords(ctx)
	if err != nil {
		t.Fatalf("list config records: %v", err)
	}
	found := false
	for _, record := range records {
		if record.RecordID == "apply-failure.example.test" && record.Version == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("committed record missing from config record list after failed apply: %#v", records)
	}
}

// prepareCrashedConfigRecordCommit simulates a crash after the record's staged
// files were moved into place but before the DB commit was confirmed: it returns
// the session id, the staged blob dir that must come back on rollback, and the
// manifest that a recovery pass would read.
func prepareCrashedConfigRecordCommit(t *testing.T, svc *Service, store *storage.Store, recordID string) (string, string, facilityCommitManifest) {
	t.Helper()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: recordID})
	if err != nil {
		t.Fatalf("begin crashed edit session %s: %v", recordID, err)
	}
	operationID := "crash-" + recordID
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "added", operationID, FacilityEditAssetInput{
		Revision:          session.Revision,
		ClientOperationID: operationID,
		Name:              "added",
		Kind:              StaticSourceUploadedFile,
		ContentMode:       "text",
		FileName:          "added.html",
		Content:           []byte("added-" + recordID),
	})
	if err != nil {
		t.Fatalf("stage crashed edit asset %s: %v", recordID, err)
	}
	var blobDir string
	if err := store.AppDB().QueryRow(`SELECT blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key='added'`, session.ID).Scan(&blobDir); err != nil {
		t.Fatalf("read staged blob dir %s: %v", recordID, err)
	}
	record, err := svc.loadFacilityEditSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("reload crashed edit session %s: %v", recordID, err)
	}
	manifest, err := svc.prepareFacilityCommitManifest(record)
	if err != nil {
		t.Fatalf("prepare crash manifest %s: %v", recordID, err)
	}
	manifestPath := filepath.Join(svc.facilityEditPath(session.ID), "commit-manifest.json")
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		t.Fatalf("write crash manifest %s: %v", recordID, err)
	}
	past := formatTime(time.Now().UTC().Add(-time.Minute))
	if _, err := store.AppDB().Exec(
		`UPDATE facility_edit_sessions SET state=?,commit_lease_owner='crashed',commit_lease_expires_at=?,manifest_path=? WHERE id=?`,
		FacilityEditSessionCommitting, past, manifestPath, session.ID,
	); err != nil {
		t.Fatalf("mark crashed session %s: %v", recordID, err)
	}
	if err := svc.moveFacilityManifestFiles(&manifest); err != nil {
		t.Fatalf("move crash manifest files %s: %v", recordID, err)
	}
	manifest.FilesMoved = true
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		t.Fatalf("rewrite crash manifest %s: %v", recordID, err)
	}
	return session.ID, blobDir, manifest
}

// TestFacilityConfigRecordManifestRecoveryIsScoped covers FAC-RP-005: crash
// recovery reads the manifest of exactly one record, only rewrites/rolls back
// that record's files and version, and never guesses or touches other records'
// sessions/assets.
func TestFacilityConfigRecordManifestRecoveryIsScoped(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	trigger := &configRecordReconcileTrigger{}
	svc.reconciler = trigger
	ctx := context.Background()
	saveConfigRecordGateways(t, svc, "srv-a")
	seedConfigRecordForTest(t, svc, "recover-a.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, &configRecordAssetFixture{Name: "site", Content: "site-a"})
	seedConfigRecordForTest(t, svc, "recover-b.example.test", []string{"srv-a"}, applications.AnyAccessConfig{}, &configRecordAssetFixture{Name: "site", Content: "site-b"})

	t.Run("rollback restores only the crashed record", func(t *testing.T) {
		trigger.reset()
		bSession, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: "recover-b.example.test"})
		if err != nil {
			t.Fatalf("begin unrelated b session: %v", err)
		}
		sessionID, blobDir, _ := prepareCrashedConfigRecordCommit(t, svc, store, "recover-a.example.test")

		recovered, err := svc.GetFacilityEditSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("recover crashed session: %v", err)
		}
		if recovered.State != FacilityEditSessionActive {
			t.Fatalf("recovered state = %q, want active after rollback", recovered.State)
		}
		if got := mustGetConfigRecord(t, svc, "recover-a.example.test").Version; got != 1 {
			t.Fatalf("crashed record version = %d after rollback, want 1", got)
		}
		if got := mustGetConfigRecord(t, svc, "recover-b.example.test").Version; got != 1 {
			t.Fatalf("unrelated record version = %d after rollback, want 1", got)
		}
		if _, err := os.Stat(blobDir); err != nil {
			t.Fatalf("staged blob was not restored by rollback: %v", err)
		}
		assertConfigRecordAssetContent(t, svc, mustGetConfigRecord(t, svc, "recover-b.example.test"), "site", "site-b")
		bAfter, err := svc.GetFacilityEditSession(ctx, bSession.ID)
		if err != nil {
			t.Fatalf("reload unrelated b session: %v", err)
		}
		if bAfter.State != FacilityEditSessionActive || bAfter.Revision != bSession.Revision {
			t.Fatalf("unrelated b session changed: state=%q revision=%d (want active/%d)", bAfter.State, bAfter.Revision, bSession.Revision)
		}
	})

	t.Run("db committed recovery finishes only the crashed record", func(t *testing.T) {
		trigger.reset()
		bSession, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{RecordID: "recover-b.example.test"})
		if err != nil {
			t.Fatalf("begin unrelated b session: %v", err)
		}
		sessionID, _, manifest := prepareCrashedConfigRecordCommit(t, svc, store, "recover-a.example.test")
		// 模拟文件与 DB 都已落库、但进程在标记 session committed 前崩溃。
		if err := svc.commitFacilityManifestDB(ctx, manifest); err != nil {
			t.Fatalf("commit crashed manifest db: %v", err)
		}

		recovered, err := svc.GetFacilityEditSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("recover committed session: %v", err)
		}
		if recovered.State != FacilityEditSessionCommitted || recovered.CommitResult == nil {
			t.Fatalf("recovered session = %#v, want committed with commit result", recovered)
		}
		if !recovered.CommitResult.ApplyRequested {
			t.Fatalf("recovered commit result = %#v, want config-plane apply requested", recovered.CommitResult)
		}
		if got := mustGetConfigRecord(t, svc, "recover-a.example.test").Version; got != 2 {
			t.Fatalf("recovered record version = %d, want 2", got)
		}
		if got := mustGetConfigRecord(t, svc, "recover-b.example.test").Version; got != 1 {
			t.Fatalf("unrelated record version = %d after committed recovery, want 1", got)
		}
		triggers := trigger.snapshot()
		if len(triggers) != 1 {
			t.Fatalf("recovery triggered %d config-plane reconciles, want 1", len(triggers))
		}
		if got := configRecordTriggerServerIDs(t, triggers[0]); !reflect.DeepEqual(got, []string{"srv-a"}) {
			t.Fatalf("recovery affected gateways = %#v, want [srv-a]", got)
		}
		assertConfigRecordAssetContent(t, svc, mustGetConfigRecord(t, svc, "recover-b.example.test"), "site", "site-b")
		bAfter, err := svc.GetFacilityEditSession(ctx, bSession.ID)
		if err != nil {
			t.Fatalf("reload unrelated b session: %v", err)
		}
		if bAfter.State != FacilityEditSessionActive || bAfter.Revision != bSession.Revision {
			t.Fatalf("unrelated b session changed: state=%q revision=%d (want active/%d)", bAfter.State, bAfter.Revision, bSession.Revision)
		}
	})
}
