# 部署 Seamark

[English](deployment.md) | 简体中文

Seamark 通过容器镜像发布。请使用 Docker Compose 或 Docker 运行。

> Seamark 仍处于 alpha 阶段。升级前请备份 Seamark 数据卷，重要变更建议先在非关键环境验证。

## 环境要求

- 一台已安装 Docker Engine 的 Linux 主机。
- 推荐方案需要 Docker Compose 插件；也可以只使用 Docker 和 `docker run`。
- 主机架构为 `amd64` 或 `arm64`。
- 一个可用于 Web 界面的 TCP 端口，本文示例使用 HTTPS `8443`。

运行 Seamark 的主机与被 Seamark 管理的目标服务器是两个概念。Seamark 容器不需要挂载 Seamark 主机的 Docker Socket。

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
      - "127.0.0.1:8443:8443"
    volumes:
      - panel-data:/app/data
    cap_add:
      - NET_ADMIN
      - NET_RAW
    devices:
      - /dev/net/tun

volumes:
  panel-data:
    name: panel-data
```

`cap_add` 与 `devices` 只服务于可选的 Tailscale 集成，详见下文 [Tailscale](#tailscale) 一节；不使用该功能时可以删掉。

拉取镜像并启动 Seamark：

```bash
docker compose pull
docker compose up -d
```

检查运行状态：

```bash
docker compose ps
docker compose logs --tail=100 panel
```

启动后访问 `https://<主机>:8443`。默认使用自签名证书，首次浏览器访问需要接受证书提示。登录后可在 **设置 → 证书** 中配置 Panel 域名和用户 TLS 证书。

默认账号：

- 用户名：`admin`
- 密码：`admin`

Seamark 会在首次登录后强制修改密码。完成修改后，再继续阅读[使用说明](user-guide.zh-CN.md)。

## 使用 Docker 部署

创建持久化数据卷：

```bash
docker volume create panel-data
```

启动 Seamark：

```bash
docker run -d \
  --name panel \
  --restart unless-stopped \
  -p 127.0.0.1:8443:8443 \
  -v panel-data:/app/data \
  --cap-add=NET_ADMIN \
  --cap-add=NET_RAW \
  --device=/dev/net/tun \
  ghcr.io/delichik/panel:latest
```

这三个 `--cap-add`/`--device` 参数只服务于可选的 Tailscale 集成，详见下文 [Tailscale](#tailscale) 一节。

检查状态和日志：

```bash
docker ps --filter name=panel
docker logs --tail=100 panel
```

启动后访问 `https://<主机>:8443`，首次浏览器访问需要接受自签名证书提示，再使用 `admin/admin` 登录并按提示修改密码。之后可在 **设置 → 证书** 配置 Panel 域名和用户 TLS 证书。

## 数据持久化

Seamark 的所有持久化状态都保存在 `/app/data` 下，包括：

- 应用、任务和指标数据库。
- Seamark 保存的 SSH 凭据和服务商凭据。
- 证书与密钥资产。
- Seamark 安全设置和自动生成的主密钥。
- 备份与还原工作数据。
- 使用容器内 Tailscale 时的节点身份、LocalAPI socket 与期望态配置。

本文示例把 `/app/data` 映射到命名卷 `panel-data`。重新创建或升级容器时，必须继续使用同一个数据卷。

除非确定要清空整个 Seamark 实例，否则不要删除 `panel-data`。

## 备份 Seamark

推荐从 Seamark 界面的 **设置 → 备份与还原** 执行备份。全量导出会包含数据库、密钥材料和必要元数据。加密备份文件和备份密码应分别保存在安全位置。

导出期间 Seamark 会暂时进入维护页面。请在维护页登录、开始导出、下载完成的归档，然后退出维护模式以恢复正常运行。

升级镜像前应创建一份新的全量导出。如果还需要宿主机级快照，请先停止 Seamark，再使用现有基础设施备份工具备份 Docker 的 `panel-data` 数据卷。

## 升级 Seamark

每次升级前都应备份 Seamark。新版本启动时会自动执行数据库迁移。

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
  -p 8443:8443 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

删除容器不会删除命名卷。升级过程中不要执行 `docker volume rm panel-data`。

## 停止与启动 Seamark

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

本文示例只把 Panel 的 HTTPS `8443` 端口发布到宿主机回环地址。配置域名和用户证书后，再按需通过反向代理或防火墙公开该端口。

如果反向代理运行在同一台主机，可以只绑定本机回环地址：

```yaml
ports:
  - "127.0.0.1:8443:8443"
```

在反向代理终止 HTTPS，并把请求转发到 `https://127.0.0.1:8443`（可信任 Panel 自签名证书，或先在设置中配置用户证书）。如果反向代理也运行在容器中，应让两个容器加入同一个私有 Docker 网络，而不是使用回环地址绑定。

## Agent 部署时的防火墙接管

Seamark 只通过 **UFW** 管理服务器防火墙，并且现在把它作为 **Agent 部署的前提** 自动完成：在往节点上安装任何东西之前，缺失时安装 UFW，放行 SSH 端口、Agent 端口，以及使用反向代理设施时的 80/443，并且只在 SSH 端口放行之后才启用默认拒绝策略。手动安装与启用的入口已经移除。

需要提前知道的后果：

- 发行版没有 UFW 适配器的节点**无法部署 Agent**：部署会以 `agent_firewall_unsupported` 失败。受支持发行版与其他包管理能力一致，仍是 Debian/Ubuntu 范围。
- **应用端口不在上述基础集合内。** 每个应用必须对它发布的端口声明「开放防火墙」；未声明而发布的端口会被阻断。应用此前写入的规则即使在 UFW 未启用时也会保留在配置里，因此启用策略时会一并生效。
- 在该行为存在之前加入的服务器，会在下一次部署（包括证书续期）时被接管。
- 接管失败会让部署失败，但不会影响正在运行的 Agent。
## 通过 HTTP 投递 Agent

Seamark 会在每台受管服务器上安装 `panel-agent`。默认方式是用已有的 SSH 连接上传压缩包，在长距离或拥塞线路上很慢。也可以改为让服务器通过 HTTP 下载，这样就能在 Panel 前面放 CDN 缓存。

在 **设置 → Agent 下载 → Agent 下载基址** 填入服务器应当访问的地址，例如 `https://panel.example.com`。之后：

- 端点为 `GET /agent/{version}/{platform}/panel-agent.gz`。它不鉴权，并且注册在 `/api` 之外，因此「`/api/*` 绕缓存」这类 CDN 规则不会影响它。URL 里带 Panel 构建版本号，可以放心设置很长的缓存时间，升级自然换 URL。
- 目标服务器需要能访问该地址，并具备 `gzip` 和 `sha256sum`。如果服务器既没有 `curl`、也没有带 `timeout` 的 `wget`，Seamark 会给它一次修复机会：在 Debian 和 Ubuntu 上用发行版包管理器装上 `curl` 并重试下载一次。如果安装失败（最常见的原因是服务器到不了自己的软件源），投递会回退到 SSH 上传，而不是让部署失败。
- **Agent 传输超时**（默认 300 秒）在两侧同时生效，既管下载也管 SSH 上传。下载会拆成若干轮续传，所以比单轮更慢的链路仍能完成；链路更慢时请调大这个值，而不要指望单轮跑完。粗略参考：压缩包在默认值下大约需要 0.2 Mbit/s，而此前「未压缩 + 30 秒」大约需要 8 Mbit/s。
- 如果下载在预算内仍然完不成，部署会失败，任务日志会明确写出这一点并指出该调大哪个超时。它不会静默回退——传到一半属于数据问题，不属于可达性问题。
- **校验下载的 TLS 证书** 默认关闭，以便 Panel 使用自签证书。期望的 SHA-256 经已认证的 SSH 通道下发，并在解压后比对，所以两种设置下二进制都无法被替换；打开校验则额外保护传输本身。
- 如果服务器访问不到该地址（缺少 fetcher、DNS 或连接失败、HTTP 错误），Seamark 会回退到 SSH 上传。传输中断、压缩包损坏或哈希不匹配则直接判定部署失败，不会用更慢的路径重试。
- 基址留空即关闭 HTTP 投递，保持原有行为。

CDN 选择提示：Cloudflare 免费版通常把中国大陆访客调度到境外边缘，收益有限；带大陆节点的 CDN 需要域名已备案。源站始终是 Panel，缓存未命中时由 CDN 回源。

## Tailscale

Seamark 可以把 Panel 容器与所管理的服务器加入同一个 tailnet，并在 Panel→节点、节点→节点的连接上优先使用 tailnet 地址。Tailscale 是**可选**能力：没有下面的能力与设备时，其余功能照常工作，界面会明确显示该功能不可用，而不是静默失败。

容器要求：

- 需要 `--cap-add=NET_ADMIN`、`--cap-add=NET_RAW` 与 `--device=/dev/net/tun`。上文 Compose 示例使用 `cap_add` 与 `devices`，`docker run` 示例使用对应的命令行参数。
- 宿主机必须提供 `tun` 模块（`modprobe tun`，或用 `lsmod | grep tun` 检查）。缺少该模块时 `tailscaled` 无法以内核 TUN 模式启动。
- 容器内 `panel-init` 以 root 作为 PID 1，把 Panel 子进程降权到非 root 的 `panel` 用户，并自行管理 `tailscaled`；状态与 LocalAPI socket 都保存在 `/app/data/tailscale`。

配置方式：

- 在 **设置 → Tailscale**（[设置页](https://<主机>:8443/settings/tailscale)）填写 Tailscale 认证密钥。密钥只写不读：保存后无法再次查看，只能替换或清除；清除会删除已存密钥并停止容器内 tailscale。
- ACL 标签使用 `tag:name` 形式（仅小写字母、数字和连字符），保存前会统一小写、去重并排序。
- 逐节点开关位于服务器表单，并在服务器详情页以只读方式展示：加入 tailnet、Agent 连接优先使用 Tailscale、节点互联优先使用 Tailscale。两个偏好开关只有在节点已加入、且 Panel 容器自身已登录 tailnet 时才生效；它们不会改写 `agent.url`。
- 同一设置分区的「应用 / 重连」只请求协调，是否生效以旁边显示的实际态为准。

限制：

- Seamark 不管理 `tailscale0` 接口的 UFW 规则。节点上 UFW 处于 active 时，需要自行放行该接口，否则来自 tailnet 的流量会被拒绝。
- 开启「Agent 连接优先使用 Tailscale」的节点，其节点证书需要覆盖 tailnet 地址；Seamark 会复用既有 Agent 部署通道刷新证书（只重写证书与配置，不重传二进制）。
- DNS 记录与应用模板变量继续使用公网地址，tailnet 地址不会被发布到这些地方。

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

### 8443 端口已被占用

只修改端口映射左侧的宿主机端口。例如改为宿主机 `9080`：

```yaml
ports:
  - "9080:8443"
```

之后访问 `https://<Seamark主机地址>:9080`。

### 其他设备无法打开页面

检查容器状态、宿主机防火墙、云安全组和发布的宿主机端口。如果映射使用 `127.0.0.1`，Seamark 将只能从本机或本机反向代理访问。

### Tailscale 显示为不可用

设置页提示当前部署无法管理容器内 tailscale。请依次检查：容器是否以 `--cap-add=NET_ADMIN`、`--cap-add=NET_RAW` 和 `--device=/dev/net/tun` 启动；宿主 `tun` 模块是否已加载；镜像内是否存在 `tailscale` 与 `tailscaled` 可执行文件。Seamark 无法在容器内自行修复该前提，Panel 其余功能继续可用，各节点的 Tailscale 意图仍会保存。

### 重新创建容器后数据消失

确认容器仍将原来的命名卷挂载到 `/app/data`：

```bash
docker inspect panel --format '{{json .Mounts}}'
docker volume inspect panel-data
```

继续阅读[使用 Seamark](user-guide.zh-CN.md)。
