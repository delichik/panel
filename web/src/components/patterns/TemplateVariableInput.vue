<script setup lang="ts">
import { Braces } from '@lucide/vue';
import { nextTick, ref } from 'vue';
import Button from '@/components/ui/Button.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import Input from '@/components/ui/Input.vue';
import { insertTemplateVariable, type TemplateVariableOption } from './templateVariable';

const props = defineProps<{
  modelValue: string;
  options: TemplateVariableOption[];
  insertLabel: string;
  emptyLabel: string;
  placeholder?: string;
  invalid?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();

const input = ref<InstanceType<typeof Input> | null>(null);
let selectionStart: number | null = null;
let selectionEnd: number | null = null;

function inputElement() {
  return input.value?.$el as HTMLInputElement | undefined;
}

function rememberSelection() {
  const element = inputElement();
  selectionStart = element?.selectionStart ?? null;
  selectionEnd = element?.selectionEnd ?? null;
}

async function insert(option: TemplateVariableOption) {
  const result = insertTemplateVariable(props.modelValue, option.expression, selectionStart, selectionEnd);
  emit('update:modelValue', result.value);
  selectionStart = result.cursor;
  selectionEnd = result.cursor;
  await nextTick();
  const element = inputElement();
  element?.focus();
  element?.setSelectionRange(result.cursor, result.cursor);
}
</script>

<template>
  <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 max-sm:grid-cols-1">
    <Input
      ref="input"
      :model-value="modelValue"
      :placeholder="placeholder"
      :invalid="invalid"
      @update:model-value="$emit('update:modelValue', $event)"
      @click="rememberSelection"
      @keyup="rememberSelection"
      @select="rememberSelection"
      @blur="rememberSelection"
    />
    <Dropdown align="right">
      <template #trigger>
        <Button size="sm" variant="secondary">
          <Braces class="size-4" aria-hidden="true" />{{ insertLabel }}
        </Button>
      </template>
      <DropdownItem
        v-for="option in options"
        :key="option.id"
        class="max-w-[min(28rem,calc(100vw-3rem))] items-start"
        @click="insert(option)"
      >
        <span class="grid min-w-0 gap-0.5">
          <strong class="truncate font-medium">{{ option.label }}</strong>
          <span v-if="option.description" class="truncate font-mono text-xs text-muted-foreground">{{ option.description }}</span>
        </span>
      </DropdownItem>
      <DropdownItem v-if="!options.length" disabled>{{ emptyLabel }}</DropdownItem>
    </Dropdown>
  </div>
</template>
