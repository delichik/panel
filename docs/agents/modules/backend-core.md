# 后端核心、配置与存储

## Process supervisor

- Container runtime starts `cmd/panel-init/main.go`; local development may still run `cmd/panel/main.go` directly.
- `panel_init` starts a random local `127.0.0.1:0` restart listener, generates a random restart token, starts the child Panel process with `--init-restart-url`, `--init-restart-token`, and `--maintenance-mode`, and exits when the child exits without a restart request.
- Backup/restore restart requests POST the next mode to the local `panel_init` listener, asking it to restart Panel with `--maintenance-mode backup_export`, `restore`, or `normal`.
- Normal and maintenance Panel startup listen on the configured standalone HTTPS address and serve a process-global cached pair from `dataRoot/tls/panel.crt` plus `dataRoot/tls/panel.key`. Runtime settings and key assets synchronize this fixed pair; activation, persistence, and failure rollback run in one process-local critical section using an in-memory fixed-pair snapshot. A successful update clears the cache, and the next new TLS connection reloads it. When the pair is absent or incomplete, Panel creates and serves a local self-signed certificate from the data root.
- `cmd/panel/main.go` must not enter backup/restore maintenance mode from pending files alone; the maintenance mode argument is the required gate.

## Agent Report Stream

- Panel starts an internal `agentReportCollector` worker from `internal/bootstrap/panel/agent_report_collector.go`. It does not expose a Panel-side report API or listen address.
- The collector actively dials each compatible agent and opens `AgentReportService.Report`. Panel sends the current metrics and container report intervals on the stream; the agent responds on that same Panel-initiated stream with metrics and container snapshots.
- The collector inspects compatible agents every second. It creates missing streams, recreates streams when the agent endpoint changes, and cancels streams with no messages for `max(10s, min(metricsInterval, containerInterval) * 3)`. Stream health is recorded in `agent.report.status`, `agent.report.last_message_at`, and `agent.report.last_error`; it does not change the normal `agent.status`.
- Agent-side report streams are watchers over one shared collector hub. The hub starts only while at least one watcher exists, stops when the last watcher leaves, and broadcasts one collected periodic result to all watchers that are due at the same Unix-aligned `sampleAt`. When a metrics or container collection fails the corresponding report field stays nil and the broadcast is skipped, so the Panel never overwrites stored snapshots with empty objects.
- The agent also watches Docker container events while report watchers exist. Container create/start/stop/die/destroy and related events trigger an immediate container snapshot with reason `container_change`; periodic Unix-aligned snapshots remain the baseline. Container snapshots include managed-file drift labels computed from a local stat-based fingerprint cache (`state/managed-files.fingerprint.json`), so unchanged files are not re-hashed on every snapshot.
- Runtime settings now include `containerReportIntervalSeconds` in addition to `metricsCollectionIntervalSeconds`. Both values are persisted in `runtime_settings`, exposed through `/api/v1/settings/runtime`, and may be changed while agents are connected (metrics default 60s, containers default 30s).
- App database schema includes `container_observations`, which stores the latest full container snapshot per server. Metrics snapshots continue to use the metrics database.

## 适用场景

修改服务启动、依赖装配、API 路由、配置加载、数据库迁移、认证、运行时设置、统一响应或错误处理时，先读本文档。

## 关键入口

- 后端入口：`cmd/panel/main.go`
- 应用装配与路由：`internal/bootstrap/panel/app.go`
- 配置：`internal/platform/config/config.go`
- 进程日志：`internal/platform/logging/`
- 数据库连接和迁移：`internal/platform/database/store.go`、`internal/platform/database/migrations.go`、`internal/platform/database/orm/`、`internal/platform/database/models/`
- 应用部署控制面：`internal/orchestrator/`（Planner、Job lease、ObservationWriter、RuntimeReconcile worker）
- 认证：`internal/modules/identity/`
- 运行时设置：`internal/modules/settings/`
- 统一 HTTP 响应与路由注册契约：`internal/platform/http/`
- 统一错误：`internal/platform/errors/`
- 后端错误翻译：`internal/platform/i18n/`
- 构建版本信息：`internal/platform/buildinfo/`
- 系统版本与更新检查：`internal/modules/systeminfo/`

## 结构约定

- `bootstrap/panel.New` 负责打开数据库、创建 service、连接跨模块依赖、集中注册业务任务、启动应用部署控制面、tasks 内部 worker 和各模块自有后台 worker，并调用各模块路由注册器。业务任务定义必须在所有相关业务 service 和 bridge 创建完成后，通过集中注册阶段调用各模块 `RegisterTasks`，不要穿插在 service 构造过程中零散注册。证书、密钥资产、应用和容器之间的双向协作通过 `internal/bootstrap/panel/bridges.go` 中的窄接口 bridge 注入，禁止在生产装配中用 service setter 回连形成环。
- Panel 与 Panel Agent 启动装配都会校验构建时生成的 Agent gRPC contract hash；该 hash 基于 `agent.proto` 的 protobuf descriptor，变量为空时必须立即返回明确启动错误，不能以空 hash 继续运行。
- Agent gRPC 契约包含 `PrepareRestart` 流式重启就绪检查（详见 servers.md）：Panel 在部署/重启 agent 前调用，避免打断 agent 发起的软件包升级；旧 agent 缺失该能力时 Panel 直接继续部署。
- bootstrap bridge 在依赖尚未装配时必须返回明确错误，不能 nil pointer panic；相关防护由 `internal/bootstrap/panel/bridges_test.go` 覆盖。
- `bootstrap/panel.New` 创建任务服务后会立即校验数据库中的 `running` 任务；当前进程内没有对应 execution 对象的任务会在其他后台服务启动前标记为失败。应用服务装配完成后启动 AppDB Job controller，由它恢复过期 lease、扫描 pending/failed_retryable Job，并通过 execution ID 与 lease token 继续收敛应用运行时；旧 lifecycle target 层已全部下线，不再读取。
- API 统一挂在 `/api/v1/`。`/api/v1/auth/login`、`/api/v1/auth/session` 和只返回登录页标题/说明的 `GET /api/v1/settings/public-branding` 是开放入口，其余 API 经认证中间件保护。业务 API 路由由各模块的 `RegisterRoutes(*http.ServeMux, httpx.Middleware)` 注册，bootstrap 不再维护业务路径 switch。
- 根路径由后端静态托管 `web/dist`；没有构建前端时返回纯文本后端运行提示。
- `GET /api/v1/system/version` 返回构建时注入的版本、通道（`release` 或 `dev`）、commit、仓库和缓存的最新版本状态。`internal/modules/systeminfo` 每 6 小时只读检查 GitHub 最新 Release；只有 `release` 通道且版本为三段数字核心版本（可带 `v` 前缀和预发布后缀）时才检查更新。未注入或无效通道按 `dev` 处理，不发起检查，也不提供下载或安装能力。
- `GET /api/v1/debug/snapshot` 是仅认证用户可访问的只读诊断接口，由 `internal/modules/observability/diagnostics` 提供同一快照时间点的进程、Go runtime 内存/GC、tasks worker 运行状态、注册任务定义能力和 app/log/metrics 三库统计。数据库信息包含连接池、文件/SQLite 页面大小，以及用户表的准确行数、表数据页大小、索引大小、总占用和数据库占比；表级空间通过 SQLite `dbstat` 读取，不可用时只降级空间统计。接口不返回数据库路径、schema SQL、配置值、任务参数或任何业务记录与秘密。
- Debug 页面还提供 pprof 开关：`GET /api/v1/debug/pprof` 返回当前启用状态与监听地址，`PUT /api/v1/debug/pprof`（请求体 `{"enabled": true|false}`）启停 pprof 服务。pprof 固定只监听 `127.0.0.1:6060`，不经过认证中间件、也不暴露在 Panel 对外端口；进程退出或关闭时自动停止。端口被占用时返回 `pprof_start_failed`，停服失败返回 `pprof_stop_failed`。
- 全量备份与还原由 `internal/modules/backups` 提供。正常运行期的备份导出只写 pending export 并通过 `panel_init` 本地监听请求进入维护模式；真正归档在 `--maintenance-mode backup_export` 的下一次子进程启动早期由 `ExportApp` 执行。普通 pending 本身仍不能触发维护逻辑，必须收到匹配的 maintenance mode；但已创建的 restore transaction state 表示覆盖事务可能未收敛，任何冷启动都必须优先进入 restore recovery，state 检查权限/I/O 错误时必须拒绝 normal 启动。维护模式不装配正常业务模块或后台 worker。export 与 restore 使用互相隔离、也不接受普通 JWT 的进程内维护 session。restore pending 固化当前管理员 bcrypt 验证材料，使维护登录不依赖可能损坏的 `app.db`；旧 pending 只能回退到显式非默认配置凭据，安装默认 `admin/admin` 必须拒绝。恢复介质在 apply 前移动到 DataRoot 同级事务目录，DataRoot 和外置数据库通过持久 stage/swap/rollback 状态切换；未完成 rollback 时禁止 clear 和 normal restart。备份导出完成后提供下载并要求重启回 normal；恢复完整提交后才清理事务并请求 normal restart。
- 运行时设置从数据库读取，并以配置文件、环境变量和内置默认值作为基础。登录页自定义标题和说明分别使用 `branding.loginTitle`、`branding.loginSubtitle` 键持久化；进程日志等级使用 `log.level` 键持久化，默认 `info`，更新 `/api/v1/settings/runtime` 后立即调整 zap `AtomicLevel`；协调追踪使用 `reconcile.trace` 键持久化（默认关闭，更新后立即同步 `internal/platform/reconciletrace` 开关），开启后协调/部署全链路输出结构化追踪日志，事件与字段见 containerization.md 的协调追踪小节；旧数据库启动时由默认设置写入流程自动补齐空值。
- 后端进程日志统一使用 `internal/platform/logging` 的 zap JSON logger，输出路径固定为 `stdout`。启动、关闭、后台服务和 HTTP 请求日志保持英文消息，不进入多语言翻译；成功和重定向 HTTP 完成日志使用 debug，4xx 使用 warn，5xx 使用 error。
- 概览仪表盘卡片布局通过 `overview_card_configurations` 保存在应用数据库；当前单管理员模型使用固定 `default` 记录，整套有序卡片配置以稳定值 JSON 原子替换。
- Docker 镜像更新缓存使用 `image_updates`、`image_refreshes`，Application 的 planner/Job 运行状态使用 `jobs`，实例 desired/observed 使用 `application_instances`，兼容性退避状态使用 `application_reconcile_states`；fail2ban 的 Panel 草稿 YAML 与接管开关使用 `fail2ban_configs` 按服务器保存；Docker 实时资源清单不复制到数据库。
- 后端对外错误响应需要走 `platform/errors`、`platform/http` 和 `platform/i18n`，不要在 handler 中散落用户可见错误文案。
- `internal/platform` 禁止依赖 `internal/modules`；业务模块之间禁止直接导入其他模块的 `store` 实现。`internal/architecture/dependencies_test.go` 固化这些依赖边界。
- API method/path 清单由 `internal/bootstrap/panel/routes_manifest_test.go` 固化；有意调整 API 时必须同步确认前后端契约后更新清单，目录重构不得顺便改变清单。设施类型没有通用 list 路由；反向代理设施完整详情使用 `GET /api/v1/facility-apps/reverse-proxy`。
- 存储共享设施 API 位于 `/api/v1/facility-apps/storage-share`（GET/PUT 配置、POST `/reconcile`、DELETE 卸载并返回卸载后配置、GET `/status` 汇总导出/挂载生效状态）与 `/partitions/{id}`（GET `/download` 打包下载、DELETE 删除记录+数据）；配置表 `storage_share_configs`（多存储服务器及各自根目录存于 `servers_json`，迁移回填旧 `server_ids_json`/`server_id`）、分区记录表 `storage_share_partitions`（含 `target`/`volume_name`）位于 AppDB。存储服务器的导出配置、打包下载、目录删除与生效状态检查通过 Agent RPC（`StorageConfigureExport`/`StorageArchiveDirectory`/`StorageDeleteDirectory`/`StorageStatus`/`StorageMountStatus`）执行，不使用 Panel 侧 SSH。
- SSH 解密后的凭据传输模型定义在 `internal/platform/ssh`，服务器凭据模块通过类型别名实现该平台端口，避免 platform 反向依赖业务模块。
- `internal/platform/ssh.SSHExecutor` 默认开启主机密钥 TOFU：首次连接把目标机公钥按 `host:port` 身份写入 known_hosts 格式文件，后续连接必须匹配，否则拒绝连接并返回 `ssh_host_key_mismatch`（BadGateway）。known_hosts 默认位于 `<dataRoot>/known_hosts`（`PANEL_DATA_ROOT` 未设置时回退 `data`），可用 `WithKnownHosts` 指定路径；存储文件读写失败时按失败关闭连接，返回 `ssh_host_key_verification_failed`。服务器记录的 `host_key_mismatch` 列由类型化错误码（`ssh_host_key_mismatch`）在写入时判定，读取端不再依赖错误消息文本子串（子串仅作为迁移前旧行回退）。

## 数据库约定

- 应用业务数据、不可变 `application_revisions`、应用 `application_instances` 和部署 `jobs` 在 `Store.AppDB()`；任务中心的 task、任务步骤/日志、旧 revision 兼容副本和密钥资产导出记录在 `Store.LogDB()`；指标数据在 `Store.MetricsDB()`。协调库 `Store.CoordDB()` 不再注册任何模型，旧 `application_lifecycle_operations` / `application_lifecycle_targets` / `application_target_stages` 三张表已随迁移 DROP 删除。新的部署事实来源不得写入 LogDB task；启动迁移会把旧 LogDB revision 行复制到 AppDB，旧 LogDB 表仍可供兼容诊断读取。
- 数据库路径配置包括 `appDatabase`、`logDatabase`、`metricsDatabase`，环境变量分别是 `PANEL_APP_DATABASE`、`PANEL_LOG_DATABASE`、`PANEL_METRICS_DATABASE`，三者必须指向不同文件。旧 `taskDatabase` 配置、`PANEL_TASK_DATABASE` 环境变量和默认 `data/db/tasks.db` 文件仅作为升级兼容入口，启动时会迁移到 `data/db/log.db`。
- SQLite 连接由 `internal/platform/database` 统一配置为 WAL、5 秒 busy timeout 和小连接池；普通路径与 `file:` DSN 都必须保留这些默认 pragma，除非用户显式覆盖。
- 当前处于 alpha 但已有使用者，修改表结构必须考虑旧版本迁移。
- `Store.Migrate` 已由 ORM 驱动：对 app/log/metrics 三库分别调用 `orm.AutoMigrateModels(WithDestructive(true))`（DDL 由 `internal/platform/database/models` 的 42 个模型负责，CHECK 约束经 `TableConstraints()` 声明），随后按 `models.ExtraIndexDDL()` 幂等创建复合/部分/复合 UNIQUE 索引，并用 `orm.RunSteps` 执行一次性数据迁移；历史遗留表（旧 tasks/certificates）由对应 Step/直连迁移清理，不依赖自动删除。证书 scope 约束重建因需事务外切换 `PRAGMA foreign_keys`，由 Migrate 直接调用而非 Step。
- 入口网关设施配置表只保存部署节点、DNS 同步状态、错误和更新时间；所有路由统一保存在 `reverse_proxy_routes` 表。升级旧库时启动预迁移会忽略历史 Panel 入口字段，并继续迁移旧域名路由。
- 运行时设置保存 Panel 域名和可选 TLS 密钥资产 ID。设置和证书资产负责把当前证书同步到 `<dataRoot>/tls/panel.crt` 与 `<dataRoot>/tls/panel.key`，并清空进程级 TLS 缓存；文件激活、配置持久化及失败回滚在同一进程内临界区执行，回滚直接恢复固定文件快照而不再读取数据库。下一条新 TLS 连接才加锁加载这对固定文件，缓存命中不做文件 I/O。固定文件缺失或不完整时使用内置自签名证书，空 ID 会恢复默认自签名证书。
- 新字段或新表优先使用可重复执行的增量迁移，并在 `internal/platform/database/store_test.go` 或相关 service 测试覆盖旧库升级路径。
- 数据库迁移兼容基线不再包含短期内部结构：`applications.persistent_path`、CoordDB 的 `application_lifecycle_operations`/`application_lifecycle_targets`（已随迁移删除）、以及不支持 `application_files.kind='archive'` 的旧 `application_files` 约束；处理这些更早内部快照时应先用带兼容迁移的版本升级。AppDB 的 `application_revisions`、`jobs` 和实例 desired/observed 字段必须通过可重复迁移补齐。
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
- `runtime_events`、`runtime_event_details` 属于高增长运行历史，保存在 `Store.LogDB()`；`application_operation_records` 已随旧 lifecycle 层删除，不再创建。`runtimeEventRetentionDays`、`runtimeEventDetailRetentionDays`、`runtimeEventCleanupSchedule` 作为 runtime settings 保存在 `Store.AppDB()`。
- 记录保留时间必须大于或等于详情保留时间。清理 worker 按 `runtimeEventRetentionDays` 删除过期 `runtime_events`；`runtime_event_details` 不再读写，原应用操作阶段/投影清理已随旧 lifecycle 层下线。

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
- Panel 主进程和 `cmd/panel-agent` 保持独立二进制；`internal/bootstrap/agent` 负责 Agent 配置、mTLS 和 gRPC Server 装配，Agent gRPC service 位于 `internal/agent/rpc`，本机 Docker/运行时能力位于 `internal/agent/docker`，系统采集与维护能力位于 `internal/agent/system`。agent 服务端在 CA 客户端校验之外通过 `VerifyPeerCertificate` 拒绝携带 `ServerAuth` EKU 的客户端证书，保证节点证书不能作为客户端横向调用其他 agent（Panel client 证书只含 `ClientAuth`）。系统 traits 和 metrics 使用原生 Go 读取 Linux `/proc`、`/sys`、网络接口及文件系统统计；APT/UFW 使用固定可执行文件和参数，重启调用 logind D-Bus。Docker 镜像固定同时携带 `/app/panel` 与 `/app/panel-agents/linux-amd64|linux-arm64/panel-agent`，agent 二进制注入的 `internal/platform/buildinfo.Version` 必须与 Panel 构建版本完全一致，健康检查发现不一致时自动重装；能力列表、gRPC contract hash 和 Docker host 不作为兼容性门槛。自动部署只能从该固定 bundle 位置读取 agent，并按服务器结构化 `architecture.os`/`architecture.arch` 选择文件；结构化架构缺失时先探测目标节点并持久化结果；agent endpoint 必须是当前服务器 host 的默认 `9786` 地址。

## 安全与健壮性基线

- 登录限流：`internal/modules/identity` 为 Login 增加内存级限流，按“客户端 IP + 用户名”计数连续失败；连续 5 次失败后锁定 15 分钟（锁定期间返回 429 `login_rate_limited`），成功登录清零计数，锁定到期后重置。Handler 通过 `LoginFrom` 传入 `RemoteAddr` 的 IP；`Login` 服务层入口同样限流（IP 为空），便于复用与测试。
- JWT 默认密钥首启随机化：`internal/modules/settings` 在 `ensureDefaultRuntimeSettings` 时，若配置中的 JWT secret 为空或仍为公开默认常量 `change-me-panel-jwt-secret`，则生成随机 32 字节 secret 并持久化；配置中显式提供的 secret 视为有意配置，原样保留。
- SSH known_hosts：`internal/bootstrap/panel/app.go` 显式传入 `sshx.WithKnownHosts(<dataRoot>/known_hosts)`，不再依赖 `PANEL_DATA_ROOT` 环境变量推导默认路径；首次连接主机时按 TOFU 记录主机密钥。
- HTTP 请求体上限：`internal/platform/http/httpx.go` 的 `Decode` 使用 `http.MaxBytesReader` 限制 10 MiB，超限返回 400 `request_body_too_large`；multipart 上传接口不经 `Decode`，不受影响。
- HTTP Server 超时：`cmd/panel/main.go` 增加 `IdleTimeout: 120s` 与 `MaxHeaderBytes: 1MiB`；不设置 `WriteTimeout`，避免打断任务日志、诊断流与文件下载等长响应。
- 分页上限：`internal/platform/http/list.go` 的 `ParseListPage` 将 `page` 限制在 10000 以内，避免 `(page-1)*pageSize` 溢出。
- 启动失败清理：`internal/bootstrap/panel/app.go` 统一 `stopBackgroundServices` 清理路径；应用 orchestrator、`StartControlServer` 失败时都会取消 `CheckConfiguredAgents` goroutine、等待其退出后再关闭 store；正常 `Close()` 也先停止 Job controller，再停止 tasks 和其他后台 worker，最后等待该 goroutine 退出。
- 配置加载：`internal/platform/config` 对默认 admin 密码哈希使用包级 `sync.Once` 缓存，避免每次 `Default()/Load()` 重复执行 bcrypt。
