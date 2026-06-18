# 后端核心、配置与存储

## 适用场景

修改服务启动、依赖装配、API 路由、配置加载、数据库迁移、认证、运行时设置、统一响应或错误处理时，先读本文档。

## 关键入口

- 后端入口：`cmd/panel/main.go`
- 应用装配与路由：`internal/app/app.go`
- 配置：`internal/config/config.go`
- 进程日志：`internal/logging/`
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
- `GET /api/v1/system/version` 返回构建时注入的版本、通道（`release` 或 `dev`）、commit、仓库和缓存的最新版本状态。`internal/systeminfo` 每 6 小时只读检查 GitHub 最新 Release；只有 `release` 通道且版本为三段数字核心版本（可带 `v` 前缀和预发布后缀）时才检查更新。未注入或无效通道按 `dev` 处理，不发起检查，也不提供下载或安装能力。
- 运行时设置从数据库读取，并以配置文件、环境变量和内置默认值作为基础。登录页自定义标题和说明分别使用 `branding.loginTitle`、`branding.loginSubtitle` 键持久化；进程日志等级使用 `log.level` 键持久化，默认 `info`，更新 `/api/v1/settings/runtime` 后立即调整 zap `AtomicLevel`；旧数据库启动时由默认设置写入流程自动补齐空值。
- 后端进程日志统一使用 `internal/logging` 的 zap JSON logger，输出路径固定为 `stdout`。启动、关闭、后台服务和 HTTP 请求日志保持英文消息，不进入多语言翻译；成功和重定向 HTTP 完成日志使用 debug，4xx 使用 warn，5xx 使用 error。
- 概览仪表盘卡片布局通过 `overview_card_configurations` 保存在应用数据库；当前单管理员模型使用固定 `default` 记录，整套有序卡片配置以稳定值 JSON 原子替换。
- Docker 镜像更新缓存使用 `image_updates`、`image_refreshes`，Application 容器协调观察状态使用 `application_reconcile_states`；Docker 实时资源清单不复制到数据库。
- 后端对外错误响应需要走 `panelerr`、`httpx` 和 `internal/i18n`，不要在 handler 中散落用户可见错误文案。

## 数据库约定

- 应用数据在 `Store.AppDB()`，任务数据在 `Store.TaskDB()`，指标数据在 `Store.MetricsDB()`；不要把任务表或指标表误建到应用数据库。
- 数据库路径配置包括 `appDatabase`、`taskDatabase`、`metricsDatabase`，环境变量分别是 `PANEL_APP_DATABASE`、`PANEL_TASK_DATABASE`、`PANEL_METRICS_DATABASE`，三者必须指向不同文件。
- SQLite 连接由 `internal/storage` 统一配置为 WAL、5 秒 busy timeout 和小连接池；普通路径与 `file:` DSN 都必须保留这些默认 pragma，除非用户显式覆盖。
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

修改启动装配、配置项、运行时设置、进程日志、认证流程、API 路由、构建版本信息、数据库表/字段、错误响应结构时，必须更新本文档或模块索引。

## 密钥资产启动与存储

- `app.New` 必须在证书、应用和 scheduler 启动前初始化 `internal/secretstore`、迁移 DNS provider 凭据、初始化 `internal/keyassets` 并完成旧自签证书迁移。
- `key_assets` 保存统一密钥与证书元数据和密文私钥；`key_asset_export_artifacts` 保存短期批量导出下载信息。
- `credentials.secret_ciphertext` 使用同一 `secretstore` 保存 SSH 密码、私钥和私钥口令的加密 JSON；新凭据不得把秘密写入独立文件或旧明文字段。
- 主密钥优先读取 `PANEL_KEY_ASSETS_MASTER_KEY`，否则读取 `<dataRoot>/secrets/key-assets-master.key`；首次无资产时自动生成文件并使用 `0600` 权限。
- 数据库存在加密资产、DNS 凭据或 SSH 凭据但主密钥缺失、格式错误或环境变量与文件不一致时，Panel 必须拒绝启动，不能生成新密钥覆盖。
- 启动时必须迁移旧 SSH 明文密码、口令和私钥文件：先写入并验证密文，再删除私钥文件，最后清空旧字段；删除或验证失败时拒绝启动，迁移必须可重复执行。
- 新增路由集中在 `/api/v1/key-assets`；私钥下载响应必须禁用缓存。
- 容器化资源 API 集中在 `/api/v1/servers/{id}/containers|images|networks|volumes`，批量 Application 镜像更新位于 `/api/v1/images/upgrade-selected|upgrade-all`。
- `app.New` 还会初始化 `internal/agent` 的专用 mTLS 资产，路径为 `<dataRoot>/agent/tls`；该 CA 只用于 Panel 与目标机 agent 的双向认证，不与用户密钥资产混用。目标机 agent 默认监听 `tcp/9786`。服务启动后会后台扫描服务器，调度器也会周期检查已配置 agent：未配置 agent URL、`agent.status=incompatible` 不兼容、旧端口、版本/能力/Docker host 不匹配或证书时间问题会自动创建或复用部署任务安装/修复 agent；普通 `agent.status=unavailable`、网络错误、服务器失联或 Docker 不可用只记录状态，不触发自动重装。连续 2 次系统自动部署失败后写入 `agent.status=undeployable` 并停止自动部署。依赖 agent 的定时工作必须只在 `agent.status=compatible` 且存在 `agent.url` 时执行；agent 不正常时跳过当前工作，不创建资源操作任务，也不回退 SSH。手动重装会清除自动部署停止标记。
- Panel 主进程和 `cmd/panel-agent` 保持独立二进制；Docker 镜像固定同时携带 `/app/panel` 与 `/app/panel-agents/linux-amd64|linux-arm64/panel-agent`，agent 二进制注入的 `internal/buildinfo.Version` 跟随 Panel 构建版本，仅用于展示和排查。Agent 兼容性由健康检查返回的能力列表和自动生成的 HTTP contract 判断，不由 Panel/Agent 版本号相等性决定。自动部署只能从该固定 bundle 位置读取 agent，并按目标服务器 `sys.architecture` 选择文件；缺失时通过 SSH `uname -m` 探测。
