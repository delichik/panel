# 澶氳瑷€缈昏瘧鐘舵€?

鏈枃妗ｈ褰曞綋鍓嶅璇█瀹炵幇浠嶆湭瀹屾垚缈昏瘧鐨勫尯鍩熴€傚悗缁瘡娆″鐞嗗璇█鐩稿叧浠诲姟鏃讹紝閮藉簲鍚屾鏇存柊銆?

## 褰撳墠浠嶆湭瀹屽叏瑕嗙洊

### 鏈€杩戝凡琛ラ綈

- `web/src/layouts/AppLayout.vue`
  - Header 的 DEV 构建通道标识和完整版本提示已接入英文、简体中文词条。

- `web/src/layouts/AppLayout.vue`
  - 绉诲姩绔鑸叆鍙ｆ枃妗堝凡鎺ュ叆 `web/src/i18n/index.ts`
  - 鏈惎鐢ㄧ殑 DNS 璁板綍瀵艰埅鍏ュ彛宸茬Щ闄わ紝瀵瑰簲 `layout.nav.records` 璇嶆潯涓嶅啀淇濈暀銆?- `web/src/views/dns/domains/index.vue`
  - DNS 鍩熷悕璇︽儏銆丆loudflare 璁板綍鍒楄〃銆佽褰曞垱寤?缂栬緫/鍒犻櫎鍜?TTL/浠ｇ悊鐘舵€佹枃妗堝凡鎺ュ叆 `web/src/i18n/index.ts`銆?- `web/src/api/client.ts`
  - 闈?JSON API 鍝嶅簲鐨勫彲璇婚敊璇枃妗堝凡鎺ュ叆 `web/src/i18n/index.ts`銆?- `internal/dns`
  - Cloudflare 闈?JSON 鍝嶅簲閿欒鐮佸凡鎺ュ叆 `internal/i18n/i18n.go`銆?- `web/src/views/settings/_shared/SettingsPageContent.vue`
  - Token 杩囨湡鏃堕棿璁剧疆鍙婇€夐」鏂囨宸叉帴鍏?`web/src/i18n/index.ts`
  - 璁剧疆鍒嗙被瀛愯彍鍗曘€侀€氱敤璁剧疆銆佸畨鍏ㄨ缃€丯omad 璁剧疆銆佽瘉涔﹁缃€佺郴缁熶俊鎭枃妗堝凡鎺ュ叆 `web/src/i18n/index.ts`
  - 鐧诲綍椤垫爣棰樺拰璇存槑鑷畾涔夊瓧娈点€佺暀绌哄洖閫€鎻愮ず宸叉帴鍏?`web/src/i18n/index.ts`
- `web/src/views/auth/change-password/index.vue`
  - 棣栨寮哄埗鏀瑰瘑椤甸潰鏂囨宸叉帴鍏?`web/src/i18n/index.ts`
- `internal/auth`
  - 鐧诲綍澶辫触鐨勯€氱敤閿欒鏂囨 `Authentication failed` 宸叉帴鍏?`internal/i18n/i18n.go`
  - 寮哄埗鏀瑰瘑銆佽处鍙锋洿鏂般€丣WT 瀵嗛挜鏇存柊鐩稿叧閿欒鏂囨宸叉帴鍏?`internal/i18n/i18n.go`
- `web/src/views/runtime/nomad/nodes/index.vue`
  - Nomad 鑺傜偣閲嶉儴缃层€侀泦缇ら噸寤恒€乻erver 鍒囨崲鍙婂垏鎹㈠悗 client 閰嶇疆鍚屾鏂囨宸叉帴鍏?`web/src/i18n/index.ts`
  - 鏃ч泦缇ょ綉缁滃湴鍧€杩佺Щ銆丼SH host IP 涓?Nomad 缃戝崱 IP 鐨?advertise 鍦板潃閫夋嫨銆侀噸寤哄悗搴旂敤鎭㈠鎻愮ず宸叉帴鍏?`web/src/i18n/index.ts`
  - 鍔犲叆銆侀噸閮ㄧ讲銆侀噸寤哄拰鍒囨崲鎿嶄綔鐨?advertise 鍦板潃閫夋嫨鏍囩涓庣┖鐘舵€佹彁绀哄凡鎺ュ叆 `web/src/i18n/index.ts`
  - 棣栦釜 server 寮曞浠诲姟鍏ュ彛銆佸弽鍚戜唬鐞嗗悓姝ヤ换鍔℃彁绀烘枃妗堝凡鎺ュ叆 `web/src/i18n/index.ts`
- `web/src/views/servers/_shared/ServersPageContent.vue`
  - 鏈嶅姟鍣ㄥ嚟鎹繀閫夋彁绀恒€侀噸鍚‘璁や笌浠诲姟鍏ュ彛銆乁FW 瀹夎浠诲姟鍏ュ彛銆佺郴缁熸灦鏋?CPU/鍒嗛」缃戝崱璇︽儏鏂囨宸叉帴鍏?`web/src/i18n/index.ts`
  - 鏂板鏈嶅姟鍣ㄥ悗鐨勯杩炰俊鎭噰闆嗕换鍔℃彁绀恒€佸け璐ュ洖閫€鎻愮ず鍜岃秴鏃舵彁绀哄凡鎺ュ叆 `web/src/i18n/index.ts`
- `web/src/views/servers/firewall/index.vue`
  - UFW 闃茬伀澧欑姸鎬併€佸惎鐢ㄧ‘璁ゃ€佽鍒欒〃鍗曘€佽鍒欏垪琛ㄣ€佸垹闄ょ‘璁ゅ拰浠诲姟鍏ュ彛鏂囨宸叉帴鍏?`web/src/i18n/index.ts`
- `web/src/views/tasks/index.vue`
  - 浠诲姟绫诲瀷鍚嶇О銆佺被鍨嬬瓫閫夌壒娈婇€夐」銆佹悳绱㈡寜閽拰澶氶€夌瓫閫夊崰浣嶆枃妗堝凡鎺ュ叆 `web/src/i18n/index.ts`
  - 鎿嶄綔鏍囬銆佹楠ゅ悕绉般€佷换鍔￠樁娈靛拰鏃ュ織闈㈡澘浠诲姟绫诲瀷宸叉寜绋冲畾鏍囪瘑缈昏瘧銆?- `web/src/components/AppPagination.vue`
  - 鍏变韩鍒嗛〉缁勪欢鐨勬瘡椤垫潯鏁颁笌鎬绘暟鏂囨宸叉帴鍏?`web/src/i18n/index.ts`銆?- `web/src/components/PageLoadingState.vue`
  - 鍏变韩鍔犺浇缁勪欢鏂囨 `common.loading` 宸叉帴鍏?`web/src/i18n/index.ts`锛岃嫳鏂囧拰绠€浣撲腑鏂囧潎宸茶ˉ榻愩€?- `web/src/views/runtime/applications/ApplicationEditor.vue`
  - 鑷畾涔夊彉閲忚〃鍗曘€佸彉閲忔彃鍏ュ拰 Panel 鎵樼鏂囦欢鎸傝浇鏂囨宸叉帴鍏ヨ嫳鏂囧拰绠€浣撲腑鏂囥€?- `web/src/views/certificates/`
  - Nomad 鍐呯疆璇佷功銆佸煙鍚嶇珛鍗崇画绛俱€佽嚜绛?CA/璇佷功绠＄悊鍜屽嵄闄╃‘璁ゆ枃妗堝凡鎺ュ叆鑻辨枃鍜岀畝浣撲腑鏂囥€?- `web/src/layouts/AppLayout.vue`銆乣web/src/views/settings/_shared/SettingsPageContent.vue`
  - 褰撳墠鐗堟湰銆佹渶鏂扮増鏈急鎻愮ず鍜岀郴缁熺増鏈瓧娈靛凡鎺ュ叆 `web/src/i18n/index.ts`銆?- raw Nomad jobs/deployments 娓呭崟鍏ュ彛宸茬Щ闄わ紝瀵瑰簲椤甸潰鏂囨鍜岃矾鐢辫瘝鏉′笉鍐嶄繚鐣欍€?- `internal/nomad`
  - Nomad 閲嶉儴缃层€侀泦缇ら噸寤恒€乻erver 鍒囨崲鐩稿叧 API 閿欒鐮佸凡鎺ュ叆 `internal/i18n/i18n.go`
  - Nomad advertise 鍦板潃鏍￠獙閿欒鐮佸凡鏇存柊涓烘敮鎸佺綉鍗?IP 鎴?SSH host IP 鐨勬枃妗堛€?
### 鍓嶇浠嶆湁灏戦噺绗笁鏂瑰師濮嬫枃鏈?
浠ヤ笅鍐呭浠嶄細灞曠ず绗笁鏂圭郴缁熺洿鎺ヨ繑鍥炵殑鍘熷鎻忚堪锛屽綋鍓嶄繚鐣欏師鏍蜂互閬垮厤璇瘧锛?

- `web/src/views/runtime/applications/ApplicationRuntimePanel.vue`
  - `deployment.StatusDescription`
  - `evaluation.StatusDescription`
  - `evaluation.Type`

### 鍚庣缈昏瘧浠嶄负閮ㄥ垎瑕嗙洊

褰撳墠鍚庣宸茶鐩栫粺涓€ API 閿欒缈昏瘧鍏ュ彛锛屼絾浠ヤ笅绫诲埆浠嶉渶缁х画琛ラ綈锛?

- Cloudflare / ACME / 闀滃儚浠撳簱鐩稿叧閿欒鐮?- SSH / 杩滅▼鎵ц / 瓒呮椂鐩稿叧閿欒鐮?- 妯℃澘娓叉煋銆侀€夋嫨鍣ㄨВ鏋愮瓑搴曞眰閿欒鐮?- 绗笁鏂圭郴缁熺洿鎺ヨ繑鍥炵殑鍘熷閿欒鏂囨湰
- 浠诲姟鎽樿銆佷换鍔?system 鏃ュ織銆佷换鍔¤繃鏈熸竻鐞嗗啓鍏ョ殑閿欒鍘熷洜涓庤繙绋嬪懡浠よ瘖鏂枃鏈粛浠ュ師濮嬫墽琛屾枃鏈睍绀猴紝鍖呮嫭 Nomad 寮曞/鍔犲叆娴佺▼鏃ュ織

## 鏇存柊瑙勫垯

鍙戠敓浠ヤ笅浠讳竴鎯呭喌鏃讹紝蹇呴』鏇存柊鏈枃妗ｏ細

- 鏂伴〉闈㈡垨鏂扮粍浠舵帴鍏ヤ簡澶氳瑷€
- 鏌愪釜椤甸潰浠嶆湭缈昏瘧浣嗙户缁淇敼
- 鏂板浜嗗悗绔敊璇爜鎴栫敤鎴峰彲瑙侀敊璇枃鏈?
- 鏂板浜嗙敤鎴峰彲瑙佹枃妗堜絾鏆傛湭瀹屾垚缈昏瘧

## 瀵嗛挜涓庤瘉涔?
- `web/src/views/certificates/key-assets/index.vue` 鐨?CA銆乀LS銆丼SH銆佹壒閲忓鍏ュ鍑恒€佸啿绐佺‘璁ゅ拰寮曠敤鎻愮ず宸叉帴鍏ヨ嫳鏂囦笌绠€浣撲腑鏂囪瘝鏉°€?- `key_asset_*` 涓诲瘑閽ャ€佸綊妗ｃ€佺被鍨嬨€佺埗 CA銆佷娇鐢ㄤ腑鍜屽鍏ュ啿绐侀敊璇爜宸叉帴鍏?`internal/i18n/i18n.go`銆?- 浠诲姟涓績宸茶ˉ鍏呭瘑閽ヨ祫浜т换鍔＄被鍨嬨€侀樁娈靛拰鎿嶄綔鏍囬缈昏瘧銆?

## DNS provider 凭据

- Cloudflare 域名配置已移除 Account ID 文案，只保留 API Token。
- DNS provider 凭据缺失或格式无效的后端错误码已补充简体中文翻译。
