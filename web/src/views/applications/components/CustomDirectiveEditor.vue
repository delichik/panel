<script setup lang="ts">
import { computed } from 'vue';
import Expander from '@/components/ui/Expander.vue';
import Textarea from '@/components/ui/Textarea.vue';
import { useI18n } from '@/i18n';
import { validateCustomDirectives, type CustomDirectiveScope } from '../model';

/**
 * 页面内可复用的“自定义 nginx 指令”折叠编辑器（设施域名记录 / 应用反代对话框 /
 * 设施 http 全局参数）。只负责输入与行级校验展示；保存时由调用方再次
 * `validateCustomDirectives` 兜底，非法输入不得进入保存 payload。
 */
const props = withDefaults(defineProps<{
  modelValue: string;
  scope: CustomDirectiveScope;
  title: string;
  description?: string;
  placeholder?: string;
  disabled?: boolean;
}>(), {
  description: '',
  placeholder: '',
  disabled: false,
});

const emit = defineEmits<{ 'update:modelValue': [value: string] }>();

const { t } = useI18n();

const lines = computed(() => (props.modelValue ?? '').split(/\r?\n/));
const validation = computed(() => validateCustomDirectives(lines.value, props.scope));
const firstErrors = computed(() => validation.value.errors.slice(0, 5));
</script>

<template>
  <Expander :title="title" :description="description" :disabled="disabled">
    <div class="grid gap-2">
      <Textarea
        :model-value="modelValue"
        :placeholder="placeholder || t('applicationsPage.customDirectives.placeholder')"
        :disabled="disabled"
        :invalid="validation.errors.length > 0"
        :aria-label="title"
        class="min-h-32 font-mono"
        @update:model-value="emit('update:modelValue', $event)"
      />
      <p class="m-0 text-caption text-muted-foreground">{{ t('applicationsPage.customDirectives.hint') }}</p>
      <ul v-if="firstErrors.length" class="m-0 grid list-none gap-1 p-0 text-caption font-semibold text-danger">
        <li v-for="error in firstErrors" :key="`${error.line}-${error.key}`">
          {{ t('applicationsPage.customDirectives.lineError', { line: error.line, message: t(error.key) }) }}
        </li>
      </ul>
      <p v-else-if="validation.normalized.length" class="m-0 text-caption text-secondary-foreground">
        {{ t('applicationsPage.customDirectives.overridesHint', { count: validation.normalized.length }) }}
      </p>
    </div>
  </Expander>
</template>
