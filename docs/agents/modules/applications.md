# 搴旂敤妯″潡

## 閫傜敤鍦烘櫙

淇敼搴旂敤鍒涘缓銆佺紪杈戙€乤ppspec銆佸彉閲忚В鏋愩€佸簲鐢ㄦ枃浠躲€佷繚瀛樹細璇濄€佷慨璁€丯omad job 娓叉煋銆侀儴缃层€佸仠姝€侀噸鍚€佹棩蹇椼€佽繍琛屾椂鐘舵€併€侀暅鍍忔洿鏂版垨搴旂敤鍙嶅悜浠ｇ悊鏃讹紝鍏堣鏈枃妗ｃ€?
## 鍚庣鍏ュ彛

- 搴旂敤鏈嶅姟涓?handler锛歚internal/applications/`
- 搴旂敤瑙勬牸妯″瀷銆佹牎楠屽拰娓叉煋锛歚internal/appspec/`
- 璋冨害浣嶇疆閫夋嫨锛歚internal/orchestrator/`
- 妯℃澘娓叉煋鎺ュ彛锛歚internal/templatex/`
- Nomad client锛歚internal/nomad/`
- 浠诲姟璁板綍锛歚internal/tasks/`
- 璺敱瑁呴厤鍜岃法妯″潡杩炴帴锛歚internal/app/app.go`

## 鍓嶇鍏ュ彛

- 搴旂敤椤甸潰锛歚web/src/views/runtime/applications/index.vue`
- 缂栬緫鍣細`web/src/views/runtime/applications/ApplicationEditor.vue`
- 璇︽儏锛歚web/src/views/runtime/applications/ApplicationDetail.vue`
- 杩愯鏃讹細`web/src/views/runtime/applications/ApplicationRuntimePanel.vue`
- 鏃ュ織锛歚web/src/views/runtime/applications/ApplicationLogsPanel.vue`
- API锛歚web/src/api/applications.ts`
- 绫诲瀷锛歚web/src/types/api.ts`

## API 鑼冨洿

- 搴旂敤 CRUD锛歚GET/POST /api/v1/applications`锛宍GET/PUT/DELETE /api/v1/applications/{id}`
- 搴旂敤鏂囦欢锛歚GET/POST /api/v1/applications/{id}/files`锛宍DELETE /api/v1/applications/{id}/files/{fileId}`
- 淇濆瓨浼氳瘽锛歚POST /api/v1/application-save-sessions`锛宍POST /api/v1/application-save-sessions/{id}/files`锛宍POST /api/v1/application-save-sessions/{id}/files/delete`锛宍POST /api/v1/application-save-sessions/{id}/commit`
- 鏍￠獙鍜岃鍒掞細`POST /api/v1/applications/{id}/validate`锛宍POST /api/v1/applications/{id}/plan`
- 杩愯鎿嶄綔锛歚POST /api/v1/applications/{id}/deploy`锛宍/stop`锛宍/restart`
- 闀滃儚锛歚POST /api/v1/applications/{id}/image/check`锛宍/image/update`
- 杩愯鏃跺拰鏃ュ織锛歚GET /api/v1/applications/{id}/runtime`锛宍GET /api/v1/applications/{id}/logs`
- 鎵撳寘锛歚GET /api/v1/applications/{id}/package`
- 妯℃澘鐩綍锛歚GET /api/v1/application-template-catalog`

## 鏁版嵁涓庤涓虹害瀹?
- 涓昏琛ㄥ寘鎷?`applications`銆乣application_files`銆乣application_revisions`銆?- 搴旂敤瑙勬牸浠?YAML 杈撳叆锛岀粡杩?`internal/appspec/` 鏍￠獙鍜屾覆鏌撲负 Nomad job銆?- 搴旂敤鍙橀噺銆侀儴缃叉ā寮忋€佸弽鍚戜唬鐞嗛厤缃瓑鎸佷箙鍖栧瓧娈靛繀椤讳繚瀛樼ǔ瀹氱粨鏋勶紝涓嶄繚瀛樺凡缈昏瘧灞曠ず鏂囨銆?- 鏂囦欢鍐呭閫氳繃 API 浠?base64 鎵胯浇锛屼繚瀛樹細璇濈敤浜庢壒閲忎笂浼犮€佸垹闄ゅ拰鎻愪氦銆?- 鍚敤搴旂敤銆侀儴缃层€侀暅鍍忔洿鏂扮瓑娴佺▼闇€瑕佸厛鏍￠獙鍜岃鍒掞紝鍐嶆敞鍐?Nomad job銆?- 杩愯鏃剁姸鎬併€侀儴缃层€佽瘎浼板拰鏃ュ織閮ㄥ垎鏉ヨ嚜 Nomad锛岀涓夋柟鍘熷鎻忚堪閫氬父淇濈暀鍘熸牱銆?- 搴旂敤閮ㄧ讲銆佸仠姝€侀噸鍚拰闀滃儚鏇存柊鎺ュ彛杩斿洖 `taskId` 鏃讹紝鍓嶇蹇呴』淇濈暀浠诲姟涓績鍏ュ彛锛涚紪杈戝櫒閲岀殑鈥滀繚瀛樺苟閮ㄧ讲鈥濅篃蹇呴』鎶婇儴缃?`taskId` 浼犲洖椤甸潰鎻愮ず銆?- `application_deploy` 浠诲姟琛ㄧず Nomad 宸叉帴鍙楅儴缃茶姹傦紝涓嶇瓑浠蜂簬搴旂敤宸茬粡鍋ュ悍杩愯锛涘疄闄?allocation/deployment 鍋ュ悍鐘舵€佸繀椤婚€氳繃杩愯鏃堕潰鏉垮睍绀恒€?- 搴旂敤鍒犻櫎蹇呴』鍏堢鐢ㄥ簲鐢紝骞跺湪鍓嶇浜屾纭鍚庢墽琛屻€?- 搴旂敤鏃ュ織闈㈡澘浠嶅厑璁告墜鍔ㄨ緭鍏?allocation/task锛屼絾杩愯鏃?allocation 琛ㄥ繀椤绘彁渚涙棩蹇楀叆鍙ｏ紝灏?allocation ID 鍜?task 鍚嶇О甯﹀叆鏃ュ織闈㈡澘銆?- 璇佷功妯″潡鎻愪緵鍐呯疆鍙橀噺瑙ｆ瀽锛孨omad 妯″潡璐熻矗鍙嶅悜浠ｇ悊鍚屾銆?- 妯℃澘鐩綍鎻愪緵 `server.id`銆乣server.name`銆乣server.ssh_host`銆乣server.ssh_port`銆乣server.ssh_username` 绛夎泧褰㈣妭鐐瑰彉閲忥紱鑺傜偣鍊兼潵鑷疄闄?allocation 鎵€鍦?Nomad 鑺傜偣鐨?`panel_*` meta锛屽叾涓?SSH 鍦板潃鍙栨湇鍔″櫒閰嶇疆鐨?`host`銆?- 搴旂敤鏂囦欢妯℃澘閫氳繃 Nomad template 鍦?allocation 鍚姩鏃惰鍙?`PANEL_SERVER_*` 鐜鍙橀噺锛屽洜姝ゅ悓涓€搴旂敤鍦ㄤ笉鍚岃妭鐐瑰緱鍒颁笉鍚屾湇鍔″櫒鍊笺€?- 鎸傝浇绫诲瀷 `panel_file` 浣跨敤 `certificate:<resource-id>:<kind>` 绋冲畾寮曠敤 Panel 鎵樼璇佷功鏂囦欢銆傜閽ュ唴瀹逛笉閫氳繃鐩綍 API 杩斿洖锛岄儴缃叉椂鐢卞悗绔鍙栧苟浠ュ彧璇?Nomad template 鎸傝浇銆?- 鑷畾涔夊彉閲忓湪鍓嶇浣跨敤閿€艰〃鍗曠淮鎶わ紝鎸佷箙鍖栦粛浣跨敤鐜版湁 `variables_json`銆?- Nomad 闆嗙兢閲嶅缓瀹屾垚鍚庝細璋冪敤 `RedeployEnabledApplications`锛屾棤鏉′欢閲嶆柊娓叉煋骞舵敞鍐屾墍鏈?`enabled` 搴旂敤锛涜鎭㈠琛屼负涓嶈兘鍙緷璧栬鏍煎搱甯屽彉鍖栵紝鍚﹀垯鏂伴泦缇や腑涓嶅瓨鍦ㄧ殑 job 涓嶄細琚仮澶嶃€?
## 楠岃瘉

- 鍏堟寜妯″潡绱㈠紩鐨勨€滄鏌ュ拰娴嬭瘯鑼冨洿鈥濆垽鏂槸鍚﹂渶瑕侀獙璇併€?- 闇€瑕侀獙璇佸悗绔敼鍔ㄦ椂锛岃繍琛?`task test:backend`锛岄噸鐐瑰叧娉?`internal/applications`銆乣internal/appspec`銆乣internal/orchestrator`銆?- 鍓嶇搴旂敤椤甸潰鎴?API 绫诲瀷鏀瑰姩鍙寜闇€瑕佽繍琛?`task test:web` 鎴?`task build:web`銆?
## 鏂囨。鏇存柊瑙﹀彂

鏂板 appspec 瀛楁銆佸簲鐢ㄦ寔涔呭寲瀛楁銆丄PI銆佸簲鐢ㄦ枃浠惰涓恒€侀儴缃叉祦绋嬨€侀暅鍍忔洿鏂伴€昏緫銆佸弽鍚戜唬鐞嗗瓧娈垫垨杩愯鏃跺睍绀哄瓧娈垫椂锛屽繀椤绘洿鏂版湰鏂囨。銆?
## Panel 鎵樼瀵嗛挜鏂囦欢

- `panel_file` 鐨勬柊瑙勮寖鏉ユ簮涓?`key_asset:<asset-id>:<kind>`锛屾敮鎸?`certificate`銆乣private_key`銆乣public_key` 鍜?SSH 鐨?`ssh_public_key`銆?- appspec 鏍￠獙鍚屾椂鎺ュ彈鏃?`certificate:` 鏉ユ簮浠ュ吋瀹瑰凡鏈夊簲鐢紱鏂伴〉闈㈠拰鐩綍鍙敓鎴?`key_asset:`銆?- 绉侀挜涓嶄細鍑虹幇鍦ㄧ洰褰?API 鍝嶅簲涓紝鍙湪閮ㄧ讲娓叉煋鏃剁敱鍚庣瑙ｅ瘑骞朵綔涓哄彧璇?Nomad template 鎻愪緵銆?- 瀵嗛挜璧勪骇鏈嶅姟鎵弿搴旂敤 spec 鍜屽弽鍚戜唬鐞嗗煙鍚嶏紝杩斿洖绮剧‘鐨勫簲鐢?ID銆佸悕绉板強 `panel_file` / `reverse_proxy` 寮曠敤锛岀敤浜庡垹闄や繚鎶ゅ拰瀵煎叆瑕嗙洊纭銆?- TLS 閲嶆柊绛惧彂銆丼SH 閲嶆柊鐢熸垚鍜屾壒閲忓鍏ヤ細璋冪敤 `RedeployEnabledApplications`锛岀‘淇濇瘡鍙版湇鍔″櫒閲嶆柊鎸夎嚜韬唴缃彉閲忔覆鏌撱€?

## Application editor command fields

- In `ApplicationEditor.vue`, visual editing keeps both appspec `command` and `args` as ordered arrays. Each row is one argv item; the editor must not split user input on whitespace. Use `command` for the executable/entrypoint and `args` for flags and argument values.
- Backend appspec validation rejects more than one `command` item because Nomad Docker config uses only one executable command; all flags and values must be represented in `args`.
- The application editor dialog is a single-page sectioned form rather than a tabbed editor. Standard short fields use two-column grids; port mappings, reverse proxy rules, and mount rows remain full-width repeated rows so dense network and storage settings stay readable.
