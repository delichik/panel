# 备份、还原与诊断验收规范

本文约束全实例备份、维护模式、覆盖还原、指标/概览和运行时诊断。正常运行态、备份导出维护态与恢复维护态是三个隔离的应用与认证上下文。

## 1. 正常运行期备份入口

- `BKP-API-001`：认证用户调用 `POST /api/v1/backups/export` 时，后端只以0700目录、0600临时文件、fsync和rename原子写入 pending export；不得在正常业务进程checkpoint、复制数据库或归档dataRoot。
- `BKP-API-002`：pending仅保存exportId、createdAt和encrypt标志；加密密码不得写入pending、数据库、任务、日志或响应。
- `BKP-API-003`：pending写入成功后，仅当panel_init重启URL和token均有效才请求 `backup_export`；返回202包含exportId与真实restartSupported，独立运行panel时不得退出或伪称可重启。
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
- `BKP-MNT-008`：下载只允许completed、downloadAvailable且URL中的exportId精确匹配；成功响应即在服务端记录已下载，不把downloadedAt暴露到状态。
- `BKP-MNT-009`：completed但从未记录下载时exit必须返回409并保留归档和pending；已下载后exit清理临时文件并请求normal重启，清理失败不得发送重启信号。
- `BKP-MNT-010`：failed状态允许exit清理但不得把失败归档展示为可下载成功；restart不支持时必须明确要求外部重启而非退出当前进程。

## 3. 归档格式与加密安全

- `BKP-ARC-001`：manifest必须含formatVersion、panelVersion、UTC createdAt、encrypted、includes，以及每个相对文件的size与SHA-256；恢复前必须逐项核对。
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
- `RST-TXN-004`：dataRoot的stage/backup位于其同级；配置在dataRoot外的数据库必须在各自目标同目录stage/backup，确保只使用同文件系统rename而非跨卷非原子移动。
- `RST-TXN-005`：任一目标apply失败必须逆序rollback所有已动目标；新创建但尚未完成state flush的目标也要清理，原数据必须恢复。
- `RST-TXN-006`：`rollback_renaming`恢复必须识别“rename已成功但状态未落盘”，无需停机期全量hash即可继续；重复冷启动应收敛而非反复交换。
- `RST-TXN-007`：成功rollback保留protected media并允许retry/clear；backup缺失、state损坏或rollback失败进入阻断状态，禁止retry、clear和normal重启。
- `RST-TXN-008`：state检查只有明确不存在才能normal启动；权限/I/O/解析不确定必须fail closed进入错误/恢复路径。
- `RST-TXN-009`：只有所有目标均committed后才能删除旧backup、state和media，并请求normal重启；清理或重启请求失败必须可诊断，不得把未收敛状态当普通完成。
- `RST-TXN-010`：clear-pending成功必须删除普通pending或已允许清理的recovery介质并请求normal；重启不支持时保持服务并明确结果，同一幂等键重放不重复生命周期动作。

## 8. 指标采集、查询与保留

- `OBS-MET-001`：指标只接收compatible且有URL的Agent report；保存时绑定服务器ID并将时间UTC截断至秒，包含CPU、内存、磁盘、网络、load1/5/15和系统状态字段。
- `OBS-MET-002`：指标成功保存后可标记服务器可达；保存失败不得伪标可达或中断report stream。
- `OBS-MET-003`：`GET /servers/{id}/metrics` 默认1h，只接受 `1h|6h|1d|24h|7d`（前端公开子集可更窄）；非法range返回 `range_invalid`，存在但无数据返回所有序列为空数组。
- `OBS-MET-004`：查询结果按时间升序，CPU/内存/磁盘/网络/load各序列共享样本时间语义；增量查询只返回严格晚于按秒对齐since的点。
- `OBS-MET-005`：清理worker按runtime retention和hourly/daily/weekly周期独立运行，不属于tasks worker；retention小于1必须拒绝且不删除任何行。
- `OBS-MET-006`：服务器删除必须删除其全部指标；其他服务器指标和MetricsDB结构保持不变。

## 9. 概览与卡片配置

- `OBS-OVW-001`：`GET /api/v1/overview` 必须对服务器批量聚合最新指标、load、包更新数和刷新时间，避免逐服务器N+1；最新指标少于5分钟才标metricsFresh。
- `OBS-OVW-002`：首次 `GET /overview/cards` 必须幂等创建固定default配置；之后读取按原有顺序完整返回，空数组是合法配置。
- `OBS-OVW-003`：`PUT /overview/cards` 整套原子替换，最多100卡；ID非空且唯一、kind在白名单、width 1..6、height 1..4、range合法、networkDirection为rx/tx/both、serverIds非空项且卡内唯一。
- `OBS-OVW-004`：验证失败不得覆盖旧卡片JSON；成功后重启仍保持顺序、尺寸、范围和服务器选择。
- `OBS-OVW-005`：`GET /overview/cards/{id}/data` 对不存在ID返回404；非指标卡返回空metricsByServer；指标卡空serverIds表示全部现存服务器，非空只查询仍存在的选择。
- `OBS-OVW-006`：合法 `since` 只返回严格更新点，非法RFC3339Nano返回422；多服务器必须批量查询并为无数据服务器保留空Series，不能用0填缺失采样。
- `OBS-OVW-007`：自动刷新前端必须防重入、页面隐藏/离线/编辑态暂停、切换或卸载丢弃迟到响应；失败静默保留旧数据并下周期重试，不弹误导toast。

## 10. 诊断快照

- `DIAG-SNP-001`：只有认证用户可调用 `GET /api/v1/debug/snapshot`；响应的process、memory、tasks和所有数据库统计必须使用同一collectedAt语义生成。
- `DIAG-SNP-002`：process必须包含启动时间、非负uptime、PID、Go版本、OS/架构、CPU、goroutine和cgo计数；memory必须包含当前/累计/heap/stack/cache/span/GC统计及可选lastGCAt。
- `DIAG-SNP-003`：tasks部分必须反映worker运行、注册/可执行/周期类型、运行execution数及每个definition的hidden/executable/periodic/run-now/retry/max-retry/concurrency/stale/interval能力，不得含任务参数或日志。
- `DIAG-SNP-004`：每个app/log/metrics数据库必须返回连接池、文件大小、SQLite page/free/used和用户表统计；行数是准确COUNT，表按总大小降序、同大小按名称排序。
- `DIAG-SNP-005`：dbstat不可用时只设置 `database_table_sizes_unavailable` 并保留健康连接、行数和其他统计；单表COUNT失败只标该表错误，数据库不可用则标安全errorCode而非让整个快照失败。
- `DIAG-SNP-006`：诊断响应绝不返回数据库路径/DSN、schema SQL、配置值、秘密、业务行、任务参数或绝对敏感文件路径；文件路径只在服务内用于stat。

## 11. pprof

- `DIAG-PPF-001`：认证用户 `GET /api/v1/debug/pprof` 返回enabled和固定 `127.0.0.1:6060`地址；pprof内容本身不挂Panel对外mux，也不通过普通认证端口代理。
- `DIAG-PPF-002`：`PUT /api/v1/debug/pprof {enabled:true}` 只绑定loopback并注册标准pprof路径；端口占用返回 `pprof_start_failed`，不得改绑0.0.0.0或随机公网地址。
- `DIAG-PPF-003`：重复enable和disable必须幂等；disable失败返回 `pprof_stop_failed`；Panel关闭必须自动disable且重复Close安全。
- `DIAG-PPF-004`：pprof状态是进程内临时状态，重启默认关闭；不得持久化为runtime setting或在维护应用中启动。

## 12. 验收证据

- `BKP-EVD-001`：测试必须覆盖正常态只写pending、mode显式门禁、最小维护路由、三类token隔离、登录限流、下载前禁止exit和清理失败不重启。
- `BKP-EVD-002`：归档测试必须覆盖加密/非加密往返、路径穿越、临时目录排除、超长段拒绝、写失败、8GiB限制和错误码映射。
- `RST-EVD-001`：事务测试必须覆盖跨卷外置数据库stage、各rename断点、逆序rollback、protected media优先、previous收敛、symlink/非普通文件拒绝和阻断状态。
- `MNT-EVD-001`：并发测试必须证明同revision竞争只接受一个、幂等重放不重复、键冲突/陈旧revision/非法phase返回409及加密retry重新要密码。
- `OBS-EVD-001`：测试必须覆盖指标时间对齐/range/增量/保留、概览批量聚合与卡片全量验证、dbstat降级和pprof仅loopback/幂等关闭。
- `BKP-EVD-003`：路由清单和前端typed client必须分别验证正常态三条backup API、export维护最小API、restore维护最小API以及debug/metrics/overview API，维护静态fallback不得吞未知API。
