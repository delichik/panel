# Panel 宿主初始化

## 适用场景

修改 `panel setup`、本地控制 socket、Panel 宿主节点关系、首次宿主纳管或 Panel 自身入口初始化时，先读本文档。

## 关键入口

- CLI：`cmd/panel/setup.go`
- 安装状态与编排：`internal/modules/installation/`
- 主进程装配：`internal/bootstrap/panel/app.go`
- 数据库：`panel_installation`
- 服务器 bootstrap/Agent：`internal/modules/servers/`
- Panel 入口：`internal/modules/facilityapps/`

## 行为约定

- Linux 正常启动时创建 `<dataRoot>/run/panel-control.sock`，目录权限 `0700`、socket 权限 `0600`。控制接口只接受固定 setup 操作，不暴露通用 API、数据库或命令执行能力。
- `docker exec -it panel /app/panel setup` 只负责收集交互输入并调用 socket；不得自行打开业务数据库或重复装配 worker。
- setup 复用现有凭据加密、服务器首次 bootstrap、Agent 部署和设施应用保存/协调。SSH 密码、私钥和口令不得进入命令参数、任务参数或日志。
- `panel_installation` 使用固定 `default` 记录。`pending_server_id` 保存可恢复的初始化节点，Agent compatible 后提升为唯一 `host_server_id`；普通服务器删除流程禁止删除宿主节点。
- Panel 入口必须绑定 `host_server_id`，并确保该节点属于设施全局网关节点。首次 setup 以 HTTP 域名入口部署成功为完成基线，不新增证书签发流程。
- 宿主节点首次登记不限于 setup：设施应用网关配置启用 Panel 入口且尚未登记宿主节点时，保存会把所选入口服务器登记为唯一宿主节点；已登记后入口服务器必须与宿主节点一致。
- setup 可重复执行。服务器 bootstrap 失败时清理本次未使用凭据；Agent 失败保留 pending 节点；入口失败保留已经确认的宿主关系并从代理阶段恢复。

## 验证

- 后端改动使用 `task test:backend`，涉及编译或装配时使用 `task build:backend`。
- 修改配置页宿主节点展示时补充 `task test:web` / `task build:web`。

## 文档更新触发

修改 setup 参数、阶段、恢复语义、socket 路径/权限、宿主关系或首次入口完成条件时，必须同步更新本文档和中英文部署说明。

## 首启安全基线

- JWT 默认密钥首启随机化：未在配置中显式提供 JWT secret（或仍为公开默认常量）时，首次启动会自动生成随机 secret 并持久化到 `runtime_settings`，避免公开默认密钥长期生效；显式配置的 secret 保留不变。
- SSH known_hosts：主机密钥 TOFU 记录存放在 `<dataRoot>/known_hosts`（由启动装配显式指定，不依赖环境变量推导）。