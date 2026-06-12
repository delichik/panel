# 密钥与证书统一资产设计

## 背景与目标

现有“自签证书”页面只覆盖 CA 和由 CA 签发的 TLS 证书，无法表达不依赖 CA 的 SSH 密钥对，也仍将私钥长期保存在文件系统。该模型不足以承担统一资产管理、Panel 文件挂载和批量迁移。

本次改造将证书中心第三个二级菜单改为“密钥与证书”，建立统一资产模型，支持：

- 可重复使用的 CA 证书。
- 由 CA 签发的 TLS 证书。
- 不依赖 CA 的 SSH 密钥对。
- Panel 生成或导入资产。
- 数据库加密保存敏感材料。
- 单项下载以及密码保护的批量导入、导出。
- 统一的 Panel 文件挂载、引用检查、更新同步和删除保护。

Nomad 内置证书和 ACME 域名证书继续保留各自页面与生命周期，不迁入本模块。

## 导航与页面

证书一级菜单保留三个二级入口：

- 内置证书
- 域名证书
- 密钥与证书

原 `/certificates/self-signed` 路由重定向到 `/certificates/key-assets`。页面分为三个标签页或区域：

- CA 证书
- TLS 证书
- SSH 密钥对

页面支持单项创建、导入、下载、更新和删除，也支持多选批量导出及归档批量导入。

## 统一资产模型

新增 `key_assets` 表，稳定类型值全部使用蛇形命名：

- `ca_certificate`
- `tls_certificate`
- `ssh_key_pair`

公共字段：

- `id`
- `type`
- `name`
- `parent_asset_id`
- `algorithm`
- `key_size`
- `common_name`
- `dns_names_json`
- `ip_addresses_json`
- `fingerprint`
- `certificate_ciphertext`
- `private_key_ciphertext`
- `public_key`
- `metadata_json`
- `not_before`
- `not_after`
- `created_at`
- `updated_at`

约束：

- `tls_certificate.parent_asset_id` 必须指向 `ca_certificate`，但从归档单独导入且缺少 CA 时允许为空，并标记为不可重新签发的独立证书。
- `ssh_key_pair` 不允许设置 `parent_asset_id`、证书字段、Common Name 或 SAN。
- `ca_certificate` 和 `tls_certificate` 使用 X.509 指纹。
- `ssh_key_pair` 使用 OpenSSH SHA256 公钥指纹。
- 公钥和证书可明文存储；私钥必须以认证加密密文存储。
- 普通列表和详情 API 永不返回私钥明文或密文。

证书内容也统一写入数据库，避免数据库与文件路径产生双重事实来源。Panel 文件读取器从数据库读取资产，按需解密私钥并交给 Nomad template，不在 Panel 数据目录长期落地明文文件。

## 数据库加密

当前仓库没有可复用的数据库加密实现：凭据密码仍以明文列保存，SSH 私钥仍保存在文件中。因此本模块新增独立的 `internal/secretstore` 边界，不将 JWT secret 直接当作数据加密密钥。

### 主密钥

- 首次启动时生成 32 字节随机主密钥。
- 默认保存到 `<data_root>/secrets/key-assets-master.key`，目录权限 `0700`，文件权限 `0600`。
- 可通过环境变量提供 Base64 编码主密钥，用于容器和外部密钥管理场景。
- 环境变量与密钥文件同时存在时必须一致，否则启动失败。
- 数据库存在加密资产但主密钥缺失或错误时，Panel 启动失败并给出明确恢复提示，不能静默生成新密钥。
- 主密钥必须和数据库一起备份；普通 API、日志和任务记录不得暴露主密钥。

### 字段加密

- 使用 XChaCha20-Poly1305 认证加密。
- 每个敏感字段使用独立随机 nonce。
- 关联数据包含资产 ID、资产类型和字段类型，防止密文被复制到其他资产或字段后仍可解密。
- 密文使用版本化封装，记录格式版本、算法、nonce 和 ciphertext，便于后续轮换算法。
- 创建和更新时先完成解析、匹配校验和加密，再在数据库事务中写入。
- 解密只发生在受控下载、Panel 文件渲染、SSH 凭据解析和批量导出路径。
- 解密后的字节尽量缩短生命周期，不进入日志、错误详情或任务元数据。

本次只为统一资产引入 `secretstore`。现有凭据和 DNS token 的加密迁移另行设计，避免在本功能中扩大行为范围；但新边界必须可供后续模块复用。

## CA 与 TLS 证书

### CA 证书

- 支持 Panel 生成或导入已有 CA。
- 生成内容包括 CA 证书、私钥和公钥。
- 导入时验证证书具有 CA Basic Constraints，并验证证书、公钥、私钥匹配。
- 同一个 CA 可重复签发多个 TLS 证书。
- 仍有子证书、Panel 文件挂载或其他系统引用时禁止删除。

### TLS 证书

- 创建时必须选择已有 CA。
- 支持 DNS SAN 和 IP SAN，至少提供一项。
- 生成证书、私钥和公钥。
- 支持重新签发，重新签发后创建任务并自动同步引用应用和反向代理。
- 从归档导入且缺少原 CA 的证书可以正常挂载和下载，但不能重新签发。
- 被应用、反向代理或其他系统配置引用时禁止删除。

## SSH 密钥对

支持生成和导入，稳定算法值：

- `ed25519`，默认。
- `rsa`，允许 2048、3072、4096 位，默认 3072 位，不允许低于 2048 位。

生成结果使用兼容 OpenSSH 的公钥格式和标准私钥编码。

导入规则：

- 支持未加密的 OpenSSH、PKCS#8 及常见 PEM 私钥。
- 不支持 passphrase。
- 检测到加密私钥时返回稳定错误码 `key_asset_encrypted_private_key_unsupported`。
- 公钥可以省略；省略时从私钥推导。
- 提供公钥时必须验证与私钥匹配，否则拒绝导入。
- 导入后规范化保存公钥和指纹。

SSH 密钥对支持重新生成。该操作会改变公钥，必须显示危险确认，创建任务，并在完成后同步引用该资产的应用。被引用时不能删除，但允许用户确认后重新生成。

## Panel 文件

Panel 文件目录统一暴露稳定资产引用，不暴露数据库字段或临时路径。

文件种类：

- CA/TLS：`certificate`、`private_key`、`public_key`
- SSH：`private_key`、`ssh_public_key`

引用格式继续使用稳定资源 ID，并从原 `certificate:<id>:<kind>` 兼容迁移到 `key_asset:<id>:<kind>`。旧自签资产引用在读取时兼容，在应用保存或重新部署时规范化为新格式。

私钥文件只在服务端渲染阶段解密，通过 Nomad template 下发。列表 API 只返回可选文件元数据。

资产内容更新后，使用相同 `operation_id` 同步所有引用应用；TLS 证书同时同步反向代理。同步失败记录具体引用方和任务阶段，不回滚已成功生成或导入的资产。

## 单项下载

- 证书和公钥可以直接下载。
- 私钥下载需要危险确认并记录审计任务或操作日志。
- 响应必须使用附件下载，不在 JSON 中返回私钥。
- 下载响应禁止缓存，并设置严格内容类型和文件名。
- 私钥下载权限沿用当前管理员权限模型；未来引入细粒度权限时必须单独控制。

## 批量导出

用户多选资产后输入导出密码与确认密码。密码不保存，最低长度为 12 个字符。

归档格式使用版本化扩展名 `.panel-key-assets`：

- 明文头只包含格式版本、KDF 参数、salt、nonce 和必要的算法标识。
- 资产清单及全部证书、公钥、私钥位于加密载荷内。
- 使用 Argon2id 从用户密码派生 32 字节密钥。
- 使用 XChaCha20-Poly1305 对整个载荷进行认证加密。
- 加密载荷使用规范 JSON 或 tar 结构，必须有确定的版本和字段约束。
- CA 与其 TLS 子证书同时导出时保留父子关系。
- 导出任务不得把密码、私钥或完整归档内容写入日志。

导出密码与数据库主密钥相互独立，因此归档可以跨 Panel 实例迁移。

## 批量导入

导入分为预检和执行两个阶段。

### 预检

上传归档并输入密码后：

- 校验格式版本、KDF 参数上限、归档认证标签和载荷结构。
- 解析并验证每个资产的证书、私钥、公钥、算法和父子关系。
- 检测资产 ID 和名称冲突。
- 显示资产数量、类型、冲突项、缺失 CA 的独立证书以及覆盖后会受影响的引用。
- 返回短期有效的导入计划 ID；服务端保存临时加密载荷或不可伪造的计划摘要，临时文件放在 `tmp` 下并按时清理。

### 冲突策略

- `skip_existing`：跳过已有 ID 或名称冲突。
- `generate_new_id`：为冲突资产生成新 ID，并重写同一归档内的父子引用。
- `overwrite_existing`：覆盖相同 ID 的资产。

名称冲突但 ID 不同的资产在 `overwrite_existing` 下不能自动覆盖，必须明确选择目标或改用新 ID，避免误覆盖。

### 执行

- 执行请求携带预检计划 ID、冲突策略和确认标志。
- 覆盖正在使用的资产时必须危险确认。
- 所有资产先完成验证和数据库加密，再在单个事务中写入；任意资产失败则整批回滚。
- 覆盖或新增完成后，创建同一个 `operation_id` 下的依赖同步任务。
- 覆盖失败时保留原有资产密文和引用状态。
- 导入完成后立即使预检计划失效并清理临时数据。

## API

统一 API：

- `GET /api/v1/key-assets`
- `GET /api/v1/key-assets/{id}`
- `POST /api/v1/key-assets/ca`
- `POST /api/v1/key-assets/tls`
- `POST /api/v1/key-assets/ssh/generate`
- `POST /api/v1/key-assets/import`
- `POST /api/v1/key-assets/{id}/reissue`
- `POST /api/v1/key-assets/{id}/regenerate`
- `GET /api/v1/key-assets/{id}/files/{kind}`
- `DELETE /api/v1/key-assets/{id}`
- `POST /api/v1/key-assets/exports`
- `GET /api/v1/key-assets/exports/{taskId}/download`
- `POST /api/v1/key-assets/imports/preflight`
- `POST /api/v1/key-assets/imports/{planId}/execute`

批量导出使用任务生成短期下载结果。导入预检和执行分离，避免仅凭前端确认文本执行不可逆覆盖。

旧 `/api/v1/self-signed-*` API 在 alpha 迁移期保留兼容层，内部转发到新服务；新前端和文档只使用 `key-assets` API。

## 数据迁移

新增迁移必须兼容旧数据库：

1. 创建 `key_assets` 表和必要索引。
2. 读取 `self_signed_certificates` 的证书、私钥和公钥文件。
3. 校验文件内容及父子关系。
4. 使用资产主密钥加密私钥，并保留原 ID、名称、指纹和有效期写入 `key_assets`。
5. 迁移事务成功后才清理旧明文文件；清理失败记录警告并允许下次启动重试。
6. 迁移中任意文件缺失或密钥不匹配时停止启动，明确指出资产 ID，不创建半迁移记录。
7. 旧表暂时保留用于兼容和回滚窗口，但迁移完成标记后不再作为写入源。

现有应用 `panel_file` 引用保持可读，重新保存或重新部署时转换为新引用格式。

## 错误与安全边界

稳定错误码至少覆盖：

- `key_asset_master_key_missing`
- `key_asset_master_key_invalid`
- `key_asset_type_invalid`
- `key_asset_encrypted_private_key_unsupported`
- `key_asset_key_pair_mismatch`
- `key_asset_ca_required`
- `key_asset_ca_invalid`
- `key_asset_in_use`
- `key_asset_ca_has_children`
- `key_asset_archive_password_invalid`
- `key_asset_archive_tampered`
- `key_asset_archive_version_unsupported`
- `key_asset_archive_kdf_invalid`
- `key_asset_import_conflict`
- `key_asset_import_plan_expired`
- `key_asset_import_confirmation_required`

限制：

- 归档上传设置大小、资产数量和 KDF 参数上限，防止资源耗尽。
- 临时归档和导出结果设置短期有效期并由后台清理。
- 私钥、密码、主密钥、解密失败的原始内容不得进入日志。
- 覆盖和重新生成必须在同步完成前保持任务状态可见。

## 前端交互

- 页面使用三个标签区展示资产，并提供类型、算法、指纹、有效期和引用状态。
- 创建 SSH 密钥默认选择 Ed25519；选择 RSA 后显示位数选项。
- 导入 SSH 私钥时明确提示不支持 passphrase。
- 批量导出必须输入两次密码，并显示所选资产和包含的子资产。
- 批量导入先显示预检结果，再选择冲突策略。
- 覆盖使用中资产和重新生成 SSH 密钥使用危险确认对话框。
- 私钥不提供页面内明文预览。
- 所有用户可见文案进入中英文 i18n。

## 任务类型

新增任务类型：

- `key_asset_tls_reissue`
- `key_asset_ssh_regenerate`
- `key_asset_export`
- `key_asset_import`
- `key_asset_sync`

同一更新或导入操作及其依赖同步共享 `operation_id`。

## 验证范围

后端测试覆盖：

- 主密钥生成、加载、错误密钥和密文篡改。
- CA/TLS 创建、导入、父子关系和重新签发。
- Ed25519、RSA 生成和 SSH 导入格式。
- 加密私钥拒绝、公私钥匹配和指纹。
- 旧自签数据迁移、失败回滚和明文文件清理。
- Panel 文件读取、引用保护和更新同步。
- 归档加解密、错误密码、篡改、版本、KDF 上限。
- 三种冲突策略、父子 ID 重写、事务回滚和使用中覆盖确认。

前端测试覆盖：

- 导航和旧路由重定向。
- 三个资产标签及创建、导入表单。
- SSH 算法和 RSA 位数选择。
- 批量选择、密码确认、预检结果和冲突策略。
- 危险确认、任务入口和错误展示。

最终按仓库规则执行：

- `task test:backend`
- `task test:web`
- `task build:backend`
- `task build:web`

并同步更新证书、应用、任务、前端及国际化模块文档。
