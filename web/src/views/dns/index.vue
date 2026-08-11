<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Cloud, Plus, RefreshCcw, Trash2 } from '@lucide/vue';
import { dnsApi } from '@/api/dns';
import { waitForTask } from '@/api/taskWait';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
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
import type { DnsDomainDto, DnsRecordDto } from '@/types/dns';
import { createLatestRequestGuard } from '@/views/_shared/requestState';
import { formatDateTime } from '@/utils/datetime';
import { domainTone, normalizeRecordName } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const domains = ref<DnsDomainDto[]>([]);
const records = ref<DnsRecordDto[]>([]);
const selectedId = ref('');
const search = ref(String(route.query.search ?? ''));
const page = ref(1);
const pageSize = 50;
const totalDomains = ref(0);
let searchTimer: ReturnType<typeof setTimeout> | null = null;
let recordsRequestId = 0;
let syncController: AbortController | null = null;

function syncTaskSignal() {
  if (!syncController || syncController.signal.aborted) syncController = new AbortController();
  return syncController.signal;
}
const listRequests = createLatestRequestGuard();
const loadingDomains = ref(false);
const loadingRecords = ref(false);
const error = ref('');
const recordsError = ref('');
const providerErrorDomainId = ref('');
const domainDialog = ref(false);
const recordDialog = ref(false);
const deleteDialog = ref(false);
const recordDeleteDialog = ref(false);
const saving = ref(false);
const editingDomain = ref<DnsDomainDto | null>(null);
const editingRecord = ref<DnsRecordDto | null>(null);
const deleteTarget = ref<DnsDomainDto | null>(null);
const recordDeleteTarget = ref<DnsRecordDto | null>(null);

const domainForm = reactive({ name: '', provider: 'cloudflare', apiToken: '' });
const recordForm = reactive({ type: 'A', name: '@', value: '', ttl: '300', proxied: 'true' });

const selectedDomain = computed(() => domains.value.find((item) => item.id === selectedId.value) ?? null);
const recordTypeOptions = ['A', 'AAAA', 'CNAME', 'TXT', 'MX'].map((value) => ({ label: value, value }));
const providerOptions = [{ label: 'Cloudflare', value: 'cloudflare' }];
const booleanOptions = [{ label: t('dnsPage.proxied'), value: 'true' }, { label: t('dnsPage.dnsOnly'), value: 'false' }];

watch(search, (value) => {
  void router.replace({ query: { ...route.query, search: value || undefined } });
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => { if (page.value !== 1) page.value = 1; else void loadDomains(); }, 250);
});
watch(page, () => { void loadDomains(); });

watch(selectedId, (id, _old, onCleanup) => {
  void router.replace({ query: { ...route.query, domain: id || undefined } });
  records.value = [];
  recordsError.value = '';
  if (!id) {
    loadingRecords.value = false;
    return;
  }
  const controller = new AbortController();
  onCleanup(() => controller.abort());
  void loadRecords(id, controller.signal);
});

onMounted(loadDomains);

async function loadDomains() {
  const requestId = listRequests.begin();
  loadingDomains.value = true;
  error.value = '';
  try {
    const result = await dnsApi.listDomainPage({ page: page.value, pageSize, q: search.value.trim() || undefined });
    if (!listRequests.isCurrent(requestId)) return;
    domains.value = result.items;
    totalDomains.value = result.total;
    const queryDomain = String(route.query.domain ?? '');
    selectedId.value = domains.value.some((item) => item.id === queryDomain) ? queryDomain : selectedId.value || domains.value[0]?.id || '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('dnsPage.loadFailed');
    notifyError(err instanceof Error ? err.message : t('dnsPage.loadFailed'));
  } finally {
    if (listRequests.isCurrent(requestId)) loadingDomains.value = false;
  }
}

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer);
  if (syncController) syncController.abort();
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
    providerErrorDomainId.value = '';
  } catch (err) {
    if (signal?.aborted || requestId !== recordsRequestId) return;
    recordsError.value = err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed');
    notifyError(err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed'));
    providerErrorDomainId.value = domainId;
  } finally {
    if (requestId === recordsRequestId) loadingRecords.value = false;
  }
}

async function syncRecords() {
  const domainId = selectedId.value;
  if (!domainId) return;
  loadingRecords.value = true;
  recordsError.value = '';
  try {
    const result = await dnsApi.refreshRecords(domainId);
    notifySuccess(t('resourcesPage.taskAccepted', { taskId: result.taskId }));
    await waitForTask(result.taskId, 90_000, syncTaskSignal());
    if (selectedId.value !== domainId) return;
    await loadRecords(domainId);
  } catch (err) {
    recordsError.value = err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed');
    notifyError(err instanceof Error ? err.message : t('dnsPage.recordsLoadFailed'));
  } finally {
    if (selectedId.value === domainId) loadingRecords.value = false;
  }
}

function openCreateDomain() {
  editingDomain.value = null;
  Object.assign(domainForm, { name: '', provider: 'cloudflare', apiToken: '' });
  domainDialog.value = true;
}

function openEditDomain(domain: DnsDomainDto) {
  editingDomain.value = domain;
  Object.assign(domainForm, { name: domain.name, provider: 'cloudflare', apiToken: '' });
  domainDialog.value = true;
}

async function saveDomain() {
  saving.value = true;
  error.value = '';
  try {
    const input = { name: domainForm.name, provider: 'cloudflare' as const, apiToken: domainForm.apiToken || undefined };
    const saved = editingDomain.value ? await dnsApi.updateDomain(editingDomain.value.id, input) : await dnsApi.createDomain(input);
    selectedId.value = saved.id;
    notifySuccess(t(editingDomain.value ? 'dnsPage.domainUpdated' : 'dnsPage.domainCreated'));
    domainDialog.value = false;
    await loadDomains();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}

function openCreateRecord() {
  editingRecord.value = null;
  Object.assign(recordForm, { type: 'A', name: '@', value: '', ttl: '300', proxied: 'true' });
  recordDialog.value = true;
}

function openEditRecord(record: DnsRecordDto) {
  editingRecord.value = record;
  Object.assign(recordForm, { type: record.type, name: record.name, value: record.value, ttl: String(record.ttl), proxied: record.proxied ? 'true' : 'false' });
  recordDialog.value = true;
}

async function saveRecord() {
  if (!selectedDomain.value) return;
  saving.value = true;
  recordsError.value = '';
  try {
    const input = { type: recordForm.type, name: normalizeRecordName(recordForm.name), value: recordForm.value, ttl: Number(recordForm.ttl) || 300, proxied: recordForm.proxied === 'true' };
    if (editingRecord.value) await dnsApi.updateRecord(selectedDomain.value.id, editingRecord.value.id, input);
    else await dnsApi.createRecord(selectedDomain.value.id, input);
    notifySuccess(t(editingRecord.value ? 'dnsPage.recordUpdated' : 'dnsPage.recordCreated'));
    recordDialog.value = false;
    await loadRecords();
  } catch (err) {
    recordsError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}

async function deleteDomain() {
  if (!deleteTarget.value) return;
  saving.value = true;
  try {
    await dnsApi.deleteDomain(deleteTarget.value.id);
    notifySuccess(t('dnsPage.domainDeleted'));
    selectedId.value = '';
    deleteDialog.value = false;
    await loadDomains();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}

async function deleteRecord() {
  if (!selectedDomain.value || !recordDeleteTarget.value) return;
  saving.value = true;
  try {
    await dnsApi.deleteRecord(selectedDomain.value.id, recordDeleteTarget.value.id);
    notifySuccess(t('dnsPage.recordDeleted'));
    recordDeleteDialog.value = false;
    await loadRecords();
  } catch (err) {
    recordsError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <ConsolePage :title="t('routes.dns.title')" :description="t('routes.dns.description')">
    <template #actions>
      <Button size="sm" :loading="loadingDomains" @click="loadDomains"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button size="sm" variant="primary" @click="openCreateDomain"><Plus />{{ t('dnsPage.addDomain') }}</Button>
    </template>

    <MasterDetailLayout class="h-full min-h-[640px]">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <SearchInput v-model="search" clearable :placeholder="t('dnsPage.searchDomains')" :label="t('common.search')" :clear-label="t('common.clearSearch')" />
        </div>
        <div class="grid grid-cols-2 gap-2 border-b border-border p-4 text-sm">
          <div class="rounded-xl border border-border bg-background p-3">
            <div class="text-xs text-muted-foreground">{{ t('dnsPage.provider') }}</div>
            <strong class="mt-1 block text-foreground">Cloudflare</strong>
          </div>
          <div class="rounded-xl border border-border bg-background p-3">
            <div class="text-xs text-muted-foreground">{{ t('dnsPage.zones') }}</div>
            <strong class="mt-1 block text-foreground">{{ totalDomains }}</strong>
          </div>
        </div>
        <div class="motion-stagger min-h-0 overflow-auto p-2">
          <div v-if="loadingDomains && !domains.length" class="grid gap-2">
            <Skeleton v-for="item in 6" :key="item" class="h-16" />
          </div>
          <EmptyState v-else-if="error && !domains.length" :title="t('common.loadFailed')" :description="error">
            <template #actions>
              <Button size="sm" :loading="loadingDomains" @click="loadDomains"><RefreshCcw />{{ t('common.retry') }}</Button>
            </template>
          </EmptyState>
          <EmptyState v-else-if="!domains.length" :title="t('dnsPage.noDomains')" :description="t('dnsPage.noDomainsHint')" />
          <button
            v-for="domain in domains"
            v-else
            :key="domain.id"
            type="button"
            class="motion-list-item mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent"
            :class="selectedId === domain.id ? 'border-border-strong bg-background' : 'border-transparent'"
            :aria-current="selectedId === domain.id ? 'true' : undefined"
            @click="selectedId = domain.id"
          >
            <div class="flex items-center justify-between gap-2">
              <strong class="truncate text-sm text-foreground">{{ domain.name }}</strong>
              <Badge :tone="domainTone(domain, providerErrorDomainId)">{{ domain.provider }}</Badge>
            </div>
            <span class="truncate text-xs text-muted-foreground">{{ t('dnsPage.updatedAt') }} {{ formatDateTime(domain.updatedAt) }}</span>
          </button>
        </div>
        <PaginationBar v-model:page="page" class="px-3" :page-size="pageSize" :total="totalDomains" :loading="loadingDomains" :previous-label="t('common.previous')" :next-label="t('common.next')" />
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <EmptyState v-if="!selectedDomain" :title="t('dnsPage.selectDomain')" :description="t('dnsPage.selectDomainHint')" />
        <article v-else class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="flex items-start justify-between gap-4 border-b border-border p-5 max-md:grid">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="m-0 text-xl font-semibold text-foreground">{{ selectedDomain.name }}</h2>
                <Badge :tone="domainTone(selectedDomain, providerErrorDomainId)">{{ selectedDomain.provider }}</Badge>
              </div>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ t('dnsPage.cloudflareNoAccount') }}</p>
            </div>
            <div class="flex flex-wrap justify-end gap-2">
              <Button size="sm" :loading="loadingRecords" @click="syncRecords"><Cloud />{{ t('dnsPage.syncRecords') }}</Button>
              <Button size="sm" @click="openEditDomain(selectedDomain)">{{ t('common.edit') }}</Button>
              <Button size="sm" variant="danger" @click="deleteTarget = selectedDomain; deleteDialog = true"><Trash2 />{{ t('common.delete') }}</Button>
            </div>
          </header>
          <section class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] p-5">
            <div class="mb-3 flex items-center justify-between gap-3">
              <div>
                <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('dnsPage.records') }}</h3>
                <p class="m-0 text-sm text-muted-foreground">{{ t('dnsPage.recordsHint') }}</p>
              </div>
              <Button size="sm" variant="primary" @click="openCreateRecord"><Plus />{{ t('dnsPage.addRecord') }}</Button>
            </div>
            <div class="min-h-0 overflow-auto rounded-xl border border-border">
              <table class="w-full border-collapse text-sm">
                <thead class="sticky top-0 bg-muted text-xs uppercase text-muted-foreground">
                  <tr><th class="px-3 py-2 text-left">{{ t('common.type') }}</th><th class="px-3 py-2 text-left">{{ t('common.name') }}</th><th class="px-3 py-2 text-left">{{ t('dnsPage.content') }}</th><th class="px-3 py-2 text-left">TTL</th><th class="px-3 py-2 text-right">{{ t('common.actions') }}</th></tr>
                </thead>
                <tbody class="motion-stagger divide-y divide-border bg-background">
                  <tr v-if="loadingRecords && !records.length"><td colspan="5" class="px-3 py-3"><div class="grid gap-2"><Skeleton v-for="item in 4" :key="item" class="h-8" /></div></td></tr>
                  <tr v-if="recordsError && !loadingRecords"><td colspan="5" class="px-3 py-8"><EmptyState :title="t('common.loadFailed')" :description="recordsError"><template #actions><Button size="sm" :loading="loadingRecords" @click="loadRecords()"><RefreshCcw />{{ t('common.retry') }}</Button></template></EmptyState></td></tr>
                  <tr v-if="!records.length && !loadingRecords && !recordsError"><td colspan="5" class="px-3 py-8"><EmptyState :title="t('dnsPage.noRecords')" :description="t('dnsPage.noRecordsHint')" /></td></tr>
                  <tr v-for="record in records" :key="record.id" class="motion-table-row hover:bg-accent/60">
                    <td class="px-3 py-2"><Badge tone="info">{{ record.type }}</Badge></td>
                    <td class="px-3 py-2 font-medium">{{ record.name }}</td>
                    <td class="max-w-md truncate px-3 py-2 text-muted-foreground">{{ record.value }}</td>
                    <td class="px-3 py-2">{{ record.ttl }}</td>
                    <td class="px-3 py-2 text-right">
                      <Button size="sm" variant="ghost" @click="openEditRecord(record)">{{ t('common.edit') }}</Button>
                      <Button size="sm" variant="ghost" @click="recordDeleteTarget = record; recordDeleteDialog = true">{{ t('common.delete') }}</Button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </article>
      </main>
      </template>
    </MasterDetailLayout>

    <Dialog v-model:open="domainDialog" :title="editingDomain ? t('dnsPage.editDomain') : t('dnsPage.addDomain')" :description="t('dnsPage.domainFormHint')" :close-label="t('common.close')">
      <div class="grid gap-3">
        <label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="domainForm.name" /></label>
        <label class="grid gap-1 text-sm">{{ t('dnsPage.provider') }}<Select v-model="domainForm.provider" :options="providerOptions" /></label>
        <label class="grid gap-1 text-sm">{{ t('dnsPage.apiToken') }}<Input v-model="domainForm.apiToken" type="password" :placeholder="editingDomain ? t('dnsPage.blankTokenKeepsCurrent') : ''" /></label>
      </div>
      <template #footer>
        <Button @click="domainDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="saving" :disabled="!domainForm.name || (!editingDomain && !domainForm.apiToken)" @click="saveDomain">{{ t('common.save') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="recordDialog" :title="editingRecord ? t('dnsPage.editRecord') : t('dnsPage.addRecord')" :close-label="t('common.close')">
      <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
        <label class="grid gap-1 text-sm">{{ t('common.type') }}<Select v-model="recordForm.type" :options="recordTypeOptions" /></label>
        <label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="recordForm.name" /></label>
        <label class="col-span-2 grid gap-1 text-sm max-sm:col-span-1">{{ t('dnsPage.content') }}<Input v-model="recordForm.value" /></label>
        <label class="grid gap-1 text-sm">TTL<Input v-model="recordForm.ttl" type="number" /></label>
        <label class="grid gap-1 text-sm">{{ t('dnsPage.proxyMode') }}<Select v-model="recordForm.proxied" :options="booleanOptions" /></label>
      </div>
      <template #footer>
        <Button @click="recordDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="saving" :disabled="!recordForm.value" @click="saveRecord">{{ t('common.save') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="deleteDialog" :title="t('dnsPage.deleteDomain')" :description="deleteTarget?.name" :close-label="t('common.close')">
      <p class="m-0 text-sm text-muted-foreground">{{ t('dnsPage.deleteDomainHint') }}</p>
      <template #footer><Button @click="deleteDialog = false">{{ t('common.cancel') }}</Button><Button variant="danger" :loading="saving" @click="deleteDomain">{{ t('common.delete') }}</Button></template>
    </Dialog>

    <Dialog v-model:open="recordDeleteDialog" :title="t('dnsPage.deleteRecord')" :description="recordDeleteTarget?.name" :close-label="t('common.close')">
      <p class="m-0 text-sm text-muted-foreground">{{ t('dnsPage.deleteRecordHint') }}</p>
      <template #footer><Button @click="recordDeleteDialog = false">{{ t('common.cancel') }}</Button><Button variant="danger" :loading="saving" @click="deleteRecord">{{ t('common.delete') }}</Button></template>
    </Dialog>
  </ConsolePage>
</template>
