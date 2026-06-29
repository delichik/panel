# 备份与还原

## 适用场景

修改全量备份导出、备份归档格式、恢复模式启动、维护状态、设置页备份/还原入口或相关 API 时，先读本文档。

## 关键入口

- 后端模块：`internal/modules/backups/`
- 启动期维护入口：`cmd/panel/main.go`、`internal/modules/backups/export_app.go`、`internal/modules/backups/restore_app.go`
- 正常运行期路由装配：`internal/bootstrap/panel/app.go`
- 前端 API：`web/src/api/backups.ts`
- 前端类型：`web/src/types/api.ts`
- 设置页入口：`web/src/views/settings/backups/index.vue`
- 导出维护页：`web/src/views/maintenance/backup.vue`

## 行为约定

- 备份是全实例导出，默认包含 `dataRoot`、app/task/metrics 三个 SQLite 数据库、密钥资产主密钥文件和必要元数据。`tasks` 与 `metrics` 属于低价值历史，但全量包默认包含它们。
- 备份密码可选；前端默认启用加密，关闭加密时必须提示归档可恢复整套 Panel 身份。
- 正常运行期点击导出只写入 `data/tmp/backup-export-pending/pending.json`，提示用户重启；不会在业务运行时复制 SQLite 或归档数据。加密导出的密码不写入 pending 文件，启动期维护页会要求用户输入密码后再继续。
- 启动时若发现 pending export，Panel 不装配正常业务模块，而是进入备份导出维护模式。`ExportApp` 启动时只短暂打开 `app.db` 读取管理员用户名和 bcrypt 密码哈希，随后立即关闭连接；维护态登录和会话校验只使用内存中的哈希与 token。
- 导出维护页必须先登录。未加密导出在登录后由用户点击开始；加密导出在登录后输入备份密码并开始。真正开始导出后，`ExportApp` 会短暂打开 app/task/metrics SQLite 执行 WAL checkpoint，立即关闭连接，再归档稳定文件。
- 导出维护页通过 `GET /api/v1/backups/export/current` 展示阶段、进度、开始时间、备份创建时间、是否加密和安全错误摘要；导出完成后通过下载接口取得归档，再通过退出维护接口清理 pending 标记。此时仍需重启 Panel 才会回到正常业务服务。
- 还原上传只做预检和 pending 标记，不在正常运行期覆盖数据。pending 文件位于 `data/tmp/restore-pending/`。
- 启动时若发现 pending restore，Panel 不装配正常业务模块，而是进入恢复模式 HTTP 页面。未加密归档自动执行；加密归档在恢复页要求重新输入密码。
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

## Container self-restart

- Panel does not expose a standalone restart API. Backup/restore can only trigger a process restart as part of existing workflow endpoints.
- When normal runtime `POST /api/v1/backups/export` writes a pending export marker and Panel detects it is running inside a container, the process schedules a short delayed exit after the response. The container runtime must have a restart policy that starts Panel again; the next startup enters backup export maintenance mode.
- When normal runtime `POST /api/v1/backups/restore/confirm` writes a pending restore marker and Panel detects it is running inside a container, the process schedules the same delayed exit; the next startup enters restore mode.
- In backup export maintenance mode, `POST /api/v1/backups/export/exit` clears the pending marker and schedules the delayed exit only when container self-restart is supported. Otherwise the UI keeps instructing the user to restart Panel manually.
- In restore mode, successful restore or `POST /api/v1/restore/clear-pending` clears the pending marker and schedules the delayed exit only when container self-restart is supported.
- Container detection is intentionally local and conservative: `/.dockerenv` or Docker/containerd/Kubernetes markers in `/proc/1/cgroup`. No Docker socket, Docker CLI, shell command, or new HTTP route is required.
- `restartSupported` in existing backup/restore responses and maintenance status only means Panel can exit itself from inside a detected container. It does not guarantee the container runtime is configured to restart the container.
