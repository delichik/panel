# 鍒嗛〉

## 鍏变韩缁勪欢

鎵€鏈夌敤鎴峰彲瑙佹暟鎹垪琛ㄤ紭鍏堜娇鐢?`AppPagination.vue`銆?
### Props

| 灞炴€?| 绫诲瀷 | 榛樿鍊?|
| --- | --- | --- |
| `page` | `number` | 蹇呭～ |
| `pageSize` | `number` | 蹇呭～ |
| `total` | `number` | 蹇呭～ |
| `pageSizes` | `number[]` | `[10, 20, 50, 100]` |

### Events

- `update:page`
- `update:pageSize`

淇敼 page size 鏃剁粍浠朵細鑷姩鎶?page 閲嶇疆涓?`1`銆?
## 鏄剧ず瑙勫垯

- `total <= 0` 鏃朵笉娓叉煋銆?- 宸︿晶鏄剧ず鏈湴鍖栨€绘暟銆?- 涓棿涓洪〉澶у皬閫夋嫨鍣紝瀹藉害绾?`118px`銆?- 鍙充晶涓?Vuetify `v-pagination`銆?- 灏忓睆鏈€澶氭樉绀?`5` 涓垎椤甸」锛屽叾浠栧睆骞曟渶澶?`10` 涓€?- 褰撳墠椤垫寜閽娇鐢ㄤ富鑹插疄搴曘€?
## 甯冨眬

- 鍒嗛〉鏀惧湪鍒楄〃鎴栬〃鏍煎崱鐗囧簳閮ㄣ€?- 椤堕儴鏈?`--lp-border` 鍒嗛殧绾裤€?- 鑳屾櫙浣跨敤寮卞寲鐨?`--lp-surface-container`銆?- 妗岄潰妯悜闈犲彸锛屾€绘暟閫氳繃 `margin-right: auto` 闈犲乏銆?- `760px` 浠ヤ笅鏀逛负绾靛悜锛岄〉澶у皬閫夋嫨鍣ㄥ～婊″搴︺€?
## 鍓嶇鍒嗛〉

鏁扮粍鍨嬫帴鍙ｄ娇鐢?`usePagination.ts`锛?
- 杈撳叆鏁版嵁鍙樺寲鏃朵繚鎸侀〉鐮佸悎娉曘€?- 椤甸潰鍙秷璐?`pageItems`銆?- 鎬绘暟鏉ヨ嚜鍘熷鏁扮粍闀垮害銆?- 鎼滅储鎴栫瓫閫夊彉鍖栧悗搴斿洖鍒扮涓€椤点€?
## 鏈嶅姟绔垎椤?
- 淇濈暀鎺ュ彛杩斿洖鐨?`total`銆?- `update:page` 鍜?`update:pageSize` 瑙﹀彂閲嶆柊璇锋眰銆?- page size 鍙樺寲鍚庝粠绗竴椤佃姹傘€?- 璇锋眰鏈熼棿淇濈暀鐜版湁鍐呭骞朵娇鐢ㄥ眬閮?loading銆?
## 宓屽鍖哄煙

瀵硅瘽妗嗘垨璇︽儏瀛愯〃鍙户缁娇鐢?`AppPagination`锛屼絾蹇呴』纭繚锛?
- 瀹瑰櫒瀹藉害瓒冲銆?- 绉诲姩绔旱鍚戝竷灞€涓嶄細閬尅琛ㄥ崟鎿嶄綔銆?- 鍒嗛〉褰掑睘娓呮锛屼笉涓庨〉闈富鍒楄〃鍒嗛〉娣锋穯銆?
## 绂佸繉

- 涓嶇洿鎺ュ湪椤甸潰涓嫾瑁呭彟涓€濂楁€绘暟銆侀〉澶у皬鍜屽垎椤垫寜閽€?- 涓嶅湪 `total = 0` 鏃朵繚鐣欑┖鍒嗛〉鏉°€?- 涓嶈椤靛ぇ灏忓彉鍖栧悗鍋滅暀鍦ㄨ秺鐣岄〉鐮併€?- 涓嶆妸鍓嶇鏁扮粍鍒嗛〉浼鎴愭湇鍔＄璇锋眰鍒嗛〉銆?
## 婧愮爜渚濇嵁

- `web/src/components/AppPagination.vue`
- `web/src/composables/usePagination.ts`
- `web/src/views/runtime/applications/index.vue`
- `web/src/views/tasks/index.vue`


