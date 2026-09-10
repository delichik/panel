# 旧功能模块指引（非规范）

本目录已由 [项目设计与验收规范](../acceptance/README.md) 替代。这里保留的文件只用于追查历史实现背景，不再是后续开发、评审或验收的行为依据，也不要求随功能改动继续维护。

处理任何功能改动时必须从 [验收规范索引](../acceptance/README.md) 定位稳定验收编号；代码与旧模块指引冲突时，不得据此覆盖验收合同。

## 替代关系

| 旧指引 | 规范性替代文档 |
| --- | --- |
| `backend-core.md` | [架构、数据与 API](../acceptance/architecture-data-api.md)、[身份、设置与系统](../acceptance/identity-settings-system.md) |
| `database-orm.md` | [架构、数据与 API](../acceptance/architecture-data-api.md) |
| `backup-restore.md` | [备份、恢复与诊断](../acceptance/backup-restore-diagnostics.md) |
| `frontend.md` | [界面外壳与交互约定](../acceptance/ui-shell-and-conventions.md)、[逐页面验收](../acceptance/ui-pages.md) |
| `servers.md` | [服务器、安全与软件包](../acceptance/servers-security-packages.md) |
| `tasks-scheduler.md`、`runtime-events.md` | [协调、任务与运行事件](../acceptance/orchestration-and-tasks.md) |
| `applications.md` | [应用与设施应用](../acceptance/applications-and-facilities.md) |
| `containerization.md` | [容器与资源](../acceptance/containers-and-resources.md) |
| `dns-certificates.md` | [DNS、证书与密钥资产](../acceptance/dns-certificates-key-assets.md) |
| `release-workflow.md` | [工程质量与发布](../acceptance/engineering-release.md) |

完整的产品边界、跨模块关系、数据库/API/路由基线和变更完成定义只在新验收规范中维护。
