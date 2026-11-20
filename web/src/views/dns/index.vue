<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Cloud, Eye, MoreHorizontal, Plus, RefreshCcw, Trash2, Wrench } from '@/icons';
import { dnsApi } from '@/api/dns';
import { waitForTask } from '@/api/taskWait';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import PaginationBar from '@/components/ui/PaginationBar.vue';
import SearchInput from '@/components/ui/SearchInput.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import Skeleton from '@/components/ui/Skeleton.vue';
import DetailSection from '@/components/patterns/DetailSection.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import ListCommandBar from '@/components/patterns/ListCommandBar.vue';
import PropertyList from '@/components/patterns/PropertyList.vue';
import TableDetailName from '@/components/patterns/TableDetailName.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import ListPage from '@/components/templates/ListPage.vue';
import TableDetailLayout from '@/components/templates/TableDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { DnsDomainDto, DnsRecordDto } from '@/types/dns';
import { createLatestRequestGuard, isAbortError, normalizePage } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';
import { normalizeRecordName, validateDnsDomainForm, validateDnsRecordForm, type FieldErrors } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const pageSize = 50;
const domains = ref<DnsDomainDto[]>([]);
const records = ref<DnsRecordDto[]>([]);
const recordsObservedAt = ref('');
const recordsStale = ref(false);
const recordsLastRefreshError = ref('');
const selectedId = ref(String(route.query.domain ?? ''));
const search = ref(String(route.query.search ?? ''));
const page = ref(normalizePage(route.query.page));
const totalDomains = ref(0);
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let recordsRequestId = 0;
let recordsController: AbortController | null = null;
let syncController: AbortController | null = null;

function syncTaskSignal() {
  if (!syncController || syncController.signal.aborted) syncController = new AbortController();
  return syncController.signal;
}
const listGuard = createLatestRequestGuard();
const loadingDomains = ref(false);
const listLoaded = ref(false);
const loadingRecords = ref(false);
const error = ref('');
const recordsError = ref('');
const domainDialog = ref(false);
const recordDialog = ref(false);
const saving = ref(false);
const syncingId = ref('');
const deletingDomain = ref(false);
const deletingRecord = ref(false);
const editingDomain = ref<DnsDomainDto | null>(null);
const editingRecord = ref<DnsRecordDto | null>(null);
const deleteTarget = ref<DnsDomainDto | null>(null);
const recordDeleteTarget = ref<DnsRecordDto | null>(null);

const domainForm = reactive({ name: '', provider: 'cloudflare', apiToken: '' });
const recordForm = reactive({ type: 'A', name: '@', value: '', ttl: '300', proxied: 'true' });
const domainFormErrors = reactive<FieldErrors>({});
const recordFormErrors = reactive<FieldErrors>({});
/** NumberBox 使用 number|null；DNS 表单仍保存字符串。 */
function toNumber(value: string): number | null {
  const raw = value.trim();
  if (!raw) return null;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
}
function fromNumber(value: number | null): string {
  return value === null ? '' : String(value);
}
const domainFormValid = computed(() => {
  const errors = validateDnsDomainForm(domainForm);
  const tokenOk = Boolean(editingDomain.value) || Boolean(domainForm.apiToken.trim());
  return Object.keys(errors).length === 0 && tokenOk;
});
const recordFormValid = computed(() => Object.keys(validateDnsRecordForm(recordForm)).length === 0);

const selectedDomain = computed(() => domains.value.find((item) => item.id === selectedId.value) ?? null);
const detailOpen = computed(() => Boolean(selectedId.value));
const breadcrumbs = computed(() => [
  { label: t('routes.dns.title'), to: '/dns/domains' },
  { label: selectedDomain.value?.name ?? t('common.loading') },
]);
const recordTypeOptions = ['A', 'AAAA', 'CNAME', 'TXT', 'MX'].map((value) => ({ label: value, value }));
const providerOptions = [{ label: 'Cloudflare', value: 'cloudflare' }];
const booleanOptions = [{ label: t('dnsPage.proxied'), value: 'true' }, { label: t('dnsPage.dnsOnly'), value: 'false' }];
const domainColumns = computed(() => [
  { key: 'name', label: t('common.name'), width: 'w-72' },
  { key: 'updatedAt', label: t('dnsPage.updatedAt'), nowrap: true, width: 'w-36', hideBelow: 'wide' as const },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
const recordColumns = computed(() => [
  { key: 'type', label: t('common.type'), nowrap: true },
  { key: 'name', label: t('common.name') },
  { key: 'content', label: t('dnsPage.content') },
  { key: 'ttl', label: 'TTL', nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);
/** 详情摘要：provider / 记录数 / 更新时间（短属性 ≥wide 两列并排）。 */
const domainSummaryItems = computed(() => {
  const domain = selectedDomain.value;
  if (!domain) return [];
  return [
    { key: 'provider', label: t('dnsPage.provider'), value: domain.provider },
    { key: 'records', label: t('dnsPage.records'), value: records.value.length },
    { key: 'updatedAt', label: t('dnsPage.updatedAt'), value: formatDateTime(domain.updatedAt, t('common.never')) },
  ];
});
/** ListPage 页级状态槽：首屏 loading / 页级 error / 真·空态走槽；筛选无结果仍由 Table #empty（inset）承担。 */
const listState = computed<'ready' | 'loading' | 'error' | 'empty'>(() => {
  if (!listLoaded.value && !error.value) return 'loading';
  if (loadingDomains.value && !domains.value.length) return 'loading';
  if (error.value && !domains.value.length) return 'error';
  if (!loadingDomains.value && !error.value && !domains.value.length && !search.value.trim()) return 'empty';
  return 'ready';
});

function openDetail(domain: DnsDomainDto) {
  selectedId.value = domain.id;
  void router.push({ query: { ...route.query, domain: domain.id } });
}

function closeDetail() {
  if (!detailOpen.value) return;
  selectedId.value = '';
  void router.replace({ query: { ...route.query, domain: undefined } });
}

watch(() => route.query.domain, (value) => {
  const next = String(value ?? '');
  if (next !== selectedId.value) selectedId.value = next;
});

watch(selectedId, (id, _old, onCleanup) => {
  records.value = [];
  recordsError.value = '';
  recordsObservedAt.value = '';
  recordsStale.value = false;
  recordsLastRefreshError.value = '';
  recordsController?.abort();
  recordsRequestId += 1;
  if (!id) {
    loadingRecords.value = false;
    return;
  }
  const controller = new AbortController();
  recordsController = controller;
  onCleanup(() => controller.abort());
  void loadRecords(id, controller.signal);
});

onMounted(loadDomains);

async function loadDomains() {
  const requestId = listGuard.begin();
  loadingDomains.value = true;
  error.value = '';
  try {
    const result = await dnsApi.listDomainPage({ page: page.value, pageSize, q: search.value.trim() || undefined });
    if (!listGuard.isCurrent(requestId)) return;
    domains.value = result.items;
    totalDomains.value = result.total;
    if (!route.query.domain && selectedId.value && !result.items.some((item) => item.id === selectedId.value)) {
      selectedId.value = '';
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('dnsPage.loadFailed');
  } finally {
    if (listGuard.isCurrent(requestId)) {
      loadingDomains.value = false;
      listLoaded.value = true;
    }
  }
}

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  recordsController?.abort();
  syncController?.abort();
});

async function loadRecords(domainId = selectedId.value, signal?: AbortSignal) {
  if (!domainId) return;
  const requestId = ++recordsRequestId;
  loadingRecords.value = true;
  recordsError.value = '';
  try {
    const result = await dnsApi.listRecords(domainId, signal);
    if (requestId !== recordsRequestId || selectedId.value !== domainId) return;
    records.value = result.items;
    recordsObservedAt.value = result.observedAt ?? '';
    recordsStale.value = result.stale;
    recordsLastRefreshError.value = result.lastRefreshError ?? '';
  } catch (err) {
    if (signal?.aborted || requestId !== recordsRequestId) return;
    recordsError.value = err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed');
  } finally {
    if (requestId === recordsRequestId) loadingRecords.value = false;
  }
}

async function syncRecords(domain: DnsDomainDto) {
  if (syncingId.value) return;
  syncingId.value = domain.id;
  recordsError.value = '';
  try {
    const result = await dnsApi.refreshRecords(domain.id);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: result.taskId }), result);
    await waitForTask(result.taskId, 90_000, syncTaskSignal());
    if (selectedId.value === domain.id) await loadRecords(domain.id);
  } catch (err) {
    if (isAbortError(err)) return;
    if (selectedId.value === domain.id) recordsError.value = err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed');
    notifyError(err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed'), err);
  } finally {
    syncingId.value = '';
  }
}

function clearDomainFormErrors() {
  Object.keys(domainFormErrors).forEach((key) => { delete domainFormErrors[key]; });
}

function clearRecordFormErrors() {
  Object.keys(recordFormErrors).forEach((key) => { delete recordFormErrors[key]; });
}

function openCreateDomain() {
  editingDomain.value = null;
  Object.assign(domainForm, { name: '', provider: 'cloudflare', apiToken: '' });
  clearDomainFormErrors();
  domainDialog.value = true;
}

function openEditDomain(domain: DnsDomainDto) {
  editingDomain.value = domain;
  Object.assign(domainForm, { name: domain.name, provider: 'cloudflare', apiToken: '' });
  clearDomainFormErrors();
  domainDialog.value = true;
}

async function saveDomain() {
  const localErrors = validateDnsDomainForm(domainForm);
  Object.keys(domainFormErrors).forEach((key) => { delete domainFormErrors[key]; });
  Object.assign(domainFormErrors, localErrors);
  if (Object.keys(localErrors).length || (!editingDomain.value && !domainForm.apiToken.trim())) return;
  saving.value = true;
  error.value = '';
  try {
    const input = { name: domainForm.name.trim(), provider: 'cloudflare' as const, apiToken: domainForm.apiToken || undefined };
    const saved = editingDomain.value ? await dnsApi.updateDomain(editingDomain.value.id, input) : await dnsApi.createDomain(input);
    selectedId.value = saved.id;
    notifySuccess(t(editingDomain.value ? 'dnsPage.domainUpdated' : 'dnsPage.domainCreated'), saved);
    domainDialog.value = false;
    await router.push({ query: { ...route.query, domain: saved.id } });
    await loadDomains();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    saving.value = false;
  }
}

function openCreateRecord() {
  editingRecord.value = null;
  Object.assign(recordForm, { type: 'A', name: '@', value: '', ttl: '300', proxied: 'true' });
  clearRecordFormErrors();
  recordDialog.value = true;
}

function openEditRecord(record: DnsRecordDto) {
  editingRecord.value = record;
  Object.assign(recordForm, { type: record.type, name: record.name, value: record.value, ttl: String(record.ttl), proxied: record.proxied ? 'true' : 'false' });
  clearRecordFormErrors();
  recordDialog.value = true;
}

async function saveRecord() {
  if (!selectedDomain.value) return;
  const localErrors = validateDnsRecordForm(recordForm);
  Object.keys(recordFormErrors).forEach((key) => { delete recordFormErrors[key]; });
  Object.assign(recordFormErrors, localErrors);
  if (Object.keys(localErrors).length) return;
  saving.value = true;
  recordsError.value = '';
  try {
    const input = { type: recordForm.type, name: normalizeRecordName(recordForm.name), value: recordForm.value.trim(), ttl: Number(recordForm.ttl) || 300, proxied: recordForm.proxied === 'true' };
    const result = editingRecord.value ? await dnsApi.updateRecord(selectedDomain.value.id, editingRecord.value.id, input) : await dnsApi.createRecord(selectedDomain.value.id, input);
    notifySuccess(t(editingRecord.value ? 'dnsPage.recordUpdated' : 'dnsPage.recordCreated'), result);
    recordDialog.value = false;
    await loadRecords();
  } catch (err) {
    recordsError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    saving.value = false;
  }
}

async function deleteDomain() {
  const target = deleteTarget.value;
  if (!target) return;
  deletingDomain.value = true;
  try {
    const result = await dnsApi.deleteDomain(target.id);
    notifySuccess(t('dnsPage.domainDeleted'), result);
    deleteTarget.value = null;
    closeDetail();
    await loadDomains();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    deletingDomain.value = false;
  }
}

async function deleteRecord() {
  if (!selectedDomain.value || !recordDeleteTarget.value) return;
  deletingRecord.value = true;
  try {
    const result = await dnsApi.deleteRecord(selectedDomain.value.id, recordDeleteTarget.value.id);
    notifySuccess(t('dnsPage.recordDeleted'), result);
    recordDeleteTarget.value = null;
    await loadRecords();
  } catch (err) {
    recordsError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    deletingRecord.value = false;
  }
}

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => { if (page.value !== 1) page.value = 1; else void loadDomains(); }, 250);
});
watch(page, (value) => {
  void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } });
  void loadDomains();
});
</script>

<template>
  <TableDetailLayout class="h-full" :has-detail="detailOpen">
    <template #list>
      <ConsolePage width="content" :title="t('routes.dns.title')" :description="t('routes.dns.description')">
        <ListPage :state="listState">
          <template #toolbar>
            <ListCommandBar :label="t('routes.dns.title')">
              <template #search>
                <SearchInput v-model="search" clearable :placeholder="t('dnsPage.searchDomains')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
              </template>
              <template #actions>
                <div class="flex w-full flex-nowrap items-center gap-2 overflow-x-auto sm:w-auto sm:flex-wrap sm:justify-end">
                  <Button size="sm" :loading="loadingDomains" @click="loadDomains"><RefreshCcw />{{ t('common.refresh') }}</Button>
                  <Button size="sm" variant="primary" @click="openCreateDomain"><Plus />{{ t('common.add') }}</Button>
                </div>
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
                <Button size="sm" :loading="loadingDomains" @click="loadDomains"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>
          </template>

          <template #empty>
            <EmptyState variant="page" :title="t('dnsPage.noDomains')" :description="t('dnsPage.noDomainsHint')" />
          </template>

          <Table
            class="h-full max-lg:max-h-list"
            :columns="domainColumns"
            :rows="domains"
            row-key="id"
            :active-key="selectedId"
            :loading="loadingDomains && !domains.length"
            :loading-label="t('common.loading')"
          >
            <template #name="{ row }">
              <TableDetailName class="max-w-72" :name="row.name" :label="t('common.openDetails', { name: row.name })" @click="openDetail(row)" />
              <span class="block text-xs text-muted-foreground">{{ row.provider }}</span>
            </template>
            <template #updatedAt="{ row }">
              <span class="text-muted-foreground">{{ formatDateTime(row.updatedAt, t('common.never')) }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="secondary" @click="openDetail(row)"><Eye />{{ t('common.details') }}</Button>
                <Button class="max-wide:hidden" size="sm" variant="ghost" :loading="syncingId === row.id" :disabled="syncingId === row.id" @click="syncRecords(row)"><Cloud />{{ t('dnsPage.syncRecords') }}</Button>
                <Dropdown align="right">
                  <template #trigger>
                    <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
                  </template>
                  <DropdownItem :disabled="syncingId === row.id" @click="syncRecords(row)"><Cloud />{{ t('dnsPage.syncRecords') }}</DropdownItem>
                  <DropdownItem @click="openEditDomain(row)"><Wrench />{{ t('common.edit') }}</DropdownItem>
                  <DropdownItem tone="danger" @click="deleteTarget = row"><Trash2 />{{ t('common.delete') }}</DropdownItem>
                </Dropdown>
              </div>
            </template>
            <template #empty>
              <EmptyState
                variant="inset"
                :title="search ? t('common.filterEmpty') : t('dnsPage.noDomains')"
                :description="search ? t('common.filterEmptyHint') : t('dnsPage.noDomainsHint')"
              />
            </template>
          </Table>

          <template #pagination>
            <PaginationBar v-model:page="page" :page-size="pageSize" :total="totalDomains" :loading="loadingDomains" :previous-label="t('common.previous')" :next-label="t('common.next')" />
          </template>
        </ListPage>
      </ConsolePage>
    </template>

    <template #detail>
      <ConsolePage width="content" :title="selectedDomain?.name ?? t('common.loading')" :breadcrumbs="breadcrumbs" :back-label="t('common.backToList')" :body-scroll="false" @back="closeDetail">
        <template v-if="selectedDomain" #actions>
          <Button size="sm" variant="secondary" @click="openEditDomain(selectedDomain)"><Wrench />{{ t('common.edit') }}</Button>
        </template>
        <div class="grid h-full min-h-0 content-start gap-4 overflow-y-auto pr-1">
          <DetailSection :title="t('common.details')">
            <PropertyList :items="domainSummaryItems" :columns="2" />
          </DetailSection>

          <div class="grid gap-3">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0">
                <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('dnsPage.records') }}</h3>
                <p class="m-0 text-sm text-muted-foreground">{{ t('dnsPage.recordsHint') }}</p>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-xs text-muted-foreground">
                  {{ formatDateTime(recordsObservedAt, t('common.never')) }}
                  <template v-if="recordsStale"> · {{ t('dnsPage.staleSnapshot') }}</template>
                </span>
                <Button size="sm" :loading="loadingRecords || syncingId === selectedId" @click="selectedDomain && syncRecords(selectedDomain)"><Cloud />{{ t('dnsPage.syncRecords') }}</Button>
                <Button size="sm" variant="primary" @click="openCreateRecord"><Plus />{{ t('dnsPage.addRecord') }}</Button>
              </div>
            </div>

            <InfoBar v-if="recordsLastRefreshError" tone="warning">{{ recordsLastRefreshError }}</InfoBar>

            <EmptyState v-if="recordsError && !records.length" variant="inset" :title="t('common.loadFailed')" :description="recordsError">
              <template #actions>
                <Button size="sm" :loading="loadingRecords" @click="loadRecords()"><RefreshCcw />{{ t('common.retry') }}</Button>
              </template>
            </EmptyState>

            <Table
              v-else
              :columns="recordColumns"
              :rows="records"
              row-key="id"
              :loading="loadingRecords"
              :loading-label="t('common.loading')"
            >
            <template #type="{ row }">
              <Badge tone="info">{{ row.type }}</Badge>
            </template>
            <template #name="{ row }">
              <strong class="text-foreground">{{ row.name }}</strong>
            </template>
            <template #content="{ row }">
              <span class="block max-w-md truncate text-muted-foreground">{{ row.value }}</span>
            </template>
            <template #ttl="{ row }">
              <span class="text-muted-foreground">{{ row.ttl }}</span>
            </template>
            <template #actions="{ row }">
              <div class="flex items-center justify-end gap-1">
                <Button size="sm" variant="ghost" @click="openEditRecord(row)"><Wrench />{{ t('common.edit') }}</Button>
                <Button size="sm" variant="ghost" @click="recordDeleteTarget = row"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </template>
            <template #empty>
              <EmptyState variant="inset" :title="t('dnsPage.noRecords')" :description="t('dnsPage.noRecordsHint')" />
            </template>
          </Table>
          </div>
        </div>
      </ConsolePage>
    </template>
  </TableDetailLayout>

  <Dialog v-model:open="domainDialog" :title="editingDomain ? t('dnsPage.editDomain') : t('dnsPage.addDomain')" :description="t('dnsPage.domainFormHint')" :close-label="t('common.close')">
    <div class="grid gap-3">
      <div class="grid gap-3 sm:grid-cols-2">
        <FormField :label="t('common.name')" required :error="domainFormErrors.name ? t(domainFormErrors.name) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="domainForm.name" @update:model-value="domainForm.name = $event; delete domainFormErrors.name" />
          </template>
        </FormField>
        <FormField :label="t('dnsPage.provider')">
          <template #default="{ id }">
            <Select :id="id" :model-value="domainForm.provider" :options="providerOptions" @update:model-value="domainForm.provider = $event" />
          </template>
        </FormField>
      </div>
      <FormField :label="t('dnsPage.apiToken')" :hint="editingDomain ? t('dnsPage.blankTokenKeepsCurrent') : ''">
        <template #default="{ id, describedBy }">
          <Input :id="id" :aria-describedby="describedBy" :model-value="domainForm.apiToken" type="password" @update:model-value="domainForm.apiToken = $event" />
        </template>
      </FormField>
    </div>
    <template #footer>
      <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
      <Button @click="domainDialog = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="!domainFormValid" @click="saveDomain">{{ t('common.save') }}</Button>
    </div>
    </template>
  </Dialog>

  <Dialog v-model:open="recordDialog" :title="editingRecord ? t('dnsPage.editRecord') : t('dnsPage.addRecord')" :close-label="t('common.close')">
    <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
      <FormField :label="t('common.type')" required :error="recordFormErrors.type ? t(recordFormErrors.type) : ''">
        <template #default="{ id, describedBy, invalid }">
          <Select :id="id" :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :invalid="invalid" :model-value="recordForm.type" :options="recordTypeOptions" @update:model-value="recordForm.type = $event; delete recordFormErrors.type" />
        </template>
      </FormField>
      <FormField :label="t('common.name')" required :error="recordFormErrors.name ? t(recordFormErrors.name) : ''">
        <template #default="{ id, describedBy, invalid }">
          <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="recordForm.name" @update:model-value="recordForm.name = $event; delete recordFormErrors.name" />
        </template>
      </FormField>
      <div class="col-span-2 max-sm:col-span-1">
        <FormField :label="t('dnsPage.content')" required :error="recordFormErrors.value ? t(recordFormErrors.value) : ''">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" :aria-describedby="describedBy" :invalid="invalid" :model-value="recordForm.value" @update:model-value="recordForm.value = $event; delete recordFormErrors.value" />
          </template>
        </FormField>
      </div>
      <FormField label="TTL">
        <template #default="{ id, describedBy, invalid }">
          <NumberBox :id="id" :aria-describedby="describedBy" :invalid="invalid" :min="1" :max="86400" :step="1" :model-value="toNumber(recordForm.ttl)" @update:model-value="recordForm.ttl = fromNumber($event)" />
        </template>
      </FormField>
      <FormField :label="t('dnsPage.proxyMode')">
        <template #default="{ id }">
          <Select :id="id" :model-value="recordForm.proxied" :options="booleanOptions" @update:model-value="recordForm.proxied = $event" />
        </template>
      </FormField>
    </div>
    <template #footer>
      <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
      <Button @click="recordDialog = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="!recordFormValid" @click="saveRecord">{{ t('common.save') }}</Button>
    </div>
    </template>
  </Dialog>

  <ConfirmDialog
    :open="Boolean(deleteTarget)"
    :title="t('dnsPage.deleteDomain')"
    :description="deleteTarget?.name ?? ''"
    :impact="t('dnsPage.deleteDomainHint')"
    tone="danger"
    :loading="deletingDomain"
    :confirm-label="t('common.delete')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @update:open="(value) => { if (!value) deleteTarget = null; }"
    @confirm="deleteDomain"
    @cancel="deleteTarget = null"
  />

  <ConfirmDialog
    :open="Boolean(recordDeleteTarget)"
    :title="t('dnsPage.deleteRecord')"
    :description="recordDeleteTarget?.name ?? ''"
    :impact="t('dnsPage.deleteRecordHint')"
    tone="danger"
    :loading="deletingRecord"
    :confirm-label="t('common.delete')"
    :cancel-label="t('common.cancel')"
    :checkbox-label="t('serversPage.confirmCheckbox')"
    @update:open="(value) => { if (!value) recordDeleteTarget = null; }"
    @confirm="deleteRecord"
    @cancel="recordDeleteTarget = null"
  />
</template>
