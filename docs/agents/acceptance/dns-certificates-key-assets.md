# DNS、证书与密钥资产验收规范

本文约束 Cloudflare DNS、ACME 域名证书、自签证书、统一密钥资产和系统证书。私钥材料只允许在受控导入、下载、导出或内部文件读取的瞬时边界出现。

## 1. DNS 域名

- `DNS-DOM-001`：认证用户调用 `GET /api/v1/dns/domains` 时，只接受 `page`、`pageSize`、`q`，按名称和 ID 稳定分页；列表只查本地摘要且只返回 ID、域名、provider和时间，不得解密凭据或访问 provider。
- `DNS-DOM-002`：域名创建/编辑必须小写、裁剪并去末尾点，拒绝非法 DNS名；provider缺省可归一为cloudflare，但任何非cloudflare值返回 `dns_provider_invalid`。
- `DNS-DOM-003`：创建必须提供 API token；编辑 token留空必须沿用旧token，非空才替换；旧客户端 `accountId` 不得进入持久化模型或影响请求。
- `DNS-DOM-004`：创建或编辑写库前必须以最终域名和最终token访问Cloudflare验证Zone读取及记录权限；验证失败不得创建记录、改名或覆盖旧密文。
- `DNS-DOM-005`：provider配置非秘密保存到 `provider_config_json`，token JSON必须通过secret store加密到 `provider_secret_ciphertext`；列表、详情、错误和日志均不得回显token。
- `DNS-DOM-006`：删除未被证书引用的域名必须删除本地域和级联快照；存在任一证书引用时返回409 `dns_domain_in_use`，不得删除provider记录或生命周期数据。
- `DNS-DOM-007`：旧明文token迁移必须在主密钥加载后加密并事务重建表以移除旧厂商列；仅有legacy account_id且无token来源时可保留空凭据完成启动，但首次实际使用必须明确报凭据缺失。

## 2. DNS 记录快照与写操作

- `DNS-REC-001`：`GET /dns/domains/{id}/records` 必须只读 `dns_record_snapshots`，不得同步访问Cloudflare；首次无快照返回空items、`stale=true`且无虚构observedAt。
- `DNS-REC-002`：有效快照必须返回 items、observedAt、stale、refreshing、可选 refreshTaskId和lastRefreshError；损坏时间戳可保留items但必须标记stale。
- `DNS-REC-003`：`POST .../records/refresh` 必须返回202和taskId，创建或复用 `dns_records_refresh` 并异步执行；刷新成功原子替换整份快照、更新时间且清空旧错误，失败保留旧items并记录可诊断错误。
- `DNS-REC-004`：记录输入只允许 `A|AAAA|CNAME|TXT|MX|SRV|CAA|NS`，名称合法、value非空、TTL不小于0；proxied=true只允许A/AAAA/CNAME，失败不得访问provider。
- `DNS-REC-005`：记录名接受 `@`、相对名和zone内完整名，发送provider前必须规范化为同一主机语义；禁止把zone外名称静默改成zone内记录。
- `DNS-REC-006`：创建前尽力读取现有全分页记录；同一主机存在同类型冲突时创建语义升级为更新既有记录，跨CNAME与A/AAAA冲突返回稳定409；预检读取失败可继续真实创建并透传provider结果。
- `DNS-REC-007`：更新时排除自身ID再做同主机冲突检查；空recordId返回 `dns_record_id_required`；provider成功后尽力刷新快照，快照刷新失败不得把已成功远端写入报告为未执行。
- `DNS-REC-008`：删除只删除指定provider记录，成功后尽力重建快照；空ID拒绝，provider失败时不得本地假删。
- `DNS-REC-009`：Cloudflare列表必须按 `result_info.total_pages` 拉取全部分页，默认HTTP client超时30秒；官方错误envelope优先提供错误码/消息，81054统一映射为友好CNAME冲突。

## 3. 入口代理 DNS 同步

- `DNS-SYNC-001`：同步必须按zone独立收敛A/AAAA期望记录，仅管理 `comment=panel:reverse-proxy` 的记录；用户自建记录永不改写或删除。
- `DNS-SYNC-002`：自动记录从服务器IPv4/IPv6生成，TTL固定120、proxied=false；某zone遇到用户CNAME冲突时只标记该zone失败，不删除冲突记录且不阻断其他zone。
- `DNS-SYNC-003`：同步失败不得回滚入口代理或服务器配置；每个域结果必须保存到DNS同步状态并可通过 `dns_proxy_records_sync` 重试。
- `DNS-SYNC-004`：同一zone再次同步相同期望集合必须幂等，不得重复创建；移除的托管目标只能删除Panel标记记录。

## 4. ACME 域名证书

- `CRT-ACME-001`：`GET /api/v1/certificates` 必须返回分页摘要，按名称或域名搜索；摘要不得包含私钥、资产metadata、内部变量名或遗留文件路径，完整详情由 `GET /certificates/{id}`承担。
- `CRT-ACME-002`：签发请求必须引用存在且可解密凭据的DNS域；名称空时使用主域名，prefixes去重并规范化，空/空数组归一为根域。
- `CRT-ACME-003`：`@`生成根域，普通前缀拼接托管域，`*`或`*.name`仅生成对应层级通配SAN；任何最终SAN不合法返回 `certificate_domain_invalid` 且不得建生命周期记录。
- `CRT-ACME-004`：兼容旧scope时仅接受 `single|wildcard`；显式prefixes使用 `prefixes` scope；非法scope返回 `certificate_scope_invalid`。
- `CRT-ACME-005`：签发必须先持久化pending生命周期记录和任务，再返回certificate与taskId，并立即交给task manager；周期worker只负责恢复/兜底，不能成为正常启动延迟。
- `CRT-ACME-006`：执行开始将首次签发置issuing；成功必须验证完整证书/私钥PEM、原子upsert ACME key asset、写assetId/有效期/nextRenewAt/status=issued并清lastError。
- `CRT-ACME-007`：首次签发失败置status=failed并保存lastError；重新签发失败只更新lastError且保留上一份issued材料与可用状态，不得先破坏旧证书。
- `CRT-ACME-008`：ACME使用DNS-01；challenge创建后无论授权、finalize、取消或超时都必须尝试清理。账户私钥跨签发复用，账户已存在时通过GetReg恢复而不是报失败。
- `CRT-ACME-009`：签发/重签/续签必须把账号、订单、challenge、等待、清理和finalize阶段写入任务步骤/metadata，日志不得包含token、账户私钥或证书私钥。
- `CRT-ACME-010`：手动renew必须有存在的证书并创建 `certificate_renew`；30分钟周期仅为autoRenew、nextRenewAt已到的证书创建共享operation批次，同一证书重复执行需由任务去重保护。
- `CRT-ACME-011`：续签成功才替换ACME资产并更新生命周期；任何失败保留旧材料并写lastError。随后普通应用刷新与反向代理证书协调必须分别尝试，一个失败不能阻止另一个尝试。
- `CRT-ACME-012`：仅issued证书进入内部文件目录，source稳定为 `certificate:<id>:certificate|private_key`；未签发、失败或不存在证书不可读取。
- `CRT-ACME-013`：删除前必须检查应用文件和反向代理引用；在用返回409 `certificate_in_use`。未引用时先删除其ACME资产再删生命周期记录，普通key-assets API不可代管该资产。
- `CRT-ACME-014`：旧certificate/private_key path只用于启动迁移；迁移成功后材料进入key_assets并清理旧文件，新签发不得再写路径。

## 5. 自签 CA/TLS

- `CRT-SELF-001`：`GET /self-signed-certificates` 只分页返回用户CA/TLS摘要，排除systemManaged、agent_tls、panel_tls和ACME；列表不得含私钥、metadata、文件路径和引用详情。
- `CRT-SELF-002`：创建CA必须要求名称、commonName、有效期和受支持算法/位数；成功生成可签发CA证书与加密私钥，失败不得留下孤立行。
- `CRT-SELF-003`：创建leaf必须引用用户CA、至少提供一个有效DNS/IP身份并有正有效期；证书必须由该CA签名，父子关系、指纹和有效期一致持久化。
- `CRT-SELF-004`：续期只允许有父CA的leaf，保留身份与相对有效期语义并生成新key/cert；成功后刷新引用应用，失败保留旧材料。
- `CRT-SELF-005`：删除沿用key asset安全规则：有子证书的CA返回 `key_asset_ca_has_children`，存在应用/代理引用返回 `key_asset_in_use`，系统或ACME资产不能从该工作流删除。

## 6. 统一密钥资产列表与创建

- `KAS-LIST-001`：`GET /key-assets` 返回稳定分页摘要，搜索仅匹配名称、commonName或fingerprint；摘要不查私钥密文、证书密文、metadata、引用详情，并将数组字段规范为非nil空数组。
- `KAS-LIST-002`：`GET /key-assets/{id}` 才可返回非秘密metadata、引用、childCount、可操作性和文件kind；任何响应不得包含私钥密文或明文。
- `KAS-LIST-003`：普通列表/详情/下载/导出/删除对ACME资产统一返回不存在；systemManaged资产从普通列表排除，变更操作返回 `key_asset_system_managed`或专用接口语义。
- `KAS-LIST-004`：`GET /key-assets/tls` 返回所有证书/私钥完整且通过Panel监听兼容性验证的TLS资产，包括用户、ACME和内置panel-tls，排除agent_tls；不传域名、不按SAN筛选，非法资产静默不列入。
- `KAS-CRT-001`：创建CA和TLS必须验证名称、commonName、算法与keySize；RSA仅2048/3072/4096，ECDSA仅256，Ed25519按无位数语义；私钥始终加密保存。
- `KAS-CRT-002`：TLS父资产必须存在且为CA，至少一个DNS/IP身份，私钥与证书匹配且由父CA签名；失败不得写入半资产。
- `KAS-SSH-001`：生成SSH key pair必须有名称并使用受支持算法/位数；保存authorized public key、fingerprint和加密私钥，comment只进入公钥展示材料。
- `KAS-IMP-001`：单资产导入只接受CA/TLS/SSH类型；拒绝无效PEM、加密私钥、私钥公钥不匹配、非CA的CA输入、TLS父CA不匹配和非法SSH公钥。
- `KAS-IMP-002`：导入system保留ID/metadata或 `origin=acme` 必须拒绝；不得允许归档覆盖系统或ACME管理边界。

## 7. 文件下载、重签与删除

- `KAS-FILE-001`：下载只允许资产类型声明的kind；证书、公钥用0644语义，私钥用0600语义；私钥HTTP响应必须禁用缓存并使用安全basename文件名。
- `KAS-FILE-002`：统一下载分发必须正确区分 `/key-assets/{id}/files/{kind}` 与 `/key-assets/exports/{taskId}/download`；未知三段路径返回JSON/HTTP 404，不得触发ServeMux冲突或HTML回退。
- `KAS-MUT-001`：TLS reissue只允许有父CA的用户TLS；SSH regenerate只允许用户SSH key；同一资产已有操作时返回409 `key_asset_operation_in_progress`。
- `KAS-MUT-002`：reissue/regenerate必须先生成并验证新材料，再原子替换并刷新引用应用；失败保留旧材料，任务不得标completed。
- `KAS-DEL-001`：删除CA前无子项、删除任意资产前无引用；失败返回稳定409且保留资产。删除不存在返回404；成功后内部文件目录不可再解析。

## 8. 批量导出与导入

- `KAS-EXP-001`：导出必须要求至少12字符密码；assetIds裁剪去重，任一ID不存在、为ACME或systemManaged时整体失败，不生成可下载归档。
- `KAS-EXP-002`：归档必须认证加密并包含所选资产及父关系/材料；文件保存于受控export目录、0600权限，LogDB记录taskId、文件、30分钟expiresAt，任务完成后才可下载。
- `KAS-EXP-003`：下载必须验证任务类型和completed状态、记录未过期、路径仍在export目录且文件存在；任一失败统一不存在，过期时删除记录和文件。
- `KAS-IMP-003`：preflight multipart必须有归档和密码；解密前验证格式、KDF、salt/nonce/ciphertext和认证标签，密码错误或篡改不得产生plan。
- `KAS-IMP-004`：preflight必须解析全部资产、父关系、ID/名称冲突、standalone TLS与在用覆盖风险，生成30分钟内存plan和0600最小元数据文件；不得把解密材料返回前端。
- `KAS-IMP-005`：execute仅接受有效未过期plan和 `skip_existing|generate_new_id|overwrite_existing`；名称指向不同ID的冲突不可自动覆盖，在用覆盖必须有显式危险确认。
- `KAS-IMP-006`：generate_new_id必须重映射冲突ID及包内父子关系并生成不冲突名称；skip不得破坏其他待导入依赖；overwrite不得覆盖system/ACME资产。
- `KAS-IMP-007`：导入执行必须以任务记录结果，成功后消费plan并刷新引用方；同一已消费/过期plan重放不得重复导入。

## 9. 系统资产、主密钥与验收证据

- `KAS-SYS-001`：首次无加密数据且无主密钥时，必须生成base64 32字节主密钥到 `<dataRoot>/secrets/key-assets-master.key`，目录0700、文件0600；环境变量优先。
- `KAS-SYS-002`：环境与文件主密钥同时存在但不一致、格式错误，或数据库已有任一key asset/DNS/SSH密文而密钥缺失时，Panel必须拒绝启动，不得生成新key覆盖。
- `KAS-SYS-003`：密文必须使用XChaCha20-Poly1305且把asset ID、类型、字段作为associated data；把密文复制到另一资产/字段必须解密失败。
- `KAS-SYS-004`：内置Agent CA/client/节点证书与Panel CA/TLS必须带systemManaged和明确systemScope；普通工作流不可变更，只能由 `GET /key-assets/system` 查看并由 `POST .../{id}/reset`创建任务。
- `KAS-SYS-005`：Panel CA/TLS为独立RSA-2048链，不复用Ed25519 Agent CA；缺失或失效时按各自scope重建，重置级联行为必须符合服务器合同。
- `DNS-EVD-001`：测试必须覆盖列表不触网/不解密、域保存前provider验证、记录快照纯本地、同类型upsert、CNAME冲突、跨zone隔离和用户记录保护。
- `CRT-EVD-001`：测试必须覆盖签发/重签/续签成功与失败保旧材料、challenge清理、账户复用、删除引用保护和自动续签输入收集。
- `KAS-EVD-001`：测试必须覆盖普通/ACME/system可见性矩阵、主密钥fail-closed、私钥不泄漏、文件kind/cache、归档篡改/过期/路径逃逸、导入冲突策略及同资产互斥。
- `KAS-EVD-002`：路由清单与前端typed client必须同时覆盖DNS、certificates、self-signed、key-assets、system certificates和两类下载路径；新增能力不得用Mock成功替代真实API。
