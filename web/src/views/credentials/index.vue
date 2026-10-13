<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Cable, Eye, KeyRound, MoreHorizontal, Plus, RefreshCcw, Trash2, Wrench } from '@lucide/vue';
import { credentialsApi } from '@/api/credentials';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import CredentialFormFields from '@/components/patterns/CredentialFormFields.vue';
import { emptyCredentialInput, secretPayload, validateCredentialInput, type CredentialFormLabels } from '@/components/patterns/credentialForm';
import { useI18n } from '@/i18n';
import type { CredentialDetailDto, CredentialDto, CredentialInput, CredentialType } from '@/types/credentials';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { credentialReferences } from '@/views/servers/model';
import { createLatestRequestGuard, isAbortError, normalizePage } from '@/views/_shared/requestState';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const pageSize = 50;
const credentials = ref<CredentialDto[]>([]);
const servers = ref<ServerDto[]>([]);
const selectedId = ref(String(route.query.credential ?? ''));
const credentialDetail = ref<CredentialDetailDto | null>(null);
const detailLoading = ref(false);
const detailError = ref('');
const search = ref(String(route.query.search ?? ''));
const page = ref(normalizePage(route.query.page));
const total = ref(0);
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const listGuard = createLatestRequestGuard();
const loading = ref(false);
const error = ref('');
const actionError = ref('');
const dialogOpen = ref(false);
const saving = ref(false);
const testOpen = ref(false);
const testing = ref(false);
const testTargetId = ref('');
const testTarget = ref<CredentialDto | null>(null);
const editing = ref<CredentialDto | null>(null);
const deleteTarget = ref<CredentialDto | null>(null);
const deleting = ref(false);

const form = ref<CredentialInput>(emptyCredentialInput());

const selectedCredential = computed(() => credentials.value.find((item) => item.id === selectedId.value)
  ?? (credentialDetail.value?.id === selectedId.value ? credentialDetail.value : null));
const detailOpen = computed(() => Boolean(selectedId.value));
const references = computed(() => selectedCredential.value ? credentialReferences(selectedCredential.value.id, servers.value) : []);
const testReferences = computed(() => testTarget.value ? credentialReferences(testTarget.value.id, servers.value) : []);
const deleteReferences = computed(() => deleteTarget.value ? credentialReferences(deleteTarget.value.id, servers.value) : []);
const typeChanged = computed(() => Boolean(editing.value && editing.value.type !== form.value.type));
const validation = computed(() => validateCredentialInput(form.value, Boolean(editing.value), typeChanged.value));
const detailTitle = computed(() => selectedCredential.value?.name ?? t('common.loading'));
const credentialFormLabels = computed<CredentialFormLabels>(() => ({
  name: t('credentialsPage.name'),
  type: t('credentialsPage.type'),
  username: t('credentialsPage.username'),
  password: t('credentialsPage.password'),
  privateKey: t('credentialsPage.privateKey'),
  passphrase: t('credentialsPage.passphrase'),
  leaveSecretBlank: t('credentialsPage.leaveSecretBlank'),
  typeChangedRequiresSecret: t('credentialsPage.typeChangedRequiresSecret'),
  blankSecretKeepsCurrent: t('credentialsPage.blankSecretKeepsCurrent'),
}));
const typeOptions = computed(() => [
  { value: 'password', label: t('credentialsPage.password') },
  { value: 'private_key', label: t('credentialsPage.privateKey') },
]);
const columns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'type', label: t('credentialsPage.type'), nowrap: true },
  { key: 'username', label: t('credentialsPage.username') },
  { key: 'references', label: t('credentialsPage.references'), nowrap: true },
  { key: 'updatedAt', label: t('credentialsPage.updatedAtColumn'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

function typeLabel(type: CredentialType) {
  return type === 'private_key' ? t('credentialsPage.privateKey') : t('credentialsPage.password');
}

function typeTone(type: CredentialType) {
  return type === 'private_key' ? 'info' as const : 'warning' as const;
}

function referenceLabel(credential: CredentialDto) {
  return t('credentialsPage.referenceCount', { count: credentialReferences(credential.id, servers.value).length });
}

function openDetail(credential: CredentialDto) {
  selectedId.value = credential.id;
  void router.push({ query: { ...route.query, credential: credential.id } });
}

function closeDetail() {
  if (!detailOpen.value) return;
  selectedId.value = '';
  void router.replace({ query: { ...route.query, credential: undefined } });
}

watch(() => route.query.credential, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});

watch(selectedId, (id) => { void loadDetail(id); }, { immediate: true });

async function load() {
  const requestId = listGuard.begin();
  loading.value = true;
  error.value = '';
  try {
    const [credentialsResult, serversResult] = await Promise.allSettled([
      credentialsApi.listPage({ page: page.value, pageSize, q: search.value.trim() || undefined }),
      serversApi.list(),
    ]);
    if (!listGuard.isCurrent(requestId)) return;
    let firstError = '';
    if (credentialsResult.status === 'fulfilled') {
      credentials.value = credentialsResult.value.items;
      total.value = credentialsResult.value.total;
      if (!route.query.credential && selectedId.value && !credentialsResult.value.items.some((item) => item.id === selectedId.value)) {
        selectedId.value = '';
      }
    } else {
      firstError = credentialsResult.reason instanceof Error ? credentialsResult.reason.message : t('credentialsPage.loadFailed');
    }
    if (serversResult.status === 'fulfilled') {
      servers.value = serversResult.value;
    } else if (!firstError) {
      firstError = serversResult.reason instanceof Error ? serversResult.reason.message : t('credentialsPage.loadFailed');
    }
    if (firstError) {
      error.value = firstError;
      notifyError(firstError);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('credentialsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.loadFailed'), err);
  } finally {
    if (listGuard.isCurrent(requestId)) loading.value = false;
  }
}

async function loadDetail(id: string) {
  if (!id) {
    credentialDetail.value = null;
    detailLoading.value = false;
    detailError.value = '';
    return;
  }
  detailLoading.value = true;
  detailError.value = '';
  try {
    const detail = await credentialsApi.get(id);
    if (selectedId.value === id) {
      credentialDetail.value = detail;
      detailError.value = '';
    }
  } catch (err) {
    if (isAbortError(err)) return;
    if (selectedId.value === id) {
      credentialDetail.value = null;
      detailError.value = err instanceof Error ? err.message : t('credentialsPage.loadFailed');
    }
  } finally {
    if (selectedId.value === id) detailLoading.value = false;
  }
}

function openCreate() {
  editing.value = null;
  form.value = emptyCredentialInput();
  actionError.value = '';
  dialogOpen.value = true;
}

function openEdit(credential: CredentialDto) {
  editing.value = credential;
  form.value = { name: credential.name, type: credential.type, username: credential.username, password: '', privateKey: '', passphrase: '' };
  actionError.value = '';
  dialogOpen.value = true;
}

watch(() => form.value.type, (next, previous) => {
  if (dialogOpen.value && editing.value && next !== previous) {
    form.value.password = '';
    form.value.privateKey = '';
    form.value.passphrase = '';
  }
});

async function saveCredential() {
  if (Object.keys(validation.value).length) return;
  saving.value = true;
  actionError.value = '';
  try {
    const payload = secretPayload(form.value, Boolean(editing.value));
    const saved = editing.value ? await credentialsApi.update(editing.value.id, payload) : await credentialsApi.create(payload);
    selectedId.value = saved.id;
    notifySuccess(t(editing.value ? 'credentialsPage.updated' : 'credentialsPage.created'), saved);
    dialogOpen.value = false;
    await router.push({ query: { ...route.query, credential: saved.id } });
    await load();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('credentialsPage.saveFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.saveFailed'), err);
  } finally {
    saving.value = false;
  }
}

function askDelete(credential: CredentialDto) {
  deleteTarget.value = credential;
}

async function deleteCredential() {
  const target = deleteTarget.value;
  if (!target || deleteReferences.value.length) return;
  deleting.value = true;
  try {
    const result = await credentialsApi.delete(target.id);
    notifySuccess(t('credentialsPage.deleted'), result);
    deleteTarget.value = null;
    closeDetail();
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('credentialsPage.deleteFailed'), err);
  } finally {
    deleting.value = false;
  }
}

function openTest(credential: CredentialDto) {
  const refs = credentialReferences(credential.id, servers.value);
  if (!refs.length) return;
  testTarget.value = credential;
  testTargetId.value = refs[0]!.id;
  testOpen.value = true;
  actionError.value = '';
}

async function testCredential(credential: CredentialDto, serverId: string) {
  if (!serverId) return;
  testing.value = true;
  actionError.value = '';
  try {
    const ref = credentialReferences(credential.id, servers.value).find((item) => item.id === serverId);
    if (!ref) return;
    const result = await serversApi.test(ref.id);
    testOpen.value = false;
    notifySuccess(t('credentialsPage.testSucceeded', { name: ref.name }), result);
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('credentialsPage.testFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.testFailed'), err);
  } finally {
    testing.value = false;
  }
}

watch(search, () => {
  void router.replace({ query: { ...route.query, search: search.value || undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => { if (page.value !== 1) page.value = 1; else void load(); }, 250);
});
watch(page, () => {
  void router.replace({ query: { ...route.query, page: page.value > 1 ? page.value : undefined } });
  void load();
});
onMounted(load);
onBeforeUnmount(() => { if (searchTimer) clearTimeout(searchTimer); });
</script>

<template>
  <TableDetailLayout class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage :title="t('routes.credentials.title')" :description="t('routes.credentials.description')">
        <template #actions>
          <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Button size="sm" variant="primary" @click="openCreate"><Plus />{{ t('credentialsPage.addCredential') }}</Button>
        </template>

        <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-4 rounded-2xl border border-border bg-card p-4">
          <SearchInput v-model="search" clearable :placeholder="t('credentialsPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />

          <EmptyState v-if="error && !credentials.length" class="self-start" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>

          <Table
            v-else
            class="max-lg:max-h-[60dvh]"
            :framed="false"
            :columns="columns"
            :rows="credentials"
            row-key="id"
            :active-key="selectedId"
            :loading="loading && !credentials.length"
            :loading-label="t('common.loading')"
          >
            <template #name="{ row }">
              <strong class="block max-w-[16rem] truncate text-foreground">{{ row.name }}</strong>
            </template>
            <template #type="{ row }">
              <Badge :tone="typeTone(row.type)">{{ typeLabel(row.type) }}</Badge>
            </template>
            <template #username="{ row }">
              <span class="text-muted-foreground">{{ row.username }}</span>
            </template>
            <template #references="{ row }">
              <span class="text-muted-foreground">{{ referenceLabel(row) }}</span>
            </template>
            <template #updatedAt="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.updatedAt, t('common.never')) }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
                <Button size="sm" variant="ghost" @click="openEdit(row)"><Wrench />{{ t('common.edit') }}</Button>
                <Button size="sm" variant="ghost" :disabled="credentialReferences(row.id, servers).length === 0" @click="openTest(row)"><Cable />{{ t('credentialsPage.test') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <DropdownItem tone="danger" @click="askDelete(row)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState :title="t('credentialsPage.noCredentials')" :description="t('credentialsPage.noCredentialsHint')" />
            </template>
          </Table>

          <PaginationBar v-model:page="page" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
        </div>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage :title="detailTitle" :description="selectedCredential?.username || ''" :back-label="t('common.backToList')" @back="closeDetail">
        <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
          <section class="rounded-2xl border border-border bg-muted p-4">
            <h3 class="flex items-center gap-2 text-sm font-semibold text-foreground"><KeyRound class="size-4 text-muted-foreground" />{{ t('credentialsPage.keySummary') }}</h3>
            <div v-if="detailLoading" class="mt-4 grid gap-2">
              <Skeleton v-for="item in 3" :key="item" class="h-10" />
            </div>
            <EmptyState v-else-if="detailError" :title="t('common.loadFailed')" :description="detailError">
              <template #actions>
                <Button size="sm" :loading="detailLoading" @click="loadDetail(selectedId)"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
            <div v-else-if="selectedCredential?.type === 'private_key' && credentialDetail?.keySummary" class="mt-4 grid gap-2 text-sm">
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
            <p v-else-if="selectedCredential?.type === 'private_key'" class="m-0 mt-4 text-sm text-muted-foreground">{{ t('credentialsPage.keySummaryUnavailable') }}</p>
            <p v-else-if="selectedCredential" class="m-0 mt-4 text-sm text-muted-foreground">{{ t('credentialsPage.passwordCredentialSummary', { username: selectedCredential.username }) }}</p>
          </section>
          <aside class="rounded-2xl border border-border bg-muted p-4">
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
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <Dialog v-model:open="dialogOpen" :size="form.type === 'private_key' ? 'large' : 'default'" :title="editing ? t('credentialsPage.editCredential') : t('credentialsPage.createCredential')" :description="editing ? t('credentialsPage.editDescription') : t('credentialsPage.createDescription')" :close-label="t('common.close')">
    <div class="grid gap-4" :class="form.type === 'private_key' ? 'h-full min-h-0' : ''">
      <CredentialFormFields
        v-model:input="form"
        :editing="Boolean(editing)"
        :type-changed="typeChanged"
        :type-options="typeOptions"
        :errors="validation"
        :labels="credentialFormLabels"
      />
      <div v-if="Object.keys(validation).length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
        {{ t(Object.values(validation)[0] || 'credentialsPage.validationGeneric') }}
      </div>
      <div v-if="actionError" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ actionError }}</div>
    </div>
    <template #footer>
      <Button variant="secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="Boolean(Object.keys(validation).length)" @click="saveCredential">{{ editing ? t('common.save') : t('common.create') }}</Button>
    </template>
  </Dialog>

  <Dialog v-model:open="testOpen" :title="t('credentialsPage.test')" :description="testTarget ? t('credentialsPage.testDescription', { name: testTarget.name }) : ''" :close-label="t('common.close')">
    <div class="grid gap-2">
      <p class="m-0 text-sm text-muted-foreground">{{ t('credentialsPage.testTargetHint') }}</p>
      <button v-for="ref in testReferences" :key="ref.id" type="button" class="grid gap-1 rounded-xl border border-border p-3 text-left hover:bg-accent" :class="testTargetId === ref.id ? 'border-border-strong bg-card' : ''" @click="testTargetId = ref.id">
        <strong class="truncate text-sm text-foreground">{{ ref.name }}</strong>
        <span class="text-xs text-muted-foreground">{{ ref.host }}</span>
      </button>
    </div>
    <template #footer>
      <Button variant="secondary" @click="testOpen = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :disabled="!testTargetId" :loading="testing" @click="testTarget && testCredential(testTarget, testTargetId)"><Cable />{{ t('credentialsPage.test') }}</Button>
    </template>
  </Dialog>

  <ConfirmDialog
    :open="Boolean(deleteTarget)"
    :title="t('credentialsPage.deleteCredential')"
    :description="deleteTarget ? t('credentialsPage.deleteDescription', { name: deleteTarget.name }) : ''"
    :impact="deleteReferences.length ? `${t('credentialsPage.deleteBlocked')} ${deleteReferences.map((ref) => `${ref.name} / ${ref.host}`).join('; ')}` : t('credentialsPage.deleteSafe')"
    :tone="deleteReferences.length ? 'warning' : 'danger'"
    :confirm-disabled="deleteReferences.length > 0"
    :loading="deleting"
    :confirm-label="t('common.delete')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @update:open="(value) => { if (!value) deleteTarget = null; }"
    @confirm="deleteCredential"
    @cancel="deleteTarget = null"
  />
</template>
