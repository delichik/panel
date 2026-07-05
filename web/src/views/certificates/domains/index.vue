<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useI18n } from '@/i18n';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import type { CertificateDto, CertificateIssueInput, CertificateScope, DnsDomainDto } from '@/types/api';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppDetailPanel from '@/components/AppDetailPanel.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import AppSelectorSummaryItem from '@/components/AppSelectorSummaryItem.vue';
import { usePagination } from '@/composables/usePagination';

const certificates = ref<CertificateDto[]>([]);
const domains = ref<DnsDomainDto[]>([]);
const loading = ref(false);
const issuing = ref(false);
const error = ref('');
const dialog = ref(false);
const deleteDialog = ref(false);
const deleting = ref(false);
const deletingCertificate = ref<CertificateDto | null>(null);
const renewingId = ref('');
const selectedCertificateId = ref('');
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const lastTaskId = ref('');
const { t, formatDateTime } = useI18n();

const form = reactive<CertificateIssueInput>({
  name: '',
  domainId: '',
  prefix: '@',
  scope: 'single',
  variableName: '',
});

const scopeItems = computed<Array<{ label: string; value: CertificateScope }>>(() => [
  { label: t('certificatesPage.singleDomain'), value: 'single' },
  { label: t('certificatesPage.wildcard'), value: 'wildcard' },
]);

const domainOptions = computed(() => domains.value.map((domain) => ({ label: domain.name, value: domain.id })));
const {
  page,
  pageSize,
  total,
  pageItems: pagedCertificates,
} = usePagination(certificates);
const selectedCertificate = computed(() => certificates.value.find((item) => item.id === selectedCertificateId.value) ?? null);

const previewDomains = computed(() => {
  const base = domains.value.find((domain) => domain.id === form.domainId)?.name;
  if (!base) return [];
  const prefix = form.prefix.trim().replace(/^\*\./, '').replace(/\.$/, '') || '@';
  const domain = prefix === '@' ? base : `${prefix}.${base}`;
  return form.scope === 'wildcard' ? [domain, `*.${domain}`] : [domain];
});

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function taskRoute(taskId = lastTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function resetForm() {
  Object.assign(form, { name: '', domainId: domains.value[0]?.id ?? '', prefix: '@', scope: 'single', variableName: '' });
  dialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    const [certificateRows, domainRows] = await Promise.all([certificatesApi.list(), dnsApi.listDomains()]);
    certificates.value = certificateRows;
    domains.value = domainRows;
    if (!certificateRows.some((item) => item.id === selectedCertificateId.value)) {
      selectedCertificateId.value = certificateRows[0]?.id ?? '';
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('certificatesPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function issueCertificate() {
  issuing.value = true;
  try {
    const result = await certificatesApi.issue({ ...form });
    lastTaskId.value = result.taskId || '';
    dialog.value = false;
    showMessage(t('certificatesPage.queued'));
    await load();
  } catch (err) {
    lastTaskId.value = '';
    showMessage(err instanceof Error ? err.message : t('certificatesPage.issueFailed'), 'error');
  } finally {
    issuing.value = false;
  }
}

function askDeleteCertificate(certificate: CertificateDto) {
  deletingCertificate.value = certificate;
  deleteDialog.value = true;
}

async function deleteCertificate() {
  const certificate = deletingCertificate.value;
  if (!certificate) return;
  deleting.value = true;
  try {
    await certificatesApi.delete(certificate.id);
    deleteDialog.value = false;
    deletingCertificate.value = null;
    lastTaskId.value = '';
    showMessage(t('certificatesPage.deleted'));
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('certificatesPage.deleteFailed'), 'error');
  } finally {
    deleting.value = false;
  }
}

async function renewCertificate(certificate: CertificateDto) {
  renewingId.value = certificate.id;
  try {
    await certificatesApi.renew(certificate.id);
    showMessage(t('certificatesPage.renewed'));
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('certificatesPage.renewFailed'), 'error');
  } finally {
    renewingId.value = '';
  }
}

function formatDate(value?: string) {
  if (!value) return t('certificatesPage.unknownDate');
  return formatDateTime(value);
}

function statusLabel(status: string) {
  if (status === 'issued') return t('certificatesPage.statusIssued');
  if (status === 'issuing') return t('certificatesPage.statusIssuing');
  if (status === 'failed') return t('certificatesPage.statusFailed');
  return status;
}

function statusColor(status: string) {
  if (status === 'issued') return 'success';
  if (status === 'failed') return 'error';
  if (status === 'issuing') return 'warning';
  return 'grey';
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <AppMasterDetailWorkspace>
      <template #aside>
      <AppSelectorPanel
        :title="t('routes.domainCertificates.title')"
        :loading="loading"
        :empty="certificates.length === 0"
        empty-icon="mdi-certificate-outline"
        :empty-text="t('certificatesPage.noCertificates')"
        :page="page"
        :page-size="pageSize"
        :total="total"
        @update:page="page = $event"
        @update:page-size="pageSize = $event"
      >
        <template #actions>
          <AppActionButton kind="tool" icon="mdi-plus" :label="t('certificatesPage.issueCertificate')" @click="resetForm" />
        </template>
        <AppSelectorSummaryItem
          v-for="row in pagedCertificates"
          :key="row.id"
          :selected="row.id === selectedCertificateId"
          :title="row.name"
          :subtitle="row.domains.join(', ')"
          :status="false"
          @select="selectedCertificateId = row.id"
        >
          <v-chip :color="statusColor(row.status)" size="x-small" label variant="tonal">{{ statusLabel(row.status) }}</v-chip>
        </AppSelectorSummaryItem>
      </AppSelectorPanel>
      </template>

      <AppDetailPanel class="certificate-detail" :empty="!selectedCertificate" :empty-text="t('certificatesPage.noCertificates')">
        <template v-if="selectedCertificate" #header>
            <div class="min-width-0">
              <div class="text-h6 font-weight-bold text-truncate">{{ selectedCertificate.name }}</div>
              <div class="text-body-2 text-medium-emphasis">{{ selectedCertificate.prefix }} / {{ selectedCertificate.issuer || 'acme' }}</div>
            </div>
            <AppActionGroup context="detail" class="app-detail-actions">
              <v-chip :color="statusColor(selectedCertificate.status)" size="small" label variant="tonal">{{ statusLabel(selectedCertificate.status) }}</v-chip>
              <AppActionButton icon="mdi-autorenew" :label="t('certificatesPage.renewNow')" :loading="renewingId === selectedCertificate.id" :disabled="selectedCertificate.status !== 'issued'" @click="renewCertificate(selectedCertificate)" />
              <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" @click="askDeleteCertificate(selectedCertificate)" />
            </AppActionGroup>
        </template>
        <template v-if="selectedCertificate" #body>
            <div class="detail-grid">
              <div><span>{{ t('certificatesPage.domains') }}</span><strong>{{ selectedCertificate.domains.join(', ') }}</strong></div>
              <div><span>{{ t('certificatesPage.variable') }}</span><strong class="font-mono">certs.{{ selectedCertificate.variableName }}</strong></div>
              <div><span>{{ t('common.validUntil') }}</span><strong>{{ formatDate(selectedCertificate.notAfter) }}</strong></div>
              <div><span>{{ t('certificatesPage.renewal') }}</span><strong>{{ t('certificatesPage.nextRenewal', { value: formatDate(selectedCertificate.nextRenewAt) }) }}</strong></div>
            </div>
            <v-alert v-if="selectedCertificate.lastError" type="error" variant="tonal" density="compact">{{ selectedCertificate.lastError }}</v-alert>
        </template>
      </AppDetailPanel>
    </AppMasterDetailWorkspace>

    <v-dialog v-model="dialog" width="620">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('certificatesPage.issueCertificateTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="dialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-form @submit.prevent="issueCertificate">
            <v-alert v-if="domains.length === 0" type="warning" variant="tonal" density="comfortable" class="mb-3">
              {{ t('certificatesPage.addDnsFirst') }}
            </v-alert>
            <v-text-field v-model="form.name" :label="t('common.name')" variant="outlined" density="comfortable" class="mb-3" />
            <div class="cert-domain-grid mb-3">
              <v-text-field v-model="form.prefix" :label="t('certificatesPage.prefix')" placeholder="@, www, api.v1" variant="outlined" density="comfortable" hide-details />
              <v-select
                v-model="form.domainId"
                :items="domainOptions"
                item-title="label"
                item-value="value"
                :label="t('certificatesPage.managedDomain')"
                variant="outlined"
                density="comfortable"
                hide-details
              />
            </div>
            <v-select
              v-model="form.scope"
              :items="scopeItems"
              item-title="label"
              item-value="value"
              :label="t('certificatesPage.certificateScope')"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
            <v-text-field v-model="form.variableName" :label="t('certificatesPage.variableName')" placeholder="example_com" variant="outlined" density="comfortable" class="mb-3" />
            <div v-if="previewDomains.length" class="preview pa-3 mb-3">
              <div class="text-caption text-medium-emphasis mb-2">{{ t('certificatesPage.requestedDnsNames') }}</div>
              <div class="d-flex flex-wrap" style="gap: 4px;">
                <v-chip v-for="domain in previewDomains" :key="domain" size="small" label color="primary" variant="tonal">
                  {{ domain }}
                </v-chip>
              </div>
            </div>
            <v-alert type="info" variant="tonal" density="comfortable">
              {{ t('certificatesPage.certTemplateHint') }}
            </v-alert>
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="dialog = false" />
            <AppActionButton kind="primary" :label="t('certificatesPage.issue')" :loading="issuing" @click="issueCertificate" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="deleteDialog" width="440">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('certificatesPage.deleteCertificate') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="deleteDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ t('certificatesPage.deleteCertificateConfirm', { name: deletingCertificate?.name ?? '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="deleteDialog = false" />
            <AppActionButton kind="danger-primary" :label="t('common.delete')" :loading="deleting" @click="deleteCertificate" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <AppActionGroup context="snackbar">
          <AppActionButton v-if="lastTaskId" kind="snackbar" :label="t('taskCenter.task')" :to="taskRoute()" />
          <AppActionButton kind="snackbar" :label="t('common.close')" @click="snackbar = false" />
        </AppActionGroup>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.cert-domain-grid {
  display: grid;
  grid-template-columns: minmax(120px, 0.45fr) minmax(180px, 0.55fr);
  gap: 8px;
}

.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.detail-grid > div { display: grid; gap: 4px; min-width: 0; padding: 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.detail-grid span { color: var(--lp-text-muted); font-size: 0.76rem; }
.detail-grid strong { overflow-wrap: anywhere; font-size: 0.88rem; }

.preview {
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--lp-surface-muted), transparent 36%);
}

@media (max-width: 720px) {
  .detail-grid { grid-template-columns: 1fr; }

  .cert-domain-grid {
    grid-template-columns: 1fr;
  }
}
</style>
