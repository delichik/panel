# 应用与设施应用验收合同

## 1. 目的与判定方法

本文是应用定义、文件与编辑会话、修订、部署入口、变量、入口网关和存储共享的变更验收合同，不是实现教程。任何修改只要改变本域 API、持久化字段、用户可见行为、部署输入或跨模块调用，即使函数名未变，也必须逐条复核本文件。

每条合同包含：**前置**（准备状态）、**动作**（被测行为）、**结果**（必须可观察）、**失败**（拒绝或降级语义）、**不变量**（并发/持久化边界）、**验证**（建议证据）。“结果”和“不变量”任一不满足即视为回归。稳定编号不得因重排而复用；废弃条目应保留编号并标记废弃。

事实源优先级：可执行测试与当前路由/模型 > 当前实现 > 模块说明。发现三者冲突时不得擅自选择对用户影响更大的语义，应记录缺口并补契约测试。

## 2. 应用读取与基本定义

### APP-READ-001 应用列表分页与查询边界

- **前置**：存在多页普通应用、隐藏设施应用、删除中应用和若干实例/Job。
- **动作**：调用 `GET /api/v1/applications?page=&pageSize=&q=`。
- **结果**：返回分页 `items/total/page/pageSize`；单项只含摘要所需字段：`enabled`、`instanceCount`、节点三态计数 `healthyInstanceCount`/`abnormalInstanceCount`/`deployingInstanceCount`、**`imageReference`**（来自 `applications.image_reference` 列）、可选 `planningError`/`reconcileStopped`、镜像更新与 `updatedAt`。三计数与实例/活跃 Job 由当前页 ID 的本地批量查询按 `(application_id, server_id)` 归类；三者之和可以小于 `instanceCount`（**仅禁用应用**上已收敛的 stop/purge 节点可省略）。已启用应用若实例仍为 `desired=stopped` 且观测已收敛（stopped/missing），必须计入 `abnormalInstanceCount`，不得因 stop 收敛而省略——覆盖“启用已写入但 apply 规划尚未翻转 desired / 规划失败”的半状态。兼容字段 `runtimeStatus` 仅为观测 rollup，不得再写入应用级 `needs_attention`/`deploying` 作为列表主状态。
- **失败**：未知参数、非法页码或分页值返回 400；不得默许 `limit` 或 snake_case 别名。
- **不变量**：列表不得解析 AppSpec/YAML、读取完整配置 JSON、访问 Agent/Registry、逐应用读取详情或 runtime；普通列表必须排除 `kind=facility_application` 和 `deletion_requested=1`。应用主状态语义是启停，不是聚合运行态。`imageReference` 必须在创建/更新时从 AppSpec/runtime 镜像写入列；空列不得用 `jobId`/`panel-<name>` 冒充。
- **验证**：路由契约测试、固定查询次数/假 Agent 零调用测试、跨页 `q` 测试、三计数分类测试（含启用+stale stop desired→异常）、创建/更新持久化 `imageReference` 与空列回填迁移测试。

### APP-READ-002 应用详情完整性

- **前置**：普通应用包含部署目标、反向代理规则、文件、实例和镜像检查缓存。
- **动作**：调用 `GET /api/v1/applications/{id}`。
- **结果**：返回版本、用户配置、generation/spec hash、只读 persistentPath、实例聚合镜像更新目标和当前运行摘要；设施路由不会被装载为普通应用规则。
- **失败**：不存在或已物理删除的 ID 返回稳定 not-found；设施隐藏应用不得通过普通详情变成可编辑资源。
- **不变量**：`persistentPath` 只由应用 ID 派生，不进入保存 DTO；设施路由 `target_port=0` 不经过普通应用端口校验。
- **验证**：详情 DTO 测试、facility route regression 测试、持久路径派生测试。

### APP-DEF-001 AppSpec 解码和结构化错误

- **前置**：提交合法或损坏的 YAML。
- **动作**：执行编辑会话 validate/preview/commit，或任何需要重新解析 AppSpec 的计划、刷新、迁移、镜像更新路径。
- **结果**：合法输入先 Normalize 再 Validate；非法输入统一返回 `code=application_invalid`，首条 `<field>: <message>` 作为 message，全部 `{field,message}` 位于 `details.issues`。
- **失败**：YAML 语法错误定位 `specYaml`；不得只返回泛化错误或丢弃后续 issue。
- **不变量**：field 是稳定机器路径，message 按当前语言翻译；validate/preview 的 `diagnostics` 永远为数组而非 `null`。
- **验证**：spec validate、handler 错误体和多语言回归测试。

### APP-DEF-002 名称、镜像、命令与环境

- **前置**：结构化草稿包含名称、镜像、command argv 与 env。
- **动作**：校验并往返保存。
- **结果**：名称为 1–32 位小写字母/数字/连字符且首尾为字母数字；镜像非空；command 保持有序 argv，不按空格拆分，空白项正规化移除；env key 非空且不含 `=`；创建/编辑会话提交必须把解析后的镜像写入 `applications.image_reference`（列表摘要与详情同源）。
- **失败**：任一字段非法时返回对应稳定 field；不得用示例默认值掩盖缺失输入。
- **不变量**：command 下发 Docker `Cmd`，不转换为 Entrypoint；含冒号的参数按字符串无损往返；不得只把镜像留在 `specYaml`/`imageUpdateTargets` 而让 `imageReference` 列为空。
- **验证**：spec validate/render、创建/更新后 ListSummaries 含 `imageReference`、前端 model round-trip 测试。

### APP-DEF-003 端口、服务、检查与资源限制

- **前置**：AppSpec 包含 ports/services/checks/resources。
- **动作**：校验并渲染 runtime spec。
- **结果**：容器端口和非零 static 端口位于 1–65535，static 端口不得重复；service/check 引用存在的 port label；check type 仅 `tcp/http/script`；CPU/Memory 仅正数形成限制。
- **失败**：负资源值、非法端口、重复 static 或失效引用按字段拒绝。
- **不变量**：资源字段缺省或显式 0 均表示不限制，不得补默认限额。
- **验证**：spec 校验与零资源渲染测试。

### APP-DEF-004 权限、能力与重启语义

- **前置**：AppSpec 包含 `privileged`、`capAdd`、restart 和挂载权限。
- **动作**：正规化、保存并创建容器。
- **结果**：`capAdd` 去空、去重、大写后写入 `HostConfig.CapAdd`，可与 privileged 并存；UID/GID 非负；mode 为 3/4 位八进制。
- **失败**：capability 含非 `[A-Z0-9_]`、不支持的权限组合或 restart 枚举非法时拒绝。
- **不变量**：只允许 file/panel_file/persistent 设置 UID/GID，只允许 file/persistent 设置 mode；当前不得提供 capDrop；创建容器不得向 Docker 下发 RestartPolicy，长期恢复由控制面拥有。
- **验证**：spec mount/capAdd 测试与 Agent create-container 请求测试。

### APP-DEF-005 挂载类型与路径

- **前置**：AppSpec 包含 volume/host/global/file/panel_file/persistent/storage_share。
- **动作**：校验来源、目标和权限。
- **结果**：目标必须为非根绝对 Linux 路径；volume 名符合 Docker 名称；host/global 来源为绝对路径；file/persistent 来源为应用工作区内相对路径；panel_file 符合已注册 scheme；storage_share 为 `storage-share` 或 `storage-share:<serverId>`。
- **失败**：反斜杠、空段、`.`/`..`、根目标、未知类型/scheme 或越权权限字段均拒绝。
- **不变量**：应用不得存储 `networkMode`；所有运行容器固定加入 `panel-apps` bridge。
- **验证**：spec mount table-driven tests、旧字段清理回归。

### APP-DEF-006 未覆盖规格字段往返

- **前置**：已保存 AppSpec 含结构化表单暂未直接暴露的字段（如 constraints/checks/resources/sysctls 类扩展）。
- **动作**：打开编辑器、只修改一个已覆盖字段并保存。
- **结果**：未覆盖字段经 `uncoveredSpec` 合并后保持语义等价；新建草稿不预填名称、镜像、端口、环境、挂载或路由样例。
- **失败**：标准 YAML 不能解析时阻止 preview/commit；不得用手写 parser 猜测修复。
- **不变量**：格式差异和默认路由选项不应产生虚假 pending change。
- **验证**：前端 model round-trip 与 no-pending-change 测试。

## 3. 模板变量和内部文件

### APP-VAR-001 内置应用与服务器变量

- **前置**：AppSpec/YAML 或 template 文件引用 `app.*`、`server.*`、`server.variables.<key>`。
- **动作**：为多个目标服务器渲染。
- **结果**：`app.id/name/namespace/generation` 与 `server.id/name/host/ssh_host/ssh_port/ssh_username/variables` 使用该目标实际上下文；同一 template 可按节点得到不同内容。
- **失败**：缺失变量形成可定位 validation issue，不得落空字符串继续部署。
- **不变量**：应用级自定义变量已删除且不得复活；旧引用必须失败并引导改用服务器或内置变量。
- **验证**：variables registry、per-target rendering 测试。

### APP-VAR-002 变量/内部文件注册表隔离

- **前置**：证书或密钥资产模块注册变量 root 与 panel_file source scheme。
- **动作**：列目录、解析并部署 `key_asset:*` 或 `certificate:*`。
- **结果**：registry 按根 key/scheme 分发；私钥只在部署渲染时解密并作为只读 managed file 下发。
- **失败**：未知 scheme/kind 或已删除资产阻止部署并返回结构化问题。
- **不变量**：系统 Agent CA、Panel Agent 客户端/服务端证书不得进入应用内部文件目录；目录 API 不返回私钥正文。
- **验证**：panel_file spec validation、registry 和密钥删除保护测试。

### APP-VAR-003 稳定应用容器引用

- **前置**：应用 AppSpec 或 template 文件使用变量目录返回的 `applications.<applicationId>.containerName` 表达式引用另一普通应用；引用以稳定 application ID 持久化。
- **动作**：预览、提交或部署引用应用，或修改被引用应用的名称。
- **结果**：`GET /api/v1/applications/template-catalog` 返回当前可引用普通应用的 ID、显示名及 AppSpec/template 表达式；渲染时表达式解析为目标应用当前容器名。目标改名提交后，系统重新计算已启用应用的派生快照，只为实际引用且结果变化的应用递增 generation、写修订并请求协调。
- **失败**：目标不存在或已进入删除流程时，引用必须产生 `template_render_failed`，不得用空字符串或旧容器名继续部署；目录读取失败不得阻止应用编辑器使用其余字段。
- **不变量**：表达式不得持久化目标显示名或容器名；依赖刷新不得递增引用应用的用户 version。源应用改名已提交后，依赖刷新失败仅记录为可重试派生失败，不得把源提交谎报为回滚。
- **边界**：容器名只在引用应用与目标应用部署到同一服务器并加入 `panel-apps` 网络时可作为 Docker DNS 目标；停止或删除目标不等同于改名联动。
- **验证**：变量目录路由、缺失目标渲染错误，以及目标改名后引用应用 generation/spec/Job 环境变量更新测试。

## 4. 应用文件与保存时临时编辑会话

编辑会话不是跨设备/跨停留的用户草稿。对用户只有本地编辑与一次「保存」；会话仅在保存流水线内创建，用于大文件落盘、revision CAS、preview token 与失败清理。

### APP-EDIT-001 会话创建与所有权

- **前置**：管理员在创建/编辑页完成本地编辑后点击保存；保存流水线调用 `POST /api/v1/application-edit-sessions`。
- **动作**：创建会话。编辑已有应用时请求不得携带客户端草稿（服务端以已提交定义为准）；创建应用可带初始草稿。
- **结果**：201 返回短生命周期 session、稳定 `baseResourceVersion(value,updatedAt)`、revision、draft、files、idle/absolute expiry；所有权绑定稳定单管理员主体。
- **失败**：资源不存在或会话状态不可恢复时返回稳定错误；保存失败须 discard 会话并保留用户本地草稿以便重试。
- **不变量**：会话不是用户可恢复的跨设备草稿；修改管理员显示用户名不得隐藏/孤立会话；base timestamp 永远是开始时快照；新建资源使用预留 create ID 防重复创建；idle/absolute TTL 仅作保存中断兜底。
- **验证**：edit-session ownership/base-version/recovery 测试；编辑 begin 无 draft 时 draft 与 Get 一致。

### APP-EDIT-002 草稿 mutation 的 revision CAS

- **前置**：active session revision=N。
- **动作**：PATCH draft 或任何文件 mutation 携带 revision、client operation ID/幂等键。
- **结果**：合法变更原子写入并返回 revision=N+1；相同操作重放返回同一效果。
- **失败**：旧 revision、过期/终态会话、错误幂等键冲突不改变草稿、文件引用或 blob。
- **不变量**：同会话 mutation 必须串行；不得复制会话锁或临时目录清理逻辑到业务 CRUD。
- **验证**：revision/idempotency/race tests。

### APP-FILE-001 文件身份、列表与下载

- **前置**：应用或会话内存在 template/binary/archive。
- **动作**：列文件、读取 JSON 内容或调用 `/content` 下载。
- **结果**：外部身份始终为应用内唯一不透明 `name`；列表不返回正文；template JSON 读取可用 base64，用户下载走认证流式 content endpoint，并使用净化后的下载文件名。
- **失败**：名称含 `/`、`\`、`..`、控制字符或超过 255 字符时拒绝；物理 file id 不得暴露为调用身份。
- **不变量**：旧 path/file key 只作兼容输入并原样迁移；两个应用可有同名文件但物理 ID 全局唯一；正式正文在 `{dataRoot}/applications/files/{applicationId}/{fileId}/content/{filename}`，表行持元数据与 `filename`，路径由 id+filename 推导（alpha 可 dual-read 旧绝对 `blob_path` / `content` BLOB / 扁平 `.blob`）。下载会话文件时：有 `blob_dir` 读会话 `content/`，否则回落正式 `source_file_id` 的 content 目录（与设施静态资源同构）。
- **验证**：handler download、filename sanitization、cross-application same-name、content-layout / begin 零拷贝 / download 回落正式 content 测试。

### APP-FILE-002 文本、二进制与归档入口隔离

- **前置**：active session。
- **动作**：JSON PUT template、multipart PUT binary、multipart POST archive。
- **结果**：template 允许空/纯空白 UTF-8 文本；普通二进制与文件夹归档走各自 multipart 入口，单请求上限 64 MiB；替换复用原 name 和 kind。
- **失败**：JSON 入口提交 binary、kind 转换、超限或损坏 multipart 均拒绝且旧内容不变。
- **不变量**：binary 不进入解包或文本编辑路径；archive 是一行稳定文件记录，管理端不展开。
- **验证**：handler upload boundary、kind immutability、file closure 测试。

### APP-FILE-003 归档安全限制

- **前置**：上传 zip/tar/tar.gz/tgz 文件夹归档。
- **动作**：Panel 预检并在 Agent 写入/展开。
- **结果**：合法包保留原包并解到独立托管目录；总条目（文件和目录）≤10000、深度≤32、解包总量≤256 MiB、压缩比≤100。
- **失败**：路径逃逸、空目录/ignored-header 洪泛、格式不支持、任一限制超出均在替换正式内容前失败。
- **不变量**：Agent 单遍流式落盘并校验 sha256；即使原包 sha256 未变也覆盖解包目标以清理节点漂移。
- **验证**：Panel/Facility/Agent 三侧 archive limit 和 drift 测试。

### APP-FILE-004 blob 提交原子性

- **前置**：文件新增或替换产生新 immutable session `blob_dir`（`{session}/files/{blobId}/content/{filename}`）。
- **动作**：提交行事务或遭遇 revision/path/DB 冲突；commit 时对有 `blob_dir` 的项 `Rename` 到正式 `{fileId}/`，旧目录进 session `backup/`。
- **结果**：事务成功后引用新内容树并删除被替换目录；事务失败回滚 rename（从 backup 还原）、删除未提交的新 `blob_dir`、保留旧引用与旧正文。Begin 只写 `source_file_id`+空 `blob_dir`，不整份拷贝。
- **失败**：磁盘/DB 任一失败不得形成指向不存在内容树的行或泄漏可立即确认的孤儿目录。
- **不变量**：workspace 根为 `{dataRoot}/tmp/application-edit-sessions/{sessionId}`（与设施 `tmp/facility-*-edit-sessions` 同族），不依赖进程 cwd；原子写/目录哈希共用 `internal/platform/filestorage`。
- **验证**：故障注入、commit rename+rollback、begin 零拷贝与 orphan cleanup 测试。

### APP-EDIT-003 validate、preview 与 token

- **前置**：active session revision=N。
- **动作**：依次调用 validate 和 preview。
- **结果**：validate 刷新 idle TTL 并返回 `valid/revision/diagnostics[]`；preview 返回同 revision、诊断、带 action/subjectVersion 的短期 token 和过期时间。
- **失败**：blocking diagnostic、旧 revision、过期 token 或草稿后续变更使 commit 失败；不得让已过期 session 被 validate/preview 复活。
- **不变量**：preview 不改变正式应用/文件；任何后续 mutation 使旧 token 失效。
- **验证**：expiry、revision/token binding 测试。

### APP-EDIT-004 commit 的配置 CAS 与 no-op

- **前置**：valid preview token、当前 revision、baseResourceVersion、Idempotency-Key。
- **动作**：commit 创建或更新。
- **结果**：应用用户字段与完整文件集在同一 AppDB 事务 CAS；真实配置变化递增 version/config updated_at；完全相同定义和文件集为 no-op；返回 application、resourceVersion、`applyNeeded`/`applyAction` 与 diagnostics。edit-session commit **只持久化配置**，`applyRequested` 恒为 false；需要运行态收敛时返回 `applyNeeded=true` 与 `applyAction=deploy|stop`，由随后的 `POST /applications/{id}/deploy` 或 `/stop` 请求规划。commit **信任同 revision 的 preview token**，不再重复跑一轮完整 `prepareWithFiles` 校验；草稿/文件在 preview 之后的任何 mutation 必须抬升 revision 并使旧 token 失效。入口代理不得因单应用变更对全部网关 force 全量重算：仅对受影响网关节点规划（origins ∪ 显式 relays）；AnyAccess 且 relay 为空（表示全部非源站网关）时才退回全网关集合。无路由应用的普通保存不得触发入口代理协调。
- **失败**：并发输家返回 `resource_version_conflict` 且其应用/文件变更均不落库；过期/错配 preview token 拒绝 commit。
- **不变量**：镜像检查、变量解析、renderer snapshot 等派生刷新不得递增用户 version 或覆盖并发用户字段。正式应用文件正文落在 `{dataRoot}/applications/files/{applicationId}/{fileId}/content/{filename}`（与设施 `{dataRoot}/facility-apps/static-assets/{assetId}/content/` 同构）；`application_files` 持久化元数据与 `filename`，新写入不再填绝对 `blob_path` 或 `content` BLOB；alpha 旧行 dual-read 回落，启动 backfill 把 `.blob`/BLOB 迁入 content 布局并清空旧路径/BLOB。Commit 对会话 `blob_dir` 使用 rename+backup 回滚，happy path 不再整文件读入再拷贝落盘。删除应用时必须 `RemoveAll` 对应 `{fileId}/` 整树。应用 `archive` 仍不在 Panel 展开（产品差异保留）。
- **验证**：concurrent commit、identical commit、derived-update isolation、无路由保存跳过入口代理重算、带路由变更仅影响相关网关、content-layout/backfill/rename-commit 测试。

### APP-EDIT-005 配置成功与 apply 请求分离

- **前置**：AppDB 配置/文件已成功提交；随后的 deploy/stop 或入口代理局部协调失败。
- **动作**：commit 收尾、follow-up apply，或进程重启恢复。
- **结果**：session 仍为 `committed`；commit 响应 `applyRequested=false` 且可带 `applyNeeded`；不回滚已持久化配置。follow-up apply 失败不得否定 commit 成功。
- **失败**：恢复仅在预留 ID/精确版本、配置和文件集可验证时确认提交；结果可见但被后续变化遮蔽时转 `conflict/commit_outcome_ambiguous`。
- **不变量**：模糊结果不得重置 active、重复创建或重复应用。
- **验证**：apply failure remains committed 与 ambiguous recovery 测试。

### APP-EDIT-006 TTL、lease 与清理恢复

- **前置**：active/committing/terminal sessions、过期 commit lease、partial/unreferenced/orphan workspace。
- **动作**：周期 cleanup 或启动恢复。
- **结果**：过期 commit lease 先恢复；过期/终态 workspace、过期 partial/unreferenced `blob_dir` 子树清理；无 DB 行 workspace 仅在目录及所有嵌套条目均至少陈旧 1 小时后删除（`filestorage.WorkspaceStale`，对齐设施）。
- **失败**：活跃 lease 或 committing workspace 不得被普通 TTL 清理。
- **不变量**：cleanup 本身与 mutation/commit 串行，且不得根据目录 mtime 单独推断 orphan。
- **验证**：lease/TTL/orphan/race tests。

### APP-EDIT-007 前端事务编辑闭环

- **前置**：进入隐藏的创建/编辑页；进入时只加载已提交定义与文件清单到本地，不创建 edit-session。
- **动作**：纯本地编辑结构化字段、文件（pending 队列）与复杂对话框；用户唯一主操作是「保存」。
- **结果**：保存流水线固定为本地校验→begin→patch 本地草稿→flush 文件 pending→preview→无 blocking diagnostic 才 commit→若 `applyNeeded` 再按 `applyAction` 调用 deploy 或 stop→清理会话；提交期间禁止重复提交和离开；复杂项取消不污染主草稿；成功表示配置已保存，若已发出 apply 则提示已请求同步，并退出编辑页回到应用列表（选中该应用）。无独立 Preview 主按钮；诊断在保存失败时展示。
- **失败**：已提交定义 load 失败时禁止保存；保存任一步失败 discard 临时会话并保留本地草稿；取消/离开丢弃本地状态，使用统一 ConfirmDialog 保护脏编辑。
- **不变量**：部署完成只能从协调记录/runtime 观察，不能把 commit/deploy 200 解释为远端运行完成；编辑期文件操作不得提前打 session mutation API。
- **验证**：editor model/dialog isolation/dirty guard、local pending flush order 测试。

## 5. 应用生命周期、修订与运行能力

### APP-LIFE-001 保存与启用

- **前置**：合法 session commit，可选择 enabled/disabled。
- **动作**：提交应用定义；若需运行态收敛再调用 deploy/stop。
- **结果**：disabled 应用只持久化不规划运行；enabled 应用在以下任一成立时必须通过 follow-up deploy（或非 session 的即时 Update 路径）请求协调（写 desired / Instance / Job）：首次启用、SpecHash 变化、部署目标集合变化，或 selected 模式下仍有期望节点缺少非 purged 实例行。edit-session commit 以 `applyNeeded`/`applyAction` 表达是否需要 follow-up。入口代理仅对受影响网关局部协调（见 `APP-EDIT-004`）。
- **失败**：revision 或批量计划写入失败必须回滚该次规划请求，不能留下无 revision 的 apply Job；不得回滚已成功的配置 commit。
- **不变量**：HTTP 不直接调用 Agent/Docker；远端突变仅由 Controller 执行；不得把“配置已写入但未请求规划”报告为已应用。保存触发的规划默认 `force=false`，已满足节点跳过。
- **验证**：disabled create、commit applyNeeded、atomic revision-and-plan、proxy reconcile gating、adding selected server、re-save with missing selected instance tests。

### APP-LIFE-002 部署目标选择

- **前置**：存在兼容/不兼容 Agent 与 `all/selected` 部署模式。
- **动作**：部署或保存后 follow-up apply；或周期漂移巡检发现 selected 目标缺少实例行。
- **结果**：all 对所有健康兼容 Agent 节点各规划一实例；selected 仅规划所选节点，并对被移除旧目标规划 purge；**仅改部署节点时不得因 force 重部署已满足节点**。无整应用删除意图的 purge 只删除该节点应用目录内的 managed workspace，保留同节点应用级 persistent 和其它节点各自的数据。selected 目标若尚无 `application_instances` 行，保存后的 apply 与周期巡检都必须为其创建实例与 apply Job，不得因 SpecHash 未变或仅扫描已有实例而永久遗漏。
- **失败**：selected 空目标、不可用 Agent 或不合法目标返回稳定校验；持 persistent 的应用必须且只能选择一个节点。
- **不变量**：单节点失败不阻断其它节点收敛；目标集合在单次 batch 事务中可见，不暴露部分计划。
- **验证**：selected/all、removed target purge、node-workspace-only purge、persistent target、missing selected instance re-save / drift、delta add-server tests。

### APP-LIFE-003 显式部署/同步

- **前置**：应用可部署，可能处于 disabled 或 `reconcile_stopped`。
- **动作**：`POST /applications/{id}/deploy` 或显式同步（含保存后的 follow-up deploy）。
- **结果**：必要时以 version CAS 启用，清除人工停止协调状态，并以 `Manual=true`、默认 `force=false` 直接请求 Planner；实际规划时保存 desired/revision/Job，返回 Job `deploymentId` 且响应附本次 `operationId`，全部目标已满足时返回 `noChange=true`。若应用含反向代理规则，仅对受影响网关触发入口代理局部协调。
- **失败**：人工 deploy 不得被自动协调 `reconcile_next_run_at` 静默跳过；并发配置变化时不得整行覆盖；planner 失败原样报告，不合成 target task/log anchor。
- **不变量**：人工操作绕过应用级退避但仍执行满足态过滤；force 才同时绕过满足态，不绕过 app/server active Job 唯一性。满足态以实例内容 SpecHash 为准，generation-only 上浮不得逼迫内容未变的节点重部署。
- **验证**：enable CAS、manual bypass backoff、explicit no-change、skip satisfied、force planning tests。

### APP-PLAN-001 部署规划失败的可见性

- **前置**：已存在的应用经手动同步或后台漂移巡检进入 Planner，配置、文件引用、目标或依赖检查失败，可能尚无任何 Job。
- **动作**：记录本次规划结果，并读取应用详情、列表摘要或 runtime。
- **结果**：三种响应均提供可选 `planningError`，包含稳定错误码、脱敏原因、可用的字段/文件名、是否可重试、发生时间、配置 version 和独立 operationId；手动同步失败后页面立即重载，错误在 Toast 消失及刷新后仍可查询，并可进入对应活动记录。
- **失败**：未知内部错误仅返回通用安全原因；不得暴露配置内容、令牌或内部路径。单个应用规划失败不得中止其它应用的后台巡检。显式同步可能已启用应用，但不得把规划失败解释为远端部署已接受或完成。多阶段规划（apply / 移除目标 purge / 显式 stop）若前序批次已持久化 Job、后序批次失败：必须唤醒 Controller 执行已落库 Job，并仍记录 `planningError`；不得假装“规划失败=零副作用”。Deploy/Restart 等同步 API 在返回 error 时可不带回 `DeploymentID`——客户端应以重载后的应用 `planningError` 与 Job/runtime 为准，而不是依赖错误响应里的部署 ID。
- **不变量**：诊断与容器 observed 状态分离，不伪造 Job/execution。诊断更新与失败活动事件在同一事务提交，不修改用户 version、generation 或 updatedAt；同配置同原因的自动重复失败去重，人工重试保留独立记录。旧 version 的结果不能覆盖新配置的诊断。
- **恢复与升级**：成功创建/合并计划或校验通过且目标均已满足时清除当前错误，保留历史失败；因停止自动协调或无匹配目标而跳过不能清错。现存数据库新增 `applications.planning_error_json`，默认空值并保留既有应用数据；本项不放宽文件名规则。
- **验证**：`planning_outcome_test.go` 覆盖无 Job、人工同步、去重、恢复、跳过、脱敏及旧 version；数据库升级测试与容器巡检隔离测试。

### APP-LIFE-004 停止

- **前置**：应用存在一个或多个实例。
- **动作**：`POST /applications/{id}/stop` 或重放 `application_stop`。
- **结果**：只更新必要生命周期列为 disabled、递增用户 version；同一请求内同步规划现有实例 desired=stopped/action=stop（`purge=true` 时为 purged/purge）；stop 删除容器释放端口和名称，但保留 managed files 与 persistent 数据。
- **失败**：规划失败按配置已变更/应用请求失败分离呈现；不得回滚为 enabled 或同步直连 Agent。
- **不变量**：旧整行快照不得覆盖并发派生字段/用户编辑；任务 executor 只规划且最终完成自身任务；`enabled=0` 且仍有 `desired=running` 实例时，周期漂移扫描必须继续为其规划 stop，不得因禁用而跳过。
- **验证**：stop CAS/concurrent derived fields/task replay、stop plans without reconcile trigger、disabled desired-running drift reconverge tests。

### APP-LIFE-005 重启

- **前置**：已启用应用。
- **动作**：`POST /applications/{id}/restart` 或重放 `application_restart`。
- **结果**：强制 planner 创建/合并 apply Job；任务在规划完成后完成，远端重建由 Controller 执行。
- **失败**：不得直接调用 Agent restart 或制造第二条 active Job。
- **不变量**：running Job 通过 force nonce 记录更新意图，完成 RPC 后比较最新 desired 并 requeue。
- **验证**：restart planning、force nonce、desired-change integration tests。

### APP-LIFE-006 删除 finalizer

- **前置**：应用存在当前实例、首次部署前恢复的数据，或曾在已移除部署节点上保留持久数据。
- **动作**：`DELETE /applications/{id}`；存在持久化配置或已记录位置时必须显式传入持久数据删除确认。
- **结果**：确认通过后才置 `deletion_requested=1` 并隐藏普通列表；同一请求内同步规划 desired=purged、action=purge/removeData=true（覆盖当前实例与 `application_persistent_locations` 历史节点并集）。即使当前零实例，也必须先为历史节点建立清理 Job；全部节点 observed=missing 后清理终态 Job 并物理删除应用，位置记录随应用级联删除。
- **失败**：未确认、历史节点无法解析或规划/purge 失败时保留 `deletion_requested=1` 与应用/实例/位置记录供重试，不得回滚删除意图；不得在确认前置 deletion_requested，也不得通过 FK cascade 绕过 active Job。
- **不变量**：普通列表排除 `deletion_requested=1`，但协调用 `ListForReconcile` 必须包含待删应用，周期巡检须继续为其规划 purge；普通移除部署节点只删除 managed workspace 并永久保留位置记录与 persistent；jobs→applications 为 RESTRICT；应用停止、移除节点与删除应用的数据语义不得混淆。
- **验证**：confirmation-before-mutation、zero-instance historical-node purge、multi-node delete finalizer full-chain integration、ListForReconcile includes deletion_requested、delete planning failure retains retry path tests。

### APP-LIFE-007 持久数据导出与恢复

- **前置**：AppSpec 真正包含 persistent mount；可处于已有单实例或首次部署前。
- **动作**：GET persistent-data 携带 `serverId`，或 multipart POST zip。
- **结果**：下载节点必须属于该应用已记录位置；单一位置可兼容省略 `serverId`，多位置必须明确选择。下载不依赖当前 Instance，因此首次 apply 前恢复到选定节点的数据及已移除部署节点上的旧副本均可导出；恢复在远端写入前追加位置记录，已有实例恢复后规划强制重启，首次部署前恢复成功不触发重启。
- **失败**：无已记录位置、下载目标不唯一/不属于应用、归档为空/路径逃逸或替换失败时拒绝并保留旧数据。
- **不变量**：上传恢复采用临时目录/原子 swap；失败不破坏旧 persistent。
- **验证**：explicit/implicit node selection、unapplied/historical-node download、restore/predeploy/atomic Agent tests。

### APP-LIFE-008 无损迁移门禁

- **前置**：恰有一个 running 来源实例，指定兼容且无该实例的目标节点。
- **动作**：请求迁移。
- **结果**：仅无 persistent、host/global bind、Docker volume 的应用可迁移；先部署目标成功，再删来源容器/节点应用 managed workspace/instance 行并切换部署目标。
- **失败**：任一门禁、目标冲突或目标部署失败时保留来源运行态与数据。
- **不变量**：不得先删来源再尝试目标。
- **验证**：迁移成功/拒绝/目标失败故障注入。

### APP-LIFE-009 节点应用目录布局与旧版本升级

- **前置**：节点首次部署应用，或升级前已存在 `/opt/panel/apps/<appId>/instances/<instanceId>` 旧 workspace 与引用该路径的运行容器。
- **动作**：Agent 执行 apply、reload、persistent archive/restore 或 purge。
- **结果**：节点本地 managed workspace 直接使用 `/opt/panel/apps/<appId>/{files,archives,manifest,state}`，persistent 继续位于同级 `persistent`；apply 将单节点唯一旧实例的各 managed area 幂等迁移到新位置，persistent archive/restore 也会在未 apply 时发现并迁移唯一旧 persistent 后继续，且重建仍引用旧 bind 路径的容器；purge 按删除意图区分只删 managed workspace 或删除整个应用目录。
- **失败**：新旧布局同一 area 同时存在时停止迁移并保留两侧数据；旧 bind 容器不得执行 reload 后伪报成功，必须降级为 recreate；迁移失败不得删除 persistent。
- **不变量**：`instanceId/serverId` 仍是控制面和容器身份，不再进入节点文件路径；同一节点同一应用只能有一个 Instance，因此不得重新引入 server 目录层。
- **验证**：flat workspace path、legacy migration/conflict、pre-apply legacy persistent archive、legacy bind recreate/reload fallback、workspace-only/full purge tests。

### APP-RUN-001 runtime 状态与刷新写回

- **前置**：Instance 具有 desired/observed，可能存在按 server 的 active Job。
- **动作**：`GET /applications/{id}/runtime`。
- **结果**：只以 AppDB observed 快照派生实例观测；返回 serverId/serverName、generation、容器身份、`lastError`、observedAt，以及应用级三态计数（与 `APP-READ-001` 同一归类：已启用且 `desired=stopped` 收敛计入异常；活跃 Job 计入部署中）。每个实例旁挂该 server 的 `operation`（活跃 `queued`/`running`/`failed_retryable`，或无活跃时该 server 最新终态 `failed`）：含 status、stage、attempt、nextRunAt、operationId、`executionId`、结构化错误；uncertainty 时另含 `monitorReasonCode`、`evidenceDeadlineAt`、`graceDeadlineAt`（便于深链日志与排障，不替代过程事件）。应用级 `status`/`operation` 若仍返回，仅为兼容观测 rollup / 最差节点回声，UI 主路径不得依赖。
- **失败**：Docker not found 映射 `missing` 而非 stopped；Agent 不兼容/不可达不回退 SSH；终态部署错误不得因 active 查询为空而从**该节点** runtime 投影消失；容器启动后退出的结构化错误必须保留有界脱敏的状态、exit code 与 Docker State.Error。runtime 读取失败不得因 Agent 不可达而失败。
- **不变量**：runtime 读取不得触发 Agent RPC（见 `APP-RUN-005`）；handler/业务服务不得直接覆盖 observed；无容器的 pending/failed Job 不提供日志入口。apply 启动成功必须经过 `ORCH-CTRL-007` 的稳定观察；短时退出即使退出码为 0，也必须展示部署失败/退避，而非以瞬间 running 表示部署完成。协调单位是节点上的应用。
- **验证**：local observation snapshot、retryable schedule、per-server terminal failure projection、启用+stale stop desired 三计数测试。

### APP-RUN-005 动作回执与请求内 Agent 调用边界

- **前置**：已持久化应用，调用 `POST /applications/{id}/deploy|stop|restart` 等同步业务入口；随后由调用方按需读取 runtime。
- **动作**：调用同步动作接口；再读取 `GET /applications/{id}/runtime`。
- **结果**：同步动作响应只是规划/受理回执（`deploymentId`、`noChange`、`application`），不内嵌 runtime 快照，也不表示远端已完成；runtime 详情按 `APP-RUN-001` 从 AppDB 快照返回。
- **失败**：Agent 慢/不可达不得让同步动作接口等待逐实例状态查询而超时；动作受理与远端收敛失败分别表达，不得把请求超时写成部署失败。
- **不变量**：任何主 Panel HTTP 请求路径（含 deploy/stop/restart/runtime 与列表摘要）不得调用 Agent `RuntimeStatus` 或其它 Agent 读写 RPC；观测新鲜度只由 Agent report、周期漂移巡检和 `RuntimeReconcile` 经 `ObservationWriter` 写回；需要强制刷新时走后台任务。
- **验证**：`TestApplicationActionReceiptsDoNotCallAgentStatus`、`TestRuntimeProjectsLocalObservationSnapshotWithoutAgent`、`TestListSummariesUsesCachedInstanceBucketsWithoutAgent`。

### APP-RUN-002 实例日志

- **前置**：runtime 实例有后端保存的 container_name。
- **动作**：`GET /applications/{id}/logs?instanceId=&tail=`。
- **结果**：后端按 instanceId 解析容器名并读取日志；tail 限制在 1..10000（缺省使用安全默认）。应用页容器日志弹窗先读取该应用实例，用服务器名与容器名区分目标；多个可读实例由用户选择，单个自动选择，日志请求必须携带所选 instanceId。
- **失败**：客户端传 containerName 不得成为授权/定位依据；跨应用 instance 或无容器返回稳定错误。
- **不变量**：日志展示属于 runtime 实例，不依赖 allocation/task 投影。停止/失败但仍存在的容器可查看；missing/purged/deleted 或尚无容器的 pending 实例不可读取。切换实例/应用、关闭弹窗或离页取消旧请求并丢弃迟到响应；无实例、空日志与读取失败分开展示；仅手动刷新，不定时轮询。
- **验证**：handler/service logs tests，`ApplicationLogsDialog.test.ts` 的多/单实例、已退出容器、无实例、读取重试、切换/关闭竞态及 Mock instanceId 范围测试。

### APP-RUN-004 执行 monitor / 无进度注意力态

- **前置**：某节点活跃 Job 携带 `error_class=uncertainty`（remote-fence monitor mode）。
- **动作**：读取列表摘要/runtime 或查看应用详情节点列表。
- **结果**：
  - 该节点计入应用级 **部署中** 计数；不得把应用主状态写成 `needs_attention` 或在列表主徽标显示“系统核实中”。
  - 已投影 Agent 进度阶段（如 `ensure_image` / `verify_running`）：该节点行展示真实 `last_stage`，不得显示“系统核实中”。
  - 尚无投影进度：该节点行显示“系统核实中”与任务/日志入口，并继续显示该节点观测态；文案说明 Panel 正在向该节点收取执行报告，不得引导用户声明成败。
- **失败**：不得把无进度的 monitor fence 显示为成功或确定失败；不能用等待时间推测结果。证据窗口耗尽后 Job 按 `ORCH-LEASE-005` 终态失败，该节点改为 **异常** 并可同步/重试。不得把 `execution_monitoring` 传输原因渲染成部署失败危险告警。
- **不变量**：展示投影不修改底层 Job state、lease 或远端证据；有界轮询（任一节点活跃 Job）与自动核实仍适用。不得要求用户经 UI 提交人工 resolve。
- **验证**：节点 uncertainty/进度投影测试、三计数把 monitor 计为部署中、前端节点行文案测试。

### APP-IMG-001 镜像检查聚合

- **前置**：应用各实例节点有不同 `image_updates` 缓存。
- **动作**：读取详情或执行 `application_image_check`。
- **结果**：任一节点可更新即 `imageUpdateAvailable=true`，并返回逐节点 reference/local/latest/check time/error；检查任务可 run-now/retry。
- **失败**：详情不得提供已移除的手动 check HTTP 入口；registry 网络失败记录节点错误而不伪造 up-to-date。
- **不变量**：详情聚合仅限已部署实例；检查是缓存/派生状态，不制造配置版本冲突。
- **验证**：image check executor 与 multi-instance aggregation tests。

### APP-IMG-002 镜像更新

- **前置**：至少一实例检查出可更新镜像。
- **动作**：POST image/update 或重放 `application_image_update`。
- **结果**：解析镜像/digest、记录新 revision、更新 desired 并规划重部署；成功后把对应节点缓存标为当前。
- **失败**：更新/计划失败保留结构化 registry/Agent 诊断；不得在 task executor 内直接 runtime apply。
- **不变量**：与 stop/restart/refresh 共用应用 lifecycle 并发 key。
- **验证**：image update executor/cache-current/redeploy tests。

### APP-IMG-003 应用对象级"允许自动更新"开关

- **前置**：应用存在镜像检查缓存；全局设置页「自动更新 → 应用更新」可能开启或关闭。
- **动作**：应用编辑保存 `autoUpdateEnabled`，或设置页自动更新任务规划应用镜像更新。
- **结果**：`applications.auto_update_enabled` 默认 1（旧数据/旧客户端缺省为开）；保存/详情/列表读回同一值；只有 `Enabled && AutoUpdateEnabled && ImageUpdateAvailable` 的应用会被自动更新任务纳入并规划 `UpdateImage`；关闭后手动「检查更新/立即更新/批量升级」仍可用。
- **失败**：非法/缺省不得被解释为关闭；保存失败保留原值；不得把关闭状态当作停用应用或禁止部署。
- **不变量**：对象开关只影响自动更新调度，不改变应用生命周期、镜像检查缓存或手动升级语义。
- **验证**：默认值/保存往返/自动更新过滤/关闭后手动更新可用测试；`UI-APP-019` 前端开关测试。

## 6. 应用反向代理规则

### APP-PROXY-001 域名、路径与所有权

- **前置**：应用保存 reverseProxy rules。
- **动作**：正规化并写入 `reverse_proxy_routes`。
- **结果**：域名符合 DNS hostname（允许单个 `*.` 前缀），targetPort 1–65535，同一所有者可有多个 Path；Path 为安全 Nginx 路径。
- **失败**：域名与设施路由、其它应用、Panel 入口冲突，或 Path 含 `# ? \` 等特殊字符时返回结构化错误。
- **不变量**：规范化 domain 全局唯一；持久化稳定结构，不存翻译文案或旧 targetType。
- **验证**：domain/path/global conflict tests。

### APP-PROXY-002 源站自动解析与 AnyAccess

- **前置**：应用部署目标与设施全局 gateway 集合已知。
- **动作**：保存路由并渲染。
- **结果**：源站由后端计算为部署目标∩gateway；客户端 originServerIds/primaryOriginServerId 被忽略。策略仅 round_robin/primary_backup/ip_hash；originPriority 对仍存在节点保序，删除节点丢弃，新节点追加。
- **失败**：无有效源站时阻断；显式 relay 必须为非源站全局 gateway。
- **不变量**：relay 为空表示全部非源站 gateway；前端只展示自动源站，不可提交越权源站。
- **验证**：normalize options/ignored client origins/priority projection tests。

### APP-PROXY-003 高级 HTTP 选项完整性

- **前置**：Path 设置 gzip、body limit、连接/读/写超时、buffering、WebSocket、请求/响应 headers。
- **动作**：加载、编辑、保存、hash、转换为设施路由并渲染 Nginx。
- **结果**：全部 options 无损；WebSocket 仅由 `options.webSocketMode=auto/on/off` 表达，前端、API、持久化与渲染不得出现旧 `webSocket` 布尔字段；升级时一次性把旧 JSON 值迁入尚未设置的 mode 并删除旧 key，此后运行时不再兼容读取；Header 名按 HTTP token、大小写不敏感去重，值拒绝 CR/LF/NUL/Nginx 变量注入。
- **失败**：非法 header/options 阻断，不得静默删除高级项。
- **不变量**：每个代理 location 显式 `proxy_cache off`；Panel 不生成/覆盖/隐藏客户端缓存 Header。
- **验证**：前端 reactive path 结构化 WebSocket mode 往返、旧字段不写入测试、reverse_proxy_options 与 Nginx render tests。

### APP-PROXY-004 路由生效范围与上游不可用

- **前置**：应用与入口网关均部署到部分节点。
- **动作**：入口网关协调并接受请求。
- **结果**：仅二者交集节点获得应用路由，始终代理同节点容器名和端口；变量 `$panel_proxy_upstream` + Docker resolver 延迟解析。
- **失败**：上游缺失/停止不阻止 Nginx validate/reload；请求 502/504 显示中英文 Seamark 风格提示，不暴露 Nginx、错误码或容器名。
- **不变量**：容器恢复后无需再次保存/同步即可连通。
- **验证**：deferred upstream resolution/error-page render tests。

## 7. 入口网关设施应用

入口网关分为两个逻辑平面（UI 显式分开）：

- **代理平面**：网关成员与 nginx 容器是否存在/运行；隐藏应用 `facility-reverse-proxy` 只表达容器身份。
- **配置平面**：每条设施域名记录、每条应用反代记录均为独立配置——独立 `version`、独立 CAS、私有文件；一次只编辑/提交一条。

### FAC-RP-001 设施边界与双平面

- **前置**：访问设施目录、详情和配置页。
- **动作**：加载 `reverse-proxy`。
- **结果**：目录/详情使用专属 adapter/API。代理平面状态存于 `facility_app_configs`（网关列表等，**不再**作为业务配置单一 version 源），读写入口为 `GET/PUT /api/v1/facility-apps/reverse-proxy/proxy-plane`。配置记录存于 `reverse_proxy_routes`（每域名/应用路由一行，自带 `version`）与按记录隔离的资产元数据/目录；列表与单条读写为 `GET /api/v1/facility-apps/reverse-proxy/config-records`、`GET/DELETE /api/v1/facility-apps/reverse-proxy/config-records/{recordId}`，配置平面生效为 `POST /api/v1/facility-apps/reverse-proxy/config-records/{recordId}/apply`，已提交资产按记录下载为 `GET /api/v1/facility-apps/reverse-proxy/static-assets/{assetName}/content?recordId={recordId}`。隐藏 identity `facility-reverse-proxy` 只投影容器 Instance/Job。
- **失败**：不得通过普通 application editor 编辑隐藏应用。
- **不变量**：代理平面 `ProxyIdentityHash` 不含路由/资产内容；配置平面变更不得 bump 无关记录的 `version`；应用反代记录 version 随应用保存单调递增，不得因整表替换归零或回退。
- **验证**：hidden filtering、per-record version isolation tests。

### FAC-RP-002 单条配置编辑会话

- **前置**：稳定管理员主体在**单条**配置记录编辑页完成本地编辑后点击保存。
- **动作**：保存流水线 POST facility edit-session，绑定唯一 `recordId`（设施域名为 domain）；请求体省略业务 draft 时由服务端已提交该记录播种。
- **结果**：会话草稿等于该条已提交记录；返回 draft/assets/revision/baseResourceVersion（base 为**该记录** version）；idle TTL 24h、absolute TTL 7d；mutation/validate/preview 延长 idle。
- **失败**：过期 session mutation 不得复活；保存失败须 discard 并保留本地草稿。
- **不变量**：一次会话不得编辑多条记录；进入编辑不得创建会话；资产只引用本记录私有源。
- **验证**：per-record begin/TTL tests；begin 后 draft 与 GetRecord 一致。

### FAC-RP-003 记录私有资产

- **前置**：单条记录编辑会话已创建；本地 pending 含 text/binary uploaded_file 或 binary uploaded_bundle。
- **动作**：flush 时按**该记录内**唯一 assetName PUT/替换/下载/删除。
- **结果**：正文存放在内容寻址的内部目录 `{dataRoot}/facility-apps/static-assets/{assetId}/content/`（asset id 只是存储键，不属于公开契约），元数据行以 `record_id` 绑定该配置记录，同一记录内 `name` 唯一；跨记录允许同名但互不覆盖。alpha 迁移把共享旧资产按引用域名拆成记录级行，并在启动时从内容哈希/文件名一致的 donor 补齐缺失的私有目录。text ≤1 MiB UTF-8；bundle 只能 binary；kind/contentMode 对同名不可变；仅改资产再 commit 不得回滚未改路由字段。
- **失败**：跨记录引用、重复 name、仍被本记录 static path 引用的删除在 mutation 前拒绝；`recordId` 归属的已提交资产不带 `recordId` 下载返回校验错误，未归属记录（迁移前无引用）的 legacy 资产仍可按名兼容读取。
- **不变量**：不同记录的同名资产互不覆盖；无全设施共享资产表作为写路径。
- **验证**：per-record asset isolation/reference/backfill tests、带 recordId 的已提交资产下载测试。

### FAC-RP-004 单条 CAS 与提交互斥

- **前置**：两个会话基于同一**记录** version，或会话已 conflict。
- **动作**：并发 commit 同一 recordId，或 PATCH rebase。
- **结果**：该记录 CAS 仅一方成功；另一方 `resource_version_conflict`；其他记录 version 不变。
- **失败**：origin 不在当前代理平面网关集合、删除仍被引用资产均阻断。
- **不变量**：commit/recovery 锁按 recordId 分隔，不得阻塞无关记录提交。
- **验证**：concurrent per-record commit tests。

### FAC-RP-005 单条 manifest 恢复

- **前置**：某记录 commit manifest 已记录，进程可能在文件移动或 DB 前后崩溃。
- **动作**：启动或 lease 过期恢复。
- **结果**：DB 未提交则回滚该记录文件并恢复 session；DB 已提交或该记录 version=base+1 且资产 hash 匹配则标 committed。
- **失败**：不满足精确条件不得猜测成功；不得改写其他记录。
- **不变量**：恢复路径可直接请求该记录的 config-plane apply；apply 失败仍为 committed warning。
- **验证**：per-record crash recovery tests。

### FAC-RP-006 域名源站与 AnyAccess

- **前置**：代理平面已配置 gateways；编辑单条域名记录的 originServerIds 与 AnyAccess。
- **动作**：validate/commit/render。
- **结果**：每记录至少一 origin 且属于当前 gateways；AnyAccess 关闭仅源站开放，开启时 relay 按全部非源站或指定子集转发；负载策略为轮询/主备/IP hash，固定 `max_fails=3 fail_timeout=30s`。代理平面移除网关前校验所有配置记录（设施域名与应用反代）的 origin/AnyAccess primary/relay，仍在引用则拒绝并返回 `facility_proxy_plane_gateway_in_use`（空网关集合表示整体停用，放行且不改记录）。
- **失败**：新请求空 origin 不得解释为全部；上游模式域名不得与普通应用/Panel 入口共享；不得通过代理平面 API 悄悄改写配置记录或 bump 其 version。
- **不变量**：记录级校验不依赖其他域名草稿；移除网关后不得留下指向非网关服务器的 origin/relay。
- **验证**：normalize origins/relay/render tests、proxy plane gateway-removal rejection tests。

### FAC-RP-007 Path 规则类型

- **前置**：单条记录下配置 static/redirect/proxy_pass。
- **动作**：validate、保存、重开与渲染。
- **结果**：static 只接受本记录私有 uploaded_file/uploaded_bundle；redirect 仅 301/302/307/308 与安全目标；proxy_pass 必须 `http(s)://` 且保持原值和 preserve_source/hide_source。
- **失败**：host_path 返回 `facility_static_site_source_invalid`。
- **不变量**：非 static normalize 清空 sourceType；proxy_pass 不得被纠正成 static。
- **验证**：service/model route-type tests。

### FAC-RP-008 TLS、静态内容和 Nginx 文件布局

- **前置**：域名可能匹配域名证书、自签用户域证书或无证书。
- **动作**：构建每节点 Nginx runtime spec（拼装所有相关配置记录）。
- **结果**：入口网关镜像由 Panel 受管：固定标签线 `nginx:1.28-alpine`（补丁版本按 `FAC-RP-014` 自动更新，minor/major 仅随 Panel 发布显式改线），用户不可配置镜像；主配置目录只读挂载 `/etc/panel-nginx`，证书独立 `/etc/panel-certs`。每域名按自身 HTTPS 开关与证书匹配决定是否生成 443：域名证书优先，其次匹配自签，`httpsEnabled=false` 或无证书时仅生成 80；证书 managed files 只为实际启用 HTTPS 的域名下发。HTTP/2、HTTP/3 为每域名开关（默认 H2 开、H3 关），HTTP/3 生成 `listen 443 quic`/`http3 on`/`quic_retry on`/`Alt-Svc`，依赖容器发布 UDP 443 与 UFW 放行；`ssl_early_data` 不再输出。80 与 443 顶层指令支持每域名 `custom_server_directives`、http 块支持 `customHttpDirectives`（同名覆盖、其余追加，见 `FAC-RP-017`）。AnyAccess 中继回源 HTTPS 时 `proxy_ssl_verify on`，公共/ACME 信任锚只读绑定宿主 `/etc/ssl/certs/ca-certificates.crt`（`mount.type=host`），不得把 CA 包写入仓库或 managed files；自签中继用独立 `trusted.pem`。静态内容来自各记录私有 managed files。
- **失败**：不得覆盖镜像 `/etc/nginx` 或单文件 bind 主配置；HTTPS 关闭的域名不得残留其 443 server/证书文件；HTTP/3 未发布 UDP 443 时不得宣告 Alt-Svc。
- **不变量**：主配置显式 `nginx -c /etc/panel-nginx/nginx.conf`；父目录 0755、上传文件 0644；中继 TLS 校验不得关闭；公共 CA 仅来自宿主系统信任库；证书匹配规则与 `routeSummaries` 同源。
- **验证**：proxy spec mounts/TLS/static serving tests；`TestRenderAnyAccessRelayTLSUsesPublicCABundleNotLeaf`；`TestRelayTrustedCertificatePathUsesHostCABundleForDomainCerts`；每域名传输开关渲染矩阵与证书文件 gating 测试。

### FAC-RP-009 代理平面 recreate 与配置平面 reload

- **前置**：网关上已有或尚无 nginx 容器；代理身份或配置内容发生变化。
- **动作**：规划并执行。
- **结果**：
  - **配置平面**：记录内容变化（路由、upstream、Header、证书、每域名传输开关、设施全局参数、自定义 http/server 指令）→ 仅对受影响网关 `WriteManagedFiles` + `nginx -t` + `nginx -s reload`（`RuntimeReload`）。成功路径**不得** stop/remove/create 容器。
  - **代理平面**：网关加入、镜像引用/digest、命令、端口布局（含新增 UDP 443）、mount 结构变化、或节点尚无容器 → `RuntimeReconcile` create/recreate；网关移除 → stop/purge。判定必须比较 current/desired 的代理身份（镜像引用、镜像 digest、命令、端口、env、mount 结构、restart），不得只比较配置内容哈希。
- **失败**：配置 validate 失败回滚该节点文件并保留原 worker，**不**自动 recreate；代理平面失败保留诊断；`nginx -t` 失败不得留下半生效配置。
- **不变量**：`ProxyIdentityHash`（含镜像引用/digest）与 `ConfigContentHash` 分离并都参与满足判定；满足判定禁止把无关记录 version 打进其他节点 hash；每节点目标镜像 digest 只影响该节点。
- **验证**：reload-not-recreate、identity-change-recreate（含镜像 digest/端口变化）、per-record hash isolation、自定义指令变化 reload tests。

### FAC-RP-010 单条配置提交后的协调

- **前置**：单条配置记录已 commit，或应用反代记录变更。
- **动作**：commit 只持久化该记录并返回 `applyNeeded`；随后请求 **config-plane apply**（受该记录 origins∪relays 覆盖的网关；空 relay AnyAccess 时为全部非源站网关）。崩溃恢复可服务端直接 apply。
- **结果**：未变 `ConfigContentHash` 的网关跳过；变了的网关只发 reload Job。代理平面 ensure 仅在缺容器时由代理 API/网关变更触发，不因普通配置保存 recreate。
- **失败**：apply 错误须为稳定 `panelerr` 或 committed warning；**不得**用裸 500 否定 commit 成功。
- **不变量**：不得同步执行远端 Docker；同一次 apply 只加载一次应用路由目录并按网关复用。
- **验证**：single-record commit applyNeeded、reload job、unaffected record version unchanged tests。

### FAC-RP-011 DNS 联动

- **前置**：单条设施域名记录保存新增、修改或删除，或应用反向代理路由变更/删除，DNS zone 与服务器地址可能不完整。
- **动作**：触发 `dns_proxy_records_sync`（`ConcurrencyPolicy=resource_queue`，同资源串行排队而非复用活跃任务）；手动重试/接管走 `POST /api/v1/facility-apps/reverse-proxy/dns-sync`；设施 DNS 页对 `pending`/`failed`/`skipped`/`unknown` 均提供重试。
- **结果**：全部当前设施域名与应用反向代理域名及需清理旧域名进入 pending；任务读取并合并数据库中所有 pending（接管请求除外）；只在期望 A/AAAA 与实际不同时增删改；每域名持久化 pending/synced/failed/skipped；任务成功完成后若仍有 pending（同步过程中新写入），必须再触发 `pending_drain`，不得长期停在「DNS 同步中」。
- **失败**：服务器列表读取失败或 `servers` 依赖未装配使任务失败且域名保留 pending，不得按“无服务器”误删；无 IPv4/IPv6 的域名 skipped 并带提示；接管缺少确认返回校验错误。
- **不变量**：anyAccess 关闭目标=origins，开启目标=全局 gateways；活跃/排队任务期间新增 pending 不得因旧 params 或任务复用遗漏；应用域名与设施域名共用同一同步状态与托管 comment。
- **验证**：dns sync all-current/merge-pending/pending-drain/application-domain/server-error/status/takeover tests；同值非托管记录见 `DNS-SYNC-001/004`。

### FAC-RP-012 设施逐节点部署诊断

- **前置**：多个网关节点分别成功、重试、失败、系统核实中或无 Job。
- **动作**：GET 设施代理平面详情。
- **结果**：`deployments` 分别返回节点身份、容器 observed 状态/时间、优先活跃否则最新 Job 的 operationId、状态、阶段、attempt、nextRunAt 和错误码/类/摘要/详情。另一节点较晚成功不能掩盖失败；容器与部署结果独立显示，移除节点后未完成的清理失败须保留。
- **失败**：无 Job 显示无结果；配置/同步 lastError 独立说明来源；DNS 提供商同步与容器内名称解析错误保持区分。
- **不变量**：常规读取只查询本地投影，不远端读日志，不新增表或 schema；无 Job 节点不能因列表顺序遮住其他节点进行中的操作。
- **验证**：`TestFacilityDeploymentDiagnosticsKeepsEachNodesFailure`、`TestFacilityPendingOperationNotHiddenByNodeWithoutJob`、`FacilityDiagnostics.test.ts`。

### FAC-RP-013 按需代理请求错误诊断

- **前置**：管理员选择已有设施实例节点。
- **动作**：显式 GET `/api/v1/facility-apps/reverse-proxy/diagnostics?serverId=`，只接受唯一 serverId，不允许客户端指定容器/地址。
- **结果**：兼容 Agent 按设施容器 ID 读取最多 200 行日志。后端仅对标准 Nginx error 前缀及错误正文分类，返回稳定 code、节点、域名、上游、计数和可用日志本地时间；前端按码解释名称解析/TLS/连接/超时或未知错误及下一步。同 code/域名/上游合并，最多 20 项，标记截断。
- **失败**：无实例拒绝；不兼容/离线/容器缺失/5秒超时为 unavailable；空样本不能宣称所有路由健康。未知错误不臆造根因，TLS 建议不得关闭校验；access log 和请求/UA 文本不能伪造错误。
- **不变量**：仅人工点击，无轮询/自动重试/持久化/活动追加，不修改 Job、观测或退避。尾样本不保证固定时间覆盖，历史错误不等于当前仍故障。保留分析输入最多128 KiB；证据限长，移除请求 URL/query、客户端地址和上游用户信息。配置变化/离页取消并丢弃迟到结果。
- **验证**：分类、去重、未知回退、请求隐私、范围拒绝测试；前端按需调用、双语解释和取消测试。

### FAC-RP-014 入口代理镜像检查与补丁自动更新

- **前置**：入口代理设施已配置至少一个网关；Panel 可访问 registry（解析受管标签 digest）；全局设置页「自动更新 → 应用更新」与设施 `imageAutoUpdateEnabled` 可能开启或关闭。
- **动作**：周期检查任务（默认 6h）、手动 `POST /api/v1/facility-apps/reverse-proxy/image/check`、手动 `POST /api/v1/facility-apps/reverse-proxy/image/update`、或自动更新协调任务调用。
- **结果**：
  - 检查任务解析受管标签（`nginx:1.28-alpine`）的 registry digest，逐网关对比容器实际运行镜像 digest（Agent 容器/镜像事实），状态持久化于 `facility_proxy_image_states`（每网关一行：reference/target/observed/previous digest、status、lastError、checkedAt、pinned、autoUpdateFailures/failedDigest）并投影到隐藏应用 `image_*` 列与 `image_updates` 缓存。
  - 有可用更新且两处开关均开启时逐节点滚动：预拉取 → 写入该节点 target digest（仅该节点 spec hash 变化）→ 触发该节点代理平面 recreate（先拉后建）→ 等待 Job 终态 → 复核运行 digest → 继续下一节点；单节点失败中止滚动，其余节点保持旧版本继续服务。
  - 小版本自动：受管标签线内 digest 变化即自动更新；跨 minor/major 不自动（仅 Panel 发布显式改线）；P1 预检 `nginx -v` 版本线不符时拒绝。
  - **回滚/pinned（P1）**：`POST .../image/rollback`（`{serverIds?,confirm:true,digest?}`，digest 缺省取该节点 previous_digest）走同一预拉取→切换→校验流程；成功后节点 `pinned=true`、自动更新暂停；`POST .../image/resume` 清除 pinned 并恢复自动更新；无 previous digest/非网关/缺确认返回稳定错误。
  - **失败退避（P1）**：同一目标 digest 自动更新连续失败达 3 次后暂停该节点自动更新（状态投影含失败次数与失败 digest），手动检查/更新/回滚成功后清零并可再次自动更新。
  - **实际版本展示（P2）**：检查任务通过 Agent 在运行容器内执行 `nginx -v`（RuntimeStatus 可选版本命令）采集实际 nginx 版本并持久化到 `facility_proxy_image_states.image_version`；GET 的 `image` 投影按网关返回 `imageVersion`（聚合取首个非空），前端在镜像卡片与逐网关行展示；采集失败/旧 Agent 为空显示不可用，不得伪造版本。
- **失败**：registry 不可达/超时软失败（保留旧状态与错误，不伪造最新）；节点离线/Agent 不兼容/容器缺失为 unknown 并带原因；拉取失败旧容器继续服务；更新失败保留错误与节点容器证据，不把失败显示为最新；不得在 GET 中触发远端调用。Panel 的 digest 解析可按 `SET-RUN-024` 配置仓库/镜像主机（仍走 `/v2/<repo>/manifests/<tag>`，Bearer 流程不变）；该配置只影响 Panel 出口检查，不改写镜像引用，也不改变节点 Docker 拉取。
- **不变量**：每节点 target digest 只影响该节点 spec hash；不得引入用户可编辑镜像；手动更新不受对象开关限制；自动更新必须同时满足全局设置开关、计划时间与 `imageAutoUpdateEnabled`；成功路径不得形成自我触发循环。
- **边界**：无网关、无证书/离线、并发检查/更新（互斥或去重）、重复触发幂等、旧库无状态行（首次检查补齐）、P1 回滚到 previous digest 后 pinned 暂停自动更新。
- **验证**：registry 失败/节点离线/更新可用计算、逐节点滚动与失败中止、幂等 no-op、状态投影、手动检查/更新 API、自动更新联动测试。

### FAC-RP-015 入口代理全局运行参数

- **前置**：管理员在设施「设置」页修改入口代理全局参数（80 端口策略、HSTS、`imageAutoUpdateEnabled`、自定义 http 指令等）。
- **动作**：`GET /api/v1/facility-apps/reverse-proxy` 返回 `settings`；`PUT /api/v1/facility-apps/reverse-proxy/settings` 保存。
- **结果**：`httpPortMode` 取 `serve`（默认，80 直接服务）或 `redirect`（仅对 HTTPS 开关开且有证书的域名生成 301 跳转）；`imageUpdateIntervalSeconds`（0..600，默认 0）控制逐网关镜像滚动更新相邻节点间的等待秒数（0=不等待，便于观察/回滚），超时/取消会中断滚动并保留已完成节点；`imageAutoUpdateEnabled` 默认 true；HSTS（P1）默认关且只在 443 server 输出；`serverTokens`（on/off，默认 off）、`clientMaxBodySize`（默认 50m，nginx size 语法）、`tlsMinVersion`（1.2/1.3，默认 1.2）渲染到 http 块（`server_tokens`/`client_max_body_size`/`ssl_protocols`）；保存以 `facility_app_configs.version` CAS，成功后 bump version、重算配置哈希并触发配置平面协调（reload）；旧库空 `settings_json` 归一化为默认值。
- **失败**：非法值结构化拒绝（稳定错误码 + 字段 details）；version 冲突返 409；保存成功但 apply 失败仍保留已保存配置并给出 reload 失败诊断（不自动 recreate）。
- **不变量**：全局参数不改变每域名开关语义；`redirect` 不得把无证书/HTTPS 关闭的域名跳转到不存在的 443；GET 只读本地投影。
- **验证**：默认值/归一化/CAS/非法值、80 策略 × 证书有无 × HTTPS 开关渲染矩阵、reload 失败保留旧 worker 测试。

### FAC-RP-016 每域名 HTTPS / HTTP2 / HTTP3 开关

- **前置**：设施域名记录或应用反代规则保存/编辑；域名证书匹配结果可能不同。
- **动作**：保存域名级 `httpsEnabled/http2Enabled/http3Enabled`（默认 on/on/off）；构建每节点 Nginx spec 与 `routeSummaries`。
- **结果**：`reverse_proxy_routes` 三列持久化，设施域名与应用反代共用；设施 `normalizeInput`、edit-session commit、配置记录读取、应用 normalize/render/映射全链路带出；旧数据/旧 draft/旧 API 缺省按 on/on/off；`httpsEnabled=false` 时不生成该域名 443 server、不下发其证书文件；`http2Enabled` 仅在有效 HTTPS 下输出 `http2 on`；`http3Enabled` 仅在有效 HTTPS 下输出 `listen 443 quic`/`http3 on`/`quic_retry on`/`Alt-Svc`（依赖 FAC-RP-014/§HTTP3 的 UDP 443 发布与 UFW 放行）；80 端口不再输出 `http2 on`；`routeSummaries` 返回三开关与 effectiveHTTPS（开关关闭时 `httpsStatus=disabled`）。
- **失败**：开关关闭时不得残留 443/证书/Alt-Svc；HTTPS 关闭的域名中继回源必须改用 80/http（按同一有效 HTTPS helper）；H2/H3 在 HTTPS 关闭时忽略而非报错。
- **不变量**：证书匹配规则在渲染/摘要/中继三处同源；配置变化只 reload，不因开关本身 recreate；`ssl_early_data` 不输出；HTTPS 关闭仅"不生成 443 配置"，不做默认 443 硬拒绝（已知边界写入说明）。
- **验证**：三开关 × 证书有无 × HTTP/2/3 组合渲染、设施/应用保存往返、旧 draft 缺键默认、routeSummaries、单测覆盖 80/443/quic/证书文件 gating。

### FAC-RP-017 自定义 nginx 指令覆盖（http / server）

- **前置**：管理员填写设施全局 `customHttpDirectives` 或域名级 `customServerDirectives`（每行一条单行指令）。
- **动作**：保存（随域名记录/应用规则或设施设置），渲染每节点 Nginx 配置并 reload。
- **结果**：只允许 `http` 与 `server` 顶层单行指令（`directive args;`）；同名指令覆盖面板生成的同名指令（作用域优先级 server > http > 面板默认），不同名追加；禁止块级/多行/注释与 `listen`、`server_name`、`ssl_certificate*`、`include`、`load_module`、`root`、`alias`、`proxy_pass`、`upstream`、`map`、`if`、`location` 等；每作用域最多 64 行、单行 ≤2 KiB、总量 ≤16 KiB；归一化去空行、重复同名取最后一条；空列表等价未配置（旧数据默认空）。
- **失败**：语法/禁项/超限保存时返回结构化错误（稳定错误码含行号），不落库；合法但语义冲突的指令由节点 `nginx -t` 兜底——该节点 reload 失败、回滚文件并保留旧 worker，错误可见且可修改重试，不得全站切换。
- **不变量**：自定义指令不进入 location/嵌套块；不影响证书/监听/域名身份由面板负责的边界；变更进入该节点配置哈希并触发 reload；设施与应用两来源统一存储与校验。
- **验证**：解析/禁项/行号/上限、同名覆盖与追加、多值指令顺序、渲染哈希变化、edit-session/应用保存往返不丢、reload 失败保留旧容器测试。

### FAC-RP-018 每域名 HTTP 80 端口策略覆盖

- **前置**：设施全局 80 策略为 serve/redirect；单条设施域名记录或应用反代规则需要不同策略。
- **动作**：域名记录/应用反代规则保存 `httpPortMode`（`''`=继承全局、`serve`、`redirect`），编辑会话与配置记录读写、渲染、摘要。
- **结果**：`reverse_proxy_routes.http_port_mode` 持久化（继承写空串）；有效策略=域名覆盖优先，否则设施全局；redirect 仅在 effectiveHTTPS（HTTPS 开关开且有匹配证书）时生成 301，否则按 serve 服务；设施域名与应用反代规则共用同一解析；`routeSummaries[].httpPortMode` 返回实际生效值（无证书时 redirect 投影为 serve）；旧数据/旧客户端缺省为继承。
- **失败**：非 `serve|redirect` 返回 `facility_http_port_mode_invalid`（设施侧）或 `application_reverse_proxy_http_port_mode_invalid`（应用保存侧），不落库；无证书/HTTPS 关闭时 redirect 不得生成跳转。
- **不变量**：域名级覆盖不改变全局设置本身；应用反代与设施域名同表同语义；80 端口策略变化只触发配置 reload。
- **验证**：继承/覆盖/回退/非法值、summary、edit-session 与记录往返、应用保存校验测试。

### FAC-RP-019 每域名 HSTS 与请求体上限覆盖

- **前置**：设施全局 HSTS（默认关）与 clientMaxBodySize（默认 50m）已配置；单条设施域名记录或应用反代规则需要不同值。
- **动作**：域名记录/应用反代规则保存 `hstsEnabled`（nil=继承、true=启用、false=关闭）与 `clientMaxBodySize`（空=继承，nginx size 语法）；编辑会话、配置记录、渲染、summary。
- **结果**：`reverse_proxy_routes.hsts_enabled`（''/on/off）与 `client_max_body_size` 持久化；有效值=域名覆盖优先，否则全局；HSTS 仅在该域名 effectiveHTTPS（HTTPS 开关开且有证书）时输出 `Strict-Transport-Security`（maxAge/includeSubDomains 取全局），body size 在 server 块输出覆盖 http 层；设施域名与应用反代规则共用同一解析；`routeSummaries` 返回有效值；旧数据/旧客户端缺省继承。
- **失败**：非法 body size 返回 `facility_client_max_body_size_invalid` / `application_reverse_proxy_client_max_body_size_invalid`，不落库；无证书/HTTPS 关闭时不得输出 HSTS；继承语义不得被空串意外覆盖成默认值。
- **不变量**：域名覆盖不改变全局设置；HSTS/body size 变化只触发配置 reload；自定义 server 指令仍按同名覆盖规则优先于生成项。
- **验证**：设施/应用两侧继承/覆盖/非法/往返、渲染（含无证书不输出 HSTS、body size 覆盖 http 层）、summary、edit-session 测试。

## 8. 存储共享设施

### FAC-STO-001 配置与乐观锁

- **前置**：配置 0..N 个 `{serverId,root}` 与 version。
- **动作**：PUT storage-share。
- **结果**：合法配置保存于单行 `storage_share_configs(id=storage-share)`，servers_json 保留每节点独立 root，version 成功递增。
- **失败**：版本冲突返回 409；已启用节点 root 改变被拒绝，必须先卸载再启用。
- **不变量**：旧 server_id/server_ids_json 仅迁移回填；稳定读取统一输出 servers 数组。
- **验证**：multi-server/root immutable/version tests。

### FAC-STO-002 根目录安全

- **前置**：配置 storage root 或执行目录/归档/删除。
- **动作**：Panel 与 Agent 分别校验路径。
- **结果**：仅注册 root 内路径可操作；拒绝 `/etc,/var,/usr,/bin,/sbin,/lib,/boot,/dev,/proc,/sys,/run,/home,/root,/tmp` 及子路径。
- **失败**：相对/逃逸/系统路径在任何远端动作前拒绝。
- **不变量**：Agent 不能接受由客户端绕过配置构造的任意宿主路径。
- **验证**：validStorageRoot/path 与 Agent storage boundary tests。

### FAC-STO-003 启用与周期导出协调

- **前置**：新增存储服务器且 Agent/SSH 能力可用。
- **动作**：保存/5 分钟 reconcile。
- **结果**：Panel 侧 SSH 安装 nfs-kernel-server 并 enable/start（无 systemd 回退 service）；Agent 确保服务、root/分区目录、managed exports、`exportfs -ra` 与 UFW 2049 tcp/udp；白名单随服务器增删刷新。
- **失败**：systemd 与 service 都失败才报安装启动失败；配置导出写失败原子回滚并保留用户其它 exports。
- **不变量**：SSH 只用于安装启用；其它存储动作走 Agent；managed block 以固定标记隔离。
- **验证**：storage reconcile/install/export rollback tests。

### FAC-STO-004 移除存储服务器与卸载

- **前置**：配置移除节点或 DELETE facility。
- **动作**：保存移除/卸载。
- **结果**：保存移除前先关闭该节点 export，失败则阻止保存；整体卸载先做引用和挂载门禁，再尽力清理 exports/UFW。
- **失败**：AppSpec 仍引用或运行容器仍挂载 NFS 卷时拒绝；挂载状态查询失败（Agent 不可达、未注入 storage agent、无 endpoint 等）必须 fail-closed 拒绝卸载（`storage_share_mount_status_unavailable`），不得当作未挂载继续；整体卸载远端清理失败时**配置仍保留且 Enabled**，把错误写入 lastError 供重试，partition history/data 保留。
- **不变量**：单节点“保存移除失败阻断”与整体“卸载清理失败保留配置可重试”的语义不得混用；挂载门禁失败与清理失败不得混用。
- **验证**：removed-server cleanup、usage gate、mount-status-unavailable fail-closed、missing-agent fail-closed、cleanup-failure retains config tests。

### FAC-STO-005 storage_share 解析与分区身份

- **前置**：应用 mount source 为指定 storage server 或旧 `storage-share`。
- **动作**：为应用目标节点渲染 runtime spec。
- **结果**：旧 source 选择配置第一台服务器；先 Agent ensure directory，再解析为 NFS `<root>/<storageServerID>/<appNodeID>/<appID>`；按 storageServer×application×appNode×target 唯一登记 partition、target、deterministic volumeName。
- **失败**：仅当应用确含 storage_share 时才因设施未配置/服务器不存在失败；无关应用不受影响。
- **不变量**：设施配置变化进入实例期望 spec hash 并触发重建；应用 stop/delete 释放引用但不删 NFS 数据。
- **验证**：ignore non-storage、multi-server own roots、ensure-directory tests。

### FAC-STO-006 Agent NFS volume

- **前置**：runtime spec 含解析后的 nfs mount。
- **动作**：apply/purge。
- **结果**：Agent 确保 nfs-common，创建确定名 local volume `panel-nfs-<hash>`，driver opts 固定 NFSv4 addr/rw/device；readOnly 在卷驱动层追加 ro；purge 清理无人引用的本地 NFS 卷。
- **失败**：已有同名卷 driver/labels/options 不匹配时冲突，不得接管；远端 NFS 数据永不因本地 volume remove 删除。
- **不变量**：卷名/选项对同一 source 稳定。
- **验证**：nfsvol/docker NFS deterministic/options tests。

### FAC-STO-007 分区下载与删除

- **前置**：存在 partition，应用可能仍引用或卷仍挂载。
- **动作**：GET download 或 DELETE partition。
- **结果**：下载由存储 Agent 打包 tgz；删除在引用与活跃挂载均解除后删目录及记录，204 返回。
- **失败**：仍被 AppSpec 引用或 running container 挂载时拒绝并给出可操作提示；Agent 删除失败保留记录供重试。
- **不变量**：partition ID 必须解析到记录中的 storage server/root，客户端不能替换目标路径。
- **验证**：download handler、usage/mount gate/delete failure tests。

### FAC-STO-008 健康状态汇总

- **前置**：多 storage servers 与 partitions，部分 Agent 离线/超时。
- **动作**：GET status。
- **结果**：30 秒整体上限内并行汇总服务安装、root、export、内核 NFS 是否在服务（优先 `/proc/fs/nfsd/threads`>0，回退 `rpc.nfsd` 进程）和每分区 volume/mounted/writable（写探测 5 秒）；不得仅因现代发行版上 `rpc.nfsd` 启动内核线程后退出而把已加载导出判为未生效；前端每 15 秒刷新且防重入。
- **失败**：单节点错误写入对应 lastError/detail，不得把整组成功项清空或串行拖到无界。
- **不变量**：状态只读，不在 GET 隐式修改配置；导出配置读写由 Agent mutex 串行。
- **验证**：status parallel/partial error/timeout tests。

## 9. 网络探测设施

网络探测设施（`network-probe`）是纯观测型设施：没有隐藏应用/容器，也不产生协调 Job，只保存一份全局配置和每 `(source,target)` 的最新 ping 观测。配置与观测都保存在 AppDB，不进入 metrics 时间序列库。

### FAC-PRB-001 设施边界与数据归属

- **前置**：访问设施目录、`/applications/facility-apps/network-probe` 或服务器详情的探测页签。
- **动作**：读取/保存网络探测设施。
- **结果**：目录卡片与设施详情使用 `networkProbeFacilityApi`（`web/src/api/networkProbe.ts`）；配置读写为 `GET/PUT /api/v1/facility-apps/network-probe`，结果读取统一走 `GET /api/v1/facility-apps/network-probe/results`：默认返回按探测源组织的矩阵（兼容保留），设施详情与服务器概览使用 `?view=targets&history=1` 的按目标聚合视图（所有服务器各一条、24h 跨源聚合历史，见 `FAC-PRB-006`、`UI-FAC-018`、`UI-SRV-027`），单节点结果为 `GET /api/v1/servers/{id}/network-probes`。配置存于 AppDB `network_probe_configs` 单行（`id='network_probe'`，`mode`、`server_ids_json`、`interval_seconds`/`ping_count`/`ping_timeout_seconds`（`FAC-PRB-010`）、`version`、`updated_at`），最新观测存于 AppDB `network_probe_results`（主键 `(source_server_id,target_server_id)`），同表 `history_json` 保存该 (source,target) 最近 24 小时的 30 分钟桶历史（见 `FAC-PRB-009`）。实现位于 `internal/modules/networkprobe`，模块在 `internal/bootstrap/panel/app.go` 装配并注册路由。
- **失败**：不得把探测结果写入 `metrics_snapshots`，不得为探测设施创建隐藏应用或协调 Job；未知 facilityKind 仍按设施空态处理。
- **不变量**：设施读取只查本地投影，不现场 SSH/ping；配置与观测的版本语义互不混用（配置 `version` 只在保存时递增，观测按时间单调覆盖）。
- **验证**：设施路由注册与清单测试（`internal/bootstrap/panel/routes_manifest_test.go`）、`networkprobe` handler/service 测试、设施目录/Mock 路由测试。

### FAC-PRB-002 探测目标与地址解析

- **前置**：存在多台带地址的服务器，`mode=all` 或 `mode=selected`。
- **动作**：生成某探测节点应执行的目标列表（配置下发或结果读取前都按同一规则计算）。
- **结果**：探测节点集合在 `all` 模式下为全部服务器，在 `selected` 模式下仅 `serverIds` 选中的服务器；每个探测节点要 ping **所有其他服务器**（无论模式），服务端强制跳过探测节点自身；目标地址按 `servers.ipv4` 优先、否则 `ipv6`、否则规范化 `host` 解析，三者均无地址的目标跳过；目标按 `serverId` 升序并去重，配置快照缓存 3 秒，避免每轮每秒查库。
- **失败**：服务器列表读取失败时本轮不下发新目标、不覆盖上一份配置；不得把无地址目标或自身作为目标下发；不得因 `all` 模式把非探测节点也当成探测源。
- **不变量**：目标集合只由当前服务器表与配置派生，不得由 Agent 自选目标；节点地址变化后在下一个配置快照周期生效，无需重启 Agent。
- **验证**：`networkprobe` 服务 `ReportConfig` 测试（模式、排序、去重、跳过自身、地址回落、无地址跳过）、3 秒 TTL 快照测试。

### FAC-PRB-003 配置保存与校验

- **前置**：认证用户提交 `PUT /api/v1/facility-apps/network-probe`，body 为 `{mode, serverIds, intervalSeconds?, pingCount?, pingTimeoutSeconds?}`；旧客户端可省略后三个参数。设施页表单以**分钟**输入探测间隔（1..60 分钟、默认 1 分钟，见 `UI-FAC-015`），保存时换算为 `intervalSeconds = 分钟 × 60`；设施 API 仍以秒存储和下发。
- **动作**：校验、归一化、去重并保存配置。
- **结果**：`mode` 只允许 `all|selected`（`networkprobe.ModeAll/ModeSelected`）。探测参数按以下规则归一化后写入同一单行（列 `interval_seconds`/`ping_count`/`ping_timeout_seconds`）：`intervalSeconds` 为 0 或缺省时取默认 60（1 分钟），否则必须在 60..3600（即 1..60 分钟）；`pingCount` 为 0 或缺省时取默认 3，否则必须在 1..10；`pingTimeoutSeconds` 为 0 或缺省时取默认 2，否则必须在 1..10。`selected` 时 `serverIds` 必须非空且每个 ID 当前存在，保存前去空、去重、按稳定服务器 ID 保留。成功写单行配置并 `version` 递增、更新 `updated_at`，返回保存后的完整配置（含归一化后的 mode/serverIds/intervalSeconds/pingCount/pingTimeoutSeconds/version/updatedAt）。
- **失败**：非法 mode 返回 `network_probe_mode_invalid`；参数越界（含负数）返回 `network_probe_interval_invalid`、`network_probe_ping_count_invalid`、`network_probe_ping_timeout_invalid`；`selected` 空集合返回 `network_probe_servers_required`；不存在的服务器 ID 返回 `network_probe_server_not_found`。任一校验失败不得写入或 bump version，也不得部分保存；参数校验顺序固定为 mode → 三个参数 → selected 集合，便于稳定断言。
- **边界**：0/缺省表示“使用默认值”而不是“显式 0”（旧客户端兼容）；负数与越界在保存路径必须拒绝而不是静默夹紧。读取侧对旧 alpha 行、手工数据或越界值做防御性归一化（`loadConfig` 回落到默认 60/3/2），旧版本可能保存的 10..59 秒 `interval` 同样回落默认 60 秒（1 分钟），不得因旧数据把非法探测节奏下发给 Agent，也不得让读取失败。`all` 模式忽略并保存空 `serverIds`（归一化后为空数组），但切到 `selected` 时仍需重新提供非空集合；重复 ID 静默去重而不是报错；删除服务器后的配置修剪见 `FAC-PRB-007`。
- **不变量**：配置是单行 upsert，不存在同一设施多行；保存成功不要求探测目标立即重算（目标仍在下一次配置快照/下发时生效，见 `FAC-PRB-004`）；参数值不参与权限或目标选择语义。
- **验证**：handler/service 校验与归一化测试（三参数 0/缺省→默认、边界值 60/3600/1/10、旧数据 10..59 秒回落默认 60、越界与负数错误码、失败不写库、版本递增、旧客户端省略字段）、读取侧坏数据归一化测试、i18n 错误码测试。

### FAC-PRB-004 配置下发与周期探测

- **前置**：Panel 已与某兼容 Agent 建立 report stream；探测设施已保存配置。
- **动作**：Panel 周期性下发 `AgentReportControl.probe`（`AgentProbeConfig`：`enabled`、`intervalSeconds`、`pingCount`、`pingTimeoutSeconds`、`targets[]`），Agent 侧按参数执行 ping 并上报结果。
- **结果**：探测节点收到自己的目标列表（`AgentProbeTarget{serverId,address,name}`）与设施探测参数；探测周期使用配置的 `intervalSeconds`（默认 60 秒即 1 分钟，范围 60..3600 秒即 1..60 分钟，见 `FAC-PRB-010`），**不再固定 60 秒**；`pingCount`/`pingTimeoutSeconds` 使用配置值（默认 3/2，范围 1..10）。Agent 配置整体替换，支持 disabled 或清空目标；配置变化在下一轮 stream 下发后生效（无需重连或重启）。Agent 部署步骤必须确保目标机系统 `ping` 可用（`AGT-DEP-008`：已有则只探测，缺失则安装 `iputils-ping` 并验证），因此探测能力不依赖运维手工装包。Agent 侧并发上限 8；单目标兜底超时按 `Count*TimeoutSeconds+2s` 计算且不低于 3 秒（`internal/agent/probe` 的 `Options.targetTimeout`），执行 `LC_ALL=C ping -n -c <pingCount> -W <pingTimeoutSeconds> <addr>`（IPv6 用 `-6`），逐目标回报平均延迟、丢包率、发送/接收计数和错误（`AGT-RPT-008`）。
- **失败**：旧 Agent 忽略 `probe` 字段且不上报 `probes`，Panel 不得清空其历史观测，只保持最后一次结果并按 stale 展示；旧 Agent 只理解 `interval_seconds`，对新字段忽略，仍按自身 3 包/2 秒逻辑探测（协议向后兼容，不得因新字段改变旧 Agent 行为）。旧 Panel 不下发 `ping_count`/`ping_timeout_seconds` 时，新 Agent 必须按 0 处理并回退默认 3 包/2 秒；`intervalSeconds` 缺省或 ≤0 时 Agent 兜底默认 60 秒；Agent 不修改探测间隔的上下界：旧 Panel 若下发 10..59 秒，Agent 按原值执行（协议向后兼容），新 Panel 读取侧已把旧数据 10..59 秒归一回默认 60 秒，因此新 Panel 不再下发亚分钟值。探测配置读取失败时按 disabled 下发（只记录日志、不中断指标流），Agent 停止该节点探测，设施恢复后下一轮配置重新启用；服务器已从 Panel 删除时同样按 disabled 下发，不回退旧目标；单个目标 ping 失败不阻断其他目标与指标上报。
- **不变量**：探测只依赖 Agent report stream，不新增 Panel→Agent 的独立 RPC、不新增后台 ticker；探测节点自身不再是目标；非探测节点即使收到空目标也不得执行 ping；Agent 不得自行放大或改写 Panel 已归一化的参数（仅对 `pingCount`/`pingTimeoutSeconds` 做防御性夹紧，探测间隔缺失或 ≤0 才兜底默认 60 秒，不做上下界夹紧）。
- **验证**：`reportHub` 配置解析（含三参数默认/夹紧与探测间隔缺省/≤0 兜底 60 秒）/整体替换/disabled/广播范围测试（`internal/agent/rpc/report_test.go`）；`internal/agent/probe` 的 `Options.Normalize`、目标超时公式、动态 `-c`/`-W` 命令参数、并发与输出解析测试；collector 配置闭包包含三字段的测试。

### FAC-PRB-005 结果上报、落库与新鲜度

- **前置**：Agent 上报 `AgentReport{probes, sampleAt}`，或同一 `(source,target)` 已有历史观测。
- **动作**：Panel 保存探测结果并在读取时判定新鲜度。
- **结果**：`sampleAt` UTC 截断到秒；同一 `source_server_id` 的报告在单事务内先删除该源的全部旧行，再按 `(source,target)` upsert 本次所有目标结果，保证每个 `(source,target)` 只有最新一行；`target` 为空或等于 source 的结果被丢弃；读取时超过新鲜度阈值的结果标记 `stale=true`，阈值按当前配置动态计算为 `max(5min, 3×intervalSeconds)`（`networkprobe.ResultFreshFor` 与 `freshnessFor`，见 `FAC-PRB-010`），不删除旧行、不产生空矩阵。
- **失败**：保存失败只记录日志，不中断 report stream（`AGT-RPT-003`）；目标在保存瞬间已不存在不报错，读取时按当前服务器表过滤掉悬空目标，不得把它显示为有效目标。
- **边界**：同一轮并发/重复上报按“后到覆盖”收敛；旧 Agent 不上报时保留最后一次结果并按当前阈值标 stale；无任何观测时 `results` 为空数组、`updatedAt=null`。探测周期调大（最长 3600 秒）后阈值随之放大为 3 倍周期（最长 3 小时），避免刚上报的结果立即显示过期；周期调小后按新阈值重新判定，阈值本身是读取期派生、不写库。读取侧对旧配置行的越界 interval（含旧版本 10..59 秒）先归一化回默认 60 秒再计算阈值。
- **不变量**：结果行只表达最新观测，不保留历史曲线（24 小时历史另存 `history_json`，见 `FAC-PRB-009`）；读取不现场探测；`stale` 不得进入持久化字段。
- **验证**：`SaveReported` 事务删除+批量插入/时间截断/重复上报收敛测试，`Matrix`/`ServerResults`/`ServerResultsHistory` 的 stale 与目标过滤测试（5 分钟下限、interval=120s→6 分钟、interval=3600s→3 小时、旧配置行归一化）。

### FAC-PRB-006 读取视图：矩阵、服务器结果、按目标聚合与概览卡

- **前置**：已保存配置与部分/全部探测结果，可能包含已删除目标、stale 结果、没有探测结果的服务器或未探测节点。
- **动作**：读取默认矩阵 `GET /api/v1/facility-apps/network-probe/results`、按目标聚合视图 `GET /api/v1/facility-apps/network-probe/results?view=targets&history=1`、单服务器 `GET /api/v1/servers/{id}/network-probes`、概览 `networkProbe` 卡数据。
- **结果**：默认矩阵返回 `mode`、`nodes`（当前探测节点 ID/名称/目标地址）与 `results`（只包含 source 属于当前探测节点且 target 仍存在的行）；结果接口支持可选的 `serverIds` 查询参数（可重复或逗号分隔）只看部分探测节点，缺省/空表示全部节点。`serverResults` 返回 `{serverId,enabled,nodes,results,updatedAt}`，`enabled=false` 表示该节点当前不是探测节点（仍可展示其目标说明与历史行的空态），`nodes` 为该节点的目标列表，服务器不存在返回稳定 not-found；概览卡返回 `{nodes,results,updatedAt}` 摘要（节点数、平均延迟、最大丢包与最差结果由前端派生）。**按目标聚合视图**：查询参数 `view=targets`（忽略大小写）或兼容别名 `targets=1`/`targets=true` 选择以“目标服务器”为中心的聚合读取，`history=1`/`history=true` 时附带 24h 历史；响应为 `TargetMatrix{nodes:[TargetResult],updatedAt}`，每个 `TargetResult` 含 `serverId`、`name`、`address`、`sourceCount`（为该目标贡献结果的探测源数量）、`latencyMs|null`（所有**有效**延迟的平均：仅 `received>0` 且 `error` 为空的结果计入）、`packetLossPercent|null`（所有有结果源的平均丢包）、`worstPacketLossPercent|null`（所有有结果源的最大丢包）、`sampledAt|null`（最新采样时间）、`stale`（有结果时按当前配置的 `freshnessFor` 判定；无结果时默认 `true`）与 `history`（30 分钟桶按目标跨源聚合的平均延迟/丢包，最近 24h、时间升序）。`nodes` **必须包含当前所有服务器**（无任何探测结果的服务器也返回，`sourceCount=0`、聚合值/采样时间/历史为 null 或空、`stale=true`），顺序稳定（按 serverId 升序）；只聚合当前确实作为探测节点的源，target 已不存在的结果忽略；`updatedAt` 取所有结果的最新采样时间，无结果为 null。默认（不带 `view=targets`）行为保持不变，`serverIds` 过滤只作用于默认矩阵视图，聚合视图不使用该过滤。
- **失败**：配置不存在时返回默认 `all` 配置与空结果；未注入探测服务时概览卡返回空 `probes` 而不是 500；已删除服务器作为 target 的行不得出现在矩阵/聚合视图中（服务器列表是聚合节点的来源）；`view` 非 `targets`（或 `targets` 非 `1`/`true`）及非法值回落到默认矩阵契约而不是报错；`history` 非法值按未请求处理；服务器详情读取非探测节点时 `enabled=false` 且前端显示设施入口空态（`UI-SRV-025`）。聚合读取失败不得让设施详情/服务器概览以外的区域失败。
- **边界**：模式切换后旧观测可能来自不再被选中的源，读取时按当前探测节点过滤；`serverIds` 过滤为空表示全部节点，非空只保留选择内的源/目标（默认视图）；JSON 中时间使用 RFC3339 UTC。`view=targets` 下无服务器时 `nodes=[]`、`updatedAt=null`；某服务器只有失败样本时延迟为 null、丢包按失败样本计入平均、`stale` 按采样时间判定；历史跨源聚合只包含已有历史桶的源，不因为某源缺历史而把平均拉成 0。
- **不变量**：默认矩阵/服务器结果/概览卡/聚合视图必须来自同一份本地投影，不得各自引用不同快照；前端不得把缺失值当成 0 延迟或 0 丢包；聚合算法只在服务端实现，前端不得自行用单源结果拼装“所有源平均”。
- **验证**：handler/service 的矩阵/服务器结果/概览卡/`TargetMatrix` DTO 测试（多源平均、`worstPacketLossPercent`、`sourceCount`、无结果服务器仍出现且为 null、只聚合当前探测源、target 已删除忽略、历史桶跨源平均与升序、`history=1` 开关、`view=targets` 别名、默认 view 不受影响）、过滤与空态测试，前端 `networkProbeFacility`/`ProbesTab`/`ProbeOverviewPanel`/概览卡模型测试。

### FAC-PRB-007 服务器删除与配置修剪

- **前置**：删除一台可能同时是探测源、探测目标或 `selected` 配置成员的服务器。
- **动作**：在同一删除清理路径（`internal/modules/servers/deletecleanup.go` 的 `cleanupServerReferences`）中清理该服务器的探测引用。
- **结果**：删除 `network_probe_results` 中 `source_server_id=?` 或 `target_server_id=?` 的全部行；`mode=selected` 时从 `network_probe_configs.server_ids_json` 移除该 ID 并将 `version` 加 1、更新 `updated_at`；`mode=all` 时不改写 `server_ids_json`（`all` 的探测节点集合由服务器表现算）。
- **失败**：清理失败必须让服务器删除返回错误，不得留下指向已删除服务器的探测行或选中 ID；重复删除/清理幂等。
- **边界**：目标服务器删除后，其他探测节点到它的结果行同时清除，矩阵中不再出现该目标；该服务器作为探测源的历史结果全部删除；配置修剪只在 `selected` 下发生，不把 `all` 模式改成 `selected`。
- **不变量**：删除清理必须与服务器删除同事务，不得事后异步补删（否则配置引用会短暂悬空）；`serverIds` 修剪后仍保持稳定排序/去重。
- **验证**：`servers` 删除清理测试（结果行删除、selected 修剪与版本递增、all 不改写、幂等）、再次读取矩阵的行为测试。

### FAC-PRB-008 失败语义与验收证据

- **前置**：Agent 不可达/不兼容、服务器列表为空、所有目标无地址、`ping` 命令缺失、探测结果全部失败、旧 Agent 或没有历史观测等异常场景。
- **动作**：读取/保存配置或结果，等待周期探测。
- **结果**：没有服务器时 `nodes=[]`、`results=[]`、`updatedAt=null`，页面显示明确空态；`selected` 模式下全部所选节点无地址时结果为空但配置合法；`ping` 命令缺失或不可执行时逐目标 `error`（不得让整个探测设施不可用），结果行保留 `sent=0/received=0`；正常情况下 `ping` 由 Agent 部署步骤保证（`AGT-DEP-008`），只有安装失败/被移除等异常才出现该降级；旧 Agent 不刷新观测时保留最后一行并标 stale；服务器详情非探测节点显示空态与设施入口。
- **失败**：任何单目标失败不得升级为设施级失败或阻塞指标上报；配置校验失败不得写库（`FAC-PRB-003`）；读取失败返回 `panelerr` 稳定错误并由页面局部重试，不得用空数据冒充“全部正常”。
- **边界**：结果表本体每个 `(source,target)` 仍只保留最新一行；最近 24 小时历史仅在 `results[].history` 返回（见 `FAC-PRB-009`），不得把历史数组当作“最新观测”；`stale` 是读取期派生，不写库；观测时间与服务器时钟无关，以 Panel 接收的 sampleAt 为准。
- **验证**：`internal/modules/networkprobe` 服务/handler 测试（空集合、无地址、错误目标、stale、旧 Agent 不清空）、`internal/agent/probe` 解析与失败测试、collector 单测、前端设施/探测页签空错态测试；人工观察点：真实 `ping` 权限/命令缺失场景与跨节点网络延迟展示。

### FAC-PRB-009 24 小时探测历史

- **前置**：探测节点按设施配置的探测周期上报结果（默认 60 秒即 1 分钟，可配置 1..60 分钟即 60..3600 秒，见 `FAC-PRB-004/010`；**不再写死 60 秒**）；读取方请求 `GET /api/v1/servers/{id}/network-probes?history=1`（也接受 `history=true`）。
- **动作**：Panel 落库最新观测的同时维护该 `(source,target)` 的 24 小时桶历史。
- **结果**：`network_probe_results.history_json` 保存 `[{"t":<unix 秒桶起点>,"l":<延迟毫秒>,"p":<丢包百分比>}]`，按 30 分钟桶（`1800s`）聚合：同一桶内后一次上报替换该桶，跨桶追加；只保留距本次 `sampleAt` 最近 24 小时内、最多 48 项，时间升序。`history=1` 时每个 `results[]` 附带 `history: [{time,latencyMs,packetLossPercent}]`（RFC3339 时间、延迟毫秒、丢包百分比）；不带该参数时响应与改造前一致，`Matrix`/`CardData` 永不返回 `history`。失败样本（`error` 非空或 `received=0`）在桶历史中记延迟 0、丢包 100；成功样本按本次上报的延迟与丢包值记录。
- **失败**：旧数据或旧 Agent 没有历史时 `history=[]` 或字段缺省，读取不得报错；非法 `history` 值按未请求处理；历史合并失败只影响该 source 的历史（记录日志），不得阻断最新结果落库或指标上报。
- **不变量**：历史只由服务端从已上报观测派生，不由前端拼接；桶宽固定 30 分钟，与探测周期无关（周期大于 30 分钟时每桶最多一次上报，周期小于 30 分钟时同桶替换）；不新增表，`history_json` 为同表列；服务器删除随结果行一并清理历史。
- **验证**：`networkprobe` 服务测试（同桶替换、跨桶追加、24h 剪枝、48 上限、失败样本语义、默认不带 history、探测周期 60s/3600s 下历史桶行为不变、跨源聚合桶平均）、handler 测试（`history=1` 传参、`view=targets` 时返回聚合历史）、`models`/迁移测试、前端设施详情与服务器概览折线图测试（`UI-FAC-018`、`UI-SRV-027`）。

### FAC-PRB-010 探测参数配置（周期/包数/超时）

- **触发入口**：认证用户在设施配置页提交 `PUT /api/v1/facility-apps/network-probe`（表单见 `UI-FAC-015`），或读取 `GET /api/v1/facility-apps/network-probe`；保存后由既有 Agent report stream 下发给探测节点。
- **前置**：探测设施已启用或即将启用；调用方已认证；系统 `ping` 已由 Agent 部署步骤保证（`AGT-DEP-008`）。
- **成功**：配置持久化三列 `interval_seconds`/`ping_count`/`ping_timeout_seconds`，默认 60/3/2；保存时按 `FAC-PRB-003` 归一化并 `version`+1、更新 `updated_at`，返回完整配置；探测节点在下一轮 stream 更新后按新周期与新的包数/单包超时执行 ping，无需重连或重启 Agent；Panel 读取侧对旧行越界值防御性回落默认值（含旧版本 10..59 秒 interval 回落 60 秒），再计算结果新鲜度 `max(5min, 3×intervalSeconds)` 与下发参数。
- **失败**：`intervalSeconds` 不在 60..3600（1..60 分钟）返回 `network_probe_interval_invalid`（错误码不变，中文提示「探测间隔必须在 1 到 60 分钟之间」）；`pingCount` 不在 1..10 返回 `network_probe_ping_count_invalid`；`pingTimeoutSeconds` 不在 1..10 返回 `network_probe_ping_timeout_invalid`；任一失败不写库、不 bump version、不下发新参数；Agent 收到非法/缺失的 `pingCount`/`pingTimeoutSeconds` 时自行夹紧，探测间隔缺失或 ≤0 时回退默认 60 秒且不做上下界夹紧（`FAC-PRB-004`），不得让探测协程崩溃或无限拖住。
- **边界**：0 与缺省（旧客户端/旧 Panel）表示“用默认值”，不是合法显式值；前端表单按分钟输入（1..60，默认 1）并在保存时换算为秒，读取时 `分钟 = max(1, 四舍五入(intervalSeconds/60))`，旧 alpha 行 10..59 秒在读取侧归一回默认 60 秒；负数与越界只允许在保存路径拒绝，读取路径回落到默认值，不得让旧数据导致读取失败。探测周期影响上报频率与 stale 阈值，但不改变 30 分钟历史桶宽（`FAC-PRB-009`）；周期越短故障发现越快（最短 1 分钟），周期大于 5 分钟时 stale 阈值按 3 倍周期放大。**观测入口**：设施详情与服务器详情概览读取按目标聚合视图 `GET /api/v1/facility-apps/network-probe/results?view=targets&history=1`（`FAC-PRB-006`），直接展示所有服务器按目标聚合的结果，不要求用户选择探测源/目标；服务器数量超过 100 台时仍返回全部服务器的聚合条目，设施详情以卡片网格/纵向列表在结果容器内部滚动展示（每卡含 24h 折线图），不得改写为横向矩阵、历史条或整页横向滚动（`UI-FAC-018`、`UI-SRV-027`）。旧 Agent 忽略新字段时保持其自身 3 包/2 秒口径，探测仍可用但不反映新配置；旧 Panel 不下发新字段时新 Agent 回退默认。
- **验证点**：`internal/modules/networkprobe` 服务保存/读取测试（默认值、边界值 60/3600、旧数据 10..59 秒回落、越界与负数错误码、失败不写库、版本递增、旧数据归一化）、`ReportConfig` 下发三参数测试；`internal/agent/rpc/report_test.go` 配置解析（默认/缺省或 ≤0 兜底 60 秒/旧 Panel 缺省，Agent 不对间隔做上下界夹紧）与 `internal/agent/probe/probe_test.go`（`Options.Normalize`、`Count*Timeout+2s` 兜底超时、动态 `-c`/`-W`）；前端 `NetworkProbeFacility` 表单单测（分钟默认 1/范围 1..60/秒↔分钟换算/脏检测/保存 payload `intervalSeconds=分钟×60`）与按目标聚合结果测试（全服务器卡片、探测源数、24h 折线图、无数据空态、无选择器）。

## 10. 指标设施

指标设施（`metrics`）是观测型设施：没有隐藏应用与容器，也不产生协调 Job；只保存一份全局采集配置（采集范围、采集间隔、保留天数与清理计划），指标样本仍只写入 metrics 库 `metrics_snapshots`。原运行时设置中的指标保留天数、指标采集间隔和清理计划移入本设施（`FAC-MET-006`）；容器上报间隔等其它运行时设置保持不变。

### FAC-MET-001 设施边界与数据归属

- **触发入口**：访问设施目录 `/applications/facility-apps`、指标设施详情 `/applications/facility-apps/metrics` 或配置页 `/applications/facility-apps/metrics/config`；直接调用 `GET/PUT /api/v1/facility-apps/metrics`。
- **前置**：认证用户；AppDB 已完成启动迁移（`metrics_facility_configs` 表已由既有 destructive AutoMigrate 创建）；MetricsDB 可读。
- **成功**：目录卡片与设施详情/配置页使用 `web/src/api/metricsFacility.ts`（`config` → `GET`、`saveConfig` → `PUT`，路径 `/facility-apps/metrics`）；配置保存于 AppDB `metrics_facility_configs` 单行（`id='metrics'`，列 `mode`、`server_ids_json`、`retention_days`、`collection_interval_seconds`、`cleanup_schedule`、`version`、`updated_at`）；指标样本继续保存在 MetricsDB `metrics_snapshots`，不因设施化新增指标表或复制历史。实现位于 `internal/modules/observability/metricsfacility`（`NewService`/`NewHandler`/`RegisterRoutes`），在 `internal/bootstrap/panel/app.go` 装配独立注册路由。
- **失败**：不得为指标设施创建隐藏应用、容器或协调 Job；不得把设施配置写入 LogDB/Coordination 库；未知 `facilityKind` 按设施不可用空态处理，不猜测成其它设施。
- **边界**：从未保存且尚未播种时 `GET` 返回默认值（`mode=all`、`serverIds=[]`、`retentionDays=7`、`collectionIntervalSeconds=60`、`cleanupSchedule=daily`、`version=0`、`updatedAt` 零值/`null`），不落行；正常启动会由 `EnsureMigrated` 写入播种行（`version=1`，见 `FAC-MET-006`），此后用户首次保存再递增为 2。设施目录统计失败只影响该卡片，不影响其它设施卡。
- **不变量**：配置 `version` 只在保存成功时递增；指标读取仍走既有 `GET /api/v1/servers/{id}/metrics`（`OBS-MET-003/004`），设施 API 不返回时间序列；设施配置与指标样本的事实源分库，不得混用。
- **验证点**：路由清单测试（新增 2 条 method/path 后计数/哈希同步，`internal/bootstrap/panel/routes_manifest_test.go`）、`metricsfacility` handler/service 测试（`TestHandlerRoutes`、`TestGetDefaultsWhenNeverSaved`、`TestTableConstraintsRejectInvalidRows`）、设施目录与 Mock 路由测试（`web/src/mocks/domainRoutes.test.ts`、`web/src/api/metricsFacility.test.ts`）。

### FAC-MET-002 采集节点选择与下发

- **触发入口**：保存设施配置后，既有 Agent report stream 在每条连接建立时及后续每轮按**该服务器**读取 `ReportConfig` 并下发指标间隔；也可经 `GET /api/v1/facility-apps/metrics` 读取当前选择。
- **前置**：至少一个兼容 Agent 已连接（既有 report stream 链路）；设施配置合法（`FAC-MET-003`）。
- **成功**：`mode=all` 时所有现存服务器均启用采集；`mode=selected` 时仅 `serverIds` 内且当前仍存在的服务器启用采集。Panel 按服务器下发 `metrics_interval_seconds`：启用节点为设施 `collectionIntervalSeconds`，禁用节点为 `0`，Agent 据此停止该节点的指标采集；容器上报间隔 `containers_interval_seconds` 仍取运行时设置（`SET-RUN-014`），不因指标模式改变，容器上报也不因节点被指标禁用而停止。
- **失败**：设施配置或服务器列表读取失败时按 disabled（间隔 0）下发并只记录日志，不中断 report stream、不清空其它节点；服务器已从 Panel 删除时按 disabled 下发，不回退旧间隔。单节点取消失败不影响其它连接。
- **边界**：`all` 模式下 `serverIds` 被忽略；切换 `all`→`selected` 只对未选中的节点停止采集，已存在的历史样本保留（`FAC-MET-004`）；`selected` 删空后没有节点被启用，但配置仍为 `selected`。stream 静默超时取 `min(指标设施采集间隔, containerReportIntervalSeconds)`，既有 10 秒下限逻辑保留，设施读取失败回退默认 60 秒。
- **不变量**：节点选择与间隔只经既有 Agent report stream 下发，不新增 Panel→Agent 独立 RPC、不新增后台 ticker；下发集合只由当前服务器表与设施配置派生，Agent 不得自选节点或放大 Panel 已归一化的间隔。
- **验证点**：`internal/bootstrap/panel/agent_report_collector_test.go` 覆盖按服务器计算间隔（all/selected/已删除节点/读失败回退）、容器间隔不受影响；`metricsfacility` 的 `TestReportConfigAllMode`、`TestReportConfigSelectedMode`、`TestSnapshotsRespectTTLAndInvalidation`、`TestSnapshotReadFailureFallsBackToDefaults`；Agent report stream 配置解析测试。

### FAC-MET-003 配置保存与校验

- **触发入口**：认证用户在指标设施配置页点击「保存」，或直接 `PUT /api/v1/facility-apps/metrics`，body 为 `{mode, serverIds, retentionDays, collectionIntervalSeconds, cleanupSchedule}`（无 `version`/`updatedAt`）。
- **前置**：AppDB 可写；服务器列表可读；请求与当前已保存配置不同（页面仅在脏状态启用保存，但服务端不依赖前端校验）。
- **成功**：`mode` 只允许 `all|selected`；`mode=selected` 时 `serverIds` 必须非空、每项当前存在，保存前去空白、去重、按稳定服务器 ID 升序；`retentionDays` 必须 ≥1；`collectionIntervalSeconds` 必须 ≥1；`cleanupSchedule` 只允许 `hourly|daily|weekly`。`mode=all` 忽略并保存空 `serverIds`（归一化为 `[]`）。校验通过后单行 upsert，`version = 上次 version + 1`、更新 `updated_at`，返回保存后的完整配置。
- **失败**：非法 mode 返回 `metrics_facility_mode_invalid`；`selected` 空集合返回 `metrics_facility_servers_required`；不存在的服务器 ID 返回 `metrics_facility_server_not_found`；`retentionDays < 1` 返回 `metrics_facility_retention_invalid`；`collectionIntervalSeconds < 1` 返回 `metrics_facility_interval_invalid`；`cleanupSchedule` 非法返回 `metrics_facility_cleanup_schedule_invalid`。任一校验失败不得写库、不得 bump `version`、不得部分保存。
- **边界**：与网络探测不同，保存路径中的 `0` 不是“使用默认值”而是非法值（默认值只用于从未保存的读取与迁移播种，见 `FAC-MET-001/006`）；重复服务器 ID 静默去重而不是报错；`mode=all` 时即使提交了 `serverIds` 也归一化为空数组；读取侧对旧行/手工数据的非法值做防御性归一化，不得让读取失败或把非法节奏下发给 Agent。
- **不变量**：配置恒为单行 upsert；保存成功不要求立即重算指标历史（间隔在对齐落库路径生效，见 `FAC-MET-004`）；保存成功后使服务端 3 秒 TTL 配置快照失效，下一次 stream 读取到新值。设施页把「采集范围」与「采集配置」上下堆叠为两个独立区域（范围在上、配置在下）、节点选择器改为紧凑网格（`UI-FAC-019`，不改变本条字段、校验、payload 与归一化语义）。
- **验证点**：`metricsfacility` handler/service 的归一化与错误码测试（`TestSaveValidationErrors`、`TestSaveAllModeClearsServerIDsAndBumpsVersion`、`TestSaveSelectedModeNormalizesServerIDs`、`TestHandlerMapsValidationErrors`、`TestValidationErrorCodesHaveChineseTranslations`；覆盖六类错误码、失败不写库、`version` 递增、`all` 忽略 serverIds、`selected` 空集合、重复 ID 去重、读取侧坏数据归一化）。

### FAC-MET-004 采集间隔与对齐生效

- **触发入口**：保存设施配置后下一轮 report stream 下发；Agent 按新间隔上报指标样本；Panel 的 `handleReport` 处理样本。
- **前置**：节点被设施判定为启用；Agent 已连接且兼容。
- **成功**：新采集间隔在下一轮 stream 生效，无需重连或重启 Agent；启用节点按设施 `collectionIntervalSeconds` 做整点对齐后落库（沿用既有指标整点对齐语义，`AGT-RPT-004`）；`replayed=true` 的补报样本跳过整点对齐但仍按 `(server_id, time)` 幂等落库（`OBS-MET-009`）；禁用节点上报的样本跳过落库并记录 debug 日志，其**既有历史行保留**，读取查询仍返回历史序列。
- **失败**：落库路径（`handleReport`）设施配置读取失败时按 fail-open 回退 enabled/默认 60 秒继续落库并记录日志，不中断流（下发路径按 `FAC-MET-002` fail-closed 下发 disabled）；`metrics.SaveReported` 失败只记录日志，不中断 report stream、不回退其它节点；采集间隔在样本在途期间变化导致对齐不匹配时跳过该样本并记录 debug 日志，不追写。
- **边界**：间隔变更只影响后续样本的落库对齐，不追溯删除或重写历史；禁用→重新启用后从新样本继续，中间空档在指标图中表现为断线而不是补 0；禁用节点不产生新行，不代表服务器不可达（可达性由其它上报/探测判定）。
- **不变量**：指标落库仍只写 MetricsDB `metrics_snapshots`，字段与幂等语义不变（`OBS-MET-001/009`）；整点对齐与重放跳过逻辑不因设施化改变。
- **验证点**：`internal/bootstrap/panel/agent_report_collector_test.go` 覆盖禁用节点跳过落库、启用节点按设施间隔对齐、`replayed` 跳过对齐、读失败回退默认、保存失败仅日志；`metrics.Service.Save` 幂等回归测试。

### FAC-MET-005 保留清理

- **触发入口**：既有 metrics cleanup worker 的周期循环（不新增 ticker/任务类型），由 `internal/bootstrap/panel/app.go` 把 provider 从运行时设置换为指标设施。
- **前置**：设施已迁移（`FAC-MET-006`）；`cleanupSchedule` 为合法值。
- **成功**：worker 通过设施 `CleanupSettings()` 获取 `retentionDays` 与 `cleanupSchedule`（3 秒 TTL 快照，`Save` 后失效），按既有 hourly/daily/weekly 调度独立运行，不属于 event executor worker；每轮只删除超过保留期的 `metrics_snapshots` 行，成功后记录日志。
- **失败**：设施读取失败回退默认（7 天/daily）继续运行并记录日志，不得删除额外行；读取层对非法 `retentionDays`（<1，仅可能来自手工/旧数据）防御性回落默认 7 天，保证下发给清理的保留期总是合法；worker 若仍收到 <1 的保留期（直接调用/装配错误），必须拒绝本轮删除且不删除任何行，不得按 0 天全删；单次删除失败不得终止 worker。
- **边界**：保存更低保留天数后在下一次清理生效；提高保留天数不会恢复已删除行；清理按既有时间/调度语义执行，配置热更新无需重启 Panel。
- **不变量**：清理只作用于 `metrics_snapshots`，不得清理事件、日志、协调 Job 或其它表；worker 结构不变，仅 provider 换源。
- **验证点**：`internal/modules/observability/metrics/cleanup_worker_test.go` 的 `TestCleanupWorkerUsesFacilityProviderSettings`、`TestCleanupInterval`、`TestCleanupDue`（provider 来自设施配置、调度周期、默认回退）；`internal/modules/observability/metrics/service_test.go` 的 `TestMetricsCleanupRejectsInvalidRetention`（retention<1 拒绝且零删除）；`internal/bootstrap/panel` 装配测试；`FAC-MET-003` 的保存校验回归。

### FAC-MET-006 旧设置迁移与设置页移除

- **触发入口**：Panel 启动装配时调用 `metricsfacility.Service.EnsureMigrated`；同时 `GET /api/v1/settings/runtime`（以及分区写入端点 `PUT /api/v1/settings/runtime/*`，`SET-RUN-022`）与设置页「运行时」分区不再包含指标三项。
- **前置**：旧 alpha 数据库的 `runtime_settings` 可能已有旧键 `metricsRetentionDays`、`metricsCollectionIntervalSeconds`、`cleanupSchedule`；`metrics_facility_configs` 可能为空（首次升级）。
- **成功**：`metrics_facility_configs` 无行时读取旧三键播种：合法值（retention≥1、interval≥1、schedule∈hourly/daily/weekly）原样采用，缺失或非法时使用默认（7 天/60 秒/daily）；写入单行（`version=1`、`updated_at` 为播种时间）后**不再读取旧键**（只播种一次）；旧键保留不删除，使旧备份恢复后仍能播种。`GET /settings/runtime` 与分区写入端点（`SET-RUN-022`）以及前端类型不再包含 `metricsRetentionDays`、`metricsCollectionIntervalSeconds`、`cleanupSchedule`，设置页“运行时”分区只保留容器上报间隔、语言、日志等级、协调 trace 与清空事件日志；`containerReportIntervalSeconds` 及其更新语义保持不变（`SET-RUN-014`）。
- **失败**：`EnsureMigrated` 失败必须导致 Panel 启动失败（配置无法可靠建立），不得静默用默认值继续对外服务；设置接口不得因移除字段而对合法请求报错或写入旧键。
- **边界**：表由既有 destructive AutoMigrate 自动创建（旧库升级即为空表），无需手写迁移；旧客户端仍提交三个旧字段时按未知 JSON 字段忽略，不得写回 `runtime_settings` 或影响设施配置；删除旧键后不会重新播种（设计上旧键保留）。
- **不变量**：运行时设置不再读取/写入/校验指标三项（`SET-RUN-006/007/021`）；设置页“运行时”分区不得再出现这三项表单；迁移播种是唯一读取旧键的位置，且只在首次无行时发生。
- **验证点**：`metricsfacility` 的 `EnsureMigrated` 测试（`TestEnsureMigratedSeedsLegacySettings`、`TestEnsureMigratedFallsBackForInvalidLegacySettings`、`TestEnsureMigratedIsOneTime`；覆盖合法旧值采用、非法回退默认、只播种一次、重复启动幂等）、`internal/modules/settings` 服务测试（`GET/PUT` 不再含三项、容器上报间隔仍生效、旧 payload 不恢复语义）、前端设置页 fixture/mocks 同步删除字段（`web/src/mocks/settings.ts`、`web/src/api/settings.test.ts`）。

### FAC-MET-007 服务器删除修剪

- **触发入口**：`DELETE /api/v1/servers/{id}`，在既有 `cleanupServerReferences` 同一 AppDB 事务内执行。
- **前置**：被删服务器可能出现在 `selected` 配置的 `server_ids_json` 中，也可能不存在于其中（`all` 模式或未被选中）；服务器可能已有指标行。
- **成功**：`mode=selected` 时从 `server_ids_json` 移除该 ID 并将 `version` 加 1、更新 `updated_at`；`mode=all` 时不改写 `server_ids_json`（`all` 的采集节点集合由服务器表现算）；该服务器的指标数据行删除仍由既有 OBS-MET-006 路径负责且不得被遗漏。
- **失败**：设施修剪或指标行删除失败必须让服务器删除返回错误（同一事务回滚），不得留下指向已删除服务器的选中 ID 或悬空指标行；重复删除/清理必须幂等。
- **边界**：修剪后 `serverIds` 仍保持稳定排序/去重；删空后配置仍为 `selected` 但没有节点被启用（`FAC-MET-002`），不会把 `all` 改成 `selected`；被删节点仅作为采集节点时，其历史指标删除后其它服务器指标不变。
- **不变量**：配置修剪与服务器删除同事务，不得事后异步补删；指标数据删除路径（`OBS-MET-006`）与设施配置修剪互不替代；不 bump 无关配置或其它设施版本。
- **验证点**：`internal/modules/servers` 删除清理测试（`TestDeleteServerCleansMetricsFacilityReferences`、`TestDeleteServerLeavesMetricsFacilityAllModeAlone`、`TestDeleteServerFailsWhenMetricsFacilityPruneFails`；覆盖 `selected` 移除+version 递增、`all` 不改写、指标行删除仍执行、幂等、失败回滚）、`OBS-MET-006` 回归测试。

### FAC-MET-008 失败语义与验证证据

- **触发入口**：设施配置/服务器列表读取失败、保存失败、页面加载与保存、空服务器、无观测数据、节点被禁用/重新启用等异常场景；由 `GET/PUT /api/v1/facility-apps/metrics` 与设施页面、report stream 共同触发。
- **前置**：系统处于部分依赖不可用或数据为空的状态；调用方已认证（页面/API 均受通用认证约束）。
- **成功**：设施配置读取失败时 report stream 按 disabled 下发（只记录日志）；保存失败不影响已生效的指标流；页面配置读取失败显示局部 `EmptyState` + 重试，保存失败弹全局错误 Dialog 并保留草稿（`UI-SHELL-065`、`UI-FAC-019`）；没有服务器时 `mode=all` 仍然合法、页面显示节点数 0 的空态；没有任何观测样本时指标页/概览显示明确空态，不用 0 或伪造曲线冒充数据。
- **失败**：任何单节点或单次保存失败不得升级为设施级失败、不得阻塞容器上报或其它设施；不得用空数据冒充“采集正常”；不得伪造 `version`/`updatedAt`，也不得在错误响应里泄漏内部路径或配置正文。
- **边界**：`selected` 且选中节点均已删除/禁用时全部节点停止采集但配置仍可读取与保存（保存非空校验只在用户提交 `selected` 时执行）；旧 Agent 按既有协议字段理解间隔，协议向后兼容由既有 report contract 保证；页面文案全部走 i18n（`applicationsPage.metrics*`，见[多语言翻译状态](../i18n-translation-status.md)）。
- **不变量**：失败路径只记录日志/返回稳定 `panelerr` 错误码，不改变已保存配置；验证证据必须能以稳定验收编号或测试名称反查（`GOV-TRACE-002`）。
- **验证点**：`metricsfacility` 服务/handler 的空集合、读失败、保存失败、非法配置测试（`TestGetDefaultsWhenNeverSaved`、`TestSaveValidationErrors`、`TestSnapshotReadFailureFallsBackToDefaults`、`TestHandlerRoutes`）；`internal/bootstrap/panel/agent_report_collector_test.go` 的读失败 disabled 与保存失败仅日志；前端 `MetricsFacility` 组件/模型测试（加载失败重试、保存失败保留草稿、空服务器空态）、`web/src/mocks/domainRoutes.test.ts` 的设施路由与 PUT 校验；人工观察点：真实 Agent 断连/重连后新间隔是否在下一轮生效。

## 11. 用户界面验收边界

### APP-UI-001 信息架构与滚动

- **前置**：桌面/中屏/窄屏进入普通应用、设施目录、详情或配置。
- **动作**：加载长列表/长配置。
- **结果**：普通应用与设施应用是独立入口；桌面最外围填满页头外视口，主从列表和正文内部滚动；窄屏才恢复页面滚动；配置为连续纵向流和 sticky 摘要。
- **失败**：不得恢复顶层 tabs、左 rail、同等级卡片堆叠或依赖横向滚动。
- **不变量**：详情只读，编辑控件仅在独立配置页；操作按钮复用 AppActionButton/Group 和 ConfirmDialog。
- **验证**：前端组件结构、viewport e2e/截图验收。

### APP-UI-002 加载、错误与操作反馈

- **前置**：列表/详情/runtime/设施 API 可独立慢或失败。
- **动作**：进入页面、切换选择、执行操作。
- **结果**：当前页列表摘要先加载；选中详情/runtime 按需异步；骨架不以 0/`jobId` 冒充数据；镜像列只显示 `imageReference`（缺省补详情，失败显示不可用），**不得**用容器名 `panel-<name>`/`jobId` 顶替；应用主徽标仅启停，旁挂健康/异常/部署中三计数（仅展示 count>0 的桶，含列表徽标、详情页头与 Runtime 汇总卡）；节点列表就地显示各节点观测态、Job 阶段或“系统核实中”、尝试次数、下次重试和结构化错误，并可按 operationId 深链；规划失败与 reconcile_stopped 以独立告警展示；设施直达 URL 先加载目录再判断 kind。
- **失败**：旧请求响应不得覆盖新选择；普通应用入口不得预拉设施或多应用 runtime；实例错误不得读取不存在的 `error` 字段而丢失后端 `lastError`。
- **不变量**：危险删除/批量操作必须标准确认；部署请求完成与节点收敛状态分开展示；有界轮询以任一节点活跃 Job 为谓词。
- **验证**：API call-count、race/stale-response、status/error contract、bounded polling model tests。

## 12. 覆盖来源与已知缺口

主要取证来源：

- `docs/agents/modules/applications.md`、`containerization.md`、`tasks-scheduler.md`、`runtime-events.md`
- `internal/modules/applications/`、`internal/modules/facilityapps/`、`internal/modules/networkprobe/`、`internal/modules/observability/metricsfacility/`、`internal/orchestrator/`
- `internal/agent/docker/`、`internal/agent/nfsvol/`、`internal/agent/probe/` 及相应契约/测试
- `web/src/api/applications.ts`、`facilityApps.ts`、`networkProbe.ts`、`metricsFacility.ts`、`containers.ts`，以及 `web/src/types/applications.ts`、`facilityApps.ts`、`networkProbe.ts`、`metricsFacility.ts`、`resources.ts`；探测参数与纵向结果列表的证据还包括 `web/src/utils/networkProbe.ts`、`web/src/views/applications/NetworkProbeFacility.vue` 与 `web/src/views/overview/HistoryStrip.vue`
- `internal/integration/appdeploy/` 与上述模块单元测试

已知缺口（变更触及前应先补证据）：

- 当前仓库没有覆盖所有 UI 布局、焦点、键盘、窄屏和 stale-response 行为的端到端测试，APP-UI 条目仍需人工/组件级验收。
- 设施 edit-session 的独立 heartbeat、全局草稿配额、warning 确认协议与专用结构化 commit/recovery 日志被明确暂缓，不得在后续改动中假定已存在。
- 应用迁移的公开 HTTP 路由在当前 `applications/routes.go` 未出现；APP-LIFE-008 记录现有服务行为门禁，新增/恢复入口时必须先补 API 合同。
- `ApplicationRuntime`/前端类型仍保留 lifecycle 命名兼容结构，但事实源已是 Job/Instance；不得据类型名恢复旧 CoordDB lifecycle 表。
