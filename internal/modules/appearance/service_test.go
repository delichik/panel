package appearance

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
)

// newTestService 用真实 SQLite（由模型建表）与临时 dataRoot 构造外观服务。
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := orm.AutoMigrateModels(context.Background(), db, []any{&models.AppearanceSetting{}}, orm.WithDestructive(true)); err != nil {
		t.Fatalf("migrate appearance_settings: %v", err)
	}
	dataRoot := filepath.Join(dir, "data")
	svc := NewService(db, dataRoot)
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	if err := svc.EnsureSeeded(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return svc, dataRoot
}

func requireCode(t *testing.T, err error, want string) {
	t.Helper()
	var domain *panelerr.Error
	if !errors.As(err, &domain) {
		t.Fatalf("expected panel error %q, got %v", want, err)
	}
	if domain.Code != want {
		t.Fatalf("error code = %q, want %q", domain.Code, want)
	}
}

func stringPtr(value string) *string { return &value }

func encodeTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func encodeTestPNG(t *testing.T, width, height int, transparent bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent && x == 0 && y == 0 {
				alpha = 0
			}
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 200, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func mustGet(t *testing.T, svc *Service) Appearance {
	t.Helper()
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return got
}

func mustUpload(t *testing.T, svc *Service, content []byte) Appearance {
	t.Helper()
	got, err := svc.UploadBackground(context.Background(), content)
	if err != nil {
		t.Fatalf("UploadBackground: %v", err)
	}
	return got
}

// TestEnsureSeededDefaults 验证首次启动幂等默认行与默认外观。
func TestEnsureSeededDefaults(t *testing.T) {
	svc, _ := newTestService(t)
	// 二次 seed 不得覆盖/重复。
	if err := svc.EnsureSeeded(context.Background()); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	got := mustGet(t, svc)
	if got.ThemeMode != ThemeModeSystem || !got.Mica || got.Accent != AccentBlue {
		t.Fatalf("default appearance = %#v", got)
	}
	if got.Background.Kind != BackgroundKindPreset || got.Background.HasImage || got.Background.AssetID != nil || got.Background.PresetID == nil || *got.Background.PresetID != PresetMist {
		t.Fatalf("default background = %#v", got.Background)
	}
	if got.Initialized || got.ThumbDataURL != nil {
		t.Fatalf("default flags = %#v", got)
	}
	var count int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM appearance_settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("appearance_settings rows = %d, want 1", count)
	}
}

// TestUpdateValidatesAndWritesWholeDocument 覆盖 PUT 校验、零写入与整体替换。
func TestUpdateValidatesAndWritesWholeDocument(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	invalid := []struct {
		name string
		in   AppearanceUpdate
		code string
	}{
		{"theme", AppearanceUpdate{ThemeMode: "neon", Mica: true, Accent: AccentBlue}, "invalid_theme_mode"},
		{"accent", AppearanceUpdate{ThemeMode: ThemeModeDark, Mica: true, Accent: "cyan"}, "invalid_appearance_accent"},
		{"kind", AppearanceUpdate{ThemeMode: ThemeModeDark, Mica: false, Accent: AccentTeal, Background: &BackgroundUpdate{Kind: "wallpaper"}}, "invalid_background_kind"},
		{"preset missing", AppearanceUpdate{ThemeMode: ThemeModeDark, Mica: false, Accent: AccentTeal, Background: &BackgroundUpdate{Kind: BackgroundKindPreset}}, "invalid_background_kind"},
		{"preset unknown", AppearanceUpdate{ThemeMode: ThemeModeDark, Mica: false, Accent: AccentTeal, Background: &BackgroundUpdate{Kind: BackgroundKindPreset, PresetID: stringPtr("lighthouse")}}, "invalid_background_kind"},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Update(ctx, tt.in)
			requireCode(t, err, tt.code)
			got := mustGet(t, svc)
			if got.ThemeMode != ThemeModeSystem || got.Accent != AccentBlue || !got.Mica || got.Background.Kind != BackgroundKindPreset {
				t.Fatalf("validation failure must not write: %#v", got)
			}
		})
	}

	updated, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeDark, Mica: false, Accent: AccentPurple, Background: &BackgroundUpdate{Kind: BackgroundKindPreset, PresetID: stringPtr(PresetMist)}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.ThemeMode != ThemeModeDark || updated.Mica || updated.Accent != AccentPurple {
		t.Fatalf("updated = %#v", updated)
	}
	if updated.Background.Kind != BackgroundKindPreset || updated.Background.PresetID == nil || *updated.Background.PresetID != PresetMist {
		t.Fatalf("background = %#v", updated.Background)
	}
	if !updated.Initialized || updated.UpdatedAt == "" {
		t.Fatalf("initialized/updatedAt = %#v", updated)
	}
	again := mustGet(t, svc)
	if again.ThemeMode != ThemeModeDark || again.Accent != AccentPurple || again.Background.Kind != BackgroundKindPreset {
		t.Fatalf("persisted = %#v", again)
	}
}

// TestUpdateKeepsAndReEnablesUploadedImage 验证 background 省略/切换 none/preset
// 保留资产，kind=image 可重新启用，DELETE 才删除资产。
func TestUpdateKeepsAndReEnablesUploadedImage(t *testing.T) {
	svc, dataRoot := newTestService(t)
	ctx := context.Background()
	uploaded := mustUpload(t, svc, encodeTestJPEG(t, 640, 360))
	if !uploaded.Background.HasImage || uploaded.Background.Kind != BackgroundKindImage || uploaded.Background.AssetID == nil {
		t.Fatalf("uploaded = %#v", uploaded.Background)
	}
	assetID := *uploaded.Background.AssetID
	assetDir := filepath.Join(dataRoot, "appearance", assetID)

	// 省略 background：只改主题，图片资产与 thumb 必须保留。
	kept, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeLight, Mica: true, Accent: AccentGreen})
	if err != nil {
		t.Fatalf("Update omit background: %v", err)
	}
	if kept.Background.Kind != BackgroundKindImage || !kept.Background.HasImage || kept.ThumbDataURL == nil {
		t.Fatalf("omit background must keep image: %#v", kept)
	}
	if _, err := os.Stat(assetDir); err != nil {
		t.Fatalf("asset dir must survive PUT without background: %v", err)
	}

	// kind=none：隐藏但保留资产，响应仍带 thumbDataUrl。
	hidden, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeLight, Mica: true, Accent: AccentGreen, Background: &BackgroundUpdate{Kind: BackgroundKindNone}})
	if err != nil {
		t.Fatalf("Update none: %v", err)
	}
	if hidden.Background.Kind != BackgroundKindNone || !hidden.Background.HasImage || hidden.ThumbDataURL == nil || hidden.Background.AssetID == nil || *hidden.Background.AssetID != assetID {
		t.Fatalf("none background must retain asset: %#v", hidden)
	}
	if _, err := os.Stat(assetDir); err != nil {
		t.Fatalf("asset dir must survive kind=none: %v", err)
	}

	// kind=preset：仍保留资产，thumb 继续返回。
	preset, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeLight, Mica: true, Accent: AccentGreen, Background: &BackgroundUpdate{Kind: BackgroundKindPreset, PresetID: stringPtr(PresetAurora)}})
	if err != nil {
		t.Fatalf("Update preset: %v", err)
	}
	if preset.Background.Kind != BackgroundKindPreset || !preset.Background.HasImage || preset.ThumbDataURL == nil {
		t.Fatalf("preset background must retain asset: %#v", preset)
	}

	// kind=image：重新启用。
	reenabled, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeLight, Mica: true, Accent: AccentGreen, Background: &BackgroundUpdate{Kind: BackgroundKindImage}})
	if err != nil {
		t.Fatalf("Update image: %v", err)
	}
	if reenabled.Background.Kind != BackgroundKindImage || reenabled.ThumbDataURL == nil {
		t.Fatalf("re-enabled image = %#v", reenabled)
	}

	// DELETE 才删除资产。
	deleted, err := svc.RemoveBackground(ctx)
	if err != nil {
		t.Fatalf("RemoveBackground: %v", err)
	}
	if deleted.Background.Kind != BackgroundKindNone || deleted.Background.HasImage || deleted.Background.AssetID != nil || deleted.ThumbDataURL != nil {
		t.Fatalf("deleted = %#v", deleted)
	}
	if _, err := os.Stat(assetDir); !os.IsNotExist(err) {
		t.Fatalf("asset dir must be removed, stat err = %v", err)
	}
	// 幂等：再次删除返回当前外观且不报错。
	again, err := svc.RemoveBackground(ctx)
	if err != nil || again.Background.HasImage {
		t.Fatalf("idempotent remove = %#v, %v", again, err)
	}
}

// TestRemoveBackgroundFallsBackToNoneWithoutImage 验证 DELETE 在仅有预设/无图时
// 也要把背景回落为 none，且重复删除不再改变 revision。
func TestRemoveBackgroundFallsBackToNoneWithoutImage(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	if _, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeSystem, Mica: true, Accent: AccentBlue, Background: &BackgroundUpdate{Kind: BackgroundKindPreset, PresetID: stringPtr(PresetSlate)}}); err != nil {
		t.Fatalf("Update preset: %v", err)
	}
	deleted, err := svc.RemoveBackground(ctx)
	if err != nil {
		t.Fatalf("RemoveBackground: %v", err)
	}
	if deleted.Background.Kind != BackgroundKindNone || deleted.Background.PresetID != nil || deleted.Background.HasImage {
		t.Fatalf("deleted preset = %#v", deleted)
	}
	again, err := svc.RemoveBackground(ctx)
	if err != nil {
		t.Fatalf("second RemoveBackground: %v", err)
	}
	if again.Background.Revision != deleted.Background.Revision || again.Background.Kind != BackgroundKindNone {
		t.Fatalf("repeated delete must be idempotent: first=%#v second=%#v", deleted.Background, again.Background)
	}
}

// TestUpdateImageWithoutAssetRejected 验证 kind=image 无资产时返回
// background_required 且不写入。
func TestUpdateImageWithoutAssetRejected(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.Update(context.Background(), AppearanceUpdate{ThemeMode: ThemeModeSystem, Mica: true, Accent: AccentBlue, Background: &BackgroundUpdate{Kind: BackgroundKindImage}})
	requireCode(t, err, "background_required")
	got := mustGet(t, svc)
	if got.Initialized || got.Background.Kind != BackgroundKindPreset {
		t.Fatalf("failed update must not write: %#v", got)
	}
}

// TestUploadNormalizesStoresAndServes 验证上传的入库、归一化、缩略图与下发。
func TestUploadNormalizesStoresAndServes(t *testing.T) {
	svc, dataRoot := newTestService(t)
	ctx := context.Background()
	uploaded := mustUpload(t, svc, encodeTestJPEG(t, 3000, 1500))
	if uploaded.Background.Revision != 1 {
		t.Fatalf("revision = %d, want 1", uploaded.Background.Revision)
	}
	if uploaded.Background.AssetID == nil || !validAssetID(*uploaded.Background.AssetID) {
		t.Fatalf("assetId = %v", uploaded.Background.AssetID)
	}
	assetID := *uploaded.Background.AssetID
	masterPath := filepath.Join(dataRoot, "appearance", assetID, "master.jpg")
	if _, err := os.Stat(masterPath); err != nil {
		t.Fatalf("master: %v", err)
	}
	masterBytes, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	master, err := jpeg.Decode(bytes.NewReader(masterBytes))
	if err != nil {
		t.Fatalf("decode master: %v", err)
	}
	// 3000x1500 → 最大边 2560（2560x1280），保持等比。
	if master.Bounds().Dx() != 2560 || master.Bounds().Dy() != 1280 {
		t.Fatalf("master size = %dx%d, want 2560x1280", master.Bounds().Dx(), master.Bounds().Dy())
	}
	thumbBytes, err := os.ReadFile(filepath.Join(dataRoot, "appearance", assetID, "thumb.jpg"))
	if err != nil {
		t.Fatalf("thumb: %v", err)
	}
	thumb, err := jpeg.Decode(bytes.NewReader(thumbBytes))
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	if thumb.Bounds().Dx() != thumbWidth || thumb.Bounds().Dy() != thumbHeight {
		t.Fatalf("thumb size = %dx%d", thumb.Bounds().Dx(), thumb.Bounds().Dy())
	}
	if uploaded.ThumbDataURL == nil || len(*uploaded.ThumbDataURL) < len("data:image/jpeg;base64,") || (*uploaded.ThumbDataURL)[:23] != "data:image/jpeg;base64," {
		t.Fatalf("thumbDataUrl = %v", uploaded.ThumbDataURL)
	}
	if !uploaded.Initialized {
		t.Fatal("upload must mark initialized")
	}

	full, err := svc.BackgroundContent(ctx, "full")
	if err != nil {
		t.Fatalf("full content: %v", err)
	}
	if full.ContentType != "image/jpeg" || !bytes.Equal(full.Content, masterBytes) {
		t.Fatalf("full content type=%q len=%d", full.ContentType, len(full.Content))
	}
	if full.ETag != `"`+assetID+`-full"` {
		t.Fatalf("full etag = %q", full.ETag)
	}
	thumbContent, err := svc.BackgroundContent(ctx, "thumb")
	if err != nil {
		t.Fatalf("thumb content: %v", err)
	}
	if thumbContent.ContentType != "image/jpeg" || !bytes.Equal(thumbContent.Content, thumbBytes) {
		t.Fatalf("thumb content type=%q", thumbContent.ContentType)
	}
	if _, err := svc.BackgroundContent(ctx, "huge"); err == nil {
		t.Fatal("invalid variant must fail")
	} else {
		requireCode(t, err, "bad_request")
	}
}

// TestUploadPNGKeepsAlpha 验证 PNG 重编码保留透明通道。
func TestUploadPNGKeepsAlpha(t *testing.T) {
	svc, dataRoot := newTestService(t)
	uploaded := mustUpload(t, svc, encodeTestPNG(t, 200, 100, true))
	assetID := *uploaded.Background.AssetID
	if uploaded.Background.Kind != BackgroundKindImage {
		t.Fatalf("kind = %q", uploaded.Background.Kind)
	}
	masterBytes, err := os.ReadFile(filepath.Join(dataRoot, "appearance", assetID, "master.png"))
	if err != nil {
		t.Fatalf("master.png: %v", err)
	}
	master, err := png.Decode(bytes.NewReader(masterBytes))
	if err != nil {
		t.Fatalf("decode png master: %v", err)
	}
	_, _, _, alpha := master.At(0, 0).RGBA()
	if alpha != 0 {
		t.Fatalf("transparent pixel alpha = %d, want 0", alpha)
	}
}

// TestUploadRejectsUnsupportedOversizedAndBroken 覆盖图片管线的四类拒绝。
func TestUploadRejectsUnsupportedOversizedAndBroken(t *testing.T) {
	svc, dataRoot := newTestService(t)
	ctx := context.Background()
	gif := []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;")
	_, err := svc.UploadBackground(ctx, gif)
	requireCode(t, err, "background_unsupported_type")

	webp := append([]byte("RIFF\x24\x00\x00\x00WEBPVP8 "), make([]byte, 8)...)
	_, err = svc.UploadBackground(ctx, webp)
	requireCode(t, err, "background_unsupported_type")

	oversized := make([]byte, MaxBackgroundUploadBytes+1)
	copy(oversized, encodeTestJPEG(t, 10, 10))
	_, err = svc.UploadBackground(ctx, oversized)
	requireCode(t, err, "background_too_large")

	wide := encodeTestPNG(t, 4097, 8, false)
	_, err = svc.UploadBackground(ctx, wide)
	requireCode(t, err, "background_dimensions_exceeded")

	tall := encodeTestPNG(t, 8, 4097, false)
	_, err = svc.UploadBackground(ctx, tall)
	requireCode(t, err, "background_dimensions_exceeded")

	broken := encodeTestJPEG(t, 64, 64)
	_, err = svc.UploadBackground(ctx, append(broken[:32], make([]byte, 16)...))
	requireCode(t, err, "background_invalid_image")

	empty, err := svc.UploadBackground(ctx, nil)
	requireCode(t, err, "background_required")
	_ = empty

	entries, readErr := os.ReadDir(filepath.Join(dataRoot, "appearance"))
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read appearance dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed uploads must not leave assets: %v", entries)
	}
	current := mustGet(t, svc)
	if current.Background.HasImage || current.Background.Revision != 0 {
		t.Fatalf("failed uploads must not write: %#v", current)
	}
}

// TestUploadReplacesAtomically 验证新资产先写、旧目录随后删除。
func TestUploadReplacesAtomically(t *testing.T) {
	svc, dataRoot := newTestService(t)
	first := mustUpload(t, svc, encodeTestJPEG(t, 320, 180))
	firstID := *first.Background.AssetID
	second := mustUpload(t, svc, encodeTestPNG(t, 400, 200, false))
	secondID := *second.Background.AssetID
	if firstID == secondID {
		t.Fatal("different content must produce different asset ids")
	}
	if second.Background.Revision != 2 || second.Initialized != true {
		t.Fatalf("second upload = %#v", second)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "appearance", firstID)); !os.IsNotExist(err) {
		t.Fatalf("old asset dir must be removed, err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "appearance", secondID, "master.png")); err != nil {
		t.Fatalf("new asset dir must exist: %v", err)
	}
}

// TestMissingAssetDegradesToNone 验证 DB 有记录但文件缺失时降级并告警。
func TestMissingAssetDegradesToNone(t *testing.T) {
	svc, dataRoot := newTestService(t)
	uploaded := mustUpload(t, svc, encodeTestJPEG(t, 320, 180))
	assetID := *uploaded.Background.AssetID
	if err := os.RemoveAll(filepath.Join(dataRoot, "appearance", assetID)); err != nil {
		t.Fatal(err)
	}
	got := mustGet(t, svc)
	if got.Background.Kind != BackgroundKindNone || got.Background.HasImage || got.Background.AssetID != nil || got.ThumbDataURL != nil {
		t.Fatalf("degraded = %#v", got)
	}
	if got.Background.Revision != uploaded.Background.Revision+1 {
		t.Fatalf("degrade revision = %d, want %d", got.Background.Revision, uploaded.Background.Revision+1)
	}
	if _, err := svc.BackgroundContent(context.Background(), "full"); err == nil {
		t.Fatal("missing asset content must 404")
	} else {
		requireCode(t, err, "not_found")
	}
}

// TestPublicAppearanceNeverLeaksMaster 验证公开外观只带 kind=image 的缩略图，
// 且不含 assetId/revision。
func TestPublicAppearanceNeverLeaksMaster(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()
	uploaded := mustUpload(t, svc, encodeTestJPEG(t, 640, 360))

	publicWithImage, err := svc.PublicAppearance(ctx)
	if err != nil {
		t.Fatalf("PublicAppearance: %v", err)
	}
	if publicWithImage.Background.Kind != BackgroundKindImage || !publicWithImage.Background.HasImage || publicWithImage.ThumbDataURL == nil {
		t.Fatalf("public image = %#v", publicWithImage)
	}
	if publicWithImage.ThemeMode != ThemeModeSystem || publicWithImage.Accent != AccentBlue || !publicWithImage.Mica {
		t.Fatalf("public theme = %#v", publicWithImage)
	}

	if _, err := svc.Update(ctx, AppearanceUpdate{ThemeMode: ThemeModeDark, Mica: true, Accent: AccentRed, Background: &BackgroundUpdate{Kind: BackgroundKindPreset, PresetID: stringPtr(PresetDusk)}}); err != nil {
		t.Fatalf("Update preset: %v", err)
	}
	publicPreset, err := svc.PublicAppearance(ctx)
	if err != nil {
		t.Fatalf("PublicAppearance preset: %v", err)
	}
	if publicPreset.ThumbDataURL != nil {
		t.Fatalf("public preset must not carry thumbnail: %#v", publicPreset)
	}
	if !publicPreset.Background.HasImage || publicPreset.Background.Kind != BackgroundKindPreset {
		t.Fatalf("public preset background = %#v", publicPreset.Background)
	}
	if publicPreset.Background.PresetID == nil || *publicPreset.Background.PresetID != PresetDusk {
		t.Fatalf("presetId = %v", publicPreset.Background.PresetID)
	}
	// 公开响应不能出现原图 base64（缩略图长度远小于 master）。
	if uploaded.ThumbDataURL != nil && publicWithImage.ThumbDataURL != nil && *publicWithImage.ThumbDataURL != *uploaded.ThumbDataURL {
		t.Fatal("public thumbnail must equal validated thumb")
	}
}

// TestCleanupOrphansRemovesOnlyStaleUnreferencedDirs 覆盖启动/定时孤儿清理。
func TestCleanupOrphansRemovesOnlyStaleUnreferencedDirs(t *testing.T) {
	svc, dataRoot := newTestService(t)
	ctx := context.Background()
	uploaded := mustUpload(t, svc, encodeTestJPEG(t, 320, 180))
	currentID := *uploaded.Background.AssetID

	root := filepath.Join(dataRoot, "appearance")
	staleID := "0123456789abcdef0123456789abcdef"
	staleDir := filepath.Join(root, staleID)
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(staleDir, "master.jpg")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-48 * time.Hour)
	if err := os.Chtimes(staleFile, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(staleDir, old, old); err != nil {
		t.Fatal(err)
	}
	freshID := "fedcba9876543210fedcba9876543210"
	freshDir := filepath.Join(root, freshID)
	if err := os.MkdirAll(freshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	freshFile := filepath.Join(freshDir, "master.jpg")
	if err := os.WriteFile(freshFile, []byte("fresh"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := svc.CleanupOrphans(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Fatalf("stale orphan must be removed, err = %v", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("fresh orphan must be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, currentID)); err != nil {
		t.Fatalf("current asset must be kept: %v", err)
	}
}
