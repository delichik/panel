<script setup lang="ts">
import CodeEditor from '@/components/ui/CodeEditor.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Input from '@/components/ui/Input.vue';

defineProps<{ open: boolean; editing: boolean; assetKey: string; name: string; filename: string; content: string; loading: boolean; saving: boolean; error: string; conflict: boolean; labels: Record<string, string> }>();
const emit = defineEmits<{
  'update:open': [value: boolean]; 'update:name': [value: string]; 'update:filename': [value: string]; 'update:content': [value: string];
  save: [payload: { assetKey: string; name: string; filename: string; content: string; contentMode: 'text'; kind: 'uploaded_file' }]; reload: [];
}>();
</script>

<template>
  <Dialog :open="open" size="large" :title="editing ? labels.editTitle : labels.newTitle" :close-label="labels.close" @update:open="emit('update:open', $event)">
    <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
      <div class="grid gap-3 md:grid-cols-2">
        <label class="field">{{ labels.name }}<Input :model-value="name" @update:model-value="emit('update:name', $event)" /></label>
        <label class="field">{{ labels.filename }}<Input :model-value="filename" @update:model-value="emit('update:filename', $event)" /></label>
      </div>
      <div class="min-h-0">
        <div v-if="loading" class="grid h-full place-items-center text-sm text-muted-foreground">{{ labels.loading }}</div>
        <CodeEditor v-else :model-value="content" language="plain" :editor-label="labels.content" @update:model-value="emit('update:content', $event)" />
      </div>
      <div v-if="error" role="alert" class="row-error">
        {{ error }}
        <Button v-if="conflict" size="sm" variant="secondary" @click="emit('reload')">{{ labels.reload }}</Button>
      </div>
    </div>
    <template #footer>
      <Button variant="secondary" @click="emit('update:open', false)">{{ labels.cancel }}</Button>
      <Button variant="primary" :loading="saving" :disabled="loading || !filename.trim()" @click="emit('save', { assetKey, name, filename, content, contentMode: 'text', kind: 'uploaded_file' })">{{ labels.save }}</Button>
    </template>
  </Dialog>
</template>
