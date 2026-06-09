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

- DNS 域名页面：`web/src/features/dns/pages/DomainsPage.vue`
- 证书页面：`web/src/features/certificates/pages/CertificatesPage.vue`
- 证书设置：`web/src/features/settings/pages/SettingsPage.vue`
- API：`web/src/api/dns.ts`、`web/src/api/certificates.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- DNS 域名：`GET/POST /api/v1/dns/domains`，`PUT/DELETE /api/v1/dns/domains/{id}`
- 证书：`GET/POST /api/v1/certificates`，`DELETE /api/v1/certificates/{id}`
- 证书默认值和 ACME 目录通过运行时设置读写：`GET/PUT /api/v1/settings/runtime`

## 数据与行为约定

- DNS 域名存储在 `dns_domains`，证书记录存储在 `certificates`。
- 当前 DNS provider 以 Cloudflare 为主，API token 和 account ID 属于敏感配置。
- ACME 签发会创建 DNS-01 challenge，等待 DNS 传播后完成签发。
- 通配符证书会展开需要的域名集合；签发成功后写入证书路径、私钥路径、有效期和续签时间。
- 证书可注册为应用内置变量，并被应用模块和 Nomad 反向代理读取。
- 签发、失败和续签应记录任务日志；第三方错误文本是否翻译需要按 i18n 指南评估。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `internal/dns`、`internal/certs`、`internal/scheduler`。
- 前端页面、设置或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 DNS provider、证书字段、ACME 行为、续签规则、证书变量、反向代理证书联动或相关 API 时，必须更新本文档。
