# 鑿滃崟涓庢彁绀?
## 婧㈠嚭鎿嶄綔鑿滃崟

褰撲竴琛屾垨涓€涓崱鐗囨湁涓変釜浠ヤ笂娆¤鎿嶄綔鏃讹紝浣跨敤 `v-menu` 鏀剁撼锛岃Е鍙戞寜閽€氬父涓猴細

```html
<v-menu location="bottom end">
  <template #activator="{ props }">
    <v-btn
      v-bind="props"
      icon="mdi-dots-vertical"
      variant="text"
      size="small"
      aria-label="..."
    />
  </template>
  <v-list density="compact">...</v-list>
</v-menu>
```

## 鑿滃崟椤?
- 浣跨敤 `v-list density="compact"`銆?- 浣跨敤 `v-list-item`锛屾爣棰樺繀椤绘湰鍦板寲銆?- 甯歌鍔ㄤ綔鍙坊鍔?`prepend-icon`銆?- 缂栬緫绛夋櫘閫氭搷浣滀娇鐢ㄩ粯璁ゆ枃鏈壊銆?- 鍒犻櫎绛夌牬鍧忔€ф搷浣滀娇鐢?`class="text-error"`銆?- 鑿滃崟椤归『搴忎负甯哥敤鎿嶄綔鍦ㄥ墠銆佺牬鍧忔€ф搷浣滃湪鍚庛€?- 鐐瑰嚮鍗＄墖鎴栧垪琛ㄨ鍐呰彍鍗曟椂浣跨敤浜嬩欢闃绘锛岄伩鍏嶅悓鏃惰Е鍙戣閫夋嫨銆?
## 瑙﹀彂鎸夐挳

- 榛樿浣跨敤 text/icon/small銆?- 鍥炬爣涓?`mdi-dots-vertical` 鏃跺繀椤绘湁 `aria-label`銆?- 鍗曚竴鏄庣‘鍔ㄤ綔涓嶅簲涓轰簡瑙嗚绠€娲佽€岃棌鍏ヨ彍鍗曘€?- 椤甸潰涓绘搷浣滀笉鏀惧叆婧㈠嚭鑿滃崟銆?
## 閫夋嫨鑿滃崟

鐢ㄤ簬鎻掑叆鍙橀噺銆侀€夋嫨浠诲姟鏃ュ織绛変笟鍔￠€夐」鏃讹細

- 瑙﹀彂鍣ㄥ彲浠ユ槸 outlined 鏂囨湰鎸夐挳鎴栧皬鍨嬪浘鏍囨寜閽€?- 鍒楄〃浣跨敤 compact 瀵嗗害銆?- 閫夐」鏍囬搴旇兘鐙珛璇存槑閫夋嫨缁撴灉銆?- 閫夐」杈冨鎴栭渶瑕佹悳绱㈡椂鏀圭敤 `v-select` 鎴?`v-combobox`锛屼笉浣跨敤瓒呴暱鑿滃崟銆?
## Tooltip

`v-tooltip` 鐢ㄤ簬瑙ｉ噴锛?
- 浠呭浘鏍囪〃杈俱€佷絾 `aria-label` 浠嶄笉瓒充互甯姪榧犳爣鐢ㄦ埛鐞嗚В鐨勫伐鍏锋寜閽€?- 鎴柇浣嗗繀椤诲彲鏌ョ湅瀹屾暣鍊肩殑鐭枃鏈€?- 涓嶅父瑙佺殑鐘舵€佸浘鏍囥€?
Tooltip 涓嶆槸浠ヤ笅鍐呭鐨勬浛浠ｏ細

- 琛ㄥ崟 `label` 鎴?`hint`銆?- 閲嶈閿欒鍜岄闄╂彁绀恒€?- 闇€瑕佺敤鎴锋搷浣滅殑纭淇℃伅銆?- 绉诲姩绔繀椤诲彲鍙戠幇鐨勬牳蹇冨姛鑳借鏄庛€?
## 寮瑰嚭浣嶇疆涓庤鍙?
- 琛屾搷浣滆彍鍗曚紭鍏?`bottom end`锛屼笌鍙冲榻愭搷浣滃垪涓€鑷淬€?- 鑿滃崟涓嶅緱瓒呭嚭瑙嗗彛锛涗娇鐢?Vuetify 榛樿 overlay 瀹氫綅鑳藉姏銆?- 涓嶉€氳繃鍥哄畾缁濆鍧愭爣瀹氫綅鑿滃崟銆?- 瀵硅瘽妗嗗唴鑿滃崟搴旂‘淇濆眰绾х敱 Vuetify overlay 绠＄悊銆?
## 閿洏涓庢棤闅滅

- 瑙﹀彂鍣ㄥ彲閫氳繃閿洏鑱氱劍鍜屾墦寮€銆?- 鍥炬爣瑙﹀彂鍣ㄥ繀椤绘湁鍙闂悕绉般€?- 涓嶅湪鑿滃崟椤逛腑宓屽鍙︿竴缁勯毦浠ラ敭鐩樿闂殑鎸夐挳銆?- Tooltip 鍙彁渚涜ˉ鍏呬俊鎭紝鏍稿績璇箟浠嶇敱鎸夐挳鍚嶇О鎴栨鏂囨彁渚涖€?
## 绂佸繉

- 涓嶆妸涓昏淇濆瓨銆佸垱寤烘垨纭鍔ㄤ綔闅愯棌鍒拌彍鍗曘€?- 涓嶇敤鑿滃崟浠ｆ浛澶嶆潅琛ㄥ崟鎴栧姝ラ娴佺▼銆?- 涓嶄负姣忎釜琛ㄦ牸琛屽缁堝睍寮€鍥涗簲涓寜閽€?- 涓嶇敤 Tooltip 鎵胯浇闀挎璇存槑銆?
## 婧愮爜渚濇嵁

- `web/src/views/dns/domains/index.vue`
- `web/src/views/overview/index.vue`
- `web/src/views/runtime/applications/ApplicationRuntimePanel.vue`
- `web/src/views/runtime/applications/ApplicationEditor.vue`


