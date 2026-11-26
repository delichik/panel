<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import Button from './Button.vue';
import Checkbox from './Checkbox.vue';
import Dialog from './Dialog.vue';

const props = withDefaults(defineProps<{
  open: boolean;
  title: string;
  description?: string;
  impact?: string;
  tone?: 'neutral' | 'warning' | 'danger';
  confirmLabel: string;
  cancelLabel: string;
  loading?: boolean;
  /** Blocks confirmation for callers that gate the action (for example references still exist). */
  confirmDisabled?: boolean;
  requireCheckbox?: boolean;
  checkboxLabel: string;
}>(), {
  tone: 'warning',
});

const emit = defineEmits<{
  'update:open': [value: boolean];
  confirm: [];
  cancel: [];
}>();

const checked = ref(false);
const canConfirm = computed(() => !props.loading && !props.confirmDisabled && (!props.requireCheckbox || checked.value));
const confirmVariant = computed(() => props.tone === 'danger' ? 'danger' : 'primary');
/** 影响说明是 InfoBar 语义（4px 圆角、语义 bg/border），neutral 映射到 info。 */
const infoBarTone = computed(() => (props.tone === 'danger' ? 'danger' : props.tone === 'warning' ? 'warning' : 'info'));

watch(() => props.open, (open) => {
  if (!open) checked.value = false;
});

function close() {
  if (props.loading) return;
  emit('update:open', false);
  emit('cancel');
}

function handleOpenChange(value: boolean) {
  if (!value && props.loading) return;
  emit('update:open', value);
}
</script>

<template>
  <Dialog :open="open" :title="title" :description="description" @update:open="handleOpenChange">
    <div class="grid gap-4">
      <InfoBar v-if="impact" :tone="infoBarTone">{{ impact }}</InfoBar>
      <slot />
      <div v-if="requireCheckbox" class="flex items-start gap-3 surface-control-line bg-layer p-3 text-body text-foreground">
        <Checkbox v-model="checked" :disabled="loading" :label="checkboxLabel" />
      </div>
    </div>
    <template #footer>
      <Button variant="secondary" :disabled="loading" @click="close">{{ cancelLabel }}</Button>
      <Button :variant="confirmVariant" :loading="loading" :disabled="!canConfirm" @click="$emit('confirm')">
        {{ confirmLabel }}
      </Button>
    </template>
  </Dialog>
</template>
