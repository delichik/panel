<script setup lang="ts">
withDefaults(defineProps<{
  context?: 'page' | 'toolbar' | 'detail' | 'table' | 'section' | 'selector' | 'filter' | 'dialog' | 'snackbar' | 'inline';
  align?: 'start' | 'end';
  mobileStack?: boolean;
}>(), {
  context: 'inline',
  align: 'end',
  mobileStack: false,
});
</script>

<template>
  <div
    class="app-action-group"
    :class="[
      `app-action-group--${context}`,
      `app-action-group--${align}`,
      { 'app-action-group--mobile-stack': mobileStack },
    ]"
  >
    <slot />
  </div>
</template>

<style scoped>
.app-action-group {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
}

.app-action-group--start {
  justify-content: flex-start;
}

.app-action-group--end {
  justify-content: flex-end;
}

.app-action-group--table,
.app-action-group--selector {
  gap: 6px;
}

.app-action-group--filter {
  gap: 10px;
}

.app-action-group--dialog,
.app-action-group--snackbar {
  justify-content: flex-end;
}

.app-action-group--dialog :deep(.v-btn) {
  min-width: 92px;
  min-height: 36px;
}

.app-action-group :deep(.v-btn) {
  flex: 0 0 auto;
}

@media (max-width: 760px) {
  .app-action-group--mobile-stack {
    align-items: stretch;
    flex-direction: column;
    justify-content: flex-start;
  }

  .app-action-group--detail,
  .app-action-group--section,
  .app-action-group--toolbar,
  .app-action-group--filter,
  .app-action-group--dialog {
    justify-content: flex-start;
  }
}
</style>
