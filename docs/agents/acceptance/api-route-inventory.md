# 主 Panel API 路由逐项清单

本文把主 Panel 当前注册的每个 HTTP method/path 逐项映射到规范性领域文档。它解决“新增了入口但没有验收标准”的漏项检查；请求、成功、失败、并发和安全细节以映射文档的稳定编号为准。

- `API-COV-001`：清单中的每一行都必须在所属领域文档有可定位的功能/操作合同；路由存在不等于允许绕过通用认证、错误、幂等和安全合同。
- `API-COV-002`：增加、删除、更名或改 method 时，必须同步修改本清单、`coverage-matrix.md`、路由清单测试、typed client/Mock（如有）及所属领域验收项。
- `API-COV-003`：同一路径的不同 method 是不同操作，必须分别验证；动态参数名变化也视为合同变化。
- `API-COV-004`：本清单覆盖主 Panel 路由；备份导出/恢复最小维护应用的额外路由由备份恢复文档独立逐项约束。
- `API-COV-006`：本清单与 `COV-API-003` 的计数只覆盖 `/api/...` 路由，公开产物路由 `GET /agent/{version}/{platform}/panel-agent.gz` 与 `/agent/` 前缀的 404 兜底不在其中——这是有意为之，不是遗漏。它属于认证之外、由 CDN 缓存的产物面，验收合同见 [服务器、安全与软件包](servers-security-packages.md) 的 `AGT-DL-001..011`，路由清单断言见 `SRV-EVD-005` 与 `internal/bootstrap/panel/routes_manifest_test.go` 中的公开路由清单测试。
- `API-COV-005`：排序后清单的条目数与 SHA-256 以 `internal/bootstrap/panel/routes_manifest_test.go` 中 `TestAPIRouteManifest` 的 `wantCount` / `wantHash` 为准；当前主 Panel 基线为 **218** 条（本表同为 218 行，移除 5 条 `/fail2ban*` 路由后由 223 降为 218）。新增/删除/重命名路由后须在同一改动中重跑该测试并更新期望值。本文表格只作逐项映射，不得在此硬编码与测试不一致的哈希。
- `API-COV-007`：服务器连接代理模式（`SRV-PROXY-001..014`）不新增 method/path：探测仍是既有 `POST /api/v1/servers/probe`（表中序号 152），只是请求/响应增加 `proxyMode`、`tunnelServerId`、`socks5Address`、`socks5Username`、`socks5Password`、探测专用可选 `serverId`（保存接口忽略）与派生 `proxyInvalid`/`proxyError` 等字段；`GET/POST/PUT /servers` 同路径增加字段。因此本清单条目数与路由清单哈希保持不变，任何“为代理新增路由”的实现都属于偏离合同。

| # | Method | Path | 验收文档 |
| --- | --- | --- | --- |
| 1 | `DELETE` | `/api/v1/application-edit-sessions/{id}` | [应用与设施应用](applications-and-facilities.md) |
| 2 | `DELETE` | `/api/v1/application-edit-sessions/{id}/files/{name}` | [应用与设施应用](applications-and-facilities.md) |
| 3 | `DELETE` | `/api/v1/applications/{id}` | [应用与设施应用](applications-and-facilities.md) |
| 4 | `DELETE` | `/api/v1/certificates/{id}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 5 | `DELETE` | `/api/v1/credentials/{id}` | [服务器、安全与软件包](servers-security-packages.md) |
| 6 | `DELETE` | `/api/v1/dns/domains/{domainId}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 7 | `DELETE` | `/api/v1/dns/domains/{domainId}/records/{recordId}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 8 | `DELETE` | `/api/v1/facility-apps/reverse-proxy/config-records/{recordId}` | [应用与设施应用](applications-and-facilities.md) |
| 9 | `DELETE` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}` | [应用与设施应用](applications-and-facilities.md) |
| 10 | `DELETE` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetName}` | [应用与设施应用](applications-and-facilities.md) |
| 11 | `DELETE` | `/api/v1/facility-apps/storage-share` | [应用与设施应用](applications-and-facilities.md) |
| 12 | `DELETE` | `/api/v1/facility-apps/storage-share/partitions/{id}` | [应用与设施应用](applications-and-facilities.md) |
| 13 | `DELETE` | `/api/v1/key-assets/{id}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 14 | `DELETE` | `/api/v1/self-signed-certificates/{id}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 15 | `DELETE` | `/api/v1/servers/{id}` | [服务器、安全与软件包](servers-security-packages.md) `SRV-DEL-007`（含网络探测清理） |
| 16 | `DELETE` | `/api/v1/servers/{id}/ufw/rules/{number}` | [服务器、安全与软件包](servers-security-packages.md) |
| 17 | `DELETE` | `/api/v1/servers/{serverId}/containers/{resourceId}` | [容器与资源](containers-and-resources.md) |
| 18 | `DELETE` | `/api/v1/servers/{serverId}/images/{resourceId}` | [容器与资源](containers-and-resources.md) |
| 19 | `DELETE` | `/api/v1/servers/{serverId}/volumes/{resourceId}` | [容器与资源](containers-and-resources.md) |
| 20 | `DELETE` | `/api/v1/settings/appearance/background` | [身份、设置与系统](identity-settings-system.md) |
| 21 | `GET` | `/api/v1/application-edit-sessions/{id}/files/{name}` | [应用与设施应用](applications-and-facilities.md) |
| 22 | `GET` | `/api/v1/application-edit-sessions/{id}/files/{name}/content` | [应用与设施应用](applications-and-facilities.md) |
| 23 | `GET` | `/api/v1/applications` | [应用与设施应用](applications-and-facilities.md) |
| 24 | `GET` | `/api/v1/applications/template-catalog` | [应用与设施应用](applications-and-facilities.md) |
| 25 | `GET` | `/api/v1/applications/{id}` | [应用与设施应用](applications-and-facilities.md) |
| 26 | `GET` | `/api/v1/applications/{id}/files` | [应用与设施应用](applications-and-facilities.md) |
| 27 | `GET` | `/api/v1/applications/{id}/files/{name}/content` | [应用与设施应用](applications-and-facilities.md) |
| 28 | `GET` | `/api/v1/applications/{id}/logs` | [应用与设施应用](applications-and-facilities.md) |
| 29 | `GET` | `/api/v1/applications/{id}/persistent-data` | [应用与设施应用](applications-and-facilities.md) |
| 30 | `GET` | `/api/v1/applications/{id}/runtime` | [应用与设施应用](applications-and-facilities.md) |
| 31 | `GET` | `/api/v1/auth/session` | [身份、设置与系统](identity-settings-system.md) |
| 32 | `GET` | `/api/v1/certificates` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 33 | `GET` | `/api/v1/certificates/{id}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 34 | `GET` | `/api/v1/credentials` | [服务器、安全与软件包](servers-security-packages.md) |
| 35 | `GET` | `/api/v1/credentials/{id}` | [服务器、安全与软件包](servers-security-packages.md) |
| 36 | `GET` | `/api/v1/debug/clear-runtime-data` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 37 | `GET` | `/api/v1/debug/databases` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 38 | `GET` | `/api/v1/debug/pprof` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 39 | `GET` | `/api/v1/debug/runtime` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 40 | `GET` | `/api/v1/debug/tasks` | [备份、恢复与诊断](backup-restore-diagnostics.md) `DIAG-SNP-003`（event executor 快照；路径名保留） |
| 41 | `GET` | `/api/v1/dns/domains` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 42 | `GET` | `/api/v1/dns/domains/{domainId}/records` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 43 | `GET` | `/api/v1/events` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`；AppDB `events` 进程行（state/module/level），筛选 origin/operationId/executionId/module/from/to/hadError |
| 44 | `POST` | `/api/v1/events/clear` | [工程质量与发布](engineering-release.md) `ENG-EVT-002`；确认后清空 AppDB `events` + JSONL |
| 45 | `GET` | `/api/v1/events/export` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`；NDJSON 导出 |
| 46 | `GET` | `/api/v1/events/{eventId}` | [工程质量与发布](engineering-release.md) `ENG-EVT-001` |
| 47 | `GET` | `/api/v1/events/{eventId}/logs` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`；JSONL 行（level/eventId/msg，无 state） |
| 48 | `GET` | `/api/v1/events/{eventId}/lines` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`；与 `{eventId}/logs` 等价 |
| 49 | `GET` | `/api/v1/logs` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`；`?eventId=` 按需读 JSONL 行 |
| 50 | `GET` | `/api/v1/executions` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 51 | `GET` | `/api/v1/executions/{id}` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 52 | `GET` | `/api/v1/executions/{id}/logs` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 53 | `GET` | `/api/v1/executions/{id}/steps` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 54 | `GET` | `/api/v1/facility-apps/metrics` | [应用与设施应用](applications-and-facilities.md) `FAC-MET-001/003`；读取指标设施配置 |
| 55 | `GET` | `/api/v1/facility-apps/network-probe` | [应用与设施应用](applications-and-facilities.md) `FAC-PRB-001/003`；网络探测设施配置 |
| 56 | `GET` | `/api/v1/facility-apps/network-probe/results` | [应用与设施应用](applications-and-facilities.md) `FAC-PRB-006`；默认探测矩阵（可选 `serverIds` 过滤）；`?view=targets&history=1` 返回按目标聚合的全部服务器摘要（`targets=1`/`targets=true` 兼容别名） |
| 57 | `GET` | `/api/v1/facility-apps/reverse-proxy` | [应用与设施应用](applications-and-facilities.md) |
| 58 | `GET` | `/api/v1/facility-apps/reverse-proxy/config-records` | [应用与设施应用](applications-and-facilities.md) |
| 59 | `GET` | `/api/v1/facility-apps/reverse-proxy/config-records/{recordId}` | [应用与设施应用](applications-and-facilities.md) |
| 60 | `GET` | `/api/v1/facility-apps/reverse-proxy/diagnostics` | [应用与设施应用](applications-and-facilities.md) |
| 61 | `GET` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetName}/content` | [应用与设施应用](applications-and-facilities.md) |
| 62 | `GET` | `/api/v1/facility-apps/reverse-proxy/proxy-plane` | [应用与设施应用](applications-and-facilities.md) |
| 63 | `GET` | `/api/v1/facility-apps/reverse-proxy/static-assets/{assetName}/content` | [应用与设施应用](applications-and-facilities.md) |
| 64 | `GET` | `/api/v1/facility-apps/storage-share` | [应用与设施应用](applications-and-facilities.md) |
| 65 | `GET` | `/api/v1/facility-apps/storage-share/partitions/{id}/download` | [应用与设施应用](applications-and-facilities.md) |
| 66 | `GET` | `/api/v1/facility-apps/storage-share/status` | [应用与设施应用](applications-and-facilities.md) |
| 67 | `GET` | `/api/v1/key-assets` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 68 | `GET` | `/api/v1/key-assets/system` | [服务器、安全与软件包](servers-security-packages.md) |
| 69 | `GET` | `/api/v1/key-assets/tls` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 70 | `GET` | `/api/v1/key-assets/{downloadPath...}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 71 | `GET` | `/api/v1/key-assets/{id}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 72 | `GET` | `/api/v1/log-contexts` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`（legacy 别名，等同 `/events`） |
| 73 | `GET` | `/api/v1/log-contexts/export` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`（legacy 别名） |
| 74 | `GET` | `/api/v1/log-contexts/{streamId}` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`（legacy：`streamId` 作 eventId） |
| 75 | `GET` | `/api/v1/log-contexts/{streamId}/lines` | [工程质量与发布](engineering-release.md) `ENG-EVT-001`（legacy 别名） |
| 76 | `GET` | `/api/v1/notifications` | [工程质量与发布](engineering-release.md) `ENG-EVT-011`；事件通知长轮询（认证；`after`/`wait` 参数、基线不回放、`module=http` 不进入、内存 Hub 不读写 DB） |
| 77 | `GET` | `/api/v1/overview` | [身份、设置与系统](identity-settings-system.md) |
| 78 | `GET` | `/api/v1/overview/cards` | [身份、设置与系统](identity-settings-system.md) |
| 79 | `GET` | `/api/v1/overview/cards/{cardId}/data` | [身份、设置与系统](identity-settings-system.md)；`OBS-OVW-005/006`；`disk` 卡返回 `diskCurrent`、`diskIO` 卡返回 `diskIo`、`networkProbe` 卡返回 `probes` |
| 80 | `GET` | `/api/v1/self-signed-certificates` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 81 | `GET` | `/api/v1/servers` | [服务器、安全与软件包](servers-security-packages.md) |
| 82 | `GET` | `/api/v1/servers/{id}` | [服务器、安全与软件包](servers-security-packages.md) |
| 84 | `GET` | `/api/v1/servers/{id}/metrics` | [服务器、安全与软件包](servers-security-packages.md)；`OBS-MET-003/004/007/008`（`diskIo`/`diskCurrent`，无 `disk`；可选 `bucket` 桶聚合并返回 `latest` 当前样本） |
| 85 | `GET` | `/api/v1/servers/{id}/network-probes` | [服务器、安全与软件包](servers-security-packages.md)；[应用与设施应用](applications-and-facilities.md) `FAC-PRB-006/009`；单节点探测页签；`?history=1` 时附带 24h 历史 |
| 86 | `GET` | `/api/v1/servers/{id}/packages/updates` | [服务器、安全与软件包](servers-security-packages.md) |
| 87 | `GET` | `/api/v1/servers/{id}/ufw` | [服务器、安全与软件包](servers-security-packages.md) |
| 88 | `GET` | `/api/v1/servers/{serverId}/containers` | [容器与资源](containers-and-resources.md) |
| 89 | `GET` | `/api/v1/servers/{serverId}/containers/{resourceId}/logs` | [容器与资源](containers-and-resources.md) |
| 90 | `GET` | `/api/v1/servers/{serverId}/images` | [容器与资源](containers-and-resources.md) |
| 91 | `GET` | `/api/v1/servers/{serverId}/networks` | [容器与资源](containers-and-resources.md) |
| 92 | `GET` | `/api/v1/servers/{serverId}/volumes` | [容器与资源](containers-and-resources.md) |
| 93 | `GET` | `/api/v1/settings/appearance` | [身份、设置与系统](identity-settings-system.md) |
| 94 | `GET` | `/api/v1/settings/appearance/background/content` | [身份、设置与系统](identity-settings-system.md) |
| 95 | `GET` | `/api/v1/settings/public-appearance` | [身份、设置与系统](identity-settings-system.md) |
| 96 | `GET` | `/api/v1/settings/public-branding` | [身份、设置与系统](identity-settings-system.md) |
| 97 | `GET` | `/api/v1/settings/runtime` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-001/022` |
| 98 | `GET` | `/api/v1/settings/server-variables` | [身份、设置与系统](identity-settings-system.md) |
| 99 | `GET` | `/api/v1/system/version` | [身份、设置与系统](identity-settings-system.md) |
| 100 | `PATCH` | `/api/v1/application-edit-sessions/{id}/draft` | [应用与设施应用](applications-and-facilities.md) |
| 101 | `PATCH` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/draft` | [应用与设施应用](applications-and-facilities.md) |
| 102 | `POST` | `/api/v1/application-edit-sessions` | [应用与设施应用](applications-and-facilities.md) |
| 103 | `POST` | `/api/v1/application-edit-sessions/{id}/archives` | [应用与设施应用](applications-and-facilities.md) |
| 104 | `POST` | `/api/v1/application-edit-sessions/{id}/commit` | [应用与设施应用](applications-and-facilities.md) |
| 105 | `POST` | `/api/v1/application-edit-sessions/{id}/preview` | [应用与设施应用](applications-and-facilities.md) |
| 106 | `POST` | `/api/v1/application-edit-sessions/{id}/validate` | [应用与设施应用](applications-and-facilities.md) |
| 107 | `POST` | `/api/v1/applications/{id}/deploy` | [应用与设施应用](applications-and-facilities.md) |
| 108 | `POST` | `/api/v1/applications/{id}/image/update` | [应用与设施应用](applications-and-facilities.md) |
| 109 | `POST` | `/api/v1/applications/{id}/persistent-data` | [应用与设施应用](applications-and-facilities.md) |
| 110 | `POST` | `/api/v1/applications/{id}/restart` | [应用与设施应用](applications-and-facilities.md) |
| 111 | `POST` | `/api/v1/applications/{id}/stop` | [应用与设施应用](applications-and-facilities.md) |
| 112 | `POST` | `/api/v1/auth/account` | [身份、设置与系统](identity-settings-system.md) |
| 113 | `POST` | `/api/v1/auth/jwt-secret` | [身份、设置与系统](identity-settings-system.md) |
| 114 | `POST` | `/api/v1/auth/login` | [身份、设置与系统](identity-settings-system.md) |
| 115 | `POST` | `/api/v1/auth/logout` | [身份、设置与系统](identity-settings-system.md) |
| 116 | `POST` | `/api/v1/backups/export` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 117 | `POST` | `/api/v1/backups/restore/confirm` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 118 | `POST` | `/api/v1/backups/restore/preflight` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 119 | `POST` | `/api/v1/certificates` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 120 | `POST` | `/api/v1/certificates/{id}/renew` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 121 | `POST` | `/api/v1/credentials` | [服务器、安全与软件包](servers-security-packages.md) |
| 122 | `POST` | `/api/v1/debug/clear-runtime-data` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 123 | `POST` | `/api/v1/dns/domains` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 124 | `POST` | `/api/v1/dns/domains/{domainId}/records` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 125 | `POST` | `/api/v1/dns/domains/{domainId}/records/refresh` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 126 | `POST` | `/api/v1/executions/{id}/resolve` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 127 | `POST` | `/api/v1/executions/{id}/retry` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 128 | `POST` | `/api/v1/executions/{id}/run-now` | [协调、任务与运行事件](orchestration-and-tasks.md) |
| 129 | `POST` | `/api/v1/facility-apps/reverse-proxy/config-records/{recordId}/apply` | [应用与设施应用](applications-and-facilities.md) |
| 130 | `POST` | `/api/v1/facility-apps/reverse-proxy/dns-sync` | [应用与设施应用](applications-and-facilities.md) |
| 131 | `POST` | `/api/v1/facility-apps/reverse-proxy/edit-sessions` | [应用与设施应用](applications-and-facilities.md) |
| 132 | `POST` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/commit` | [应用与设施应用](applications-and-facilities.md) |
| 133 | `POST` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/preview` | [应用与设施应用](applications-and-facilities.md) |
| 134 | `POST` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/validate` | [应用与设施应用](applications-and-facilities.md) |
| 135 | `POST` | `/api/v1/facility-apps/reverse-proxy/reconcile` | [应用与设施应用](applications-and-facilities.md) |
| 136 | `POST` | `/api/v1/facility-apps/storage-share/reconcile` | [应用与设施应用](applications-and-facilities.md) |
| 137 | `POST` | `/api/v1/images/upgrade-all` | [容器与资源](containers-and-resources.md) |
| 138 | `POST` | `/api/v1/images/upgrade-selected` | [容器与资源](containers-and-resources.md) |
| 139 | `POST` | `/api/v1/key-assets/ca` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 140 | `POST` | `/api/v1/key-assets/exports` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 141 | `POST` | `/api/v1/key-assets/import` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 142 | `POST` | `/api/v1/key-assets/imports/preflight` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 143 | `POST` | `/api/v1/key-assets/imports/{planId}/execute` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 144 | `POST` | `/api/v1/key-assets/ssh/generate` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 145 | `POST` | `/api/v1/key-assets/system/{id}/reset` | [服务器、安全与软件包](servers-security-packages.md) |
| 146 | `POST` | `/api/v1/key-assets/tls` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 147 | `POST` | `/api/v1/key-assets/{id}/regenerate` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 148 | `POST` | `/api/v1/key-assets/{id}/reissue` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 149 | `POST` | `/api/v1/self-signed-cas` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 150 | `POST` | `/api/v1/self-signed-certificates` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 151 | `POST` | `/api/v1/self-signed-certificates/{id}/renew` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 152 | `POST` | `/api/v1/servers` | [服务器、安全与软件包](servers-security-packages.md) |
| 153 | `POST` | `/api/v1/servers/probe` | [服务器、安全与软件包](servers-security-packages.md) |
| 154 | `POST` | `/api/v1/servers/{id}/agent/certificate` | [服务器、安全与软件包](servers-security-packages.md) |
| 155 | `POST` | `/api/v1/servers/{id}/agent/deploy` | [服务器、安全与软件包](servers-security-packages.md) |
| 159 | `POST` | `/api/v1/servers/{id}/packages/refresh` | [服务器、安全与软件包](servers-security-packages.md) |
| 160 | `POST` | `/api/v1/servers/{id}/packages/upgrade-all` | [服务器、安全与软件包](servers-security-packages.md) |
| 161 | `POST` | `/api/v1/servers/{id}/packages/upgrade-selected` | [服务器、安全与软件包](servers-security-packages.md) |
| 162 | `POST` | `/api/v1/servers/{id}/restart` | [服务器、安全与软件包](servers-security-packages.md) |
| 163 | `POST` | `/api/v1/servers/{id}/test` | [服务器、安全与软件包](servers-security-packages.md) |
| 164 | `POST` | `/api/v1/servers/{id}/trust-host-key` | [服务器、安全与软件包](servers-security-packages.md) |
| 165 | `POST` | `/api/v1/servers/{id}/ufw/rules` | [服务器、安全与软件包](servers-security-packages.md) |
| 166 | `POST` | `/api/v1/servers/{serverId}/containers/{resourceId}/{action}` | [容器与资源](containers-and-resources.md) |
| 167 | `POST` | `/api/v1/servers/{serverId}/images/delete-unused` | [容器与资源](containers-and-resources.md) |
| 168 | `POST` | `/api/v1/servers/{serverId}/images/pull` | [容器与资源](containers-and-resources.md) |
| 169 | `POST` | `/api/v1/servers/{serverId}/images/refresh` | [容器与资源](containers-and-resources.md) |
| 170 | `POST` | `/api/v1/servers/{serverId}/networks/refresh` | [容器与资源](containers-and-resources.md) |
| 171 | `POST` | `/api/v1/servers/{serverId}/volumes/delete-unused` | [容器与资源](containers-and-resources.md) |
| 172 | `POST` | `/api/v1/servers/{serverId}/volumes/refresh` | [容器与资源](containers-and-resources.md) |
| 173 | `POST` | `/api/v1/settings/appearance/background` | [身份、设置与系统](identity-settings-system.md) |
| 174 | `PUT` | `/api/v1/application-edit-sessions/{id}/files/{name}` | [应用与设施应用](applications-and-facilities.md) |
| 175 | `PUT` | `/api/v1/application-edit-sessions/{id}/uploads/{name}` | [应用与设施应用](applications-and-facilities.md) |
| 176 | `PUT` | `/api/v1/certificates/{id}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 177 | `PUT` | `/api/v1/credentials/{id}` | [服务器、安全与软件包](servers-security-packages.md) |
| 178 | `PUT` | `/api/v1/debug/pprof` | [备份、恢复与诊断](backup-restore-diagnostics.md) |
| 179 | `PUT` | `/api/v1/dns/domains/{domainId}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 180 | `PUT` | `/api/v1/dns/domains/{domainId}/records/{recordId}` | [DNS、证书与密钥资产](dns-certificates-key-assets.md) |
| 181 | `PUT` | `/api/v1/facility-apps/metrics` | [应用与设施应用](applications-and-facilities.md) `FAC-MET-003`；保存指标设施配置 |
| 182 | `PUT` | `/api/v1/facility-apps/network-probe` | [应用与设施应用](applications-and-facilities.md) `FAC-PRB-003`；保存网络探测配置 |
| 183 | `PUT` | `/api/v1/facility-apps/reverse-proxy/edit-sessions/{id}/assets/{assetName}` | [应用与设施应用](applications-and-facilities.md) |
| 184 | `PUT` | `/api/v1/facility-apps/reverse-proxy/proxy-plane` | [应用与设施应用](applications-and-facilities.md) |
| 185 | `PUT` | `/api/v1/facility-apps/storage-share` | [应用与设施应用](applications-and-facilities.md) |
| 186 | `PUT` | `/api/v1/overview/cards` | [身份、设置与系统](identity-settings-system.md) |
| 187 | `PUT` | `/api/v1/servers/{id}` | [服务器、安全与软件包](servers-security-packages.md) |
| 189 | `PUT` | `/api/v1/settings/appearance` | [身份、设置与系统](identity-settings-system.md) |
| 190 | `PUT` | `/api/v1/settings/runtime/agent` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-022` |
| 191 | `PUT` | `/api/v1/settings/runtime/branding` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-022` |
| 192 | `PUT` | `/api/v1/settings/runtime/certificates` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-022` |
| 193 | `PUT` | `/api/v1/settings/runtime/general` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-022` |
| 194 | `PUT` | `/api/v1/settings/runtime/security` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-022` |
| 195 | `PUT` | `/api/v1/settings/server-variables` | [身份、设置与系统](identity-settings-system.md) |
| 196 | `POST` | `/api/v1/facility-apps/reverse-proxy/image/check` | [应用与设施应用](applications-and-facilities.md) `FAC-RP-014` |
| 197 | `POST` | `/api/v1/facility-apps/reverse-proxy/image/update` | [应用与设施应用](applications-and-facilities.md) `FAC-RP-014` |
| 198 | `PUT` | `/api/v1/facility-apps/reverse-proxy/settings` | [应用与设施应用](applications-and-facilities.md) `FAC-RP-015` |
| 199 | `PUT` | `/api/v1/settings/runtime/auto-update` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-023` |
| 200 | `POST` | `/api/v1/facility-apps/reverse-proxy/image/rollback` | [应用与设施应用](applications-and-facilities.md) `FAC-RP-014` |
| 201 | `POST` | `/api/v1/facility-apps/reverse-proxy/image/resume` | [应用与设施应用](applications-and-facilities.md) `FAC-RP-014` |
| 202 | `POST` | `/api/v1/settings/runtime/auto-update/run` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-023` |
| 203 | `PUT` | `/api/v1/settings/runtime/image-registry` | [身份、设置与系统](identity-settings-system.md) `SET-RUN-024` |
| 204 | `GET` | `/api/v1/setup/status` | [身份、设置与系统](identity-settings-system.md) `ID-SETUP-001` |
| 205 | `POST` | `/api/v1/setup` | [身份、设置与系统](identity-settings-system.md) `ID-SETUP-002` |
| 206 | `GET` | `/api/v1/permissions` | [身份、设置与系统](identity-settings-system.md) `ID-PERM-001` |
| 207 | `GET` | `/api/v1/users` | [身份、设置与系统](identity-settings-system.md) `ID-USR-001` |
| 208 | `POST` | `/api/v1/users` | [身份、设置与系统](identity-settings-system.md) `ID-USR-002` |
| 209 | `GET` | `/api/v1/users/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-USR-003` |
| 210 | `PATCH` | `/api/v1/users/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-USR-004` |
| 211 | `DELETE` | `/api/v1/users/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-USR-005` |
| 212 | `POST` | `/api/v1/users/{id}/password` | [身份、设置与系统](identity-settings-system.md) `ID-USR-006` |
| 213 | `POST` | `/api/v1/users/{id}/logout` | [身份、设置与系统](identity-settings-system.md) `ID-USR-007` |
| 214 | `GET` | `/api/v1/roles` | [身份、设置与系统](identity-settings-system.md) `ID-ROLE-001` |
| 215 | `POST` | `/api/v1/roles` | [身份、设置与系统](identity-settings-system.md) `ID-ROLE-002` |
| 216 | `GET` | `/api/v1/roles/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-ROLE-003` |
| 217 | `PATCH` | `/api/v1/roles/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-ROLE-004` |
| 218 | `DELETE` | `/api/v1/roles/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-ROLE-005` |
| 219 | `GET` | `/api/v1/access-credentials` | [身份、设置与系统](identity-settings-system.md) `ID-CRED-001` |
| 220 | `POST` | `/api/v1/access-credentials` | [身份、设置与系统](identity-settings-system.md) `ID-CRED-002` |
| 221 | `DELETE` | `/api/v1/access-credentials/{id}` | [身份、设置与系统](identity-settings-system.md) `ID-CRED-003` |

| 222 | `GET` | `/api/v1/servers/{id}/ssh-access/sessions` | [服务器、安全与软件包](servers-security-packages.md) `SRV-SESS-004` |
| 223 | `GET` | `/api/v1/servers/{id}/ssh-access/attempts` | [服务器、安全与软件包](servers-security-packages.md) `SRV-SESS-004` |