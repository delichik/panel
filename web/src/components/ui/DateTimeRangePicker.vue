<script setup lang="ts">
import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight } from '@lucide/vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue';
import Button from './Button.vue';
import TimeSegmentInput from './TimeSegmentInput.vue';
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

const fromDraft = ref<Date | null>(null);
const toDraft = ref<Date | null>(null);
const viewMonth = ref<Date>(startOfMonth(new Date()));
const hoverDay = ref<Date | null>(null);

const weekdayFormatter = new Intl.DateTimeFormat(undefined, { weekday: 'narrow' });
const monthFormatter = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'long' });
const weekdayLabels = Array.from({ length: 7 }, (_, index) => weekdayFormatter.format(new Date(2024, 0, index + 1)));

const emptyLabel = computed(() => t('components.dateTimeRange.empty'));
const triggerText = computed(() => {
  const from = props.modelValue.from ? formatShort(props.modelValue.from) : emptyLabel.value;
  const to = props.modelValue.to ? formatShort(props.modelValue.to) : emptyLabel.value;
  return `${from} – ${to}`;
});

const buttonClasses = computed(() => [
  'motion-field flex h-9 w-full min-w-0 items-center justify-between gap-2 rounded-xl border border-input bg-card px-3 text-left text-sm text-foreground transition-colors',
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card',
  open.value && 'border-border-strong bg-popover shadow-[var(--panel-motion-shadow-raised)]',
]);


function startOfMonth(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), 1);
}

function addMonths(date: Date, delta: number): Date {
  return new Date(date.getFullYear(), date.getMonth() + delta, 1);
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function daysInMonth(month: Date): number {
  return new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate();
}

function monthCells(month: Date): Array<Date | null> {
  const firstWeekday = (new Date(month.getFullYear(), month.getMonth(), 1).getDay() + 6) % 7;
  const cells: Array<Date | null> = [];
  for (let index = 0; index < firstWeekday; index += 1) cells.push(null);
  for (let day = 1; day <= daysInMonth(month); day += 1) cells.push(new Date(month.getFullYear(), month.getMonth(), day));
  while (cells.length % 7 !== 0) cells.push(null);
  return cells;
}

function sameDay(left: Date | null, right: Date | null): boolean {
  if (!left || !right) return false;
  return left.getFullYear() === right.getFullYear() && left.getMonth() === right.getMonth() && left.getDate() === right.getDate();
}

function formatShort(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const pad = (value: number) => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

// 选完起止日期后，开始时间自动归零、结束时间自动到当天最后一秒；用户可再手动调时间。
function pickDay(day: Date) {
  if (!fromDraft.value || (fromDraft.value && toDraft.value)) {
    fromDraft.value = new Date(day.getFullYear(), day.getMonth(), day.getDate(), 0, 0, 0);
    toDraft.value = null;
    return;
  }
  if (day < fromDraft.value) {
    fromDraft.value = new Date(day.getFullYear(), day.getMonth(), day.getDate(), 0, 0, 0);
    return;
  }
  toDraft.value = new Date(day.getFullYear(), day.getMonth(), day.getDate(), 23, 59, 59);
}

// 只高亮 from..to（或 from..hover 预览）之间的日期；比较一律用当天零点。
function inRange(day: Date): boolean {
  if (!fromDraft.value) return false;
  const end = toDraft.value ?? hoverDay.value;
  if (!end) return false;
  const start = startOfDay(fromDraft.value).getTime();
  const endAt = startOfDay(end).getTime();
  const dayAt = startOfDay(day).getTime();
  return dayAt >= Math.min(start, endAt) && dayAt <= Math.max(start, endAt);
}

function timeNum(side: 'from' | 'to', part: 'hour' | 'minute' | 'second'): number {
  const date = side === 'from' ? fromDraft.value : toDraft.value;
  if (!date) return 0;
  return part === 'hour' ? date.getHours() : part === 'minute' ? date.getMinutes() : date.getSeconds();
}

function setTimeNum(side: 'from' | 'to', part: 'hour' | 'minute' | 'second', value: number) {
  setTime(side, part, String(value));
}

function setTime(side: 'from' | 'to', part: 'hour' | 'minute' | 'second', value: string) {
  const date = side === 'from' ? fromDraft.value : toDraft.value;
  if (!date) return;
  const next = new Date(date);
  if (part === 'hour') next.setHours(Number(value));
  else if (part === 'minute') next.setMinutes(Number(value));
  else next.setSeconds(Number(value));
  if (side === 'from') {
    fromDraft.value = next;
    if (toDraft.value && next > toDraft.value) toDraft.value = new Date(next);
  } else {
    toDraft.value = next;
    if (fromDraft.value && next < fromDraft.value) fromDraft.value = new Date(next);
  }
}

function syncDrafts() {
  fromDraft.value = props.modelValue.from ? new Date(props.modelValue.from) : null;
  toDraft.value = props.modelValue.to ? new Date(props.modelValue.to) : null;
  viewMonth.value = startOfMonth(fromDraft.value ?? new Date());
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
    from: fromDraft.value?.toISOString(),
    to: toDraft.value?.toISOString(),
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
  const panelWidth = Math.min(584, window.innerWidth - viewportPadding * 2);
  const availableBelow = window.innerHeight - rect.bottom - gap - viewportPadding;
  const availableAbove = rect.top - gap - viewportPadding;
  const placeAbove = availableBelow < 380 && availableAbove > availableBelow;
  panelStyle.value = {
    position: 'fixed',
    left: `${Math.max(viewportPadding, Math.min(rect.left, window.innerWidth - panelWidth - viewportPadding))}px`,
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
          class="z-50 max-h-[calc(100dvh-32px)] w-[min(584px,calc(100vw-32px))] overflow-y-auto rounded-xl border border-border bg-popover p-4 text-popover-foreground shadow-xl"
          :style="panelStyle"
          role="dialog"
          :aria-label="t('components.dateTimeRange.title')"
          @keydown.esc.prevent="closePanel"
        >
          <p class="m-0 mb-3 text-sm font-semibold text-foreground">{{ t('components.dateTimeRange.title') }}</p>

          <div class="grid grid-cols-2 gap-4 max-md:grid-cols-1">
            <div v-for="month in [viewMonth, addMonths(viewMonth, 1)]" :key="month.toISOString()" class="rounded-xl border border-border p-2">
              <div class="mb-1 flex items-center justify-between px-1">
                <button v-if="month === viewMonth" type="button" class="rounded-lg p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" :aria-label="t('components.dateTimeRange.previousMonth')" @click="viewMonth = addMonths(viewMonth, -1)">
                  <ChevronLeft class="size-4" aria-hidden="true" />
                </button>
                <span v-else class="w-6" />
                <span class="text-sm font-medium text-foreground">{{ monthFormatter.format(month) }}</span>
                <button v-if="month !== viewMonth" type="button" class="rounded-lg p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" :aria-label="t('components.dateTimeRange.nextMonth')" @click="viewMonth = addMonths(viewMonth, 1)">
                  <ChevronRight class="size-4" aria-hidden="true" />
                </button>
                <span v-else class="w-6" />
              </div>
              <div class="grid grid-cols-7 text-center text-[10px] text-muted-foreground">
                <span v-for="(label, index) in weekdayLabels" :key="index" class="py-1">{{ label }}</span>
              </div>
              <div class="grid grid-cols-7 gap-0.5 text-center text-xs" @mouseleave="hoverDay = null">
                <template v-for="(cell, index) in monthCells(month)" :key="index">
                  <span v-if="!cell" class="py-1" />
                  <button
                    v-else
                    type="button"
                    class="rounded-lg py-1 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
                    :class="[
                      sameDay(cell, fromDraft) || sameDay(cell, toDraft) ? 'bg-primary font-semibold text-primary-foreground' : '',
                      inRange(cell) && !sameDay(cell, fromDraft) && !sameDay(cell, toDraft) ? 'bg-brand-bg text-brand' : '',
                      !sameDay(cell, fromDraft) && !sameDay(cell, toDraft) && !inRange(cell) ? 'text-foreground hover:bg-accent' : '',
                    ]"
                    @mouseenter="hoverDay = cell"
                    @click="pickDay(cell)"
                  >
                    {{ cell.getDate() }}
                  </button>
                </template>
              </div>
            </div>
          </div>

          <div class="mt-3 flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
            <div class="flex items-start gap-1.5">
              <span class="mt-2 text-xs font-medium text-muted-foreground">{{ t('components.dateTimeRange.from') }}</span>
              <TimeSegmentInput :model-value="timeNum('from', 'hour')" :min="0" :max="23" :label="t('components.dateTimeRange.hour')" @update:model-value="setTimeNum('from', 'hour', $event)" />
              <span class="mt-2 text-xs text-muted-foreground">:</span>
              <TimeSegmentInput :model-value="timeNum('from', 'minute')" :min="0" :max="59" :label="t('components.dateTimeRange.minute')" @update:model-value="setTimeNum('from', 'minute', $event)" />
              <span class="mt-2 text-xs text-muted-foreground">:</span>
              <TimeSegmentInput :model-value="timeNum('from', 'second')" :min="0" :max="59" :label="t('components.dateTimeRange.second')" @update:model-value="setTimeNum('from', 'second', $event)" />
            </div>
            <span class="mt-2 text-sm text-muted-foreground" aria-hidden="true">–</span>
            <div class="flex items-start gap-1.5">
              <span class="mt-2 text-xs font-medium text-muted-foreground">{{ t('components.dateTimeRange.to') }}</span>
              <TimeSegmentInput :model-value="timeNum('to', 'hour')" :min="0" :max="23" :label="t('components.dateTimeRange.hour')" @update:model-value="setTimeNum('to', 'hour', $event)" />
              <span class="mt-2 text-xs text-muted-foreground">:</span>
              <TimeSegmentInput :model-value="timeNum('to', 'minute')" :min="0" :max="59" :label="t('components.dateTimeRange.minute')" @update:model-value="setTimeNum('to', 'minute', $event)" />
              <span class="mt-2 text-xs text-muted-foreground">:</span>
              <TimeSegmentInput :model-value="timeNum('to', 'second')" :min="0" :max="59" :label="t('components.dateTimeRange.second')" @update:model-value="setTimeNum('to', 'second', $event)" />
            </div>
          </div>
          <div class="mt-3 flex flex-wrap gap-2 border-t border-border pt-3">
            <button type="button" class="rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" @click="applyPreset(24)">{{ t('components.dateTimeRange.last24h') }}</button>
            <button type="button" class="rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" @click="applyPreset(168)">{{ t('components.dateTimeRange.last7d') }}</button>
            <button type="button" class="rounded-lg border border-border px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" @click="applyPreset(720)">{{ t('components.dateTimeRange.last30d') }}</button>
          </div>
          <div class="mt-4 flex justify-end gap-2">
            <Button size="sm" variant="secondary" @click="cancel">{{ t('common.cancel') }}</Button>
            <Button size="sm" variant="primary" @click="apply">{{ t('common.apply') }}</Button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>