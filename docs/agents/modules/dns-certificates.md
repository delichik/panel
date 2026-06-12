# DNS 涓庤瘉涔︽ā鍧?
## 閫傜敤鍦烘櫙

淇敼 DNS 鍩熷悕绠＄悊銆丆loudflare 闆嗘垚銆丄CME 绛惧彂銆佽瘉涔﹀瓨鍌ㄣ€佽嚜鍔ㄧ画绛俱€佽瘉涔﹀彉閲忔垨搴旂敤/鍙嶅悜浠ｇ悊璇佷功鑱斿姩鏃讹紝鍏堣鏈枃妗ｃ€?
## 鍚庣鍏ュ彛

- DNS 鏈嶅姟涓?handler锛歚internal/dns/`
- Cloudflare provider锛歚internal/dns/cloudflare.go`
- 璇佷功鏈嶅姟涓?handler锛歚internal/certs/`
- ACME 闆嗘垚锛歚internal/certs/acme.go`
- 浠诲姟璁板綍锛歚internal/tasks/`
- 璋冨害缁锛歚internal/scheduler/`
- 搴旂敤鍙橀噺鍜屽弽鍚戜唬鐞嗚仈鍔細`internal/applications/`銆乣internal/nomad/`
- 璺敱瑁呴厤锛歚internal/app/app.go`

## 鍓嶇鍏ュ彛

- DNS 鍩熷悕涓庤褰曢〉闈細`web/src/views/dns/domains/index.vue`
- 鍩熷悕璇佷功椤甸潰锛歚web/src/views/certificates/domains/index.vue`
- Nomad 鍐呯疆璇佷功锛歚web/src/views/certificates/builtin/index.vue`
- 瀵嗛挜涓庤瘉涔︼細`web/src/views/certificates/key-assets/index.vue`
- 璇佷功璁剧疆锛歚web/src/views/settings/_shared/SettingsPageContent.vue`
- API锛歚web/src/api/dns.ts`銆乣web/src/api/certificates.ts`
- 绫诲瀷锛歚web/src/types/api.ts`

## API 鑼冨洿

- DNS 鍩熷悕锛歚GET/POST /api/v1/dns/domains`锛宍PUT/DELETE /api/v1/dns/domains/{id}`
- DNS 璁板綍锛歚GET/POST /api/v1/dns/domains/{id}/records`锛宍PUT/DELETE /api/v1/dns/domains/{id}/records/{recordId}`锛涘綋鍓嶅彧鏀寔 Cloudflare锛岃褰曚笉钀芥湰鍦板簱锛屽疄鏃惰鍐?Cloudflare銆?- 璇佷功锛歚GET/POST /api/v1/certificates`锛宍DELETE /api/v1/certificates/{id}`
- 鍩熷悕璇佷功绔嬪嵆缁锛歚POST /api/v1/certificates/{id}/renew`
- Nomad 鍐呯疆璇佷功锛歚GET /api/v1/certificates/builtin`锛宍POST /api/v1/certificates/builtin/rotate`
- 缁熶竴瀵嗛挜璧勪骇锛歚/api/v1/key-assets`锛涙棫 `/api/v1/self-signed-certificates` 鍜?`/api/v1/self-signed-cas` 浠呬繚鐣欏吋瀹瑰叆鍙?- 璇佷功榛樿鍊煎拰 ACME 鐩綍閫氳繃杩愯鏃惰缃鍐欙細`GET/PUT /api/v1/settings/runtime`

## 鏁版嵁涓庤涓虹害瀹?
- DNS 鍩熷悕瀛樺偍鍦?`dns_domains`锛岃瘉涔﹁褰曞瓨鍌ㄥ湪 `certificates`銆?- 褰撳墠 DNS provider 浠?Cloudflare 涓轰富锛孉PI token 鍜?account ID 灞炰簬鏁忔劅閰嶇疆銆?- DNS 璁板綍绠＄悊澶嶇敤鍩熷悕淇濆瓨鐨?Cloudflare API token 鍜?account ID锛涘墠绔湪鍩熷悕椤典娇鐢ㄥ乏渚у煙鍚嶅垪琛ㄣ€佸彸渚т笂鏂硅鎯呫€佸彸渚т笅鏂硅褰曡〃鐨勫竷灞€銆?- 鍩熷悕宸︿晶鍒楄〃涓庡叾浠栭€夋嫨鍣ㄨ鎯呴〉浣跨敤缁熶竴瀹藉害鍜岀揣鍑戦€変腑鎬侊紱鍒囨崲鍩熷悕鏃剁珛鍗虫竻绌轰笂涓€鍩熷悕鐨勮褰曞苟鏄剧ず鍔犺浇鐘舵€侊紝鍙帴鏀跺綋鍓嶅煙鍚嶅搴旂殑璇锋眰缁撴灉銆?- Cloudflare provider 浣跨敤瀹樻柟 REST API v4 鍜?Bearer API Token锛涜褰曞垪琛ㄥ繀椤绘寜 `result_info.total_pages` 璇诲彇鍏ㄩ儴鍒嗛〉锛屼笉鑳藉亣璁惧崟椤靛寘鍚?zone 鐨勫叏閮ㄨ褰曘€?- 闈㈡澘 API 鍙互鎺ユ敹 `@`銆佺浉瀵硅褰曞悕鎴?zone 鍐呭畬鏁磋褰曞悕锛涘彂閫佸埌 Cloudflare 鍓嶇粺涓€瑙勮寖鍖栦负 zone 鍐呭畬鏁村悕绉般€侰loudflare 閿欒鍝嶅簲浼樺厛瑙ｆ瀽瀹樻柟 envelope 涓殑閿欒鐮佸拰娑堟伅銆?- 鏂板鎴栫紪杈?Cloudflare 鍩熷悕鏃讹紝鍚庣蹇呴』鍏堜娇鐢ㄦ渶缁堢敓鏁堢殑 token銆乤ccount ID 鍜屽煙鍚嶈闂?Cloudflare 璁板綍鎺ュ彛楠岃瘉鏉冮檺涓?zone 鍙鎬э紱楠岃瘉澶辫触涓嶅緱鍐欏叆鏈湴鍩熷悕璁板綍銆?- ACME 绛惧彂浼氬垱寤?DNS-01 challenge锛岀瓑寰?DNS 浼犳挱鍚庡畬鎴愮鍙戯紱涓€鏃?challenge 璁板綍宸插垱寤猴紝鍚庣画绛夊緟銆佹巿鏉冩垨绛惧彂澶辫触涔熷繀椤诲皾璇曟竻鐞?DNS 璁板綍銆?- 閫氶厤绗﹁瘉涔︿細灞曞紑闇€瑕佺殑鍩熷悕闆嗗悎锛涚鍙戞垚鍔熷悗鍐欏叆璇佷功璺緞銆佺閽ヨ矾寰勩€佹湁鏁堟湡鍜岀画绛炬椂闂淬€?- 璇佷功鍙敞鍐屼负搴旂敤鍐呯疆鍙橀噺锛屽苟琚簲鐢ㄦā鍧楀拰 Nomad 鍙嶅悜浠ｇ悊璇诲彇銆?- 鏂板彉閲忓瓧娈典娇鐢?`certificate_pem`銆乣private_key_pem` 铔囧舰鍚嶇О锛沘lpha 鍏煎鏈熺户缁В鏋愭棫椹煎嘲瀛楁銆?- 鑷 CA 鍙互閲嶅绛惧彂鍙跺瓙璇佷功锛涘彾瀛愯瘉涔︽敮鎸?DNS/IP SAN锛屽苟淇濆瓨璇佷功銆佺閽ュ拰鍏挜銆?- CA 鏈夊瓙璇佷功鏃剁姝㈠垹闄わ紱鍩熷悕璇佷功鎴栬嚜绛捐瘉涔﹁搴旂敤 `panel_file` 鎴栧弽鍚戜唬鐞嗕娇鐢ㄦ椂绂佹鍒犻櫎銆?- 鏃ц嚜绛捐瘉涔﹂〉浠嶄繚鐣欏吋瀹瑰叆鍙ｏ紝浣嗘柊寤恒€侀噸绛惧拰鍒犻櫎閮藉簲浣跨敤缁熶竴鐨?`app-dialog-*` 瀵硅瘽妗嗙粨鏋勶紱鍒犻櫎纭涓嶈兘閫€鍥炲埌娴忚鍣ㄥ師鐢?`window.confirm`銆?- Nomad 鍐呯疆璇佷功涓嶈兘鍒犻櫎锛屽彧鑳介珮椋庨櫓閲嶆柊鐢熸垚銆傞噸鏂扮敓鎴愪細杞崲 CA/agent/Panel client 璇佷功骞惰嚜鍔ㄩ噸寤烘墭绠?Nomad 闆嗙兢銆?- 鍩熷悕缁鍜岃嚜绛惧彾瀛愰噸鏂扮鍙戞垚鍔熷悗浼氶噸鏂伴儴缃插彈褰卞搷搴旂敤骞跺悓姝ュ弽鍚戜唬鐞嗭紱澶辫触鏃朵繚鐣欎笂涓€浠藉彲鐢ㄦ枃浠躲€?- 绛惧彂銆佸け璐ュ拰缁搴旇褰曚换鍔℃棩蹇楋紱绗笁鏂归敊璇枃鏈槸鍚︾炕璇戦渶瑕佹寜 i18n 鎸囧崡璇勪及銆?- 璇佷功绛惧彂鎺ュ彛杩斿洖 `taskId`锛屽墠绔鍙戞垚鍔熷悗蹇呴』鎻愪緵浠诲姟涓績鍏ュ彛銆?- 鑷姩缁湡澶辫触蹇呴』鍐欏叆璇佷功 `lastError`锛屽苟璁板綍澶辫触鐨?`certificate_renew` 浠诲姟锛涚画鏈熷け璐ヤ笉搴旀竻闄や粛鐒跺彲鐢ㄧ殑鏃㈡湁璇佷功鏂囦欢銆?
## 楠岃瘉

- 鍏堟寜妯″潡绱㈠紩鐨勨€滄鏌ュ拰娴嬭瘯鑼冨洿鈥濆垽鏂槸鍚﹂渶瑕侀獙璇併€?- 闇€瑕侀獙璇佸悗绔敼鍔ㄦ椂锛岃繍琛?`task test:backend`锛岄噸鐐瑰叧娉?`internal/dns`銆乣internal/certs`銆乣internal/scheduler`銆?- 鍓嶇椤甸潰銆佽缃垨 API 绫诲瀷鏀瑰姩鍙寜闇€瑕佽繍琛?`task test:web` 鎴?`task build:web`銆?
## 鏂囨。鏇存柊瑙﹀彂

鏂板 DNS provider銆丏NS 璁板綍瀛楁銆佽瘉涔﹀瓧娈点€丄CME 琛屼负銆佺画绛捐鍒欍€佽瘉涔﹀彉閲忋€佸弽鍚戜唬鐞嗚瘉涔﹁仈鍔ㄦ垨鐩稿叧 API 鏃讹紝蹇呴』鏇存柊鏈枃妗ｃ€?
## 瀵嗛挜涓庤瘉涔︾粺涓€璧勪骇

- 缁熶竴璧勪骇鍚庣浣嶄簬 `internal/keyassets/`锛屾敮鎸?`ca_certificate`銆乣tls_certificate`銆乣ssh_key_pair`锛涚閽ョ敱 `internal/secretstore/` 鍔犲瘑鍚庡啓鍏?`key_assets`銆?- 瀵嗛挜涓庤瘉涔﹂〉闈綅浜?`web/src/views/certificates/key-assets/index.vue`锛岃瘉涔︿竴绾ц彍鍗曚笅鍖呭惈鍐呯疆璇佷功銆佸煙鍚嶈瘉涔︺€佸瘑閽ヤ笌璇佷功涓変釜浜岀骇鑿滃崟銆?- API 浣跨敤 `/api/v1/key-assets`锛屽寘鍚?CA/TLS/SSH 鐢熸垚鎴栧鍏ャ€乀LS 閲嶆柊绛惧彂銆丼SH 閲嶆柊鐢熸垚銆佹枃浠朵笅杞姐€佸垹闄ゃ€佸姞瀵嗘壒閲忓鍑哄拰棰勬鍚庢壒閲忓鍏ャ€?- CA 鍙噸澶嶇鍙戝涓?TLS 瀛愯瘉涔︼紱鏈夊瓙璇佷功鐨?CA銆佽搴旂敤 `panel_file` 鎴栧弽鍚戜唬鐞嗗紩鐢ㄧ殑璧勪骇绂佹鍒犻櫎锛孉PI 杩斿洖鍑嗙‘鐨勫簲鐢ㄥ紩鐢ㄤ俊鎭€?- TLS 閲嶆柊绛惧彂銆丼SH 閲嶆柊鐢熸垚鍜屾壒閲忓鍏ユ垚鍔熷悗浼氶噸鏂伴儴缃插凡鍚敤搴旂敤骞跺悓姝ュ弽鍚戜唬鐞嗭紱澶辫触鏃朵繚鐣欎笂涓€浠藉彲鐢ㄦ暟鎹€?- 鏃?`self_signed_certificates` 鍦ㄥ惎鍔ㄦ椂瀹屾暣鏍￠獙鍚庝簨鍔¤縼绉诲埌 `key_assets`锛屾彁浜ゆ垚鍔熸墠娓呯悊鏃ф枃浠讹紱鏃ц嚜绛?API 鍜?`certificate:` 鎸傝浇浠呬繚鐣欏吋瀹硅鍙栥€?- 鎵归噺瀵煎嚭鏂囦欢浣跨敤鐢ㄦ埛瀵嗙爜鍔犲瘑锛岀煭鏈熷瓨鏀惧湪 `tmp`锛涘鍏ュ厛鎵ц鍐茬獊銆佺埗 CA 鍜屼娇鐢ㄤ腑瑕嗙洊妫€鏌ワ紝鍐嶆寜鐢ㄦ埛绛栫暐鎵ц銆?
