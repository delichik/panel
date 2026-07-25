<script setup lang="ts">
import { AlertTriangle, Info, ShieldAlert } from '@lucide/vue';
import { computed, ref, watch } from 'vue';
import Button from './Button.vue';
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
const canConfirm = computed(() => !props.loading && (!props.requireCheckbox || checked.value));
const confirmVariant = computed(() => props.tone === 'danger' ? 'danger' : 'primary');
const iconToneClass = computed(() => {
  if (props.tone === 'danger') return 'border-danger-border bg-danger-bg text-danger';
  if (props.tone === 'warning') return 'border-warning-border bg-warning-bg text-warning';
  return 'border-info-border bg-info-bg text-info';
});

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
      <div v-if="impact" class="flex gap-3 rounded-xl border p-3" :class="iconToneClass">
        <div class="grid size-9 shrink-0 place-items-center rounded-lg border border-current/20 bg-background/60">
          <ShieldAlert v-if="tone === 'danger'" class="size-4" aria-hidden="true" />
          <AlertTriangle v-else-if="tone === 'warning'" class="size-4" aria-hidden="true" />
          <Info v-else class="size-4" aria-hidden="true" />
        </div>
        <p class="m-0 text-sm leading-6">{{ impact }}</p>
      </div>
      <label v-if="requireCheckbox" class="flex items-start gap-3 rounded-xl border border-border bg-muted/30 p-3 text-sm text-foreground">
        <input v-model="checked" class="mt-1 size-4 rounded border-input accent-primary" type="checkbox" :disabled="loading" />
        <span>{{ checkboxLabel }}</span>
      </label>
    </div>
    <template #footer>
      <Button variant="secondary" :disabled="loading" @click="close">{{ cancelLabel }}</Button>
      <Button :variant="confirmVariant" :loading="loading" :disabled="!canConfirm" @click="$emit('confirm')">
        {{ confirmLabel }}
      </Button>
    </template>
  </Dialog>
</template>
