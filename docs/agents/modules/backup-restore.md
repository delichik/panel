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
- 只有同时存在 pending export 且启动参数为 `--maintenance-mode backup_export` 时，Panel 才进入备份导出维护模式；pending 文件本身不能触发维护逻辑。`ExportApp` 启动时只短暂打开 `app.db` 读取管理员用户名和 bcrypt 密码哈希，随后立即关闭连接；维护态登录和会话校验只使用内存中的哈希与 token。
- 导出维护页必须先登录。未加密导出在登录后由用户点击开始；加密导出在登录后输入备份密码并开始。真正开始导出后，`ExportApp` 会短暂打开 app/log/metrics SQLite 执行 WAL checkpoint，立即关闭连接，再归档稳定文件。
- 导出维护页通过 `GET /api/v1/backups/export/current` 展示阶段、进度、开始时间、备份创建时间、是否加密和安全错误摘要；导出完成后通过下载接口取得归档，再通过退出维护接口清理 pending 标记并发送 `normal` 重启信号，让 `panel_init` 回到正常业务服务。
- 还原上传只做预检和 pending 标记，再通过 `panel_init` 随机本地监听请求下一次子进程以 `--maintenance-mode restore` 启动；不在正常运行期覆盖数据。pending 文件位于 `data/tmp/restore-pending/`。
- 只有同时存在 pending restore 且启动参数为 `--maintenance-mode restore` 时，Panel 才进入恢复模式 HTTP 页面；pending 文件本身不能触发维护逻辑。未加密归档自动执行；加密归档在恢复页要求重新输入密码。
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

恢复模式仅注册最小 API：

- `GET /api/v1/restore/status`
- `POST /api/v1/restore/password`
- `POST /api/v1/restore/retry`
- `POST /api/v1/restore/clear-pending`

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

## Panel init restart signal

- Container runtime starts `cmd/panel-init`, not `cmd/panel`. `panel_init` starts the child Panel process and exits when the child exits without a restart signal.
- `panel_init` listens on a random `127.0.0.1:0` HTTP port, generates a random restart token, and passes both to the child as `--init-restart-url` and `--init-restart-token`. The listener is not exposed as a Panel business route and must reject requests whose `X-Panel-Init-Token` does not match.
- The local restart request carries the next startup mode: `backup_export`, `restore`, or `normal`. Backup export and restore confirmation write pending files first, then request `backup_export` or `restore`; completed export exit, completed restore, and restore clear-pending remove temporary pending data and request `normal`.
- When `--init-restart-url` or `--init-restart-token` is absent, `restartSupported` must be false and backup/restore APIs must not exit or restart the process. This prevents standalone `panel` runs from entering maintenance logic unexpectedly.
