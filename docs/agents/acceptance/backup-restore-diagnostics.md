# 备份、还原与诊断验收规范

本文约束全实例备份、维护模式、覆盖还原、指标/概览和运行时诊断。正常运行态、备份导出维护态与恢复维护态是三个隔离的应用与认证上下文。

## 1. 正常运行期备份入口

- `BKP-API-001`：认证用户调用 `POST /api/v1/backups/export` 时，后端只以0700目录、0600临时文件、fsync和rename原子写入 pending export；不得在正常业务进程checkpoint、复制数据库或归档dataRoot。
- `BKP-API-002`：pending仅保存exportId、createdAt和encrypt标志；加密密码不得写入pending、数据库、任务、日志或响应。
- `BKP-API-003`：pending写入成功后，仅当panel_init控制面socket和token均有效才请求 `backup_export`；返回202包含exportId与真实restartSupported，独立运行panel时不得退出或伪称可重启。
- `BKP-API-004`：pending文件自身不得触发维护模式；只有子进程明确收到 `--maintenance-mode backup_export` 且pending存在时才启动ExportApp。
- `BKP-API-005`：默认全量包必须覆盖dataRoot及app/log/metrics数据库、主密钥和恢复身份所需元数据，并兼容读取旧 `databases/tasks.db`；新增持久化真源必须同步进入manifest/归档合同。
- `BKP-API-006`：归档必须排除tmp下backups、backup-export-pending、restore-pending、restore-pending.previous、restore-staging、maintenance、key-assets和key-asset-exports，防止临时私密材料或旧恢复介质复活。

## 2. 备份导出维护态

- `BKP-MNT-001`：ExportApp只可短暂打开app.db读取管理员用户名与bcrypt哈希后立即关闭；无法读取时只可回退显式配置验证材料，绝不开放匿名API。
- `BKP-MNT-002`：导出维护态只注册独立login/session/logout、export current/start/password/download/exit及 `/maintenance/backup`静态资源；普通业务API和页面路径必须404，未知API返回JSON而非200 HTML。
- `BKP-MNT-003`：未登录可加载维护页面，但current/start/password/download/exit均要求export maintenance token；普通JWT或restore token必须返回401。
- `BKP-MNT-004`：未加密pending登录后进入ready且需显式start；加密pending进入password_required且密码只在提交后进入执行内存，不得落盘。
- `BKP-MNT-005`：正式导出开始后必须依次对app/log/metrics执行WAL checkpoint并关闭连接，再归档稳定文件；任何阶段失败进入failed并返回安全摘要。
- `BKP-MNT-006`：状态必须包含schemaVersion=1、严格递增revision、mode/typed phase、0..100 progress、时间、manifest、download/restart字段、capabilities、retryable、pollAfterMs及结构化errorDetail；不得包含路径、秘密或业务内容。
- `BKP-MNT-007`：执行中phase为checkpointing/archiving/encrypting时pollAfterMs为750且不可重复start；终态按精确phase开放download/exit能力，UI不得从旧字符串猜测能力。
- `BKP-MNT-008`：下载只允许completed、downloadAvailable且URL中的exportId精确匹配；仅在完整非 Range GET 响应体成功发送完毕后记录已下载；HEAD 与 Range 可返回内容但不得确认下载；不把downloadedAt暴露到状态。
- `BKP-MNT-009`：completed但从未记录下载时exit必须返回409并保留归档和pending；已完整下载后exit清理临时文件并请求normal重启，清理失败不得发送重启信号。
- `BKP-MNT-010`：failed状态允许exit清理但不得把失败归档展示为可下载成功；restart不支持时必须明确要求外部重启而非退出当前进程。

## 3. 归档格式与加密安全

- `BKP-ARC-001`：manifest必须含formatVersion、panelVersion、UTC createdAt、encrypted、includes，以及每个相对文件的size与SHA-256；恢复解包时必须流式逐项核对 size/SHA-256，拒绝多余、重复或缺失成员；符号链接不得跟随写入归档。
- `BKP-ARC-002`：归档路径必须拒绝绝对路径、`..`穿越、重复/歧义目标和指向归档根外的路径；验证失败不得写入目标dataRoot。
- `BKP-ARC-003`：加密头的salt、nonce、ciphertext uint32长度必须同时受剩余输入上限校验；伪造超长段应立即失败，不得按声明值分配巨量内存。
- `BKP-ARC-004`：写入任何超过uint32范围的段必须返回错误而非截断；底层写错误必须向上返回，不能产出看似成功的归档。
- `BKP-ARC-005`：密码缺失、错误、格式不支持、认证失败和普通损坏必须映射为 `restore_password_required|restore_password_invalid|restore_compatibility_failed|restore_archive_invalid`，响应不得区分可用于探测秘密的内部细节。

## 4. 正常运行期恢复入口

- `RST-API-001`：preflight multipart必须含file，单次上传上限8GiB；超限返回413 `restore_archive_too_large`，无文件或非法multipart返回稳定4xx，临时上传始终清理。
- `RST-API-002`：`POST /backups/restore/preflight` 只读取、解密并验证manifest，返回manifest、encrypted、passwordRequired；不得写pending、停止服务或修改现有数据。
- `RST-API-003`：`POST /backups/restore/confirm` 必须要求表单 `confirmOverwrite=true`，否则 `restore_confirmation_required`；确认不替代密码与归档完整性验证。
- `RST-API-004`：confirm必须在发布pending前完整读取manifest并固化当前管理员用户名+bcrypt哈希为维护认证快照；该快照0600保存且不进入API/备份。
- `RST-API-005`：pending发布必须在dataRoot/tmp内0700临时目录用O_EXCL创建0600 archive/marker，记录真实SHA-256与size，fsync文件/目录并整组rename；半发布不得被当作有效pending。
- `RST-API-006`：替换已有pending时必须先收紧旧权限并以 `restore-pending.previous`保留最后一致对；digest不符、symlink、非普通文件或非法basename的target不得淘汰previous。
- `RST-API-007`：pending发布成功后才请求 `restore`重启并返回pending=true与真实restartSupported；重启不可用时不得自动退出，管理员可手动以明确mode重启。
- `RST-API-008`：普通pending本身不能让normal启动进入恢复HTTP应用；但已建立事务state或protected media表示未收敛覆盖事务，任何启动mode都必须优先恢复。

## 5. 恢复维护认证与最小路由

- `RST-MNT-001`：RestoreApp只注册独立login/session/logout、status/password/retry/clear-pending和 `/maintenance/restore`资源；未知API返回JSON404，业务页面与其他设置页不得SPA回退。
- `RST-MNT-002`：status/password/retry/clear全部要求restore maintenance token；普通JWT和export token不可用，前端必须使用独立sessionStorage key。
- `RST-MNT-003`：维护登录先使用pending认证快照；旧pending无快照时依次尝试仍可读app.db和显式非默认配置哈希；数据库损坏且仅默认admin/admin时必须拒绝启动维护应用。
- `RST-MNT-004`：维护登录按来源IP连续5次失败锁15分钟，成功清零；最多保留32个内存session，过期验证时清理，logout只撤销当前维护上下文。
- `RST-MNT-005`：状态只显示phase、progress、备份时间/版本、是否加密、能力与安全错误摘要；数据库/密钥路径、凭据、私钥、任务参数和业务记录不得出现。
- `RST-MNT-006`：未加密归档可自动执行；加密归档必须进入password_required，错误密码保持该可恢复流程且不得保存密码。

## 6. 维护命令并发、幂等与状态机

- `MNT-CMD-001`：start/password/retry/clear-pending必须在同一状态锁中完成phase校验、expectedRevision校验、幂等检查和running迁移，锁外才能启动工作；并发请求最多启动一个执行。
- `MNT-CMD-002`：客户端可用JSON `clientOperationId`或 `Idempotency-Key`，两者同时存在必须相同，最大128字符；相同命令+键重放返回首次接受结果且不重复副作用。
- `MNT-CMD-003`：同一幂等键用于不同命令必须409；expectedRevision陈旧必须409并返回/允许刷新当前状态；无幂等键的旧客户端仍受原子phase迁移保护。
- `MNT-CMD-004`：非法phase下的start/password/retry/clear必须409且revision/文件/事务不变；不能以“已经运行”为成功再次启动。
- `MNT-CMD-005`：加密恢复retry必须先回到password_required，清除此前明文密码；只有新的password命令才继续执行。
- `MNT-CMD-006`：clear-pending必须明确危险确认并仅在capability允许时展示；applying期间、rollback未完成或阻断状态下绝不允许clear。

## 7. 覆盖事务、回滚与重启恢复

- `RST-TXN-001`：真正覆盖前必须完成解密、解包、manifest/hash/path验证；任何预备失败都不得移动或清空当前dataRoot/外置数据库。
- `RST-TXN-002`：apply前必须把archive、marker和认证快照移动到dataRoot同级 `.<name>-restore-transaction/media`；protected media是retry/rollback唯一可信介质且优先于后来普通pending。
- `RST-TXN-003`：state.json必须持久记录prepared、applying、各目标backup_planned/backup_moved/swapped、rolling_back/rollback_renaming/rolled_back和committed；每次破坏性rename前先落盘下一可恢复意图。
- `RST-TXN-004`：dataRoot的stage/backup位于其同级；配置在dataRoot外的数据库必须在各自目标同目录stage/backup，确保只使用同文件系统rename而非跨卷非原子移动；外置 SQLite 主文件换入前后必须清除目标路径旁的 `-wal`/`-shm`/`-journal`，避免旧 WAL 接到新库。
- `RST-TXN-005`：任一目标apply失败必须逆序rollback所有已动目标；新创建但尚未完成state flush的目标也要清理，原数据必须恢复。
- `RST-TXN-006`：`rollback_renaming`恢复必须识别“rename已成功但状态未落盘”，无需停机期全量hash即可继续；重复冷启动应收敛而非反复交换。
- `RST-TXN-007`：成功rollback保留protected media并允许retry/clear；backup缺失、state损坏或rollback失败进入阻断状态，禁止retry、clear和normal重启。
- `RST-TXN-008`：state检查只有明确不存在才能normal启动；权限/I/O/解析不确定必须fail closed进入错误/恢复路径。
- `RST-TXN-009`：只有所有目标均committed后才能删除旧backup、state和media，并请求normal重启；清理或重启请求失败必须可诊断，不得把未收敛状态当普通完成。
- `RST-TXN-010`：clear-pending成功必须删除普通pending或已允许清理的recovery介质并请求normal；重启不支持时保持服务并明确结果，同一幂等键重放不重复生命周期动作。

## 8. 指标采集、查询与保留

- `OBS-MET-001`：指标只接收compatible且有URL的Agent report；保存时绑定服务器ID并将时间UTC截断至秒，包含CPU、内存、磁盘用量、磁盘IO读写速率、网络、load1/5/15和系统状态字段。`metrics_snapshots.disk_read_bytes_rate`/`disk_write_bytes_rate` 为可空列：NULL表示该样本未测得磁盘IO（旧Agent或采样未就绪），不得写0冒充真实速率。磁盘用量在未重新采样的样本中写0，不得据此认定磁盘为空（`OBS-MET-007`）。保存前按 `(server_id, time)` 幂等去重：同一秒样本已存在时直接跳过，不覆盖、不重复插入（`OBS-MET-009`）。被指标设施判定为禁用的节点（`FAC-MET-002`）不得写入新样本：Panel 跳过落库并只记录 debug 日志；其既有历史行保留、不得删除，读取仍返回历史序列（`FAC-MET-004`）。
- `OBS-MET-002`：指标成功保存后可标记服务器可达；保存失败不得伪标可达或中断report stream。被指标设施禁用的节点没有新指标样本，不得据此把服务器标记为不可达/可达（可达性与上报链路健康由 Agent report/容器上报判定，`AGT-RPT-004`）；禁用不影响容器上报。
- `OBS-MET-003`：`GET /servers/{id}/metrics` 默认1h，只接受 `1h|6h|1d|24h|7d`（前端公开子集可更窄）；非法range返回 `range_invalid`，存在但无数据返回所有序列为空数组。可选查询参数 `bucket`（秒，60..86400）：合法时按时间桶聚合该服务器的 CPU/内存/网络/load/diskIo 序列（复用批量桶聚合语义，点数有界），并在响应中附加 `latest` 当前原始样本（`time/cpuUsagePercent/memoryUsedBytes/memoryTotalBytes/diskUsedBytes/diskTotalBytes/diskSampledAt/load1/rxBytesPerSecond/txBytesPerSecond`，上下行取最新原始样本，磁盘字段取最近一次有效磁盘行，无有效样本时 `diskUsedBytes/diskTotalBytes=0` 且 `diskSampledAt=null`）；未传或 `bucket=0` 时保持原始序列、不返回 `latest`。非法 `bucket` 返回 `bucket_invalid`。首页卡片使用 `range=1d&bucket=1800` 获取 24h 历史与当前值（含实时上下行）。
- `OBS-MET-004`：查询结果按时间升序，CPU/内存/网络/load/diskIo各序列共享样本时间语义；磁盘使用率不再返回历史序列（响应不含 `disk`），只返回 `diskCurrent`（范围内最后一次 `disk_total_bytes > 0` 的有效采样，字段 `time/usedBytes/totalBytes`，没有则为 `null`），前端不得为磁盘画曲线。`diskIo` 只包含至少一侧速率非 NULL 的样本，未知的一侧按0序列化但不得回写成历史事实。增量查询只返回严格晚于按秒对齐since的点；`diskCurrent` 是当前值语义，不参与桶降采样，概览 `disk` 卡直接使用每服务器最新有效磁盘行。
- `OBS-MET-005`：清理worker按指标设施的 `retentionDays` 和 `hourly|daily|weekly` `cleanupSchedule` 独立运行（`FAC-MET-001/005`），不属于 event executor worker；设施读取使用 3 秒 TTL 快照并在保存后失效，读取失败回退默认 7 天/daily 且只记录日志，读取层对非法保留期（<1）防御性回落默认 7 天；worker 收到小于1的保留期（直接调用/装配错误）必须拒绝本轮清理且不删除任何行。清理成功只删除超过保留期的 `metrics_snapshots` 行，不清理事件、日志或其它表。
- `OBS-MET-006`：服务器删除必须删除其全部指标；其他服务器指标和MetricsDB结构保持不变。该指标行删除与服务器删除同一事务，并与指标设施 `selected` 配置修剪（`FAC-MET-007`）一起提交，不得只删其中一半。
- `OBS-MET-007`：磁盘使用率低频采样与“仅当前值”。**触发入口**：Agent指标上报（`internal/agent/rpc/report.go` 的 `metricCache`）与 `GET /servers/{id}/metrics`、概览 `disk` 卡。**前置**：新Agent磁盘使用率采样节拍固定为5分钟（`diskUsageSampleInterval`），每轮上报用 `disk_usage_sampled` 标记本轮是否重新采样磁盘使用率。**成功**：Panel只把 `disk_usage_sampled=true` 的样本按其用量/总量落库，其余样本磁盘两列写0；旧Agent不带该标记（nil）时按“每次样本都携带磁盘值”兼容保留。查询只返回 `diskCurrent`，概览磁盘卡只显示当前用量/总量与采样时间。**失败**：磁盘读取失败时该轮不递增采样标记，样本按未采样（0/0）处理，不得把失败样本写成有效磁盘值或让整个指标上报中断。**边界**：首次启动在所有指标（含磁盘用量）至少成功观测一次前不提交指标；此后磁盘使用率不参与15秒全量新鲜度门槛（`AGT-RPT-004`），因此历史行中的0只表示未采样。**验证点**：`internal/modules/observability/metrics` 的 `Save/Query/QueryMany` 测试（含 `diskCurrent` 取最后有效行、0值被跳过）、Agent指标缓存新鲜度与磁盘标记单测、前端指标卡/概览卡模型测试。
- `OBS-MET-008`：磁盘IO读写速率。**触发入口**：Agent每1秒读取 `/proc/diskstats` 累计扇区数，经指标上报写入 `metrics_snapshots.disk_read_bytes_rate`/`disk_write_bytes_rate`。**前置**：只汇总 `/sys/block` 列出的整盘设备行，排除分区行以避免整盘与分区重复计数，并排除 `loop`/`ram`/`zram` 伪设备；内核块层扇区单位固定为512字节，与设备逻辑扇区大小无关；`/sys/block` 或 `/proc/diskstats` 读取失败时本轮磁盘IO不可用。**成功**：按网络速率的同一窗口口径，在内存累加相邻读数差，上报整点时消费累加值并除以自上次提交以来读数窗口的实际时长，得到读/写字节每秒；`disk_io_available` 为 true 时两列写实际值。**失败**：`disk_io_available` 为 nil（旧Agent）或false时两列写NULL表示未知，不得写0；两个速率都未知的样本不产生 `diskIo` 查询点。**边界**：磁盘IO是可选能力，不参与15秒全量新鲜度门槛（否则平台不支持 `/proc/diskstats` 时全部指标会停摆）；累计读数超过15秒未更新或读取失败时本轮按未可用上报，面板保留旧值而不是伪造0；计数器回退（设备重建、计数器回绕）按0增量跳过；单侧已知、单侧未知时只输出已知侧。**验证点**：Agent侧 `/proc/diskstats` 解析/整盘过滤/窗口累计/伪设备排除/回退单测（`internal/agent/system/diskio_test.go`），Panel指标保存NULL语义与 `diskIO` 卡桶平均测试，`overview` 卡片序列选择测试。
- `OBS-MET-009`：指标补报与幂等。**触发入口**：Agent report stream `Send` 失败（`internal/agent/rpc/report.go` 的 `reportHub`）与重连后的补发。**前置**：指标发送失败时，把失败报告与该watcher待发队列中携带指标的样本放入进程内内存环形缓冲，容量12条、最老10分钟；只保留 `Metrics != nil` 且样本时间未超龄的条目，并把每份报告裁剪为纯指标报告（保留 `sampleAt`/`reason`/`metrics`，不重放容器、Tailscale 等可能过期的观测）。**成功**：新连接建立后、进入正常发送循环前，按样本时间升序逐条补发并置 `replayed=true`；任一条发送失败则把剩余样本恢复到缓冲队首并结束本轮，等待下次重连。Panel对 `replayed=true` 的样本跳过整点对齐校验（`AGT-RPT-004`），但仍校验服务器身份与时间合法性，并按 `(server_id, time)` 幂等去重，重复重放不产生重复行、不覆盖既有值。**失败**：补报发送失败、Panel保存失败都只记录日志，不中断流、不回退为其他上报通道；缓冲满时丢弃最老的指标样本（丢弃的是未接受的事实，不得伪报成功）。**边界**：非指标报告不缓存；缓冲不持久化，进程重启丢失的仅是内存中尚未发送的样本，已被Panel接受并落库的样本不受影响（符合 `ARCH-CON-003`：内存队列只降低延迟）；补报只补缺，不覆盖同秒已有行。**验证点**：`reportHub` 补报队列容量/年龄/升序/失败恢复/裁剪为纯指标单测（`internal/agent/rpc/report_test.go`）、`agent_report_collector` 对 `replayed` 跳过对齐且失败仅日志的测试、`metrics.Service.Save` 去重测试。

## 9. 概览与卡片配置

> **2026-11-16 起首页不再消费本节接口**：`/overview` 改为多接口并行取数（`servers` + 每服务器 `metrics` + 所选探测源 `network-probes` + `packages`/`images`），前端不再调用 `GET /overview`、`/overview/cards*` 与卡片数据接口。本节后端契约继续成立并保留，直至旧接口与 `overview_card_configurations` 表按 `GAP-031` 单独清理；清理前不得新增前端消费者，也不得据“无消费者”删除契约。

- `OBS-OVW-001`：`GET /api/v1/overview` 必须对服务器批量聚合最新指标、load、包更新数和刷新时间，避免逐服务器N+1；最新指标少于5分钟才标metricsFresh。
- `OBS-OVW-002`：首次 `GET /overview/cards` 必须幂等创建固定default配置；之后读取按原有顺序完整返回，空数组是合法配置。
- `OBS-OVW-003`：`PUT /overview/cards` 整套原子替换，最多100卡；ID非空且唯一、kind在白名单、width 1..6、height 1..4、range合法、networkDirection为rx/tx/both、serverIds非空项且卡内唯一。
- `OBS-OVW-004`：验证失败不得覆盖旧卡片JSON；成功后重启仍保持顺序、尺寸、范围和服务器选择。
- `OBS-OVW-005`：`GET /overview/cards/{id}/data` 对不存在ID返回404；非指标卡返回空metricsByServer和bucketSeconds=0；指标卡空serverIds表示全部现存服务器，非空只查询仍存在的选择；指标卡只物化该卡kind对应的序列：`cpu`/`memory`/`network` 分别物化同名序列（load不参与概览），`disk` 只物化 `diskCurrent`（不再物化磁盘历史序列），`diskIO` 只物化 `diskIo`；未物化的序列返回空数组。`networkProbe` 卡不是指标卡：响应返回 `probes`（`nodes`：探测节点ID/名称/目标地址；`results`：源/目标、延迟、丢包、收发计数、采样时间、错误与stale标记；`updatedAt`：最近采样时间，无数据为null）且 `metricsByServer` 为空对象、`bucketSeconds=0`，`serverIds` 为空表示不过滤探测节点，非空只保留落在选择内且目标仍存在的节点/结果；未注入探测提供方时返回空probes而不是报错。
- `OBS-OVW-006`：指标卡数据必须服务端降采样：按range和本次查询的服务器数量选择固定时间桶（响应携带bucketSeconds），保证单卡点数有界且与原始采样频率无关；合法 `since` 先向下对齐到桶起点，并返回从该桶开始、完整重算的桶；非法RFC3339Nano返回422；多服务器必须批量查询并为无数据服务器保留空Series，不能用0填缺失采样。`disk` 卡仍返回 `bucketSeconds` 但只返回 `diskCurrent`（当前值不随时间桶变化），不因桶产生曲线点；`diskIO` 卡按桶分别平均读/写速率，两个速率都NULL的桶不产生点；`networkProbe` 卡不降采样、不受 `since` 影响，直接返回最新观测（卡片契约见 `OBS-OVW-005`）。
- `OBS-OVW-007`：自动刷新前端必须防重入、页面隐藏/离线/编辑态暂停、切换或卸载丢弃迟到响应；增量按桶对已有序列做后缀替换，不得把同一未满桶重复追加；`bucketSeconds` 变化时必须整卡重载；失败静默保留旧数据并下周期重试，不弹误导toast。`disk` 卡是当前值、无时间序列：每轮用最新 `diskCurrent` 覆盖，不参与桶后缀合并；`networkProbe` 卡无桶，每轮以最新 `probes` 覆盖。
- `OBS-OVW-008`：概览卡片的时间轴对齐、跨服务器聚合、最新值与峰值必须在数据落地时派生一次并缓存，渲染期只读缓存；单卡派生复杂度与总点数线性，禁止渲染期做逐点查找（O(n²)）或 `Math.max(...values)` 式参数展开。

## 10. 诊断快照

- `DIAG-SNP-001`：只有认证用户可调用 `GET /api/v1/debug/runtime`、`GET /api/v1/debug/tasks`（event executor 注册/运行快照，路径名保留兼容）和 `GET /api/v1/debug/databases`；三类诊断必须独立采集和响应，任一接口变慢或失败不得阻塞另两个接口完成加载；每个响应提供自身采集语义的 `collectedAt`。
- `DIAG-SNP-002`：runtime 响应的process必须包含启动时间、非负uptime、PID、Go版本、OS/架构、CPU、goroutine和cgo计数；memory必须包含当前/累计/heap/stack/cache/span/GC统计及可选lastGCAt。
- `DIAG-SNP-003`：`debug/tasks` 响应必须反映 event executor worker 运行、注册/可执行/周期类型、运行 execution 数及每个 definition 的 hidden/quiet/executable/periodic/run-now/retry/max-retry/recoveryPolicy（`local_timeout`|`remote_fence`）/concurrency/stale 能力，不得含 run 参数、JSONL 日志或 periodic interval。
- `DIAG-SNP-004`：databases响应中的每个app/log/metrics数据库必须返回连接池、文件大小、SQLite page/free/used和用户表统计；行数是准确COUNT，表按总大小降序、同大小按名称排序。
- `DIAG-SNP-005`：dbstat不可用时只设置 `database_table_sizes_unavailable` 并保留健康连接、行数和其他统计；单表COUNT失败只标该表错误，数据库不可用则标安全errorCode而非让整个快照失败。
- `DIAG-SNP-006`：诊断响应绝不返回数据库路径/DSN、schema SQL、配置值、秘密、业务行、任务参数或绝对敏感文件路径；文件路径只在服务内用于stat。

## 11. pprof

- `DIAG-PPF-001`：认证用户 `GET /api/v1/debug/pprof` 返回enabled和固定 `127.0.0.1:6060`地址；pprof内容本身不挂Panel对外mux，也不通过普通认证端口代理。
- `DIAG-PPF-002`：`PUT /api/v1/debug/pprof {enabled:true}` 只绑定loopback并注册标准pprof路径；端口占用返回 `pprof_start_failed`，不得改绑0.0.0.0或随机公网地址。
- `DIAG-PPF-003`：重复enable和disable必须幂等；disable失败返回 `pprof_stop_failed`；Panel关闭必须自动disable且重复Close安全。
- `DIAG-PPF-004`：pprof状态是进程内临时状态，重启默认关闭；不得持久化为runtime setting或在维护应用中启动。

## 12. Debug 运行数据清理

- `DIAG-CLR-001`：只有认证用户可在 `/debug` 经危险确认调用 `POST /api/v1/debug/clear-runtime-data`；确认值不精确匹配时必须 422 且不删除任何数据。接受后必须立即返回 202，含本次 `runId`，并由 `GET /api/v1/debug/clear-runtime-data` 查询后台任务状态；同一清理尚在运行时重复 POST 返回同一 runId，不重复启动，客户端断开不得取消清理。
- `DIAG-CLR-002`：清理必须暂停协调与 event executor worker，删除 activity/system logs、metrics、`event_runs`/`event_run_steps`、AppDB `events` 进程行与关联 JSONL、Jobs、reconcile backoff 及全部协调库记录，并清除资源上的悬空 Job 投影与 `applications.planning_error_json`；规划诊断历史随进程事件清除，资源配置及 version/generation 不因清理改变。
- `DIAG-CLR-003`：清理不得删除应用、服务器、凭据、证书、密钥资产、应用修订或期望/观测状态；完成后 worker 必须重启并从当前状态重新收敛。
- `DIAG-CLR-004`：清理期间必须暂停业务 writer，并以最多 1000 行的短事务分批删除；每批独立提交，任一批失败不得回滚此前已完成批次，也不得用覆盖整个清理过程的长写事务阻塞数据库。完成后应 checkpoint WAL，再以不影响逻辑清理结果的方式尝试 VACUUM 回收文件空间；状态响应只返回阶段和结果，不暴露路径或被删除内容。
- `DIAG-CLR-005`：POST/GET 共用状态结构 `runId/cleared/running/status/stage/failedStage/startedAt/finishedAt/errorCode`；stage 区分暂停后台、清理协调、日志、指标、空间回收、恢复后台、完成。失败保留实际失败阶段和安全错误码；`cleared=true,status=failed` 表示逻辑清理完成但恢复后台失败，不能显示为全部成功；`cleared=false` 的失败可能已删除部分批次。
- `DIAG-CLR-006`：清理接受后必须在返回 202 的同一请求路径上先拒绝新的业务写入（HTTP gate BeginPause），再排空在途 writer；节点报告、事件写入/投影、任务收集及编辑会话清理均不能与删除并发。期间普通资源变更返回 503 `runtime_data_maintenance`，认证与清理状态查询仍可用；清理收尾释放暂停门，恢复此前运行的后台工作，不能因失败永久暂停 writer 或意外启动原本未运行的 worker。
- `DIAG-CLR-007`：清理状态在当前 Panel 进程中保留，页面重新进入后用 GET 恢复显示；进程重启后返回 idle 不代表之前清理成功。前端为未确认的请求保留最小会话标记，GET/POST 网络错误不能当作清理终态失败，也不能自动重发清理 POST。GET 失败必须退避后继续查询，不得在固定次数后停止。再次清理仍必须危险确认。
- `DIAG-CLR-008`：等待在途 writer 排空必须受超时限制，且各暂停阶段使用独立超时预算；暂停阶段失败时尚未开始删除，必须释放已经取得的暂停门并提示等待在途工作结束后重试，不得无限等待或带着未排空的 writer 进入删除阶段。

清理入口的首次请求审计及手动任务异步失败收尾也属于在途 writer；首次请求完成审计后才允许开始删除。暂停期间的重复清理 POST 仍需认证，只返回同一运行回执，不向正在清除的活动账本追加重复请求审计。

## 13. 验收证据

- `BKP-EVD-001`：测试必须覆盖正常态只写pending、mode显式门禁、最小维护路由、三类token隔离、登录限流、下载前禁止exit和清理失败不重启。
- `BKP-EVD-002`：归档测试必须覆盖加密/非加密往返、路径穿越、临时目录排除、超长段拒绝、写失败、8GiB限制和错误码映射。
- `RST-EVD-001`：事务测试必须覆盖跨卷外置数据库stage、各rename断点、逆序rollback、protected media优先、previous收敛、symlink/非普通文件拒绝和阻断状态。
- `MNT-EVD-001`：并发测试必须证明同revision竞争只接受一个、幂等重放不重复、键冲突/陈旧revision/非法phase返回409及加密retry重新要密码。
- `OBS-EVD-001`：测试必须覆盖指标时间对齐/range/增量/保留、`(server_id,time)` 补报去重、磁盘当前值不再产出历史序列、磁盘IO可空列与桶平均、概览批量聚合与卡片（含 `diskCurrent`/`diskIo`/`networkProbe` 数据形状）全量验证、dbstat降级和pprof仅loopback/幂等关闭。
- `BKP-EVD-003`：路由清单和前端typed client必须分别验证正常态三条backup API、export维护最小API、restore维护最小API以及debug/metrics/overview API，维护静态fallback不得吞未知API。
