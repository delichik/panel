# 容器与资源验收合同

## 1. 范围与使用

本文约束 Docker 容器、镜像、网络、卷、本地观测快照、每服务器写队列、Agent Docker 原子能力，以及它们与 Application/设施应用/任务/编排的协调边界。它是后续变更的验收合同，不是用户操作文档或实现教程。

每条稳定编号包含**前置、动作、结果、失败、不变量、验证**。任一结果或不变量不满足即为回归。编号不得因章节重排复用；废弃条目保留编号并明确标记。

## 2. 共用资源读取合同

### RES-SNAP-001 GET 只读本地快照

- **前置**：服务器可能有/无本地容器或 Docker 资源快照，Agent 可能离线。
- **动作**：GET containers/images/networks/volumes。
- **结果**：只读取 AppDB 本地快照，返回 `items`、`observedAt`、`stale`、`refreshing`、可选 `refreshTaskId/lastRefreshError`；无快照返回空 `items` 且 stale=true。
- **失败**：GET 不得因 Agent 离线发起远端调用、创建任务或长时间阻塞。
- **不变量**：items 永远为数组；资源读取与远端刷新严格分离。
- **验证**：假 Agent 零调用、no snapshot、snapshot DTO 测试。

### RES-SNAP-002 容器摘要快照

- **前置**：Agent report 已保存 `container_observations`，可能来自旧数据。
- **动作**：GET container list。
- **结果**：从 `summary_json` 反序列化 id/names/image/imageId/state/status/ports/labels 和托管投影，不读取完整 `container_json`；migration 对旧 observation 回填摘要。
- **失败**：列表不得依赖 command/created/mounts 等完整详情字段或逐容器 Agent inspect。
- **不变量**：Application 协调和资源列表使用同一必要字段快照，完整详情按需调用原子接口而非扩大 report payload。
- **验证**：summary backfill/list-column tests。

### RES-SNAP-003 full report 的 nil 与空语义

- **前置**：服务器已有容器观察。
- **动作**：SaveReportedContainers 接收未携带容器字段（nil）或明确空列表。
- **结果**：nil 保留既有观察；明确空列表原子清空该服务器 observations，并同步清理已消失实例相关 reconcile state。
- **失败**：不得把协议未提供字段解释为“Docker 上无容器”。
- **不变量**：full snapshot 替换在单事务完成，不暴露半旧半新集合。
- **验证**：nil-keeps/empty-clears tests。

### RES-SNAP-004 未知托管容器容错

- **前置**：report 含 `panel.application.managed=true` 但 application_id 已不存在或属于其它 Panel。
- **动作**：保存 observation。
- **结果**：容器观察照常保存用于资源页；跳过带 FK 的 application_reconcile_states 登记并记录限流诊断。
- **失败**：单个残留容器不得回滚整批服务器 observation。
- **不变量**：启动迁移清理存量 orphan reconcile state。
- **验证**：unknown application managed container test。

### RES-SNAP-005 快照刷新状态

- **前置**：同 server/type 存在 queued/running 或最新 failed refresh task。
- **动作**：GET images/networks/volumes。
- **结果**：queued/running 推导 refreshing=true 和 refreshTaskId；最新 failed/failed_retryable 推导 lastRefreshError；成功快照提供 observedAt。
- **失败**：历史失败不得覆盖更新的成功快照状态；字段缺失不能被前端当成 false 数据时间。
- **不变量**：推导只读 LogDB，不启动 executor。
- **验证**：refresh status task ordering tests。

### RES-SNAP-006 首次页面自动刷新

- **前置**：网络或卷页面首次打开且 snapshot stale/observedAt 为空。
- **动作**：前端加载。
- **结果**：自动提交一次 network_refresh/volume_refresh 并等待终态后重载快照；活跃同类刷新被复用。
- **失败**：失败时保留可解释空态和 manual refresh，不循环自动重试。
- **不变量**：同一页面/服务器不得并发重复提交首次刷新。
- **验证**：前端 request-count 与活跃任务复用测试。

## 3. 服务器与 Agent 门禁

### RES-AGENT-001 ready server

- **前置**：请求带 serverId，服务器可能不存在、无 agent URL、不兼容或不可达。
- **动作**：任何需要 Agent 的资源读原子能力/写操作/刷新 task。
- **结果**：仅登记服务器、Agent URL 存在、reachable 且 trait status=compatible 时调用 Agent。
- **失败**：稳定返回服务器/Agent unavailable 错误；mTLS server 证书过期/未生效交服务器模块标记不兼容并按受限策略处理重装。
- **不变量**：资源业务不回退 SSH；Agent 构建版本与 Panel 完全一致才 compatible。
- **验证**：readyServer/handleAgentError tests。

### RES-AGENT-002 错误透传与刷新

- **前置**：Agent Docker API 返回结构化错误。
- **动作**：资源 service 执行。
- **结果**：保留可操作 error code/message/detail，必要时更新 Agent 健康；同步 API 失败直接返回，不伪造 refreshTaskId。
- **失败**：不得把非证书 Docker 错误误标 Agent 不兼容。
- **不变量**：错误处理不在持有 AppDB 写事务时等待网络。
- **验证**：Agent error classification/failure tests。

## 4. 每服务器资源写队列

### RES-QUEUE-001 串行与跨服务器并行

- **前置**：同/不同 serverId 同时提交容器、镜像、卷写操作。
- **动作**：Service.Execute 入队。
- **结果**：同 server 所有普通 Docker 写严格串行；不同 server 可并行；调用方等待自己的动作完成/失败。
- **失败**：同节点不得因资源类型不同绕开队列并发修改 Docker。
- **不变量**：Application Controller 拥有自己的 durable app/server Job 边界，但底层设施/普通资源的节点操作仍不得与共享队列产生未定义并发。
- **验证**：serialize same server/concurrent different servers tests。

### RES-QUEUE-002 panic 隔离

- **前置**：队列 job panic。
- **动作**：worker 执行。
- **结果**：recover 为该请求错误并继续处理后续 job；进程和队列不退出。
- **失败**：不得让等待者永久阻塞或误报成功。
- **不变量**：panic 诊断不得包含 secrets。
- **验证**：runQueue recovers panic test。

### RES-QUEUE-003 同步 API 与刷新分工

- **前置**：普通容器/镜像/卷 mutation 成功。
- **动作**：API 返回。
- **结果**：容器 action/delete 直接 200 且不创建 operation task，后续状态等 Agent report；镜像/卷 mutation 成功后立即创建 image_refresh/volume_refresh 并可返回 refreshTaskId。
- **失败**：Agent mutation 失败时不得先返回成功或创建掩盖失败的刷新。
- **不变量**：网络没有 mutation；资源 GET 不负责刷新。
- **验证**：sync action/no task/refresh result tests。

## 5. 容器

### CTR-LIST-001 容器字段与托管识别

- **前置**：snapshot 包含普通/Panel 托管容器。
- **动作**：GET `/servers/{serverId}/containers`。
- **结果**：显示 id/names/image/imageId/state/status/ports/labels，以及 managed/applicationId/instanceId；无发布端口显示“无端口映射”。
- **失败**：不得用“不可用”描述无端口；managed 只由严格 Label 集判定。
- **不变量**：观测-only manifest labels 不得作为 Docker 身份 label。
- **验证**：managedLabels 与前端空端口测试。

### CTR-LABEL-001 托管身份 Label 最小集

- **前置**：Application 创建容器。
- **动作**：检查 Docker labels。
- **结果**：只写 `panel.application.managed/id/instance.id/generation/spec.hash` 五类身份信息。
- **失败**：缺 managed/id/instance 时不得被当成可协调的托管实例；非托管同名容器不得接管或删除。
- **不变量**：managed-file manifest/error 仅由 Agent report 动态补充到返回 map，不写入 Docker 容器。
- **验证**：managed labels/runtime conflict tests。

### CTR-ACT-001 start/stop/restart

- **前置**：合法 server/container/action，可为普通或缓存识别的 managed Application 容器。
- **动作**：POST `/containers/{id}/{start|stop|restart}`。
- **结果**：通过服务器队列同步调用 Agent，成功200；托管容器也允许直接修改，UI 提示协调可能恢复 desired state。
- **失败**：其它 action 路由 not-found/validation；Agent 错误同步返回。
- **不变量**：直接修改托管容器是节点漂移，不更新应用 desired；后续编排负责修复。
- **验证**：synchronous action、managed action allowed、invalid action tests。

### CTR-ACT-002 删除容器

- **前置**：容器存在或 Agent 能幂等判断缺失。
- **动作**：DELETE container。
- **结果**：同步队列删除并200；托管容器删除后 report 为 missing，应用协调可重建。
- **失败**：不得因旧本地 snapshot 判断存在就绕过 Agent 结果；非托管/托管权限不由可伪造客户端标志决定。
- **不变量**：资源删除不删除 application/instance desired 或 persistent 数据。
- **验证**：delete/drift repair integration tests。

### CTR-LOG-001 容器日志

- **前置**：server/container 可访问。
- **动作**：GET `/containers/{id}/logs?tail=`。
- **结果**：Agent 流式解码 Docker multiplexed frames，返回 containerId/logs；tail 非法/越界规范化，最大10000。
- **失败**：单帧超限停止并报错；截断帧可返回已解码部分并报告协议错误，不得无界内存增长。
- **不变量**：资源日志按 containerId；应用日志授权仍按 instanceId 后端解析。
- **验证**：tail clamp、frame decode/oversize/truncate tests。

### CTR-NET-001 受管 bridge

- **前置**：Agent apply Application/入口网关。
- **动作**：创建或检查容器。
- **结果**：确保 `panel-apps` bridge 存在并加入；入口网关用 Docker DNS 解析同节点应用容器。
- **失败**：托管容器缺该网络时视为 drift 并 recreate；不得恢复 networkMode/host 网络兼容。
- **不变量**：受管网络由 Agent 幂等创建，应用不可自定义。
- **验证**：managedContainerMatchesDesired network test。

## 6. 镜像

### IMG-LIST-001 镜像列表与使用关系

- **前置**：镜像快照、容器摘要和 image_updates 缓存存在。
- **动作**：GET images。
- **结果**：按 container.imageId 与 image.id 精确匹配 inUse/applicationIds；带可解析 tag 的 reference 为 checkable；有应用引用才 upgradeable；合并 local/latest/update/check/error。
- **失败**：不得按字符串 image 名模糊匹配使用关系；普通容器镜像不提供升级。
- **不变量**：RepoTags 为空/dangling 镜像仍可展示但不伪造可检查 reference。
- **验证**：image in-use exact-id/cache merge tests。

### IMG-PULL-001 拉取镜像

- **前置**：用户提交非空 reference。
- **动作**：POST images/pull。
- **结果**：同步进入 server queue；Panel→Agent 与 Agent→Docker pull timeout 均15分钟；无显式 tag 按 Docker CLI 语义显式传 `latest`。
- **失败**：空 reference validation；不得让 Docker API 拉取仓库全部 tags。
- **不变量**：其它 Docker 操作维持常规短 timeout。
- **验证**：explicit latest tag 与 timeout contract tests。

### IMG-DEL-001 单镜像删除门禁

- **前置**：指定 imageId，可能被任一容器使用。
- **动作**：DELETE image。
- **结果**：在同一 server queue 内先实时查询 Docker containers；无人使用才删除并触发 image refresh。
- **失败**：使用中返回 `image_in_use` conflict；检查/删除任一 Agent 失败则请求失败。
- **不变量**：不能只信可能过期的本地 inUse snapshot。
- **验证**：live usage gate/failure tests。

### IMG-DEL-002 删除未使用镜像

- **前置**：Docker 有使用中、未使用、无 ID 或删除失败镜像。
- **动作**：POST images/delete-unused 并确认。
- **结果**：执行瞬间实时获取 images+containers，跳过空 ID/使用中，逐一删除其余；全部成功后触发 refresh。
- **失败**：收集逐镜像失败并使当前请求失败，不把部分成功描述为全部成功。
- **不变量**：批量危险操作必须前端二次确认。
- **验证**：mixed usage/partial failure/confirm UI tests。

### IMG-REF-001 手动/周期刷新

- **前置**：Agent ready，可能存在活跃 image_refresh。
- **动作**：POST images/refresh 或30分钟周期 collector。
- **结果**：创建/复用 resource-exclusive task；远端 registry digest 查询全部在 DB 事务外完成，再以短事务原子替换 `image_updates`/refresh time；成功后前端重载快照。
- **失败**：网络错误记录 lastRefreshError；不得持 SQLite 写锁等待 registry。
- **不变量**：周期仅为 compatible nodes 创建输入；Agent image event 可实时补充 raw image snapshot，但不替代 digest 周期检查。
- **验证**：no write-lock during digest、task reuse/periodic tests。

### IMG-UPG-001 Application 镜像升级

- **前置**：选择若干 applicationIds 或 upgrade-all。
- **动作**：POST `/images/upgrade-selected|upgrade-all`。
- **结果**：202 返回全局互斥 task；task 复用 applications.UpdateImage，更新应用 desired/revision 并交 orchestrator 重部署；普通镜像不直接替换容器。
- **失败**：空/非法应用、无可更新项或单应用失败应形成明确任务结果，不将 Job 收敛状态伪装成 task 已完成。
- **不变量**：selected/all 同类批量不能并行；应用级生命周期仍共用应用并发键。
- **验证**：upgrade task definition/executor/application tests。

## 7. 网络

### NET-LIST-001 只读网络资源

- **前置**：网络 snapshot 存在或为空。
- **动作**：GET networks。
- **结果**：返回 id/name/driver/scope/created/internal/labels 及共用快照状态。
- **失败**：不得暴露创建、删除或修改 API/按钮。
- **不变量**：`panel-apps` 可见但其管理仍归 Agent/Application，不从资源页修改。
- **验证**：routes/UI actions absence test。

### NET-REF-001 网络刷新

- **前置**：Agent ready。
- **动作**：POST networks/refresh。
- **结果**：202 创建/复用 network_refresh，executor 读取 Agent networks 后原子替换 `docker_resource_snapshots(kind=networks)`。
- **失败**：刷新失败旧快照保留，任务失败可 retry，GET 返回 lastRefreshError。
- **不变量**：首次无快照自动刷新只提交一次。
- **验证**：refresh task/atomic replacement/old snapshot preservation tests。

## 8. 卷

### VOL-LIST-001 卷使用状态

- **前置**：volume snapshot 包含 usage data/inUse/containerCount。
- **动作**：GET volumes。
- **结果**：显示 name/driver/mountpoint/created/labels/usage/inUse/containerCount；前端明确区分使用中与未使用。
- **失败**：usage 缺失不得误判为安全可删。
- **不变量**：最终删除门禁以执行瞬间 Agent 查询为准。
- **验证**：snapshot DTO/UI guard tests。

### VOL-DEL-001 单卷删除

- **前置**：卷名存在，可能使用中。
- **动作**：DELETE volume。
- **结果**：同一 server queue 内实时列卷，目标明确未使用才删除；成功触发 volume_refresh。
- **失败**：使用中返回 `volume_in_use` conflict；查询或删除失败请求失败。
- **不变量**：不得通过编码/路径把卷名解释为宿主路径。
- **验证**：live in-use gate/encoded name tests。

### VOL-DEL-002 删除未使用卷

- **前置**：卷集合可能在确认后状态变化。
- **动作**：确认并 POST volumes/delete-unused。
- **结果**：执行瞬间重新查询，跳过空名/使用中，只删除当时未使用项；成功后触发 refresh。
- **失败**：逐卷失败聚合并使请求失败；不得删除确认时未使用但执行时已使用的卷。
- **不变量**：前端必须二次确认且不把旧 snapshot 当门禁。
- **验证**：TOCTOU usage/partial failure/confirm tests。

### VOL-REF-001 卷刷新

- **前置**：Agent ready，可能有活跃 volume_refresh。
- **动作**：POST refresh 或首次自动触发。
- **结果**：202 创建/复用任务，读取 Agent volumes 后原子替换 snapshot；任务可 retry。
- **失败**：失败保留旧快照并通过 lastRefreshError 展示。
- **不变量**：refresh task 的 server/resource concurrency key 保证同节点同类型复用。
- **验证**：task registration/reuse/replace tests。

## 9. Agent Application Runtime 原子能力

### AGRT-ID-001 容器命名与身份冲突

- **前置**：apply 目标名 `panel-<application-name>` 可能已存在。
- **动作**：RuntimeReconcile inspect/reuse_or_replace/create。
- **结果**：只有完整 managed identity 与期望 instance/app 匹配才可复用/替换；创建遇到删除竞态同名冲突可在受限次数内重查重试。
- **失败**：非托管同名资源返回 terminal conflict，绝不能删除或接管。
- **不变量**：停止、状态、日志使用 Instance 保存的 container_name，而非重新由可变名称猜测。
- **验证**：non-managed conflict integration、name conflict retry test。

### AGRT-APPLY-001 apply 步骤与幂等

- **前置**：合法 immutable runtime spec。
- **动作**：Agent RuntimeReconcile apply。
- **结果**：依次 `validate_spec/write_files/ensure_image/inspect-reuse_or_replace/create/start/verify_running`，达到 desired 的资源重复调用成功且不重复破坏。
- **失败**：任一步返回结构化 steps/error/retryable；不提供胖 deploy handler。
- **不变量**：Agent 只 ensure 单节点目标，不选择服务器、不写 Panel DB。
- **验证**：runtime unit + full appdeploy happy/retry tests。

### AGRT-STOP-001 stop 与 purge

- **前置**：容器存在、停止或缺失；managed files/persistent 可能存在。
- **动作**：RuntimeReconcile stop/purge。
- **结果**：stop 删除容器、保留工作文件和 persistent；purge 删除容器及整个应用运行目录；资源已缺失视为幂等成功并返回 observed missing。
- **失败**：身份不匹配终态冲突；removeData 不能在 stop 中误启用。
- **不变量**：NFS 侧共享数据不因 purge 删除，只清理无人引用本地 NFS volume。
- **验证**：stop/purge convergence/finalizer tests。

### AGRT-FILE-001 managed file 全量同步

- **前置**：旧 manifest 和新 desired files 不同。
- **动作**：write_files。
- **结果**：普通文件写临时 sibling 后原子 rename，manifest 最后提交；旧 manifest 管理而新集合缺失的文件删除；从未属于 Panel manifest 的文件保留。
- **失败**：写入/rename/manifest 失败返回错误，不能发布半份新 manifest 为完成。
- **不变量**：父目录0755；普通文件显式 chmod desired mode 并在要求时 chown。
- **验证**：removes stale files/mode/traversable dirs tests。

### AGRT-FILE-002 文件漂移检测

- **前置**：节点保留 managed files 和 fingerprint cache。
- **动作**：Agent full/change report。
- **结果**：普通文件比较 sha256/mode/显式 uid/gid，archive 比较 retained archive sha256+tree hash；size+mtime 未变时用 cache 快速跳过 hash。
- **失败**：cache 损坏/缺失退回实际 hash，不得默认无漂移。
- **不变量**：cache 位于 `state/managed-files.fingerprint.json`，仅优化计算，不是事实源。
- **验证**：managed file fingerprint/drift tests。

### AGRT-FILE-003 archive 落盘

- **前置**：managed archive content 与目标目录。
- **动作**：write managed archive。
- **结果**：保存原包用于 sha256，单遍流式解包，覆盖目标目录删除节点侧多余内容，应用通用 archive 安全限制。
- **失败**：路径逃逸/limit/sha mismatch/写盘失败不标成功。
- **不变量**：archive mount 最终 bind 解包目录只读，不把压缩包本身挂入容器。
- **验证**：archive limits/overwrite/extraction tests。

### AGRT-PERSIST-001 persistent 权限与安全

- **前置**：runtime mount type persistent，source 可能为空或子目录。
- **动作**：prepare mounts。
- **结果**：仅在 `/opt/panel/apps/<appId>/persistent` 内创建目标子目录并应用 UID/GID/mode；容器 bind 的 readOnly 独立生效。
- **失败**：任何 escape 或根外解析拒绝；不得修改 host/global/Docker volume 权限。
- **不变量**：persistent 目录不由数据库保存路径决定。
- **验证**：prepare persistent/escaped directory tests。

### AGRT-NFS-001 NFS 本地卷

- **前置**：Panel 已把 storage_share 解析为 nfs source。
- **动作**：Agent prepare/purge。
- **结果**：确保 nfs-common，创建 deterministic `panel-nfs-<hash>` local volume；opts 使用 NFSv4 addr/rw/device，readOnly 追加 ro；purge 移除无人引用 volume。
- **失败**：同名卷 driver/label 不匹配返回 conflict；NFS 侧数据不删除。
- **不变量**：source split/name/options 对同一输入稳定。
- **验证**：agent docker/nfsvol tests。

### AGRT-STATE-001 applied-state 与 reload

- **前置**：设施应用执行 reload，Docker labels 不可变。
- **动作**：reload/recreate 成功后写 applied-state，随后上报。
- **结果**：仅 container ID/name 与状态文件匹配时，用动态 generation/specHash 覆盖返回 label 观察。
- **失败**：状态文件属于旧容器时忽略，不得掩盖真实 drift。
- **不变量**：Docker 身份 label 本身不修改。
- **验证**：applied-state identity test。

## 10. 与应用编排的协调

### CROSS-APP-001 资源操作与 desired state

- **前置**：用户从容器页修改托管 Application 容器。
- **动作**：start/stop/restart/delete。
- **结果**：动作直接落节点；应用 desired 不变；后续 report 将偏差提交 collector，由 Planner 创建/复用该 app/server Job 修复。
- **失败**：资源模块不得偷偷把应用 enabled/deployment target 改成与节点动作一致。
- **不变量**：同冲突域 active Job 唯一；资源动作不创建旧 lifecycle target。
- **验证**：managed action + drift recovery integration test。

### CROSS-APP-002 report 驱动范围

- **前置**：full report 或 near-real-time container_change。
- **动作**：保存 cache 并检测 drift。
- **结果**：只对已确认 missing/stopped/generation/hash/file drift 的实例请求规划；container_change 可绕过应用 backoff 仅针对明确节点。
- **失败**：不得主动轮询所有兼容 Agent 或强制重部署同应用其它健康节点。
- **不变量**：report stream 共享 Agent collector hub，不能新增重复 Docker polling loop。
- **验证**：drift scope/backoff bypass tests。

### CROSS-APP-003 设施入口网关

- **前置**：反向代理设施配置保存/手动同步。
- **动作**：触发协调。
- **结果**：设施模块提供 per-server spec/update plan；应用 Planner/Controller 执行隐藏 AppDB Instance/Job；runtime 使用 panel-apps、80/443 和 managed Nginx files。
- **失败**：设施保存不直接 Docker、不创建 `application_target_*` task；入口网关自身成功不得再次触发自身形成循环。
- **不变量**：普通应用成功可触发入口网关协调以立即获取新路由。
- **验证**：facility plan/no-loop tests。

### CROSS-APP-004 存储共享

- **前置**：应用 spec 含/不含 storage_share。
- **动作**：per-server runtime rendering。
- **结果**：仅含该 mount 的应用调用 StorageShareResolver、创建 partition 并输出 nfs mount；配置变化改变 desired spec hash。
- **失败**：无关应用不因 facility 未配置失败；应用停止/删除不删除共享端数据。
- **不变量**：partition download/delete/status 由 facility domain API 管理，不混入通用 volume API。
- **验证**：resolver ignore/non-ignore/cross-module tests。

### CROSS-DB-001 数据库边界

- **前置**：资源 snapshot、应用 Job、刷新 task 同时存在。
- **动作**：清理 task/event 或删除应用/服务器。
- **结果**：AppDB snapshots/instances/jobs、LogDB tasks/logs 和 runtime events 按各自生命周期变化；服务器删除通过 FK 清节点 snapshot/instances并让应用配置修剪目标。
- **失败**：task cleanup 不得删 Job；应用删除不得级联绕过 Job RESTRICT；资源 refresh 不得覆盖应用 revision。
- **不变量**：CoordDB 不注册模型，旧 lifecycle 表不得复活。
- **验证**：cross-database cleanup/migration tests。

## 11. 资源界面

### RES-UI-001 信息架构和滚动

- **前置**：桌面/中屏进入软件包、容器、镜像、网络或卷。
- **动作**：选择服务器并浏览长列表。
- **结果**：每类资源独立路由，左服务器选择器+右内部滚动工作区，最外层填满页头外视口且页面本身不滚动；窄屏可恢复页面滚动。
- **失败**：不得退回页内 tabs 或同质通用列表；不得引入 Naive UI/Vuetify。
- **不变量**：沿用 Panel primitives 与各资源独立操作闭环。
- **验证**：组件结构/viewport 截图验收。

### RES-UI-002 操作状态与竞态

- **前置**：快速切换 server、刷新、mutation，旧请求仍在途。
- **动作**：UI 更新。
- **结果**：新选择隔离旧 response；loading/refreshing/error 对应当前 server/type；同步 mutation 等待完成，异步 refresh 跟踪 task 后重载。
- **失败**：旧服务器 items/error/taskId 不得覆盖当前选择；首次空态不得在 refresh 未完成前显示“确实为空”。
- **不变量**：同一时刻同资源列表最多一个主动 reload；批量删除/升级需确认。
- **验证**：前端 stale-response/request dedupe tests。

## 12. API 状态码与失败合同

### RES-API-001 同步与异步状态码

- **前置**：调用已注册 routes。
- **动作**：资源 GET/mutation/refresh。
- **结果**：GET 与同步 mutation 成功200；image/network/volume refresh、upgrade selected/all 成功202 `{taskId}`；storage share reconcile 202（设施合同另述）。
- **失败**：非法 action not-found，校验/冲突/Agent 错误使用统一错误 envelope；不返回含空 taskId 的202。
- **不变量**：URL 中 resourceId/name 作为不透明值 encode/decode，不拼接宿主文件路径。
- **验证**：handler status/route/encoding tests。

### RES-API-002 返回后可观察性

- **前置**：同步动作成功但 report 尚未来，或 refresh task 运行中。
- **动作**：客户端立即 GET。
- **结果**：容器允许短暂显示旧 snapshot并以 observedAt/stale 说明；镜像/卷返回 refreshTaskId/refreshing，任务完成后客户端重载。
- **失败**：不得在同步动作 response 伪造已经刷新后的资源对象。
- **不变量**：eventual observation 不等同强一致读取，UI 文案不得承诺即时快照更新。
- **验证**：sync action then delayed report/task completion tests。

## 13. 覆盖来源与缺口

主要取证来源：

- `docs/agents/modules/containerization.md`、`applications.md`、`tasks-scheduler.md`
- `internal/modules/containers/{routes,handler,service,tasks}.go`
- `internal/agent/docker/`、`internal/agent/nfsvol/`、`internal/agent/contract/`、`internal/agent/rpc/`
- `internal/orchestrator/` 与 `internal/modules/applications/runtime/`
- `web/src/api/containers.ts`、`web/src/types/resources.ts` 及资源/应用页面相关模型
- 容器模块、Agent runtime/NFS、orchestrator 与 `internal/integration/appdeploy/` 测试

已知缺口：

- 当前路由没有通用容器 inspect 详情 API；report 摘要也刻意不含 command/created/mounts。前端类型仍包含这些旧完整字段，不能据类型声明要求列表返回或上报这些字段。
- 容器模块测试名 `TestContainerActionRejectsManagedApplicationContainer` 与实际断言相反：当前合同以实现、模块文档末尾和断言为准——允许直接操作并由协调修复。应后续重命名测试，避免误导。
- 资源页面的请求竞态、首次空快照和桌面/窄屏滚动缺少完整端到端覆盖，RES-UI 条目仍需人工/组件级验证。
- 同步容器 mutation 与 orchestrator/设施 Docker 操作是否在所有路径共享完全同一物理 per-server queue，当前模块说明强制该目标，但代码层由不同所有者组合；修改相关装配前应增加跨模块并发集成测试。
- Docker network 目前只读；若新增 mutation，必须先定义系统 `panel-apps` 保护、实时使用门禁、确认和队列合同，不能直接复用卷/镜像删除逻辑。
