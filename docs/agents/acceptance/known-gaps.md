# 已知实现与验收差距

本文件记录编制验收规范时确认的现状缺口。它不是放宽标准的清单：对应代码被触及时，应修复缺口并增加证据，或经明确产品决策修改验收合同。关闭条目时保留编号、记录关闭依据，不复用编号。

| 编号 | 范围 | 当前差距与风险 | 关闭条件 |
| --- | --- | --- | --- |
| GAP-001 | 前端自动化 | 页面级 Playwright E2E、axe、多断点视觉/滚动回归未系统恢复；URL 恢复、脏状态离开、两阶段任务、上传下载、维护 token 隔离主要依赖人工检查 | 为关键页面族补浏览器级证据并关联 UI 验收编号 |
| GAP-002 | URL 状态 | ~~servers、applications、dns 等页面的页码/选择没有完整从 URL 初始化并双向同步~~（已关闭） | 表格化一轮已修复：servers/applications/dns 的 `page`/`search`/选中与服务器详情 `tab` 均从 URL 初始化并双向同步，深链与前进/后退可恢复（`UI-SHELL-035/059/062`、`UI-SRV-001`、`UI-APP-001`、`UI-DNS-001`）；页面级 e2e 证据仍归入 GAP-001 |
| GAP-003 | 应用编辑返回 | ~~编辑器 footer 取消使用 `router.back()`~~（已关闭） | 取消/返回明确 `push` 到 `/applications/apps`（编辑时带 `application` 选中）；与页头 `@back` 一致，不依赖历史栈（`UI-APP-023`） |
| GAP-004 | 页面错误态 | ~~应用详情/runtime/files 加载失败主要用 toast~~（已关闭） | 详情加载失败设 `detailError` + toast + EmptyState 重试；runtime 失败设 `runtimeError` + toast + Runtime tab 内 EmptyState 重试（`UI-APP-002/003`）；页面级 e2e 仍归入 GAP-001 |
| GAP-005 | 证书页面入口 | ~~domains/self-signed 模式可能显示“生成 SSH”入口~~（已关闭） | 表格化一轮已修复：生成 SSH/CA/TLS 入口只出现在 keys 模式页头（`UI-CERT-009`），列表结构测试可覆盖 |
| GAP-006 | 危险确认 | ~~settings 导出/恢复/重置确认仍为手写 Dialog~~（已关闭） | settings 已迁移到统一 `ConfirmDialog`（impact、危险项 `requireCheckbox`、loading 锁）；各主列表页此前已统一；新增危险动作不得再手写 |
| GAP-007 | ~~前端/后端证书类型~~（已关闭） | ~~`web/src/types/certificates.ts` 仍声明后端明确不输出的 `certificatePath/privateKeyPath`~~ | 前端类型与 mocks 已删除路径字段；后端 DTO 保持 `json:"-"`，不向客户端暴露私钥路径 |
| GAP-008 | ~~密钥资产详情类型~~（已关闭） | ~~前端要求 references/referenceCount，后端 detail DTO 未输出~~ | `assetSummaryDTO`/`detail` 输出 `referenceCount` 与 `references`；列表与详情合同对齐，删除门禁仍由 `canDelete`/`InUse` 驱动 |
| GAP-009 | ~~系统版本类型~~（已关闭） | ~~后端可能返回 `checkError`，前端 `VersionInfo` 未声明~~ | typed client 声明 `checkError`；设置页系统段安全展示检查失败 |
| GAP-010 | 备份旧文档引用 | 旧指引引用已不存在的 `web/src/api/backups.ts` 与 `web/src/types/api.ts` | 旧指引由本规范替代；后续引用只指向实际 maintenance/settings API 与类型 |
| GAP-011 | Agent report 频率说明 | 旧指引写每秒检查，当前实现为每 5 秒 | 本规范以当前实现/运行时设置为准；增加频率行为测试或统一实现 |
| GAP-012 | ~~UFW Agent 准入~~（已消除） | `InstallUFW` 曾绕过 `ensureUFWManageable` 并强制本地 privilege。手动安装入口已删除，防火墙改为 Agent 部署的自动前提（`AGT-FW-001..006`），该不一致随之消失 | 关闭；保留编号以便追溯 |
| GAP-013 | ~~账户更新原子性~~（已关闭） | ~~账户、JWT secret、nonce 通过多次写入更新，缺少跨写入事务，失败可能部分成功~~ | `UpdateAccount` / `UpdateJWTSecret` 已在同一 AppDB 事务提交账户行、JWT secret 与 nonce，成功后再更新内存快照；见 `ID-ACC-011`、`ID-SES-006` 与 identity 服务测试 |
| GAP-014 | 未来概览 API | 旧材料出现 `/overview/dashboard` 与 `baseVersion`，当前路由/服务未实现 | 保持不在当前合同；若实现，先新增 API/并发合同和前端消费测试 |
| GAP-015 | 应用迁移入口 | service 有迁移行为门禁，但主 applications 路由无公开迁移 HTTP 入口 | 保持不可达，或先定义权限、请求、任务、回滚与 UI 合同后新增入口 |
| GAP-016 | 设施编辑增强 | heartbeat、全局草稿配额、warning 确认、专用结构化恢复日志暂缓 | 不得假设存在；分别完成持久化/并发/API/UI 验收后启用 |
| GAP-017 | 旧 lifecycle 类型 | 应用 runtime/前端类型仍带 lifecycle 命名，但旧 CoordDB lifecycle 表和 dispatcher 已删除 | 逐步移除兼容命名；任何期间均不得恢复第二事实源 |
| GAP-018 | 容器完整详情 | 无通用 inspect API，report 摘要不含 command/created/mounts，但前端资源类型仍残留完整字段 | 清理旧字段，或新增独立受限 inspect 合同与按需 API，不扩大列表上报 |
| GAP-019 | ~~容器测试命名~~（已关闭） | ~~`TestContainerActionRejectsManagedApplicationContainer` 名称与断言相反~~ | 已重命名为 `TestContainerActionAllowsManagedApplicationContainer`，断言仍为允许直接动作并由协调修复 |
| GAP-020 | ~~Docker 操作物理队列~~（已关闭） | ~~普通容器、orchestrator、设施应用由不同所有者组合，缺少证明所有写路径共享同一 per-server 物理队列的集成测试~~ | 应用 `RuntimeReconcile`/legacy apply·stop 与设施存储 export 配置均经 `containers.Service.Execute` 同物理 per-server 队列；Agent 侧 Docker 写 RPC（含 persistent archive/restore）另有进程内互斥；入队后取消须等待 job 结束。验证：`TestRuntimeReconcileUsesSharedContainerOperationQueue`、`TestStorageExportConfigureUsesSharedOperationQueue`、`TestExecuteWaitsForAcceptedJobAfterCallerCancel`、`TestExecuteSerializesOperationsPerServer`、`RES-QUEUE-001/004` |
| GAP-021 | 服务器资源 tabs 回归 | 首次空快照、请求竞态、桌面/窄屏滚动与 tab 切换缺少浏览器级 E2E（原资源页缺口并入服务器详情后仍存在） | 补服务器资源 tabs 的浏览器测试与三档桌面/一档窄屏布局证据，关联 `UI-SRV-017/018` |
| GAP-022 | Docker network 写操作 | network 当前只读，尚无系统网络保护和实时使用门禁合同 | 继续只读；若新增 mutation，先定义 `panel-apps` 保护、确认、队列和使用中拒绝 |
| GAP-023 | 多实例任务调度 | 通用任务队首缓存只适用于单 Panel 进程，共享 SQLite 多实例下没有 claim/fencing 一致性 | 保持单实例部署边界，或先实现数据库级 claim/lease/fencing |
| GAP-024 | Runtime event 丢弃观测 | 满缓冲丢弃计数/告警与批量失败重试没有稳定对外合同 | 事件仍非业务事实；新增指标/告警/重试合同和测试后关闭 |
| GAP-025 | 操作/任务/日志刷新 | `/operations`、`/tasks`、`/logs` 均保持手动刷新（页头 Refresh 带 loading）；不做隐式轮询或 summary/tail 探测。页面级窄屏/a11y 证据仍不足 | 补页面级验证后关闭；若产品改为可见自动刷新须先改合同 |
| GAP-026 | 无效详情保留字段 | `runtimeEventDetailRetentionDays` 与 `runtime_event_details` 属兼容遗留且当前无有效业务意义 | 在可迁移版本中删除，或先定义真实写入/读取/清理合同，禁止半复活 |
| GAP-027 | 复杂表单本地验证 | DNS 域名/记录与证书签发/自签/密钥资产表单已补轻量本地校验（必填与基本域名形状、字段错误、无效时禁用保存）；applications 编辑器与 settings 数值校验仍有更多输入依赖服务端 | 继续为剩余高频/安全关键输入增加本地阻止，同时完整保留服务端结构化错误 |
| GAP-028 | 启动灾难兜底语言 | i18n 初始化失败前的启动兜底只有英文 | 保持仅灾难兜底例外；不得复制到正常页面，有可行初始化方案时补多语言 |
| GAP-029 | 失联节点的应用清理 | 服务器被删除时其应用实例记录随外键级联清除，节点已不存在时该节点上的容器与持久数据无法再被回收；协调 Job 行也随删除一并清除，部署历史只保留在活动日志中 | 明确"删除失联节点等于放弃该节点上的运行态与数据"的产品语义，并让受影响应用自动停用、回到可重新选定部署目标的状态（`SRV-DEL-006/007/009`）；若要求可回收的离线节点清理，需先定义离线节点的回收通道 |

## 缺口处理规则

- `GAP-RULE-001`：任何变更若扩大某缺口的影响面、删除现有保护或让临时兼容成为新依赖，验收失败。
- `GAP-RULE-002`：关闭缺口必须引用实现改动、自动测试和对应验收编号；“现象未复现”不算关闭。
- `GAP-RULE-003`：若产品决定不再需要某验收目标，应先修改领域合同并写明迁移/兼容影响，再关闭缺口；不得先改代码后追认。
- `GAP-RULE-004`：缺口优先级由数据丢失/秘密泄漏/远端误操作 > 状态错误/不可恢复 > 可访问性/体验 > 命名与文档一致性排序。

