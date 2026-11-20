<script setup lang="ts">
import InfoBar from '@/components/patterns/InfoBar.vue';
import { useI18n } from '@/i18n';
import type { Diagnostic } from '@/types/applications';

/**
 * 应用/设施配置诊断列表：每条诊断一枚语义 InfoBar（error=danger / warning=warning），
 * 诊断 details（domain/path/assetName/serverId）以追加文本展示，便于定位具体记录。
 * 纯展示组件：诊断数据由页面加载，组件只负责渲染与文案。
 */
withDefaults(defineProps<{
  items: Diagnostic[];
  /** 无诊断时的说明；缺省用页面既有 noDiagnostics 文案。 */
  emptyText?: string;
}>(), {
  emptyText: '',
});

const { t } = useI18n();

function diagnosticDetailText(item: Diagnostic): string {
  const details = item.details ?? {};
  const parts: string[] = [];
  const domain = typeof details.domain === 'string' ? details.domain : '';
  const path = typeof details.path === 'string' ? details.path : '';
  const assetName = typeof details.assetName === 'string' ? details.assetName : '';
  const serverId = typeof details.serverId === 'string' ? details.serverId : '';
  if (domain) parts.push(`${t('applicationsPage.domain')}: ${domain}`);
  if (path) parts.push(`${t('common.path')}: ${path}`);
  if (assetName) parts.push(`${t('applicationsPage.assetReferenceName')}: ${assetName}`);
  if (serverId) parts.push(`${t('applicationsPage.server')}: ${serverId}`);
  return parts.length ? ` — ${parts.join(' · ')}` : '';
}
</script>

<template>
  <div class="grid gap-2 p-4">
    <InfoBar
      v-for="item in items"
      :key="`${item.code}-${item.field ?? ''}`"
      :tone="item.severity === 'error' ? 'danger' : 'warning'"
    >
      {{ item.field ? `${item.field}: ` : '' }}{{ item.message }}{{ diagnosticDetailText(item) }}
    </InfoBar>
    <p v-if="!items.length" class="m-0 text-caption text-muted-foreground">{{ emptyText || t('applicationsPage.noDiagnostics') }}</p>
  </div>
</template>
