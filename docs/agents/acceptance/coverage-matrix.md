# 项目覆盖基线与变更检查矩阵

本文记录本规范在 2026-09-07 对当前仓库可观察入口的覆盖基线。它的用途是发现新增能力是否漏写验收合同，不是要求永久保持数量不变。

## 1. 覆盖守卫

- `COV-001`：新增、删除或改名任一 API、前端路由、后台任务类型、数据库表、运行时设置或构建产物时，必须在同一改动中更新本文件的数量/清单和对应领域验收项。
- `COV-002`：仅更新数量不算完成；新增入口必须有至少一个描述成功、失败与边界行为的稳定验收编号。
- `COV-003`：一个入口可由多个验收项共同覆盖；评审时必须能从入口定位到领域文档，不能依赖作者口头说明。
- `COV-004`：已存在但暂未在 UI 暴露的 API 或开发模式能力仍需服务端验收项，并在文档中标明可达条件。
- `COV-005`：历史兼容入口在移除前必须有迁移/拒绝策略；清单移除不代表可直接删除旧数据或远端资源。

## 2. 前端路由基线

`web/src/router/index.ts` 当前含 46 个 `path` 声明（包含父路由、重定向、开发条件路由和 catch-all）。逐操作标准见 `ui-pages.md`，全局守卫见 `ui-shell-and-conventions.md`。

| 页面族 | 路径/入口 |
| --- | --- |
| 公开入口 | `/login`、`/maintenance/backup` |
| 外壳与兜底 | `/`、`/overview`、shell 内 catch-all |
| 服务器 | `/servers`、`/credentials` |
| 资源与安全 | `/resources/packages`、`containers`、`images`、`networks`、`volumes`、`firewall`、开发模式 `fail2ban`，以及旧 `/security*` 重定向 |
| 应用 | `/applications/apps`、`create`、`:applicationId/edit`、`facility-apps`、`:facilityKind`、`:facilityKind/config` |
| DNS 与证书 | `/dns/domains`、`/certificates/domains`、`self-signed`、`keys` |
| 可观测性 | `/application-operations`、`/system-events`、`/tasks`、`/debug` |
| 设置 | `/settings/general`、`security`、`certificates`、`system-certificates`、`system`、`backups`，以及 `/settings` 重定向 |

- `COV-UI-001`：同一路由内新增或调整字段选择、校验、空态等用户可见行为时，路由数量可以不变，但必须同步更新 `ui-pages.md` 的稳定验收项和对应前端测试。

## 3. HTTP API 基线

当前主 Panel 注册 158 个 `/api` method/path 组合，路由清单 SHA-256 为 `af929361a5a5c059d1c51f8805c5a78ea30a83df028d0a2f818d27d9e422a741`。来源计数如下：

| 注册来源 | 数量 | 验收领域 |
| --- | ---: | --- |
| bootstrap auth | 5 | 身份、设置与系统 |
| applications | 28 | 应用与设施应用；协调、任务与运行事件 |
| backups | 3 | 备份、恢复与诊断 |
| certificates/certs | 11 | DNS、证书与密钥资产 |
| certificates/dns | 9 | DNS、证书与密钥资产 |
| containers | 17 | 容器与资源 |
| facilityapps | 19 | 应用与设施应用 |
| keyassets | 14 | DNS、证书与密钥资产 |
| diagnostics | 3 | 备份、恢复与诊断 |
| metrics | 1 | 身份、设置与系统；服务器、安全与软件包 |
| overview | 4 | 身份、设置与系统 |
| packages | 4 | 服务器、安全与软件包 |
| credentials | 5 | 服务器、安全与软件包 |
| servers | 23 | 服务器、安全与软件包 |
| settings | 5 | 身份、设置与系统 |
| systeminfo | 1 | 身份、设置与系统 |
| tasks | 6 | 协调、任务与运行事件 |

- `COV-API-001`：路由清单测试失败时必须先确定是哪一个 method/path 改变，再更新消费者、Mock、验收项和期望哈希；不得只替换哈希让测试通过。

- `COV-API-002`：维护导出/恢复的独立最小应用路由不计入上述主 Panel 158 条，但必须由备份恢复文档覆盖其认证、状态、密码、下载、重试、退出和清除 pending 操作。

- `COV-API-003`：158 个 method/path 的逐项映射见 [主 Panel API 路由逐项清单](api-route-inventory.md)；路由清单测试与该表必须同步变化。

## 4. 持久化基线

当前 ORM 模型有 43 个数据库内表声明；`application_revisions` 在 app 与 log 库分别存在，含义不同。coordination 库当前 0 个业务模型。

| 数据库 | 在管表 |
| --- | --- |
| app | `credentials`、`servers`、`package_updates`、`package_refreshes`、`fail2ban_configs`、`image_updates`、`image_refreshes`、`applications`、`application_reconcile_states`、`container_observations`、`docker_resource_snapshots`、`application_revisions`、`jobs`、`dns_domains`、`dns_record_snapshots`、`application_edit_sessions`、`application_edit_session_files`、`application_edit_session_operations`、`application_files`、`application_instances`、`facility_app_configs`、`facility_static_assets`、`reverse_proxy_routes`、`facility_edit_sessions`、`facility_edit_session_assets`、`facility_edit_session_operations`、`storage_share_configs`、`storage_share_partitions`、`certificates`、`self_signed_certificates`、`key_assets`、`overview_card_configurations`、`runtime_settings`、`auth_state`、`auth_accounts` |
| log | `tasks`、`task_steps`、`task_logs`、`application_revisions`、`runtime_events`、`runtime_event_details`、`key_asset_exports` |
| coordination | 无业务表 |
| metrics | `metrics_snapshots` |

- `COV-DATA-001`：新增表必须列入正确数据库；同名跨库表必须分别说明用途、备份和迁移，不得按表名误连。
- `COV-DATA-002`：删除表必须同时处理模型、迁移、索引、外键、服务查询、备份恢复和旧版本升级；仅从 `AllModels` 移除不构成安全删除。

## 5. 常驻与周期后台行为

| 后台行为 | 主要事实/输出 | 验收领域 |
| --- | --- | --- |
| 应用 orchestrator scanner/worker | app 库 Job、Instance desired/observed、revision、事件 | 协调、任务与运行事件 |
| 通用任务 worker | log 库 Task/step/log、远端副作用 | 协调、任务与运行事件及各触发域 |
| Task 清理 | 任务保留策略 | 协调、任务与运行事件 |
| metrics 清理 | metrics 快照保留策略 | 身份、设置与系统 |
| runtime event buffered writer/清理 | 运行事件及详情 | 协调、任务与运行事件 |
| Agent report collector | Server/Agent 状态、指标、资源观察、包状态、应用漂移 | 服务器、安全与软件包；容器与资源；协调 |
| 启动 Agent 检查 | 配置服务器的兼容与在线状态 | 服务器、安全与软件包 |
| systeminfo/update checker | 版本与更新状态 | 身份、设置与系统 |
| 证书续签、包/镜像/系统信息调度 | 由运行时设置产生 Task | 相应领域与协调、任务文档 |

- `COV-BG-001`：新增 goroutine、ticker、cron 或 queue consumer 必须说明启动顺序、停止等待、重复运行、失败重试、配置热更新和进程重启恢复。
- `COV-BG-002`：后台触发的远端写操作必须与等价手动操作共享服务端安全门和可追踪记录。

## 6. 构建与交付产物

| 产物 | 必须包含/支持 |
| --- | --- |
| Web bundle | 懒加载页面 chunk、指纹化静态资源、生产 API client |
| `panel` | HTTPS API、静态托管、全部主模块和后台服务 |
| `panel-init` | 容器启动/维护恢复所需初始化行为 |
| `panel-agent` amd64/arm64 | 同一 RPC contract 与版本元数据、Docker/系统能力 |
| linux/amd64 image | amd64 Panel/init + 两架构 Agent bundle + Web |
| linux/arm64 image | arm64 Panel/init + 两架构 Agent bundle + Web |
| GHCR manifest | main 正式多架构标签或唯一 dev 标签集合 |
| GitHub Release | 仅 main 正式版本，在镜像验证成功后创建 |

## 7. 自动化覆盖现状

仓库当前基线为 115 个 Go `*_test.go` 文件与 29 个 Web `*.test.ts` 文件。数量不是质量目标，但下降或大规模重命名时必须解释覆盖是否迁移。

- `COV-TEST-001`：关键安全门至少有自动测试：认证、路径安全、非托管资源保护、迁移原子性、lease fencing、秘密脱敏、恢复/导出密码流程。
- `COV-TEST-002`：关键 UI 状态至少覆盖：会话守卫、请求竞态、加载/错误/空态、确认弹窗、状态色语义、日期范围和编辑器输入。
- `COV-TEST-003`：本文件的静态数量只作为漏项信号，不允许通过增加空测试或合并路由来追求数量不变。
- `COV-TEST-004`：应用编辑器的嵌套代理 path 必须覆盖 Vue reactive 既有值回显所需的完整克隆与父草稿隔离；不得把 reactive Proxy 直接交给浏览器深拷贝 API。
