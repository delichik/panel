# 架构、数据与 API 验收规范

本文约束进程装配、模块边界、HTTP 协议、SQLite 四库、轻量 ORM、迁移和生命周期。具体业务行为由相应领域文档补充。

## 1. 系统边界与启动

- `ARCH-BOOT-001`：Panel 启动必须先校验生成的 Agent gRPC contract hash，再打开数据库；校验失败时不得继续监听或启动后台任务。
- `ARCH-BOOT-002`：四库打开和迁移、秘密存储、遗留秘密/证书迁移、Panel TLS 同步及核心服务装配任一步失败，启动必须返回失败，并关闭已打开资源。
- `ARCH-BOOT-003`：后台服务只能在依赖服务构造完成、任务定义全部注册后启动；关闭必须停止协调器、任务/清理/事件/指标/报告/诊断工作并关闭数据库，重复关闭不得 panic。
- `ARCH-BOOT-004`：启动时遗留的“运行中但无有效执行”的任务必须被标记失败或恢复为规范定义的可处理状态，不得永远保持伪运行中。
- `ARCH-BOOT-005`：Agent 健康检查允许异步启动，但应用关闭必须取消并等待该检查结束，避免访问已关闭依赖。
- `ARCH-BOOT-006`：应用协调器启动失败时，已启动的服务必须按正常关闭路径清理，进程不得带着部分后台工作继续运行。
- `ARCH-BOOT-007`：生产 HTTPS 最低 TLS 版本为 1.2；监听证书从当前运行时域名和选中证书解析，证书不可用时不得静默降级为明文 HTTP。

## 2. 配置合同

- `ARCH-CFG-001`：配置优先级必须是内置默认值 < JSON 配置文件 < 环境变量；相对路径以配置文件目录为基准，无配置文件时以当前工作目录为基准。
- `ARCH-CFG-002`：JSON 配置必须拒绝未知字段，避免拼写错误被静默忽略；历史 `taskDatabase` 仅作为 `logDatabase` 未设置时的兼容输入。
- `ARCH-CFG-003`：`listenAddress`、至少 16 字符 JWT secret、数据根目录和四个数据库路径均为必需；管理员用户名可省略（默认 `admin`），管理员密码/哈希可选：仅在库中没有任何用户时用于初始化，密码缺失时进入初始化界面；远程命令超时必须为正数，DNS 传播延迟不得为负。
- `ARCH-CFG-004`：app、log、coordination、metrics 四个数据库必须使用互不相同的路径；配置冲突时启动失败，不得合并数据到同一文件。
- `ARCH-CFG-005`：环境变量至少保持 `PANEL_LISTEN_ADDRESS`、`PANEL_ADMIN_USERNAME`、`PANEL_ADMIN_DISPLAY_NAME`、`PANEL_ADMIN_PASSWORD`、`PANEL_ADMIN_PASSWORD_HASH`、`PANEL_DATA_ROOT`、`PANEL_APP_DATABASE`、`PANEL_LOG_DATABASE`、兼容的 `PANEL_TASK_DATABASE`、`PANEL_COORDINATION_DATABASE`、`PANEL_METRICS_DATABASE`、`PANEL_CERT_ACME_DIRECTORY_URL` 语义；管理员变量只在无用户时生效。
- `ARCH-CFG-006`：运行时设置只热更新规范明确支持的字段；需要重启或 TLS 重载的字段必须返回准确状态，不得在 UI 显示已生效而实际仍使用旧值。

## 3. HTTP 通用协议

- `ARCH-API-001`：正常 JSON 响应统一为 `{data, error:null}`，失败统一为 `{data:null, error:{code,message,details?}}`；204 响应不携带 JSON body。
- `ARCH-API-002`：业务错误使用稳定、机器可判定的 `code` 和正确 HTTP 状态；`message` 是已按服务端语言策略翻译的用户可读文本，`details` 仅包含安全的结构化上下文。
- `ARCH-API-003`：未知错误对外统一为 `internal_error`，服务端记录原始错误；不得把堆栈、SQL、绝对敏感路径或秘密写入响应。
- `ARCH-API-004`：通过通用 JSON decoder 处理的请求 body 最大 10 MiB；超限返回 `request_body_too_large`，无效 JSON 返回 `bad_request`，不得执行部分业务写入。
- `ARCH-API-005`：除登录、会话查询、公开品牌、公开外观（`GET /api/v1/settings/public-appearance`）和明确的维护模式入口外，`/api/v1` 路由必须经过认证；“必须修改初始密码”状态仅允许会话、登出和账号修改相关动作。
- `ARCH-API-006`：列表接口必须对分页参数、排序字段、方向、搜索和筛选做白名单校验；空结果返回空集合和合法分页元数据，不得用 404 表示列表为空。
- `ARCH-API-007`：时间通过 RFC3339/RFC3339Nano 表达并以 UTC 持久化；前端负责按当前 locale/timezone 展示，禁止传递无时区的歧义时间。
- `ARCH-API-008`：DELETE 的成功语义在各域明确规定为 204 或带操作结果的 2xx；异步删除只表示任务已可靠创建，不得假称远端资源已删除。
- `ARCH-API-009`：当前 Panel API 路由集合由 `internal/bootstrap/panel/routes_manifest_test.go` 固定；增加、删除或改名路由时必须同时更新域验收项、前端消费者/Mock（如有）和清单计数/哈希。外观设置新增 6 条后主 API 基线由 186 变为 192；设置写入拆分后 192 → 197；下线 3 条互联路由后 197 → 194；入口代理与自动更新新增 4 条后 194 → 198；P1 三条后 198 → 201；镜像仓库源 201 → 202；通知长轮询 202 → 203；多用户/权限/初始化新增 18 条（2 条 setup、1 条权限目录、7 条用户、5 条角色、3 条访问凭证）后基线为 **221**（当前值）。公开外观走 `RegisterPublicRoutes` 但仍以 `/api/` 前缀计入主清单，不改变 2 条非 `/api` 公开产物路由。
- `ARCH-API-010`：前端静态托管对 HTML 使用 `no-cache`，指纹化 `assets/` 使用一年 immutable cache，其他静态资源使用日级缓存；未知前端路径回退 `index.html`，未构建前端时返回明确的纯文本运行状态。

## 4. 模块责任与调用边界

- `ARCH-MOD-001`：HTTP handler 只负责认证后的输入解析、调用领域服务和协议映射；持久化、远端命令和跨资源规则不得只存在于前端或 handler。
- `ARCH-MOD-002`：远端 SSH/Docker/Agent 调用必须在有 deadline 的上下文中执行；不得持有 SQLite 写事务等待网络。
- `ARCH-MOD-003`：Task 记录用户操作和通用后台执行；应用 desired/observed 和协调 Job 才是部署事实来源，不得从相近时间的 Task 推测应用状态。
- `ARCH-MOD-004`：秘密明文只可进入秘密存储或执行期内存；关系数据库、任务参数、任务日志、运行事件、错误详情和应用修订不得存储可恢复的秘密明文。
- `ARCH-MOD-005`：跨模块触发必须通过已装配的窄接口/registry/bridge；删除提供方能力时必须同步处理全部注册消费者，禁止形成可选依赖的 nil panic。
- `ARCH-MOD-006`：Agent 与 Panel 的兼容判定、RPC 转换和资源所有权检查必须在服务端/Agent 双侧保留；UI 状态不能作为安全授权。

## 5. 四库归属

| 数据库 | 规范归属 | 禁止事项 |
| --- | --- | --- |
| app | 配置、资源、应用 desired/observed、不可变协调 revision、Job、遗留 `activity_events` schema（不再接受新产品事实）、凭据元数据、设置、认证状态、进程行 `events`、event executor `event_runs`/`event_run_steps`、网络探测配置与最新观测（`network_probe_configs`/`network_probe_results`）、指标设施配置（`metrics_facility_configs`）、全局外观设置（`appearance_settings`，仅背景资产元数据） | 不存高容量 JSONL 正文或指标时间序列；JSONL 在 `<dataRoot>/logs`；外观背景图片字节在 `<dataRoot>/appearance/`，不得把图片或 base64 原图存入本库 |
| log | 遗留可重建 Activity 查询投影/FTS（不再投影新事实）、应用修订历史投影、运行事件及详情、密钥导出审计 | 不作为应用协调、event executor 或新产品/过程事实来源 |
| coordination | 当前不注册业务模型，保留路径只为兼容和备份拓扑 | 不重新引入旧 lifecycle 表作为第二事实源 |
| metrics | 指标快照时间序列（含可空磁盘 IO 速率列 `disk_read_bytes_rate`/`disk_write_bytes_rate`，NULL=未测得） | 不存资源配置或身份数据 |

- `DATA-DB-001`：四库连接均启用 WAL、foreign keys、5 秒 busy timeout，并维持有界连接池；调用方不得依赖 SQLite 默认 pragma。
- `DATA-DB-002`：数据库目录和 `dataRoot/tmp` 以仅所有者可访问的权限创建；内存 DSN 不创建目录，`file:` DSN 必须剥离查询串后再解析路径。
- `DATA-DB-003`：历史 `tasks.db` 迁移到 `log.db` 时不得覆盖已存在的 `log.db` 或其 WAL/SHM；冲突 sidecar 必须保留并记录警告。
- `DATA-DB-004`：关闭四库时即使前一个 close 失败也要尝试关闭后续数据库，并返回首个错误。

## 6. 持久化模型与迁移

应用持久化位置由 app 库 `application_persistent_locations` 表追加记录，以 `(application_id, server_id)` 唯一；部署目标变化不得删除历史位置，应用物理删除时通过 application FK 级联清理。alpha 升级迁移从当前持久化应用的部署目标、实例与历史 Job 回填位置。

服务器连接代理模式（`SRV-PROXY-*`）沿用 app 库 `servers` 表：由 ORM 自动增列 `proxy_mode`（`TEXT NOT NULL DEFAULT 'direct'`）、`tunnel_server_id`、`socks5_address`、`socks5_username`、`socks5_password_ciphertext`（均 `TEXT NOT NULL DEFAULT ''`）。`tunnel_server_id` 不加外键，允许悬空以表达“隧道服务器已删除”，依赖服务器读取时派生 `proxyInvalid`/`proxyError` 而不自动改写配置。SOCKS5 密码只经 secretstore 加密保存，associated data 固定为 `assetID=serverID`、`assetType="server_proxy"`、`field="socks5_password"`，满足 `ARCH-MOD-004` 的“秘密明文只可进入秘密存储或执行期内存”；解密失败必须返回 `server_proxy_password_unavailable`，不得降级为明文或跳过加密。旧 alpha 数据只需增列默认值，既有行读取等价 `direct`，不需要一次性数据迁移，也不得改写既有服务器字段（`DATA-MIG-008`）。

已废弃的网络互联能力不再占用 `servers` 表结构：旧版本新增的三个意图列由破坏性 ORM 同步删除（模型清单驱动，无需手写 DDL，`DATA-ORM-006`）；启动时的一次性数据迁移（稳定 Step，事务内执行，`DATA-MIG-004`）删除 `runtime_settings` 中已废弃的互联设置键，并从 `servers.traits` 中剥离已废弃的互联观测键与 `agent.certificate.hosts` 键，保留 `agent.*`、`sys.*` 等其它系统 trait；既有服务器业务字段与其它 traits 不得被删除或改写，旧库升级后读取等价“未启用/无观测”，也不得让已下线的旧地址继续参与连接或界面展示（`DATA-MIG-005/008`）。

网络探测设施新增两张 app 库表（`internal/platform/database/models/app_models.go`，注册于 `AppModels()`）：`network_probe_configs` 单行配置（`id` 固定 `network_probe`、`mode` 带 `CHECK(mode IN ('all','selected'))`、`server_ids_json`、`interval_seconds`/`ping_count`/`ping_timeout_seconds`（均 `INTEGER NOT NULL DEFAULT 60/3/2`，2026-11-17 由 AutoMigrate 增列，旧 alpha 行自动获得默认值，`FAC-PRB-010`）、`thresholds_json`（JSON，默认 `'{}'`，延迟/丢包警告阈值，2026-11-20 增列）、`version`、`updated_at`）与 `network_probe_results` 最新观测（主键 `(source_server_id,target_server_id)`，另有 `target_address`、`sampled_at`、`latency_ms`、`packet_loss_percent`、`sent`、`received`、`error`、`history_json`（最近 24 小时 30 分钟桶历史，默认 `'[]'`，`FAC-PRB-009`）与目标查询索引）。metrics 库 `metrics_snapshots` 由 ORM 自动增列可空 `disk_read_bytes_rate`/`disk_write_bytes_rate`（NULL=该样本未测得磁盘 IO，旧库升级后既有行为 NULL），不新增指标表、不改磁盘用量既有列语义。旧 alpha 数据升级只需 AutoMigrate 建表/增列；从未保存过配置时读取返回默认 `all` 空选择与默认 60/3/2（不落行），首次保存才 upsert 单行；读取侧对 `interval_seconds`/`ping_count`/`ping_timeout_seconds` 的 0、负数与越界值防御性回落到默认 60/3/2，保证下发给 Agent 的参数总是合法（`FAC-PRB-003/010`）；其中 `interval_seconds` 的 Panel 保存范围自 2026-11-17 起为 60..3600 秒（即 1..60 分钟，前端按分钟输入并换算保存），旧版本 10..59 秒行在读取侧归一回默认 60 秒。服务器删除时在同一清理事务内删除相关 `network_probe_results` 行并修剪 `selected` 配置的 `server_ids_json`（版本 +1，`FAC-PRB-007`、`SRV-DEL-007`）。备份/恢复继续覆盖 app 与 metrics 数据库文件，不需要新增 manifest 条目（`BKP-API-005`、`DATA-MIG-009`）。

指标设施新增一张 app 库表 `metrics_facility_configs`（`internal/platform/database/models/app_models.go`，注册于 `AppModels()`）：单行配置（`id` 固定 `metrics`、`mode` 带 `CHECK(mode IN ('all','selected'))`、`server_ids_json`（JSON 数组，默认 `'[]'`）、`retention_days`（`INTEGER NOT NULL`，默认 7）、`collection_interval_seconds`（`INTEGER NOT NULL`，默认 60）、`cleanup_schedule` 带 `CHECK(cleanup_schedule IN ('hourly','daily','weekly'))`（默认 `daily`）、`thresholds_json`（JSON，默认 `'{}'`，六项指标警告阈值，2026-11-20 增列）、`version`、`updated_at`）。表由既有 destructive AutoMigrate 创建，旧 alpha 库升级即为空表；`EnsureMigrated` 在无行时从 `runtime_settings` 旧键 `metricsRetentionDays`/`metricsCollectionIntervalSeconds`/`cleanupSchedule` 播种（合法值采用、非法/缺失用默认，只播种一次，旧键保留不删除，`FAC-MET-006`）。指标样本仍只存 metrics 库 `metrics_snapshots`，本表不复制历史；服务器删除在同一清理事务内修剪 `selected` 的 `server_ids_json`（`version`+1）并沿用 `OBS-MET-006` 删除指标行（`FAC-MET-007`、`SRV-DEL-007`）。运行时设置只在旧键保留作为播种来源，`GET /api/v1/settings/runtime` 与分区写入端点（`PUT /api/v1/settings/runtime/*`，`SET-RUN-022`）不再读写三项（`SET-RUN-021`）。

外观设置新增一张 app 库表 `appearance_settings`（`internal/platform/database/models/app_models.go`，注册于 `AppModels()`）：单行全局配置（`id` 固定 `appearance`，带 `CHECK(id = 'appearance')`；`theme_mode` 默认 `system`、`mica` 默认 1、`accent` 默认 `blue`、`background_kind` 默认 `none`、`background_preset`、`background_asset_id`、`background_mime`、`background_width`、`background_height`、`background_bytes`、`background_revision`、`initialized` 默认 0、`updated_at`）。图片字节**不入库**：归一化 master 与 160×90 缩略图位于 `<dataRoot>/appearance/<assetId>/`（`master.jpg|master.png`、`thumb.jpg`，目录 0700/文件 0644，`assetId` 为内容 SHA-256 前缀），DB 只保存 MIME/尺寸/字节数/revision 等元数据；写入顺序固定为“写新资产目录 → 提交 DB → 删除旧资产目录”，启动与每 6 小时清理 24 小时以上未被引用的孤儿目录，DB 记录指向缺失文件时降级为无背景并告警（`SET-APP-008/010`）。表由既有 destructive AutoMigrate 创建，旧 alpha 库升级即为空表，首次启动幂等 seed 默认行（`initialized=false`），无需一次性数据迁移（`DATA-MIG-008/009`）；只有 DELETE 背景才删除资产目录，PUT 省略背景或切到 `none`/`preset` 均保留资产供重新启用（`SET-APP-002/009`）。备份/恢复继续覆盖整个 `dataRoot` 与四库文件，外观目录自动纳入、恢复为整目录替换，无需新增 manifest 条目（`BKP-API-005`）。

- `DATA-MIG-001`：全部在管表必须在 `internal/platform/database/models` 有模型并按 app/log/coordination/metrics 明确分组；新增表必须同时进入正确清单。
- `DATA-MIG-002`：启动迁移按库隔离执行，不得因全局注册表把模型建入错误数据库；父表排序、外键、CHECK、复合/部分/唯一索引必须可重复创建且与模型合同一致。
- `DATA-MIG-003`：迁移必须幂等。同一版本数据库连续启动不得产生 schema drift、重复数据或重复索引。
- `DATA-MIG-004`：一次性数据迁移用有稳定 ID 的 Step 记录，按事务执行；已成功的 Step 不重复执行，失败 Step 不记录为完成。
- `DATA-MIG-005`：删除列/表、收紧约束、重建表前必须有旧数据搬移/备份方案；表重建的建临时表、复制、换名、重建索引必须位于原子事务，崩溃后不得丢原表。
- `DATA-MIG-006`：ORM 只允许自动删除已接管快照中的对象；外部列、表、索引不得被自动删除。破坏性同步必须显式开启，非破坏模式只报告差异。
- `DATA-MIG-007`：可空时间无值统一为 NULL；历史空字符串在读取、更新和启动归一化时按未设置处理，不得导致升级失败。
- `DATA-MIG-008`：迁移必须保留 alpha 旧版本升级路径，包括旧数据库名、旧秘密字段、旧证书/密钥资产和已下线协调表的明确处理；不得只验证全新数据库。
- `DATA-MIG-009`：备份/恢复清单必须覆盖四库及规范声明的数据文件；新增持久化位置时必须同步更新备份合同。`dataRoot` 内的持久化数据（含 `<dataRoot>/appearance/` 外观背景资产目录与 `<dataRoot>/logs`）随整目录归档自动纳入，存储于 dataRoot 之外的文件必须显式声明。

多用户与权限模型占用 app 库：`users`（内置管理员 `builtin=1`、`generation` 用户代数、`must_change_password`）、`roles`（唯一内置 `administrator`，`key` 部分唯一索引）、`role_permissions`、`user_roles`、`access_credentials`（`token_hash`、`scope`、`allow_management`、`user_generation`、`revoked_at`、`last_used_at`）。旧 `auth_accounts` 由 pre-migration Step 在破坏性 AutoMigrate 前复制为唯一内置管理员并删除旧表；旧 `token_nonce` 不再使用，`auth_state.jwt_key_generation` 保存全局密钥代数。ORM 表数 49 → **53**。`users` 用户名大小写不敏感唯一索引、`access_credentials.token_hash` 唯一索引与 `(user_id, expires_at)` 查询索引由 `ExtraIndexDDL` 创建。备份/恢复仍整库覆盖，无 manifest 变更。

- `ARCH-SEC-006`：登录 JWT 必须携带密钥代数与用户代数，任一与持久值不一致即拒绝；递增对应代数即可吊销该范围全部 JWT。访问凭证只保存哈希、明文只返回一次、最长 30 分钟、不可续期，并随用户代数递增失效。
- `ARCH-SEC-007`：权限门禁必须在后端统一执行（未映射路由 fail-closed），前端隐藏/禁用只是体验层；敏感 handler（私钥下载、他人凭证撤销）必须二次校验权限或归属。

## 7. ORM 行为合同

- `DATA-ORM-001`：模型表名/列名/tag 解析稳定且并发安全；非法 tag、列冲突、无效自增或外键声明必须在注册/迁移前失败。
- `DATA-ORM-002`：查询 builder 的用户值一律使用 `?` 参数，不得拼接；标识符和排序表达式只接受调用方已验证的常量。
- `DATA-ORM-003`：空 IN 产生恒假条件，空 NOT IN 产生恒真条件；LIKE 搜索转义反斜线、`%` 和 `_`。
- `DATA-ORM-004`：`First` 无数据返回 `sql.ErrNoRows`，`One` 要求恰好一行；`UpdateColumns` 与 `Delete` 没有 WHERE 时必须拒绝。
- `DATA-ORM-005`：Insert 回填自增主键并处理创建/更新时间；Update 需要非空主键；批量插入分块且错误不得被吞掉。
- `DATA-ORM-006`：JSON、时间、bool、二进制、指针和 `sql.Null*` 的读写必须往返一致；NULL 读入非指针标量时保持零值且不得污染相邻字段。
- `DATA-ORM-007`：AutoMigrate 首次接管已有表时只记录快照，不执行删除；后续差异输出 Added/Dropped/Rebuilt/Pending/SkippedDestructive 的可审计报告。
- `DATA-ORM-008`：切换 `PRAGMA foreign_keys` 的迁移必须固定在同一专用连接上，并在成功或失败后恢复原值。

## 8. 并发、幂等与恢复

- `ARCH-CON-001`：所有可能重复到达的外部触发应有幂等键、唯一约束、条件更新或可安全重放的 ensure 语义；重复点击不得产生不受控并行远端写入。
- `ARCH-CON-002`：读-改-写资源必须验证版本、generation、lease token 或事务内当前状态；旧请求完成不得覆盖新期望。
- `ARCH-CON-003`：进程崩溃后，正确性必须可从数据库和远端观测恢复；内存 channel/queue 仅可降低延迟，不能是唯一工作来源。
- `ARCH-CON-004`：清理 worker 依据运行时保留策略运行并容忍重复执行；清理不得删除仍被活跃工作引用的数据。
- `ARCH-CON-005`：有界后台 writer 在关闭时应尽力刷新已接受记录；若允许丢弃，必须仅限诊断性事件且不得影响业务事实或任务终态。

## 9. 安全基线

- `ARCH-SEC-001`：密码使用自适应哈希保存；JWT secret、私钥和 provider token 不出现在列表/详情响应中。
- `ARCH-SEC-002`：文件名、归档路径、下载路径和远端资源 ID 必须拒绝目录穿越、绝对路径逃逸、NUL 和跨租户/跨资源引用。
- `ARCH-SEC-003`：导入、上传和归档解包必须限制请求大小、条目数/总展开量和目标目录；失败时清理临时内容。外观背景上传必须显式限制 8 MiB+1 MiB 请求体、只接受按魔数识别的 JPEG/PNG（拒绝 GIF/WebP）、先用 `DecodeConfig` 拒绝 >4096×4096 或 >12MP 的图片再解码、忽略用户文件名并由服务端内容哈希生成资产目录/缓存键；原图仅认证接口可读，公开接口只暴露模糊缩略图。
- `ARCH-SEC-004`：删除远端资源前必须验证 Panel 托管身份与作用域；同名非托管资源不得自动删除或覆盖。
- `ARCH-SEC-005`：认证、导出、证书/密钥变更、远端执行和恢复等敏感动作必须留下不含秘密的审计或运行事件。
