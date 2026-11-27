// Package appearance 持久化全局面板外观：主题模式、Mica、强调色与背景图。
//
// 面板是单管理员全局面板，外观也只有一份全局设置（AppDB appearance_settings
// 单行，id='appearance'），最后写入生效，不做 revision/乐观锁。背景图片字节
// 不进数据库：归一化后的 master 与 160x90 缩略图存放在
// <dataRoot>/appearance/<assetID>/，数据库只保存资产元数据。写入顺序固定为
// “写新资产目录 → 提交 DB → 删除旧资产目录”，崩溃最多留下可由孤儿清理回收的
// 目录，不会出现 DB 指向缺失文件。
package appearance

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/filestorage"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

const (
	// configID 是单行设置固定主键。
	configID = "appearance"
	// appearanceDirName 是 dataRoot 下的外观资产目录名。
	appearanceDirName = "appearance"
	masterFileJPEG    = "master.jpg"
	masterFilePNG     = "master.png"
	thumbFileJPEG     = "thumb.jpg"
	// assetIDLength 是内容 SHA-256 十六进制前缀长度（目录名/ETag 缓存键）。
	assetIDLength = 32
	// backgroundOrphanStaleAge 是孤儿资产目录的最短保留时间，避免清理掉
	// “目录已写、DB 尚未提交”的瞬时中间态。
	backgroundOrphanStaleAge = 24 * time.Hour
	// orphanCleanupPeriod 是定时孤儿清理周期。
	orphanCleanupPeriod = 6 * time.Hour
)

// 主题模式枚举。
const (
	ThemeModeSystem = "system"
	ThemeModeLight  = "light"
	ThemeModeDark   = "dark"
)

// 背景类型枚举。
const (
	BackgroundKindNone   = "none"
	BackgroundKindPreset = "preset"
	BackgroundKindImage  = "image"
)

// 强调色枚举（Windows 个性化色板）。
const (
	AccentBlue   = "blue"
	AccentTeal   = "teal"
	AccentGreen  = "green"
	AccentGold   = "gold"
	AccentOrange = "orange"
	AccentRed    = "red"
	AccentPink   = "pink"
	AccentPurple = "purple"
	AccentGray   = "gray"
)

// 预设背景枚举（前端渲染 CSS 渐变，服务端只存 id）。
const (
	PresetAurora = "aurora"
	PresetDusk   = "dusk"
	PresetMist   = "mist"
	PresetSlate  = "slate"
)

var (
	themeModes = map[string]struct{}{
		ThemeModeSystem: {}, ThemeModeLight: {}, ThemeModeDark: {},
	}
	accentIDs = map[string]struct{}{
		AccentBlue: {}, AccentTeal: {}, AccentGreen: {}, AccentGold: {}, AccentOrange: {},
		AccentRed: {}, AccentPink: {}, AccentPurple: {}, AccentGray: {},
	}
	presetIDs = map[string]struct{}{
		PresetAurora: {}, PresetDusk: {}, PresetMist: {}, PresetSlate: {},
	}
)

// AppearanceBackground 是 GET /settings/appearance 中的背景摘要。
type AppearanceBackground struct {
	Kind     string  `json:"kind"`
	PresetID *string `json:"presetId"`
	AssetID  *string `json:"assetId"`
	Revision int     `json:"revision"`
	HasImage bool    `json:"hasImage"`
}

// Appearance 是 GET /settings/appearance、PUT 与上传/删除接口的完整响应。
type Appearance struct {
	ThemeMode    string               `json:"themeMode"`
	Mica         bool                 `json:"mica"`
	Accent       string               `json:"accent"`
	Background   AppearanceBackground `json:"background"`
	ThumbDataURL *string              `json:"thumbDataUrl"`
	Initialized  bool                 `json:"initialized"`
	UpdatedAt    string               `json:"updatedAt"`
}

// PublicBackground 是公开接口返回的背景摘要（不含 assetId/revision）。
type PublicBackground struct {
	Kind     string  `json:"kind"`
	PresetID *string `json:"presetId"`
	HasImage bool    `json:"hasImage"`
}

// PublicAppearance 是登录页使用的公开外观（不含原图，仅 kind=image 时给缩略图）。
type PublicAppearance struct {
	ThemeMode    string           `json:"themeMode"`
	Mica         bool             `json:"mica"`
	Accent       string           `json:"accent"`
	Background   PublicBackground `json:"background"`
	ThumbDataURL *string          `json:"thumbDataUrl"`
}

// BackgroundUpdate 是 PUT 请求中的背景选择。Kind 允许 none|preset|image；
// none/preset 会保留已上传资产，image 重新启用已上传资产。
type BackgroundUpdate struct {
	Kind     string  `json:"kind"`
	PresetID *string `json:"presetId"`
}

// AppearanceUpdate 是 PUT /settings/appearance 的请求体。Background 省略表示
// 保留当前背景（包括已上传图片资产），绝不删除文件。
type AppearanceUpdate struct {
	ThemeMode  string            `json:"themeMode"`
	Mica       bool              `json:"mica"`
	Accent     string            `json:"accent"`
	Background *BackgroundUpdate `json:"background"`
}

// BackgroundContent 是 GET /settings/appearance/background/content 的响应内容。
type BackgroundContent struct {
	Content     []byte
	ContentType string
	ETag        string
	ModTime     time.Time
}

// appearanceRow 是 appearance_settings 单行的内存表示。
type appearanceRow struct {
	themeMode   string
	mica        bool
	accent      string
	kind        string
	preset      string
	assetID     string
	mime        string
	width       int
	height      int
	bytes       int
	revision    int
	initialized bool
	updatedAt   time.Time
}

// Service 是外观设置服务。所有写操作由 mu 串行化，避免并发上传/删除交叉
// 删掉对方正在使用的资产目录（单管理员场景下最后一次写入生效）。
type Service struct {
	db       *sql.DB
	dataRoot string
	now      func() time.Time

	mu sync.Mutex
}

// NewService 创建外观设置服务。dataRoot 是 Panel 数据根目录。
func NewService(db *sql.DB, dataRoot string) *Service {
	return &Service{
		db:       db,
		dataRoot: dataRoot,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// EnsureSeeded 在表为空时幂等写入默认行（themeMode=system、mica=true、
// accent=blue、background=preset/mist、initialized=false）。启动装配调用，失败应导致
// 启动失败。对从未保存过外观的旧行（initialized=0 且无上传资产）补默认背景；
// 用户显式选择 none 后 initialized=1，不会被覆盖。
func (s *Service) EnsureSeeded(ctx context.Context) error {
	now := s.now().UTC()
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO appearance_settings(id,theme_mode,mica,accent,background_kind,background_preset,background_asset_id,background_mime,background_width,background_height,background_bytes,background_revision,initialized,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO NOTHING`,
		configID, ThemeModeSystem, boolInt(true), AccentBlue, BackgroundKindPreset, PresetMist, "", "", 0, 0, 0, 0, boolInt(false), formatTime(now))
	if err != nil {
		return err
	}
	// 旧版本默认是 none；从未初始化过的行升级到默认预设。
	_, err = orm.RawExec(ctx, s.db, `UPDATE appearance_settings
SET background_kind=?, background_preset=?, updated_at=?
WHERE id=? AND initialized=0 AND background_kind=? AND background_asset_id=''`,
		BackgroundKindPreset, PresetMist, formatTime(now), configID, BackgroundKindNone)
	return err
}

// Get 返回当前外观。若 DB 记录指向的背景文件缺失，自动降级为无背景并告警。
// hasImage=true 时响应总是带 thumbDataUrl（与当前 kind 无关），便于设置页预览
// 和重新启用已上传图片。
func (s *Service) Get(ctx context.Context) (Appearance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return Appearance{}, err
	}
	row = s.reconcileRowLocked(ctx, row)
	return s.buildAppearanceLocked(ctx, row), nil
}

// Update 整体替换主题/Mica/强调色（以及可选的背景选择）。校验失败不写库。
// 省略 background 时保留当前背景（包括已上传图片）；kind=none/preset 隐藏背景
// 但保留资产；kind=image 重新启用已上传资产，无资产返回 background_required。
func (s *Service) Update(ctx context.Context, in AppearanceUpdate) (Appearance, error) {
	themeMode := strings.ToLower(strings.TrimSpace(in.ThemeMode))
	if _, ok := themeModes[themeMode]; !ok {
		return Appearance{}, panelerr.Validation("invalid_theme_mode", "Theme mode must be system, light or dark")
	}
	accent := strings.ToLower(strings.TrimSpace(in.Accent))
	if _, ok := accentIDs[accent]; !ok {
		return Appearance{}, panelerr.Validation("invalid_appearance_accent", "Accent must be one of the supported presets")
	}
	background, err := normalizeBackgroundUpdate(in.Background)
	if err != nil {
		return Appearance{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return Appearance{}, err
	}
	row = s.reconcileRowLocked(ctx, row)
	row.themeMode = themeMode
	row.mica = in.Mica
	row.accent = accent
	switch {
	case background == nil:
		// 保留当前背景（含已上传图片资产），不修改任何背景列。
	case background.Kind == BackgroundKindNone:
		row.kind = BackgroundKindNone
		row.preset = ""
	case background.Kind == BackgroundKindPreset:
		row.kind = BackgroundKindPreset
		row.preset = background.Preset
	case background.Kind == BackgroundKindImage:
		if row.assetID == "" {
			return Appearance{}, backgroundRequiredError()
		}
		row.kind = BackgroundKindImage
		row.preset = ""
	}
	row.initialized = true
	row.updatedAt = s.now().UTC()
	if err := s.persistRowLocked(ctx, row); err != nil {
		return Appearance{}, err
	}
	return s.buildAppearanceLocked(ctx, row), nil
}

// normalizedBackground 是校验后的背景选择。
type normalizedBackgroundUpdate struct {
	Kind   string
	Preset string
}

// normalizeBackgroundUpdate 校验并归一化 PUT 中的背景选择；nil 表示保留当前背景。
func normalizeBackgroundUpdate(in *BackgroundUpdate) (*normalizedBackgroundUpdate, error) {
	if in == nil {
		return nil, nil
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	switch kind {
	case BackgroundKindNone, BackgroundKindImage:
		return &normalizedBackgroundUpdate{Kind: kind}, nil
	case BackgroundKindPreset:
		preset := ""
		if in.PresetID != nil {
			preset = strings.ToLower(strings.TrimSpace(*in.PresetID))
		}
		if _, ok := presetIDs[preset]; !ok {
			return nil, invalidBackgroundKindError()
		}
		return &normalizedBackgroundUpdate{Kind: kind, Preset: preset}, nil
	default:
		return nil, invalidBackgroundKindError()
	}
}

// UploadBackground 校验/归一化上传图片、生成缩略图并原子替换背景资产。
// 先写新资产目录，再提交 DB，最后删除旧资产目录；任一步失败都不留下
// “DB 指向缺失文件”的状态。
func (s *Service) UploadBackground(ctx context.Context, content []byte) (Appearance, error) {
	normalized, err := normalizeBackgroundImage(content)
	if err != nil {
		return Appearance{}, err
	}
	assetID := assetIDFor(normalized.Master)

	s.mu.Lock()
	defer s.mu.Unlock()
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return Appearance{}, err
	}
	row = s.reconcileRowLocked(ctx, row)
	previousAssetID := row.assetID

	if err := s.writeAsset(assetID, normalized); err != nil {
		filestorage.RemoveAllQuiet(s.assetDir(assetID))
		return Appearance{}, backgroundStoreFailedError()
	}

	next := row
	next.kind = BackgroundKindImage
	next.preset = ""
	next.assetID = assetID
	next.mime = normalized.Mime
	next.width = normalized.Width
	next.height = normalized.Height
	next.bytes = len(normalized.Master)
	next.revision = row.revision + 1
	next.initialized = true
	next.updatedAt = s.now().UTC()
	if err := s.persistRowLocked(ctx, next); err != nil {
		filestorage.RemoveAllQuiet(s.assetDir(assetID))
		return Appearance{}, backgroundStoreFailedError()
	}
	if previousAssetID != "" && previousAssetID != assetID {
		filestorage.RemoveAllQuiet(s.assetDir(previousAssetID))
	}
	return s.buildAppearanceLocked(ctx, next), nil
}

// RemoveBackground 删除背景资产并恢复 kind=none，保留主题/Mica/强调色。
// 重复调用返回当前外观（幂等）。
func (s *Service) RemoveBackground(ctx context.Context) (Appearance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return Appearance{}, err
	}
	row = s.reconcileRowLocked(ctx, row)
	if row.assetID == "" && row.kind == BackgroundKindNone {
		return s.buildAppearanceLocked(ctx, row), nil
	}
	previousAssetID := row.assetID
	next := row
	next.kind = BackgroundKindNone
	next.preset = ""
	next.assetID = ""
	next.mime = ""
	next.width = 0
	next.height = 0
	next.bytes = 0
	next.revision = row.revision + 1
	next.initialized = true
	next.updatedAt = s.now().UTC()
	if err := s.persistRowLocked(ctx, next); err != nil {
		return Appearance{}, backgroundStoreFailedError()
	}
	if previousAssetID != "" {
		filestorage.RemoveAllQuiet(s.assetDir(previousAssetID))
	}
	return s.buildAppearanceLocked(ctx, next), nil
}

// BackgroundContent 返回背景原图或缩略图字节。variant 只接受 full|thumb，
// 缺省为 full；未设置背景返回 404。
func (s *Service) BackgroundContent(ctx context.Context, variant string) (BackgroundContent, error) {
	variant = strings.ToLower(strings.TrimSpace(variant))
	switch variant {
	case "":
		variant = "full"
	case "full", "thumb":
	default:
		return BackgroundContent{}, panelerr.BadRequest("bad_request", "variant must be full or thumb")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return BackgroundContent{}, err
	}
	row = s.reconcileRowLocked(ctx, row)
	if row.assetID == "" {
		return BackgroundContent{}, panelerr.NotFound("background")
	}
	path := s.thumbPath(row.assetID)
	contentType := "image/jpeg"
	if variant == "full" {
		path = s.masterPath(row.assetID, row.mime)
		contentType = row.mime
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		s.degradeAssetLocked(ctx, row)
		return BackgroundContent{}, panelerr.NotFound("background")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		s.degradeAssetLocked(ctx, row)
		return BackgroundContent{}, panelerr.NotFound("background")
	}
	return BackgroundContent{
		Content:     content,
		ContentType: contentType,
		ETag:        `"` + row.assetID + "-" + variant + `"`,
		ModTime:     info.ModTime(),
	}, nil
}

// PublicAppearance 返回登录页可用的公开外观：主题/强调色/Mica/背景元数据，
// 以及仅在 kind=image 时的模糊缩略图 base64；绝不返回原图或 assetId。
func (s *Service) PublicAppearance(ctx context.Context) (PublicAppearance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return PublicAppearance{}, err
	}
	row = s.reconcileRowLocked(ctx, row)
	thumbnail := s.thumbDataURLLocked(ctx, row)
	public := PublicAppearance{
		ThemeMode: row.themeMode,
		Mica:      row.mica,
		Accent:    row.accent,
		Background: PublicBackground{
			Kind:     row.kind,
			PresetID: presetPointer(row),
			HasImage: row.assetID != "",
		},
	}
	if row.kind == BackgroundKindImage {
		public.ThumbDataURL = thumbnail
	}
	return public, nil
}

// Reconcile 在启动时校验 DB 与资产文件一致性；文件缺失时降级为无背景并告警。
func (s *Service) Reconcile(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, found, err := s.loadRowLocked(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	_ = s.reconcileRowLocked(ctx, row)
	return nil
}

// CleanupOrphans 删除 <dataRoot>/appearance/ 下不再被当前行引用且超过保留期的
// 资产目录（启动时与定时执行）。返回清理错误只用于诊断，不影响主流程。
func (s *Service) CleanupOrphans(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.appearanceDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	row, _, err := s.loadRowLocked(ctx)
	if err != nil {
		return err
	}
	current := row.assetID
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if filestorage.WorkspaceStale(dir, now, backgroundOrphanStaleAge) {
			filestorage.RemoveAllQuiet(dir)
		}
	}
	return nil
}

// StartPeriodic 启动定时孤儿清理；ctx 取消后停止。
func (s *Service) StartPeriodic(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(orphanCleanupPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if err := s.CleanupOrphans(ctx, now.UTC()); err != nil {
					s.logger(ctx).Warn("appearance orphan cleanup failed", zap.Error(err))
				}
			}
		}
	}()
}

// loadRowLocked 读取单行设置；不存在时返回默认行与 found=false。
func (s *Service) loadRowLocked(ctx context.Context) (appearanceRow, bool, error) {
	row := defaultRow()
	var mica, initialized int
	var updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT theme_mode,mica,accent,background_kind,background_preset,background_asset_id,background_mime,background_width,background_height,background_bytes,background_revision,initialized,updated_at FROM appearance_settings WHERE id=?`, configID).
		Scan(&row.themeMode, &mica, &row.accent, &row.kind, &row.preset, &row.assetID, &row.mime, &row.width, &row.height, &row.bytes, &row.revision, &initialized, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return appearanceRow{}, false, err
	}
	row.mica = mica != 0
	row.initialized = initialized != 0
	row.updatedAt = parseTime(updatedAt)
	row = normalizeRow(row)
	return row, true, nil
}

// normalizeRow 防御性归一化旧 alpha 行/手工数据，保证对外枚举总是合法。
func normalizeRow(row appearanceRow) appearanceRow {
	if _, ok := themeModes[row.themeMode]; !ok {
		row.themeMode = ThemeModeSystem
	}
	if _, ok := accentIDs[row.accent]; !ok {
		row.accent = AccentBlue
	}
	if row.kind == BackgroundKindPreset {
		if _, ok := presetIDs[row.preset]; !ok {
			// preset 行缺少有效预设 id 时按无背景处理，避免对外发出非法组合。
			row.kind = BackgroundKindNone
			row.preset = ""
		}
	} else {
		row.preset = ""
	}
	// kind 非 none/preset/image 时回落 none；资产元数据另行在 reconcile 校验。
	if _, ok := map[string]struct{}{BackgroundKindNone: {}, BackgroundKindPreset: {}, BackgroundKindImage: {}}[row.kind]; !ok {
		row.kind = BackgroundKindNone
	}
	return row
}

// reconcileRowLocked 校验 DB 记录与资产文件的一致性：资产 id/文件缺失或不可读时
// 清除资产元数据、把 kind=image 降级为 none，并告警。
func (s *Service) reconcileRowLocked(ctx context.Context, row appearanceRow) appearanceRow {
	if row.assetID == "" {
		if row.kind == BackgroundKindImage {
			next := row
			next.kind = BackgroundKindNone
			return s.commitRepairLocked(ctx, row, next)
		}
		return row
	}
	if !s.assetAvailable(row) {
		return s.degradeAssetLocked(ctx, row)
	}
	return row
}

// degradeAssetLocked 清除不可用/缺失的资产元数据并持久化；kind=image 时降级为 none。
func (s *Service) degradeAssetLocked(ctx context.Context, row appearanceRow) appearanceRow {
	next := row
	if next.kind == BackgroundKindImage {
		next.kind = BackgroundKindNone
	}
	next.assetID = ""
	next.mime = ""
	next.width = 0
	next.height = 0
	next.bytes = 0
	next.revision = row.revision + 1
	next.updatedAt = s.now().UTC()
	s.logger(ctx).Warn("appearance background asset is missing; falling back to no background",
		zap.String("asset_id", row.assetID))
	return s.commitRepairLocked(ctx, row, next)
}

// commitRepairLocked 持久化自动修复结果；持久化失败只告警并返回修复后的内存值，
// 保证读取接口不会因为修复写入失败而整体失败。
func (s *Service) commitRepairLocked(ctx context.Context, current, next appearanceRow) appearanceRow {
	if err := s.persistRowLocked(ctx, next); err != nil {
		s.logger(ctx).Warn("appearance background asset repair failed",
			zap.String("asset_id", current.assetID), zap.Error(err))
	}
	return next
}

// assetAvailable 校验资产 id、MIME 与 master/thumb 文件都存在且为普通文件。
func (s *Service) assetAvailable(row appearanceRow) bool {
	if !validAssetID(row.assetID) {
		return false
	}
	if row.mime != "image/jpeg" && row.mime != "image/png" {
		return false
	}
	if !regularFile(s.masterPath(row.assetID, row.mime)) {
		return false
	}
	return regularFile(s.thumbPath(row.assetID))
}

// buildAppearanceLocked 由行构建完整响应；hasImage 时总是填 thumbDataUrl。
func (s *Service) buildAppearanceLocked(ctx context.Context, row appearanceRow) Appearance {
	thumbnail := s.thumbDataURLLocked(ctx, row)
	if row.assetID != "" && thumbnail == nil {
		row = s.degradeAssetLocked(ctx, row)
	}
	return Appearance{
		ThemeMode: row.themeMode,
		Mica:      row.mica,
		Accent:    row.accent,
		Background: AppearanceBackground{
			Kind:     row.kind,
			PresetID: presetPointer(row),
			AssetID:  stringPointer(row.assetID),
			Revision: row.revision,
			HasImage: row.assetID != "",
		},
		ThumbDataURL: thumbnail,
		Initialized:  row.initialized,
		UpdatedAt:    formatUpdatedAt(row.updatedAt),
	}
}

// thumbDataURLLocked 读取缩略图并编码为 data URL；无资产或读取失败返回 nil。
func (s *Service) thumbDataURLLocked(ctx context.Context, row appearanceRow) *string {
	if row.assetID == "" {
		return nil
	}
	content, err := os.ReadFile(s.thumbPath(row.assetID))
	if err != nil {
		return nil
	}
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(content)
	return &dataURL
}

// writeAsset 原子写入新资产目录：master（JPEG/PNG）+ thumb（JPEG）。
func (s *Service) writeAsset(assetID string, normalized normalizedBackground) error {
	dir := s.assetDir(assetID)
	if err := filestorage.WriteAtomic(filepath.Join(dir, masterFileName(normalized.Mime)), normalized.Master, 0o644); err != nil {
		return err
	}
	if err := filestorage.WriteAtomic(filepath.Join(dir, thumbFileJPEG), normalized.Thumb, 0o644); err != nil {
		return err
	}
	return nil
}

// persistRowLocked upsert 单行设置。
func (s *Service) persistRowLocked(ctx context.Context, row appearanceRow) error {
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO appearance_settings(id,theme_mode,mica,accent,background_kind,background_preset,background_asset_id,background_mime,background_width,background_height,background_bytes,background_revision,initialized,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET theme_mode=excluded.theme_mode,mica=excluded.mica,accent=excluded.accent,background_kind=excluded.background_kind,background_preset=excluded.background_preset,background_asset_id=excluded.background_asset_id,background_mime=excluded.background_mime,background_width=excluded.background_width,background_height=excluded.background_height,background_bytes=excluded.background_bytes,background_revision=excluded.background_revision,initialized=excluded.initialized,updated_at=excluded.updated_at`,
		configID, row.themeMode, boolInt(row.mica), row.accent, row.kind, row.preset, row.assetID, row.mime, row.width, row.height, row.bytes, row.revision, boolInt(row.initialized), formatTime(row.updatedAt))
	return err
}

func (s *Service) appearanceDir() string {
	return filepath.Join(s.dataRoot, appearanceDirName)
}

func (s *Service) assetDir(assetID string) string {
	return filepath.Join(s.appearanceDir(), assetID)
}

func (s *Service) masterPath(assetID, mimeType string) string {
	return filepath.Join(s.assetDir(assetID), masterFileName(mimeType))
}

func (s *Service) thumbPath(assetID string) string {
	return filepath.Join(s.assetDir(assetID), thumbFileJPEG)
}

func masterFileName(mimeType string) string {
	if mimeType == "image/png" {
		return masterFilePNG
	}
	return masterFileJPEG
}

func (s *Service) logger(ctx context.Context) *logging.Logger {
	return logging.From(logging.With(ctx, logging.Context{Module: "appearance"}))
}

// defaultRow 返回尚未保存时的默认外观。
func defaultRow() appearanceRow {
	return appearanceRow{
		themeMode: ThemeModeSystem,
		mica:      true,
		accent:    AccentBlue,
		kind:      BackgroundKindPreset,
		preset:    PresetMist,
	}
}

// validAssetID 校验内容哈希前缀（32 位小写十六进制），防止 DB 被篡改时路径逃逸。
func validAssetID(value string) bool {
	if len(value) != assetIDLength {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// assetIDFor 返回归一化 master 内容的 SHA-256 十六进制前缀，作为目录名/ETag/缓存键。
func assetIDFor(content []byte) string {
	return filestorage.SHA256Hex(content)[:assetIDLength]
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func presetPointer(row appearanceRow) *string {
	if row.kind != BackgroundKindPreset || row.preset == "" {
		return nil
	}
	value := row.preset
	return &value
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// error helpers -------------------------------------------------------------

// backgroundRequiredError 返回“缺少背景”的稳定错误：上传无文件，或 PUT
// kind=image 但没有可复用资产时使用。
func backgroundRequiredError() *panelerr.Error {
	return panelerr.Validation("background_required", "A background image is required")
}

// invalidBackgroundKindError 返回非法背景类型/预设 id 的稳定错误。
func invalidBackgroundKindError() *panelerr.Error {
	return panelerr.Validation("invalid_background_kind", "Background kind must be none, preset or image, with a supported preset id")
}

// backgroundStoreFailedError 返回资产落盘失败的稳定错误。
func backgroundStoreFailedError() *panelerr.Error {
	return panelerr.New(500, "background_store_failed", "Failed to store background image")
}

// helpers -------------------------------------------------------------------

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.UTC()
	}
	parsed, err = time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func formatUpdatedAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
