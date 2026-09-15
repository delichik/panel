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
- `ID-ACC-011`：前置为当前密码正确且输入有效；`POST /api/v1/auth/account` 必须原子保存裁剪后的用户名；提供新密码时保存新的 bcrypt 哈希、清除强制改密、随机更换 JWT secret、轮换 token nonce，并返回新的有效会话。
- `ID-ACC-012`：前置为仅修改用户名且不在强制改密状态；更新必须保留原密码和 JWT secret，返回能以新用户名表示当前账户的有效会话。

## 2. Token、会话与权限边界

- `ID-SES-001`：前置为普通运行态请求；受保护路由只接受精确 `Authorization: Bearer <token>`，缺失、格式错误、签名错误、subject 错误、nonce 缺失、过期或已撤销 token 均返回 401，业务 handler 不得执行。
- `ID-SES-002`：前置为无 token 或无效 token；`GET /api/v1/auth/session` 必须仍返回 200 与 `authenticated=false`，不得把探测会话状态转换为 401。
- `ID-SES-003`：前置为有效 token；会话查询必须返回当前数据库中的管理员用户名和强制改密状态，而不是只信任 token 内旧用户名。
- `ID-SES-004`：前置为有效 token；`POST /api/v1/auth/logout` 必须轮换持久化 nonce，使此前签发的全部普通运行态 token 立即失效，并返回 `authenticated=false`；重复登出需认证且不得恢复旧 token。
- `ID-SES-005`：前置为 token expiration 设置为 `10m`、`1h`、`1d`、`5d` 或 `30d`；新 token 的过期时间必须按对应时长签发并在到期后拒绝；设置为 `never` 时 token 可无 `exp`，但仍受 nonce 和 JWT secret 撤销。
- `ID-SES-006`：前置为认证用户提交至少 16 字符的新 secret；`POST /api/v1/auth/jwt-secret` 必须先持久化 secret，再轮换 nonce并返回新会话；旧 token 必须失效，响应不得回显 secret。
- `ID-SES-007`：前置为 secret 少于 16 字符或仅空白；更新必须返回 `invalid_jwt_secret`，旧 secret、nonce和现有会话必须保持可用。
- `ID-SES-008`：普通运行态 token、备份导出维护 token、恢复维护 token 三种上下文必须互不通用；任何跨上下文使用均不得获得业务或维护权限。

## 3. 运行时设置读取与写入

- `SET-RUN-001`：前置为认证用户；`GET /api/v1/settings/runtime` 必须返回监听地址、数据库路径、数据根、指标/事件保留与周期、会话期限、语言、日志级别、远程超时、协调追踪、品牌、证书和 Panel TLS 选择状态，但绝不返回 JWT secret，仅返回 `jwtSecretConfigured`。
- `SET-RUN-002`：前置为未认证用户；仅 `GET /api/v1/settings/public-branding` 可公开，且只返回登录标题和副标题；不得泄露路径、邮箱、证书 ID、保留策略或其他运行时配置。
- `SET-RUN-003`：前置为旧库缺少设置行；启动必须用配置文件、环境变量和内置默认值幂等补齐空值；已持久化值优先于基础配置，重复启动不得漂移。
- `SET-RUN-004`：前置为配置 JWT secret 为空或仍是公开默认常量；首次确保默认设置时必须生成随机 32 字节 secret 并持久化；配置显式提供的非默认 secret 必须原样保留。
- `SET-RUN-005`：`PUT /api/v1/settings/runtime` 成功时必须以整套有效候选进行校验后持久化，并更新内存快照；失败时不得出现数据库已改、内存未改或部分字段已生效的状态。
- `SET-RUN-006`：保留天数均必须至少 1，且运行事件保留天数不得小于详情保留天数；指标和容器上报间隔至少 1 秒；远程命令超时至少 1 秒，非法值分别返回稳定验证错误且不写入。
- `SET-RUN-007`：清理周期和运行事件清理周期只允许 `hourly|daily|weekly`；token expiration 只允许 `10m|1h|1d|5d|30d|never`；日志级别只允许 `debug|info|warn|error`；语言只允许受支持 locale，非法输入不得被静默改成另一个值。
- `SET-RUN-008`：品牌标题按 Unicode 字符计数不得超过 80，副标题不得超过 240；输入成功时去除首尾空白并立即供公开品牌接口读取。
- `SET-RUN-009`：证书邮箱为空时允许保存，非空时必须是合法地址；DNS 传播延迟可为 0 但不得为负；验证失败不得改变 ACME 后续执行配置。
- `SET-RUN-010`：Panel 域名保存前必须小写并裁剪，只接受 IP、`localhost` 或合法完整主机名；空值、路径/端口/用户信息字符、连续点、无点的任意主机名或超过长度限制必须返回 `invalid_panel_domain`。
- `SET-RUN-011`：请求省略规范允许的可选子对象时必须保留当前 `branding`、`certificates`、`panel` 和协调追踪值；以 0/空串表示“保留”的兼容字段只能按服务定义处理，不得意外清空既有值。
- `SET-RUN-012`：语言更新成功后后端默认 locale必须立即切换；日志级别必须立即更新进程 AtomicLevel；协调追踪必须立即同步全局开关；这些热更新无需重启且重启后从持久值恢复。
- `SET-RUN-013`：远程命令超时更新成功后，新发起的 SSH 操作必须读取新时长；正在执行的操作不要求被追溯修改。
- `SET-RUN-014`：指标采集与容器上报间隔更新后，已连接的 Agent report stream 必须接收当前值；不得要求重建服务器记录或写入节点侧 Panel 回调地址。

## 4. Panel HTTPS 设置与持久化安全

- `SET-TLS-001`：前置为 TLS certificate ID 为空；Panel 必须选择内置 `panel-tls` 资产并同步到 `<dataRoot>/tls/panel.crt|panel.key`，Agent mTLS 资产不得被当作监听证书。
- `SET-TLS-002`：前置为选择自定义资产；资产必须存在、类型为 TLS certificate、包含可读取且匹配的证书/私钥、当前有效、具备 ServerAuth（若声明 EKU）、链完整，并使用 RSA 2048+ 或 ECDSA P-256/P-384/P-521 与 SHA-2 签名；否则返回 `invalid_panel_tls_certificate`。
- `SET-TLS-003`：TLS 候选列表不得按当前 Panel 域名 SAN 预过滤；设置保存的监听兼容性验证也不强制 SAN 匹配，但必须保留 Panel 域名自身格式校验。
- `SET-TLS-004`：更改 Panel 域名或证书时，固定证书对激活与 runtime_settings 持久化必须在同一进程临界区串行；持久化失败必须从内存快照恢复旧文件并保持旧设置。
- `SET-TLS-005`：证书对写入必须使用配对事务标记和可恢复 previous 文件，证书为 0644、私钥与事务标记为 0600；中断恢复后只能得到完整旧对或明确不可用，不得把新证书与旧私钥混配。
- `SET-TLS-006`：同步成功必须清空进程证书缓存，使下一条新 TLS 连接加载新对；固定文件只是监听缓存，数据库 key asset 才是真源。
- `SET-TLS-007`：固定证书缺失、不完整、无法解析或不符合监听基线时，HTTPS 监听必须失败关闭；不得自动生成未登记证书或退回明文 HTTP。

## 5. 服务器变量定义

- `SET-VAR-001`：认证用户调用 `GET /api/v1/settings/server-variables` 时，未配置返回空数组；已配置按保存顺序返回 `{name,key,required}`，展示名不作为持久化键。
- `SET-VAR-002`：`PUT /api/v1/settings/server-variables` 必须将整套定义作为一个稳定 JSON 值替换；成功响应与随后读取结果一致，空数组表示明确清空。
- `SET-VAR-003`：每个展示名裁剪后必须非空；key 必须匹配 `[A-Za-z_][A-Za-z0-9_]*` 且在同一集合中唯一；失败返回对应稳定错误并保留旧集合。
- `SET-VAR-004`：定义更新不得隐式改写已有服务器变量值；服务器创建/编辑如何校验 required 由服务器域合同约束。

## 6. 系统版本与更新检查

- `SYS-VER-001`：认证用户调用 `GET /api/v1/system/version` 时，必须返回构建注入的规范化 `version`、`channel`、可选 commit/repository、缓存的 latestVersion、updateAvailable、checkedAt和可选 checkError；接口只读，不提供下载或安装。
- `SYS-VER-002`：仅 `channel=release`、repository 非空且当前版本为三段数字核心版本（可有 `v` 和预发布后缀）时启动更新检查；dev、无效通道、`dev` 版本或未注入仓库不得发外部请求。
- `SYS-VER-003`：符合条件时启动后立即检查并每 6 小时重复；重复 `Start` 不得启动多个循环，`Close` 必须停止循环且可重复调用。
- `SYS-VER-004`：GitHub 请求必须使用 10 秒默认超时、GitHub JSON Accept 和可识别 User-Agent；只接受 HTTP 200、非 draft 且非空 tag 的 latest release。
- `SYS-VER-005`：检查成功时按数字核心版本比较，只有远端更高才设置 `updateAvailable=true`；预发布后缀不应把相同核心误判为更新。
- `SYS-VER-006`：网络、HTTP、JSON或 release 内容失败时，必须更新 checkedAt 和安全的 checkError，清空 latestVersion并置 updateAvailable=false；失败不得使版本接口不可用或终止 Panel。

## 7. 权限、幂等与验证证据

- `ID-EVD-001`：除登录、会话和公开品牌外，上述 API 未认证均返回 401；强制改密状态的权限矩阵必须由后端集成测试覆盖。
- `ID-EVD-002`：账户、nonce、JWT secret和 runtime settings 位于 AppDB；进程重启后账户、设置和主动注销效果必须保持，登录失败计数可重置。
- `ID-EVD-003`：账户修改、JWT secret 更新和设置更新的失败用例必须验证旧值仍可读取；成功用例必须验证旧 token 失效或继续有效的精确边界。
- `ID-EVD-004`：路由验收必须覆盖 `POST /auth/login|logout|account|jwt-secret`、`GET /auth/session`、`GET/PUT /settings/runtime`、`GET/PUT /settings/server-variables`、`GET /settings/public-branding` 和 `GET /system/version`，且与路由清单及前端 typed client 一致。
