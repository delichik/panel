package facilityapps

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"panel/internal/modules/servers"
	"panel/internal/modules/tasks"
	"panel/internal/platform/config"
	storage "panel/internal/platform/database"
	panelerr "panel/internal/platform/errors"
)

func TestFacilityEditSessionConcurrentCommitUsesConfigVersionCAS(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	drafts := []ReverseProxySaveInput{{}, {DeploymentServers: []string{"srv-a"}}}
	type candidateSpec struct {
		session FacilityEditSession
		preview FacilityEditPreviewResult
	}
	candidates := make([]candidateSpec, 2)
	for index := range drafts {
		session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{Draft: &drafts[index]})
		if err != nil {
			t.Fatal(err)
		}
		preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
		if err != nil {
			t.Fatal(err)
		}
		candidates[index] = candidateSpec{session: session, preview: preview}
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for index, item := range candidates {
		wg.Add(1)
		go func(index int, candidate candidateSpec) {
			defer wg.Done()
			<-start
			_, err := svc.CommitFacilityEditSession(ctx, candidate.session.ID, "commit-"+string(rune('a'+index)), CommitFacilityEditSessionInput{Revision: candidate.session.Revision, BaseResourceVersion: candidate.session.BaseResourceVersion.Value, PreviewToken: candidate.preview.Token.Value})
			errs <- err
		}(index, item)
	}
	close(start)
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if facilityPanelErrorCode(err) == "resource_version_conflict" {
			conflicts++
		} else {
			t.Fatalf("unexpected commit error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestFacilityStaticAssetNamesAreUniqueWithinFacility(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	input := StaticAssetUploadInput{Name: "site", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("hello")}
	if _, err := svc.UploadStaticAsset(ctx, input); err != nil {
		t.Fatal(err)
	}
	_, err := svc.UploadStaticAsset(ctx, input)
	assertFacilityPanelError(t, err, "facility_static_asset_name_duplicate")
}

func TestFacilityEditAssetNamesAreUniqueWithinSession(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.PutFacilityEditAsset(ctx, session.ID, "asset-a", "put-a", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "put-a", Name: "site", Kind: StaticSourceUploadedFile, ContentMode: "text", FileName: "index.html", Content: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-b", "put-b", FacilityEditAssetInput{
		Revision: first.Revision, ClientOperationID: "put-b", Name: "site", Kind: StaticSourceUploadedFile, ContentMode: "text", FileName: "other.html", Content: []byte("world"),
	})
	assertFacilityPanelError(t, err, "facility_static_asset_name_duplicate")
}

func TestFacilityEditSessionApplyFailureRemainsCommitted(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CommitFacilityEditSession(ctx, session.ID, "commit", CommitFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value})
	if err != nil {
		t.Fatal(err)
	}
	if result.ApplyRequested || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "facility_apply_request_failed" {
		t.Fatalf("result = %#v", result)
	}
	stored, err := svc.GetFacilityEditSession(ctx, session.ID)
	if err != nil || stored.State != FacilityEditSessionCommitted {
		t.Fatalf("session=%#v err=%v", stored, err)
	}
}

// TestFacilityEditSessionReuploadedAssetResolvesReferences 复现用户流程：// 会话内删除静态资产 → 草稿重新引用该资产名 → 重新上传同名资产后校验必须通过，
// 且删除后未恢复时的诊断必须携带 domain/path/assetName 细节。
func TestFacilityEditSessionReuploadedAssetResolvesReferences(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	uploadAsset := func(sessionID, clientOperationID string, revision int) FacilityEditSession {
		t.Helper()
		session, err := svc.PutFacilityEditAsset(ctx, sessionID, "main", clientOperationID, FacilityEditAssetInput{
			Revision: revision, ClientOperationID: clientOperationID, Name: "main", Kind: StaticSourceUploadedFile,
			ContentMode: "text", FileName: "index.html", Content: []byte("hello"),
		})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	patch := func(session FacilityEditSession, domains []FacilityRouteDomain) FacilityEditSession {
		t.Helper()
		next, err := svc.PatchFacilityEditSession(ctx, session.ID, PatchFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, Draft: ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}, Domains: domains}})
		if err != nil {
			t.Fatal(err)
		}
		return next
	}

	// 1. 提交带静态路由的设施配置。
	if _, err := svc.UploadStaticAsset(ctx, StaticAssetUploadInput{Name: "main", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{
		DeploymentServers: []string{"srv-a"},
		Domains:           []FacilityRouteDomain{{Domain: "static.example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceUploadedFile, AssetName: "main"}}}},
	}); err != nil {
		t.Fatal(err)
	}
	// 2. 打开编辑会话（前端会带上当前配置作为草稿）。
	cfg, err := svc.GetReverseProxy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	draft := ReverseProxySaveInput{DeploymentServers: cfg.DeploymentServers, PanelEntry: cfg.PanelEntry, Domains: cfg.Domains}
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	// 3. 先从草稿移除引用该资产的路由，再删除资产。
	emptyDomains := []FacilityRouteDomain{{Domain: "static.example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{}}}
	session = patch(session, emptyDomains)
	session, err = svc.DeleteFacilityEditAsset(ctx, session.ID, "main", "asset-delete", FacilityEditMutationInput{Revision: session.Revision, ClientOperationID: "asset-delete"})
	if err != nil {
		t.Fatal(err)
	}
	// 4. 重新添加引用已删除资产的路由：校验必须失败且带细节；同一域名下的
	// redirect / proxy_pass 路由即使 sourceType 被前端默认值带成 uploaded_file，
	// 也绝不能触发资产引用诊断。
	session = patch(session, []FacilityRouteDomain{{
		Domain: "static.example.test", OriginServerIDs: []string{"srv-a"},
		Paths: []FacilityRoutePath{
			{Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceUploadedFile, AssetName: "main"},
			{Path: "/go", RuleType: StaticRuleRedirect, SourceType: StaticSourceUploadedFile, RedirectURL: "https://target.example.test", RedirectCode: 302},
			{Path: "/api", RuleType: StaticRuleProxyPass, SourceType: StaticSourceUploadedFile, ProxyURL: "http://127.0.0.1:8080", ProxySourceMode: ProxySourcePreserve},
		},
	}})
	validation, err := svc.ValidateFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(validation.Diagnostics) != 1 || validation.Diagnostics[0].Code != "facility_static_asset_referenced_after_delete" {
		t.Fatalf("diagnostics = %#v, want only facility_static_asset_referenced_after_delete for the static route", validation.Diagnostics)
	}
	detail := validation.Diagnostics[0].Details
	if detail["domain"] != "static.example.test" || detail["path"] != "/" || detail["assetName"] != "main" {
		t.Fatalf("diagnostic details = %#v, want domain/path/assetName", detail)
	}
	// 5. 重新上传同名资产后，同一草稿必须通过校验。
	session = uploadAsset(session.ID, "asset-put", session.Revision)
	validation, err = svc.ValidateFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(validation.Diagnostics) != 0 {
		t.Fatalf("re-uploaded asset must resolve route references, diagnostics = %#v", validation.Diagnostics)
	}
}

func TestFacilityEditSessionDeletedReferencedAssetIsBlocking(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	draft := ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}, Domains: []FacilityRouteDomain{{Domain: "example.test", OriginServerIDs: []string{"srv-a"}, Paths: []FacilityRoutePath{{Path: "/", RuleType: StaticRuleStatic, SourceType: StaticSourceUploadedFile, AssetID: "asset-main"}}}}}
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{Draft: &draft})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-main", "asset-put", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "asset-put", Name: "main", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision := session.Revision
	var beforeDir string
	if err := svc.db.QueryRowContext(ctx, `SELECT blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key=?`, session.ID, "asset-main").Scan(&beforeDir); err != nil {
		t.Fatal(err)
	}
	_, err = svc.DeleteFacilityEditAsset(ctx, session.ID, "asset-main", "asset-delete", FacilityEditMutationInput{Revision: session.Revision, ClientOperationID: "asset-delete"})
	assertFacilityPanelError(t, err, "facility_static_asset_in_use")
	after, getErr := svc.GetFacilityEditSession(ctx, session.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if after.Revision != beforeRevision || len(after.Assets) != 1 {
		t.Fatalf("delete changed session: %#v", after)
	}
	if _, statErr := os.Stat(beforeDir); statErr != nil {
		t.Fatalf("asset blob changed: %v", statErr)
	}
}

func TestFacilityEditTextAssetModeValidationAndStableIdentity(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-text", "empty-text", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "empty-text", Name: "config", Kind: StaticSourceUploadedFile,
		ContentMode: "text", FileName: "config.txt", Content: []byte{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(session.Assets) != 1 || session.Assets[0].AssetKey != "asset-text" || session.Assets[0].ContentMode != "text" {
		t.Fatalf("text asset not preserved: %#v", session.Assets)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-text", "max-text", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "max-text", Name: "config", Kind: StaticSourceUploadedFile,
		ContentMode: "text", FileName: "config.txt", Content: bytes.Repeat([]byte("a"), 1<<20),
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision := session.Revision
	_, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-text", "large-text", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "large-text", Name: "config", Kind: StaticSourceUploadedFile,
		ContentMode: "text", FileName: "config.txt", Content: bytes.Repeat([]byte("a"), (1<<20)+1),
	})
	assertFacilityPanelError(t, err, "facility_edit_text_invalid")
	after, err := svc.GetFacilityEditSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != beforeRevision || after.Assets[0].Size != 1<<20 {
		t.Fatalf("invalid text changed session: %#v", after)
	}
	_, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-text", "bad-utf8", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "bad-utf8", Name: "config", Kind: StaticSourceUploadedFile,
		ContentMode: "text", FileName: "config.txt", Content: []byte{0xff},
	})
	assertFacilityPanelError(t, err, "facility_edit_text_invalid")
	_, err = svc.PutFacilityEditAsset(ctx, session.ID, "bundle", "bad-bundle", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "bad-bundle", Name: "bundle", Kind: StaticSourceUploadedBundle,
		ContentMode: "text", FileName: "bundle.zip", Content: []byte("text"),
	})
	assertFacilityPanelError(t, err, "facility_edit_asset_mode_invalid")
}

func TestFacilityEditAssetDefaultsToBinaryAndRejectsModeChange(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset", "binary-default", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "binary-default", Name: "asset", Kind: StaticSourceUploadedFile,
		FileName: "asset.bin", Content: []byte("binary"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Assets[0].ContentMode != "binary" {
		t.Fatalf("mode=%q", session.Assets[0].ContentMode)
	}
	beforeRevision := session.Revision
	beforeSHA := session.Assets[0].SHA256
	_, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset", "switch-mode", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "switch-mode", Name: "asset", Kind: StaticSourceUploadedFile,
		ContentMode: "text", FileName: "asset.txt", Content: []byte("text"),
	})
	assertFacilityPanelError(t, err, "facility_edit_asset_mode_immutable")
	after, err := svc.GetFacilityEditSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != beforeRevision || after.Assets[0].SHA256 != beforeSHA || after.Assets[0].ContentMode != "binary" {
		t.Fatalf("mode switch changed asset: %#v", after)
	}
}

func TestFacilityEditAssetRejectsKindChangeWithoutMutation(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset", "seed", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "seed", Name: "asset", Kind: StaticSourceUploadedFile, ContentMode: "binary", FileName: "asset.bin", Content: []byte("original")})
	if err != nil {
		t.Fatal(err)
	}
	var beforeKind, beforeMode, beforeHash, beforeBlob string
	var beforeSize int64
	if err := svc.db.QueryRowContext(ctx, `SELECT kind,content_mode,sha256,size,blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key='asset'`, session.ID).Scan(&beforeKind, &beforeMode, &beforeHash, &beforeSize, &beforeBlob); err != nil {
		t.Fatal(err)
	}
	beforeContent, err := os.ReadFile(filepath.Join(beforeBlob, "content", "asset.bin"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset", "switch-kind", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "switch-kind", Name: "asset", Kind: StaticSourceUploadedBundle, ContentMode: "binary", FileName: "asset.zip", Content: []byte("not-an-archive")})
	assertFacilityPanelError(t, err, "facility_edit_asset_kind_immutable")
	after, err := svc.GetFacilityEditSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != session.Revision {
		t.Fatalf("revision changed: %d -> %d", session.Revision, after.Revision)
	}
	var afterKind, afterMode, afterHash, afterBlob string
	var afterSize int64
	if err := svc.db.QueryRowContext(ctx, `SELECT kind,content_mode,sha256,size,blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key='asset'`, session.ID).Scan(&afterKind, &afterMode, &afterHash, &afterSize, &afterBlob); err != nil {
		t.Fatal(err)
	}
	afterContent, err := os.ReadFile(filepath.Join(afterBlob, "content", "asset.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if beforeKind != afterKind || beforeMode != afterMode || beforeHash != afterHash || beforeSize != afterSize || beforeBlob != afterBlob || !bytes.Equal(beforeContent, afterContent) {
		t.Fatalf("kind switch mutated asset: before=%q/%q/%q/%d/%q after=%q/%q/%q/%d/%q", beforeKind, beforeMode, beforeHash, beforeSize, beforeBlob, afterKind, afterMode, afterHash, afterSize, afterBlob)
	}
}

func TestFacilityEditTextAssetCommitAndReopenPreservesMode(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-text", "put-text", FacilityEditAssetInput{
		Revision: session.Revision, ClientOperationID: "put-text", Name: "config", Kind: StaticSourceUploadedFile,
		ContentMode: "text", FileName: "config.txt", Content: []byte("hello"),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CommitFacilityEditSession(ctx, session.ID, "commit-text", CommitFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value}); err != nil {
		t.Fatal(err)
	}
	reopened, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Assets) != 1 || reopened.Assets[0].AssetKey == "" || reopened.Assets[0].ContentMode != "text" {
		t.Fatalf("reopened text asset not preserved: %#v", reopened.Assets)
	}
}

func TestFacilityAssetDownloadsResolveCommittedSourceAndReplacementBlob(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	asset, err := svc.UploadStaticAsset(ctx, StaticAssetUploadInput{Name: "site", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("committed")})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := svc.GetStaticAssetDownload(ctx, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFacilityDownloadFile(t, committed, "committed")

	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	source, err := svc.GetFacilityEditAssetDownload(ctx, session.ID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFacilityDownloadFile(t, source, "committed")

	session, err = svc.PutFacilityEditAsset(ctx, session.ID, asset.ID, "replace", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "replace", Name: "site", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("replacement")})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := svc.GetFacilityEditAssetDownload(ctx, session.ID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFacilityDownloadFile(t, replacement, "replacement")
	if len(session.Assets) != 1 || session.Assets[0].AssetKey != asset.ID || session.Assets[0].SourceAssetID != asset.ID {
		t.Fatalf("replacement identity = %#v", session.Assets)
	}
}

func assertFacilityDownloadFile(t *testing.T, download FacilityAssetDownload, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(download.Root, download.Filename))
	if err != nil || string(content) != want {
		t.Fatalf("download content=%q err=%v", content, err)
	}
}

func TestFacilityEditSessionRestartRollsBackFilesBeforeDBCommit(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-new", "asset-put", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "asset-put", Name: "new", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := svc.loadFacilityEditSession(ctx, session.ID)
	manifest, err := svc.prepareFacilityCommitManifest(record)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(svc.facilityEditPath(session.ID), "commit-manifest.json")
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	lease := "lost"
	if _, err := store.AppDB().Exec(`UPDATE facility_edit_sessions SET state=?,commit_lease_owner=?,commit_lease_expires_at=?,manifest_path=? WHERE id=?`, FacilityEditSessionCommitting, lease, formatTime(time.Now().Add(-time.Minute)), manifestPath, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.moveFacilityManifestFiles(&manifest); err != nil {
		t.Fatal(err)
	}
	manifest.FilesMoved = true
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{}}, nil, WithDataRoot(svc.dataRoot))
	recovered, err := restarted.GetFacilityEditSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != FacilityEditSessionActive {
		t.Fatalf("state = %q", recovered.State)
	}
	var blobDir string
	if err := store.AppDB().QueryRow(`SELECT blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key='asset-new'`, session.ID).Scan(&blobDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blobDir); err != nil {
		t.Fatalf("staged blob was not restored: %v", err)
	}
}

func TestFacilityEditSessionRestartFinishesDBCommittedManifest(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset-new", "asset-put", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "asset-put", Name: "new", Kind: StaticSourceUploadedFile, FileName: "index.html", Content: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	record, _ := svc.loadFacilityEditSession(ctx, session.ID)
	manifest, err := svc.prepareFacilityCommitManifest(record)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(svc.facilityEditPath(session.ID), "commit-manifest.json")
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	lease := "lost"
	if _, err := store.AppDB().Exec(`UPDATE facility_edit_sessions SET state=?,commit_lease_owner=?,commit_lease_expires_at=?,manifest_path=? WHERE id=?`, FacilityEditSessionCommitting, lease, formatTime(time.Now().Add(-time.Minute)), manifestPath, session.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.moveFacilityManifestFiles(&manifest); err != nil {
		t.Fatal(err)
	}
	manifest.FilesMoved = true
	if err := writeFacilityManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	if err := svc.commitFacilityManifestDB(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{}}, nil, WithDataRoot(svc.dataRoot))
	recovered, err := restarted.GetFacilityEditSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State != FacilityEditSessionCommitted || recovered.CommitResult == nil {
		t.Fatalf("recovered = %#v", recovered)
	}
	if recovered.CommitResult.ApplyRequested || len(recovered.CommitResult.Diagnostics) < 2 || recovered.CommitResult.Diagnostics[1].Code != "facility_apply_request_failed" {
		t.Fatalf("recovery apply result = %#v", recovered.CommitResult)
	}
	if _, err := os.Stat(restarted.staticAssetDir(manifest.Assets[0].FinalID)); err != nil {
		t.Fatalf("committed asset missing: %v", err)
	}
}

func TestFacilityEditSessionRebasePreservesAssetsAndCanCommit(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	firstDraft := ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}}
	secondDraft := ReverseProxySaveInput{DeploymentServers: []string{"srv-b"}}
	first, _ := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{Draft: &firstDraft})
	second, _ := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{Draft: &secondDraft})
	second, err := svc.PutFacilityEditAsset(ctx, second.ID, "asset-draft", "put", FacilityEditAssetInput{Revision: second.Revision, ClientOperationID: "put", Name: "draft", Kind: StaticSourceUploadedFile, FileName: "a.txt", Content: []byte("asset")})
	if err != nil {
		t.Fatal(err)
	}
	firstPreview, _ := svc.PreviewFacilityEditSession(ctx, first.ID, first.Revision)
	if _, err := svc.CommitFacilityEditSession(ctx, first.ID, "first", CommitFacilityEditSessionInput{Revision: first.Revision, BaseResourceVersion: first.BaseResourceVersion.Value, PreviewToken: firstPreview.Token.Value}); err != nil {
		t.Fatal(err)
	}
	secondPreview, _ := svc.PreviewFacilityEditSession(ctx, second.ID, second.Revision)
	_, err = svc.CommitFacilityEditSession(ctx, second.ID, "second", CommitFacilityEditSessionInput{Revision: second.Revision, BaseResourceVersion: second.BaseResourceVersion.Value, PreviewToken: secondPreview.Token.Value})
	assertFacilityPanelError(t, err, "resource_version_conflict")
	current, _ := svc.loadConfig(ctx)
	rebased, err := svc.PatchFacilityEditSession(ctx, second.ID, PatchFacilityEditSessionInput{Revision: second.Revision, BaseResourceVersion: strconv.Itoa(current.Version), Draft: secondDraft})
	if err != nil {
		t.Fatal(err)
	}
	if len(rebased.Assets) != 1 || rebased.Assets[0].AssetKey != "asset-draft" {
		t.Fatalf("rebase lost assets: %#v", rebased.Assets)
	}
	preview, _ := svc.PreviewFacilityEditSession(ctx, rebased.ID, rebased.Revision)
	if _, err := svc.CommitFacilityEditSession(ctx, rebased.ID, "second-rebased", CommitFacilityEditSessionInput{Revision: rebased.Revision, BaseResourceVersion: rebased.BaseResourceVersion.Value, PreviewToken: preview.Token.Value}); err != nil {
		t.Fatal(err)
	}
}

func TestFacilityEditSessionRejectsCorruptedBlobBeforeCommit(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, _ := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	session, err := svc.PutFacilityEditAsset(ctx, session.ID, "asset", "put", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "put", Name: "asset", Kind: StaticSourceUploadedFile, FileName: "a.txt", Content: []byte("original")})
	if err != nil {
		t.Fatal(err)
	}
	var blobDir string
	if err := store.AppDB().QueryRow(`SELECT blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key='asset'`, session.ID).Scan(&blobDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blobDir, "content", "a.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, _ := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	_, err = svc.CommitFacilityEditSession(ctx, session.ID, "commit", CommitFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value})
	assertFacilityPanelError(t, err, "facility_asset_hash_mismatch")
	stored, _ := svc.GetFacilityEditSession(ctx, session.ID)
	if stored.State != FacilityEditSessionActive {
		t.Fatalf("state = %q", stored.State)
	}
}

func TestFacilityEditSessionRejectsCorruptedSourceAssetBeforeCommit(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	asset, err := svc.UploadStaticAsset(ctx, StaticAssetUploadInput{Name: "source", Kind: StaticSourceUploadedFile, FileName: "source.txt", Content: []byte("original")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svc.staticAssetContentDir(asset.ID), "source.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview, _ := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	_, err = svc.CommitFacilityEditSession(ctx, session.ID, "commit-source", CommitFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value})
	assertFacilityPanelError(t, err, "facility_asset_hash_mismatch")
}

func TestFacilityEditSessionTTLAndLiveCommitLease(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	expired, _ := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	old := formatTime(time.Now().UTC().Add(-time.Hour))
	_, _ = store.AppDB().Exec(`UPDATE facility_edit_sessions SET idle_expires_at=?,absolute_expires_at=? WHERE id=?`, old, old, expired.ID)
	got, err := svc.GetFacilityEditSession(ctx, expired.ID)
	if err != nil || got.State != FacilityEditSessionExpired {
		t.Fatalf("expired session=%#v err=%v", got, err)
	}
	live, _ := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	future := formatTime(time.Now().UTC().Add(time.Hour))
	_, _ = store.AppDB().Exec(`UPDATE facility_edit_sessions SET state=?,idle_expires_at=?,absolute_expires_at=?,commit_lease_owner='live',commit_lease_expires_at=? WHERE id=?`, FacilityEditSessionCommitting, old, old, future, live.ID)
	svc.cleanupFacilityEditSessions(time.Now().UTC())
	got, err = svc.GetFacilityEditSession(ctx, live.ID)
	if err != nil || got.State != FacilityEditSessionCommitting {
		t.Fatalf("live commit session=%#v err=%v", got, err)
	}
}

func TestFacilityDeleteMissingAssetIsIdempotentOperation(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, _ := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	input := FacilityEditMutationInput{Revision: session.Revision, ClientOperationID: "delete-missing"}
	first, err := svc.DeleteFacilityEditAsset(ctx, session.ID, "missing", "delete-key", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.DeleteFacilityEditAsset(ctx, session.ID, "missing", "delete-key", input)
	if err != nil || second.Revision != first.Revision {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestFacilityEditPutAndCleanupSerializeWorkspaceOwnership(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC()
	if _, err := store.AppDB().Exec(`UPDATE facility_edit_sessions SET idle_expires_at=?,absolute_expires_at=? WHERE id=?`, formatTime(base.Add(30*time.Second)), formatTime(base.Add(time.Hour)), session.ID); err != nil {
		t.Fatal(err)
	}
	type putResult struct {
		session FacilityEditSession
		err     error
	}
	putResults := make(chan putResult, 1)
	cleanupDone := make(chan struct{}, 1)
	start := make(chan struct{})
	go func() {
		<-start
		updated, putErr := svc.PutFacilityEditAsset(ctx, session.ID, "asset", "put-key", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "put-op", Name: "asset", Kind: StaticSourceUploadedFile, FileName: "a.txt", Content: []byte("hello")})
		putResults <- putResult{session: updated, err: putErr}
	}()
	go func() {
		<-start
		svc.cleanupFacilityEditSessions(base.Add(time.Minute))
		cleanupDone <- struct{}{}
	}()
	close(start)
	put := <-putResults
	<-cleanupDone
	if put.err != nil {
		var state string
		if err := store.AppDB().QueryRow(`SELECT state FROM facility_edit_sessions WHERE id=?`, session.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != FacilityEditSessionExpired {
			t.Fatalf("cleanup-first outcome state=%q err=%v", state, put.err)
		}
		return
	}
	if len(put.session.Assets) != 1 {
		t.Fatalf("successful put lost asset metadata: %#v", put.session.Assets)
	}
	var blobDir string
	if err := store.AppDB().QueryRow(`SELECT blob_dir FROM facility_edit_session_assets WHERE session_id=? AND asset_key='asset'`, session.ID).Scan(&blobDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(blobDir, "content", "a.txt")); err != nil {
		t.Fatalf("successful put lost workspace content: %v", err)
	}
}

func TestFacilityValidateAndPreviewDoNotReviveExpiredSessions(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	for _, action := range []string{"validate", "preview"} {
		t.Run(action, func(t *testing.T) {
			session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
			if err != nil {
				t.Fatal(err)
			}
			past := formatTime(time.Now().UTC().Add(-time.Minute))
			if _, err := store.AppDB().Exec(`UPDATE facility_edit_sessions SET idle_expires_at=?,absolute_expires_at=? WHERE id=?`, past, past, session.ID); err != nil {
				t.Fatal(err)
			}
			if action == "validate" {
				_, err = svc.ValidateFacilityEditSession(ctx, session.ID, session.Revision)
			} else {
				_, err = svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
			}
			if err == nil {
				t.Fatal("expired session was revived")
			}
			var state string
			if err := store.AppDB().QueryRow(`SELECT state FROM facility_edit_sessions WHERE id=?`, session.ID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != FacilityEditSessionExpired {
				t.Fatalf("state=%q", state)
			}
		})
	}
}

func TestFacilityAssetMutationsCannotCrossIdleDeadline(t *testing.T) {
	for _, action := range []string{"put", "delete"} {
		t.Run(action, func(t *testing.T) {
			svc, store, closeStore := newFacilityEditTestService(t)
			defer closeStore()
			ctx := context.Background()
			session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
			if err != nil {
				t.Fatal(err)
			}
			if action == "delete" {
				session, err = svc.PutFacilityEditAsset(ctx, session.ID, "asset", "seed-key", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "seed-op", Name: "asset", Kind: StaticSourceUploadedFile, FileName: "a.txt", Content: []byte("seed")})
				if err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().UTC().Add(300 * time.Millisecond)
			if _, err := store.AppDB().Exec(`UPDATE facility_edit_sessions SET idle_expires_at=? WHERE id=?`, formatTime(deadline), session.ID); err != nil {
				t.Fatal(err)
			}
			reached := make(chan struct{})
			release := make(chan struct{})
			svc.beforeFacilityEditRevisionBump = func() {
				close(reached)
				<-release
			}
			errResult := make(chan error, 1)
			go func() {
				if action == "put" {
					_, mutationErr := svc.PutFacilityEditAsset(ctx, session.ID, "asset", "put-deadline-key", FacilityEditAssetInput{Revision: session.Revision, ClientOperationID: "put-deadline-op", Name: "asset", Kind: StaticSourceUploadedFile, FileName: "a.txt", Content: []byte("hello")})
					errResult <- mutationErr
					return
				}
				_, mutationErr := svc.DeleteFacilityEditAsset(ctx, session.ID, "asset", "delete-deadline-key", FacilityEditMutationInput{Revision: session.Revision, ClientOperationID: "delete-deadline-op"})
				errResult <- mutationErr
			}()
			<-reached
			if wait := time.Until(deadline) + 20*time.Millisecond; wait > 0 {
				time.Sleep(wait)
			}
			close(release)
			assertFacilityPanelError(t, <-errResult, "edit_session_revision_conflict")
			svc.beforeFacilityEditRevisionBump = nil
			var revision int
			if err := store.AppDB().QueryRow(`SELECT revision FROM facility_edit_sessions WHERE id=?`, session.ID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if revision != session.Revision {
				t.Fatalf("revision advanced across idle deadline: got=%d want=%d", revision, session.Revision)
			}
			var assetCount int
			if err := store.AppDB().QueryRow(`SELECT COUNT(*) FROM facility_edit_session_assets WHERE session_id=? AND asset_key='asset'`, session.ID).Scan(&assetCount); err != nil {
				t.Fatal(err)
			}
			wantAssets := 0
			if action == "delete" {
				wantAssets = 1
			}
			if assetCount != wantAssets {
				t.Fatalf("asset mutation persisted across idle deadline: got=%d want=%d", assetCount, wantAssets)
			}
		})
	}
}

func TestFacilityCommitRecoversExpiredLeaseBeforeRetry(t *testing.T) {
	svc, store, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	past := formatTime(time.Now().UTC().Add(-time.Minute))
	if _, err := store.AppDB().Exec(`UPDATE facility_edit_sessions SET state=?,commit_lease_owner='stale',commit_lease_expires_at=?,manifest_path='' WHERE id=?`, FacilityEditSessionCommitting, past, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitFacilityEditSession(ctx, session.ID, "retry", CommitFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value}); err != nil {
		t.Fatal(err)
	}
}

func TestFacilityAssetDirectoryHashFramesEntries(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	if err := os.WriteFile(filepath.Join(first, "a"), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "c"), []byte("d"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "a"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "bc"), []byte("d"), 0o600); err != nil {
		t.Fatal(err)
	}
	firstHash, err := hashFacilityAssetDirectory(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := hashFacilityAssetDirectory(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatal("directory hash did not frame file boundaries")
	}
}

func TestFacilityArchiveLimitsRejectCountDepthSizeAndRatio(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		count      int
		extracted  int64
		compressed int64
	}{
		{name: "count", path: "a", count: facilityAssetArchiveMaxFiles + 1, extracted: 1, compressed: 1},
		{name: "depth", path: strings.Repeat("a/", facilityAssetArchiveMaxDepth) + "x", count: 1, extracted: 1, compressed: 1},
		{name: "size", path: "a", count: 1, extracted: facilityAssetArchiveMaxExtracted + 1, compressed: facilityAssetArchiveMaxExtracted},
		{name: "ratio", path: "a", count: 1, extracted: 2 << 20, compressed: 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertFacilityPanelError(t, validateFacilityArchiveLimits(tc.path, tc.count, tc.extracted, tc.compressed), "facility_static_asset_archive_limits_exceeded")
		})
	}
}

func TestFacilityArchiveRejectsEmptyDirectoryFlood(t *testing.T) {
	var raw bytes.Buffer
	zw := zip.NewWriter(&raw)
	for i := 0; i <= facilityAssetArchiveMaxFiles; i++ {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("d%05d/", i), Method: zip.Store}); err != nil {
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
	assertFacilityPanelError(t, extractZip(zr, t.TempDir(), int64(raw.Len())), "facility_static_asset_archive_limits_exceeded")
}

func TestFacilityTarRejectsEmptyDirectoryFlood(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := 0; i <= facilityAssetArchiveMaxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("d%05d/", i), Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	assertFacilityPanelError(t, extractTar(tar.NewReader(bytes.NewReader(raw.Bytes())), t.TempDir(), int64(raw.Len())), "facility_static_asset_archive_limits_exceeded")
}

func TestFacilityTarRejectsIgnoredHeaderFlood(t *testing.T) {
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for i := 0; i <= facilityAssetArchiveMaxFiles; i++ {
		if err := tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("l%05d", i), Typeflag: tar.TypeSymlink, Linkname: "target", Mode: 0o777}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	assertFacilityPanelError(t, extractTar(tar.NewReader(bytes.NewReader(raw.Bytes())), t.TempDir(), int64(raw.Len())), "facility_static_asset_archive_limits_exceeded")
}

type successfulFacilityReconciler struct{}

func (successfulFacilityReconciler) TriggerApplicationReconcile(context.Context, tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	return tasks.Task{ID: "operation"}, true, nil
}

func TestFacilityEditCommitRegistersUnregisteredPanelHost(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	host := &facilityPanelHostFake{}
	svc.panelHost = host
	ctx := context.Background()
	session, err := svc.BeginFacilityEditSession(ctx, BeginFacilityEditSessionInput{Draft: &ReverseProxySaveInput{
		DeploymentServers: []string{"srv-a"},
		PanelEntry:        PanelEntry{Enabled: true, ServerID: "srv-a", Domain: "panel.example.test"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.PreviewFacilityEditSession(ctx, session.ID, session.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CommitFacilityEditSession(ctx, session.ID, "commit-host-1", CommitFacilityEditSessionInput{Revision: session.Revision, BaseResourceVersion: session.BaseResourceVersion.Value, PreviewToken: preview.Token.Value}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.registered, []string{"srv-a"}) {
		t.Fatalf("registered=%v want [srv-a]", host.registered)
	}
}

func newFacilityEditTestService(t *testing.T) (*Service, *storage.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.DataRoot = filepath.Join(dir, "data")
	cfg.AppDatabase = filepath.Join(dir, "app.db")
	cfg.MetricsDatabase = filepath.Join(dir, "metrics.db")
	cfg.LogDatabase = filepath.Join(dir, "log.db")
	cfg.CoordinationDatabase = filepath.Join(dir, "coordination.db")
	store, err := storage.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store.AppDB(), nil, facilityTestServers{items: map[string]server.Server{}}, nil, WithDataRoot(cfg.DataRoot), WithCoordDB(store.CoordDB()))
	return svc, store, func() { _ = store.Close() }
}

func assertFacilityPanelError(t *testing.T, err error, code string) {
	t.Helper()
	var target *panelerr.Error
	if !errors.As(err, &target) || target.Code != code {
		t.Fatalf("error=%v want=%s", err, code)
	}
}

type fakeFacilityReconcileTrigger struct{}

func (fakeFacilityReconcileTrigger) TriggerApplicationReconcile(context.Context, tasks.PeriodicTrigger) (tasks.Task, bool, error) {
	return tasks.Task{}, false, nil
}

// TestReconcileReverseProxyNowClearsStaleLastError 回归测试：手动协调成功后必须
// 清空持久化的 last_error，否则旧的失败横幅（如端口校验错误）会一直留在设施
// 详情页，即使根因已修复。
func TestReconcileReverseProxyNowClearsStaleLastError(t *testing.T) {
	svc, _, closeStore := newFacilityEditTestService(t)
	defer closeStore()
	ctx := context.Background()
	// 先保存一次配置建立 facility_app_configs 行；无协调器时保存成功但
	// last_error 会被触发错误填充。
	if _, err := svc.SaveReverseProxy(ctx, ReverseProxySaveInput{DeploymentServers: []string{"srv-a"}}); err != nil {
		t.Fatal(err)
	}
	// 预置一条旧失败信息（模拟端口校验错误横幅）。
	if err := svc.setLastError(ctx, "reverse proxy target port must be between 1 and 65535"); err != nil {
		t.Fatal(err)
	}
	// 注入协调器后手动协调成功，旧 last_error 必须被清空。
	svc.reconciler = fakeFacilityReconcileTrigger{}
	result, err := svc.ReconcileReverseProxyNow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Config.LastError != "" {
		t.Fatalf("lastError = %q, want cleared after successful reconcile", result.Config.LastError)
	}
}
