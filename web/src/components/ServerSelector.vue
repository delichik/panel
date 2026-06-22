<script setup lang="ts">
import type { ServerDto } from '@/types/api';
import { useI18n } from '@/i18n';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import ServerSelectorItem from '@/components/ServerSelectorItem.vue';
import { usePagination } from '@/composables/usePagination';

const props = defineProps<{
  modelValue: string;
  servers: ServerDto[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();
const { t } = useI18n();
const {
  page,
  pageSize,
  total,
  pageItems: pagedServers,
} = usePagination(() => props.servers);

</script>

<template>
  <AppSelectorPanel
    class="server-selector"
    :title="t('shared.serverSelector.title')"
    :loading="loading"
    :empty="servers.length === 0"
    empty-icon="mdi-server-off"
    :empty-text="t('shared.serverSelector.empty')"
    :page="page"
    :page-size="pageSize"
    :total="total"
    @update:page="page = $event"
    @update:page-size="pageSize = $event"
  >
        <ServerSelectorItem
          v-for="server in pagedServers"
          :key="server.id"
          :server="server"
          :selected="server.id === modelValue"
          @select="emit('update:modelValue', server.id)"
        />
  </AppSelectorPanel>
</template>

<style scoped>
.server-selector {
  min-width: 0;
}
</style>
