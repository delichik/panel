<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { UploadCloud } from '@lucide/vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import Dialog from '@/components/ui/Dialog.vue';
import DownloadButton from '@/components/ui/DownloadButton.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Tabs from '@/components/ui/Tabs.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import { useErrorToast } from '@/components/ui/toast';
import type { CodeEditorLanguage } from '@/components/ui/codeEditorLanguage';
import type { AssetFileAdapter, AssetFileItem, AssetFileKind, AssetFileManagerLabels, SaveTextAssetInput, UploadAssetInput } from './assetFileManager';

const archiveAccept = '.zip,.tar,.tar.gz,.tgz,application/zip,application/x-tar,application/gzip';
const notifyError = useErrorToast();

const props = withDefaults(defineProps<{
  items: AssetFileItem[];
  adapter: AssetFileAdapter;
  labels: AssetFileManagerLabels;
  disabled?: boolean;
  showFilename?: boolean;
  languageOptions?: Array<{ label: string; value: CodeEditorLanguage }>;
  defaultLanguage?: CodeEditorLanguage;
  inferLanguage?: (name: string, filename?: string) => CodeEditorLanguage;
}>(), {
  disabled: false,
  showFilename: true,
  languageOptions: () => [],
  defaultLanguage: 'plain',
  inferLanguage: undefined,
});

const pending = ref('');
const errors = ref<Record<string, string>>({});
const assetOpen = ref(false);
type UploadMode = 'text' | 'binary' | 'archive';
const uploadMode = ref<UploadMode>('binary');
const textEditing = ref<AssetFileItem | undefined>();
const textKey = ref('');
const textLoading = ref(false);
const textSaving = ref(false);
const textError = ref('');
const textConflict = ref(false);
const textName = ref('');
const textFilename = ref('');
const filenameTouched = ref(false);
const textContent = ref('');
const textLanguage = ref<CodeEditorLanguage>(props.defaultLanguage);
const loadGeneration = ref(0);
const deleteTarget = ref<AssetFileItem>();
const deleteOpen = computed({
  get: () => Boolean(deleteTarget.value),
  set: (value: boolean) => { if (!value) deleteTarget.value = undefined; },
});

const uploadTabs = computed(() => textEditing.value
  ? [{ label: props.labels.uploadTypeText, value: 'text' }]
  : [
    { label: props.labels.uploadTypeText, value: 'text' },
    { label: props.labels.uploadTypeBinary, value: 'binary' },
    { label: props.labels.uploadTypeArchive, value: 'archive' },
  ]);
const binaryUploadKind = computed<Exclude<AssetFileItem['kind'], 'text'>>(() => uploadMode.value === 'archive' ? 'archive' : 'binary');
const assetTitle = computed(() => textEditing.value
  ? props.labels.textTitle
  : uploadMode.value === 'text' ? props.labels.newTextTitle : props.labels.uploadAssetTitle);
const textSaveDisabled = computed(() => textLoading.value || textSaving.value || Boolean(textError.value && !textContent.value) || !textName.value.trim() || (props.showFilename && !(textFilename.value.trim() || textName.value.trim())));

function itemKey(item: AssetFileItem) {
  return item.key;
}

function kindLabel(kind: AssetFileKind) {
  if (kind === 'text') return props.labels.uploadTypeText;
  if (kind === 'archive') return props.labels.uploadTypeArchive;
  return props.labels.uploadTypeBinary;
}

function firstFile(value: File | File[]) {
  return Array.isArray(value) ? value[0] : value;
}

function setError(key: string, error: unknown) {
  const message = error instanceof Error && error.message ? error.message : props.labels.operationFailed;
  notifyError(message);
  errors.value = { ...errors.value, [key]: message };
}

async function run(key: string, action: () => Promise<unknown>) {
  pending.value = key;
  errors.value = { ...errors.value, [key]: '' };
  try {
    await action();
  } catch (error) {
    setError(key, error);
  } finally {
    if (pending.value === key) pending.value = '';
  }
}

async function upload(value: File | File[], kind: Exclude<AssetFileItem['kind'], 'text'>, key: string) {
  const file = firstFile(value);
  if (!file || props.disabled) return;
  await run(key, () => props.adapter.upload({ file, kind } satisfies UploadAssetInput));
  if (!errors.value[key]) assetOpen.value = false;
}

async function replace(item: AssetFileItem, value: File | File[]) {
  const file = firstFile(value);
  if (!file || props.disabled || item.kind === 'text') return;
  const kind: Exclude<AssetFileItem['kind'], 'text'> = item.kind === 'archive' ? 'archive' : 'binary';
  await run(itemKey(item), () => props.adapter.replace(item, { file, kind }));
}

async function download(item: AssetFileItem) {
  if (props.disabled) return;
  await run(itemKey(item), () => props.adapter.download(item));
}

function selectUploadMode(value: string) {
  if (textEditing.value || !['text', 'binary', 'archive'].includes(value)) return;
  uploadMode.value = value as UploadMode;
  errors.value = { ...errors.value, '__new-asset': '' };
}

function openUpload() {
  if (props.disabled) return;
  ++loadGeneration.value;
  textEditing.value = undefined;
  textKey.value = `asset-${Date.now()}`;
  textName.value = '';
  textFilename.value = '';
  filenameTouched.value = false;
  textContent.value = '';
  textLanguage.value = props.defaultLanguage;
  textError.value = '';
  textConflict.value = false;
  textLoading.value = false;
  textSaving.value = false;
  uploadMode.value = 'binary';
  errors.value = { ...errors.value, '__new-asset': '' };
  assetOpen.value = true;
}

async function openText(item: AssetFileItem) {
  if (props.disabled || item.kind !== 'text' || item.editable === false) return;
  const generation = ++loadGeneration.value;
  textEditing.value = item;
  textKey.value = item.key;
  textName.value = item.name;
  textFilename.value = item.filename ?? item.name.split('/').at(-1) ?? item.name;
  filenameTouched.value = true;
  textContent.value = '';
  textLanguage.value = props.inferLanguage?.(item.name, item.filename) ?? props.defaultLanguage;
  textError.value = '';
  textConflict.value = false;
  textLoading.value = true;
  uploadMode.value = 'text';
  assetOpen.value = true;
  try {
    const loaded = await props.adapter.loadText(item);
    if (generation !== loadGeneration.value || !assetOpen.value) return;
    textContent.value = loaded.content;
    if (loaded.name !== undefined) textName.value = loaded.name;
    if (loaded.filename !== undefined) textFilename.value = loaded.filename;
    if (loaded.language) textLanguage.value = loaded.language;
  } catch (error) {
    if (generation === loadGeneration.value) {
      const message = error instanceof Error ? error.message : props.labels.loadFailed;
      notifyError(message);
      textError.value = message;
    }
  } finally {
    if (generation === loadGeneration.value) textLoading.value = false;
  }
}

async function saveText() {
  if (textSaveDisabled.value || props.disabled) return;
  textSaving.value = true;
  textError.value = '';
  textConflict.value = false;
  const input: SaveTextAssetInput = {
    key: textKey.value,
    item: textEditing.value,
    name: textName.value.trim(),
    filename: props.showFilename ? (textFilename.value.trim() || textName.value.trim()) : undefined,
    content: textContent.value,
    language: textLanguage.value,
  };
  try {
    await props.adapter.saveText(input);
    assetOpen.value = false;
  } catch (error) {
    const message = error instanceof Error ? error.message : props.labels.loadFailed;
    notifyError(message);
    textError.value = message;
    textConflict.value = (error as { code?: string })?.code === 'edit_session_revision_conflict';
  } finally {
    textSaving.value = false;
  }
}

watch(textName, (value) => {
  if (!textEditing.value && props.showFilename && !filenameTouched.value) {
    textFilename.value = value;
  }
});

async function reloadText() {
  if (!props.adapter.reload) return;
  textLoading.value = true;
  try {
    await props.adapter.reload();
    assetOpen.value = false;
    textError.value = '';
    textConflict.value = false;
  } catch (error) {
    const message = error instanceof Error ? error.message : props.labels.loadFailed;
    notifyError(message);
    textError.value = message;
  } finally {
    textLoading.value = false;
  }
}

function openDelete(item: AssetFileItem) {
  if (props.disabled) return;
  deleteTarget.value = item;
}

async function confirmDelete() {
  const item = deleteTarget.value;
  if (!item) return;
  await run(itemKey(item), () => props.adapter.delete(item));
  if (!errors.value[itemKey(item)]) deleteTarget.value = undefined;
}
</script>

<template>
  <section class="grid min-h-0 gap-3">
    <div class="section-heading">
      <div class="section-copy"><h3>{{ labels.title }}</h3><p>{{ labels.hint }}</p></div>
      <div class="flex flex-wrap gap-2">
        <Button size="sm" :disabled="disabled" @click="openUpload"><UploadCloud />{{ labels.uploadAsset }}</Button>
      </div>
    </div>
    <div v-for="item in items" :key="item.key" class="item-row">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2">
          <strong class="asset-name">{{ item.name }}</strong>
          <Badge tone="neutral">{{ kindLabel(item.kind) }}</Badge>
        </div>
        <span class="asset-meta">{{ item.filename ? `${item.filename} · ` : '' }}{{ item.size }} {{ labels.bytes }}</span>
      </div>
      <div class="row-actions">
        <Button v-if="item.kind === 'text' && item.editable !== false" size="sm" :disabled="pending === item.key" @click="openText(item)">{{ labels.edit }}</Button>
        <FileUploadButton v-else size="sm" :accept="item.kind === 'archive' ? archiveAccept : undefined" :loading="pending === item.key" :disabled="disabled" :label="labels.replace" @change="replace(item, $event)" />
        <DownloadButton size="sm" :loading="pending === item.key" :disabled="disabled" :label="labels.download" @click="download(item)" />
        <Button size="sm" variant="danger" :loading="pending === item.key" :disabled="disabled" @click="openDelete(item)">{{ labels.delete }}</Button>
      </div>
    </div>
    <EmptyState v-if="!items.length" :title="labels.noAssets" :description="labels.noAssetsHint" />
  </section>

  <Dialog v-model:open="assetOpen" size="large" :title="assetTitle" :close-label="labels.close">
    <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
      <div v-if="!textEditing" class="text-sm font-medium text-foreground">{{ labels.uploadType }}</div>
      <Tabs :model-value="uploadMode" :tabs="uploadTabs" @update:model-value="selectUploadMode">
        <div v-if="uploadMode === 'text'" class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-3">
          <div class="grid gap-3" :class="showFilename ? 'md:grid-cols-3' : 'md:grid-cols-2'">
            <label class="field">
              <span>{{ labels.name }}</span>
              <Input v-model="textName" />
              <span v-if="labels.nameHint" class="text-xs leading-5 text-muted-foreground">{{ labels.nameHint }}</span>
            </label>
            <label v-if="showFilename" class="field">
              <span>{{ labels.filename }}</span>
              <Input :model-value="textFilename" @update:model-value="textFilename = $event; filenameTouched = true" />
              <span v-if="labels.filenameHint" class="text-xs leading-5 text-muted-foreground">{{ labels.filenameHint }}</span>
            </label>
            <label v-if="languageOptions.length" class="field">{{ labels.language }}<Select v-model="textLanguage" :options="languageOptions" /></label>
          </div>
          <div class="relative min-h-0">
            <LoadingOverlay v-if="textLoading" :label="labels.loading" />
            <div v-else-if="textError && !textContent" class="rounded-md border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ labels.loadFailed }}</div>
            <CodeEditor v-else v-model="textContent" :language="textLanguage" :editor-label="labels.content" />
          </div>
          <div v-if="textConflict && adapter.reload" role="alert" class="row-error">
            <Button size="sm" variant="secondary" @click="reloadText">{{ labels.reload }}</Button>
          </div>
        </div>
        <div v-else class="grid min-h-0 place-items-start gap-3">
          <FileUploadButton
            size="md"
            variant="primary"
            :accept="uploadMode === 'archive' ? archiveAccept : undefined"
            :loading="pending === '__new-asset'"
            :disabled="disabled"
            :label="uploadMode === 'archive' ? labels.uploadArchive : labels.uploadFile"
            @change="upload($event, binaryUploadKind, '__new-asset')"
          />
        </div>
      </Tabs>
    </div>
    <template #footer>
      <Button variant="secondary" @click="assetOpen = false">{{ labels.cancel }}</Button>
      <Button v-if="uploadMode === 'text'" variant="primary" :loading="textSaving" :disabled="textSaveDisabled" @click="saveText">{{ labels.save }}</Button>
    </template>
  </Dialog>

  <Dialog v-if="deleteTarget" v-model:open="deleteOpen" :title="labels.deleteTitle" :description="labels.deleteDescription" :close-label="labels.close">
    <template #footer>
      <Button variant="secondary" @click="deleteTarget = undefined">{{ labels.cancel }}</Button>
      <Button variant="danger" :loading="pending === deleteTarget.key" @click="confirmDelete">{{ labels.confirmDelete }}</Button>
    </template>
  </Dialog>
</template>

<style scoped>
.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.section-copy {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
}

.section-copy h3 {
  margin: 0;
  color: var(--panel-text);
  font-size: 14px;
  font-weight: 650;
}

.section-copy p {
  margin: 0;
  color: var(--panel-text-muted);
  font-size: 0.8125rem;
  line-height: 1.5;
}

.item-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.75rem;
  align-items: center;
  min-width: 0;
  border: 1px solid var(--panel-border);
  border-radius: 0.875rem;
  background: var(--panel-bg);
  padding: 0.75rem;
  transition:
    background-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    border-color var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    box-shadow var(--panel-motion-duration-base) var(--panel-motion-ease-standard),
    transform var(--panel-motion-duration-base) var(--panel-motion-ease-standard);
}

.item-row:hover {
  border-color: color-mix(in srgb, var(--panel-border) 92%, transparent);
  background: color-mix(in srgb, var(--panel-muted) 34%, transparent);
  transform: translateY(var(--panel-motion-hover-y));
  box-shadow: var(--panel-motion-shadow-raised);
}

.asset-name {
  color: var(--panel-text);
  font-size: 0.875rem;
  font-weight: 650;
  overflow-wrap: anywhere;
}

.asset-meta {
  display: block;
  margin-top: 0.25rem;
  color: var(--panel-text-muted);
  font-size: 0.75rem;
  line-height: 1.5;
}

.row-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  justify-content: flex-end;
}

.row-error {
  grid-column: 1 / -1;
  min-width: 0;
  border: 1px solid var(--panel-danger-border);
  border-radius: 0.5rem;
  background: var(--panel-danger-bg);
  color: var(--panel-danger);
  padding: 0.5rem 0.625rem;
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}

.field {
  display: grid;
  gap: 0.35rem;
  color: var(--panel-text);
  font-size: 0.875rem;
}

.field > span {
  color: var(--panel-text-muted);
  font-size: 0.75rem;
  line-height: 1.5;
}

@media (max-width: 760px) {
  .item-row {
    grid-template-columns: 1fr;
    align-items: start;
  }

  .item-row .row-actions {
    justify-content: flex-start;
  }

  .section-heading {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
