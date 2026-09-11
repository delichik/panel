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
- `ARCH-CFG-003`：`listenAddress`、管理员用户名/密码哈希、至少 16 字符 JWT secret、数据根目录和四个数据库路径均为必需；远程命令超时必须为正数，DNS 传播延迟不得为负。
- `ARCH-CFG-004`：app、log、coordination、metrics 四个数据库必须使用互不相同的路径；配置冲突时启动失败，不得合并数据到同一文件。
- `ARCH-CFG-005`：环境变量至少保持 `PANEL_LISTEN_ADDRESS`、`PANEL_DATA_ROOT`、`PANEL_APP_DATABASE`、`PANEL_LOG_DATABASE`、兼容的 `PANEL_TASK_DATABASE`、`PANEL_COORDINATION_DATABASE`、`PANEL_METRICS_DATABASE`、`PANEL_CERT_ACME_DIRECTORY_URL` 语义。
- `ARCH-CFG-006`：运行时设置只热更新规范明确支持的字段；需要重启或 TLS 重载的字段必须返回准确状态，不得在 UI 显示已生效而实际仍使用旧值。

## 3. HTTP 通用协议

- `ARCH-API-001`：正常 JSON 响应统一为 `{data, error:null}`，失败统一为 `{data:null, error:{code,message,details?}}`；204 响应不携带 JSON body。
- `ARCH-API-002`：业务错误使用稳定、机器可判定的 `code` 和正确 HTTP 状态；`message` 是已按服务端语言策略翻译的用户可读文本，`details` 仅包含安全的结构化上下文。
- `ARCH-API-003`：未知错误对外统一为 `internal_error`，服务端记录原始错误；不得把堆栈、SQL、绝对敏感路径或秘密写入响应。
- `ARCH-API-004`：通过通用 JSON decoder 处理的请求 body 最大 10 MiB；超限返回 `request_body_too_large`，无效 JSON 返回 `bad_request`，不得执行部分业务写入。
- `ARCH-API-005`：除登录、会话查询、公开品牌和明确的维护模式入口外，`/api/v1` 路由必须经过认证；“必须修改初始密码”状态仅允许会话、登出和账号修改相关动作。
- `ARCH-API-006`：列表接口必须对分页参数、排序字段、方向、搜索和筛选做白名单校验；空结果返回空集合和合法分页元数据，不得用 404 表示列表为空。
- `ARCH-API-007`：时间通过 RFC3339/RFC3339Nano 表达并以 UTC 持久化；前端负责按当前 locale/timezone 展示，禁止传递无时区的歧义时间。
- `ARCH-API-008`：DELETE 的成功语义在各域明确规定为 204 或带操作结果的 2xx；异步删除只表示任务已可靠创建，不得假称远端资源已删除。
- `ARCH-API-009`：当前 Panel API 路由集合由 `internal/bootstrap/panel/routes_manifest_test.go` 固定；增加、删除或改名路由时必须同时更新域验收项、前端消费者/Mock（如有）和清单计数/哈希。
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
| app | 配置、资源、应用 desired/observed、不可变协调 revision、Job、只追加 Activity 原始事实/证据、凭据元数据、设置、认证状态 | 不存高容量任务输出或指标时间序列 |
| log | 可重建 Activity 查询投影/FTS、兼容 Task/step/log、应用修订历史投影、运行事件及详情、密钥导出审计 | 不作为应用协调或原始 Activity 事实来源 |
| coordination | 当前不注册业务模型，保留路径只为兼容和备份拓扑 | 不重新引入旧 lifecycle 表作为第二事实源 |
| metrics | 指标快照时间序列 | 不存资源配置或身份数据 |

- `DATA-DB-001`：四库连接均启用 WAL、foreign keys、5 秒 busy timeout，并维持有界连接池；调用方不得依赖 SQLite 默认 pragma。
- `DATA-DB-002`：数据库目录和 `dataRoot/tmp` 以仅所有者可访问的权限创建；内存 DSN 不创建目录，`file:` DSN 必须剥离查询串后再解析路径。
- `DATA-DB-003`：历史 `tasks.db` 迁移到 `log.db` 时不得覆盖已存在的 `log.db` 或其 WAL/SHM；冲突 sidecar 必须保留并记录警告。
- `DATA-DB-004`：关闭四库时即使前一个 close 失败也要尝试关闭后续数据库，并返回首个错误。

## 6. 持久化模型与迁移

- `DATA-MIG-001`：全部在管表必须在 `internal/platform/database/models` 有模型并按 app/log/coordination/metrics 明确分组；新增表必须同时进入正确清单。
- `DATA-MIG-002`：启动迁移按库隔离执行，不得因全局注册表把模型建入错误数据库；父表排序、外键、CHECK、复合/部分/唯一索引必须可重复创建且与模型合同一致。
- `DATA-MIG-003`：迁移必须幂等。同一版本数据库连续启动不得产生 schema drift、重复数据或重复索引。
- `DATA-MIG-004`：一次性数据迁移用有稳定 ID 的 Step 记录，按事务执行；已成功的 Step 不重复执行，失败 Step 不记录为完成。
- `DATA-MIG-005`：删除列/表、收紧约束、重建表前必须有旧数据搬移/备份方案；表重建的建临时表、复制、换名、重建索引必须位于原子事务，崩溃后不得丢原表。
- `DATA-MIG-006`：ORM 只允许自动删除已接管快照中的对象；外部列、表、索引不得被自动删除。破坏性同步必须显式开启，非破坏模式只报告差异。
- `DATA-MIG-007`：可空时间无值统一为 NULL；历史空字符串在读取、更新和启动归一化时按未设置处理，不得导致升级失败。
- `DATA-MIG-008`：迁移必须保留 alpha 旧版本升级路径，包括旧数据库名、旧秘密字段、旧证书/密钥资产和已下线协调表的明确处理；不得只验证全新数据库。
- `DATA-MIG-009`：备份/恢复清单必须覆盖四库及规范声明的数据文件；新增持久化位置时必须同步更新备份合同。

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
- `ARCH-SEC-003`：导入、上传和归档解包必须限制请求大小、条目数/总展开量和目标目录；失败时清理临时内容。
- `ARCH-SEC-004`：删除远端资源前必须验证 Panel 托管身份与作用域；同名非托管资源不得自动删除或覆盖。
- `ARCH-SEC-005`：认证、导出、证书/密钥变更、远端执行和恢复等敏感动作必须留下不含秘密的审计或运行事件。
