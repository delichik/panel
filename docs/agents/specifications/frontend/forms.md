# 琛ㄥ崟

## 鍩虹缁勪欢

浣跨敤 Vuetify锛?
- 鍗曡杈撳叆锛歚v-text-field`
- 閫夋嫨锛歚v-select`
- 鍙緭鍏ラ€夋嫨锛歚v-combobox`
- 澶氳杈撳叆锛歚v-textarea`
- 甯冨皵璁剧疆锛歚v-switch`
- 澶氶€夛細`v-checkbox` 鎴栬〃鏍间腑鐨?`v-checkbox-btn`
- 鏂囦欢锛歚v-file-input`

杈撳叆妗嗗叏灞€鍦嗚涓?`8px`锛岃仛鐒︽椂浣跨敤涓婚鑹茬劍鐐圭幆銆?
## 鍙樹綋涓庡瘑搴?
- 瀵硅瘽妗嗗拰鏅€氱紪杈戣〃鍗曪細`variant="outlined" density="comfortable"`銆?- 绛涢€夋爮銆侀噸澶嶈鍜岀揣鍑戝伐鍏峰尯锛歚variant="outlined" density="compact" hide-details`銆?- 鍚屼竴涓〃鍗曞垎鍖哄唴淇濇寔缁熶竴瀵嗗害銆?- 鍙湁鏃犻渶灞曠ず鏍￠獙銆佹彁绀烘垨閿欒鏂囨湰鐨勭揣鍑戞帶浠舵墠浣跨敤 `hide-details`銆?
## 鏍囩涓庢彁绀?
- 鎵€鏈夊彲缂栬緫瀛楁蹇呴』鏈夋湰鍦板寲 `label`銆?- 鏍煎紡瑕佹眰浣跨敤 `hint`锛涙寔缁噸瑕佺殑瑕佹眰浣跨敤 `persistent-hint`銆?- 绀轰緥鍊煎彲浣跨敤 `placeholder`锛屼絾涓嶈兘鏇夸唬鏍囩銆?- 瀵嗙爜瀛楁浣跨敤 `type="password"`銆?- 璺緞銆佸瘑閽ャ€乊AML銆佸搱甯岀瓑鍐呭浣跨敤绛夊瀛椾綋銆?
## 琛ㄥ崟甯冨眬

### 鍗曞垪琛ㄥ崟

璁剧疆椤电瓑杩炵画閰嶇疆浣跨敤锛?
```css
display: grid;
max-width: 560px;
gap: 16px;
```

### 鍙屽垪琛ㄥ崟

鐭瓧娈靛彲浣跨敤锛?
```css
grid-template-columns: repeat(2, minmax(0, 1fr));
gap: 12px;
```

- 闀挎枃鏈煙浣跨敤 `.span-all` 璺ㄨ秺鏁磋銆?- `760px` 浠ヤ笅杞负鍗曞垪銆?
### 閲嶅琛?
- 浣跨敤 grid锛岄棿璺?`8px` 鑷?`10px`銆?- 鍒犻櫎鎸夐挳浣嶄簬琛屽熬锛屼娇鐢?text/error 鍥炬爣鎸夐挳銆?- `760px` 浠ヤ笅蹇呴』绾靛悜鍫嗗彔銆?- 鏂板鎸夐挳浣嶄簬鍒楄〃涔嬪悗锛屼娇鐢?outlined銆?
## 鍒嗗尯

闀胯〃鍗曚娇鐢?`.section-title` 鍜?`v-divider` 鍒嗙粍銆傚垎鍖烘爣棰樻弿杩伴鍩燂紝濡傗€滆繍琛屾椂鈥濃€滅綉缁溾€濃€滃嚟鎹€濓紝涓嶈兘鍙啓妯＄硦鐨勨€滃叾浠栤€濄€?
## 甯冨皵鎺т欢

- `v-switch` 鐢ㄤ簬绔嬪嵆琛ㄨ揪鍚敤/绂佺敤鎴栨ā寮忓垏鎹€?- `v-checkbox` 鐢ㄤ簬纭銆佹壒閲忛€夋嫨鎴栭檮鍔犻€夐」銆?- 鍗遍櫓纭澶嶉€夋蹇呴』閰嶅悎璇存槑鏂囨湰锛屼笉鑳藉彧鏄剧ず鎺т欢銆?
## 鏍￠獙涓庢彁浜?
- 瀛楁閿欒鐢?Vuetify 杈撳叆缁勪欢鏄剧ず銆?- 琛ㄥ崟绾ч敊璇娇鐢?`v-alert type="error" variant="tonal"`锛岀疆浜庤〃鍗曢《閮ㄦ垨鐩稿叧鍒嗗尯涓婃柟銆?- 鎻愪氦鎸夐挳浣跨敤 `:loading`銆?- 鎻愪氦鏈熼棿鎸変笟鍔￠渶瑕佺鐢ㄧ浉鍏冲瓧娈垫垨鍏朵粬鍐茬獊鍔ㄤ綔銆?- 淇濆瓨鎴愬姛浣跨敤 Snackbar锛屼笉鍦ㄨ〃鍗曚腑姘镐箙鍫嗙Н鎴愬姛 Alert銆?
## 鏃犻殰纰?
- 涓嶇Щ闄ら粯璁ゆ爣绛惧叧鑱斿拰閿洏鎿嶄綔銆?- 鍥炬爣灏鹃儴鎿嶄綔蹇呴』鏈夊彲璁块棶鍚嶇О銆?- 閿欒淇℃伅涓嶈兘鍙€氳繃绾㈣壊杈规琛ㄨ揪銆?- 琛ㄥ崟鎻愪氦搴旀敮鎸佸師鐢?`submit` 鏃朵紭鍏堜娇鐢?`v-form @submit.prevent`銆?
## 绂佸繉

- 涓嶅湪鍚屼竴鍖哄煙娣风敤 filled銆乻olo 鍜?outlined銆?- 涓嶇敤 placeholder 浣滀负鍞竴瀛楁璇存槑銆?- 涓嶄负绱у噾鑰岄殣钘忎粛闇€瑕佸睍绀虹殑鏍￠獙淇℃伅銆?- 涓嶈鍙屽垪琛ㄥ崟鍦ㄧ獎灞忎繚鎸佸浐瀹氫袱鍒椼€?
## 婧愮爜渚濇嵁

- `web/src/styles/main.css`
- `web/src/views/settings/_shared/SettingsPageContent.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/dns/domains/index.vue`


