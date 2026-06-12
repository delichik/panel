# 后端核心、配置与存储

## 适用场景

修改服务启动、依赖装配、API 路由、配置加载、数据库迁移、认证、运行时设置、统一响应或错误处理时，先读本文档。

## 关键入口

- 后端入口：`cmd/panel/main.go`
- 应用装配与路由：`internal/app/app.go`
- 配置：`internal/config/config.go`
- 存储和迁移：`internal/storage/store.go`、`internal/storage/migrations.go`
- 认证：`internal/auth/`
- 运行时设置：`internal/settings/`
- 统一 HTTP 响应：`internal/httpx/`
- 统一错误：`internal/panelerr/`
- 后端错误翻译：`internal/i18n/`
- 构建版本信息：`internal/buildinfo/`
- 系统版本与更新检查：`internal/systeminfo/`

## 结构约定

- `app.New` 负责打开数据库、创建 service、装配 scheduler、连接跨模块依赖并注册路由。
- `app.New` 创建任务服务后会立即校验数据库中的 `running` 任务；当前进程内没有对应 execution 对象的任务会在其他后台服务启动前标记为失败。
- API 统一挂在 `/api/v1/`。`/api/v1/auth/login`、`/api/v1/auth/session` 和只返回登录页标题/说明的 `GET /api/v1/settings/public-branding` 是开放入口，其余 API 经认证中间件保护。
- 根路径由后端静态托管 `web/dist`；没有构建前端时返回纯文本后端运行提示。
- `GET /api/v1/system/version` 返回构建时注入的版本、commit、仓库和缓存的最新版本状态。`internal/systeminfo` 每 6 小时只读检查 GitHub 最新 Release；开发版本或未注入仓库时不发起检查，且不提供下载或安装能力。
- 运行时设置从数据库读取，并以配置文件、环境变量和内置默认值作为基础。登录页自定义标题和说明分别使用 `branding.loginTitle`、`branding.loginSubtitle` 键持久化；旧数据库启动时由默认设置写入流程自动补齐空值。
- 后端对外错误响应需要走 `panelerr`、`httpx` 和 `internal/i18n`，不要在 handler 中散落用户可见错误文案。

## 数据库约定

- 应用数据在 `Store.AppDB()`，指标数据在 `Store.MetricsDB()`，不要把指标表误建到应用数据库。
- 当前处于 alpha 但已有使用者，修改表结构必须考虑旧版本迁移。
- 新字段优先使用可重复执行的增量迁移，并在 `internal/storage/store_test.go` 或相关 service 测试覆盖旧库升级路径。
- 会被展示的持久化配置只保存稳定 key、kind、value，不保存当前语言下的展示文案。

## API 变更检查

- 路由在 `internal/app/app.go` 中集中维护；新增后端 API 时同步更新前端 `web/src/api/` 与 `web/src/types/api.ts`。
- 认证、强制改密、运行时设置会影响前端路由守卫，相关变更要补读 [frontend.md](frontend.md)。
- 新增用户可见错误时补读 [../i18n-guide.md](../i18n-guide.md)。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端相关改动时，优先运行 `task test:backend`。
- 影响编译、路由装配、配置或迁移且需要构建检查时，运行 `task build:backend`。
- 如果同时改前端 API 调用，再按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

修改启动装配、配置项、运行时设置、认证流程、API 路由、构建版本信息、数据库表/字段、错误响应结构时，必须更新本文档或模块索引。

## 密钥资产启动与存储

- `app.New` 必须在证书、应用和 scheduler 启动前初始化 `internal/secretstore`、迁移 DNS provider 凭据、初始化 `internal/keyassets` 并完成旧自签证书迁移。
- `key_assets` 保存统一密钥与证书元数据和密文私钥；`key_asset_export_artifacts` 保存短期批量导出下载信息。
- 主密钥优先读取 `PANEL_KEY_ASSETS_MASTER_KEY`，否则读取 `<dataRoot>/secrets/key-assets-master.key`；首次无资产时自动生成文件并使用 `0600` 权限。
- 数据库存在加密资产但主密钥缺失、格式错误或环境变量与文件不一致时，Panel 必须拒绝启动，不能生成新密钥覆盖。
- 新增路由集中在 `/api/v1/key-assets`；私钥下载响应必须禁用缓存。
