<script setup lang="ts">
import AppSelectorItem from '@/components/AppSelectorItem.vue';

withDefaults(defineProps<{
  title: string;
  subtitle?: string;
  selected?: boolean;
  as?: 'button' | 'div';
  status?: 'info' | 'success' | 'warning' | 'error' | 'grey' | false;
}>(), {
  subtitle: '',
  selected: false,
  as: 'button',
  status: 'info',
});

const emit = defineEmits<{
  select: [];
}>();
</script>

<template>
  <AppSelectorItem :selected="selected" :as="as" @select="emit('select')">
    <span class="app-selector-summary min-width-0">
      <span v-if="$slots.leading" class="app-selector-summary__leading">
        <slot name="leading" />
      </span>
      <span v-if="status" class="app-selector-summary__status" :class="`app-selector-summary__status--${status}`" />
      <span class="app-selector-summary__copy min-width-0">
        <span class="app-selector-summary__title text-truncate">{{ title }}</span>
        <span v-if="subtitle" class="app-selector-summary__subtitle text-truncate">{{ subtitle }}</span>
      </span>
    </span>
    <slot />
  </AppSelectorItem>
</template>

<style scoped>
.app-selector-summary {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.app-selector-summary__leading {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
}

.app-selector-summary__status {
  flex: 0 0 auto;
  width: 9px;
  height: 9px;
  border-radius: 999px;
  background: rgb(var(--v-theme-info));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-info), 0.12);
}

.app-selector-summary__status--success {
  background: rgb(var(--v-theme-success));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-success), 0.12);
}

.app-selector-summary__status--warning {
  background: rgb(var(--v-theme-warning));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-warning), 0.14);
}

.app-selector-summary__status--error {
  background: rgb(var(--v-theme-error));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-error), 0.12);
}

.app-selector-summary__status--grey {
  background: rgb(var(--v-theme-on-surface-variant));
  box-shadow: 0 0 0 4px rgba(var(--v-theme-on-surface), 0.04);
}

.app-selector-summary__copy,
.app-selector-summary__title,
.app-selector-summary__subtitle {
  display: block;
  min-width: 0;
}

.app-selector-summary__title {
  font-size: 0.92rem;
  font-weight: 780;
  letter-spacing: 0;
}

.app-selector-summary__subtitle {
  margin-top: 3px;
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  font-variant-numeric: tabular-nums;
}

.min-width-0 {
  min-width: 0;
}
</style>
