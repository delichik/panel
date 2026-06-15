# 瀵硅瘽妗?
## 鏍囧噯缁撴瀯

瀵硅瘽妗嗙粺涓€浣跨敤鍏ㄥ眬缁撴瀯绫伙細

```html
<v-dialog v-model="open" width="560">
  <v-card class="app-dialog-card">
    <v-card-title class="app-dialog-title">
      <span class="app-dialog-title-text">...</span>
      <v-btn icon="mdi-close" variant="text" ... />
    </v-card-title>
    <v-divider />
    <v-card-text class="app-dialog-body">...</v-card-text>
    <v-divider />
    <v-card-actions class="app-dialog-actions">...</v-card-actions>
  </v-card>
</v-dialog>
```

## 灏哄

| 绫诲瀷 | 寤鸿瀹藉害 |
| --- | --- |
| 绠€鍗曠‘璁?| `420px` 宸﹀彸 |
| 鏅€氳〃鍗?| `560px` 鑷?`640px` |
| 澶嶆潅閰嶇疆 | `720px` 鑷?`900px` |
| 澶у瀷缂栬緫鍣?| 绾?`1180px`锛屼絾浠嶅彈瑙嗗彛闄愬埗 |

鍏ㄥ眬 overlay 闄愬埗涓鸿鍙ｅ楂樺噺 `24px`锛屽苟淇濈暀 `12px` 澶栬竟璺濄€?
## 鏍囬

- 鏍囬鍖烘渶灏忛珮搴?`64px`銆?- 鏍囬鏂囧瓧绾?`1.05rem`銆侀珮瀛楅噸銆?- 鏍囬蹇呴』鑳芥埅鏂垨鎹㈣锛屼笉鑳借鍏抽棴鎸夐挳鎸ゅ嚭銆?- 鍏抽棴鎸夐挳浣跨敤 text/icon锛屽苟鎻愪緵鍙闂悕绉般€?
## 鍐呭

- `.app-dialog-body` 榛樿 `24px` 鍐呰竟璺濄€?- 鏈€澶ч珮搴︿负 `min(68vh, 720px)`锛岃秴鍑烘椂鍐呭鍖哄唴閮ㄦ粴鍔ㄣ€?- 琛ㄥ崟绾ч敊璇斁鍦ㄥ唴瀹归《閮ㄣ€?- 澶у瀷缂栬緫鍣ㄥ彲鎵╁睍鍐呴儴甯冨眬锛屼絾涓嶄慨鏀瑰叏灞€ overlay 灏哄绾︽潫銆?
## 鎿嶄綔鍖?
- 鎿嶄綔鎸夐挳闈犲彸銆?- 鍙栨秷浣跨敤 `variant="text"`銆?- 淇濆瓨鎴栫‘璁や娇鐢?`primary flat`銆?- 鍒犻櫎纭浣跨敤 `error flat`銆?- 鎸夐挳鏈€灏忓搴?`92px`銆?- `600px` 浠ヤ笅鎸夐挳绾靛悜鍗犳弧鏁磋銆?
## 纭瀵硅瘽妗?
- 鏍囬鏄庣‘鍔ㄤ綔瀵硅薄锛屼緥濡傗€滃垹闄ゅ煙鍚嶁€濓紝閬垮厤鍙啓鈥滅‘璁も€濄€?- 姝ｆ枃璇存槑鍚庢灉鍜屽璞″悕绉般€?- 楂橀闄╂搷浣滃彲澧炲姞 warning/error tonal Alert 鎴栫‘璁ゅ閫夋銆?- 鍙栨秷蹇呴』淇濇寔鍙涓斾笉寮卞寲鍒伴毦浠ュ彂鐜般€?- 鍒犻櫎瀹屾垚鍓嶄娇鐢ㄦ寜閽?loading锛岄槻姝㈤噸澶嶆彁浜ゃ€?
## 琛ㄥ崟瀵硅瘽妗?
- 琛ㄥ崟瀛楁閬靛畧 [forms.md](forms.md)銆?- 鍏抽棴瀵硅瘽妗嗘椂娓呯悊涓存椂閿欒鍜屼笉搴斾繚鐣欑殑鑽夌鐘舵€併€?- 缂栬緫鍜屾柊寤哄彲鍏辩敤瀵硅瘽妗嗭紝浣嗘爣棰樸€佷富瑕佹寜閽拰鍒濆鍊煎繀椤绘槑纭垏鎹€?
## 绂佸繉

- 涓嶇洿鎺ヤ娇鐢ㄨ８ `v-card-title/text/actions` 鑰岀粫杩囧叏灞€缁撴瀯绫汇€?- 涓嶈鏁翠釜椤甸潰鍦ㄥ璇濇鎵撳紑鍚庢壙鎷呭璇濇鍐呭婊氬姩銆?- 涓嶅湪涓€涓璇濇鍐呯户缁墦寮€灏哄鐩歌繎鐨勭浜屽眰缂栬緫瀵硅瘽妗嗐€?- 涓嶆妸 Snackbar 鐢ㄤ綔闇€瑕佺敤鎴风‘璁ょ殑瀵硅瘽妗嗘浛浠ｅ搧銆?
## 婧愮爜渚濇嵁

- `web/src/styles/main.css`
- `web/src/views/dns/domains/index.vue`
- `web/src/views/servers/_shared/ServersPageContent.vue`
- `web/src/views/runtime/applications/ApplicationEditor.vue`
- `web/src/views/certificates/key-assets/index.vue`

