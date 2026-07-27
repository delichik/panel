# DNS 与证书模块

## List And Snapshot Contracts

- Domain certificates, self-signed certificates, and key assets return `ListPage` responses. List rows omit private material, file paths, metadata, and reference detail.
- DNS record GET reads `dns_record_snapshots` only and returns `items`, `observedAt`, `stale`, `refreshing`, optional `refreshTaskId`, and optional `lastRefreshError`; it never calls a DNS provider.
- Record refresh is an async POST returning `202` and `taskId`; the frontend waits for completion and reloads the snapshot.
- `GET /api/v1/dns/domains` is a paginated local summary query with optional `q`; provider credentials and provider access checks are excluded from the list path.

## 适用场景

修改 DNS 域名管理、Cloudflare 集成、ACME 签发、证书存储、自动续签、证书变量或应用与反向代理证书联动时，先读本文档。

## 关键入口

- DNS 服务与 handler：`internal/modules/certificates/dns/`
- Cloudflare provider：`internal/modules/certificates/dns/cloudflare.go`
- 证书服务与 handler：`internal/modules/certificates/certs/`
- ACME 集成：`internal/modules/certificates/certs/acme.go`
- 周期续签：`internal/modules/certificates/certs/tasks.go` 注册周期输入，由 `internal/modules/tasks/` 内部 worker 驱动
- 前端 DNS 页面：`web/src/views/dns/index.vue`
- 域名证书、自签证书与密钥资产页面：`web/src/views/certificates/index.vue`
- API：`web/src/api/dns.ts`、`web/src/api/certificates.ts`、`web/src/api/keyAssets.ts`
- 类型：`web/src/types/dns.ts`、`web/src/types/certificates.ts`、`web/src/types/keyAssets.ts`

## API 范围

- DNS 域名：`GET/POST /api/v1/dns/domains`，`PUT/DELETE /api/v1/dns/domains/{id}`
- DNS 记录：`GET/POST /api/v1/dns/domains/{id}/records`，`PUT/DELETE /api/v1/dns/domains/{id}/records/{recordId}`
- 域名证书：`GET/POST /api/v1/certificates`，`DELETE /api/v1/certificates/{id}`
- 立即续签：`POST /api/v1/certificates/{id}/renew`
- Agent 系统内置证书由服务器模块提供：`GET /api/v1/key-assets/system`，重置使用 `POST /api/v1/key-assets/system/{id}/reset`
- 自签证书：`GET/POST /api/v1/self-signed-certificates`，`POST /api/v1/self-signed-cas`，`POST /api/v1/self-signed-certificates/{id}/renew`，`DELETE /api/v1/self-signed-certificates/{id}`
- 统一密钥资产：`GET /api/v1/key-assets`，`GET /api/v1/key-assets/{id}`，`POST /api/v1/key-assets/ca|tls|ssh/generate|import|exports`，`POST /api/v1/key-assets/imports/preflight`，`POST /api/v1/key-assets/imports/{planId}/execute`，`POST /api/v1/key-assets/{id}/reissue|regenerate`，`DELETE /api/v1/key-assets/{id}`，下载路径 `/api/v1/key-assets/{id}/files/{kind}` 与 `/api/v1/key-assets/exports/{taskId}/download`

## Cloudflare 认证

- Cloudflare provider 使用 REST API v4 和 Bearer API Token。
- 域名配置只接收域名、provider 和 API Token；Account ID 不属于业务配置。
- Token 至少需要目标 Zone 的读取权限以及 DNS 记录读写权限。
- provider 通过域名查询 Zone ID，不要求用户输入 Zone ID 或 Account ID。
- `dns_domains` 使用 `provider_config_json` 保存非敏感 provider 配置，使用 `provider_secret_ciphertext` 保存经 `secretstore` 加密的凭据 JSON。
- 新 schema 不包含 `api_token_secret`、`account_id` 等厂商专用列。
- 升级旧数据库时，启动流程在主密钥加载后加密旧 Token，并事务重建 `dns_domains` 表以删除旧列。
- 旧客户端提交的 `accountId` 会被 JSON 解码忽略。
- 新增或编辑域名时，后端先使用最终生效的 Token 和域名访问 Cloudflare，验证失败不得写入本地记录。

## DNS 行为

- DNS 记录不落本地数据库，实时读写 Cloudflare。
- 记录列表按 `result_info.total_pages` 读取全部分页。
- 记录名可以是 `@`、相对名称或 Zone 内完整名称，发送前统一规范化。
- Cloudflare 错误优先解析官方 envelope 中的错误码和消息。
- 切换域名时立即清空旧记录并展示加载状态，只接受当前域名请求的响应。
- 域名左侧选择行只负责切换域名；编辑和删除操作放在右侧已选域名详情标题区，不在选择行中显示更多菜单。
- 前端 DNS 记录表和证书列表在桌面端作为满高表格卡片展示；表格体独立滚动并吸收剩余高度，分页固定在卡片底部。域名/证书详情操作和记录行操作使用 `AppActionButton` / `AppActionGroup`；记录行编辑、删除使用带文字的小按钮，刷新和新增记录位于记录标题区。
- `GET /api/v1/dns/domains/{domainId}/records` 只读取 `dns_record_snapshots`，不得同步访问 DNS Provider。`POST .../records/refresh` 创建 `dns_records_refresh` 任务并异步替换快照；首次未刷新返回空数组。记录创建、更新、删除仍同步调用 Provider，成功后重建本地快照。

## 证书行为

- ACME 使用 DNS-01 challenge；challenge 创建后，无论后续步骤成功或失败都必须尝试清理。
- ACME 账户私钥持久化在数据目录中并跨签发复用；使用同一密钥注册时如果 ACME 服务端返回账户已存在，必须通过 `GetReg` 恢复现有账户后继续创建订单，不能把正常的账户复用当作签发失败。
- 证书签发按用户提交的前缀列表生成 SAN：`@` 表示根域名，普通前缀追加到托管域名前，`*` 或 `*.name` 只覆盖对应位置下一层标签；签发成功后保存证书路径、私钥路径、有效期和续签时间。
- 已签发的域名 HTTPS 证书通过应用侧内部文件 registry 注册为 `certificate:<cert-id>:certificate|private_key`，可被应用 `panel_file` 挂载；未签发证书不进入目录，也不能读取。
- 续签或重新签发成功后重新部署受影响应用并同步反向代理；普通应用刷新和反向代理协调必须分别尝试，某个应用刷新失败不能阻断入口网关证书更新尝试。失败时保留上一份可用文件。
- 签发、失败和续签记录任务日志；ACME 执行过程通过任务日志和步骤 metadata 展示账号、订单、DNS-01 challenge、授权等待、清理和 finalize 阶段；自动续期失败写入证书 `lastError`。
- 证书签发接口先持久化任务并返回 `taskId`，同时主动交给任务 manager 异步执行；后台 worker 只负责进程恢复和兜底唤醒，正常签发不得依赖轮询才开始运行。
- 自签证书页面只管理用户 CA/TLS。系统内置 CA/TLS 位于独立的“设置 → 系统证书”页面，使用左侧选择器和右侧详情，仅提供查看状态与允许的重置操作。
- v4 前端不再使用旧 `AppMasterDetailWorkspace`/`AppSelectorPanel` 组件名；使用 `ConsolePage` 与自有 primitives 组合主从工作台，并保持桌面内部滚动。
- 域名证书、自签证书和密钥资产必须通过路由子页呈现不同工作流，不能退回通用 `CollectionPage` 或同一参数化列表。
- 自签证书和密钥的新增/生成是页面主操作或详情区操作；导入资产、导出和批量导入预检必须明确展示任务/冲突反馈，真实 API 不存在的能力必须禁用并说明，不能用 Mock 伪装成功。
- 单个密钥资产导入使用 large Dialog：SSH 只编辑私钥材料，CA/TLS 通过私钥/证书 tab 切换并且只挂载当前 plain CodeMirror。tab 只属于本地展示状态，不进入导入 DTO；材料不会自动格式化或保存，导入失败必须在弹窗内可见并保留正文。

## 验证

- 后端 DNS 或证书行为改动运行 `task test:backend` 或 `task build:backend`。
- 前端页面、API 类型或交互改动运行 `task test:web` 或 `task build:web`。
- 同时修改前后端契约时验证两侧。

## 文档更新触发

新增 DNS provider、DNS 记录字段、认证参数、证书字段、ACME 行为、续签规则、证书变量或相关 API 时，必须更新本文档。
