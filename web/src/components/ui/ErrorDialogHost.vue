<script setup lang="ts">
import { computed, provide, ref } from 'vue';
import { RouterLink } from 'vue-router';
import { activityPath } from '@/api/activityReference';
import { useI18n } from '@/i18n';
import Button from './Button.vue';
import Dialog from './Dialog.vue';
import { errorDialogKey, type ErrorDialogApi, type ErrorDialogPayload } from './errorDialog';

/**
 * 全局阻塞错误弹窗宿主：业务代码通过 `useErrorDialog()` 入队，这里一次只渲染队首，
 * 关闭后自动露出下一条。渲染与键盘/焦点/滚动锁语义全部沿用共享 `Dialog.vue`，
 * 宿主只负责队列与“查看日志”深链。
 */
const { t } = useI18n();

/** 同时排队的上限；超出的入队直接被丢弃，保证排队错误不会无限堆积。 */
const MAX_QUEUE = 5;

const queue = ref<ErrorDialogPayload[]>([]);
const current = computed<ErrorDialogPayload | undefined>(() => queue.value[0]);
const logsTo = computed(() => (current.value ? activityPath(current.value.reference) : undefined));

function push(payload: ErrorDialogPayload) {
  if (queue.value.length >= MAX_QUEUE) return;
  queue.value = [...queue.value, payload];
}

function dismiss() {
  queue.value = queue.value.slice(1);
}

provide(errorDialogKey, { push, dismiss } satisfies ErrorDialogApi);

defineExpose({ push, dismiss });

function onOpenChange(open: boolean) {
  if (!open) dismiss();
}
</script>

<template>
  <Dialog
    v-if="current"
    :open="true"
    :title="current.title"
    :description="current.description"
    @update:open="onOpenChange"
  >
    <div data-testid="error-dialog" class="min-h-0" />
    <template v-if="logsTo" #footer>
      <RouterLink
        :to="logsTo"
        class="inline-flex h-control shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-control border border-stroke bg-control px-3 text-body font-normal text-foreground transition-control duration-fast ease-move hover:bg-hover active:bg-pressed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        @click="dismiss"
      >{{ t('eventsPage.viewLogs') }}</RouterLink>
      <Button variant="primary" @click="dismiss">{{ t('common.close') }}</Button>
    </template>
    <template v-else #footer>
      <Button variant="primary" @click="dismiss">{{ t('common.close') }}</Button>
    </template>
  </Dialog>
</template>
