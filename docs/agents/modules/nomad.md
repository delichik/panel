# Nomad 妯″潡

## 閫傜敤鍦烘櫙

淇敼 Nomad API 瀹㈡埛绔€佽妭鐐规竻鍗曘€佹帶鍒跺钩闈€乻erver 寮曞銆乧lient 鍔犲叆銆佽妭鐐圭Щ闄ゃ€乀LS 璧勪骇銆丯omad 杩愯鏃惰缃垨鍙嶅悜浠ｇ悊鍚屾鏃讹紝鍏堣鏈枃妗ｃ€?
## 鍚庣鍏ュ彛

- Nomad API client锛歚internal/nomad/client.go`
- 绫诲瀷锛歚internal/nomad/types.go`
- 杩愯閰嶇疆锛歚internal/nomad/config.go`
- 鎺у埗骞抽潰鑱氬悎锛歚internal/nomad/control_plane.go`
- 鑺傜偣鍔犲叆鍜屽紩瀵硷細`internal/nomad/join_service.go`
- TLS 璧勪骇锛歚internal/nomad/tls_assets.go`
- 鑺傜偣鐗瑰緛锛歚internal/nomad/traits.go`
- Handler锛歚internal/nomad/handler.go`
- 璺敱瑁呴厤鍜岃法妯″潡杩炴帴锛歚internal/app/app.go`

## 鍓嶇鍏ュ彛

- Nomad 璁剧疆/鍔犲叆椤甸潰锛歚web/src/views/runtime/nomad/setup/index.vue`
- Nomad 鑺傜偣椤甸潰锛歚web/src/views/runtime/nomad/nodes/index.vue`
- API锛歚web/src/api/nomad.ts`
- 绫诲瀷锛歚web/src/types/api.ts`
- 璁剧疆椤?Nomad 鍒嗙被锛歚web/src/views/settings/_shared/SettingsPageContent.vue`

## API 鑼冨洿

- 娓呭崟锛歚GET /api/v1/nomad/status`銆乣/nodes`
- 鎺у埗骞抽潰锛歚GET /api/v1/nomad/control-plane`
- 鍔犲叆娴佺▼锛歚GET /api/v1/nomad/join-candidates`锛宍POST /api/v1/nomad/join`
- 寮曞銆侀噸閮ㄧ讲銆侀噸寤恒€佸垏鎹笌绉婚櫎锛歚POST /api/v1/nomad/bootstrap-server`锛宍/redeploy-node`锛宍/rebuild-cluster`锛宍/switch-server`锛宍/remove-node`锛岃繖浜涙帴鍙ｈ繑鍥?`taskId`
- 鍙嶅悜浠ｇ悊锛歚PUT /api/v1/nomad/reverse-proxy`锛岃繑鍥炴洿鏂板悗鐨?`server` 鍜?`taskId`
- 鍐呯疆 TLS 璇佷功鐢辫瘉涔︿腑蹇冭鍙栧拰杞崲锛歚GET /api/v1/certificates/builtin`銆乣POST /api/v1/certificates/builtin/rotate`

## 琛屼负绾﹀畾

- 控制平面投影已注册的 Panel 托管节点时，当前 Nomad HTTP 地址匹配的服务器必须显示为 `server`，`nomad_server_switch` 任务目标也投影为 `server`；其他带 `panel_server_id` 的已注册托管节点默认显示为 `client`，避免 server 切换后因历史任务类型显示为未知或未加入。server 切换同步 client 配置时，应排除新的控制平面 server，并把旧 server 作为 client 一并改写到新的 `server_join.retry_join`。
- Nomad 鍦板潃銆乶amespace銆乺egion銆乨atacenter 绛夎繍琛屾椂璁剧疆鏉ヨ嚜 `internal/settings/`銆?- SSH 绠＄悊鍦板潃涓?Nomad `advertise` 鍦板潃鏄袱涓嫭绔嬬綉缁滃钩闈紝Nomad `bind_addr` 蹇呴』淇濇寔 `0.0.0.0`銆傚紩瀵笺€佸姞鍏ャ€侀噸閮ㄧ讲銆侀噸寤烘垨鍒囨崲 server/client 鏃讹紝鍓嶇蹇呴』璁╃敤鎴锋樉寮忛€夋嫨 `advertiseAddress`锛涘€欓€夊湴鍧€鍖呮嫭宸蹭繚瀛?advertise銆佺洰鏍囨湇鍔″櫒鎺㈡祴鍒扮殑鐗╃悊缃戝崱 IP锛屼互鍙?SSH `host` 鏄湁鏁?IP 鏃剁殑 SSH host IP锛岀敤浜庨樋閲屼簯銆丱racle 绛夋湇鍔″櫒鍙兘鎺㈡祴鍒板唴缃戠綉鍗′絾闇€瑕佺敤鍏綉 IP advertise 鐨勫満鏅€?- 鍚庣灏嗙敤鎴烽€夋嫨淇濆瓨涓烘湇鍔″櫒 trait `nomad.advertise_address`锛屽苟缁х画鍐欏叆/璇诲彇 legacy `nomad.server_advertise_address` 浠ュ吋瀹规棫鏁版嵁锛涜鍦板潃鐢ㄤ簬 server/client 鐨?`advertise.http/rpc/serf`銆丳anel 鐨?Nomad HTTP 鍦板潃鍜?client 鐨?`server_join.retry_join`銆?- 宸插瓨鍦?Nomad 浠诲姟浣嗗綋鍓?server 娌℃湁 `nomad.advertise_address` 鎴?legacy `nomad.server_advertise_address` 鏃讹紝鎺у埗骞抽潰杩斿洖 `migration_required`锛屽墠绔繘鍏ョ被浼奸娆″垵濮嬪寲鐨勯噸寤哄紩瀵硷紝涓嶇户缁睍绀哄彲璇搷浣滅殑鏃ф帶鍒跺钩闈€?- 鏈湴鎴栧洖鐜?Nomad 鍦板潃浼氫娇鐢ㄩ」鐩墭绠＄殑 TLS 璧勪骇锛涚浉鍏冲垽鏂湪 `internal/app/app.go`銆?- 寮曞/鍔犲叆娴佺▼閫氳繃 SSH 鍦ㄧ洰鏍囨湇鍔″櫒鎵ц杩滅▼鍛戒护锛岄渶瑕佽€冭檻鏀寔绯荤粺銆乻udo銆佸箓绛夋€у拰澶辫触鎭㈠銆?- Panel 绠＄悊鐨?Nomad agent 蹇呴』鍦ㄦ湰鏈?UFW 鏀捐 `4646/tcp`锛圚TTP API锛夈€乣4647/tcp`锛圧PC锛変互鍙?`4648/tcp`銆乣4648/udp`锛圫erf gossip锛夛紱寮曞銆佸姞鍏ャ€侀噸閮ㄧ讲鍜?server 鍒囨崲鍚庣殑 client 鍚屾閮藉繀椤诲箓绛変慨澶嶈繖浜涜鍒欍€備簯鍘傚晢瀹夊叏缁勬垨澶栭儴闃茬伀澧欎笉鐢?Panel 绠＄悊锛屼粛闇€鍏佽鑺傜偣闂村搴旀祦閲忋€?- 寮曞/鍔犲叆鍚庣殑鏈湴鍋ュ悍妫€鏌ヤ娇鐢ㄥ甫鍗曟纭秴鏃剁殑 `nomad agent-info` 妫€鏌ユ湰鍦?agent API锛屼笉浣跨敤鍙兘绛夊緟闆嗙兢鍝嶅簲鐨?`nomad status`锛涙暣浣撴鏌ュ繀椤诲湪浠诲姟闃舵瓒呮椂鍐呯粨鏉熷苟杈撳嚭 systemd/journal 璇婃柇銆?- client 鍔犲叆鎴栭噸閮ㄧ讲涓嶈兘鍙互鏈湴 agent API 鍙敤浣滀负鎴愬姛鏉′欢锛涜繕蹇呴』閫氳繃 Panel 浣跨敤鐨?Nomad API 纭鍖归厤 `panel_server_id` 鐨勮妭鐐瑰凡缁忔敞鍐屼笖鐘舵€佷负 `ready`锛岃秴鏃跺垯浠诲姟澶辫触骞惰褰曟渶鍚庣殑鑺傜偣鐘舵€佹垨 API 閿欒銆?- 鐢熸垚鐨?server/client 閰嶇疆蹇呴』鏄惧紡鍐欏叆杩愯鏃?`region` 鍜?`datacenter`锛屽苟浣跨敤鐢ㄦ埛閫夋嫨鐨?advertise 鍦板潃鍐欏叆 `advertise.http/rpc/serf`锛沜lient 閫氳繃 `server_join.retry_join` 鎸佺画閲嶈瘯鎺у埗骞抽潰 RPC 鍦板潃锛岄伩鍏?server 鐭殏涓嶅彲杈炬椂鍙暀涓嬫湰鍦板瓨娲讳絾鏈敞鍐岀殑 agent銆?- 鍚屼竴 `panel_server_id` 瀛樺湪鏃?`down` 鑺傜偣鍜屾柊 `ready` 鑺傜偣鏃讹紝鎺у埗骞抽潰鎶曞奖蹇呴』浼樺厛灞曠ず `ready` 鑺傜偣锛岄伩鍏嶉噸閮ㄧ讲鍚庤鏃ц妭鐐硅褰曡鐩栦负绂荤嚎銆?- Nomad 杩愯鏃跺噯澶囧彲浠ュ畨瑁?Docker/CNI锛屼絾涓嶅緱鏃犳潯浠堕噸鍚?Docker锛汥ocker 宸茶繍琛屾椂鍙仛鍋ュ悍妫€鏌ワ紝鏈繍琛屾椂鎵嶅惎鍔紝閬垮厤 Panel 鑷韩閮ㄧ讲鍦ㄧ洰鏍囪妭鐐?Docker 涓椂琚腑鏂€?- server 寮曞銆乻erver 閲嶉儴缃插拰闆嗙兢閲嶅缓浼氫复鏃跺垏鎹?Panel 鐨?Nomad API 鍦板潃锛涘彧鏈?Panel 楠岃瘉 TCP 4646/API 鍙揪鍚庢墠淇濈暀鍦板潃锛屽け璐ュ繀椤诲洖婊氬埌鏃у湴鍧€銆?- server 鍒囨崲鍏堜娇鐢ㄧ敤鎴烽€夋嫨鐨?advertise 鍦板潃閲嶅啓骞堕噸鍚洰鏍?server锛屽啀楠岃瘉鏂?API 鍦板潃锛涢獙璇佹垚鍔熷悗鍚屾鎵€鏈?Panel 鎵樼 client 鐨勫畬鏁撮厤缃紝鎶?`server_join.retry_join` 鏇存柊涓烘柊 server RPC 鍦板潃锛岃ˉ榻?Nomad UFW 瑙勫垯锛岄€愬彴閲嶅惎骞剁‘璁ら噸鏂版敞鍐屻€?- 鑺傜偣閲嶉儴缃蹭細鍒犻櫎 Panel 鎵樼鐨勬棫 Nomad 閰嶇疆鍜?TLS 鏂囦欢锛屽苟鏍规嵁褰撳墠杩愯鏃惰缃噸鏂扮敓鎴愬畬鏁?server/client 閰嶇疆锛沜lient 閲嶉儴缃蹭娇鐢?Panel 褰撳墠閫夋嫨鐨?server RPC 鍦板潃銆?- Nomad 鑺傜偣閰嶇疆鐨?client meta 鍚屾 Panel 鏈嶅姟鍣ㄧ殑 ID銆佸悕绉般€丼SH host銆丼SH port 鍜?SSH username锛屼緵搴旂敤 allocation 杩愯鏃跺彉閲忚В鏋愩€?- 鍐呯疆璇佷功杞崲浠诲姟 `nomad_tls_rotate` 浼氶噸鏂扮敓鎴?CA銆乤gent 鍜?Panel client 璇佷功锛屽苟澶嶇敤闆嗙兢閲嶅缓娴佺▼閲嶉儴缃插叏閮ㄦ墭绠¤妭鐐广€佹仮澶嶅簲鐢ㄥ拰鍙嶅悜浠ｇ悊銆傚墠绔繀椤诲湪鎵ц鍓嶆槑纭彁绀虹煭鏆備腑鏂闄┿€?- 闆嗙兢閲嶅缓蹇呴』鍏堝紩瀵煎苟楠岃瘉鏂扮殑鍗?server 闆嗙兢锛屽啀閲嶇疆骞堕噸鏂板姞鍏ュ叾浠?Panel 鎵樼鑺傜偣锛屾渶鍚庢棤鏉′欢閲嶆柊娉ㄥ唽鏁版嵁搴撲腑鎵€鏈?`enabled` 搴旂敤骞跺悓姝ュ弽鍚戜唬鐞嗐€傚簲鐢ㄥ畾涔夈€佹枃浠躲€佸彉閲忓拰鍚敤鐘舵€佷繚瀛樺湪 Panel 鏁版嵁搴擄紝涓嶈兘鍥?Nomad 闆嗙兢閲嶅缓涓㈠け銆?- 闀胯€楁椂娴佺▼蹇呴』鍐欏叆浠诲姟銆佹楠ゅ拰鏃ュ織銆?- 鐩存帴鐢?goroutine 鎵ц鐨?Nomad 鑺傜偣鎿嶄綔鍒涘缓浠诲姟鍚庡繀椤诲厛钀?`running` 鍐嶈繑鍥?`taskId`锛岄伩鍏?Panel 杩涚▼涓柇鍚庝换鍔℃案涔呭仠鍦?`queued`銆?- 鍓嶇 Nomad 鑺傜偣椤垫彁浜ゅ姞鍏ャ€侀噸閮ㄧ讲銆侀噸寤烘垨鍒囨崲鍓嶅繀椤昏鐢ㄦ埛閫夋嫨 Nomad advertise 鍦板潃锛涙彁浜ゅ姞鍏ャ€侀噸閮ㄧ讲銆侀噸寤恒€佸垏鎹㈡垨绉婚櫎鍚庡繀椤讳繚鐣?`taskId`锛屽苟缁欏嚭璺宠浆浠诲姟涓績鐨勫叆鍙ｃ€?- 棣栦釜 server 寮曞浠庤缃〉璺冲洖鑺傜偣椤垫椂蹇呴』淇濈暀 `taskId`锛岃妭鐐归〉搴斿睍绀轰换鍔′腑蹇冨叆鍙ｃ€?- Nomad 鑺傜偣椤典笉灞曠ず鈥滃凡杩炴帴鍒?leader鈥濈殑鎴愬姛妯箙锛沜onnected 鐘舵€佸彧閫氳繃鑺傜偣鍒楄〃鍜岀粺璁″崱鐗囦綋鐜帮紝閬垮厤姝ｅ父鐘舵€佹彁绀哄崰鐢ㄩ〉闈㈡敞鎰忓姏銆?- 绉婚櫎 Nomad 鑺傜偣灞炰簬楂橀闄╂搷浣滐紝鍓嶇蹇呴』鍏堟樉绀虹‘璁ゅ璇濇銆?- 涓嶆仮澶?raw Nomad jobs/deployments/evaluations/services 瀵艰埅銆侀〉闈㈡垨鍏紑 API锛涘簲鐢ㄨ繍琛屾€佸彧閫氳繃搴旂敤妯″潡璇诲彇鍗曚釜 job 鐨?deployment銆乪valuation 鍜?allocation 淇℃伅銆?- Nomad 鎺у埗骞抽潰鎶曞奖渚濊禆鏈€鏂?`nomad_*` 浠诲姟锛屼换鍔℃煡璇㈤渶瑕佷繚鎸佹渶鏂颁紭鍏堬紝閬垮厤鏃т换鍔″垎椤甸伄鎸℃柊杩戠殑寮曞銆佸姞鍏ャ€侀噸寤恒€佺Щ闄ゅ拰 server 鍒囨崲鎿嶄綔銆?- 鍙嶅悜浠ｇ悊鍚屾浼氳鍙栧簲鐢ㄦā鍧楀拰璇佷功妯″潡鐨勬暟鎹紱淇濆瓨鎺ュ彛浼氬垱寤?`nomad_reverse_proxy_sync` 浠诲姟锛岃褰曡繙绋?UFW 鏀捐鍜?Nomad job reconcile 鐨勭粨鏋滐紝鍓嶇蹇呴』淇濈暀浠诲姟涓績鍏ュ彛銆?
## 楠岃瘉

- 鍏堟寜妯″潡绱㈠紩鐨勨€滄鏌ュ拰娴嬭瘯鑼冨洿鈥濆垽鏂槸鍚﹂渶瑕侀獙璇併€?- 闇€瑕侀獙璇佸悗绔敼鍔ㄦ椂锛岃繍琛?`task test:backend`锛岄噸鐐瑰叧娉?`internal/nomad` 娴嬭瘯銆?- 鍓嶇 Nomad 椤甸潰鎴?API 绫诲瀷鏀瑰姩鍙寜闇€瑕佽繍琛?`task test:web` 鎴?`task build:web`銆?
## 鏂囨。鏇存柊瑙﹀彂

鏂板 Nomad API銆佽繙绋嬪畨瑁呭懡浠ゃ€佽繍琛屾椂璁剧疆銆乀LS 琛屼负銆佹帶鍒跺钩闈㈠瓧娈点€佸弽鍚戜唬鐞嗛厤缃垨璺ㄦā鍧椾緷璧栨椂锛屽繀椤绘洿鏂版湰鏂囨。銆?
