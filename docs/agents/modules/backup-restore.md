# 备份与还原

## 适用场景

修改全量备份导出、备份归档格式、恢复模式启动、维护状态、设置页备份/还原入口或相关 API 时，先读本文档。

## 关键入口

- 后端模块：`internal/modules/backups/`
- 启动期维护入口：`cmd/panel-init/main.go`、`cmd/panel/main.go`、`internal/modules/backups/export_app.go`、`internal/modules/backups/restore_app.go`
- 正常运行期路由装配：`internal/bootstrap/panel/app.go`
- 前端 API：`web/src/api/backups.ts`
- 前端类型：`web/src/types/api.ts`
- 设置页入口：`web/src/views/settings/backups/index.vue`
- 导出维护页：`web/src/views/maintenance/backup.vue`

## 行为约定

- 备份是全实例导出，默认包含 `dataRoot`、app/log/metrics 三个 SQLite 数据库、密钥资产主密钥文件和必要元数据。`log` 与 `metrics` 属于低价值历史，但全量包默认包含它们；恢复旧归档时仍兼容 `databases/tasks.db`。
- 备份密码可选；前端默认启用加密，关闭加密时必须提示归档可恢复整套 Panel 身份。
- 正常运行期点击导出只写入 `data/tmp/backup-export-pending/pending.json`，再通过 `panel_init` 随机本地监听请求下一次子进程以 `--maintenance-mode backup_export` 启动；不会在业务运行时复制 SQLite 或归档数据。加密导出的密码不写入 pending 文件，启动期维护页会要求用户输入密码后再继续。
- 只有同时存在 pending export 且启动参数为 `--maintenance-mode backup_export` 时，Panel 才进入备份导出维护模式；pending 文件本身不能触发维护逻辑。`ExportApp` 启动时只短暂打开 `app.db` 读取管理员用户名和 bcrypt 密码哈希，随后立即关闭连接；维护态登录和会话校验只使用内存中的哈希与 token。若数据库不可读，可回退到 Panel 配置中的管理员验证材料，但绝不开放匿名维护 API。
- 导出维护页必须先登录。未加密导出在登录后由用户点击开始；加密导出在登录后输入备份密码并开始。真正开始导出后，`ExportApp` 会短暂打开 app/log/metrics SQLite 执行 WAL checkpoint，立即关闭连接，再归档稳定文件。
- 导出维护页通过 `GET /api/v1/backups/export/current` 展示阶段、进度、开始时间、备份创建时间、是否加密和安全错误摘要；导出完成后通过下载接口取得归档，再通过退出维护接口清理 pending 标记并发送 `normal` 重启信号，让 `panel_init` 回到正常业务服务。
- 还原上传只做预检和 pending 标记，再通过 `panel_init` 随机本地监听请求下一次子进程以 `--maintenance-mode restore` 启动；不在正常运行期覆盖数据。pending 文件位于 `data/tmp/restore-pending/`。确认恢复时会把当前管理员用户名和 bcrypt 哈希作为 `0600` pending 维护认证快照写入，使恢复进程即使无法读取损坏的 `app.db` 也仍能验证管理员密码；该快照不进入 API 响应或备份归档。
- 只有同时存在 pending restore 且启动参数为 `--maintenance-mode restore` 时，Panel 才进入恢复模式 HTTP 页面；pending 文件本身不能触发维护逻辑。未加密归档自动执行；加密归档在恢复页要求重新输入密码。恢复页静态入口可公开以承载登录界面，但 restore status 和所有恢复命令必须先取得 restore maintenance token。
- 还原是覆盖式操作，不自动保留旧数据。真正清空目标数据只在归档解密、解包和 manifest/path 校验通过后执行。
- 恢复模式页面只显示不敏感信息：阶段、进度、备份时间、版本、是否加密和安全错误摘要；不得显示数据库路径、密钥路径、凭据、私钥、任务参数或业务记录内容。

## API 范围

正常运行期只注册：

- `POST /api/v1/backups/export`
- `POST /api/v1/backups/restore/preflight`
- `POST /api/v1/backups/restore/confirm`

启动期备份导出模式注册最小 API。认证只读取一次 `app.db` 并关闭连接，随后使用内存会话；除登录、会话和登出接口外，导出维护 API 都要求维护态 token：

- `POST /api/v1/auth/login`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/logout`
- `GET /api/v1/backups/export/current`
- `POST /api/v1/backups/export/start`
- `POST /api/v1/backups/export/password`
- `POST /api/v1/backups/export/exit`
- `GET /api/v1/backups/export/{id}/download`

维护模式的页面兜底不得处理 `/api/` 路径；未注册 API 必须返回 JSON 错误响应，避免前端 API client 收到 HTTP 200 的 HTML 页面。

恢复模式仅注册最小 API。它与导出维护模式分别维护独立内存 session；restore token 使用独立上下文，不能用于 export API，export token 和普通运行态 token 也不能用于 restore API：

- `POST /api/v1/auth/login`
- `GET /api/v1/auth/session`
- `POST /api/v1/auth/logout`
- `GET /api/v1/restore/status`
- `POST /api/v1/restore/password`
- `POST /api/v1/restore/retry`
- `POST /api/v1/restore/clear-pending`

除登录、session 和 logout 外，上述 restore API 全部要求 restore maintenance token。旧 pending 没有认证快照时，恢复进程依次尝试仍可读取的 `app.db` 与显式非默认 Panel 配置管理员哈希；安装默认 `admin/admin` 不属于可信恢复凭据，不能在已初始化实例数据库损坏时作为后门。所有可信来源都不可用时拒绝启动恢复维护应用，禁止匿名降级。

## 维护状态与命令契约

- export 与 restore 状态保留旧 `mode`、`phase`、`progress`、`error`、时间、manifest、download/restart 字段，同时提供 `schemaVersion`、严格递增的 `revision`、typed phase、`capabilities`、`retryable`、`pollAfterMs` 和 `errorDetail { code, message, retryable }`。错误信息必须保持安全摘要，不得泄露路径、密钥或归档内容。
- export start/password 和 restore password/retry/clear-pending 是状态机命令。阶段校验、可选 `expectedRevision` 校验、运行中状态迁移和幂等记录必须在同一状态锁内完成，然后才能在锁外启动后台工作，确保并发请求最多启动一个执行。
- 命令可通过 JSON `clientOperationId` 或 `Idempotency-Key` header 提供幂等键。相同命令和键的重放返回首次接受结果且不重复执行；同一键用于不同命令时返回冲突。为兼容旧客户端，`expectedRevision` 和幂等键暂时可省略，但省略幂等键的重复请求仍会被原子 phase 迁移阻止。
- 加密恢复在 retry 时先回到 `password_required`，不会保存或复用此前提交的明文密码；用户重新提交密码后才继续恢复。

## Pending 发布与恢复事务

- `SavePendingRestore` 在 `data/tmp` 下建立 `0700` 临时目录，通过 `O_EXCL` 创建 `0600` archive 和 marker；marker 保存原始 archive 的 SHA-256 与 size。文件同步、digest/size 一致性验证和目录同步完成后才整体发布。替换已有 pending 时先递归收紧旧目录和文件权限，并通过 `restore-pending.previous` 提供中断恢复；digest 不匹配或文件类型非法的 target 不得淘汰最后一致 previous。旧 marker 首次读取时会补写当前 archive digest/size，后续读取、media 和发布收敛统一验证。
- 真正 apply 前，pending archive、marker 和维护认证快照会移动到 `DataRoot` 同级的 `.<dataRoot-name>-restore-transaction/media`。该目录不属于被替换范围，是 retry、rollback 和重启恢复的唯一可信介质。`media/` 存在本身就强制进入 restore recovery，即使首次 `state.json` 尚未发布；此时 protected media 优先于任何后来出现的普通 pending，必须先完成或清除旧恢复事务。
- `state.json` 持久记录 `prepared`、`applying`、每个目标的 `backup_planned`/`backup_moved`/`swapped`、`rolling_back`/`rollback_renaming`、`rolled_back` 和 `committed`。`rollback_renaming` 在 backup 改名回正式路径前落盘，可处理改名成功但完成状态未落盘的二次中断，无需对整个 DataRoot 做停机期全量哈希。DataRoot 使用同级事务目录；配置在 DataRoot 外部的数据库会把 stage 与 backup 放到数据库目标同目录，保证跨卷配置仍使用同文件系统 rename。
- 普通 apply 失败立即逆序 rollback；进程中断后下一次启动只要检测到事务 state，即使启动参数为 normal 也必须优先进入 restore recovery。state 检查只有明确不存在时才能继续 normal，权限或 I/O 错误必须 fail closed。成功 rollback 后保留 media 并允许 retry/clear；备份缺失、状态损坏或 rollback 未完成时必须设置阻断状态，禁止 retry、clear-pending 和 normal restart。
- pending/previous 收敛前会验证 marker 是普通文件、archiveFilename 是 basename 且目标 archive 是普通文件；损坏 target 不得覆盖或删除最后一致的 previous。权限收紧会拒绝 symlink 和其他非普通文件，避免跟随链接修改事务目录外对象。
- 只有所有目标写入 committed 状态后，才允许清理旧目标备份、事务状态和 protected media，并进入 normal restart。

## 文档更新触发

修改备份内容范围、归档格式、加密方式、维护状态、还原覆盖语义、恢复模式页面、启动检测时机、API 路径或前端入口时，必须更新本文档和模块索引。

## 验证

- 后端备份、恢复、启动模式或路由改动运行 `task test:backend`。
- 前端设置页、维护页、API client 或类型改动运行 `task test:web`；涉及路由/类型/构建时运行 `task build:web`。
- 同时改动前后端契约时执行两侧相关检查。

## Maintenance HTTP isolation

- Normal runtime route registration must only include the backup/restore APIs mounted from `internal/modules/backups/routes.go`. Backup export and restore maintenance pages must not be registered in `internal/bootstrap/panel/app.go` or any normal business router.
- `cmd/panel/main.go` must not enter backup export or restore maintenance mode only because pending files exist. Maintenance mode requires `--maintenance-mode backup_export` or `--maintenance-mode restore`, which is set by `panel_init` for the restarted child process.
- Startup maintenance modes use `internal/modules/backups/maintenance_listener.go` through `ExportApp.ListenAndServe` and `RestoreApp.ListenAndServe`. This listener is owned by the backup module and is shut down from `Close`, restore completion, restore pending cleanup, or backup export exit.
- Maintenance page fallbacks must never serve HTML for business paths such as `/applications/...`, `/settings/...`, or unknown `/api/...` routes. Unknown API routes return JSON 404; unknown non-maintenance page routes return HTTP 404.
- Backup export maintenance HTML is only served at `/maintenance/backup` plus built frontend assets required by that page. Restore maintenance HTML is only served at `/maintenance/restore`.

## Maintenance V2 frontend isolation

- The isolated Vue implementation lives under `web/src/views/maintenance/_v2/` and is not connected to the production router or main entry before the G4 atomic frontend switch.
- Backup export and restore use separate maintenance session clients and token stores. Neither context may reuse the normal runtime token or the other maintenance token.
- Runtime status is accepted only when `schemaVersion=1`, mode and phase are compatible, revision/progress/polling fields are valid, capability combinations are legal for that exact mode/phase, RFC3339 timestamps and manifest/file metadata are well formed, consumed booleans are typed, and downloadable status includes a safe non-empty export ID. Unsupported or malformed status disables commands instead of deriving behavior from legacy strings or formatting invalid dates.
- Every V2 command sends the observed `expectedRevision` and a fresh `clientOperationId`. HTTP 409 refreshes status; HTTP 401 clears only the active maintenance context and returns to the embedded login on the same maintenance URL.
- Polling follows `pollAfterMs` with a 30-second contract maximum, aborts superseded requests, ignores late lower revisions and responses from polls invalidated by a command result, pauses while hidden or offline, and marks retained data stale with a safely clamped delay of at most 90 seconds. A valid command result clears any older contract failure before scheduling again.
- Downloadable export IDs use a strict alphanumeric/underscore/hyphen stable-ID whitelist. Backup download filenames and fallback names both pass through the same basename and control-character cleanup; if both collapse to empty, the client uses a fixed safe filename. Download completion changes the primary task from download to exit; restore clear-pending always requires a danger confirmation and is never shown while applying.

## Panel init restart signal

- Container runtime starts `cmd/panel-init`, not `cmd/panel`. `panel_init` starts the child Panel process and exits when the child exits without a restart signal.
- `panel_init` listens on a random `127.0.0.1:0` HTTP port, generates a random restart token, and passes both to the child as `--init-restart-url` and `--init-restart-token`. The listener is not exposed as a Panel business route and must reject requests whose `X-Panel-Init-Token` does not match.
- The local restart request carries the next startup mode: `backup_export`, `restore`, or `normal`. Backup export and restore confirmation write pending files first, then request `backup_export` or `restore`; completed export exit, completed restore, and restore clear-pending remove temporary pending data and request `normal`.
- When `--init-restart-url` or `--init-restart-token` is absent, `restartSupported` must be false and backup/restore APIs must not exit or restart the process. This prevents standalone `panel` runs from entering maintenance logic unexpectedly.
