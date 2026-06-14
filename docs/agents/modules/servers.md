# 鏈嶅姟鍣ㄣ€佸嚟鎹€佹寚鏍囦笌杞欢鍖?
## 閫傜敤鍦烘櫙

淇敼 SSH 鍑嵁銆佹湇鍔″櫒鐧昏銆佽繛閫氭€ф祴璇曘€佺郴缁熸帰娴嬨€乻udo 妫€鏌ャ€乁FW 瀹夎銆佹瑙堟寚鏍囬噰闆嗐€丄PT 杞欢鍖呭埛鏂版垨鍗囩骇鏃讹紝鍏堣鏈枃妗ｃ€?
## 鍚庣鍏ュ彛

- SSH 鍑嵁锛歚internal/credential/`
- 鏈嶅姟鍣細`internal/server/`
- SSH 鎵ц鍣細`internal/sshx/`
- Linux 鍙戣鐗堥€傞厤锛歚internal/linux/`
- 閫氱敤杩滅▼杩愮淮鎿嶄綔锛歚internal/remoteops/`
- 鎸囨爣閲囬泦锛歚internal/metrics/`
- 姒傝鑱氬悎锛歚internal/overview/`
- 杞欢鍖呯淮鎶わ細`internal/packages/`
- 璋冨害瑙﹀彂锛歚internal/scheduler/`
- 浠诲姟璁板綍锛歚internal/tasks/`
- 璺敱瑁呴厤锛歚internal/app/app.go`

## 鍓嶇鍏ュ彛

- 鏈嶅姟鍣ㄤ笌鍑嵁椤甸潰锛歚web/src/views/servers/_shared/ServersPageContent.vue`
- 鏈嶅姟鍣ㄩ€夋嫨鍣細`web/src/components/ServerSelector.vue`
- 杞欢鍖呴〉闈細`web/src/views/servers/packages/index.vue`
- 闃茬伀澧欓〉闈細`web/src/views/servers/firewall/index.vue`
- 姒傝椤甸潰锛歚web/src/views/overview/index.vue`
- API锛歚web/src/api/servers.ts`銆乣web/src/api/packages.ts`銆乣web/src/api/overview.ts`
- 绫诲瀷锛歚web/src/types/api.ts`

## API 鑼冨洿

- 鍑嵁锛歚GET/POST /api/v1/credentials`锛宍PUT/DELETE /api/v1/credentials/{id}`
- 鏈嶅姟鍣細`GET/POST /api/v1/servers`锛宍POST /api/v1/servers/probe`锛宍PUT/DELETE /api/v1/servers/{id}`锛涙柊澧炴湇鍔″櫒鍝嶅簲鍙惡甯?`initialTaskId` 鎸囧悜棣栬繛淇℃伅閲囬泦浠诲姟銆?- 鏈嶅姟鍣ㄦ搷浣滐細`POST /api/v1/servers/{id}/test`锛宍POST /api/v1/servers/{id}/restart`锛宍POST /api/v1/servers/{id}/ufw/install`
- UFW 闃茬伀澧欙細`GET /api/v1/servers/{id}/ufw`锛宍POST /api/v1/servers/{id}/ufw/enable`锛宍POST /api/v1/servers/{id}/ufw/rules`锛宍DELETE /api/v1/servers/{id}/ufw/rules/{number}`
- 鎸囨爣锛歚GET /api/v1/servers/{id}/metrics`
- 杞欢鍖咃細`GET /api/v1/servers/{id}/packages/updates`锛宍POST /api/v1/servers/{id}/packages/refresh`锛宍POST /api/v1/servers/{id}/packages/upgrade-selected`锛宍POST /api/v1/servers/{id}/packages/upgrade-all`
- 姒傝锛歚GET /api/v1/overview`
- 概览卡片布局：`GET/PUT /api/v1/overview/cards`
- 概览卡片数据：`GET /api/v1/overview/cards/{cardId}/data`，后端按卡片配置展开服务器范围并返回该卡片所有服务器的指标序列。

## 鏁版嵁涓庤涓虹害瀹?
- 概览卡片布局以系统级有序 JSON 保存在应用数据库的 `overview_card_configurations` 表；数据库没有记录时首次读取会写入默认六张卡片。空卡片数组是有效布局，浏览器 localStorage 中的旧布局不会迁移。
- `servers` 鍜?`credentials` 鍦ㄥ簲鐢ㄦ暟鎹簱锛屾寚鏍囧揩鐓у湪鎸囨爣鏁版嵁搴撱€?- 绯荤粺鎺㈡祴閫氳繃 SSH 鎵ц杩滅▼鍛戒护锛屽苟浜ょ粰 `internal/linux/` 瑙ｆ瀽鏀寔鐨?Debian/Ubuntu 鐗堟湰銆?- 绯荤粺鎺㈡祴鍐欏叆 `sys.*` traits锛屽綋鍓嶅寘鎷?CPU 鏍告暟銆佸唴瀛樸€佺鐩樸€佷富鏈哄悕銆佹灦鏋勩€丆PU 鍨嬪彿銆佺墿鐞?鐩撮€氱綉鍗″湴鍧€鎽樿銆佸彂琛岀増鍜?UFW 鏀寔/瀹夎/鍚敤鐘舵€併€傜綉鍗￠噰闆嗚姹?`/sys/class/net/{name}/device` 瀛樺湪锛屽苟杩囨护 Docker銆乿eth銆乥ridge銆丆NI銆侀毀閬撳拰 overlay 绛夊父瑙佽櫄鎷熸帴鍙ｃ€?- 瀹夎杞欢銆佹棩蹇楀寲 sudo 鍛戒护銆乻udo 鍐欐枃浠跺拰 UFW allow/delete/status 杩欑被鍩虹杩滅▼鎿嶄綔搴斾紭鍏堝鐢?`internal/remoteops/`锛岄伩鍏嶅湪涓氬姟妯″潡閲屾暎钀介暱鑴氭湰銆?- 鍓嶇鐧昏鎴栨祴璇曟湇鍔″櫒鍓嶅繀椤婚€夋嫨宸叉湁 SSH 鍑嵁锛涙病鏈夊嚟鎹椂搴斿紩瀵煎厛鍒涘缓鍑嵁锛屼笉鑳芥彁浜ょ┖ `credentialId`銆?- 缁存姢鎿嶄綔閫氬父瑕佹眰 root 鎴栧厤瀵?sudo锛涚浉鍏虫鏌ョ粨鏋滃啓鍥炴湇鍔″櫒璁板綍銆?- 杞欢鍖呯淮鎶ゅ熀浜?APT锛屽彧瀵规敮鎸佺殑绯荤粺鎵ц锛涘埛鏂板拰鍗囩骇閮戒緷璧栬繙绋?sudo锛屽墠绔細鍦ㄥ彂琛岀増鎴栧厤瀵?sudo 鏈‘璁ゆ椂闃绘柇鎵嬪姩缁存姢鎿嶄綔銆?- `POST /api/v1/servers/{id}/packages/refresh` 浼氬垱寤烘垨澶嶇敤 `package_refresh` 浠诲姟骞惰繑鍥?`taskId`锛涜皟搴﹀櫒鎸変竴杞墍鏈夋湇鍔″櫒鍒锋柊鏃讹紝鍚屼竴杞垱寤虹殑澶氫釜 `package_refresh` 浠诲姟蹇呴』鍏变韩涓€涓?`operationId`锛涘埛鏂板け璐ュ繀椤昏惤鍒颁换鍔￠敊璇拰鏃ュ織閲岋紝涓嶈兘鍙啓鍚庡彴鏃ュ織銆?- 鍛ㄦ湡鎬ф寚鏍囬噰闆嗕細鍒涘缓 `metrics_collect` 浠诲姟璁板綍锛涘悓涓€杞鍙版湇鍔″櫒閲囬泦鍏变韩涓€涓?`operationId`銆備换鍔′腑蹇冮粯璁も€滃父鐢ㄧ被鍨嬧€濅細闅愯棌璇ラ珮棰戠被鍨嬶紝鍒囧埌鈥滄墍鏈夌被鍨嬧€濇垨绮剧‘閫夋嫨 `metrics_collect` 鏃跺彲鏌ョ湅銆?- `POST /api/v1/servers/{id}/ufw/install` 杩斿洖 `taskId`锛涘墠绔惎鍔ㄥ悗蹇呴』淇濈暀浠诲姟涓績鍏ュ彛锛岄伩鍏嶇敤鎴锋棤娉曡拷韪繙绋嬪畨瑁呰繘搴︺€俇FW 瀹夎浠诲姟鐢卞唴瀛?goroutine 鎵ц锛屽垱寤哄悗蹇呴』鍏堟爣璁颁负 `running` 鍐嶈繑鍥烇紝閬楃暀鏃?`queued` 鐢变换鍔℃竻鐞嗗厹搴曟爣璁板け璐ャ€?- `POST /api/v1/servers/{id}/restart` 瑕佹眰鏈嶅姟鍣ㄥ彲杈句笖宸茬‘璁ゅ厤瀵?sudo锛岃繑鍥?`server_restart` 浠诲姟鐨?`taskId`锛涘墠绔繀椤讳簩娆＄‘璁ゅ苟淇濈暀浠诲姟涓績鍏ュ彛銆傝繙绋嬪懡浠ゅ厛鍚庡彴寤惰繜鍐嶈皟鐢?`systemctl reboot` 鎴?`shutdown -r now`锛岄伩鍏?SSH 涓诲姩鏂紑琚鍒や负閲嶅惎澶辫触銆?- UFW 绠＄悊椤甸潰鍙敮鎸?UFW锛氱姸鎬佹煡璇€佹坊鍔?allow 瑙勫垯鍜屾寜缂栧彿鍒犻櫎瑙勫垯閫氳繃杩滅▼ sudo 鍚屾鎵ц銆傚惎鐢ㄦ搷浣滆繑鍥?`server_ufw_enable` 浠诲姟锛涙湭瀹夎鏃跺厛瀹夎锛岄殢鍚庢斁琛屾湇鍔″櫒褰撳墠 SSH 绔彛骞舵墽琛?`ufw --force enable`锛岄〉闈㈤渶瑕佷簩娆＄‘璁ゅ苟淇濈暀浠诲姟涓績鍏ュ彛锛涚鐢?UFW 鏆備笉鐢遍〉闈㈡彁渚涖€?- 鏂板鏈嶅姟鍣ㄦ椂鍙垱寤轰竴涓彲瑙佺殑 `server_info_collect` 棣栬繛淇℃伅閲囬泦浠诲姟锛屽苟鍦ㄥ垱寤哄搷搴旇繑鍥?`initialTaskId` 渚涘墠绔睍绀轰换鍔″叆鍙ｏ紱璇ヤ换鍔￠杩炲け璐ユ椂蹇呴』鏍囪澶辫触骞跺垹闄ゅ垰鍒涘缓鐨勬湇鍔″櫒璁板綍锛岃鐢ㄦ埛鍥炲埌鍒涘缓琛ㄥ崟閲嶆柊璋冩暣 SSH 淇℃伅銆傚悗缁紪杈戙€佹墜鍔ㄦ祴璇曞拰闄堟棫鍒锋柊澶嶇敤鍐呴儴 `server_connectivity_test` 杩為€氭€т换鍔★紝榛樿涓嶅湪浠诲姟涓績灞曠ず锛涗竴娆℃湇鍔″櫒鍒楄〃瑙﹀彂鐨勫鍙伴檲鏃ф湇鍔″櫒鍒锋柊搴斿叡浜竴涓?`operationId`銆?- 闀胯€楁椂鎿嶄綔搴旇褰曚负浠诲姟锛屾棩蹇楀拰姝ラ浜ょ粰 `internal/tasks/`銆?- 姒傝鎸囨爣鍗＄墖鍦ㄧ獎灏哄涓嬩細鑷姩闅愯棌閲嶅彔鐨勬椂闂磋酱鏍囩锛涘崟椤规寚鏍囨媺鍙栧け璐ユ椂涓嶅湪鍗＄墖鍐呭睍绀洪敊璇枃妗堬紝鍥捐〃娴獥鎸傝浇鍒伴〉闈㈠眰浠ラ伩鍏嶈鍗＄墖杈圭晫瑁佸壀銆?- 鏈嶅姟鍣ㄨ鎯呮寜缃戝崱鍒嗙粍灞曠ず鎺ュ彛鍚嶅強 IPv4/IPv6 鍦板潃锛屼笉鎶婃墍鏈夋帴鍙ｅ湴鍧€鎷煎湪鍚屼竴涓睘鎬у€间腑锛涜繛鎺ユ祴璇曠粨鏋滀娇鐢ㄧ揣鍑戠殑鍒嗛」缃戝崱鏍囩銆?- 鏈嶅姟鍣ㄣ€佽蒋浠跺寘鍜岄槻鐏椤甸潰鐨勫乏渚ф湇鍔″櫒閫夋嫨鍣ㄤ娇鐢ㄧ粺涓€鐨勭揣鍑戝钩闈㈠垪琛ㄩ鏍硷紝妗岄潰瀹藉害涓?`clamp(300px, 26vw, 340px)`锛涜蒋浠跺寘鍜岄槻鐏鍒囨崲鏈嶅姟鍣ㄦ椂娓呯┖涓婁竴鍙版湇鍔″櫒鐨勫紓姝ヨ鎯呭苟鏄剧ず鍔犺浇鐘舵€侊紝涓斿拷鐣ヨ繜鍒板搷搴斻€?
## 楠岃瘉

- 鍏堟寜妯″潡绱㈠紩鐨勨€滄鏌ュ拰娴嬭瘯鑼冨洿鈥濆垽鏂槸鍚﹂渶瑕侀獙璇併€?- 闇€瑕侀獙璇佸悗绔敼鍔ㄦ椂锛岃繍琛?`task test:backend`锛岄噸鐐瑰叧娉?`server`銆乣credential`銆乣linux`銆乣metrics`銆乣packages` 鐩稿叧娴嬭瘯銆?- 鍓嶇椤甸潰鎴?API 绫诲瀷鏀瑰姩鍙寜闇€瑕佽繍琛?`task test:web` 鎴?`task build:web`銆?
## 鏂囨。鏇存柊瑙﹀彂

鏂板鏀寔绯荤粺銆佽繙绋嬪懡浠ゃ€佹湇鍔″櫒瀛楁銆佸嚟鎹瓧娈点€佹寚鏍囧瓧娈点€佽蒋浠跺寘琛屼负鎴栫浉鍏?API 鏃讹紝蹇呴』鏇存柊鏈枃妗ｃ€?

## SSH 凭据安全存储

- SSH 密码、私钥和私钥口令统一封装到 `credentials.secret_ciphertext`，并通过 `internal/secretstore` 加密。
- 新建和更新凭据不得写入 `password_secret`、`passphrase_secret` 或 `dataRoot/keys` 私钥文件。
- 启动迁移会加密旧字段及旧私钥文件内容；密文验证成功后删除原私钥文件并清空旧字段。
- 凭据解析只从密文恢复秘密，并且仍不得通过 API 响应或任务日志返回秘密内容。

## Panel Agent 只读远程通道

- `cmd/panel-agent` 是部署在目标服务器上的被动 HTTPS agent，使用 Panel 专用 agent CA 做 mTLS 双向认证；Panel 启动时在 `dataRoot/agent/tls` 生成或复用 agent CA 与 Panel client 证书。
- 服务器通过 traits 启用 agent：`agent.enabled=true` 且 `agent.url=https://host:9443` 时，Panel 对已支持的读取路径必须调用 agent；agent 不可达、版本不兼容或调用失败时不回退 SSH，而是记录 `agent.status`、`agent.last_checked_at`、`agent.version` 和 `agent.last_error` traits，并让本次读取失败。
- Panel 启动后会检查已配置 agent 的服务器，要求 agent 至少为当前协议版本并具备健康检查、`/etc/os-release`、系统 traits、metrics snapshot 和 UFW status 能力；不满足时页面显示不兼容，并提示用户部署 agent。
- Agent 第一版只覆盖低风险读取类能力：健康检查、`/etc/os-release`、系统 traits、metrics snapshot、UFW status。metrics 采集、服务器信息刷新和 UFW 状态读取在启用 agent 后走 agent。
- 安装软件、软件包刷新/升级、UFW allow/delete/enable/install、服务器重启、Nomad 部署/重建/切换、应用部署等写入型或高风险操作继续走 SSH。
- `POST /api/v1/servers/{id}/agent/certificate` 会签发目标机 `panel-agent` 的 mTLS server 证书包；响应包含 CA、server certificate、server private key、建议监听地址和 agent URL，只用于安装配置，不落库。服务器详情页的“部署 Agent”会下载节点专属部署包，调用方不得把返回私钥写入任务日志或普通服务器字段。
