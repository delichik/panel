<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Cable, Eye, KeyRound, MoreHorizontal, Plus, RefreshCcw, Trash2, Wrench } from '@/icons';
import { credentialsApi } from '@/api/credentials';
import { serversApi } from '@/api/servers';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import RadioButton from '@/components/ui/RadioButton.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import DetailSection from '@/components/patterns/DetailSection.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import PropertyList from '@/components/patterns/PropertyList.vue';
import TableDetailName from '@/components/patterns/TableDetailName.vue';
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
const listLoaded = ref(false);
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
const keySummaryItems = computed(() => {
  const summary = credentialDetail.value?.keySummary;
  if (!summary) return [];
  return [
    { key: 'keyName', label: t('credentialsPage.keyName'), value: summary.comment || selectedCredential.value?.name || '' },
    { key: 'algorithm', label: t('credentialsPage.algorithm'), value: summary.bits ? `${summary.algorithm} / ${summary.bits}` : summary.algorithm },
    { key: 'fingerprint', label: t('credentialsPage.fingerprint'), value: summary.fingerprint, mono: true },
  ];
});
const referenceItems = computed(() => references.value.map((ref) => ({ key: ref.id, label: ref.name, value: ref.host })));

/** 语义化测试目标单选组：方向键在目标间移动，选中态用 accent-selected。 */
function onTestTargetKeydown(event: KeyboardEvent, index: number) {
  const targets = testReferences.value;
  if (!targets.length) return;
  let next = index;
  if (event.key === 'ArrowDown' || event.key === 'ArrowRight') next = (index + 1) % targets.length;
  else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') next = (index - 1 + targets.length) % targets.length;
  else if (event.key === 'Home') next = 0;
  else if (event.key === 'End') next = targets.length - 1;
  else return;
  event.preventDefault();
  testTargetId.value = targets[next]!.id;
  const radios = (event.currentTarget as HTMLElement | null)?.parentElement?.querySelectorAll<HTMLButtonElement>('[role="radio"]');
  radios?.[next]?.focus();
}
const typeChanged = computed(() => Boolean(editing.value && editing.value.type !== form.value.type));
const validation = computed(() => validateCredentialInput(form.value, Boolean(editing.value), typeChanged.value));
const detailTitle = computed(() => selectedCredential.value?.name ?? t('common.loading'));
const breadcrumbs = computed(() => [
  { label: t('routes.credentials.title'), to: '/credentials' },
  { label: detailTitle.value },
]);
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
  { key: 'name', label: t('common.name'), width: 'w-72' },
  { key: 'summary', label: t('credentialsPage.username'), hideBelow: 'wide' as const },
  { key: 'references', label: t('credentialsPage.references'), nowrap: true, hideBelow: 'lg' as const },
  { key: 'updatedAt', label: t('credentialsPage.updatedAtColumn'), nowrap: true, width: 'w-36', hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
/** ListPage 页级状态槽：首屏 loading / 页级 error / 真·空态走槽；筛选无结果仍由 Table #empty（inset）承担。 */
const listState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (!listLoaded.value && !error.value) return 'loading';
  if (loading.value && !credentials.value.length) return 'loading';
  if (error.value && !credentials.value.length) return 'error';
  if (!loading.value && !error.value && !credentials.value.length && !search.value.trim()) return 'empty';
  return 'ready';
});

function typeLabel(type: CredentialType) {
  return type === 'private_key' ? t('credentialsPage.privateKey') : t('credentialsPage.password');
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
    if (listGuard.isCurrent(requestId)) {
      loading.value = false;
      listLoaded.value = true;
    }
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

function setCredentialType(value: string) {
  form.value.type = value as CredentialType;
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
      <ConsolePage width="content" :title="t('routes.credentials.title')" :description="t('routes.credentials.description')">
        <ListPage :state="listState">
          <template #toolbar>
            <ListCommandBar :label="t('routes.credentials.title')">
              <template #search>
                <SearchInput v-model="search" clearable :placeholder="t('credentialsPage.searchPlaceholder')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
              </template>
              <template #actions>
                <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
                <Button size="sm" variant="primary" @click="openCreate"><Plus />{{ t('common.add') }}</Button>
              </template>
            </ListCommandBar>
          </template>

          <template #loading>
            <div class="grid gap-2" aria-busy="true">
              <Skeleton v-for="row in 6" :key="row" class="h-10 w-full" />
            </div>
          </template>

          <template #error>
            <EmptyState variant="page" :title="t('common.loadFailed')" :description="error">
              <template #actions>
                <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
          </template>

          <template #empty>
            <EmptyState variant="page" :title="t('credentialsPage.noCredentials')" :description="t('credentialsPage.noCredentialsHint')" />
          </template>

          <Table
            class="h-full max-lg:max-h-list"
            :columns="columns"
            :rows="credentials"
            row-key="id"
            :active-key="selectedId"
            :loading="loading && !credentials.length"
            :loading-label="t('common.loading')"
          >
            <template #name="{ row }">
              <TableDetailName class="max-w-64" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row)" />
              <span class="block text-xs text-muted-foreground">{{ typeLabel(row.type) }}</span>
            </template>
            <template #summary="{ row }">
              <span class="block max-w-48 truncate text-muted-foreground" :title="row.username">{{ row.username }}</span>
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
                <Button class="max-wide:hidden" size="sm" variant="ghost" :disabled="credentialReferences(row.id, servers).length === 0" @click="openTest(row)"><Cable />{{ t('credentialsPage.test') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <DropdownItem @click="openEdit(row)"><Wrench />{{ t('common.edit') }}</DropdownItem>
                  <DropdownItem :disabled="credentialReferences(row.id, servers).length === 0" @click="openTest(row)"><Cable />{{ t('credentialsPage.test') }}</DropdownItem>
                  <DropdownItem tone="danger" @click="askDelete(row)"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState
                variant="inset"
                :title="search ? t('common.filterEmpty') : t('credentialsPage.noCredentials')"
                :description="search ? t('common.filterEmptyHint') : t('credentialsPage.noCredentialsHint')"
              />
            </template>
          </Table>

          <template #pagination>
            <PaginationBar v-model:page="page" :page-size="pageSize" :total="total" :loading="loading" :previous-label="t('common.previous')" :next-label="t('common.next')" />
          </template>
        </ListPage>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage width="content" :title="detailTitle" :description="selectedCredential?.username || ''" :breadcrumbs="breadcrumbs" :back-label="t('common.backToList')" :body-scroll="false" @back="closeDetail">
        <template v-if="selectedCredential" #actions>
          <Button size="sm" variant="secondary" @click="openEdit(selectedCredential)"><Wrench />{{ t('common.edit') }}</Button>
        </template>
        <div class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1 lg:grid-cols-2">
          <DetailSection :title="t('credentialsPage.keySummary')">
            <div v-if="detailLoading" class="grid gap-2 px-4 py-3">
              <Skeleton v-for="item in 3" :key="item" class="h-10" />
            </div>
            <EmptyState v-else-if="detailError" variant="inset" :title="t('common.loadFailed')" :description="detailError">
              <template #actions>
                <Button size="sm" :loading="detailLoading" @click="loadDetail(selectedId)"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
            <PropertyList v-else-if="credentialDetail?.keySummary" :items="keySummaryItems" :columns="1" />
            <p v-else-if="selectedCredential?.type === 'private_key'" class="m-0 px-4 py-3 text-caption text-muted-foreground">{{ t('credentialsPage.keySummaryUnavailable') }}</p>
            <p v-else class="m-0 px-4 py-3 text-caption text-muted-foreground">{{ t('credentialsPage.passwordCredentialSummary', { username: selectedCredential?.username ?? '' }) }}</p>
          </DetailSection>

          <DetailSection :title="t('credentialsPage.references')">
            <PropertyList v-if="referenceItems.length" :items="referenceItems" :columns="1" />
            <EmptyState v-else variant="inset" :title="t('credentialsPage.noReferences')" />
          </DetailSection>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <Dialog v-model:open="dialogOpen" :size="form.type === 'private_key' ? 'large' : 'default'" :title="editing ? t('credentialsPage.editCredential') : t('credentialsPage.createCredential')" :description="editing ? t('credentialsPage.editDescription') : t('credentialsPage.createDescription')" :close-label="t('common.close')">
    <div class="grid gap-4" :class="form.type === 'private_key' ? 'h-full min-h-0' : ''">
      <div class="grid gap-3 sm:grid-cols-2">
        <FormField :label="credentialFormLabels.name" required :error="validation.name ? t(validation.name) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="form.name" @update:model-value="form.name = $event" />
          </template>
        </FormField>
        <FormField :label="credentialFormLabels.type">
          <template #default="{ id }">
            <Select :id="id" :model-value="form.type" :options="typeOptions" @update:model-value="setCredentialType" />
          </template>
        </FormField>
        <FormField :label="credentialFormLabels.username" required :error="validation.username ? t(validation.username) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="form.username" @update:model-value="form.username = $event" />
          </template>
        </FormField>
        <FormField v-if="form.type === 'password'" :label="credentialFormLabels.password" :hint="editing ? credentialFormLabels.leaveSecretBlank : ''" :error="validation.password ? t(validation.password) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="form.password ?? ''" type="password" @update:model-value="form.password = $event" />
          </template>
        </FormField>
        <FormField v-else :label="credentialFormLabels.passphrase">
          <template #default="{ id }">
            <Input :id="id" :model-value="form.passphrase ?? ''" type="password" @update:model-value="form.passphrase = $event" />
          </template>
        </FormField>
      </div>
      <FormField v-if="form.type === 'private_key'" :label="credentialFormLabels.privateKey" :error="validation.privateKey ? t(validation.privateKey) : ''" class="min-h-0 grid-rows-page">
        <CodeEditor :model-value="form.privateKey ?? ''" language="plain" :editor-label="credentialFormLabels.privateKey" :invalid="Boolean(validation.privateKey)" @update:model-value="form.privateKey = $event" />
      </FormField>
      <InfoBar v-if="editing && typeChanged" tone="warning">{{ credentialFormLabels.typeChangedRequiresSecret }}</InfoBar>
      <InfoBar v-else-if="editing" tone="info">{{ credentialFormLabels.blankSecretKeepsCurrent }}</InfoBar>
      <InfoBar v-if="Object.keys(validation).length" tone="warning">{{ t(Object.values(validation)[0] || 'credentialsPage.validationGeneric') }}</InfoBar>
      <InfoBar v-if="actionError" tone="danger">{{ actionError }}</InfoBar>
    </div>
    <template #footer>
      <Button variant="secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="Boolean(Object.keys(validation).length)" @click="saveCredential">{{ editing ? t('common.save') : t('common.create') }}</Button>
    </template>
  </Dialog>

  <Dialog v-model:open="testOpen" :title="t('credentialsPage.test')" :description="testTarget ? t('credentialsPage.testDescription', { name: testTarget.name }) : ''" :close-label="t('common.close')">
    <div class="grid gap-2">
      <p class="m-0 text-caption text-muted-foreground">{{ t('credentialsPage.testTargetHint') }}</p>
      <div role="radiogroup" :aria-label="t('credentialsPage.test')" class="grid gap-2">
        <div
          v-for="(ref, index) in testReferences"
          :key="ref.id"
          class="rounded-control"
          :class="testTargetId === ref.id ? 'bg-accent-selected' : undefined"
          @keydown="onTestTargetKeydown($event, index)"
        >
          <RadioButton
            :model-value="testTargetId"
            :value="ref.id"
            :label="ref.name"
            :description="ref.host"
            @update:model-value="testTargetId = String($event)"
          />
        </div>
      </div>
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
