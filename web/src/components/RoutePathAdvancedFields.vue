<script setup lang="ts">
import { computed } from 'vue';
import AppActionButton from '@/components/AppActionButton.vue';
import type { HTTPHeaderDto, HTTPRouteOptionsDto } from '@/types/api';
import { useI18n } from '@/i18n';

const props = withDefaults(defineProps<{
  modelValue?: HTTPRouteOptionsDto;
  proxy?: boolean;
  gzip?: boolean;
}>(), {
  modelValue: () => ({}),
  proxy: false,
  gzip: false,
});

const emit = defineEmits<{ (event: 'update:modelValue', value: HTTPRouteOptionsDto): void }>();
const { t } = useI18n();
const modeOptions = computed(() => [
  { title: t('routePathOptions.inherit'), value: 'inherit' },
  { title: t('common.enabled'), value: 'on' },
  { title: t('common.disabled'), value: 'off' },
]);
const websocketOptions = computed(() => [
  { title: t('routePathOptions.websocketAuto'), value: 'auto' },
  { title: t('common.enabled'), value: 'on' },
  { title: t('common.disabled'), value: 'off' },
]);

function current(): HTTPRouteOptionsDto {
  return {
    gzipMode: props.modelValue?.gzipMode || 'inherit',
    clientMaxBodySizeMb: props.modelValue?.clientMaxBodySizeMb || 0,
    connectTimeoutSeconds: props.modelValue?.connectTimeoutSeconds || 0,
    readTimeoutSeconds: props.modelValue?.readTimeoutSeconds || 0,
    sendTimeoutSeconds: props.modelValue?.sendTimeoutSeconds || 0,
    bufferingMode: props.modelValue?.bufferingMode || 'inherit',
    webSocketMode: props.modelValue?.webSocketMode || 'off',
    requestHeaders: [...(props.modelValue?.requestHeaders ?? [])].map((item) => ({ ...item })),
    responseHeaders: [...(props.modelValue?.responseHeaders ?? [])].map((item) => ({ ...item })),
  };
}

function update(patch: Partial<HTTPRouteOptionsDto>) {
  emit('update:modelValue', { ...current(), ...patch });
}

function addHeader(kind: 'requestHeaders' | 'responseHeaders') {
  update({ [kind]: [...(current()[kind] ?? []), { name: '', value: '' }] });
}

function updateHeader(kind: 'requestHeaders' | 'responseHeaders', index: number, field: keyof HTTPHeaderDto, value: string) {
  const items = [...(current()[kind] ?? [])].map((item) => ({ ...item }));
  items[index][field] = value;
  update({ [kind]: items });
}

function removeHeader(kind: 'requestHeaders' | 'responseHeaders', index: number) {
  update({ [kind]: (current()[kind] ?? []).filter((_, itemIndex) => itemIndex !== index) });
}
</script>

<template>
  <div class="route-options">
    <div class="route-options__title">{{ t('routePathOptions.title') }}</div>
    <div class="route-options__grid">
      <v-select
        v-if="gzip"
        :model-value="current().gzipMode"
        :items="modeOptions"
        :label="t('routePathOptions.gzip')"
        variant="outlined"
        density="compact"
        hide-details="auto"
        @update:model-value="update({ gzipMode: String($event) })"
      />
      <template v-if="proxy">
        <v-text-field :model-value="current().clientMaxBodySizeMb" type="number" min="0" max="10240" :label="t('routePathOptions.clientMaxBodySize')" suffix="MB" variant="outlined" density="compact" hide-details="auto" @update:model-value="update({ clientMaxBodySizeMb: Number($event || 0) })" />
        <v-text-field :model-value="current().connectTimeoutSeconds" type="number" min="0" max="300" :label="t('routePathOptions.connectTimeout')" suffix="s" variant="outlined" density="compact" hide-details="auto" @update:model-value="update({ connectTimeoutSeconds: Number($event || 0) })" />
        <v-text-field :model-value="current().readTimeoutSeconds" type="number" min="0" max="3600" :label="t('routePathOptions.readTimeout')" suffix="s" variant="outlined" density="compact" hide-details="auto" @update:model-value="update({ readTimeoutSeconds: Number($event || 0) })" />
        <v-text-field :model-value="current().sendTimeoutSeconds" type="number" min="0" max="3600" :label="t('routePathOptions.sendTimeout')" suffix="s" variant="outlined" density="compact" hide-details="auto" @update:model-value="update({ sendTimeoutSeconds: Number($event || 0) })" />
        <v-select :model-value="current().bufferingMode" :items="modeOptions" :label="t('routePathOptions.buffering')" variant="outlined" density="compact" hide-details="auto" @update:model-value="update({ bufferingMode: String($event) })" />
        <v-select :model-value="current().webSocketMode" :items="websocketOptions" :label="t('applicationEditor.websocket')" variant="outlined" density="compact" hide-details="auto" @update:model-value="update({ webSocketMode: String($event) })" />
      </template>
    </div>

    <div v-if="proxy" class="route-options__headers">
      <div class="route-options__heading">
        <span>{{ t('routePathOptions.requestHeaders') }}</span>
        <AppActionButton icon="mdi-plus" :label="t('routePathOptions.addHeader')" @click="addHeader('requestHeaders')" />
      </div>
      <div v-for="(header, index) in current().requestHeaders" :key="`request-${index}`" class="route-options__header-row">
        <v-text-field :model-value="header.name" :label="t('routePathOptions.headerName')" variant="outlined" density="compact" hide-details="auto" @update:model-value="updateHeader('requestHeaders', index, 'name', String($event ?? ''))" />
        <v-text-field :model-value="header.value" :label="t('routePathOptions.headerValue')" variant="outlined" density="compact" hide-details="auto" @update:model-value="updateHeader('requestHeaders', index, 'value', String($event ?? ''))" />
        <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="removeHeader('requestHeaders', index)" />
      </div>
    </div>

    <div class="route-options__headers">
      <div class="route-options__heading">
        <span>{{ t('routePathOptions.responseHeaders') }}</span>
        <AppActionButton icon="mdi-plus" :label="t('routePathOptions.addHeader')" @click="addHeader('responseHeaders')" />
      </div>
      <div v-for="(header, index) in current().responseHeaders" :key="`response-${index}`" class="route-options__header-row">
        <v-text-field :model-value="header.name" :label="t('routePathOptions.headerName')" variant="outlined" density="compact" hide-details="auto" @update:model-value="updateHeader('responseHeaders', index, 'name', String($event ?? ''))" />
        <v-text-field :model-value="header.value" :label="t('routePathOptions.headerValue')" variant="outlined" density="compact" hide-details="auto" @update:model-value="updateHeader('responseHeaders', index, 'value', String($event ?? ''))" />
        <AppActionButton kind="danger" icon="mdi-delete-outline" :label="t('common.delete')" @click="removeHeader('responseHeaders', index)" />
      </div>
    </div>
    <div class="text-caption text-medium-emphasis">{{ t('routePathOptions.noGatewayCache') }}</div>
  </div>
</template>

<style scoped>
.route-options { display: grid; gap: 12px; padding-top: 14px; border-top: 1px solid color-mix(in srgb, var(--lp-border), transparent 30%); }
.route-options__title { font-weight: 700; }
.route-options__grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.route-options__headers { display: grid; gap: 8px; }
.route-options__heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; font-weight: 600; }
.route-options__header-row { display: grid; grid-template-columns: minmax(140px, .7fr) minmax(220px, 1.3fr) auto; align-items: start; gap: 8px; }
@media (max-width: 760px) {
  .route-options__grid,
  .route-options__header-row { grid-template-columns: 1fr; }
}
</style>
