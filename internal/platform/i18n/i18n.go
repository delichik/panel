package i18n

import (
	"strings"
	"sync"
)

const (
	LocaleEnglish           = "en"
	LocaleSimplifiedChinese = "zh-CN"
	defaultLocale           = LocaleEnglish
)

var (
	mu            sync.RWMutex
	currentLocale = defaultLocale

	codeTranslations = map[string]map[string]string{
		LocaleSimplifiedChinese: {
			"internal_error":                                  "服务器内部错误",
			"bad_request":                                     "请求体 JSON 无效",
			"unauthorized":                                    "未授权",
			"invalid_metrics_retention":                       "指标保留时间至少为 1 天",
			"invalid_metrics_interval":                        "指标采集间隔至少为 10 秒",
			"invalid_cleanup_schedule":                        "清理计划必须为每小时、每天或每周",
			"invalid_token_expiration":                        "令牌过期时间必须为 10 分钟、1 小时、1 天、5 天、30 天或永不过期",
			"invalid_language":                                "语言必须为 English 或简体中文",
			"invalid_log_level":                               "日志等级必须为 debug、info、warn 或 error",
			"invalid_remote_command_timeout":                  "远程命令超时时间至少为 1 秒",
			"invalid_branding_login_title":                    "登录页标题不能超过 80 个字符",
			"invalid_branding_login_subtitle":                 "登录页说明不能超过 240 个字符",
			"invalid_certificate_dns_delay":                   "证书 DNS 生效等待时间不能为负数",
			"invalid_certificate_email":                       "证书邮箱格式无效",
			"invalid_jwt_secret":                              "JWT 密钥至少需要 16 个字符",
			"admin_username_required":                         "用户名不能为空",
			"admin_password_change_required":                  "继续使用前必须修改密码",
			"admin_password_too_short":                        "密码至少需要 8 个字符",
			"admin_password_unchanged":                        "新密码必须和当前密码不同",
			"password_change_required":                        "继续使用前必须修改密码",
			"credential_invalid":                              "凭据名称和用户名不能为空",
			"credential_type_invalid":                         "凭据类型必须为 password 或 private_key",
			"credential_password_required":                    "密码凭据需要密码",
			"credential_private_key_required":                 "私钥凭据需要私钥",
			"credential_in_use":                               "该凭据仍被一个或多个服务器使用",
			"credential_secret_missing":                       "凭据密文缺失",
			"credential_secret_store_missing":                 "凭据密文存储不可用",
			"credential_secret_invalid":                       "凭据密文无效",
			"server_invalid":                                  "服务器名称、主机和 credentialId 不能为空",
			"server_port_invalid":                             "服务器端口必须在 1 到 65535 之间",
			"server_docker_host_required":                     "Docker host 不能为空",
			"server_not_supported":                            "服务器发行版不受支持",
			"server_not_reachable":                            "服务器连通性尚未确认",
			"server_required":                                 "必须选择服务器",
			"server_id_required":                              "必须提供服务器 ID",
			"panel_host_server_required":                     "必须先配置 Panel 宿主节点",
			"panel_host_server_already_set":                  "Panel 宿主节点已经配置",
			"panel_host_server_delete_forbidden":             "不能删除 Panel 宿主节点",
			"server_architecture_invalid":                     "服务器架构响应无效",
			"ufw_not_supported":                               "当前发行版不支持 UFW",
			"ufw_not_installed":                               "该服务器尚未安装 UFW",
			"ufw_rule_required":                               "至少需要一条 UFW 规则",
			"ufw_rule_number_invalid":                         "UFW 规则编号必须为正数",
			"ufw_port_invalid":                                "UFW 端口必须在 1 到 65535 之间",
			"ufw_protocol_invalid":                            "UFW 协议必须为 tcp、udp 或 any",
			"fail2ban_config_invalid":                         "fail2ban YAML 无效",
			"fail2ban_jail_required":                          "至少需要一个 fail2ban jail",
			"fail2ban_jail_name_invalid":                      "fail2ban jail 名称无效",
			"fail2ban_jail_duplicate":                         "fail2ban jail 名称不能重复",
			"fail2ban_maxretry_invalid":                       "fail2ban 最大失败次数不能为负数",
			"fail2ban_value_invalid":                          "fail2ban 参数值必须为单行文本",
			"fail2ban_option_key_invalid":                     "fail2ban 高级参数名无效",
			"fail2ban_takeover_confirmation_required":         "接管已安装的 fail2ban 前必须确认",
			"server_executor_unavailable":                     "服务器连通性测试执行器不可用",
			"agent_tls_unavailable":                           "Agent TLS 资产不可用",
			"agent_required":                                  "此操作需要可用的 Agent",
			"agent_incompatible":                              "Agent 不兼容或不可用",
			"agent_runtime_unavailable":                       "Agent 运行时客户端不可用",
			"agent_binary_unavailable":                        "panel-agent 二进制不可用",
			"agent_auto_deploy_blocked":                       "Agent 自动部署已暂停，请修复错误后手动重装 Agent",
			"agent_auto_deploy_exhausted":                     "Agent 自动部署已多次失败，请修复错误后手动重装 Agent",
			"remote_executor_unavailable":                     "远程执行器不可用",
			"remote_session_failed":                           "无法打开 SSH 会话",
			"remote_command_failed":                           "远程命令执行失败",
			"ssh_connection_failed":                           "SSH 连接失败",
			"ssh_auth_failed":                                 "SSH 认证失败",
			"upload_path_required":                            "上传路径不能为空",
			"remote_upload_failed":                            "远程上传失败",
			"download_not_implemented":                        "SFTP 下载将在后续阶段支持",
			"remote_path_invalid":                             "远程路径必须为绝对路径",
			"remote_file_mode_invalid":                        "远程文件权限模式无效",
			"private_key_invalid":                             "私钥无法解析",
			"passwordless_sudo_required":                      "需要免密 sudo",
			"privileged_access_required":                      "需要 root 或免密 sudo 权限",
			"template_parse_failed":                           "模板解析失败",
			"template_render_failed":                          "模板渲染失败",
			"invalid_selector":                                "选择器无效",
			"dns_domain_invalid":                              "域名必须是有效的 DNS 名称",
			"dns_domain_in_use":                               "该 DNS 域仍被一个或多个证书使用",
			"dns_provider_invalid":                            "DNS 提供商必须为 cloudflare",
			"dns_provider_credentials_unavailable":            "DNS 提供商凭据存储不可用",
			"dns_provider_credentials_invalid":                "DNS 提供商凭据无效",
			"dns_api_token_required":                          "DNS 提供商 API 令牌不能为空",
			"dns_record_id_required":                          "DNS 记录 ID 不能为空",
			"dns_record_name_invalid":                         "DNS 记录名称无效",
			"dns_record_type_invalid":                         "DNS 记录类型无效",
			"dns_record_value_required":                       "DNS 记录值不能为空",
			"dns_record_ttl_invalid":                          "DNS 记录 TTL 不能为负数",
			"dns_record_proxy_invalid":                        "DNS 记录代理只支持 A、AAAA 和 CNAME 记录",
			"cloudflare_api_token_required":                   "Cloudflare API 令牌不能为空",
			"cloudflare_zone_not_found":                       "未找到匹配的 Cloudflare Zone",
			"cloudflare_unreachable":                          "无法访问 Cloudflare API",
			"cloudflare_api_error":                            "Cloudflare API 返回错误",
			"cloudflare_invalid_response":                     "Cloudflare API 返回了非 JSON 响应",
			"certificate_domain_invalid":                      "域名必须是有效的 DNS 名称",
			"certificate_domain_required":                     "至少需要一个域名",
			"certificate_scope_invalid":                       "证书请求范围无效",
			"certificate_variable_invalid":                    "变量名必须以下划线或字母开头，且只能包含字母、数字或下划线",
			"certificate_provider_not_configured":             "证书提供器未配置",
			"certificate_dns_provider_invalid":                "证书 DNS 提供器未配置",
			"certificate_required":                            "必须选择证书",
			"certificate_in_use":                              "证书仍被应用或反向代理使用",
			"certificate_issue_failed":                        "证书签发失败",
			"acme_register_failed":                            "ACME 账号注册失败",
			"acme_order_failed":                               "ACME 订单创建失败",
			"acme_authorization_failed":                       "ACME 授权失败",
			"acme_dns_challenge_missing":                      "ACME 服务端未提供 DNS-01 challenge",
			"acme_dns_challenge_failed":                       "ACME DNS-01 challenge 配置失败",
			"acme_challenge_failed":                           "ACME challenge 验证失败",
			"acme_finalize_failed":                            "ACME 证书 finalize 失败",
			"packages_required":                               "至少需要一个软件包",
			"package_name_invalid":                            "软件包名称包含无效字符",
			"package_task_service_unavailable":                "软件包任务服务不可用",
			"task_not_running":                                "任务未在运行",
			"container_action_invalid":                        "不支持的容器操作",
			"image_reference_required":                        "镜像引用不能为空",
			"image_refresh_running":                           "镜像更新检查正在运行",
			"image_registry_unavailable":                      "镜像仓库解析器未配置",
			"image_registry_unreachable":                      "无法访问镜像仓库",
			"image_registry_auth_unreachable":                 "无法访问镜像仓库认证服务",
			"image_registry_auth_error":                       "镜像仓库认证失败",
			"image_registry_error":                            "镜像仓库返回错误",
			"image_registry_missing_digest":                   "镜像仓库未返回 digest",
			"application_required":                            "必须选择应用",
			"applications_required":                           "至少需要选择一个应用",
			"application_updater_unavailable":                 "应用镜像更新服务不可用",
			"task_service_unavailable":                        "任务服务不可用",
			"volume_in_use":                                   "卷正在被容器使用，不能删除",
			"image_in_use":                                    "镜像正在被容器使用，不能删除",
			"range_invalid":                                   "时间范围必须为 1h、6h、1d 或 7d",
			"overview_cards_too_many":                         "概览仪表盘最多只能包含 100 张卡片",
			"overview_card_id_invalid":                        "概览卡片 ID 不能为空",
			"overview_card_id_duplicate":                      "概览卡片 ID 不能重复",
			"overview_card_kind_invalid":                      "概览卡片类型无效",
			"overview_card_size_invalid":                      "概览卡片尺寸无效",
			"overview_card_range_invalid":                     "概览卡片时间范围无效",
			"overview_card_network_direction_invalid":         "概览卡片网络方向无效",
			"overview_card_server_id_invalid":                 "概览卡片服务器 ID 不能为空",
			"overview_card_server_id_duplicate":               "概览卡片服务器 ID 不能重复",
			"task_type_required":                              "任务类型不能为空",
			"task_type_unregistered":                          "任务类型未注册",
			"task_type_registered":                            "任务类型已注册",
			"task_batch_type_mismatch":                        "批量任务输入必须使用同一个任务类型",
			"task_executor_unregistered":                      "任务执行器未注册",
			"task_step_required":                              "任务步骤不能为空",
			"task_not_runnable":                               "任务已经结束，不能再次运行",
			"task_run_now_unsupported":                        "该任务当前不支持立即运行",
			"task_run_now_status_invalid":                     "只有排队、已调度或等待重试的任务可以立即运行",
			"task_retry_unsupported":                          "该任务当前不支持从任务中心重试",
			"task_retry_status_invalid":                       "只有失败、等待重试或已阻塞的任务可以重试",
			"application_invalid":                             "应用配置无效",
			"application_spec_yaml_invalid":                   "应用 YAML 无效",
			"application_name_duplicate":                      "应用名称不能重复",
			"application_command_invalid":                     "command 只能包含可执行程序；参数请放到 args",
			"application_enabled":                             "删除前请先禁用应用",
			"application_disabled":                            "更新镜像前请先启用应用",
			"application_image_empty":                         "应用镜像不能为空",
			"application_image_invalid":                       "应用镜像无效",
			"application_task_resource_required":              "应用任务资源不能为空",
			"application_no_runtime_targets":                  "没有可用的 Agent 运行时目标",
			"application_reconciler_unavailable":              "应用协调器不可用",
			"application_migration_servers_required":          "必须选择源服务器和目标服务器",
			"application_migration_servers_must_differ":       "源服务器和目标服务器不能相同",
			"application_migration_requires_enabled":          "迁移前必须先启用应用",
			"application_migration_persistent_not_supported":  "使用持久化存储的应用不能进行无损迁移",
			"application_migration_source_not_exclusive":      "源服务器必须是唯一的应用部署实例",
			"application_migration_target_exists":             "目标服务器已存在该应用部署",
			"application_migration_source_not_running":        "源应用实例必须处于运行中",
			"application_migration_mounts_not_supported":      "使用宿主机路径或 Docker 卷的应用不能进行无损迁移",
			"application_persistent_data_unavailable":         "应用未使用持久化存储",
			"application_persistent_archive_invalid":          "Agent 返回的持久化数据压缩包无效",
			"application_persistent_archive_required":         "必须上传持久化数据压缩包",
			"runtime_instance_required":                       "必须选择运行时实例",
			"application_deployment_mode_invalid":             "部署模式必须为 all 或 selected",
			"application_deployment_servers_required":         "请至少选择一个部署服务器",
			"application_persistent_single_target_required":   "持久化应用必须且只能部署到一个服务器",
			"application_reverse_proxy_target_type_invalid":   "反向代理目标类型无效",
			"application_reverse_proxy_target_port_invalid":   "反向代理目标端口必须在 1 到 65535 之间",
			"application_reverse_proxy_path_invalid":          "反向代理路径必须以 / 开头",
			"application_reverse_proxy_path_duplicate":        "同一域名下的反向代理路径不能重复",
			"application_reverse_proxy_domain_invalid":        "反向代理域名无效",
			"reverse_proxy_gzip_mode_invalid":                 "Gzip 模式无效",
			"reverse_proxy_buffering_mode_invalid":            "代理缓冲模式无效",
			"reverse_proxy_websocket_mode_invalid":            "WebSocket 模式无效",
			"reverse_proxy_option_invalid":                    "反向代理 Path 高级参数无效",
			"reverse_proxy_headers_too_many":                  "自定义 Header 数量过多",
			"reverse_proxy_header_name_invalid":               "自定义 Header 名称无效",
			"reverse_proxy_header_value_invalid":              "自定义 Header 值无效",
			"reverse_proxy_header_duplicate":                  "自定义 Header 不能重复",
			"reverse_proxy_origin_servers_required":           "每条反向代理路由至少需要一个源站节点",
			"reverse_proxy_origin_server_invalid":             "源站节点必须部署该应用并属于全局网关节点",
			"reverse_proxy_any_access_strategy_invalid":       "AnyAccess 流量分配策略无效",
			"reverse_proxy_any_access_primary_origin_invalid": "主备策略必须选择有效的主源站",
			"reverse_proxy_domain_owner_conflict":             "该域名已被其他反向代理入口使用",
			"reverse_proxy_domain_duplicate":                  "同一应用内的反向代理域名不能重复",
			"application_file_archive_invalid":                "\u5e94\u7528\u6587\u4ef6\u5939\u538b\u7f29\u5305\u65e0\u6548",
			"application_file_archive_empty":                  "\u5e94\u7528\u6587\u4ef6\u5939\u538b\u7f29\u5305\u4e3a\u7a7a",
			"application_file_kind_invalid":                   "应用文件类型无效",
			"application_file_content_invalid":                "应用文件内容无效",
			"application_file_mount_missing":                  "应用文件挂载引用缺失",
			"application_file_path_invalid":                   "应用文件路径无效",
			"panel_file_provider_unavailable":                 "Panel 文件提供器不可用",
			"panel_file_source_invalid":                       "Panel 文件来源无效",
			"panel_file_kind_invalid":                         "Panel 文件类型无效",
			"facility_static_site_domain_invalid":             "静态站点域名无效",
			"facility_static_site_root_invalid":               "静态站点宿主机根目录无效",
			"facility_static_site_path_invalid":               "静态站点路径必须以 / 开头",
			"facility_static_site_path_duplicate":             "静态站点路径不能重复",
			"facility_static_site_source_invalid":             "静态站点来源无效",
			"facility_static_site_rule_invalid":               "静态站点规则无效",
			"facility_static_site_redirect_invalid":           "静态站点重定向目标无效",
			"facility_static_site_proxy_invalid":              "静态站点反向代理目标无效",
			"facility_static_site_proxy_mode_invalid":         "静态站点请求信息模式无效",
			"facility_static_site_asset_required":             "请选择静态站点资产",
			"facility_static_site_asset_kind_invalid":         "静态站点资产类型与来源不匹配",
			"facility_static_site_server_invalid":             "静态站点服务器必须已启用反向代理设施应用",
			"facility_domain_invalid":                         "反向代理设施域名无效",
			"facility_domain_duplicate":                       "反向代理设施域名不能重复",
			"facility_domain_servers_required":                "每个域名必须至少选择一个入口节点",
			"facility_domain_server_invalid":                  "域名入口节点必须属于设施的全局网关节点",
			"facility_domain_server_host_invalid":             "域名上游节点地址无效",
			"facility_domain_strategy_invalid":                "域名上游调度策略无效",
			"facility_domain_primary_server_invalid":          "主备模式必须选择一个入口节点作为主节点",
			"facility_domain_without_routes":                  "域名必须至少包含一条路由",
			"facility_domain_origin_servers_required":         "每个设施域名至少需要一个源站节点",
			"facility_domain_origin_server_invalid":           "设施域名的源站节点必须属于全局网关节点",
			"facility_domain_owner_conflict":                  "该域名已被其他反向代理入口使用",
			"facility_route_conflict":                         "反向代理路由与现有路由冲突",
			"facility_panel_entry_server_required":            "面板入口必须选择宿主机",
			"facility_panel_entry_host_required":              "Panel 入口必须使用已配置的 Panel 宿主节点",
			"facility_panel_entry_server_invalid":             "面板宿主机必须是已选择的入口节点",
			"facility_panel_entry_domain_invalid":             "面板入口域名无效",
			"facility_panel_entry_static_conflict":            "面板入口域名与静态路由冲突",
			"facility_panel_entry_route_conflict":             "面板入口域名与应用路由冲突",
			"facility_panel_entry_upstream_domain_conflict":   "面板入口不能与上游模式域名共用域名",
			"facility_upstream_domain_application_conflict":   "上游模式域名由反向代理设施独占，不能与普通应用共用",
			"facility_reverse_proxy_config_changed":           "反向代理设施配置已被其他操作修改，请重新加载后再保存",
			"facility_static_asset_name_required":             "静态资产名称不能为空",
			"facility_static_asset_kind_invalid":              "静态资产类型无效",
			"facility_static_asset_file_required":             "请选择静态资产文件",
			"facility_static_asset_storage_unavailable":       "静态资产存储不可用",
			"facility_static_asset_archive_invalid":           "静态资产压缩包无效",
			"facility_static_asset_archive_empty":             "静态资产压缩包为空",
			"facility_static_asset_in_use":                    "静态资产仍被反向代理使用",
			"server_provider_unavailable":                     "服务器服务不可用",
			"invalid_server_variable_name":                    "服务器变量名称无效",
			"invalid_server_variable_key":                     "服务器变量键无效",
			"duplicate_server_variable_key":                   "服务器变量键不能重复",
			"key_asset_master_key_missing":                    "存在已加密的敏感数据，但主密钥缺失",
			"key_asset_master_key_invalid":                    "密钥资产主密钥无效",
			"key_asset_type_invalid":                          "密钥资产内容无效",
			"key_asset_encrypted_private_key_unsupported":     "暂不支持导入带密码的私钥",
			"key_asset_key_pair_mismatch":                     "公钥与私钥不匹配",
			"key_asset_ca_required":                           "该 TLS 证书缺少可用于重签发的 CA",
			"key_asset_ca_invalid":                            "所选 CA 无效",
			"key_asset_in_use":                                "该密钥资产仍被应用或反向代理使用",
			"key_asset_ca_has_children":                       "该 CA 仍有子证书，无法删除",
			"key_asset_archive_password_invalid":              "导出归档密码无效",
			"key_asset_archive_tampered":                      "密钥资产归档已损坏、密码错误或内容被篡改",
			"key_asset_archive_version_unsupported":           "不支持该密钥资产归档版本",
			"key_asset_archive_kdf_invalid":                   "密钥资产归档的密钥派生参数无效",
			"key_asset_import_conflict":                       "导入资产存在冲突",
			"key_asset_import_plan_expired":                   "导入预检计划已过期",
			"key_asset_import_confirmation_required":          "覆盖使用中的密钥资产前需要危险确认",
			"restore_password_required":                       "备份密码不能为空",
			"restore_password_invalid":                        "备份密码无效",
			"restore_archive_invalid":                         "备份归档无效",
			"restore_compatibility_failed":                    "备份归档版本不受支持",
			"restore_archive_required":                        "必须上传备份归档",
			"restore_confirmation_required":                   "必须确认覆盖还原",
		},
	}

	exactTranslations = map[string]map[string]string{
		LocaleSimplifiedChinese: {
			"Authentication required":                                                       "需要登录认证",
			"Authentication failed":                                                         "认证失败",
			"Invalid username or password":                                                  "用户名或密码错误",
			"Invalid JSON request body":                                                     "请求体 JSON 无效",
			"Metrics retention must be at least 1 day":                                      "指标保留时间至少为 1 天",
			"Metrics collection interval must be at least 10 seconds":                       "指标采集间隔至少为 10 秒",
			"Cleanup schedule must be hourly, daily, or weekly":                             "清理计划必须为每小时、每天或每周",
			"Token expiration must be 10 minutes, 1 hour, 1 day, 5 days, 30 days, or never": "令牌过期时间必须为 10 分钟、1 小时、1 天、5 天、30 天或永不过期",
			"Language must be English or Simplified Chinese":                                "语言必须为 English 或简体中文",
			"Log level must be debug, info, warn, or error":                                 "日志等级必须为 debug、info、warn 或 error",
			"Remote command timeout must be at least 1 second":                              "远程命令超时时间至少为 1 秒",
			"Login title must be 80 characters or fewer":                                    "登录页标题不能超过 80 个字符",
			"Login subtitle must be 240 characters or fewer":                                "登录页说明不能超过 240 个字符",
			"Certificate DNS propagation delay cannot be negative":                          "证书 DNS 生效等待时间不能为负数",
			"Certificate email must be valid":                                               "证书邮箱格式无效",
			"JWT secret must be at least 16 characters":                                     "JWT 密钥至少需要 16 个字符",
			"Username is required":                                                          "用户名不能为空",
			"Password change required":                                                      "继续使用前必须修改密码",
			"Password must be changed before continuing":                                    "继续使用前必须修改密码",
			"Password must be at least 8 characters":                                        "密码至少需要 8 个字符",
			"New password must be different from the current password":                      "新密码必须和当前密码不同",
			"Credential is still used by one or more servers":                               "该凭据仍被一个或多个服务器使用",
			"Password credential requires a password":                                       "密码凭据需要密码",
			"Private key credential requires a private key":                                 "私钥凭据需要私钥",
			"Password credential requires a password when changing type":                    "切换到密码凭据时必须提供密码",
			"Private key credential requires a private key when changing type":              "切换到私钥凭据时必须提供私钥",
			"Server distribution is not supported":                                          "服务器发行版不受支持",
			"UFW is not supported on this distribution":                                     "当前发行版不支持 UFW",
			"UFW is not installed on this server":                                           "该服务器尚未安装 UFW",
			"At least one UFW rule is required":                                             "至少需要一条 UFW 规则",
			"UFW rule number must be positive":                                              "UFW 规则编号必须为正数",
			"UFW port must be between 1 and 65535":                                          "UFW 端口必须在 1 到 65535 之间",
			"UFW protocol must be tcp, udp, or any":                                         "UFW 协议必须为 tcp、udp 或 any",
			"Remote executor is unavailable":                                                "远程执行器不可用",
			"Remote path must be absolute":                                                  "远程路径必须为绝对路径",
			"Remote file mode is invalid":                                                   "远程文件权限模式无效",
			"Server connectivity has not been confirmed":                                    "服务器连通性尚未确认",
			"Passwordless sudo is required":                                                 "需要免密 sudo",
			"At least one package is required":                                              "至少需要一个软件包",
			"Package name contains invalid characters":                                      "软件包名称包含无效字符",
			"Package task service is unavailable":                                           "软件包任务服务不可用",
			"This task type cannot be run from the task center":                             "该任务类型不能从任务中心直接运行",
			"Certificate issuer is not configured":                                          "证书签发器未配置",
			"Domain must be a valid DNS name":                                               "域名必须是有效的 DNS 名称",
			"DNS provider must be cloudflare":                                               "DNS 提供商必须为 cloudflare",
			"DNS provider API token is required":                                            "DNS 提供商 API 令牌不能为空",
			"DNS record ID is required":                                                     "DNS 记录 ID 不能为空",
			"DNS record name is invalid":                                                    "DNS 记录名称无效",
			"DNS record type is invalid":                                                    "DNS 记录类型无效",
			"DNS record value is required":                                                  "DNS 记录值不能为空",
			"DNS record TTL cannot be negative":                                             "DNS 记录 TTL 不能为负数",
			"DNS record proxy is only supported for A, AAAA, and CNAME records":             "DNS 记录代理只支持 A、AAAA 和 CNAME 记录",
			"Cloudflare API token is required":                                              "Cloudflare API 令牌不能为空",
			"Cloudflare API returned a non-JSON response":                                   "Cloudflare API 返回了非 JSON 响应",
			"DNS domain is still used by one or more certificates":                          "该 DNS 域仍被一个或多个证书使用",
			"Certificate request scope is invalid":                                          "证书请求范围无效",
			"Variable name must start with a letter or underscore and contain only letters, digits, or underscores": "变量名必须以下划线或字母开头，且只能包含字母、数字或下划线",
			"Certificate provider is not configured":                    "证书提供器未配置",
			"Unsupported DNS provider":                                  "不支持的 DNS 提供商",
			"Credential type must be password or private_key":           "凭据类型必须为 password 或 private_key",
			"Server connectivity test executor is unavailable":          "服务器连通性测试执行器不可用",
			"Server name, host, and credentialId are required":          "服务器名称、主机和 credentialId 不能为空",
			"Server host and credentialId are required":                 "服务器主机和 credentialId 不能为空",
			"Server port must be between 1 and 65535":                   "服务器端口必须在 1 到 65535 之间",
			"Task type is required":                                     "任务类型不能为空",
			"Task step is required":                                     "任务步骤不能为空",
			"Range must be 1h, 6h, 1d, or 7d":                           "时间范围必须为 1h、6h、1d 或 7d",
			"Only queued, scheduled, or retryable tasks can be run now": "只有排队、已调度或等待重试的任务可以立即运行",
			"Only failed, retryable, or blocked tasks can be retried":   "只有失败、等待重试或已阻塞的任务可以重试",
			"This task type cannot be retried from the task center":     "该任务类型不能从任务中心重试",
			"Server ID is required":                                     "必须提供服务器 ID",
			"Application image is empty":                                "应用镜像不能为空",
			"name must be 1-32 lowercase letters, digits, or hyphens and start and end with an alphanumeric character": "名称必须为 1 到 32 个小写字母、数字或连字符，并且以字母或数字开头和结尾",
			"image is required": "镜像不能为空",
			"command must contain only the executable; put flags and values in args":           "command 只能包含可执行程序；参数和值请放到 args",
			"networkMode must be bridge or host":                                               "networkMode 必须为 bridge 或 host",
			"cpu cannot be negative":                                                           "CPU 不能为负数",
			"memoryMb cannot be negative":                                                      "memoryMb 不能为负数",
			"capability must use uppercase letters, digits, or underscores":                    "capability 只能使用大写字母、数字或下划线",
			"restart policy must be no, on-failure, always, or unless-stopped":                 "重启策略必须为 no、on-failure、always 或 unless-stopped",
			"restart attempts cannot be negative":                                              "重启尝试次数不能为负数",
			"restart interval cannot be negative":                                              "重启间隔不能为负数",
			"restart delay cannot be negative":                                                 "重启延迟不能为负数",
			"restart mode must be fail or delay":                                               "重启模式必须为 fail 或 delay",
			"ports cannot be configured when networkMode is host":                              "networkMode 为 host 时不能配置 ports",
			"port label must use application name format":                                      "端口标签必须使用应用名称格式",
			"target port must be between 1 and 65535":                                          "目标端口必须在 1 到 65535 之间",
			"static port must be between 1 and 65535":                                          "静态端口必须在 1 到 65535 之间",
			"service port must reference an existing port label":                               "服务端口必须引用已有端口标签",
			"check type must be tcp, http, or script":                                          "健康检查类型必须为 tcp、http 或 script",
			"check port must reference an existing port label":                                 "健康检查端口必须引用已有端口标签",
			"volume source is required":                                                        "卷来源不能为空",
			"volume target must be an absolute Linux path":                                     "卷目标路径必须为 Linux 绝对路径",
			"mount type must be volume, host, global, file, panel_file, or persistent":         "挂载类型必须为 volume、host、global、file、panel_file 或 persistent",
			"mount source is required":                                                         "挂载来源不能为空",
			"workspace mount source must be a relative path inside the application workspace":  "工作区挂载来源必须是应用工作区内的相对路径",
			"Panel file source is invalid":                                                     "Panel 文件来源无效",
			"host path mount source must be an absolute Linux path":                            "宿主机路径挂载来源必须为 Linux 绝对路径",
			"mount target must be an absolute Linux path":                                      "挂载目标路径必须为 Linux 绝对路径",
			"mount uid cannot be negative":                                                     "挂载 uid 不能为负数",
			"mount gid cannot be negative":                                                     "挂载 gid 不能为负数",
			"mount mode must be an octal file mode such as 0755":                               "挂载 mode 必须为八进制文件权限，例如 0755",
			"mount uid and gid are only supported for file, panel_file, and persistent mounts": "挂载 uid 和 gid 仅支持 file、panel_file 和 persistent 挂载",
			"mount mode is only supported for file and persistent mounts":                      "挂载 mode 仅支持 file 和 persistent 挂载",
			"Disable the application before deleting it":                                       "删除前请先禁用应用",
			"enable the application before updating its image":                                 "更新镜像前请先启用应用",
			"deployment mode must be all or selected":                                          "部署模式必须为 all 或 selected",
			"select at least one deployment server":                                            "请至少选择一个部署服务器",
			"persistent applications must target exactly one server":                           "持久化应用必须且只能部署到一个服务器",
			"reverse proxy target port must be between 1 and 65535":                            "反向代理目标端口必须在 1 到 65535 之间",
			"reverse proxy path must start with /":                                             "反向代理路径必须以 / 开头",
			"reverse proxy domain is invalid":                                                  "反向代理域名无效",
			"Static site domain is invalid":                                                    "静态站点域名无效",
			"Static site root path is invalid":                                                 "静态站点宿主机根目录无效",
			"Static site path must start with /":                                               "静态站点路径必须以 / 开头",
			"Server provider is unavailable":                                                   "服务器服务不可用",
			"Agent is required for facility applications":                                      "设施应用需要可用的 Agent",
			"Agent is not compatible with facility applications":                               "Agent 不兼容或不可用，无法处理设施应用",
			"Application reconciler is unavailable":                                            "应用协调器不可用",
			"Encrypted private keys are not supported":                                         "暂不支持导入带密码的私钥",
			"Public key does not match the private key":                                        "公钥与私钥不匹配",
			"Certificate authority still has child certificates":                               "该 CA 仍有子证书，无法删除",
			"Key asset is still used by an application or reverse proxy":                       "该密钥资产仍被应用或反向代理使用",
		},
	}

	codePrefixTranslations = map[string]map[string]map[string]string{
		LocaleSimplifiedChinese: {
			"acme_register_failed": {
				"ACME account registration failed: ": "ACME 账号注册失败：",
			},
			"acme_order_failed": {
				"ACME order failed: ": "ACME 订单创建失败：",
			},
			"acme_authorization_failed": {
				"ACME authorization failed: ": "ACME 授权失败：",
			},
			"acme_dns_challenge_failed": {
				"ACME DNS-01 challenge setup failed: ": "ACME DNS-01 challenge 配置失败：",
			},
			"acme_challenge_failed": {
				"ACME challenge failed: ": "ACME challenge 验证失败：",
			},
			"acme_finalize_failed": {
				"ACME certificate finalization failed: ": "ACME 证书 finalize 失败：",
			},
			"cloudflare_zone_not_found": {
				"Cloudflare zone not found for ": "未找到匹配的 Cloudflare Zone：",
			},
			"cloudflare_unreachable": {
				"Cloudflare API unreachable: ": "无法访问 Cloudflare API：",
			},
			"cloudflare_api_error": {
				"Cloudflare API error ": "Cloudflare API 错误 ",
			},
			"application_runtime_operation_failed": {
				"Application runtime operation failed: deployment failed on ": "应用运行时操作失败：部署失败目标 ",
				"Application runtime operation failed: ":                      "应用运行时操作失败：",
			},
			"agent_request_failed": {
				"Agent request failed: ": "Agent 请求失败：",
			},
		},
	}

	prefixCodeTranslations = map[string]map[string]string{
		LocaleSimplifiedChinese: {
			"Application runtime operation failed: deployment failed on ": "应用运行时操作失败：部署失败目标 ",
			"Application runtime operation failed: ":                      "应用运行时操作失败：",
		},
	}
)

func SupportedLocales() []string {
	return []string{LocaleEnglish, LocaleSimplifiedChinese}
}

func NormalizeLocale(value string) string {
	candidate := strings.TrimSpace(strings.ToLower(value))
	switch candidate {
	case "", "en", "en-us", "en-gb":
		return LocaleEnglish
	case "zh", "zh-cn", "zh-hans", "zh-hans-cn", "cn":
		return LocaleSimplifiedChinese
	default:
		return ""
	}
}

func IsSupportedLocale(value string) bool {
	return NormalizeLocale(value) != ""
}

func DefaultLocale() string {
	mu.RLock()
	defer mu.RUnlock()
	return currentLocale
}

func SetDefaultLocale(value string) {
	locale := NormalizeLocale(value)
	if locale == "" {
		locale = defaultLocale
	}
	mu.Lock()
	currentLocale = locale
	mu.Unlock()
}

func Translate(code, fallback string) string {
	return TranslateLocale(DefaultLocale(), code, fallback)
}

func TranslateLocale(locale, code, fallback string) string {
	normalized := NormalizeLocale(locale)
	if normalized == "" || normalized == LocaleEnglish {
		return fallback
	}
	if byCodePrefix := codePrefixTranslations[normalized][code]; byCodePrefix != nil {
		if translated := translateByPrefix(fallback, byCodePrefix); translated != "" {
			return translated
		}
	}
	if byCode := codeTranslations[normalized]; byCode != nil {
		if translated := byCode[code]; translated != "" {
			return translated
		}
	}
	if exact := exactTranslations[normalized][fallback]; exact != "" {
		return exact
	}
	if prefixTranslations := prefixCodeTranslations[normalized]; prefixTranslations != nil {
		if translated := translateByPrefix(fallback, prefixTranslations); translated != "" {
			return translated
		}
	}
	if strings.HasSuffix(fallback, " not found") {
		resource := strings.TrimSuffix(fallback, " not found")
		if resource != "" {
			return resource + " 未找到"
		}
	}
	return fallback
}

func translateByPrefix(fallback string, prefixTranslations map[string]string) string {
	bestPrefix := ""
	bestTranslation := ""
	for prefix, translated := range prefixTranslations {
		if strings.HasPrefix(fallback, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestTranslation = translated
		}
	}
	if bestPrefix == "" {
		return ""
	}
	return bestTranslation + strings.TrimPrefix(fallback, bestPrefix)
}
