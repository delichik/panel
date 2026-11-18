<script setup lang="ts">
/**
 * WinUI 3 SettingsCard 行：左侧标题/说明（可选图标），右侧控件（#control），
 * 或标题下方的整行内容（默认插槽 / #description 追加说明）。
 * `#hint` 用于字段级 hint/校验错误，统一显示在控件下方。
 * 行内只放内容，不再包第二层卡片；间距与表面由 SettingsSection 的卡片承担。
 */
withDefaults(defineProps<{
  title?: string;
  description?: string;
  /** 视觉禁用：降低整行不透明度；实际控件仍需由调用方传 disabled。 */
  disabled?: boolean;
  /** 校验失败：标题转为危险色并标记 aria-invalid（控件自身也用 invalid 视觉）。 */
  invalid?: boolean;
  /** 控件在窄屏/任意宽度下都放到标题下方整行。 */
  stacked?: boolean;
}>(), {
  title: '',
  description: '',
  disabled: false,
  invalid: false,
  stacked: false,
});
</script>

<template>
  <div
    data-settings-card
    class="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 max-sm:flex-col max-sm:items-stretch"
    :class="disabled ? 'opacity-45' : undefined"
    :aria-disabled="disabled || undefined"
    :aria-invalid="invalid || undefined"
    :data-disabled="disabled || undefined"
    :data-invalid="invalid || undefined"
  >
    <div v-if="title || description || $slots.icon || $slots.description" class="flex min-w-0 flex-1 items-start gap-3">
      <span v-if="$slots.icon" class="grid size-8 shrink-0 place-items-center text-secondary-foreground" aria-hidden="true">
        <slot name="icon" />
      </span>
      <div class="grid min-w-0 gap-0.5">
        <p v-if="title" class="m-0 break-words text-body" :class="invalid ? 'text-danger' : 'text-foreground'">{{ title }}</p>
        <p v-if="description" class="m-0 break-words text-caption text-muted-foreground">{{ description }}</p>
        <slot name="description" />
      </div>
    </div>

    <div
      v-if="$slots.control || $slots.hint"
      class="flex w-full shrink-0 flex-col gap-1 sm:w-auto max-sm:w-full max-sm:items-stretch"
      :class="stacked ? 'sm:w-full sm:items-stretch' : 'sm:items-end'"
    >
      <div v-if="$slots.control" class="flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto max-sm:w-full max-sm:flex-col max-sm:items-stretch max-sm:justify-start" :class="stacked ? 'sm:w-full sm:justify-start' : undefined">
        <slot name="control" />
      </div>
      <div v-if="$slots.hint" class="w-full text-left text-caption">
        <slot name="hint" />
      </div>
    </div>

    <div v-if="$slots.default" class="w-full min-w-0">
      <slot />
    </div>
  </div>
</template>
