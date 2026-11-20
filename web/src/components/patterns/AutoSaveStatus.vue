<script setup lang="ts">
import { AlertTriangle, CheckCircle2, XCircle } from '@/icons';
import ProgressRing from '@/components/ui/ProgressRing.vue';
import Button from '@/components/ui/Button.vue';
import type { AutoSaveState, AutoSaveStatusLabels } from '@/composables/useDebouncedAutoSave';

/**
 * 设置分区自动保存的只读状态指示。只负责呈现状态与重试入口，
 * 不持有业务数据；文案全部由页面传入（i18n 由调用方决定）。
 */
defineProps<{
  status: AutoSaveState;
  labels: AutoSaveStatusLabels;
}>();

const emit = defineEmits<{ retry: [] }>();
</script>

<template>
  <span
    v-if="status !== 'idle'"
    data-autosave-status
    :data-autosave-state="status"
    :role="status === 'error' ? 'alert' : 'status'"
    aria-live="polite"
    class="inline-flex min-w-0 flex-wrap items-center gap-1.5 text-caption"
  >
    <template v-if="status === 'pending' || status === 'saving'">
      <ProgressRing :size="14" aria-hidden="true" />
      <span class="text-muted-foreground">{{ labels.saving }}</span>
    </template>
    <template v-else-if="status === 'saved'">
      <CheckCircle2 class="size-3.5 text-success" aria-hidden="true" />
      <span class="text-success">{{ labels.saved }}</span>
    </template>
    <template v-else-if="status === 'invalid'">
      <AlertTriangle class="size-3.5 text-danger" aria-hidden="true" />
      <span class="text-danger">{{ labels.invalid }}</span>
    </template>
    <template v-else>
      <XCircle class="size-3.5 text-danger" aria-hidden="true" />
      <span class="text-danger">{{ labels.failed }}</span>
      <Button size="sm" variant="ghost" data-autosave-retry @click="emit('retry')">{{ labels.retry }}</Button>
    </template>
  </span>
</template>
