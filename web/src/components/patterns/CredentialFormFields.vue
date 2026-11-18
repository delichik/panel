<script setup lang="ts">
import { computed } from 'vue';
import { useI18n } from '@/i18n';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import FormField from './FormField.vue';
import InfoBar from './InfoBar.vue';
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

function errorText(key: keyof CredentialInput): string {
  const errorKey = props.errors[key];
  return errorKey ? t(errorKey) : '';
}

const privateKeyModel = computed({
  get: () => props.input.privateKey ?? '',
  set: (value: string) => setField('privateKey', value),
});
</script>

<template>
  <div v-if="input.type === 'password'" class="grid gap-4">
    <FormField :label="labels.name" :error="errorText('name')">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          :model-value="input.name"
          :aria-describedby="describedBy"
          :invalid="invalid"
          @update:model-value="setField('name', $event)"
        />
      </template>
    </FormField>
    <FormField :label="labels.type">
      <template #default="{ id, describedBy, invalid }">
        <Select
          :id="id"
          :model-value="input.type"
          :options="typeOptions"
          :aria-describedby="describedBy"
          :invalid="invalid"
          @update:model-value="setField('type', $event as CredentialInput['type'])"
        />
      </template>
    </FormField>
    <FormField :label="labels.username" :error="errorText('username')">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          :model-value="input.username"
          :aria-describedby="describedBy"
          :invalid="invalid"
          @update:model-value="setField('username', $event)"
        />
      </template>
    </FormField>
    <FormField :label="labels.password" :error="errorText('password')">
      <template #default="{ id, describedBy, invalid }">
        <Input
          :id="id"
          :model-value="input.password ?? ''"
          type="password"
          :placeholder="editing ? labels.leaveSecretBlank : ''"
          :aria-describedby="describedBy"
          :invalid="invalid"
          @update:model-value="setField('password', $event)"
        />
      </template>
    </FormField>
    <div class="grid gap-2">
      <InfoBar v-if="editing && typeChanged" tone="warning">{{ labels.typeChangedRequiresSecret }}</InfoBar>
      <InfoBar v-else-if="editing" tone="info">{{ labels.blankSecretKeepsCurrent }}</InfoBar>
    </div>
  </div>
  <div v-else class="grid h-full min-h-0 grid-rows-page-footer gap-3">
    <div class="grid gap-3 md:grid-cols-2">
      <FormField :label="labels.name" :error="errorText('name')">
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            :model-value="input.name"
            :aria-describedby="describedBy"
            :invalid="invalid"
            @update:model-value="setField('name', $event)"
          />
        </template>
      </FormField>
      <FormField :label="labels.type">
        <template #default="{ id, describedBy, invalid }">
          <Select
            :id="id"
            :model-value="input.type"
            :options="typeOptions"
            :aria-describedby="describedBy"
            :invalid="invalid"
            @update:model-value="setField('type', $event as CredentialInput['type'])"
          />
        </template>
      </FormField>
      <FormField :label="labels.username" :error="errorText('username')">
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            :model-value="input.username"
            :aria-describedby="describedBy"
            :invalid="invalid"
            @update:model-value="setField('username', $event)"
          />
        </template>
      </FormField>
      <FormField :label="labels.passphrase">
        <template #default="{ id, describedBy, invalid }">
          <Input
            :id="id"
            :model-value="input.passphrase ?? ''"
            type="password"
            :aria-describedby="describedBy"
            :invalid="invalid"
            @update:model-value="setField('passphrase', $event)"
          />
        </template>
      </FormField>
    </div>
    <FormField class="h-full min-h-0 grid-rows-page" :label="labels.privateKey" :error="errorText('privateKey')">
      <template #default="{ id, describedBy, invalid }">
        <CodeEditor
          :id="id"
          :model-value="privateKeyModel"
          :aria-describedby="describedBy"
          :invalid="invalid"
          language="plain"
          :editor-label="labels.privateKey"
          @update:model-value="privateKeyModel = $event"
        />
      </template>
    </FormField>
    <div class="grid gap-2">
      <InfoBar v-if="editing && typeChanged" tone="warning">{{ labels.typeChangedRequiresSecret }}</InfoBar>
      <InfoBar v-else-if="editing" tone="info">{{ labels.blankSecretKeepsCurrent }}</InfoBar>
    </div>
  </div>
</template>
