# 部署 Panel

[English](deployment.md) | 简体中文

Panel 通过容器镜像发布。请使用 Docker Compose 或 Docker 运行。

> Panel 仍处于 alpha 阶段。升级前请备份 Panel 数据卷，重要变更建议先在非关键环境验证。

## 环境要求

- 一台已安装 Docker Engine 的 Linux 主机。
- 推荐方案需要 Docker Compose 插件；也可以只使用 Docker 和 `docker run`。
- 主机架构为 `amd64` 或 `arm64`。
- 一个可用于 Web 界面的 TCP 端口，本文示例使用 `8080`。

运行 Panel 的主机与被 Panel 管理的目标服务器是两个概念。Panel 容器不需要挂载 Panel 主机的 Docker Socket。

## 使用 Docker Compose 部署

创建部署目录：

```bash
mkdir -p panel
cd panel
```

创建 `compose.yaml`：

```yaml
services:
  panel:
    image: ghcr.io/delichik/panel:latest
    container_name: panel
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - panel-data:/app/data

volumes:
  panel-data:
    name: panel-data
```

拉取镜像并启动 Panel：

```bash
docker compose pull
docker compose up -d
```

检查运行状态：

```bash
docker compose ps
docker compose logs --tail=100 panel
```

在浏览器访问 `http://<Panel主机地址>:8080`。

默认账号：

- 用户名：`admin`
- 密码：`admin`

Panel 会在首次登录后强制修改密码。完成修改后，再继续阅读[使用说明](user-guide.zh-CN.md)。

## 使用 Docker 部署

创建持久化数据卷：

```bash
docker volume create panel-data
```

启动 Panel：

```bash
docker run -d \
  --name panel \
  --restart unless-stopped \
  -p 8080:8080 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

检查状态和日志：

```bash
docker ps --filter name=panel
docker logs --tail=100 panel
```

访问 `http://<Panel主机地址>:8080`，使用 `admin/admin` 登录，并按提示修改密码。

## 数据持久化

Panel 的所有持久化状态都保存在 `/app/data` 下，包括：

- 应用、任务和指标数据库。
- Panel 保存的 SSH 凭据和服务商凭据。
- 证书与密钥资产。
- Panel 安全设置和自动生成的主密钥。
- 备份与还原工作数据。

本文示例把 `/app/data` 映射到命名卷 `panel-data`。重新创建或升级容器时，必须继续使用同一个数据卷。

除非确定要清空整个 Panel 实例，否则不要删除 `panel-data`。

## 备份 Panel

推荐从 Panel 界面的 **设置 → 备份与还原** 执行备份。全量导出会包含数据库、密钥材料和必要元数据。加密备份文件和备份密码应分别保存在安全位置。

导出期间 Panel 会暂时进入维护页面。请在维护页登录、开始导出、下载完成的归档，然后退出维护模式以恢复正常运行。

升级镜像前应创建一份新的全量导出。如果还需要宿主机级快照，请先停止 Panel，再使用现有基础设施备份工具备份 Docker 的 `panel-data` 数据卷。

## 升级 Panel

每次升级前都应备份 Panel。新版本启动时会自动执行数据库迁移。

### Docker Compose

使用 `latest` 时：

```bash
docker compose pull
docker compose up -d
docker compose ps
```

如果希望控制升级时间，请将 `compose.yaml` 中的 `latest` 替换为仓库 Release 中的具体正式版本标签，然后执行相同命令。

### Docker

拉取新镜像，只删除旧容器，然后使用同一个数据卷重新创建：

```bash
docker pull ghcr.io/delichik/panel:latest
docker stop panel
docker rm panel
docker run -d \
  --name panel \
  --restart unless-stopped \
  -p 8080:8080 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

删除容器不会删除命名卷。升级过程中不要执行 `docker volume rm panel-data`。

## 停止与启动 Panel

Docker Compose：

```bash
docker compose stop
docker compose start
```

Docker：

```bash
docker stop panel
docker start panel
```

## 网络与 HTTPS

本文示例把 Panel 的 `8080` 端口发布到主机的所有网络接口，适合可信内网使用。不要在没有 HTTPS 和访问控制的情况下直接暴露到公网。

如果反向代理运行在同一台主机，可以只绑定本机回环地址：

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

在反向代理终止 HTTPS，并把请求转发到 `http://127.0.0.1:8080`。如果反向代理也运行在容器中，应让两个容器加入同一个私有 Docker 网络，而不是使用回环地址绑定。

## 常见问题

### 容器退出或状态异常

查看日志：

```bash
docker compose logs --tail=200 panel
```

Docker 方式：

```bash
docker logs --tail=200 panel
```

确认 `panel-data` 数据卷可写，并确认主机架构为 `amd64` 或 `arm64`。

### 8080 端口已被占用

只修改端口映射左侧的宿主机端口。例如改为宿主机 `9080`：

```yaml
ports:
  - "9080:8080"
```

之后访问 `http://<Panel主机地址>:9080`。

### 其他设备无法打开页面

检查容器状态、宿主机防火墙、云安全组和发布的宿主机端口。如果映射使用 `127.0.0.1`，Panel 将只能从本机或本机反向代理访问。

### 重新创建容器后数据消失

确认容器仍将原来的命名卷挂载到 `/app/data`：

```bash
docker inspect panel --format '{{json .Mounts}}'
docker volume inspect panel-data
```

继续阅读[使用 Panel](user-guide.zh-CN.md)。
