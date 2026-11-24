# 身份、运行时设置与系统信息验收规范

本文是平台身份、会话、运行时设置、服务器变量和版本检查的验收合同。条目中的“认证用户”均指持有当前有效普通运行态 Bearer token 的管理员；维护态身份不属于本文范围。

## 1. 管理员账户与首次登录

- `ID-ACC-001`：前置为全新应用数据库；启动身份服务时，必须创建且只创建固定管理员账户，采用配置中的用户名与 bcrypt 哈希；配置缺省时使用初始 `admin/admin` 验证材料，并把 `passwordChangeRequired` 置为 true。
- `ID-ACC-002`：前置为管理员账户已存在；重复启动身份服务时，必须保留已修改的用户名、密码哈希和强制改密状态，不得以配置默认值覆盖。
- `ID-ACC-003`：前置为用户名与密码均正确；调用 `POST /api/v1/auth/login` 时，必须返回 200、`authenticated=true`、当前用户名、签名 token 和真实的 `passwordChangeRequired`，响应不得包含密码或哈希。
- `ID-ACC-004`：前置为用户名或密码错误；登录必须返回统一认证失败，不得分别暴露“用户不存在”和“密码错误”，也不得签发 token。
- `ID-ACC-005`：前置为同一“客户端 IP + 用户名”连续登录失败；第 5 次失败建立 15 分钟锁定，锁定期间后续请求必须返回 429 `login_rate_limited`，不得执行密码比对后绕过锁定。
- `ID-ACC-006`：前置为锁定到期或同一键成功登录；下一次计数必须从零开始；不同 IP 或不同用户名的计数互不影响，限流表属于进程内临时状态且不得无限增长。
- `ID-ACC-007`：前置为客户端地址含端口；登录限流键必须使用解析后的 IP；无法解析时才使用经裁剪的原始地址，代理头不得在无可信代理配置时覆盖来源。
- `ID-ACC-008`：前置为初始密码尚未修改；访问普通受保护 API 必须返回 403 `password_change_required`；仅会话查询、登出和账户修改可在该状态继续，不能只依靠前端路由守卫限制。
- `ID-ACC-009`：前置为强制改密状态；提交账户修改但不提供新密码时，必须返回 `admin_password_change_required`，用户名和强制改密状态不得发生部分更新。
- `ID-ACC-010`：前置为账户修改请求；当前密码错误返回认证失败，空白用户名返回 `admin_username_required`，新密码少于 8 字符返回 `admin_password_too_short`，新密码与旧密码相同返回 `admin_password_unchanged`；任一失败不得改变账户或会话密钥。
- `ID-ACC-011`：前置为当前密码正确且输入有效；`POST /api/v1/auth/account` 必须原子保存裁剪后的用户名；提供新密码时在同一 AppDB 事务内保存新的 bcrypt 哈希、清除强制改密、随机更换 JWT secret、轮换 token nonce，事务成功后再更新内存 JWT 快照并返回新的有效会话；任一写失败必须整笔回滚，不得出现“密码已改但旧会话仍有效”。
- `ID-ACC-012`：前置为仅修改用户名且不在强制改密状态；更新必须保留原密码和 JWT secret，返回能以新用户名表示当前账户的有效会话。

## 2. Token、会话与权限边界

- `ID-SES-001`：前置为普通运行态请求；受保护路由只接受精确 `Authorization: Bearer <token>`，缺失、格式错误、签名错误、subject 错误、nonce 缺失、过期或已撤销 token 均返回 401，业务 handler 不得执行。
- `ID-SES-002`：前置为无 token 或无效 token；`GET /api/v1/auth/session` 必须仍返回 200 与 `authenticated=false`，不得把探测会话状态转换为 401。
- `ID-SES-003`：前置为有效 token；会话查询必须返回当前数据库中的管理员用户名和强制改密状态，而不是只信任 token 内旧用户名。
- `ID-SES-004`：前置为有效 token；`POST /api/v1/auth/logout` 必须轮换持久化 nonce，使此前签发的全部普通运行态 token 立即失效，并返回 `authenticated=false`；重复登出需认证且不得恢复旧 token。
- `ID-SES-005`：前置为 token expiration 设置为 `10m`、`1h`、`1d`、`5d` 或 `30d`；新 token 的过期时间必须按对应时长签发并在到期后拒绝；设置为 `never` 时 token 可无 `exp`，但仍受 nonce 和 JWT secret 撤销。
- `ID-SES-006`：前置为具备 `settings.security` 权限的会话用户（拒绝访问凭证）；`POST /api/v1/auth/jwt-secret` 必须在服务端通过 `crypto/rand` 随机生成新的 32 字节密钥（不接受客户端自定义密钥，防止非内置管理员借已知密钥伪造任意会话），在同一 AppDB 事务内持久化新密钥并递增全局密钥代数，提交成功后再更新内存快照并失效认证缓存，返回 `authenticated=false` 与 `mustRelogin=true`；旧 token 必须立即失效，响应不得回显密钥。
- `ID-SES-007`：**已废弃（2026-11-24）**。因 `POST /api/v1/auth/jwt-secret` 不再接受客户端传入密钥（见 `ID-SES-006`），前端不再提供密钥输入框，不再因客户端输入长度触发 `invalid_jwt_secret`。
- `ID-SES-008`：普通运行态 token、备份导出维护 token、恢复维护 token 三种上下文必须互不通用；任何跨上下文使用均不得获得业务或维护权限。

## 3. 运行时设置读取与写入

- `SET-RUN-001`：前置为认证用户；`GET /api/v1/settings/runtime` 必须返回监听地址、数据库路径、数据根、容器上报间隔、会话期限、语言、日志级别、远程超时、协调追踪、品牌、证书、Panel TLS 选择状态和 Agent 投递分组，但绝不返回 JWT secret，仅返回 `jwtSecretConfigured`。指标保留天数、指标采集间隔与清理计划不属于运行时设置，改由指标设施管理（`FAC-MET-001/006`、`SET-RUN-021`）。写入按设置分区子资源进行，见 `SET-RUN-022`。
- `SET-RUN-002`：前置为未认证用户；仅 `GET /api/v1/settings/public-branding` 与 `GET /api/v1/settings/public-appearance` 可公开，且品牌接口只返回登录标题和副标题、外观接口只返回主题/Mica/强调色/背景元数据和 `kind='image'` 时的模糊缩略图（`SET-APP-007`）；不得泄露路径、邮箱、证书 ID、保留策略、背景原图或资产 id 或其他运行时配置。
- `SET-RUN-003`：前置为旧库缺少设置行；启动必须用配置文件、环境变量和内置默认值幂等补齐空值；已持久化值优先于基础配置，重复启动不得漂移。
- `SET-RUN-004`：前置为配置 JWT secret 为空或仍是公开默认常量；首次确保默认设置时必须生成随机 32 字节 secret 并持久化；配置显式提供的非默认 secret 必须原样保留。
- `SET-RUN-005`：五个分区写入端点（`PUT /api/v1/settings/runtime/general|security|certificates|agent|branding`，见 `SET-RUN-022`）成功时必须以“当前内存快照 + 本分区字段”合成整套有效候选，校验后持久化并更新内存快照；失败时不得出现数据库已改、内存未改或部分字段已生效的状态。整体 `PUT /api/v1/settings/runtime` 已删除，不保留兼容别名。
- `SET-RUN-006`：容器上报间隔至少 1 秒，远程命令超时至少 1 秒，非法值分别返回稳定验证错误且不写入。指标保留天数与采集间隔不再属于运行时设置，改由指标设施校验（`FAC-MET-003`）并由清理 worker 使用（`FAC-MET-005`）；设置接口不得再发射 `invalid_metrics_retention`/`invalid_metrics_interval`。
- `SET-RUN-007`：token expiration 只允许 `10m|1h|1d|5d|30d|never`；日志级别只允许 `debug|info|warn|error`；语言只允许受支持 locale，非法输入不得被静默改成另一个值。清理计划 `cleanupSchedule` 已随指标设施移出设置接口（`FAC-MET-006`），运行时设置不再校验或保存该字段。另外，已移除的保留/采集间隔/清理计划错误码 `invalid_metrics_retention`、`invalid_metrics_interval`、`invalid_cleanup_schedule` 不得继续发射。
- `SET-RUN-008`：品牌标题按 Unicode 字符计数不得超过 80，副标题不得超过 240；输入成功时去除首尾空白并立即供公开品牌接口读取。
- `SET-RUN-009`：证书邮箱为空时允许保存，非空时必须是合法地址；DNS 传播延迟可为 0 但不得为负；验证失败不得改变 ACME 后续执行配置。
- `SET-RUN-010`：Panel 域名保存前必须小写并裁剪，只接受 IP、`localhost` 或合法完整主机名；空值、路径/端口/用户信息字符、连续点、无点的任意主机名或超过长度限制必须返回 `invalid_panel_domain`。
- `SET-RUN-011`：分区写入请求中省略本分区可选字段时必须保留当前值（`branding`、`certificates`/`panel`、协调追踪、`agent` 等），不得把省略解释为清空；以 0/空串表示“保留”的兼容字段只能按服务定义处理，不得意外清空既有值。
- `SET-RUN-012`：语言更新成功后后端默认 locale必须立即切换；日志级别必须立即更新进程 AtomicLevel；协调追踪必须立即同步全局开关；这些热更新无需重启且重启后从持久值恢复。
- `SET-RUN-013`：远程命令超时更新成功后，新发起的 SSH 操作必须读取新时长；正在执行的操作不要求被追溯修改。
- `SET-RUN-014`：容器上报间隔更新后，已连接的 Agent report stream 必须接收当前值；不得要求重建服务器记录或写入节点侧 Panel 回调地址。指标采集间隔不再由运行时设置下发，而由指标设施按节点下发并在下一轮 stream 生效（`FAC-MET-002/004`）；改指标间隔不需要也不得触发运行时设置保存。
- `SET-RUN-015`：运行时设置包含 Agent 投递分组 `agent.downloadBaseUrl`、`agent.downloadVerifyTls`、`agent.transferTimeoutSeconds`（默认空、false、120）。`GET /settings/runtime` 必须返回该分组，`PUT /settings/runtime/agent` 必须接受该分组；请求省略 `transferTimeoutSeconds`（或本分区其它字段）时保留已存值而不是写入 0。空 `downloadBaseUrl` 表示关闭 HTTP 投递，Agent 部署继续只走 SSH 上传。
- `SET-RUN-016`：`downloadBaseUrl` 必须校验并归一化为不含 path/query/fragment/userinfo 的 `http(s)://host[:port]` 源，并拒绝引号、空白、反斜杠与 shell 元字符（该值会被插值进远端 shell 命令）；`transferTimeoutSeconds` 必须在 60..3600 之间。读写响应必须回显归一化结果（去掉末尾 `/`）。
- `SET-RUN-017`：Agent 投递设置更新成功后，随后创建的 `server_agent_deploy` 任务必须读取新值，无需重启 Panel。
- `SET-RUN-021`：`GET /api/v1/settings/runtime`、五个分区写入端点（`SET-RUN-022`）与设置页「运行时」分区都不包含 `metricsRetentionDays`、`metricsCollectionIntervalSeconds`、`cleanupSchedule`；这三项改由指标设施配置管理（`FAC-MET-001/003/005/006`），设置页与前端类型、Mock、校验与自动保存负载都必须移除它们。`runtime_settings` 旧键保留不删除，仅供设施首次播种；旧客户端仍提交这三项时按未知字段忽略，不得写回运行时设置或改变设施配置。`containerReportIntervalSeconds` 保留并继续按 `SET-RUN-014` 热更新。
- `SET-RUN-022`：设置写入拆分为五个分区子资源端点。触发入口为认证用户调用 `PUT /api/v1/settings/runtime/general`（`{containerReportIntervalSeconds,language,logLevel,reconcileTraceEnabled}`）、`PUT /api/v1/settings/runtime/security`（`{tokenExpiration,remoteCommandTimeoutSeconds}`）、`PUT /api/v1/settings/runtime/certificates`（`{panel:{domain,tlsCertificateId},certificates:{email,dnsPropagationDelaySeconds}}`）、`PUT /api/v1/settings/runtime/agent`（`{downloadBaseUrl,downloadVerifyTls,transferTimeoutSeconds}`）与 `PUT /api/v1/settings/runtime/branding`（`{loginTitle,loginSubtitle}`）；`GET /api/v1/settings/runtime` 保持返回完整 `RuntimeSettings`，整体 `PUT /api/v1/settings/runtime` 删除且不保留兼容别名。每个分区端点只接受/只改变本分区字段：范围内字段省略时保留当前值，成功一律返回完整 `RuntimeSettings`（与 `GET` 同构），不得只返回本分区投影。服务端必须以“当前内存快照 + 本分区字段”合成完整候选，先校验后持久化、再更新内存快照；任一校验或写库失败必须整笔不生效（不得数据库已改而内存未改、或部分字段生效），失败沿用既有稳定错误码（`invalid_container_report_interval`、`invalid_token_expiration`、`invalid_language`、`invalid_log_level`、`invalid_remote_command_timeout`、`invalid_branding_*`、`invalid_certificate_*`、`invalid_panel_domain`、`invalid_panel_tls_certificate`、`invalid_agent_*` 等），不得新增错误码。热更新范围沿用 `SET-RUN-012/013/014`；`certificates` 分区继续在 `paneltls.WithUpdate` 临界区内固定证书对并遵守 `SET-TLS-004` 的回滚语义。并发边界：同一分区的连续写入按到达顺序生效、最后一次成功写入为准，不得把不同请求的字段交叉合并成未提交组合；不同分区可各自成功，一次失败不得回滚其它分区已持久化的值；客户端不得用迟到的旧响应覆盖较新的分区值（前端行为由 `UI-SET-014` 约束）。验证点：`internal/modules/settings` 的分区端点测试覆盖各端点只改本分区、返回完整设置、非法值 422 且旧值不变；`internal/bootstrap/panel/routes_manifest_test.go` 的 `wantCount=194`（删除整体 PUT、新增 5 条 PUT）；前端 typed client 与 Mock 路径/负载一致（`API-COV-002`、`ID-EVD-004`）。
- `SET-RUN-024`：镜像仓库源设置。`GET /api/v1/settings/runtime` 在 `RuntimeSettings.imageRegistry` 返回 `{endpoint,scheme}`；`PUT /api/v1/settings/runtime/image-registry` 只接受 `{endpoint?,scheme?}`（指针语义：省略=保留，显式空串=官方仓库），成功返回完整 `RuntimeSettings`。默认 `endpoint=""`、`scheme="https"`；旧库缺键/非法持久值归一化默认。校验：`endpoint` 必须为空或裸 `host[:port]`（禁止 scheme/path/query/userinfo/空白/引号/反斜杠；host 或 IP 合法、端口 1..65535），`scheme` 只能 `http|https`；错误码 `invalid_image_registry_endpoint`、`invalid_image_registry_scheme`，失败整笔不生效。语义：Panel 解析镜像标签 digest 时使用该主机替换镜像引用自带 registry（路径 `/v2/<repo>/manifests/<tag>` 不变，Bearer 认证流程不变），仅影响 Panel 出口检查；节点 Docker 拉取仍由 Agent/节点 Docker 配置决定，不得改写应用/设施的镜像引用。热更新：下一次 digest 解析即生效。验证：分区端点只改本分区、默认/旧库、四类非法 endpoint/scheme、显式 http、省略保留、解析器主机覆盖与 401 重试单测、前端类型/Mock（`UI-SET-016`）。

- `SET-RUN-025`：SSH 访问记录保留期。`GET /api/v1/settings/runtime` 在 `RuntimeSettings.sshAccess` 返回 `{successRetentionDays,failureRetentionDays}`；`PUT /api/v1/settings/runtime/security` 除既有 `tokenExpiration`、`remoteCommandTimeoutSeconds` 外接受可选 `sshAccess`（指针语义：省略=保留，字段省略=保留该字段），成功返回完整 `RuntimeSettings`。默认成功 30 天、失败 3 天；范围 1..3650，越界或非整数返回 `invalid_ssh_access_retention` 且整笔不生效。持久键为 `sshAccess.successRetentionDays`、`sshAccess.failureRetentionDays`；旧库缺键/非法值归一化为默认。保存后下一次每小时清理即生效，无需重启；成功会话只清理已退出且过期者，在线会话永不自动删除。
- `SET-RUN-023`：自动更新设置分区。`GET /api/v1/settings/runtime` 在 `RuntimeSettings.autoUpdate` 返回 `{appsEnabled,packagesEnabled,packagesMode,scheduleTime,lastRunAt,lastRunStatus,lastRunTaskId,lastAutoRunAt,lastRunSummary}`；`PUT /api/v1/settings/runtime/auto-update` 只接受 `{appsEnabled?,packagesEnabled?,packagesMode?,scheduleTime?}`（均用指针表达"省略=保留"），成功返回完整 `RuntimeSettings`。默认值：`appsEnabled=false`、`packagesEnabled=false`、`packagesMode=security`、`scheduleTime="04:00"`（UTC）；旧库缺键与非法持久值按默认归一化。校验：`scheduleTime` 必须匹配 `^([01]\d|2[0-3]):[0-5]\d$`（UTC，错误码 `invalid_auto_update_schedule_time`；前端按用户浏览器时区显示与编辑并在加载/保存时与 UTC 自动换算），`packagesMode` 只能是 `security|all`（`invalid_auto_update_packages_mode`）；失败整笔不生效。`lastRunAt/lastRunStatus/lastRunTaskId/lastAutoRunAt/lastRunSummary` 为只读运行态（不进入写入负载，普通设置保存不得覆盖）：`lastRunAt` 为最近一次运行（手动或计划）开始时间，`lastAutoRunAt` 只由计划自动运行写入并按 UTC 日期去重，`lastRunSummary` 为最近一次运行的结果摘要（完成/失败/禁用均写，失败附原因）。热更新：调度器每次 tick 读取当前快照，改时间/开关后无需重启。手动触发：`POST /api/v1/settings/runtime/auto-update/run`（payload 可选 `{appsEnabled?,packagesEnabled?,packagesMode?}` 临时覆盖，省略=当前设置）在任一开关开启时创建 `auto_update_run`（`TriggerType=user`，跳过计划触发条件但仍写 lastRun，不写 lastAutoRunAt），返回 202 `{taskId}`；两个开关都关返回 422 `auto_update_disabled`；已有活跃任务时幂等返回其 taskId；若已有任务处于 queued/scheduled/failed_retryable 且未在运行，必须补派发而不是静默返回。验证：分区端点只改本分区、false 能真正落库、旧库默认、非法值不写库、只读运行态投影、手动运行不抑制当天计划自动运行、前端类型/Mock 一致（`UI-SET-015`）。

## 4. 外观设置（主题 / Mica / 强调色 / 背景图）

面板是单管理员全局面板：外观只有一份全局设置，所有浏览器与设备共享，最后写入生效，不做“每用户偏好”或 revision 乐观锁。真源是 AppDB `appearance_settings` 单行（`id='appearance'`），前端 localStorage 只作首屏缓存（`SET-APP-012`）。

- `SET-APP-001`：前置为认证用户；`GET /api/v1/settings/appearance` 必须返回与前端 `web/src/types/appearance.ts` 完全一致的字段：`{themeMode,mica,accent,background:{kind,presetId,assetId,revision,hasImage},thumbDataUrl,initialized,updatedAt}`。`themeMode` 只能是 `system|light|dark`，`accent` 只能取 9 个预设，`kind` 只能是 `none|preset|image`，`presetId` 在 `kind='preset'` 时是 `aurora|dusk|mist|slate` 之一否则为 null，`assetId` 在 `hasImage=false` 时为 null，`updatedAt` 为 UTC RFC3339(Nano) 字符串。**`hasImage=true` 时响应必须总是带 `thumbDataUrl`（`data:image/jpeg;base64,...`，约 2–6KB），与当前 `kind` 是 none/preset/image 无关**；响应绝不包含背景原图字节或服务端文件路径。
- `SET-APP-002`：`PUT /api/v1/settings/appearance` 以整套有效候选整体替换主题/Mica/强调色，并可选更新背景；请求体为 `{themeMode,mica,accent,background?}`。`background` 省略时**必须保留当前背景（包括已上传图片资产与缩略图），不得删除任何文件**；成功返回完整 `Appearance` 并置 `initialized=true`、刷新 `updatedAt`。校验失败必须零写入（主题/Mica/强调色/背景均不改变），单管理员场景下最后一次成功写入生效。
- `SET-APP-003`：PUT 与上传接口必须发射稳定错误码：非法 `themeMode` 返回 `invalid_theme_mode`；非法 `accent` 返回 `invalid_appearance_accent`；非法 `kind`/预设 id 返回 `invalid_background_kind`；`kind='image'` 但没有可复用资产、或上传请求缺少 `file` 字段时返回 `background_required`。任一错误不得写库或改动既有背景资产。
- `SET-APP-004`：`POST /api/v1/settings/appearance/background` 使用 multipart 字段 `file`。必须显式用 `http.MaxBytesReader` 限制请求体为 **8 MiB + 1 MiB** 余量（不依赖全局 10 MiB JSON 上限）、用小内存 `ParseMultipartForm` 并 `defer RemoveAll`，再用 `io.LimitReader(limit+1)` 判定文件本身是否超过 8 MiB；超过返回 413 `background_too_large`。**一律忽略用户文件名**，目录名/缓存键由服务端按内容哈希生成；损坏或非 multipart 请求返回稳定 4xx。
- `SET-APP-005`：图片只接受真实 **JPEG/PNG**（以 `http.DetectContentType` 魔数判断，不看扩展名/客户端 Content-Type）；GIF/WebP 必须拒绝并返回 `background_unsupported_type`（等价 APNG 按 PNG 静态首帧处理）。JPEG 以 quality 87 重新编码（剥离 EXIF 等元数据），PNG 重新编码并保留透明通道；最大边超过 2560 时用标准库盒子采样等比缩小到 2560 内，其余尺寸原样保留。服务端不解析 EXIF 方向：浏览器上传前用 canvas 归一化，直接调 API 的带方向 JPEG 按像素原样处理。
- `SET-APP-006`：解码前必须先用 `image.DecodeConfig` 读取尺寸并拒绝 >4096×4096 或 >12MP 的图片（返回 `background_dimensions_exceeded`），再完整解码；无法解码/伪造魔数/损坏文件返回 `background_invalid_image`。必须生成 **160×90（cover）JPEG quality 70** 缩略图作为 Mica 底图，缩略图超过 64KB 或归一化 master 超过 8 MiB 时失败且不写 DB；上传失败不得留下资产目录或改变旧背景（原子替换）。
- `SET-APP-007`：`GET /api/v1/settings/public-appearance` 必须 `skipAuth`（登录页可用，注册于公开路由但仍计入主 API 路由清单），只返回 `{themeMode,mica,accent,background:{kind,presetId,hasImage},thumbDataUrl}`；`thumbDataUrl` **仅在 `kind='image'` 时返回**，且始终是模糊缩略图。该接口绝不返回背景原图、`assetId`、`revision`、文件路径或其它运行时配置；原图只能经认证的 background/content 接口读取。
- `SET-APP-008`：背景资产存放于 `<dataRoot>/appearance/<assetId>/`（`assetId` 为归一化 master 的 SHA-256 十六进制前缀，目录 0700、文件 0644），文件名固定为 `master.jpg|master.png` 与 `thumb.jpg`；写入必须复用 `filestorage.WriteAtomic`（临时文件 + rename），顺序固定为“写新资产目录 → 提交 DB → 删除旧资产目录”。上传/删除都要递增 `background_revision`（用于缓存与并发提示），删除后旧目录必须被移除。
- `SET-APP-009`：`DELETE /api/v1/settings/appearance/background` 删除背景资产并恢复 `kind='none'`、`hasImage=false`、`presetId=null`，保留主题/Mica/强调色；重复删除幂等返回当前外观。`GET /api/v1/settings/appearance/background/content?variant=full|thumb` 必须返回 `Content-Type`、`ETag`（按 assetId+variant）、`Cache-Control: private, max-age=31536000, immutable`、`Content-Disposition: inline`；未设置背景返回 404，非法 variant 返回 400。`GET/PUT/DELETE` 与上传/删除之外的 `kind=none|preset` 切换必须保留已上传资产，只有 DELETE 才真正删除文件与 `hasImage`。
- `SET-APP-010`：启动时必须幂等 seed 默认行（`themeMode=system`、`mica=true`、`accent=blue`、`background=preset/mist`、`initialized=false`），并把从未初始化过（`initialized=0`、无上传资产、背景 none）的旧行升级为默认 `mist` 预设；用户显式选择 none 后 `initialized=1`，不得覆盖。旧 alpha 库由 AutoMigrate 自动建表，不需要额外迁移工具。启动与定时任务必须清理 `<dataRoot>/appearance/` 下不再被当前行引用且超过保留期（24 小时）的孤儿目录；DB 记录指向缺失/不可读文件时必须降级为“无背景”（清空资产元数据、`kind=image` 时回落 none、revision 递增）并记录告警，不得让读取接口失败或阻塞启动。
- `SET-APP-011`：默认全量备份归档覆盖整个 `dataRoot`，因此必须自动包含 `<dataRoot>/appearance/` 背景资产目录（`BKP-API-005`），恢复按整目录替换；不需要新增 manifest 条目或备份代码改动。备份/恢复合同必须与“外观是全局唯一设置 + 文件在 dataRoot 内”的持久化模型保持一致。
- `SET-APP-012`：外观是单管理员全局面板设置（无每用户/多租户分支）：多个浏览器/设备共享同一 `appearance_settings` 行，最后写入生效；并发上传/删除必须由服务端互斥串行化，避免两个请求交叉删除对方资产目录。`initialized=false` 表示从未显式保存过外观，仅用于前端首次登录时把旧 localStorage 偏好（lighthouse→teal、ocean→blue、旧 mode）一次性 PUT 迁移；迁移成功后服务器置 `initialized=true`，已初始化安装不得被本地缓存覆盖。localStorage `panel.appearance.cache` 只是首屏缓存，服务器始终为真源。

## 5. Panel HTTPS 设置与持久化安全

- `SET-TLS-001`：前置为 TLS certificate ID 为空；Panel 必须选择内置 `panel-tls` 资产并同步到 `<dataRoot>/tls/panel.crt|panel.key`，Agent mTLS 资产不得被当作监听证书。
- `SET-TLS-002`：前置为选择自定义资产；资产必须存在、类型为 TLS certificate、包含可读取且匹配的证书/私钥、当前有效、具备 ServerAuth（若声明 EKU）、链完整，并使用 RSA 2048+ 或 ECDSA P-256/P-384/P-521 与 SHA-2 签名；否则返回 `invalid_panel_tls_certificate`。
- `SET-TLS-003`：TLS 候选列表不得按当前 Panel 域名 SAN 预过滤；设置保存的监听兼容性验证也不强制 SAN 匹配，但必须保留 Panel 域名自身格式校验。
- `SET-TLS-004`：更改 Panel 域名或证书时，固定证书对激活与 runtime_settings 持久化必须在同一进程临界区串行；持久化失败必须从内存快照恢复旧文件并保持旧设置。
- `SET-TLS-005`：证书对写入必须使用配对事务标记和可恢复 previous 文件，证书为 0644、私钥与事务标记为 0600；中断恢复后只能得到完整旧对或明确不可用，不得把新证书与旧私钥混配。
- `SET-TLS-006`：同步成功必须清空进程证书缓存，使下一条新 TLS 连接加载新对；固定文件只是监听缓存，数据库 key asset 才是真源。
- `SET-TLS-007`：固定证书缺失、不完整、无法解析或不符合监听基线时，HTTPS 监听必须失败关闭；不得自动生成未登记证书或退回明文 HTTP。

## 6. 服务器变量定义

- `SET-VAR-001`：认证用户调用 `GET /api/v1/settings/server-variables` 时，未配置返回空数组；已配置按保存顺序返回 `{name,key,required}`，展示名不作为持久化键。
- `SET-VAR-002`：`PUT /api/v1/settings/server-variables` 必须将整套定义作为一个稳定 JSON 值替换；成功响应与随后读取结果一致，空数组表示明确清空。
- `SET-VAR-003`：每个展示名裁剪后必须非空；key 必须匹配 `[A-Za-z_][A-Za-z0-9_]*` 且在同一集合中唯一；失败返回对应稳定错误并保留旧集合。
- `SET-VAR-004`：定义更新不得隐式改写已有服务器变量值；服务器创建/编辑如何校验 required 由服务器域合同约束。

## 7. 系统版本与更新检查

- `SYS-VER-001`：认证用户调用 `GET /api/v1/system/version` 时，必须返回构建注入的规范化 `version`、`channel`、可选 commit/repository、缓存的 latestVersion、updateAvailable、checkedAt和可选 checkError；接口只读，不提供下载或安装。
- `SYS-VER-002`：仅 `channel=release`、repository 非空且当前版本为三段数字核心版本（可有 `v` 和预发布后缀）时启动更新检查；dev、无效通道、`dev` 版本或未注入仓库不得发外部请求。
- `SYS-VER-003`：符合条件时启动后立即检查并每 6 小时重复；重复 `Start` 不得启动多个循环，`Close` 必须停止循环且可重复调用。
- `SYS-VER-004`：GitHub 请求必须使用 10 秒默认超时、GitHub JSON Accept 和可识别 User-Agent；只接受 HTTP 200、非 draft 且非空 tag 的 latest release。
- `SYS-VER-005`：检查成功时按数字核心版本比较，只有远端更高才设置 `updateAvailable=true`；预发布后缀不应把相同核心误判为更新。
- `SYS-VER-006`：网络、HTTP、JSON或 release 内容失败时，必须更新 checkedAt 和安全的 checkError，清空 latestVersion并置 updateAvailable=false；失败不得使版本接口不可用或终止 Panel。

## 8. 权限、幂等与验证证据

- `ID-EVD-001`：除登录、会话、公开品牌与公开外观外，上述 API 未认证均返回 401；强制改密状态的权限矩阵必须由后端集成测试覆盖。
- `ID-EVD-002`：账户、nonce、JWT secret和 runtime settings 位于 AppDB；进程重启后账户、设置和主动注销效果必须保持，登录失败计数可重置。
- `ID-EVD-003`：账户修改、JWT secret 更新和设置更新的失败用例必须验证旧值仍可读取；成功用例必须验证旧 token 失效或继续有效的精确边界。
- `ID-EVD-004`：路由验收必须覆盖 `POST /auth/login|logout|account|jwt-secret`、`GET /auth/session`、`GET /setup/status`、`POST /setup`、`GET /permissions`、`GET/POST /users`、`GET/PATCH/DELETE /users/{id}`、`POST /users/{id}/password|logout`、`GET/POST /roles`、`GET/PATCH/DELETE /roles/{id}`、`GET/POST /access-credentials`、`DELETE /access-credentials/{id}`、`GET /settings/runtime`、`PUT /settings/runtime/general|security|certificates|agent|branding|auto-update|image-registry`、`POST /settings/runtime/auto-update/run`、`GET/PUT /settings/server-variables`、`GET/PUT /settings/appearance`、`POST/DELETE /settings/appearance/background`、`GET /settings/appearance/background/content`、`GET /settings/public-branding`、`GET /settings/public-appearance` 和 `GET /system/version`，且与路由清单（`routes_manifest_test.go` 的 `wantCount=221` 基线）及前端 typed client 一致。

## 9. 多用户、角色与权限

- `ID-USR-001`：`GET /api/v1/users` 仅 `access.users.manage` 可调用；返回分页用户列表（id、username、displayName、status、builtin、mustChangePassword、roles、lastLoginAt、createdAt），按创建时间升序；不返回密码哈希、代数或会话凭证。
- `ID-USR-002`：`POST /api/v1/users` 创建自定义用户；用户名 3..64 字符且大小写不敏感唯一；密码 8..72 字节；新用户 `mustChangePassword=true`，首次登录必须改密；内置 administrator 角色不可分配。
- `ID-USR-003`：`GET /api/v1/users/{id}` 返回单个用户投影，不存在返回 404。
- `ID-USR-004`：`PATCH /api/v1/users/{id}` 可改显示名、状态与角色；禁用时递增该用户代数使全部 JWT 与访问凭证立即失效；内置管理员不可修改；不能禁用自己。
- `ID-USR-005`：`DELETE /api/v1/users/{id}` 删除用户及其角色关联与访问凭证；内置管理员、自己不可删除。
- `ID-USR-006`：`POST /api/v1/users/{id}/password` 由管理员重置密码：新密码满足 8..72 字节，写新哈希、置 `mustChangePassword=true`、递增目标用户代数；内置管理员只能本人改密。
- `ID-USR-007`：`POST /api/v1/users/{id}/logout` 递增目标用户代数，撤销其全部 JWT 与访问凭证。
- `ID-USR-008`：内置管理员由初始化创建，`builtin=1`，不可被改名、改角色、禁用、删除或由他人重置密码；用户名仅允许本人在首次登录改密时修改一次，之后锁定。

- `ID-ROLE-001`：`GET /api/v1/roles` 返回内置 administrator 与自定义角色；内置角色 `builtin=true` 且拥有全部权限；响应包含 permissionKeys、用户数、创建时间。
- `ID-ROLE-002`：`POST /api/v1/roles` 创建自定义角色；至少选择一项权限；未知权限返回 `unknown_permission`，`access.*` 返回 `permission_not_grantable`；服务端按权限目录补齐 `requires` 依赖闭包后落库。
- `ID-ROLE-003`：`GET /api/v1/roles/{id}` 返回单个角色投影。
- `ID-ROLE-004`：`PATCH /api/v1/roles/{id}` 更新名称/说明/权限集合并重新计算依赖闭包；内置 administrator 不可修改；保存失败不得留下部分权限。
- `ID-ROLE-005`：`DELETE /api/v1/roles/{id}` 删除自定义角色；仍被用户引用返回 `409 role_in_use`；内置角色不可删除。
- `ID-PERM-001`：`GET /api/v1/permissions` 登录即可读取，返回权限目录（groups + permissions），包含 key/group/labelKey/descriptionKey/risk/mode/requires/grantable；不包含用户或角色数据。
- `ID-PERM-002`：每个受保护 API 必须且只能映射到一个权限；未映射路由 fail-closed 返回 `403 permission_denied`；目录测试枚举全部已注册 `/api` 路由并断言覆盖。
- `ID-PERM-003`：权限按请求实时解析（用户角色并集 + 依赖闭包）；角色变更/删除后下一次请求立即生效，无需重新登录；JWT 不携带角色或权限。
- `ID-PERM-004`：内置管理员隐式拥有目录全部权限；自定义用户只拥有角色并集；权限目录新增权限时内置管理员自动获得。
- `ID-PERM-005`：每个权限在目录中至少声明一条 UI 映射（page/kind/target/effect），可交互类型（button/menu/field/column/tab/dialog/mask）必须声明稳定 `anchor`；Vue 源码中的 `data-perm-anchor` 与目录 anchor 必须双向一致，由 `internal/modules/identity/catalog_ui_test.go` 强制。

## 10. 访问凭证

- `ID-CRED-001`：`GET /api/v1/access-credentials` 返回当前用户的凭证列表（不含 token 明文），状态为 active/expired/revoked。
- `ID-CRED-002`：`POST /api/v1/access-credentials` 仅接受交互式会话（拒绝访问凭证自身调用）；`expiresInSeconds` 60..1800，默认 900；`scope` 只能 readonly/readwrite，默认 readonly；readonly 时拒绝 `allowManagement=true`；每人最多 10 个有效凭证；响应 201 且只返回一次明文 token，`Cache-Control: no-store`。
- `ID-CRED-003`：`DELETE /api/v1/access-credentials/{id}` 撤销本人凭证；持有 `access.credentials.manage` 可撤销他人凭证；撤销后立即失效。
- `ID-CRED-004`：凭证使用不透明 `pac_` token，服务端只保存 SHA-256 与展示前缀；校验 token、未撤销、未过期、用户 active、用户代数一致；失败统一 401。
- `ID-CRED-005`：凭证有效权限为用户当前权限（readonly 再与目录 `mode=read` 求交），实时生效；用户降权后凭证下一请求即按新权限。
- `ID-CRED-006`：凭证硬禁止：创建/续期凭证、修改密码/用户名、修改 JWT secret、登录/登出；`allowManagement=false` 时另禁止用户/角色管理端点并返回 `403 credential_scope_restricted`；只读凭证调用写权限接口返回 `403 credential_read_only`。
- `ID-CRED-007`：到期或撤销后 JWT/凭证均不可再用；用户登出、改密、重置密码、禁用等递增用户代数后，该用户已签发凭证一并失效。

## 11. 初始化与首登改密

- `ID-SETUP-001`：`GET /api/v1/setup/status` 公开；`required=true` 仅当 `users` 表为空，已有用户后恒为 false。
- `ID-SETUP-002`：`POST /api/v1/setup` 公开但仅无用户时可用；创建唯一内置管理员（`builtin=1`、`mustChangePassword=true`）并分配 administrator 角色；不签发 token；重复调用返回 `409 setup_completed`，不得覆盖现有用户；用户名/密码默认 `admin/admin`（不随机生成），支持请求覆盖。
- `ID-SETUP-003`：无环境变量且无用户时系统进入初始化模式，登录/业务接口仍要求认证；环境变量存在时启动直接创建内置管理员并跳过初始化界面；只要库中已有任何用户，环境变量全部忽略（包括非法值）。
- `ID-ACC-013`：首次登录（`mustChangePassword=true`）只允许 `GET /auth/session`、`POST /auth/logout`、`POST /auth/account`；访问其它端点返回 `403 password_change_required`。
- `ID-ACC-014`：`POST /auth/account` 在首登状态必须提供新密码；可同时修改用户名（内置管理员仅限首登）；成功后清 `mustChangePassword`、递增用户代数、返回 `{authenticated:false,mustRelogin:true}`，旧 JWT 立即失效；必须重新登录。
- `ID-SES-009`：JWT claims 携带 `kg`（密钥代数）与 `ug`（用户代数）；任一与当前持久值不一致返回 401；登出只递增该用户代数，JWT secret 轮换递增全局密钥代数使全部 JWT 失效。
- `ID-SES-010`：JWT 不含角色/权限；角色调整不递增代数、不强制重新登录，下一次请求按最新权限解析。
