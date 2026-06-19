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
			"internal_error":                                "服务器内部错误",
			"bad_request":                                   "请求体 JSON 无效",
			"unauthorized":                                  "未授权",
			"invalid_metrics_retention":                     "指标保留时间至少为 1 天",
			"invalid_metrics_interval":                      "指标采集间隔至少为 10 秒",
			"invalid_cleanup_schedule":                      "清理计划必须为每小时、每天或每周",
			"invalid_token_expiration":                      "令牌过期时间必须为 10 分钟、1 小时、1 天、5 天、30 天或永不过期",
			"invalid_language":                              "语言必须为 English 或简体中文",
			"invalid_log_level":                             "日志等级必须为 debug、info、warn 或 error",
			"invalid_remote_command_timeout":                "远程命令超时时间至少为 1 秒",
			"invalid_branding_login_title":                  "登录页标题不能超过 80 个字符",
			"invalid_branding_login_subtitle":               "登录页说明不能超过 240 个字符",
			"invalid_certificate_dns_delay":                 "证书 DNS 生效等待时间不能为负数",
			"invalid_certificate_email":                     "证书邮箱格式无效",
			"invalid_jwt_secret":                            "JWT 密钥至少需要 16 个字符",
			"admin_username_required":                       "用户名不能为空",
			"admin_password_change_required":                "继续使用前必须修改密码",
			"admin_password_too_short":                      "密码至少需要 8 个字符",
			"admin_password_unchanged":                      "新密码必须和当前密码不同",
			"password_change_required":                      "继续使用前必须修改密码",
			"credential_invalid":                            "凭据名称和用户名不能为空",
			"credential_type_invalid":                       "凭据类型必须为 password 或 private_key",
			"credential_password_required":                  "密码凭据需要密码",
			"credential_private_key_required":               "私钥凭据需要私钥",
			"server_invalid":                                "服务器名称、主机和 credentialId 不能为空",
			"server_port_invalid":                           "服务器端口必须在 1 到 65535 之间",
			"server_not_supported":                          "服务器发行版不受支持",
			"server_not_reachable":                          "服务器连通性尚未确认",
			"ufw_not_supported":                             "当前发行版不支持 UFW",
			"ufw_not_installed":                             "该服务器尚未安装 UFW",
			"ufw_rule_required":                             "至少需要一条 UFW 规则",
			"ufw_rule_number_invalid":                       "UFW 规则编号必须为正数",
			"ufw_port_invalid":                              "UFW 端口必须在 1 到 65535 之间",
			"ufw_protocol_invalid":                          "UFW 协议必须为 tcp、udp 或 any",
			"server_executor_unavailable":                   "服务器连通性测试执行器不可用",
			"agent_tls_unavailable":                         "Agent TLS 资产不可用",
			"agent_required":                                "此操作需要可用的 Agent",
			"agent_incompatible":                            "Agent 不兼容或不可用",
			"agent_runtime_unavailable":                     "Agent 运行时客户端不可用",
			"agent_binary_unavailable":                      "panel-agent 二进制不可用",
			"agent_auto_deploy_exhausted":                   "Agent 自动部署已多次失败，请修复错误后手动重装 Agent",
			"remote_executor_unavailable":                   "远程执行器不可用",
			"upload_path_required":                          "上传路径不能为空",
			"remote_upload_failed":                          "远程上传失败",
			"remote_path_invalid":                           "远程路径必须为绝对路径",
			"remote_file_mode_invalid":                      "远程文件权限模式无效",
			"passwordless_sudo_required":                    "需要免密 sudo",
			"privileged_access_required":                    "需要 root 或免密 sudo 权限",
			"dns_domain_invalid":                            "域名必须是有效的 DNS 名称",
			"dns_provider_invalid":                          "DNS 提供商必须为 cloudflare",
			"dns_provider_credentials_unavailable":          "DNS 提供商凭据存储不可用",
			"dns_provider_credentials_invalid":              "DNS 提供商凭据无效",
			"dns_api_token_required":                        "DNS 提供商 API 令牌不能为空",
			"dns_record_id_required":                        "DNS 记录 ID 不能为空",
			"dns_record_name_invalid":                       "DNS 记录名称无效",
			"dns_record_type_invalid":                       "DNS 记录类型无效",
			"dns_record_value_required":                     "DNS 记录值不能为空",
			"dns_record_ttl_invalid":                        "DNS 记录 TTL 不能为负数",
			"dns_record_proxy_invalid":                      "DNS 记录代理只支持 A、AAAA 和 CNAME 记录",
			"cloudflare_api_token_required":                 "Cloudflare API 令牌不能为空",
			"cloudflare_invalid_response":                   "Cloudflare API 返回了非 JSON 响应",
			"certificate_domain_invalid":                    "域名必须是有效的 DNS 名称",
			"certificate_scope_invalid":                     "证书范围必须为 single 或 wildcard",
			"certificate_variable_invalid":                  "变量名必须以下划线或字母开头，且只能包含字母、数字或下划线",
			"certificate_provider_not_configured":           "证书提供器未配置",
			"certificate_dns_provider_invalid":              "证书 DNS 提供器未配置",
			"packages_required":                             "至少需要一个软件包",
			"package_name_invalid":                          "软件包名称包含无效字符",
			"package_task_service_unavailable":              "软件包任务服务不可用",
			"container_action_invalid":                      "不支持的容器操作",
			"image_reference_required":                      "镜像引用不能为空",
			"image_refresh_running":                         "镜像更新检查正在运行",
			"applications_required":                         "至少需要选择一个应用",
			"application_updater_unavailable":               "应用镜像更新服务不可用",
			"volume_in_use":                                 "卷正在被容器使用，不能删除",
			"image_in_use":                                  "镜像正在被容器使用，不能删除",
			"range_invalid":                                 "时间范围必须为 1h、6h、1d 或 7d",
			"overview_cards_too_many":                       "概览仪表盘最多只能包含 100 张卡片",
			"overview_card_id_invalid":                      "概览卡片 ID 不能为空",
			"overview_card_id_duplicate":                    "概览卡片 ID 不能重复",
			"overview_card_kind_invalid":                    "概览卡片类型无效",
			"overview_card_size_invalid":                    "概览卡片尺寸无效",
			"overview_card_range_invalid":                   "概览卡片时间范围无效",
			"overview_card_network_direction_invalid":       "概览卡片网络方向无效",
			"overview_card_server_id_invalid":               "概览卡片服务器 ID 不能为空",
			"overview_card_server_id_duplicate":             "概览卡片服务器 ID 不能重复",
			"task_type_required":                            "任务类型不能为空",
			"task_step_required":                            "任务步骤不能为空",
			"task_not_runnable":                             "任务已经结束，不能再次运行",
			"task_run_now_unsupported":                      "该任务当前不支持立即运行",
			"task_run_now_status_invalid":                   "只有排队、已调度或等待重试的任务可以立即运行",
			"task_retry_unsupported":                        "该任务当前不支持从任务中心重试",
			"task_retry_status_invalid":                     "只有失败、等待重试或已阻塞的任务可以重试",
			"application_invalid":                           "应用配置无效",
			"application_spec_yaml_invalid":                 "应用 YAML 无效",
			"application_name_duplicate":                    "应用名称不能重复",
			"application_command_invalid":                   "command 只能包含可执行程序；参数请放到 args",
			"application_enabled":                           "删除前请先禁用应用",
			"application_disabled":                          "更新镜像前请先启用应用",
			"application_image_empty":                       "应用镜像不能为空",
			"application_image_invalid":                     "应用镜像无效",
			"application_deployment_mode_invalid":           "部署模式必须为 all 或 selected",
			"application_deployment_servers_required":       "请至少选择一个部署服务器",
			"application_persistent_single_target_required": "持久化应用必须且只能部署到一个服务器",
			"application_reverse_proxy_target_port_invalid": "反向代理目标端口必须在 1 到 65535 之间",
			"application_reverse_proxy_path_invalid":        "反向代理路径必须以 / 开头",
			"application_reverse_proxy_domain_invalid":      "反向代理域名无效",
			"key_asset_master_key_missing":                  "存在已加密的敏感数据，但主密钥缺失",
			"key_asset_master_key_invalid":                  "密钥资产主密钥无效",
			"key_asset_type_invalid":                        "密钥资产内容无效",
			"key_asset_encrypted_private_key_unsupported":   "暂不支持导入带密码的私钥",
			"key_asset_key_pair_mismatch":                   "公钥与私钥不匹配",
			"key_asset_ca_required":                         "该 TLS 证书缺少可用于重签发的 CA",
			"key_asset_ca_invalid":                          "所选 CA 无效",
			"key_asset_in_use":                              "该密钥资产仍被应用或反向代理使用",
			"key_asset_ca_has_children":                     "该 CA 仍有子证书，无法删除",
			"key_asset_archive_password_invalid":            "导出归档密码无效",
			"key_asset_archive_tampered":                    "密钥资产归档已损坏、密码错误或内容被篡改",
			"key_asset_archive_version_unsupported":         "不支持该密钥资产归档版本",
			"key_asset_archive_kdf_invalid":                 "密钥资产归档的密钥派生参数无效",
			"key_asset_import_conflict":                     "导入资产存在冲突",
			"key_asset_import_plan_expired":                 "导入预检计划已过期",
			"key_asset_import_confirmation_required":        "覆盖使用中的密钥资产前需要危险确认",
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
			"Certificate scope must be single or wildcard":                                  "证书范围必须为 single 或 wildcard",
			"Variable name must start with a letter or underscore and contain only letters, digits, or underscores": "变量名必须以下划线或字母开头，且只能包含字母、数字或下划线",
			"Certificate provider is not configured":                     "证书提供器未配置",
			"Unsupported DNS provider":                                   "不支持的 DNS 提供商",
			"Credential type must be password or private_key":            "凭据类型必须为 password 或 private_key",
			"Server connectivity test executor is unavailable":           "服务器连通性测试执行器不可用",
			"Server name, host, and credentialId are required":           "服务器名称、主机和 credentialId 不能为空",
			"Server host and credentialId are required":                  "服务器主机和 credentialId 不能为空",
			"Server port must be between 1 and 65535":                    "服务器端口必须在 1 到 65535 之间",
			"Task type is required":                                      "任务类型不能为空",
			"Task step is required":                                      "任务步骤不能为空",
			"Range must be 1h, 6h, 1d, or 7d":                            "时间范围必须为 1h、6h、1d 或 7d",
			"Only queued, scheduled, or retryable tasks can be run now":  "只有排队、已调度或等待重试的任务可以立即运行",
			"Only failed, retryable, or blocked tasks can be retried":    "只有失败、等待重试或已阻塞的任务可以重试",
			"This task type cannot be retried from the task center":      "该任务类型不能从任务中心重试",
			"Server ID is required":                                      "必须提供服务器 ID",
			"Application image is empty":                                 "应用镜像不能为空",
			"Disable the application before deleting it":                 "删除前请先禁用应用",
			"enable the application before updating its image":           "更新镜像前请先启用应用",
			"deployment mode must be all or selected":                    "部署模式必须为 all 或 selected",
			"select at least one deployment server":                      "请至少选择一个部署服务器",
			"persistent applications must target exactly one server":     "持久化应用必须且只能部署到一个服务器",
			"reverse proxy target port must be between 1 and 65535":      "反向代理目标端口必须在 1 到 65535 之间",
			"reverse proxy path must start with /":                       "反向代理路径必须以 / 开头",
			"reverse proxy domain is invalid":                            "反向代理域名无效",
			"Encrypted private keys are not supported":                   "暂不支持导入带密码的私钥",
			"Public key does not match the private key":                  "公钥与私钥不匹配",
			"Certificate authority still has child certificates":         "该 CA 仍有子证书，无法删除",
			"Key asset is still used by an application or reverse proxy": "该密钥资产仍被应用或反向代理使用",
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
	if byCode := codeTranslations[normalized]; byCode != nil {
		if translated := byCode[code]; translated != "" {
			return translated
		}
	}
	if exact := exactTranslations[normalized][fallback]; exact != "" {
		return exact
	}
	if prefixTranslations := prefixCodeTranslations[normalized]; prefixTranslations != nil {
		bestPrefix := ""
		bestTranslation := ""
		for prefix, translated := range prefixTranslations {
			if strings.HasPrefix(fallback, prefix) && len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestTranslation = translated
			}
		}
		if bestPrefix != "" {
			return bestTranslation + strings.TrimPrefix(fallback, bestPrefix)
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
