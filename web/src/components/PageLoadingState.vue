<script setup lang="ts">
import { useI18n } from '@/i18n';

defineProps<{
  message?: string;
  minHeight?: string;
  compact?: boolean;
}>();

const { t } = useI18n();
</script>

<template>
  <div class="page-loading-state" :class="{ 'page-loading-state--compact': compact }" :style="{ minHeight: minHeight || '260px' }" role="status" aria-live="polite">
    <v-progress-circular indeterminate color="primary" :size="compact ? 32 : 42" :width="compact ? 3 : 4" />
    <div class="page-loading-state__text">{{ message || t('common.loading') }}</div>
    <div class="page-loading-state__skeleton" aria-hidden="true">
      <span />
      <span />
      <span />
    </div>
  </div>
</template>

<style scoped>
.page-loading-state {
  display: grid;
  place-items: center;
  align-content: center;
  gap: 14px;
  width: 100%;
  min-width: 0;
  padding: var(--lp-space-6);
  border: 1px solid var(--lp-border);
  border-radius: var(--lp-radius-sm);
  background:
    linear-gradient(180deg, color-mix(in srgb, var(--lp-surface-container), transparent 26%), var(--lp-surface));
  color: var(--lp-text-muted);
  text-align: center;
  box-shadow: var(--lp-shadow-sm);
}

.page-loading-state__text {
  font-size: 0.9rem;
  font-weight: 650;
}

.page-loading-state__skeleton {
  display: grid;
  gap: 8px;
  width: min(320px, 100%);
}

.page-loading-state--compact {
  gap: 10px;
  padding: var(--lp-space-4);
  box-shadow: none;
}

.page-loading-state--compact .page-loading-state__skeleton {
  width: min(220px, 100%);
}

.page-loading-state__skeleton span {
  height: 8px;
  border-radius: 999px;
  background: linear-gradient(
    90deg,
    color-mix(in srgb, var(--lp-surface-muted), transparent 12%),
    color-mix(in srgb, var(--lp-border), transparent 28%),
    color-mix(in srgb, var(--lp-surface-muted), transparent 12%)
  );
  background-size: 220% 100%;
  animation: loading-shimmer 1.25s ease-in-out infinite;
}

.page-loading-state__skeleton span:nth-child(2) {
  width: 82%;
  justify-self: center;
}

.page-loading-state__skeleton span:nth-child(3) {
  width: 58%;
  justify-self: center;
}

@keyframes loading-shimmer {
  0% { background-position: 100% 0; }
  100% { background-position: -100% 0; }
}

@media (prefers-reduced-motion: reduce) {
  .page-loading-state__skeleton span {
    animation: none;
  }
}
</style>
