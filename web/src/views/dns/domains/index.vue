<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { dnsApi } from '@/api/dns';
import type { DnsDomainDto, DnsDomainInput, DnsRecordDto, DnsRecordInput, DnsRecordType } from '@/types/api';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { usePagination } from '@/composables/usePagination';

const domains = ref<DnsDomainDto[]>([]);
const records = ref<DnsRecordDto[]>([]);
const loading = ref(false);
const recordsLoading = ref(false);
const saving = ref(false);
const recordSaving = ref(false);
const deleting = ref(false);
const deletingRecord = ref(false);
const error = ref('');
const recordsError = ref('');
const selectedDomainId = ref('');
const dialog = ref(false);
const recordDialog = ref(false);
const editing = ref<DnsDomainDto | null>(null);
const editingRecord = ref<DnsRecordDto | null>(null);
const deleteDialog = ref(false);
const deleteRecordDialog = ref(false);
const deletingDomain = ref<DnsDomainDto | null>(null);
const deletingRecordRow = ref<DnsRecordDto | null>(null);
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const { t, formatDateTime } = useI18n();
let recordsRequestId = 0;

const recordTypes: DnsRecordType[] = ['A', 'AAAA', 'CNAME', 'TXT', 'MX', 'SRV', 'CAA', 'NS'];
const selectedDomain = computed(() => domains.value.find((item) => item.id === selectedDomainId.value) ?? null);
const {
  page: domainPage,
  pageSize: domainPageSize,
  total: domainTotal,
  pageItems: pagedDomains,
} = usePagination(domains);
const {
  page: recordsPage,
  pageSize: recordsPageSize,
  total: recordsTotal,
  pageItems: pagedRecords,
} = usePagination(records);

const form = reactive<DnsDomainInput>({
  name: '',
  provider: 'cloudflare',
  apiToken: '',
});

const recordForm = reactive<DnsRecordInput>({
  name: '',
  type: 'A',
  value: '',
  ttl: 120,
  proxied: false,
});

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function resetForm(domain?: DnsDomainDto) {
  editing.value = domain ?? null;
  Object.assign(form, {
    name: domain?.name ?? '',
    provider: domain?.provider ?? 'cloudflare',
    apiToken: '',
  });
  dialog.value = true;
}

function resetRecordForm(record?: DnsRecordDto) {
  editingRecord.value = record ?? null;
  Object.assign(recordForm, {
    name: record?.name ?? '',
    type: (record?.type as DnsRecordType) ?? 'A',
    value: record?.value ?? '',
    ttl: record?.ttl ?? 120,
    proxied: record?.proxied ?? false,
  });
  recordDialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    domains.value = await dnsApi.listDomains();
    error.value = '';
    if (!selectedDomainId.value && domains.value.length > 0) {
      selectedDomainId.value = domains.value[0].id;
    } else if (selectedDomainId.value && !domains.value.some((item) => item.id === selectedDomainId.value)) {
      selectedDomainId.value = domains.value[0]?.id ?? '';
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('domainsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function loadRecords() {
  const domain = selectedDomain.value;
  const requestId = ++recordsRequestId;
  records.value = [];
  recordsError.value = '';
  if (!domain) {
    recordsLoading.value = false;
    return;
  }
  recordsLoading.value = true;
  try {
    const result = await dnsApi.listRecords(domain.id);
    if (requestId !== recordsRequestId || selectedDomainId.value !== domain.id) return;
    records.value = result;
    recordsError.value = '';
  } catch (err) {
    if (requestId !== recordsRequestId || selectedDomainId.value !== domain.id) return;
    recordsError.value = err instanceof Error ? err.message : t('domainsPage.recordsLoadFailed');
  } finally {
    if (requestId === recordsRequestId && selectedDomainId.value === domain.id) {
      recordsLoading.value = false;
    }
  }
}

async function saveDomain() {
  saving.value = true;
  try {
    const payload = { ...form };
    if (editing.value && !payload.apiToken) delete payload.apiToken;
    if (editing.value) {
      const updated = await dnsApi.updateDomain(editing.value.id, payload);
      selectedDomainId.value = updated.id;
      showMessage(t('domainsPage.updated'));
    } else {
      const created = await dnsApi.createDomain(payload);
      selectedDomainId.value = created.id;
      showMessage(t('domainsPage.added'));
    }
    dialog.value = false;
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('domainsPage.saveFailed'), 'error');
  } finally {
    saving.value = false;
  }
}

async function saveRecord() {
  const domain = selectedDomain.value;
  if (!domain) return;
  recordSaving.value = true;
  try {
    const payload = { ...recordForm };
    if (editingRecord.value) {
      await dnsApi.updateRecord(domain.id, editingRecord.value.id, payload);
      showMessage(t('domainsPage.recordUpdated'));
    } else {
      await dnsApi.createRecord(domain.id, payload);
      showMessage(t('domainsPage.recordAdded'));
    }
    recordDialog.value = false;
    await loadRecords();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('domainsPage.recordSaveFailed'), 'error');
  } finally {
    recordSaving.value = false;
  }
}

function askDeleteDomain(domain: DnsDomainDto) {
  deletingDomain.value = domain;
  deleteDialog.value = true;
}

function askDeleteRecord(record: DnsRecordDto) {
  deletingRecordRow.value = record;
  deleteRecordDialog.value = true;
}

async function deleteDomain() {
  const domain = deletingDomain.value;
  if (!domain) return;
  deleting.value = true;
  try {
    await dnsApi.deleteDomain(domain.id);
    deleteDialog.value = false;
    deletingDomain.value = null;
    showMessage(t('domainsPage.deleted'));
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('domainsPage.deleteFailed'), 'error');
  } finally {
    deleting.value = false;
  }
}

async function deleteRecord() {
  const domain = selectedDomain.value;
  const record = deletingRecordRow.value;
  if (!domain || !record) return;
  deletingRecord.value = true;
  try {
    await dnsApi.deleteRecord(domain.id, record.id);
    deleteRecordDialog.value = false;
    deletingRecordRow.value = null;
    showMessage(t('domainsPage.recordDeleted'));
    await loadRecords();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('domainsPage.recordDeleteFailed'), 'error');
  } finally {
    deletingRecord.value = false;
  }
}

watch(selectedDomainId, () => {
  void loadRecords();
});

onMounted(async () => {
  await load();
});
</script>

<template>
  <div class="page-shell dns-workspace">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <PageLoadingState v-if="loading && domains.length === 0" min-height="340px" />

    <div v-else class="dns-grid">
      <v-card variant="outlined" class="domain-list-card">
        <div class="app-card-header domain-list-header">
          <div>
            <div class="text-subtitle-1 font-weight-bold">{{ t('domainsPage.domains') }}</div>
            <div class="text-caption text-medium-emphasis">{{ t('common.total', { count: domains.length }) }}</div>
          </div>
          <v-btn icon="mdi-plus" color="primary" variant="flat" size="small" :aria-label="t('domainsPage.addDomain')" @click="resetForm()" />
        </div>
        <v-divider />
        <v-list class="domain-list" density="comfortable" nav>
          <v-list-item v-if="domains.length === 0 && !loading" class="text-medium-emphasis">
            {{ t('domainsPage.noDomains') }}
          </v-list-item>
          <v-list-item
            v-for="domain in pagedDomains"
            :key="domain.id"
            class="domain-list-item"
            :active="domain.id === selectedDomainId"
            :title="domain.name"
            :subtitle="domain.provider"
            rounded="lg"
            @click="selectedDomainId = domain.id"
          >
            <template #append>
              <v-menu>
                <template #activator="{ props }">
                  <v-btn v-bind="props" icon="mdi-dots-vertical" variant="text" size="small" @click.stop />
                </template>
                <v-list density="compact">
                  <v-list-item prepend-icon="mdi-pencil" :title="t('common.edit')" @click="resetForm(domain)" />
                  <v-list-item prepend-icon="mdi-delete" :title="t('common.delete')" class="text-error" @click="askDeleteDomain(domain)" />
                </v-list>
              </v-menu>
            </template>
          </v-list-item>
        </v-list>
        <AppPagination v-model:page="domainPage" v-model:page-size="domainPageSize" :total="domainTotal" />
      </v-card>

      <div class="dns-main">
        <v-card variant="outlined" class="detail-card">
          <template v-if="selectedDomain">
            <div class="app-card-header">
              <div class="min-width-0">
                <div class="text-h6 font-weight-bold text-truncate">{{ selectedDomain.name }}</div>
                <div class="text-caption text-medium-emphasis">{{ t('domainsPage.updatedAt') }} {{ formatDateTime(selectedDomain.updatedAt) }}</div>
              </div>
              <v-chip size="small" label color="primary" variant="tonal">{{ selectedDomain.provider }}</v-chip>
            </div>
            <v-divider />
            <v-card-text class="domain-detail-grid">
              <div>
                <div class="detail-label">{{ t('domainsPage.createdAt') }}</div>
                <div>{{ formatDateTime(selectedDomain.createdAt) }}</div>
              </div>
            </v-card-text>
          </template>
          <v-card-text v-else class="empty-detail text-medium-emphasis">
            {{ t('domainsPage.selectDomainHint') }}
          </v-card-text>
        </v-card>

        <v-card variant="outlined" class="records-card">
          <div class="app-card-header records-header">
            <div>
              <div class="text-subtitle-1 font-weight-bold">{{ t('domainsPage.records') }}</div>
              <div class="text-caption text-medium-emphasis">{{ selectedDomain?.name || t('domainsPage.noDomainSelected') }}</div>
            </div>
            <div class="records-actions">
              <v-btn icon="mdi-refresh" variant="text" :disabled="!selectedDomain" :loading="recordsLoading" :aria-label="t('common.refresh')" @click="loadRecords" />
              <v-btn color="primary" prepend-icon="mdi-plus" class="text-none font-weight-bold" :disabled="!selectedDomain" @click="resetRecordForm()">
                {{ t('domainsPage.addRecord') }}
              </v-btn>
            </div>
          </div>
          <v-alert v-if="recordsError" type="error" variant="tonal" class="mx-4 mb-3">{{ recordsError }}</v-alert>
          <PageLoadingState v-if="recordsLoading && records.length === 0 && selectedDomain" min-height="220px" />
          <div v-else class="records-table-wrap">
            <v-table class="text-left" style="background: transparent;">
              <thead>
                <tr>
                  <th class="font-weight-bold">{{ t('common.type') }}</th>
                  <th class="font-weight-bold">{{ t('common.name') }}</th>
                  <th class="font-weight-bold">{{ t('common.value') }}</th>
                  <th class="font-weight-bold">{{ t('domainsPage.ttl') }}</th>
                  <th class="font-weight-bold">{{ t('domainsPage.proxy') }}</th>
                  <th class="font-weight-bold text-right" style="width: 180px;">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-if="records.length === 0">
                  <td colspan="6" class="text-center py-6 text-medium-emphasis">
                    {{ selectedDomain ? t('domainsPage.noRecords') : t('domainsPage.selectDomainHint') }}
                  </td>
                </tr>
                <tr v-for="record in pagedRecords" :key="record.id">
                  <td><v-chip size="small" label variant="tonal">{{ record.type }}</v-chip></td>
                  <td class="font-mono">{{ record.name }}</td>
                  <td class="font-mono record-value">{{ record.value }}</td>
                  <td>{{ record.ttl === 1 ? t('domainsPage.ttlAuto') : (record.ttl || '-') }}</td>
                  <td>
                    <v-chip size="small" label :color="record.proxied ? 'success' : undefined" variant="tonal">
                      {{ record.proxied ? t('common.enabled') : t('common.disabled') }}
                    </v-chip>
                  </td>
                  <td class="text-right">
                    <div class="app-table-actions">
                      <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="resetRecordForm(record)">{{ t('common.edit') }}</v-btn>
                      <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="askDeleteRecord(record)">{{ t('common.delete') }}</v-btn>
                    </div>
                  </td>
                </tr>
              </tbody>
            </v-table>
          </div>
          <AppPagination v-model:page="recordsPage" v-model:page-size="recordsPageSize" :total="recordsTotal" />
        </v-card>
      </div>
    </div>

    <v-dialog v-model="dialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editing ? t('domainsPage.editDomain') : t('domainsPage.createDomain') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="dialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-form @submit.prevent="saveDomain">
            <v-text-field v-model="form.name" :label="t('domainsPage.domain')" :placeholder="t('domainsPage.domainNameHint')" variant="outlined" density="comfortable" class="mb-3" />
            <v-select
              v-model="form.provider"
              :items="[{ label: t('domainsPage.cloudflare'), value: 'cloudflare' }]"
              item-title="label"
              item-value="value"
              :label="t('domainsPage.provider')"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
            <v-text-field
              v-model="form.apiToken"
              type="password"
              :label="t('domainsPage.apiToken')"
              :placeholder="editing ? t('domainsPage.keepTokenHint') : t('domainsPage.apiTokenHint')"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="dialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" :loading="saving" class="text-none" @click="saveDomain">
            {{ editing ? t('common.save') : t('common.create') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="recordDialog" width="620">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editingRecord ? t('domainsPage.editRecord') : t('domainsPage.createRecord') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="recordDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-form @submit.prevent="saveRecord">
            <div class="record-form-grid">
              <v-select v-model="recordForm.type" :items="recordTypes" :label="t('common.type')" variant="outlined" density="comfortable" />
              <v-text-field v-model.number="recordForm.ttl" type="number" min="0" :label="t('domainsPage.ttl')" :hint="t('domainsPage.ttlHint')" variant="outlined" density="comfortable" />
            </div>
            <v-text-field v-model="recordForm.name" :label="t('common.name')" :placeholder="t('domainsPage.recordNameHint')" variant="outlined" density="comfortable" class="mb-3" />
            <v-textarea v-model="recordForm.value" :label="t('common.value')" rows="3" auto-grow variant="outlined" density="comfortable" class="mb-3" />
            <v-switch v-model="recordForm.proxied" color="primary" :label="t('domainsPage.proxy')" hide-details />
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="recordDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" :loading="recordSaving" class="text-none" @click="saveRecord">
            {{ editingRecord ? t('common.save') : t('common.create') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="deleteDialog" width="420">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('domainsPage.deleteDomain') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="deleteDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ t('domainsPage.deleteDomainConfirm', { name: deletingDomain?.name ?? '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="deleteDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :loading="deleting" class="text-none" @click="deleteDomain">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="deleteRecordDialog" width="420">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('domainsPage.deleteRecord') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="deleteRecordDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ t('domainsPage.deleteRecordConfirm', { name: deletingRecordRow?.name ?? '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="deleteRecordDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :loading="deletingRecord" class="text-none" @click="deleteRecord">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.dns-workspace {
  flex: 1 1 auto;
  min-height: 0;
}

.dns-grid {
  display: grid;
  grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
  flex: 1 1 auto;
  gap: 18px;
  min-height: 0;
  align-items: stretch;
}

.domain-list-card,
.detail-card,
.records-card {
  min-height: 0;
  overflow: hidden;
}

.domain-list-card {
  display: flex;
  flex-direction: column;
}

.domain-list-header,
.records-header {
  align-items: center;
}

.domain-list {
  display: grid;
  flex: 1 1 auto;
  gap: 8px;
  min-height: 0;
  padding: 10px;
  overflow: auto;
}

.domain-list-item {
  border: 1px solid transparent;
  border-radius: 8px;
  transition: background-color 0.16s ease, border-color 0.16s ease;
}

.domain-list-item:hover {
  background: rgba(var(--v-theme-on-surface), 0.025);
}

.domain-list-item.v-list-item--active {
  border-color: rgba(var(--v-theme-primary), 0.26);
  background: rgba(var(--v-theme-primary), 0.06);
}

.dns-main {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 16px;
  min-height: 0;
  min-width: 0;
}

.domain-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.detail-label {
  margin-bottom: 4px;
  color: var(--lp-text-muted);
  font-size: 0.78rem;
  font-weight: 650;
}

.empty-detail {
  min-height: 112px;
  display: grid;
  place-items: center;
}

.records-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.record-value {
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.records-card {
  display: flex;
  flex-direction: column;
}

.records-table-wrap {
  min-height: 0;
  overflow: auto;
}

.record-form-grid {
  display: grid;
  grid-template-columns: minmax(120px, 160px) minmax(0, 1fr);
  gap: 12px;
  margin-bottom: 12px;
}

.min-width-0 {
  min-width: 0;
}

@media (max-width: 1080px) {
  .dns-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .dns-workspace,
  .dns-grid,
  .domain-list {
    flex: none;
  }

  .dns-main {
    grid-template-rows: none;
  }

  .domain-list-card,
  .detail-card,
  .records-card {
    overflow: visible;
  }

  .domain-list {
    overflow: visible;
  }

  .records-table-wrap {
    overflow-x: auto;
    overflow-y: visible;
  }
}

@media (max-width: 640px) {
  .domain-detail-grid,
  .record-form-grid {
    grid-template-columns: 1fr;
  }

  .records-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .records-actions {
    justify-content: flex-start;
    width: 100%;
  }
}
</style>
