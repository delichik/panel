<script setup lang="ts">
import { computed } from 'vue';

defineOptions({ inheritAttrs: false });

type ActionKind =
  | 'primary'
  | 'secondary'
  | 'danger'
  | 'danger-primary'
  | 'warning'
  | 'warning-primary'
  | 'plain'
  | 'snackbar'
  | 'tool';

const props = withDefaults(defineProps<{
  label: string;
  icon?: string;
  kind?: ActionKind;
  size?: 'x-small' | 'small' | 'large' | 'x-large';
  loading?: boolean;
  disabled?: boolean;
  to?: string | Record<string, unknown>;
  type?: 'button' | 'submit' | 'reset';
}>(), {
  kind: 'secondary',
  size: 'small',
  loading: false,
  disabled: false,
  to: undefined,
  type: 'button',
});

const isTool = computed(() => props.kind === 'tool');

const color = computed(() => {
  if (props.kind === 'primary') return 'primary';
  if (props.kind === 'danger' || props.kind === 'danger-primary') return 'error';
  if (props.kind === 'warning' || props.kind === 'warning-primary') return 'warning';
  if (props.kind === 'snackbar') return 'white';
  return undefined;
});

const variant = computed(() => {
  if (props.kind === 'primary' || props.kind === 'danger-primary' || props.kind === 'warning-primary') return 'flat';
  if (props.kind === 'plain' || props.kind === 'snackbar' || props.kind === 'tool') return 'text';
  return 'outlined';
});
</script>

<template>
  <v-btn
    v-if="isTool"
    v-bind="$attrs"
    class="app-action-button app-action-button--tool"
    :aria-label="label"
    :title="label"
    :icon="icon"
    :size="size"
    :variant="variant"
    :color="color"
    :loading="loading"
    :disabled="disabled"
    :to="to"
    :type="type"
  />
  <v-btn
    v-else
    v-bind="$attrs"
    class="app-action-button text-none"
    :prepend-icon="icon"
    :size="size"
    :variant="variant"
    :color="color"
    :loading="loading"
    :disabled="disabled"
    :to="to"
    :type="type"
  >
    {{ label }}
  </v-btn>
</template>
