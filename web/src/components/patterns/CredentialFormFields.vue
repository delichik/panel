<script setup lang="ts">
import { computed } from 'vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import { useI18n } from '@/i18n';
import type { CredentialInput } from '@/types/credentials';
import type { CredentialFormLabels } from './credentialForm';

/** 凭据表单字段（credentials 页与服务器创建弹窗共用）；文案经 labels 传入，校验错误 key 由组件翻译。 */
const props = defineProps<{
  input: CredentialInput;
  editing: boolean;
  typeChanged: boolean;
  typeOptions: Array<{ value: string; label: string }>;
  errors: Partial<Record<keyof CredentialInput, string>>;
  labels: CredentialFormLabels;
}>();

const emit = defineEmits<{ 'update:input': [value: CredentialInput] }>();
const { t } = useI18n();

function setField<K extends keyof CredentialInput>(field: K, value: CredentialInput[K]) {
  emit('update:input', { ...props.input, [field]: value });
}

const privateKeyModel = computed({
  get: () => props.input.privateKey ?? '',
  set: (value: string) => setField('privateKey', value),
});
</script>

<template>
  <div v-if="input.type === 'password'" class="grid gap-4">
    <div class="grid gap-1">
      <label class="grid gap-1 text-sm">{{ labels.name }}<Input :model-value="input.name" :invalid="Boolean(errors.name)" @update:model-value="setField('name', $event)" /></label>
      <p v-if="errors.name" class="m-0 text-sm text-danger">{{ t(errors.name) }}</p>
    </div>
    <label class="grid gap-1 text-sm">{{ labels.type }}<Select :model-value="input.type" :options="typeOptions" @update:model-value="setField('type', $event as CredentialInput['type'])" /></label>
    <div class="grid gap-1">
      <label class="grid gap-1 text-sm">{{ labels.username }}<Input :model-value="input.username" :invalid="Boolean(errors.username)" @update:model-value="setField('username', $event)" /></label>
      <p v-if="errors.username" class="m-0 text-sm text-danger">{{ t(errors.username) }}</p>
    </div>
    <div class="grid gap-1">
      <label class="grid gap-1 text-sm">
        {{ labels.password }}
        <Input :model-value="input.password ?? ''" type="password" :placeholder="editing ? labels.leaveSecretBlank : ''" :invalid="Boolean(errors.password)" @update:model-value="setField('password', $event)" />
      </label>
      <p v-if="errors.password" class="m-0 text-sm text-danger">{{ t(errors.password) }}</p>
    </div>
    <div class="grid gap-2">
      <div v-if="editing && typeChanged" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ labels.typeChangedRequiresSecret }}</div>
      <div v-else-if="editing" class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ labels.blankSecretKeepsCurrent }}</div>
    </div>
  </div>
  <div v-else class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
    <div class="grid gap-3 md:grid-cols-2">
      <div class="grid gap-1">
        <label class="grid gap-1 text-sm">{{ labels.name }}<Input :model-value="input.name" :invalid="Boolean(errors.name)" @update:model-value="setField('name', $event)" /></label>
        <p v-if="errors.name" class="m-0 text-sm text-danger">{{ t(errors.name) }}</p>
      </div>
      <label class="grid gap-1 text-sm">{{ labels.type }}<Select :model-value="input.type" :options="typeOptions" @update:model-value="setField('type', $event as CredentialInput['type'])" /></label>
      <div class="grid gap-1">
        <label class="grid gap-1 text-sm">{{ labels.username }}<Input :model-value="input.username" :invalid="Boolean(errors.username)" @update:model-value="setField('username', $event)" /></label>
        <p v-if="errors.username" class="m-0 text-sm text-danger">{{ t(errors.username) }}</p>
      </div>
      <label class="grid gap-1 text-sm">{{ labels.passphrase }}<Input :model-value="input.passphrase ?? ''" type="password" @update:model-value="setField('passphrase', $event)" /></label>
    </div>
    <div class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
      {{ labels.privateKey }}
      <CodeEditor :model-value="privateKeyModel" @update:model-value="privateKeyModel = $event" language="plain" :editor-label="labels.privateKey" :invalid="Boolean(errors.privateKey)" />
      <p v-if="errors.privateKey" class="m-0 text-sm text-danger">{{ t(errors.privateKey) }}</p>
    </div>
    <div class="grid gap-2">
      <div v-if="editing && typeChanged" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ labels.typeChangedRequiresSecret }}</div>
      <div v-else-if="editing" class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ labels.blankSecretKeepsCurrent }}</div>
    </div>
  </div>
</template>
