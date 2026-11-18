<script setup lang="ts">
/**
 * 设施页统一表单字段：label + 控件 + hint/error 的 WinUI 排布。
 * 控件本身（Input/Select/ServerMultiPicker）由调用方作为默认插槽传入。
 */
withDefaults(defineProps<{
  label: string;
  hint?: string;
  error?: string;
}>(), {
  hint: '',
  error: '',
});
</script>

<template>
  <label class="grid min-w-0 gap-1">
    <span class="text-caption text-muted-foreground">{{ label }}</span>
    <slot />
    <span v-if="hint" class="text-caption text-muted-foreground">{{ hint }}</span>
    <span v-if="error" class="text-caption text-danger">{{ error }}</span>
  </label>
</template>
