# 后端核心、配置与存储

## Process supervisor

- Container runtime starts `cmd/panel-init/main.go`; local development may still run `cmd/panel/main.go` directly.
- `panel_init` starts a random local `127.0.0.1:0` restart listener, generates a random restart token, starts the child Panel process with `--init-restart-url`, `--init-restart-token`, and `--maintenance-mode`, and exits when the child exits without a restart request.
- Backup/restore restart requests POST the next mode to the local `panel_init` listener, asking it to restart Panel with `--maintenance-mode backup_export`, `restore`, or `normal`.
- Normal Panel startup also creates `<dataRoot>/run/panel-control.sock` on Linux with owner-only permissions. `panel setup` uses this local socket to ask the already-running process to perform the fixed Panel-host bootstrap workflow; the CLI must not open the databases or duplicate business-service assembly.
- `cmd/panel/main.go` must not enter backup/restore maintenance mode from pending files alone; the maintenance mode argument is the required gate.

## Agent Report Stream

- Panel starts an internal `agentReportCollector` worker from `internal/bootstrap/panel/agent_report_collector.go`. It does not expose a Panel-side report API or listen address.
- The collector actively dials each compatible agent and opens `AgentReportService.Report`. Panel sends the current metrics and container report intervals on the stream; the agent responds on that same Panel-initiated stream with metrics and container snapshots.
- The collector inspects compatible agents every second. It creates missing streams, recreates streams when the agent endpoint changes, and cancels streams with no messages for `max(10s, min(metricsInterval, containerInterval) * 3)`. Stream health is recorded in `agent.report.status`, `agent.report.last_message_at`, and `agent.report.last_error`; it does not change the normal `agent.status`.
- Agent-side report streams are watchers over one shared collector hub. The hub starts only while at least one watcher exists, stops when the last watcher leaves, and broadcasts one collected periodic result to all watchers that are due at the same Unix-aligned `sampleAt`.
- The agent also watches Docker container events while report watchers exist. Container create/start/stop/die/destroy and related events trigger an immediate full container snapshot with reason `container_change`; periodic Unix-aligned full snapshots remain the baseline.
- Runtime settings now include `containerReportIntervalSeconds` in addition to `metricsCollectionIntervalSeconds`. Both values are persisted in `runtime_settings`, exposed through `/api/v1/settings/runtime`, and may be changed while agents are connected.
- App database schema includes `container_observations`, which stores the latest full container snapshot per server. Metrics snapshots continue to use the metrics database.

## 适用场景

修改服务启动、依赖装配、API 路由、配置加载、数据库迁移、认证、运行时设置、统一响应或错误处理时，先读本文档。

## 关键入口

- 后端入口：`cmd/panel/main.go`
- 应用装配与路由：`internal/bootstrap/panel/app.go`
- 配置：`internal/platform/config/config.go`
- 进程日志：`internal/platform/logging/`
- 数据库连接和迁移：`internal/platform/database/store.go`、`internal/platform/database/migrations.go`
- 认证：`internal/modules/identity/`
- 运行时设置：`internal/modules/settings/`
- 统一 HTTP 响应与路由注册契约：`internal/platform/http/`
- 统一错误：`internal/platform/errors/`
- 后端错误翻译：`internal/platform/i18n/`
- 构建版本信息：`internal/platform/buildinfo/`
- 系统版本与更新检查：`internal/modules/systeminfo/`

## 结构约定

- `bootstrap/panel.New` 负责打开数据库、创建 service、连接跨模块依赖、集中注册业务任务、启动 tasks 内部 worker 和各模块自有后台 worker，并调用各模块路由注册器。业务任务定义必须在所有相关业务 service 和 bridge 创建完成后，通过集中注册阶段调用各模块 `RegisterTasks`，不要穿插在 service 构造过程中零散注册。证书、密钥资产、应用和容器之间的双向协作通过 `internal/bootstrap/panel/bridges.go` 中的窄接口 bridge 注入，禁止在生产装配中用 service setter 回连形成环。
- Panel 与 Panel Agent 启动装配都会校验构建时生成的 Agent gRPC contract hash；该 hash 基于 `agent.proto` 的 protobuf descriptor，变量为空时必须立即返回明确启动错误，不能以空 hash 继续运行。
- bootstrap bridge 在依赖尚未装配时必须返回明确错误，不能 nil pointer panic；相关防护由 `internal/bootstrap/panel/bridges_test.go` 覆盖。
- `bootstrap/panel.New` 创建任务服务后会立即校验数据库中的 `running` 任务；当前进程内没有对应 execution 对象的任务会在其他后台服务启动前标记为失败。应用服务装配完成后还会收敛这些失败目标任务对应的 lifecycle target，避免重启后应用运行时状态永久停在部署中。
- API 统一挂在 `/api/v1/`。`/api/v1/auth/login`、`/api/v1/auth/session` 和只返回登录页标题/说明的 `GET /api/v1/settings/public-branding` 是开放入口，其余 API 经认证中间件保护。业务 API 路由由各模块的 `RegisterRoutes(*http.ServeMux, httpx.Middleware)` 注册，bootstrap 不再维护业务路径 switch。
- 根路径由后端静态托管 `web/dist`；没有构建前端时返回纯文本后端运行提示。
- `GET /api/v1/system/version` 返回构建时注入的版本、通道（`release` 或 `dev`）、commit、仓库和缓存的最新版本状态。`internal/modules/systeminfo` 每 6 小时只读检查 GitHub 最新 Release；只有 `release` 通道且版本为三段数字核心版本（可带 `v` 前缀和预发布后缀）时才检查更新。未注入或无效通道按 `dev` 处理，不发起检查，也不提供下载或安装能力。
- `GET /api/v1/debug/snapshot` 是仅认证用户可访问的只读诊断接口，由 `internal/modules/observability/diagnostics` 提供同一快照时间点的进程、Go runtime 内存/GC、tasks worker 运行状态、注册任务定义能力和 app/log/metrics 三库统计。数据库信息包含连接池、文件/SQLite 页面大小，以及用户表的准确行数、表数据页大小、索引大小、总占用和数据库占比；表级空间通过 SQLite `dbstat` 读取，不可用时只降级空间统计。接口不返回数据库路径、schema SQL、配置值、任务参数或任何业务记录与秘密。
- 全量备份与还原由 `internal/modules/backups` 提供。正常运行期的备份导出只写 pending export 并通过 `panel_init` 本地监听请求进入维护模式；真正归档在 `--maintenance-mode backup_export` 的下一次子进程启动早期由 `ExportApp` 执行。普通 pending 本身仍不能触发维护逻辑，必须收到匹配的 maintenance mode；但已创建的 restore transaction state 表示覆盖事务可能未收敛，任何冷启动都必须优先进入 restore recovery，state 检查权限/I/O 错误时必须拒绝 normal 启动。维护模式不装配正常业务模块或后台 worker。export 与 restore 使用互相隔离、也不接受普通 JWT 的进程内维护 session。restore pending 固化当前管理员 bcrypt 验证材料，使维护登录不依赖可能损坏的 `app.db`；旧 pending 只能回退到显式非默认配置凭据，安装默认 `admin/admin` 必须拒绝。恢复介质在 apply 前移动到 DataRoot 同级事务目录，DataRoot 和外置数据库通过持久 stage/swap/rollback 状态切换；未完成 rollback 时禁止 clear 和 normal restart。备份导出完成后提供下载并要求重启回 normal；恢复完整提交后才清理事务并请求 normal restart。
- 运行时设置从数据库读取，并以配置文件、环境变量和内置默认值作为基础。登录页自定义标题和说明分别使用 `branding.loginTitle`、`branding.loginSubtitle` 键持久化；进程日志等级使用 `log.level` 键持久化，默认 `info`，更新 `/api/v1/settings/runtime` 后立即调整 zap `AtomicLevel`；旧数据库启动时由默认设置写入流程自动补齐空值。
- 后端进程日志统一使用 `internal/platform/logging` 的 zap JSON logger，输出路径固定为 `stdout`。启动、关闭、后台服务和 HTTP 请求日志保持英文消息，不进入多语言翻译；成功和重定向 HTTP 完成日志使用 debug，4xx 使用 warn，5xx 使用 error。
- 概览仪表盘卡片布局通过 `overview_card_configurations` 保存在应用数据库；当前单管理员模型使用固定 `default` 记录，整套有序卡片配置以稳定值 JSON 原子替换。
- Docker 镜像更新缓存使用 `image_updates`、`image_refreshes`，Application 容器协调观察状态使用 `application_reconcile_states`；fail2ban 的 Panel 草稿 YAML 与接管开关使用 `fail2ban_configs` 按服务器保存；Docker 实时资源清单不复制到数据库。
- 后端对外错误响应需要走 `platform/errors`、`platform/http` 和 `platform/i18n`，不要在 handler 中散落用户可见错误文案。
- `internal/platform` 禁止依赖 `internal/modules`；业务模块之间禁止直接导入其他模块的 `store` 实现。`internal/architecture/dependencies_test.go` 固化这些依赖边界。
- API method/path 清单由 `internal/bootstrap/panel/routes_manifest_test.go` 固化；有意调整 API 时必须同步确认前后端契约后更新清单，目录重构不得顺便改变清单。设施类型没有通用 list 路由；反向代理设施完整详情使用 `GET /api/v1/facility-apps/reverse-proxy`。
- SSH 解密后的凭据传输模型定义在 `internal/platform/ssh`，服务器凭据模块通过类型别名实现该平台端口，避免 platform 反向依赖业务模块。

## 数据库约定

- 应用业务数据在 `Store.AppDB()`，任务、任务日志、应用 revision 记录、密钥资产导出记录和应用 lifecycle 历史在 `Store.LogDB()`，指标数据在 `Store.MetricsDB()`；不要把 log 表或指标表误建到应用数据库。`application_revisions` 与 `key_asset_exports` 从 AppDB 移出后不迁移旧数据，也不读取旧 AppDB 表兼容。
- 数据库路径配置包括 `appDatabase`、`logDatabase`、`metricsDatabase`，环境变量分别是 `PANEL_APP_DATABASE`、`PANEL_LOG_DATABASE`、`PANEL_METRICS_DATABASE`，三者必须指向不同文件。旧 `taskDatabase` 配置、`PANEL_TASK_DATABASE` 环境变量和默认 `data/db/tasks.db` 文件仅作为升级兼容入口，启动时会迁移到 `data/db/log.db`。
- SQLite 连接由 `internal/platform/database` 统一配置为 WAL、5 秒 busy timeout 和小连接池；普通路径与 `file:` DSN 都必须保留这些默认 pragma，除非用户显式覆盖。
- 当前处于 alpha 但已有使用者，修改表结构必须考虑旧版本迁移。
- 入口网关设施配置表 `facility_app_configs` 使用 `domains_json` 保存域名、源站、AnyAccess 和嵌套 Path。升级旧库时迁移会转换旧设施字段与应用 `reverse_proxy_json`，先检查设施、应用和 Panel 入口的域名所有权冲突，再重建设施表删除旧镜像、静态站点和域名策略列；迁移完成后业务代码不保留旧 JSON 字段兼容。
- `panel_installation` 是固定 `default` 记录的单例安装状态，使用服务器外键保存待初始化节点和唯一 Panel 宿主节点。宿主节点不能通过普通服务器删除流程移除；Panel 入口启用时必须绑定该节点。
- 新字段或新表优先使用可重复执行的增量迁移，并在 `internal/platform/database/store_test.go` 或相关 service 测试覆盖旧库升级路径。
- 数据库迁移兼容基线不再包含短期内部结构：`applications.persistent_path`、AppDB 中的 `application_lifecycle_operations`/`application_lifecycle_targets`、以及不支持 `application_files.kind='archive'` 的旧 `application_files` 约束；处理这些更早内部快照时应先用带兼容迁移的版本升级。
- 会被展示的持久化配置只保存稳定 key、kind、value，不保存当前语言下的展示文案。

## API 变更检查

- 路由由对应模块 handler 的 `RegisterRoutes` 注册；新增后端 API 时同步更新对应模块路由注册、前端 `web/src/api/` 与 `web/src/types/api.ts`。
- 认证、强制改密、运行时设置会影响前端路由守卫，相关变更要补读 [frontend.md](frontend.md)。
- 新增用户可见错误时补读 [../i18n-guide.md](../i18n-guide.md)。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端相关改动时，优先运行 `task test:backend`。
- 影响编译、路由装配、配置或迁移且需要构建检查时，运行 `task build:backend`。
- 如果同时改前端 API 调用，再按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

修改启动装配、配置项、运行时设置、进程日志、认证流程、API 路由、构建版本信息、数据库表/字段、错误响应结构、维护状态或恢复模式启动行为时，必须更新本文档或模块索引。

## 运行事件装配

- 统一运行事件模块位于 `internal/modules/runtimeevents/`，生产装配在 `internal/bootstrap/panel/app.go` 中创建服务、注册 `/api/v1/application-operations` 与 `/api/v1/system-events` 路由，并启动独立清理 worker。
- `runtime_events`、`runtime_event_details` 和 `application_operation_records` 属于高增长运行历史，必须保存在 `Store.LogDB()`；`runtimeEventRetentionDays`、`runtimeEventDetailRetentionDays`、`runtimeEventCleanupSchedule` 作为 runtime settings 保存在 `Store.AppDB()`。
- 记录保留时间必须大于或等于详情保留时间。清理 worker 先清详情并标记详情不可用，再删除过期摘要和应用操作投影。

## 密钥资产启动与存储

- `bootstrap/panel.New` 必须在证书、应用和 tasks 内部 worker 启动前初始化 `internal/platform/secrets`、迁移 DNS provider 凭据、初始化 `internal/modules/keyassets` 并完成旧自签证书迁移。
- `key_assets` 保存统一密钥与证书元数据和密文私钥；`key_asset_exports` 位于 `Store.LogDB()`，保存短期批量导出下载信息，沿用 30 分钟 `expires_at` 语义并由密钥资产服务清理过期记录和归档文件。旧 AppDB 中的导出记录不迁移、不读取兼容。
- `credentials.secret_ciphertext` 使用同一 `secretstore` 保存 SSH 密码、私钥和私钥口令的加密 JSON；新凭据不得把秘密写入独立文件或旧明文字段。
- 主密钥优先读取 `PANEL_KEY_ASSETS_MASTER_KEY`，否则读取 `<dataRoot>/secrets/key-assets-master.key`；首次无资产时自动生成文件并使用 `0600` 权限。
- 数据库存在加密资产、DNS 凭据或 SSH 凭据但主密钥缺失、格式错误或环境变量与文件不一致时，Panel 必须拒绝启动，不能生成新密钥覆盖。
- 启动时必须迁移旧 SSH 明文密码、口令和私钥文件：先写入并验证密文，再删除私钥文件，最后清空旧字段；删除或验证失败时拒绝启动，迁移必须可重复执行。
- 新增路由集中在 `/api/v1/key-assets`；私钥下载响应必须禁用缓存。
- 密钥资产文件下载与导出归档下载的公开 URL 保持不变；由于两种三段路径在 Go `ServeMux` 中会交叉匹配，统一由 `/api/v1/key-assets/{downloadPath...}` 分发，避免启动注册路由时 panic。
- 容器化资源实现位于 `internal/modules/containers`；API 集中在 `/api/v1/servers/{id}/containers|images|networks|volumes`，批量 Application 镜像更新位于 `/api/v1/images/upgrade-selected|upgrade-all`。
- `bootstrap/panel.New` 还会通过 `internal/modules/keyassets` 初始化 Panel Agent 专用 mTLS 资产，统一保存在 `key_assets` 表，并用 `metadata_json.systemManaged=true`、`systemScope=agent_tls` 标记为系统托管；旧 `<dataRoot>/agent/tls` 文件只作为升级时的一次性导入来源，不再作为真源。Agent CA 默认 30 年有效，Panel client 与目标机 server 证书默认 30 天有效，目标机证书剩余不足 7 天时触发重部署续期；CA 资产缺失时会重建整套 Agent TLS 资产，client 资产缺失时只补发 client 证书。该 CA 只用于 Panel 与目标机 agent 的双向认证，不与用户自建证书语义混用。Panel 侧调用目标机 agent 使用 `internal/agent/client`，协议与能力声明在 `internal/agent/contract`。
- Agent 健康响应中的证书信息由 `internal/agent/contract` 自己定义，gRPC client 在 TLS 边界填充；contract 不依赖 `internal/agent/security`。
- Panel 主进程和 `cmd/panel-agent` 保持独立二进制；`internal/bootstrap/agent` 负责 Agent 配置、mTLS 和 gRPC Server 装配，Agent gRPC service 位于 `internal/agent/rpc`，本机 Docker/运行时能力位于 `internal/agent/docker`，系统采集与维护能力位于 `internal/agent/system`。系统 traits 和 metrics 使用原生 Go 读取 Linux `/proc`、`/sys`、网络接口及文件系统统计；APT/UFW 使用固定可执行文件和参数，重启调用 logind D-Bus。Docker 镜像固定同时携带 `/app/panel` 与 `/app/panel-agents/linux-amd64|linux-arm64/panel-agent`，agent 二进制注入的 `internal/platform/buildinfo.Version` 必须与 Panel 构建版本完全一致，健康检查发现不一致时自动重装；能力列表、gRPC contract hash 和 Docker host 不作为兼容性门槛。自动部署只能从该固定 bundle 位置读取 agent，并按服务器结构化 `architecture.os`/`architecture.arch` 选择文件；结构化架构缺失时先探测目标节点并持久化结果；agent endpoint 必须是当前服务器 host 的默认 `9786` 地址。
