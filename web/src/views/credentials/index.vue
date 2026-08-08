<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { Cable, KeyRound, Plus, RefreshCcw, Trash2, Wrench } from '@lucide/vue';
import { credentialsApi } from '@/api/credentials';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { CredentialDetailDto, CredentialDto, CredentialInput, CredentialType } from '@/types/credentials';
import type { ServerDto } from '@/types/servers';
import { credentialReferences } from '@/views/servers/model';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { secretPayload, validateCredentialInput } from './model';

const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const credentials = ref<CredentialDto[]>([]);
const servers = ref<ServerDto[]>([]);
const selectedId = ref('');
const credentialDetail = ref<CredentialDetailDto | null>(null);
const detailLoading = ref(false);
const search = ref('');
const page = ref(1);
const pageSize = 50;
const total = ref(0);
let searchTimer: ReturnType<typeof setTimeout> | null = null;
const listRequests = createLatestRequestGuard();
const loading = ref(false);
const error = ref('');
const actionError = ref('');
const dialogOpen = ref(false);
const confirmOpen = ref(false);
const saving = ref(false);
const testing = ref(false);
const editing = ref<CredentialDto | null>(null);
const deleteTarget = ref<CredentialDto | null>(null);

const form = reactive<CredentialInput>({
  name: '',
  type: 'password',
  username: '',
  password: '',
  privateKey: '',
  passphrase: '',
});

const selectedCredential = computed(() => credentials.value.find((item) => item.id === selectedId.value) ?? null);
const references = computed(() => selectedCredential.value ? credentialReferences(selectedCredential.value.id, servers.value) : []);
const deleteReferences = computed(() => deleteTarget.value ? credentialReferences(deleteTarget.value.id, servers.value) : []);
const validation = computed(() => validateCredentialInput(form, Boolean(editing.value)));
const privateKeyModel = computed({
  get: () => form.privateKey ?? '',
  set: (value: string) => { form.privateKey = value; },
});
const typeOptions = computed(() => [
  { value: 'password', label: t('credentialsPage.password') },
  { value: 'private_key', label: t('credentialsPage.privateKey') },
]);

async function load() {
  const requestId = listRequests.begin();
  loading.value = true;
  error.value = '';
  try {
    const [credentialsResult, serversResult] = await Promise.allSettled([
      credentialsApi.listPage({ page: page.value, pageSize, q: search.value.trim() || undefined }),
      serversApi.list(),
    ]);
    if (!listRequests.isCurrent(requestId)) return;
    let firstError = '';
    if (credentialsResult.status === 'fulfilled') {
      credentials.value = credentialsResult.value.items;
      total.value = credentialsResult.value.total;
      selectedId.value = selectedId.value && credentialsResult.value.items.some((item) => item.id === selectedId.value) ? selectedId.value : credentialsResult.value.items[0]?.id || '';
      if (selectedId.value) {
        void loadDetail(selectedId.value);
      } else {
        credentialDetail.value = null;
      }
    } else {
      firstError = credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('credentialsPage.loadFailed');
    }
    if (serversResult.status === 'fulfilled') {
      servers.value = serversResult.value;
    } else {
      if (!firstError) firstError = serversResult.reason instanceof Error ? serversResult.reason.message : t('credentialsPage.loadFailed');
    }
    if (firstError) {
      error.value = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('credentialsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.loadFailed'));
  } finally {
    if (listRequests.isCurrent(requestId)) loading.value = false;
  }
}

async function loadDetail(id: string) {
  if (!id) {
    credentialDetail.value = null;
    detailLoading.value = false;
    return;
  }
  detailLoading.value = true;
  try {
    const detail = await credentialsApi.get(id);
    if (selectedId.value === id) credentialDetail.value = detail;
  } catch {
    if (selectedId.value === id) credentialDetail.value = null;
  } finally {
    if (selectedId.value === id) detailLoading.value = false;
  }
}

function openCreate() {
  editing.value = null;
  Object.assign(form, { name: '', type: 'password', username: '', password: '', privateKey: '', passphrase: '' });
  actionError.value = '';
  dialogOpen.value = true;
}

function openEdit(credential: CredentialDto) {
  editing.value = credential;
  Object.assign(form, { name: credential.name, type: credential.type, username: credential.username, password: '', privateKey: '', passphrase: '' });
  actionError.value = '';
  dialogOpen.value = true;
}

async function saveCredential() {
  if (Object.keys(validation.value).length) return;
  saving.value = true;
  actionError.value = '';
  try {
    const payload = secretPayload(form, Boolean(editing.value));
    const saved = editing.value ? await credentialsApi.update(editing.value.id, payload) : await credentialsApi.create(payload);
    selectedId.value = saved.id;
    notifySuccess(t(editing.value ? 'credentialsPage.updated' : 'credentialsPage.created'));
    dialogOpen.value = false;
    await load();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('credentialsPage.saveFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.saveFailed'));
  } finally {
    saving.value = false;
  }
}

function confirmDelete(credential: CredentialDto) {
  deleteTarget.value = credential;
  confirmOpen.value = true;
}

async function deleteCredential() {
  const target = deleteTarget.value;
  if (!target) return;
  actionError.value = '';
  try {
    await credentialsApi.delete(target.id);
    notifySuccess(t('credentialsPage.deleted'));
    confirmOpen.value = false;
    await load();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('credentialsPage.deleteFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.deleteFailed'));
  }
}

async function testCredential(credential: CredentialDto) {
  const ref = credentialReferences(credential.id, servers.value)[0];
  if (!ref) return;
  testing.value = true;
  actionError.value = '';
  try {
    await serversApi.test(ref.id);
    notifySuccess(t('credentialsPage.testSucceeded', { name: ref.name }));
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('credentialsPage.testFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.testFailed'));
  } finally {
    testing.value = false;
  }
}

function typeTone(type: CredentialType) {
  return type === 'private_key' ? 'info' : 'warning';
}

watch(search, () => { if (searchTimer) clearTimeout(searchTimer); searchTimer = setTimeout(() => { if (page.value !== 1) page.value = 1; else void load(); }, 250); });
watch(page, () => { void load(); });
watch(selectedId, (id) => { void loadDetail(id); });
onMounted(load);
onBeforeUnmount(() => { if (searchTimer) clearTimeout(searchTimer); });
</script>

<template>
  <ConsolePage :title="t('routes.credentials.title')" :description="t('routes.credentials.description')">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" variant="primary" @click="openCreate"><Plus />{{ t('credentialsPage.addCredential') }}</Button>
    </template>

    <MasterDetailLayout class="h-full min-h-[640px]">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_minmax(0,1fr)_auto] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <SearchInput v-model="search" clearable :placeholder="t('credentialsPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
        </div>
        <div class="min-h-0 overflow-auto p-2">
          <div v-if="loading && !credentials.length" class="grid gap-2">
            <Skeleton v-for="item in 6" :key="item" class="h-20" />
          </div>
          <EmptyState v-else-if="error && !credentials.length" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <EmptyState v-else-if="!credentials.length" :title="t('credentialsPage.noCredentials')" :description="t('credentialsPage.noCredentialsHint')" />
          <button
            v-for="credential in credentials"
            v-else
            :key="credential.id"
            type="button"
            class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent"
            :class="selectedId === credential.id ? 'border-border-strong bg-background' : 'border-transparent bg-transparent'"
            :aria-current="selectedId === credential.id ? 'true' : undefined"
            @click="selectedId = credential.id"
          >
            <div class="flex items-center justify-between gap-2">
              <strong class="truncate text-sm text-foreground">{{ credential.name }}</strong>
              <Badge :tone="typeTone(credential.type)">{{ credential.type === 'private_key' ? t('credentialsPage.privateKey') : t('credentialsPage.password') }}</Badge>
            </div>
            <span class="text-xs text-muted-foreground">{{ credential.username }}</span>
            <span class="text-xs text-muted-foreground">{{ t('credentialsPage.referenceCount', { count: credentialReferences(credential.id, servers).length }) }}</span>
          </button>
        </div>
        <PaginationBar v-model:page="page" class="px-3" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <EmptyState v-if="!selectedCredential" :title="t('credentialsPage.selectCredential')" :description="t('credentialsPage.selectCredentialHint')" />
        <article v-else class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="flex items-start justify-between gap-4 border-b border-border p-5 max-md:grid">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="m-0 truncate text-xl font-semibold text-foreground">{{ selectedCredential.name }}</h2>
                <Badge :tone="typeTone(selectedCredential.type)">{{ selectedCredential.type === 'private_key' ? t('credentialsPage.privateKey') : t('credentialsPage.password') }}</Badge>
              </div>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedCredential.username }}</p>
            </div>
            <div class="flex flex-wrap justify-end gap-2">
              <Button size="sm" :disabled="references.length === 0" :loading="testing" @click="testCredential(selectedCredential)"><Cable />{{ t('credentialsPage.test') }}</Button>
              <Button size="sm" @click="openEdit(selectedCredential)"><Wrench />{{ t('common.edit') }}</Button>
              <Button size="sm" variant="danger" @click="confirmDelete(selectedCredential)"><Trash2 />{{ t('common.delete') }}</Button>
            </div>
          </header>

          <div class="min-h-0 overflow-auto p-5">
            <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
              <section class="rounded-2xl border border-border bg-background p-4">
                <h3 class="flex items-center gap-2 text-sm font-semibold text-foreground"><KeyRound class="size-4 text-muted-foreground" />{{ t('credentialsPage.keySummary') }}</h3>
                <div v-if="detailLoading" class="mt-4 grid gap-2">
                  <Skeleton v-for="item in 3" :key="item" class="h-10" />
                </div>
                <div v-else-if="selectedCredential.type === 'private_key' && credentialDetail?.keySummary" class="mt-4 grid gap-2 text-sm">
                  <div class="flex items-center justify-between gap-3 rounded-xl border border-border p-3">
                    <span class="text-muted-foreground">{{ t('credentialsPage.keyName') }}</span>
                    <strong class="truncate text-foreground">{{ credentialDetail.keySummary.comment || selectedCredential.name }}</strong>
                  </div>
                  <div class="flex items-center justify-between gap-3 rounded-xl border border-border p-3">
                    <span class="text-muted-foreground">{{ t('credentialsPage.algorithm') }}</span>
                    <strong class="text-foreground">{{ credentialDetail.keySummary.algorithm }}<template v-if="credentialDetail.keySummary.bits"> / {{ credentialDetail.keySummary.bits }}</template></strong>
                  </div>
                  <div class="flex items-center justify-between gap-3 rounded-xl border border-border p-3">
                    <span class="text-muted-foreground">{{ t('credentialsPage.fingerprint') }}</span>
                    <strong class="break-all text-right text-foreground">{{ credentialDetail.keySummary.fingerprint }}</strong>
                  </div>
                </div>
                <p v-else-if="selectedCredential.type === 'private_key'" class="m-0 mt-4 text-sm text-muted-foreground">{{ t('credentialsPage.keySummaryUnavailable') }}</p>
                <p v-else class="m-0 mt-4 text-sm text-muted-foreground">{{ t('credentialsPage.passwordCredentialSummary', { username: selectedCredential.username }) }}</p>
              </section>
              <aside class="rounded-2xl border border-border bg-background p-4">
                <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('credentialsPage.references') }}</h3>
                <div v-if="references.length" class="mt-3 grid gap-2">
                  <div v-for="ref in references" :key="ref.id" class="rounded-xl border border-border p-3">
                    <strong class="block truncate text-sm text-foreground">{{ ref.name }}</strong>
                    <span class="text-xs text-muted-foreground">{{ ref.host }}</span>
                  </div>
                </div>
                <p v-else class="m-0 mt-3 text-sm text-muted-foreground">{{ t('credentialsPage.noReferences') }}</p>
              </aside>
            </div>
          </div>
        </article>
      </main>
      </template>
    </MasterDetailLayout>

    <Dialog v-model:open="dialogOpen" :size="form.type === 'private_key' ? 'large' : 'default'" :title="editing ? t('credentialsPage.editCredential') : t('credentialsPage.createCredential')" :description="editing ? t('credentialsPage.editDescription') : t('credentialsPage.createDescription')" :close-label="t('common.close')">
      <div v-if="form.type === 'password'" class="grid gap-4">
        <label class="grid gap-1 text-sm">{{ t('credentialsPage.name') }}<Input v-model="form.name" :invalid="Boolean(validation.name)" /></label>
        <label class="grid gap-1 text-sm">{{ t('credentialsPage.type') }}<Select v-model="form.type" :options="typeOptions" /></label>
        <label class="grid gap-1 text-sm">{{ t('credentialsPage.username') }}<Input v-model="form.username" :invalid="Boolean(validation.username)" /></label>
        <label class="grid gap-1 text-sm">
          {{ t('credentialsPage.password') }}
          <Input v-model="form.password" type="password" :placeholder="editing ? t('credentialsPage.leaveSecretBlank') : ''" :invalid="Boolean(validation.password)" />
        </label>
        <div v-if="editing" class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t('credentialsPage.blankSecretKeepsCurrent') }}</div>
        <div v-if="Object.values(validation).length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
          {{ t(Object.values(validation)[0] || 'credentialsPage.validationGeneric') }}
        </div>
      </div>
      <div v-else class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-3">
        <div class="grid gap-3 md:grid-cols-2">
          <label class="grid gap-1 text-sm">{{ t('credentialsPage.name') }}<Input v-model="form.name" :invalid="Boolean(validation.name)" /></label>
          <label class="grid gap-1 text-sm">{{ t('credentialsPage.type') }}<Select v-model="form.type" :options="typeOptions" /></label>
          <label class="grid gap-1 text-sm">{{ t('credentialsPage.username') }}<Input v-model="form.username" :invalid="Boolean(validation.username)" /></label>
          <label class="grid gap-1 text-sm">{{ t('credentialsPage.passphrase') }}<Input v-model="form.passphrase" type="password" /></label>
        </div>
        <label class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-1 text-sm">
          {{ t('credentialsPage.privateKey') }}
          <CodeEditor v-model="privateKeyModel" language="plain" :editor-label="t('credentialsPage.privateKey')" :invalid="Boolean(validation.privateKey)" />
        </label>
        <div class="grid gap-2">
          <div v-if="editing" class="rounded-xl border border-info-border bg-info-bg p-3 text-sm text-info">{{ t('credentialsPage.blankSecretKeepsCurrent') }}</div>
          <div v-if="Object.values(validation).length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(Object.values(validation)[0] || 'credentialsPage.validationGeneric') }}</div>
        </div>
      </div>
      <template #footer>
        <Button variant="secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="saving" :disabled="Boolean(Object.keys(validation).length)" @click="saveCredential">{{ editing ? t('common.save') : t('common.create') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="confirmOpen" :title="t('credentialsPage.deleteCredential')" :description="deleteTarget ? t('credentialsPage.deleteDescription', { name: deleteTarget.name }) : ''" :close-label="t('common.close')">
      <div v-if="deleteReferences.length" class="grid gap-2 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
        <strong>{{ t('credentialsPage.deleteBlocked') }}</strong>
        <span v-for="ref in deleteReferences" :key="ref.id">{{ ref.name }} / {{ ref.host }}</span>
      </div>
      <p v-else class="m-0 text-sm text-muted-foreground">{{ t('credentialsPage.deleteSafe') }}</p>
      <template #footer>
        <Button variant="secondary" @click="confirmOpen = false">{{ t('common.cancel') }}</Button>
        <Button variant="danger" :disabled="deleteReferences.length > 0" @click="deleteCredential">{{ t('common.delete') }}</Button>
      </template>
    </Dialog>
  </ConsolePage>
</template>
