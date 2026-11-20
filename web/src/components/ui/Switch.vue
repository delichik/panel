<script setup lang="ts">
import { focusRing } from './states';

defineProps<{ modelValue: boolean; disabled?: boolean; label: string }>();
defineEmits<{ 'update:modelValue': [value: boolean] }>();
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :disabled="disabled"
    class="group inline-grid min-h-control max-lg:min-h-touch w-10 shrink-0 place-items-center rounded-control disabled:pointer-events-none"
    :class="focusRing"
    @click="$emit('update:modelValue', !modelValue)"
  >
    <!-- WinUI 3 ToggleSwitch：40×20 胶囊轨道 + 12px 圆滑块；命中区至少 32px（窄屏 40px）。
         PointerOver/Pressed 只改变轨道/滑块的填充与描边（167/83ms），无位移/缩放动画（滑块位移除外）。 -->
    <span
      class="relative block h-5 w-10 rounded-full border transition-fill duration-fast ease-move group-active:duration-fade"
      :class="disabled
        ? 'border-stroke bg-muted'
        : modelValue
          ? 'border-transparent bg-primary group-hover:bg-accent-strong group-active:bg-accent-stronger'
          : 'border-border-strong bg-control group-hover:bg-hover group-active:bg-pressed'"
    >
      <span
        class="absolute left-1 top-1 size-3 rounded-full transition-transform-fill duration-fast ease-enter"
        :class="[
          modelValue ? 'translate-x-5' : 'translate-x-0',
          disabled ? 'bg-muted-foreground' : modelValue ? 'bg-primary-foreground' : 'bg-foreground',
        ]"
      />
    </span>
  </button>
</template>
