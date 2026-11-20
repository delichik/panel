# Patterns（共享交互/展示模式）

本目录的组件遵循 [前端组件设计规范](../../../../docs/agents/specifications/frontend/INDEX.md) 与
[统一交互组件模式](../../../../docs/agents/specifications/frontend/interaction-patterns.md)。
新增可跨页面复用的组件时，必须同步更新对应规范文档。

## 操作系统发行版标识（`osMarkData.ts` / `osMark.ts` / `OsMark.vue`）

- 用途：服务器详情概览摘要卡中标识发行版（Ubuntu、Debian、Arch Linux 等），纯识别用途，
  不得表达“受支持/健康/权限”，列表摘要等其它位置不得读取完整 OS/架构（`SRV-API-001`）。
- 数据来源：[Simple Icons](https://simpleicons.org/)（CC0 1.0），每个发行版为官方 24×24 单路径字形，
  品牌色取自 Simple Icons 徽标元数据。图标与品牌色均本地 vendored，**运行时不请求任何外部 CDN**。
- 许可：字形与颜色数据以 CC0 1.0 提供（<https://creativecommons.org/publicdomain/zero/1.0/>）。
  发行版名称与徽标是其各自权利人的商标，仅用于标识服务器实际运行的系统，不做其它用途。
- 当前收录：Ubuntu、Debian、Arch Linux、Fedora、CentOS、Red Hat、AlmaLinux、Rocky Linux、
  Alpine Linux、openSUSE、Linux Mint、Kali Linux、Manjaro、Raspberry Pi OS、Gentoo、NixOS、
  通用 Linux（Tux）。
- 字形前景：品牌色较亮（白字对比度不足 3:1）时使用深色字形，其余使用白色字形；颜色只取
  `--panel-mark-on-brand` / `--panel-mark-on-light`，不随主题/强调色变化。
- 映射：`resolveOsMark` 先规范化 `os.id`（含 `arch`→`archlinux`、`rocky`→`rockylinux`、`rhel`→`redhat`、
  `opensuse-leap`→`opensuse` 等别名），未知或缺失时按 `prettyName` 关键词回退，仍无法识别返回 `null`
  （组件回退中性 Fluent server 图标）。
- 重新生成：`tmp/gen-os-marks.mjs` 从 `https://cdn.simpleicons.org/<slug>` 拉取字形与品牌色并写入
  `osMarkData.ts`（`tmp/` 为临时目录，不提交）。**禁止手改 `osMarkData.ts`**；增删发行版时同时更新
  `tmp/gen-os-marks.mjs` 的 `entries`、本文件与 `visual-tokens.md` 的图标例外说明。
- 品牌色常量只允许存在于 `osMarkData.ts`，该文件在 `web/src/styles/tokenGuard.test.ts` 的 allowlist 中；
  组件/页面不得再写裸品牌色。

## 架构标识（`ArchitectureMark.vue` / `osMark.ts` 的 `resolveArchitectureMark`）

- 输入详情响应既有 `architecture.arch` / `architecture.rawMachine`，归一为 x86 / arm64 / arm / riscv /
  s390x / ppc / generic 家族；芯片图形内用 X/A/V 区分家族，旁边显示可读架构标签（`amd64`），
  `title`/`aria-label` 使用完整形态（`amd64 (x86_64)`）。
- 中性 `bg-muted` 表面 + `currentColor`，不得使用品牌色/状态色；`architecture` 缺失时不渲染。
- 立即架构未知（如自定义机器类型）只落入 generic 家族，不猜测具体架构。

## 磁盘使用率环形图（`DiskUsageChart.vue`）

- 摘要卡内的磁盘**当前值**环形图，不是历史曲线，也不是 `ChartCard` 图表卡；页面负责已用/可用/总量
  与采样时间图例，组件只绘制比例并给出带百分比的 `role="img"` + `aria-label`。
- `percent = null` 表示尚无有效磁盘样本：环形为空、中心显示 `—`，不得伪造 0%。
- 颜色只使用语义 tone（success/warning/danger/accent）与 `--panel-*` token；阈值由页面
  （`metricsModel.diskUsageTone`，≥85% warning、≥95% danger）提供。
