# DNS 与证书模块

## 适用场景

修改 DNS 域名管理、Cloudflare 集成、ACME 签发、证书存储、自动续签、证书变量或应用/反向代理证书联动时，先读本文档。

## 后端入口

- DNS 服务与 handler：`internal/dns/`
- Cloudflare provider：`internal/dns/cloudflare.go`
- 证书服务与 handler：`internal/certs/`
- ACME 集成：`internal/certs/acme.go`
- 任务记录：`internal/tasks/`
- 调度续签：`internal/scheduler/`
- 应用变量和反向代理联动：`internal/applications/`、`internal/nomad/`
- 路由装配：`internal/app/app.go`

## 前端入口

- DNS 域名与记录页面：`web/src/features/dns/pages/DomainsPage.vue`
- 域名证书页面：`web/src/features/certificates/pages/CertificatesPage.vue`
- Nomad 内置证书：`web/src/features/certificates/pages/BuiltinCertificatesPage.vue`
- 密钥与证书：`web/src/features/certificates/pages/KeyAssetsPage.vue`
- 证书设置：`web/src/features/settings/pages/SettingsPage.vue`
- API：`web/src/api/dns.ts`、`web/src/api/certificates.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- DNS 域名：`GET/POST /api/v1/dns/domains`，`PUT/DELETE /api/v1/dns/domains/{id}`
- DNS 记录：`GET/POST /api/v1/dns/domains/{id}/records`，`PUT/DELETE /api/v1/dns/domains/{id}/records/{recordId}`；当前只支持 Cloudflare，记录不落本地库，实时读写 Cloudflare。
- 证书：`GET/POST /api/v1/certificates`，`DELETE /api/v1/certificates/{id}`
- 域名证书立即续签：`POST /api/v1/certificates/{id}/renew`
- Nomad 内置证书：`GET /api/v1/certificates/builtin`，`POST /api/v1/certificates/builtin/rotate`
- 统一密钥资产：`/api/v1/key-assets`；旧 `/api/v1/self-signed-certificates` 和 `/api/v1/self-signed-cas` 仅保留兼容入口
- 证书默认值和 ACME 目录通过运行时设置读写：`GET/PUT /api/v1/settings/runtime`

## 数据与行为约定

- DNS 域名存储在 `dns_domains`，证书记录存储在 `certificates`。
- 当前 DNS provider 以 Cloudflare 为主，API token 和 account ID 属于敏感配置。
- DNS 记录管理复用域名保存的 Cloudflare API token 和 account ID；前端在域名页使用左侧域名列表、右侧上方详情、右侧下方记录表的布局。
- Cloudflare provider 使用官方 REST API v4 和 Bearer API Token；记录列表必须按 `result_info.total_pages` 读取全部分页，不能假设单页包含 zone 的全部记录。
- 面板 API 可以接收 `@`、相对记录名或 zone 内完整记录名；发送到 Cloudflare 前统一规范化为 zone 内完整名称。Cloudflare 错误响应优先解析官方 envelope 中的错误码和消息。
- 新增或编辑 Cloudflare 域名时，后端必须先使用最终生效的 token、account ID 和域名访问 Cloudflare 记录接口验证权限与 zone 可见性；验证失败不得写入本地域名记录。
- ACME 签发会创建 DNS-01 challenge，等待 DNS 传播后完成签发；一旦 challenge 记录已创建，后续等待、授权或签发失败也必须尝试清理 DNS 记录。
- 通配符证书会展开需要的域名集合；签发成功后写入证书路径、私钥路径、有效期和续签时间。
- 证书可注册为应用内置变量，并被应用模块和 Nomad 反向代理读取。
- 新变量字段使用 `certificate_pem`、`private_key_pem` 蛇形名称；alpha 兼容期继续解析旧驼峰字段。
- 自签 CA 可以重复签发叶子证书；叶子证书支持 DNS/IP SAN，并保存证书、私钥和公钥。
- CA 有子证书时禁止删除；域名证书或自签证书被应用 `panel_file` 或反向代理使用时禁止删除。
- Nomad 内置证书不能删除，只能高风险重新生成。重新生成会轮换 CA/agent/Panel client 证书并自动重建托管 Nomad 集群。
- 域名续签和自签叶子重新签发成功后会重新部署受影响应用并同步反向代理；失败时保留上一份可用文件。
- 签发、失败和续签应记录任务日志；第三方错误文本是否翻译需要按 i18n 指南评估。
- 证书签发接口返回 `taskId`，前端签发成功后必须提供任务中心入口。
- 自动续期失败必须写入证书 `lastError`，并记录失败的 `certificate_renew` 任务；续期失败不应清除仍然可用的既有证书文件。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `internal/dns`、`internal/certs`、`internal/scheduler`。
- 前端页面、设置或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 DNS provider、DNS 记录字段、证书字段、ACME 行为、续签规则、证书变量、反向代理证书联动或相关 API 时，必须更新本文档。

## 密钥与证书统一资产

- 统一资产后端位于 `internal/keyassets/`，支持 `ca_certificate`、`tls_certificate`、`ssh_key_pair`；私钥由 `internal/secretstore/` 加密后写入 `key_assets`。
- 密钥与证书页面位于 `web/src/features/certificates/pages/KeyAssetsPage.vue`，证书一级菜单下包含内置证书、域名证书、密钥与证书三个二级菜单。
- API 使用 `/api/v1/key-assets`，包含 CA/TLS/SSH 生成或导入、TLS 重新签发、SSH 重新生成、文件下载、删除、加密批量导出和预检后批量导入。
- CA 可重复签发多个 TLS 子证书；有子证书的 CA、被应用 `panel_file` 或反向代理引用的资产禁止删除，API 返回准确的应用引用信息。
- TLS 重新签发、SSH 重新生成和批量导入成功后会重新部署已启用应用并同步反向代理；失败时保留上一份可用数据。
- 旧 `self_signed_certificates` 在启动时完整校验后事务迁移到 `key_assets`，提交成功才清理旧文件；旧自签 API 和 `certificate:` 挂载仅保留兼容读取。
- 批量导出文件使用用户密码加密，短期存放在 `tmp`；导入先执行冲突、父 CA 和使用中覆盖检查，再按用户策略执行。
