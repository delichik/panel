<script setup lang="ts">
import { useRouter } from 'vue-router';
import Button from '@/components/ui/Button.vue';
import { useI18n } from '@/i18n';
const props = defineProps<{ operationId?: string; eventId?: string; executionId?: string; resourceType?: string; resourceId?: string }>();
const router = useRouter();
const { t } = useI18n();
function open() { void router.push({ path: '/activity', query: { operationId: props.operationId || undefined, eventId: props.eventId || undefined, executionId: props.executionId || undefined, resourceType: props.resourceType || undefined, resourceId: props.resourceId || undefined } }); }
</script>
<template><Button size="sm" variant="ghost" @click="open">{{ t('activity.relatedLogs') }}</Button></template>
