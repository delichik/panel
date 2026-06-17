# DNS 与证书模块

## 适用场景

修改 DNS 域名管理、Cloudflare 集成、ACME 签发、证书存储、自动续签、证书变量或应用与反向代理证书联动时，先读本文档。

## 关键入口

- DNS 服务与 handler：`internal/dns/`
- Cloudflare provider：`internal/dns/cloudflare.go`
- 证书服务与 handler：`internal/certs/`
- ACME 集成：`internal/certs/acme.go`
- 调度续签：`internal/scheduler/`
- 前端 DNS 页面：`web/src/views/dns/domains/index.vue`
- 域名证书页面：`web/src/views/certificates/domains/index.vue`
- API：`web/src/api/dns.ts`、`web/src/api/certificates.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- DNS 域名：`GET/POST /api/v1/dns/domains`，`PUT/DELETE /api/v1/dns/domains/{id}`
- DNS 记录：`GET/POST /api/v1/dns/domains/{id}/records`，`PUT/DELETE /api/v1/dns/domains/{id}/records/{recordId}`
- 域名证书：`GET/POST /api/v1/certificates`，`DELETE /api/v1/certificates/{id}`
- 立即续签：`POST /api/v1/certificates/{id}/renew`
- Agent 系统内置证书由服务器模块提供：`GET /api/v1/key-assets/system`，重置使用 `POST /api/v1/key-assets/system/{id}/reset`
- 统一密钥资产：`/api/v1/key-assets`

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
- 前端 DNS 记录表和证书列表在桌面端作为满高表格卡片展示；表格体独立滚动并吸收剩余高度，分页固定在卡片底部。

## 证书行为

- ACME 使用 DNS-01 challenge；challenge 创建后，无论后续步骤成功或失败都必须尝试清理。
- 通配符证书展开所需域名集合，签发成功后保存证书路径、私钥路径、有效期和续签时间。
- 续签或重新签发成功后重新部署受影响应用并同步反向代理；失败时保留上一份可用文件。
- 签发、失败和续签记录任务日志；ACME 执行过程通过任务日志和步骤 metadata 展示账号、订单、DNS-01 challenge、授权等待、清理和 finalize 阶段；自动续期失败写入证书 `lastError`。
- 证书签发接口返回 `taskId`，前端提供任务中心入口。

## 验证

- 后端 DNS 或证书行为改动运行 `task test:backend` 或 `task build:backend`。
- 前端页面、API 类型或交互改动运行 `task test:web` 或 `task build:web`。
- 同时修改前后端契约时验证两侧。

## 文档更新触发

新增 DNS provider、DNS 记录字段、认证参数、证书字段、ACME 行为、续签规则、证书变量或相关 API 时，必须更新本文档。
