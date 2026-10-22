<script setup lang="ts">
import { useRouter } from 'vue-router';
import { logsPath } from '@/api/activityReference';
import Button from '@/components/ui/Button.vue';
import { useI18n } from '@/i18n';
const props = defineProps<{
  streamId?: string;
  operationId?: string;
  executionId?: string;
  resourceType?: string;
  resourceId?: string;
}>();
const router = useRouter();
const { t } = useI18n();
function open() {
  void router.push(logsPath({
    streamId: props.streamId,
    operationId: props.operationId,
    executionId: props.executionId,
    resourceType: props.resourceType,
    resourceId: props.resourceId,
    level: 'warning,error',
  }));
}
</script>
<template><Button size="sm" variant="ghost" @click="open">{{ t('intentRecords.viewLogs') }}</Button></template>
