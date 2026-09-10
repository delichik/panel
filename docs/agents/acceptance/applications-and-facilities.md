# 应用与设施应用验收合同

## 1. 目的与判定方法

本文是应用定义、文件与编辑会话、修订、部署入口、变量、入口网关和存储共享的变更验收合同，不是实现教程。任何修改只要改变本域 API、持久化字段、用户可见行为、部署输入或跨模块调用，即使函数名未变，也必须逐条复核本文件。

每条合同包含：**前置**（准备状态）、**动作**（被测行为）、**结果**（必须可观察）、**失败**（拒绝或降级语义）、**不变量**（并发/持久化边界）、**验证**（建议证据）。“结果”和“不变量”任一不满足即视为回归。稳定编号不得因重排而复用；废弃条目应保留编号并标记废弃。

事实源优先级：可执行测试与当前路由/模型 > 当前实现 > 模块说明。发现三者冲突时不得擅自选择对用户影响更大的语义，应记录缺口并补契约测试。

## 2. 应用读取与基本定义

### APP-READ-001 应用列表分页与查询边界

- **前置**：存在多页普通应用、隐藏设施应用、删除中应用和若干实例/Job。
- **动作**：调用 `GET /api/v1/applications?page=&pageSize=&q=`。
- **结果**：返回分页 `items/total/page/pageSize`；单项只含摘要所需字段，`instanceCount`、运行态、活跃 Job 错误和镜像更新状态由当前页 ID 的本地批量查询聚合。
- **失败**：未知参数、非法页码或分页值返回 400；不得默许 `limit` 或 snake_case 别名。
- **不变量**：列表不得解析 AppSpec/YAML、读取完整配置 JSON、访问 Agent/Registry、逐应用读取详情或 runtime；普通列表必须排除 `kind=facility_application` 和 `deletion_requested=1`。
- **验证**：路由契约测试、固定查询次数/假 Agent 零调用测试、跨页 `q` 测试。

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
- **结果**：名称为 1–32 位小写字母/数字/连字符且首尾为字母数字；镜像非空；command 保持有序 argv，不按空格拆分，空白项正规化移除；env key 非空且不含 `=`。
- **失败**：任一字段非法时返回对应稳定 field；不得用示例默认值掩盖缺失输入。
- **不变量**：command 下发 Docker `Cmd`，不转换为 Entrypoint；含冒号的参数按字符串无损往返。
- **验证**：spec validate/render 与前端 model round-trip 测试。

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

## 4. 应用文件与持久编辑会话

### APP-EDIT-001 会话创建与所有权

- **前置**：管理员创建应用或编辑既有应用，可存在可恢复草稿。
- **动作**：`POST /api/v1/application-edit-sessions`。
- **结果**：201 返回 durable session、稳定 `baseResourceVersion(value,updatedAt)`、revision、draft、files、idle/absolute expiry；所有权绑定稳定单管理员主体。
- **失败**：资源不存在或会话状态不可恢复时返回稳定错误。
- **不变量**：修改管理员显示用户名不得隐藏/孤立会话；base timestamp 永远是开始时快照，不被当前应用时间替换；新建资源使用预留 create ID 防重复创建。
- **验证**：edit-session ownership/base-version/recovery 测试。

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
- **不变量**：旧 path/file key 只作兼容输入并原样迁移；两个应用可有同名文件但物理 ID 全局唯一。
- **验证**：handler download、filename sanitization、cross-application same-name 测试。

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

- **前置**：文件新增或替换产生新 immutable session blob。
- **动作**：提交行事务或遭遇 revision/path/DB 冲突。
- **结果**：事务成功后引用新 blob 并删除被替换 blob；事务失败删除新 blob、保留旧引用与旧正文。
- **失败**：磁盘/DB 任一失败不得形成指向不存在 blob 的行或泄漏可立即确认的孤儿 blob。
- **不变量**：workspace 根固定由 `<dataRoot>/tmp/application-save-sessions` 派生，不依赖进程 cwd。
- **验证**：故障注入与 orphan cleanup 测试。

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
- **结果**：应用用户字段与完整文件集在同一 AppDB 事务 CAS；真实配置变化递增 version/config updated_at；完全相同定义和文件集为 no-op；返回 application、resourceVersion、applyRequested 与 diagnostics。
- **失败**：并发输家返回 `resource_version_conflict` 且其应用/文件变更均不落库。
- **不变量**：镜像检查、变量解析、renderer snapshot 等派生刷新不得递增用户 version 或覆盖并发用户字段。
- **验证**：concurrent commit、identical commit、derived-update isolation 测试。

### APP-EDIT-005 配置成功与 apply 请求分离

- **前置**：AppDB 配置/文件已成功提交，但协调器或入口网关触发失败。
- **动作**：commit 收尾或进程重启恢复。
- **结果**：session 仍为 `committed`，`applyRequested=false`，返回 `application_apply_request_failed` warning；不回滚已持久化配置。
- **失败**：恢复仅在预留 ID/精确版本、配置和文件集可验证时确认提交；结果可见但被后续变化遮蔽时转 `conflict/commit_outcome_ambiguous`。
- **不变量**：模糊结果不得重置 active、重复创建或重复应用。
- **验证**：apply failure remains committed 与 ambiguous recovery 测试。

### APP-EDIT-006 TTL、lease 与清理恢复

- **前置**：active/committing/terminal sessions、过期 commit lease、partial/unreferenced/orphan workspace。
- **动作**：周期 cleanup 或启动恢复。
- **结果**：过期 commit lease 先恢复；过期/终态 workspace、过期 partial/unreferenced blobs 清理；无 DB 行 workspace 仅在目录及所有文件均至少陈旧 1 小时后删除。
- **失败**：活跃 lease 或 committing workspace 不得被普通 TTL 清理。
- **不变量**：cleanup 本身与 mutation/commit 串行，且不得根据目录 mtime 单独推断 orphan。
- **验证**：lease/TTL/orphan/race tests。

### APP-EDIT-007 前端事务编辑闭环

- **前置**：进入隐藏的创建/编辑页。
- **动作**：编辑结构化字段、文件、复杂对话框并保存/取消/离开。
- **结果**：流程固定为本地校验→patch→服务端 validate→preview diff→commit；提交期间禁止重复提交和离开；复杂项取消不污染主草稿；成功只表示配置保存并已请求协调。
- **失败**：load 失败时禁止保存；取消按钮显式 discard，会话未保存离开使用统一 ConfirmDialog 保护。
- **不变量**：部署完成只能从协调记录/runtime 观察，不能把 commit 200 解释为运行完成。
- **验证**：editor model/dialog isolation/dirty guard tests。

## 5. 应用生命周期、修订与运行能力

### APP-LIFE-001 保存与启用

- **前置**：合法 session commit，可选择 enabled/disabled。
- **动作**：提交应用定义。
- **结果**：disabled 应用只持久化不规划运行；enabled 应用保存 desired state、不可变 revision 与目标 Instance/Job 后快速返回。
- **失败**：revision 或批量计划写入失败必须回滚控制面请求，不能留下无 revision 的 apply Job。
- **不变量**：HTTP 不直接调用 Agent/Docker；远端突变仅由 Controller 执行。
- **验证**：disabled create、async deploy return、atomic revision-and-plan tests。

### APP-LIFE-002 部署目标选择

- **前置**：存在兼容/不兼容 Agent 与 `all/selected` 部署模式。
- **动作**：部署或保存并应用。
- **结果**：all 对所有健康兼容 Agent 节点各规划一实例；selected 仅规划所选节点，并对被移除旧目标规划 purge。
- **失败**：selected 空目标、不可用 Agent 或不合法目标返回稳定校验；持 persistent 的应用必须且只能选择一个节点。
- **不变量**：单节点失败不阻断其它节点收敛；目标集合在单次 batch 事务中可见，不暴露部分计划。
- **验证**：selected/all、removed target purge、persistent target tests。

### APP-LIFE-003 显式部署/同步

- **前置**：应用可部署，可能处于 disabled 或 `reconcile_stopped`。
- **动作**：`POST /applications/{id}/deploy` 或显式同步。
- **结果**：必要时以 version CAS 启用，清除人工停止协调状态，保存 desired/revision/Job 并返回 operation 结果；已经满足且非 force 的目标不重复规划。
- **失败**：并发配置变化时不得整行覆盖；planner 失败原样报告，不合成 target task/log anchor。
- **不变量**：force 仅绕过满足态和应用级退避，不绕过 app/server active Job 唯一性。
- **验证**：enable CAS、skip satisfied、force planning tests。

### APP-LIFE-004 停止

- **前置**：应用存在一个或多个实例。
- **动作**：`POST /applications/{id}/stop` 或重放 `application_stop`。
- **结果**：只更新必要生命周期列为 disabled、递增用户 version，并为现有实例 desired=stopped/action=stop；stop 删除容器释放端口和名称，但保留 managed files 与 persistent 数据。
- **失败**：协调触发失败按配置已变更/应用请求失败分离呈现；不得回滚为 enabled 或同步直连 Agent。
- **不变量**：旧整行快照不得覆盖并发派生字段/用户编辑；任务 executor 只规划且最终完成自身任务。
- **验证**：stop CAS/concurrent derived fields/task replay tests。

### APP-LIFE-005 重启

- **前置**：已启用应用。
- **动作**：`POST /applications/{id}/restart` 或重放 `application_restart`。
- **结果**：强制 planner 创建/合并 apply Job；任务在规划完成后完成，远端重建由 Controller 执行。
- **失败**：不得直接调用 Agent restart 或制造第二条 active Job。
- **不变量**：running Job 通过 force nonce 记录更新意图，完成 RPC 后比较最新 desired 并 requeue。
- **验证**：restart planning、force nonce、desired-change integration tests。

### APP-LIFE-006 删除 finalizer

- **前置**：应用存在实例、持久数据及终态/活跃 Jobs。
- **动作**：`DELETE /applications/{id}`。
- **结果**：先置 `deletion_requested=1` 并隐藏列表，将实例 desired=purged、Job action=purge/removeData=true；observed=missing 后删实例，全部实例消失后清理终态 Job 并物理删除应用与整个应用运行目录（含 persistent）。
- **失败**：purge 失败保留应用/实例供重试；不得通过 FK cascade 绕过 active Job。
- **不变量**：jobs→applications 为 RESTRICT；应用停止与删除的数据语义不得混淆。
- **验证**：delete finalizer full-chain integration test。

### APP-LIFE-007 持久数据导出与恢复

- **前置**：AppSpec 真正包含 persistent mount；可处于已有单实例或首次部署前。
- **动作**：GET persistent-data 或 multipart POST zip。
- **结果**：已有实例从其节点 `/opt/panel/apps/<appId>/persistent` 打包/全量原子替换并规划强制重启；首次部署前可在选定节点创建并导入，导入成功不触发重启。
- **失败**：无 persistent、目标不唯一、归档为空/路径逃逸或替换失败时拒绝并保留旧数据。
- **不变量**：上传恢复采用临时目录/原子 swap；失败不破坏旧 persistent。
- **验证**：persistent download/restore/predeploy/atomic Agent tests。

### APP-LIFE-008 无损迁移门禁

- **前置**：恰有一个 running 来源实例，指定兼容且无该实例的目标节点。
- **动作**：请求迁移。
- **结果**：仅无 persistent、host/global bind、Docker volume 的应用可迁移；先部署目标成功，再删来源容器/实例目录/instance 行并切换部署目标。
- **失败**：任一门禁、目标冲突或目标部署失败时保留来源运行态与数据。
- **不变量**：不得先删来源再尝试目标。
- **验证**：迁移成功/拒绝/目标失败故障注入。

### APP-RUN-001 runtime 状态与刷新写回

- **前置**：Instance 具有 desired/observed，可能存在 active Job。
- **动作**：GET runtime，必要时主动查询 Agent status。
- **结果**：默认以 AppDB observed 快照派生 status/stage；主动结果也经 ObservationWriter CAS 写回；返回 serverId/serverName、generation、容器身份、错误和 observedAt。
- **失败**：Docker not found 映射 `missing` 而非 stopped；Agent 不兼容/不可达不回退 SSH。
- **不变量**：handler/业务服务不得直接覆盖 observed；无容器的 pending/failed Job 不提供日志入口。
- **验证**：runtime cache/refresh/missing tests。

### APP-RUN-002 实例日志

- **前置**：runtime 实例有后端保存的 container_name。
- **动作**：`GET /applications/{id}/logs?instanceId=&tail=`。
- **结果**：后端按 instanceId 解析容器名并读取日志；tail 限制在 1..10000（缺省使用安全默认）。
- **失败**：客户端传 containerName 不得成为授权/定位依据；跨应用 instance 或无容器返回稳定错误。
- **不变量**：日志展示属于 runtime 实例，不依赖 allocation/task 投影。
- **验证**：handler/service logs tests。

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

### FAC-RP-001 设施边界与隐藏应用

- **前置**：访问设施目录、详情和配置页。
- **动作**：加载 `reverse-proxy`。
- **结果**：目录/详情/配置使用专属 adapter/API；配置存在 `facility_app_configs` 和 `reverse_proxy_routes`，隐藏 identity `facility-reverse-proxy` 只投影 Instance/Job。
- **失败**：不得通过普通 application editor 或通用设施 list API 编辑隐藏应用。
- **不变量**：设施 generation/spec_hash 只由设施模块按 facilityConfigHash 写；应用 snapshot refresh 不得改写。
- **验证**：hidden filtering、generation churn regression tests。

### FAC-RP-002 编辑会话创建与期限

- **前置**：稳定管理员主体请求编辑；可有未结束会话。
- **动作**：POST facility edit-session。
- **结果**：返回 draft/assets/revision/baseResourceVersion；idle TTL 24h、absolute TTL 7d，mutation/validate/preview 延长 idle。
- **失败**：GET 只检查状态不得延长 TTL；过期 session mutation 不得复活。
- **不变量**：既有资产只引用 source_asset_id 和 metadata，不复制正文。
- **验证**：TTL/mutation deadline/source fallback tests。

### FAC-RP-003 设施资产身份与模式

- **前置**：会话上传 text/binary 的 uploaded_file 或 binary uploaded_bundle。
- **动作**：按设施内唯一 assetName PUT/替换/下载/删除。
- **结果**：text 为≤1 MiB 的合法 UTF-8（允许空）；bundle 只能 binary；kind/contentMode 对同名资产不可变；未替换资产下载回退正式 source。
- **失败**：转换 kind/mode、重复 name、损坏上传或删除仍被 static route 引用的资产在 mutation 前拒绝。
- **不变量**：失败不改变 blob/metadata/route/revision；物理 asset ID/key 不属于公开契约。
- **验证**：asset mode/identity/reference/download tests。

### FAC-RP-004 草稿 rebase 与提交互斥

- **前置**：两个会话基于同一 config version 或会话已 conflict。
- **动作**：并发 commit，或 PATCH 当前 baseResourceVersion 做 rebase。
- **结果**：单资源提交锁阻止 rename/DB commit 交错；CAS 仅一方成功；rebase 保留 session assets、更新 base、清冲突后可再次 preview/commit。
- **失败**：删除仍被引用资产、删除仍被 domain/AnyAccess 使用的 gateway、旧 base commit 均阻断。
- **不变量**：commit、recovery、cleanup 共用同一资源锁；长文件阶段续 lease。
- **验证**：concurrent commit/rebase/workspace ownership tests。

### FAC-RP-005 manifest 两阶段恢复

- **前置**：commit manifest 已记录目标/backup/blob/base version，进程可能在目录移动或 DB 事务前后崩溃。
- **动作**：启动或 lease 过期恢复。
- **结果**：DB 未提交则回滚目录并恢复 session；DB 已提交或 config version=base+1 且配置及全部 asset ID/hash 精确匹配则完成收尾并标 committed。
- **失败**：commit 前重算新 blob 与 source asset content hash，缺失/漂移阻断；不满足精确条件不得猜测成功。
- **不变量**：恢复不得重复写配置；applyRequest 失败仍为 committed warning，不能谎报已请求协调。
- **验证**：pre/post DB crash、corrupt blob/source、apply failure recovery tests。

### FAC-RP-006 域名源站与 AnyAccess

- **前置**：配置全局 gateways、domains、originServerIds 与 AnyAccess。
- **动作**：validate/commit/render。
- **结果**：每域名至少一 origin 且属于全局 gateways；AnyAccess 关闭仅源站开放，开启时 relay 按全部非源站或指定子集转发；负载策略为轮询/主备/IP hash，固定 `max_fails=3 fail_timeout=30s`。
- **失败**：新请求空 origin 不得解释为全部；上游模式域名不得与普通应用/Panel 入口共享。
- **不变量**：兼容读取旧空 deploymentServers 时按旧全局集合展开并取 Path 节点并集，避免迁移缩小覆盖面。
- **验证**：normalize origins/relay/render/global ownership tests。

### FAC-RP-007 Path 规则类型

- **前置**：domain 下配置 static/redirect/proxy_pass。
- **动作**：validate、保存、重开与渲染。
- **结果**：static 只接受 uploaded_file/uploaded_bundle；redirect 仅 301/302/307/308 与安全目标；proxy_pass 必须 `http(s)://` 且保持原值和 preserve_source/hide_source。
- **失败**：host_path 返回 `facility_static_site_source_invalid`；非静态规则不因前端默认 sourceType 被资产引用校验误伤。
- **不变量**：非 static normalize 清空 sourceType；proxy_pass 不得被纠正成 static。
- **验证**：service/model route-type tests。

### FAC-RP-008 TLS、静态内容和 Nginx 文件布局

- **前置**：域名可能匹配域名证书、自签用户域证书或无证书。
- **动作**：构建每节点 Nginx runtime spec。
- **结果**：固定镜像 `nginx:1.28-alpine`；主配置目录只读挂载 `/etc/panel-nginx`，证书独立 `/etc/panel-certs`；域名证书优先，其次匹配自签，无匹配仅生成 80；UI HTTPS 摘要与渲染共用匹配逻辑。
- **失败**：不得覆盖镜像 `/etc/nginx` 或单文件 bind 主配置；静态内容由 managed files 只读分发，用户看不到内部 mount target。
- **不变量**：主配置显式 `nginx -c /etc/panel-nginx/nginx.conf` 并保留镜像 mime.types；文件父目录 0755、上传文件 0644。
- **验证**：proxy spec mounts/TLS summary/static serving tests。

### FAC-RP-009 reload 与 recreate 判定

- **前置**：已运行入口网关，新旧 runtime spec 有差异。
- **动作**：设施 provider 计算 update plan 并协调。
- **结果**：仅纯路由/upstream/header/现有挂载证书变化且结构完全一致时允许 validate+reload；镜像、命令、env、网络、端口、mount、权限、资源或 restart 差异必须 recreate。
- **失败**：validate 失败回滚 managed files 并保留旧 worker；reload 或状态确认失败在同服务器串行边界回退 recreate。
- **不变量**：默认/未知策略为 recreate；reload 成功用 applied-state 动态 generation/hash 防静态 label 误报漂移。
- **验证**：reload plan/fallback/applied-state tests。

### FAC-RP-010 配置提交后的协调

- **前置**：设施配置已 commit。
- **动作**：请求 application reconcile。
- **结果**：设施模块只提供 per-server spec 和策略；Planner 写隐藏应用 Instance/Job，Controller RuntimeReconcile；响应继续暴露 reconcileStopped/lastError。
- **失败**：协调不可用时配置仍 committed 并返回 `facility_apply_request_failed` warning。
- **不变量**：不得同步执行远端 Docker、创建旧 lifecycle target 或 `application_target_*` task。
- **验证**：facility apply failure、hidden AppDB Job tests。

### FAC-RP-011 DNS 联动

- **前置**：设施保存新增、修改、保留或删除域名，DNS zone 与服务器地址可能不完整。
- **动作**：触发 `dns_proxy_records_sync`。
- **结果**：全部当前域名及需清理旧域名进入 pending；任务读取并合并数据库中所有 pending；只在期望 A/AAAA 与实际不同时增删改；每域名持久化 pending/synced/failed/skipped。
- **失败**：服务器列表读取失败使任务失败且域名保留 pending，不得按“无服务器”误删；无 IPv4/IPv6 的域名 skipped 并带提示。
- **不变量**：anyAccess 关闭目标=origins，开启目标=全局 gateways；活跃任务期间新增 pending 不得因旧 params 遗漏。
- **验证**：dns sync all-current/merge-pending/server-error/status tests。

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
- **失败**：AppSpec 仍引用或运行容器仍挂载 NFS 卷时拒绝；整体卸载远端清理失败时配置仍删除、partition history/data 保留并在返回 lastError 呈现。
- **不变量**：单节点“保存移除失败阻断”与整体“卸载清理失败不阻断”的语义不得混用。
- **验证**：removed-server cleanup、usage gate、cleanup-failure uninstall tests。

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
- **结果**：30 秒整体上限内并行汇总服务安装、root、export、rpc.nfsd 和每分区 volume/mounted/writable（写探测 5 秒）；前端每 15 秒刷新且防重入。
- **失败**：单节点错误写入对应 lastError/detail，不得把整组成功项清空或串行拖到无界。
- **不变量**：状态只读，不在 GET 隐式修改配置；导出配置读写由 Agent mutex 串行。
- **验证**：status parallel/partial error/timeout tests。

## 9. 用户界面验收边界

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
- **结果**：当前页列表摘要先加载；选中详情/runtime 按需异步；骨架不以 0/jobId 冒充数据；行级 mutation 错误留在对应行/弹窗；设施直达 URL 先加载目录再判断 kind。
- **失败**：旧请求响应不得覆盖新选择；普通应用入口不得预拉设施或多应用 runtime。
- **不变量**：危险删除/批量操作必须标准确认；部署请求完成与运行收敛状态分开展示。
- **验证**：API call-count、race/stale-response、UI component tests。

## 10. 覆盖来源与已知缺口

主要取证来源：

- `docs/agents/modules/applications.md`、`containerization.md`、`tasks-scheduler.md`、`runtime-events.md`
- `internal/modules/applications/`、`internal/modules/facilityapps/`、`internal/orchestrator/`
- `internal/agent/docker/`、`internal/agent/nfsvol/` 及相应契约/测试
- `web/src/api/applications.ts`、`facilityApps.ts`、`containers.ts`，以及 `web/src/types/applications.ts`、`facilityApps.ts`、`resources.ts`
- `internal/integration/appdeploy/` 与上述模块单元测试

已知缺口（变更触及前应先补证据）：

- 当前仓库没有覆盖所有 UI 布局、焦点、键盘、窄屏和 stale-response 行为的端到端测试，APP-UI 条目仍需人工/组件级验收。
- 设施 edit-session 的独立 heartbeat、全局草稿配额、warning 确认协议与专用结构化 commit/recovery 日志被明确暂缓，不得在后续改动中假定已存在。
- 应用迁移的公开 HTTP 路由在当前 `applications/routes.go` 未出现；APP-LIFE-008 记录现有服务行为门禁，新增/恢复入口时必须先补 API 合同。
- `ApplicationRuntime`/前端类型仍保留 lifecycle 命名兼容结构，但事实源已是 Job/Instance；不得据类型名恢复旧 CoordDB lifecycle 表。
