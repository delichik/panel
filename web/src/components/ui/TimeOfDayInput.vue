<script setup lang="ts">
import { ref, watch } from 'vue';
import { fieldFocus } from './states';

/**
 * WinUI 3 轻量 TimeOfDay 输入：两个 2 位数字片段（小时 + 分钟），组合成 `HH:MM`。
 *
 * 组件只负责“输入 -> 原始 HH:MM 字符串”：非数字字符被丢弃、每段最多两位；
 * 合法性（24 小时制、0-23 / 0-59）由父级 `isValidScheduleTime` 判定，非法输入
 * 不进入自动保存载荷（沿用设置页“本地校验失败不发请求”的约束）。
 * 失焦时补齐前导零，但不会静默钳制越界值，避免用户输入被悄悄改写。
 */
const props = withDefaults(defineProps<{
  modelValue: string;
  label?: string;
  hourLabel?: string;
  minuteLabel?: string;
  invalid?: boolean;
  disabled?: boolean;
}>(), {
  label: '',
  hourLabel: '',
  minuteLabel: '',
  invalid: false,
  disabled: false,
});

const emit = defineEmits<{ 'update:modelValue': [value: string] }>();

function split(value: string): [string, string] {
  const match = /^(\d{0,2}):?(\d{0,2})$/.exec((value ?? '').trim());
  if (!match) return ['', ''];
  return [match[1] ?? '', match[2] ?? ''];
}

const [initialHour, initialMinute] = split(props.modelValue);
const hour = ref(initialHour);
const minute = ref(initialMinute);

// 只在外部值真正变化且与本地组合值不同时同步，避免输入过程被回填打断。
// 按数值比较可避免自动保存回填把“4:0”立刻改写成“04:00”（前导零在失焦时补齐）。
watch(() => props.modelValue, (value) => {
  const [nextHour, nextMinute] = split(value);
  const sameHour = hour.value === nextHour || (hour.value !== '' && nextHour !== '' && Number(hour.value) === Number(nextHour));
  const sameMinute = minute.value === nextMinute || (minute.value !== '' && nextMinute !== '' && Number(minute.value) === Number(nextMinute));
  if (sameHour && sameMinute) return;
  hour.value = nextHour;
  minute.value = nextMinute;
});

function sanitize(raw: string): string {
  return raw.replace(/\D/g, '').slice(0, 2);
}

function emitValue() {
  emit('update:modelValue', `${hour.value}:${minute.value}`);
}

function onHourInput(event: Event) {
  hour.value = sanitize((event.target as HTMLInputElement).value);
  emitValue();
}

function onMinuteInput(event: Event) {
  minute.value = sanitize((event.target as HTMLInputElement).value);
  emitValue();
}

function padOnBlur() {
  const paddedHour = hour.value ? hour.value.padStart(2, '0') : '';
  const paddedMinute = minute.value ? minute.value.padStart(2, '0') : '';
  const changed = paddedHour !== hour.value || paddedMinute !== minute.value;
  hour.value = paddedHour;
  minute.value = paddedMinute;
  if (changed) emitValue();
}

const inputClasses = 'h-control-sm w-12 surface-control text-center text-body tabular-nums text-foreground transition-state duration-fast ease-move hover:border-border-strong disabled:cursor-not-allowed disabled:border-stroke disabled:bg-muted disabled:text-muted-foreground';

const fieldClasses = (invalid: boolean) => [
  inputClasses,
  fieldFocus,
  invalid ? 'border-danger-border focus:border-danger-border' : undefined,
];
</script>

<template>
  <div class="flex min-w-0 items-center gap-1" data-time-of-day :aria-label="label || undefined">
    <input
      type="text"
      inputmode="numeric"
      autocomplete="off"
      class="text-center"
      :class="fieldClasses(invalid)"
      :value="hour"
      :aria-label="hourLabel || label"
      :aria-invalid="invalid ? 'true' : undefined"
      :disabled="disabled"
      maxlength="2"
      @input="onHourInput"
      @blur="padOnBlur"
    />
    <span class="text-body text-muted-foreground" aria-hidden="true">:</span>
    <input
      type="text"
      inputmode="numeric"
      autocomplete="off"
      class="text-center"
      :class="fieldClasses(invalid)"
      :value="minute"
      :aria-label="minuteLabel || label"
      :aria-invalid="invalid ? 'true' : undefined"
      :disabled="disabled"
      maxlength="2"
      @input="onMinuteInput"
      @blur="padOnBlur"
    />
  </div>
</template>
