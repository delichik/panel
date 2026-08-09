<script setup lang="ts">
import { CalendarDays, ChevronDown } from '@lucide/vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue';
import Button from './Button.vue';
import { useI18n } from '@/i18n';
import type { DateTimeRangeValue } from './dateTimeRange';

const props = defineProps<{ modelValue: DateTimeRangeValue }>();
const emit = defineEmits<{ 'update:modelValue': [value: DateTimeRangeValue] }>();
const { t } = useI18n();

const open = ref(false);
const root = ref<HTMLElement | null>(null);
const button = ref<HTMLButtonElement | null>(null);
const panel = ref<HTMLElement | null>(null);
const panelId = useId();
const panelStyle = ref<Record<string, string>>({});
const fromDraft = ref('');
const toDraft = ref('');

const emptyLabel = computed(() => t('components.dateTimeRange.empty'));
const triggerText = computed(() => {
  const from = props.modelValue.from ? formatShort(props.modelValue.from) : emptyLabel.value;
  const to = props.modelValue.to ? formatShort(props.modelValue.to) : emptyLabel.value;
  return `${from} – ${to}`;
});

const buttonClasses = computed(() => [
  'motion-field flex h-9 w-full min-w-0 items-center justify-between gap-2 rounded-xl border border-input bg-background px-3 text-left text-sm text-foreground transition-colors',
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background',
  open.value && 'border-border-strong bg-popover shadow-[var(--panel-motion-shadow-raised)]',
]);

const inputClasses = 'motion-field h-9 w-full rounded-xl border border-input bg-background px-3 text-sm text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background';

function formatShort(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function toLocalInput(iso?: string): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function localInputToIso(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

function syncDrafts() {
  fromDraft.value = toLocalInput(props.modelValue.from);
  toDraft.value = toLocalInput(props.modelValue.to);
}

function togglePanel() {
  if (open.value) {
    open.value = false;
    return;
  }
  syncDrafts();
  open.value = true;
  void nextTick(updatePanelPosition);
}

function closePanel() {
  open.value = false;
}

function apply() {
  emit('update:modelValue', {
    from: localInputToIso(fromDraft.value),
    to: localInputToIso(toDraft.value),
  });
  closePanel();
}

function cancel() {
  syncDrafts();
  closePanel();
}

function applyPreset(hours: number) {
  const to = new Date();
  const from = new Date(to.getTime() - hours * 3600 * 1000);
  emit('update:modelValue', { from: from.toISOString(), to: to.toISOString() });
  closePanel();
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target as Node;
  if (root.value && !root.value.contains(target) && !panel.value?.contains(target)) closePanel();
}

function updatePanelPosition() {
  if (!open.value || !button.value) return;
  const rect = button.value.getBoundingClientRect();
  const gap = 8;
  const viewportPadding = 16;
  const availableBelow = window.innerHeight - rect.bottom - gap - viewportPadding;
  const availableAbove = rect.top - gap - viewportPadding;
  const placeAbove = availableBelow < 260 && availableAbove > availableBelow;
  panelStyle.value = {
    position: 'fixed',
    left: `${Math.min(rect.left, window.innerWidth - 320 - viewportPadding)}px`,
    top: placeAbove ? 'auto' : `${rect.bottom + gap}px`,
    bottom: placeAbove ? `${window.innerHeight - rect.top + gap}px` : 'auto',
  };
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown);
  window.addEventListener('resize', updatePanelPosition);
  window.addEventListener('scroll', updatePanelPosition, true);
});
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown);
  window.removeEventListener('resize', updatePanelPosition);
  window.removeEventListener('scroll', updatePanelPosition, true);
});
</script>

<template>
  <div ref="root" class="relative w-full min-w-0">
    <button
      ref="button"
      type="button"
      :class="buttonClasses"
      aria-haspopup="dialog"
      :aria-expanded="open"
      :aria-controls="panelId"
      @click="togglePanel"
      @keydown.esc.prevent="closePanel"
    >
      <CalendarDays class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span class="min-w-0 flex-1 truncate text-foreground">{{ triggerText }}</span>
      <ChevronDown class="size-4 shrink-0 text-muted-foreground transition-transform duration-150 ease-out" :class="open ? 'rotate-180' : undefined" aria-hidden="true" />
    </button>

    <Teleport to="body">
      <Transition name="menu">
        <div
          v-if="open"
          :id="panelId"
          ref="panel"
          class="z-50 w-80 rounded-xl border border-border bg-popover p-3 text-popover-foreground shadow-xl"
          :style="panelStyle"
          role="dialog"
          :aria-label="t('components.dateTimeRange.title')"
          @keydown.esc.prevent="closePanel"
        >
          <div class="grid grid-cols-2 gap-2">
            <label class="grid gap-1 text-xs text-muted-foreground">{{ t('components.dateTimeRange.from') }}<input v-model="fromDraft" type="datetime-local" :class="inputClasses" /></label>
            <label class="grid gap-1 text-xs text-muted-foreground">{{ t('components.dateTimeRange.to') }}<input v-model="toDraft" type="datetime-local" :class="inputClasses" /></label>
          </div>
          <div class="mt-2 flex flex-wrap gap-1.5">
            <button type="button" class="rounded-lg border border-border px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" @click="applyPreset(24)">{{ t('components.dateTimeRange.last24h') }}</button>
            <button type="button" class="rounded-lg border border-border px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" @click="applyPreset(168)">{{ t('components.dateTimeRange.last7d') }}</button>
            <button type="button" class="rounded-lg border border-border px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" @click="applyPreset(720)">{{ t('components.dateTimeRange.last30d') }}</button>
          </div>
          <div class="mt-3 flex justify-end gap-2">
            <Button size="sm" variant="secondary" @click="cancel">{{ t('common.cancel') }}</Button>
            <Button size="sm" variant="primary" @click="apply">{{ t('common.apply') }}</Button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>