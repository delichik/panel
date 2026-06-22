<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useI18n } from '@/i18n';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import type { CertificateDto, CertificateIssueInput, CertificateScope, DnsDomainDto } from '@/types/api';
import AppSelectorItem from '@/components/AppSelectorItem.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
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

    <div class="certificate-workspace">
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
          <v-btn icon="mdi-plus" color="primary" variant="flat" size="small" :aria-label="t('certificatesPage.issueCertificate')" @click="resetForm" />
        </template>
        <AppSelectorItem v-for="row in pagedCertificates" :key="row.id" :selected="row.id === selectedCertificateId" @select="selectedCertificateId = row.id">
          <span class="selector-copy min-width-0">
            <span class="selector-name text-truncate">{{ row.name }}</span>
            <span class="selector-meta text-truncate">{{ row.domains.join(', ') }}</span>
          </span>
          <v-chip :color="statusColor(row.status)" size="x-small" label variant="tonal">{{ statusLabel(row.status) }}</v-chip>
        </AppSelectorItem>
      </AppSelectorPanel>

      <v-card variant="outlined" class="certificate-detail">
        <template v-if="selectedCertificate">
          <div class="detail-header">
            <div class="min-width-0">
              <div class="text-h6 font-weight-bold text-truncate">{{ selectedCertificate.name }}</div>
              <div class="text-body-2 text-medium-emphasis">{{ selectedCertificate.prefix }} / {{ selectedCertificate.issuer || 'acme' }}</div>
            </div>
            <div class="detail-actions">
              <v-chip :color="statusColor(selectedCertificate.status)" size="small" label variant="tonal">{{ statusLabel(selectedCertificate.status) }}</v-chip>
              <v-btn size="small" variant="outlined" prepend-icon="mdi-autorenew" :loading="renewingId === selectedCertificate.id" :disabled="selectedCertificate.status !== 'issued'" @click="renewCertificate(selectedCertificate)">{{ t('certificatesPage.renewNow') }}</v-btn>
              <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="askDeleteCertificate(selectedCertificate)">{{ t('common.delete') }}</v-btn>
            </div>
          </div>
          <div class="detail-body">
            <div class="detail-grid">
              <div><span>{{ t('certificatesPage.domains') }}</span><strong>{{ selectedCertificate.domains.join(', ') }}</strong></div>
              <div><span>{{ t('certificatesPage.variable') }}</span><strong class="font-mono">certs.{{ selectedCertificate.variableName }}</strong></div>
              <div><span>{{ t('common.validUntil') }}</span><strong>{{ formatDate(selectedCertificate.notAfter) }}</strong></div>
              <div><span>{{ t('certificatesPage.renewal') }}</span><strong>{{ t('certificatesPage.nextRenewal', { value: formatDate(selectedCertificate.nextRenewAt) }) }}</strong></div>
            </div>
            <v-alert v-if="selectedCertificate.lastError" type="error" variant="tonal" density="compact">{{ selectedCertificate.lastError }}</v-alert>
          </div>
        </template>
        <div v-else class="empty-detail text-medium-emphasis">{{ t('certificatesPage.noCertificates') }}</div>
      </v-card>
    </div>

    <v-dialog v-model="dialog" width="620">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('certificatesPage.issueCertificateTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="dialog = false" />
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
          <v-btn variant="text" class="text-none" @click="dialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" :loading="issuing" class="text-none" @click="issueCertificate">{{ t('certificatesPage.issue') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="deleteDialog" width="440">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('certificatesPage.deleteCertificate') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="deleteDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ t('certificatesPage.deleteCertificateConfirm', { name: deletingCertificate?.name ?? '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="deleteDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :loading="deleting" class="text-none" @click="deleteCertificate">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn v-if="lastTaskId" color="white" variant="text" :to="taskRoute()">{{ t('taskCenter.task') }}</v-btn>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
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

.certificate-workspace { display: grid; grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr); flex: 1 1 auto; gap: 18px; min-height: 0; }
.certificate-detail { display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; }
.detail-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 16px; border-bottom: 1px solid var(--lp-border); }
.detail-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.detail-body { display: grid; gap: 16px; align-content: start; min-height: 0; padding: 16px; overflow: auto; }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.detail-grid > div { display: grid; gap: 4px; min-width: 0; padding: 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.detail-grid span, .selector-meta { color: var(--lp-text-muted); font-size: 0.76rem; }
.detail-grid strong { overflow-wrap: anywhere; font-size: 0.88rem; }
.selector-copy, .selector-name, .selector-meta { display: block; min-width: 0; }
.selector-name { font-size: 0.9rem; font-weight: 700; }
.selector-meta { margin-top: 2px; }
.empty-detail { display: grid; flex: 1 1 auto; place-items: center; min-height: 220px; padding: 32px; }

.preview {
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--lp-surface-muted), transparent 36%);
}

@media (max-width: 1080px) {
  .certificate-workspace { grid-template-columns: 1fr; }
}

@media (max-width: 720px) {
  .certificate-workspace { flex: none; }
  .certificate-detail, .detail-body { overflow: visible; }
  .detail-header { align-items: stretch; flex-direction: column; }
  .detail-actions { justify-content: flex-start; }
  .detail-grid { grid-template-columns: 1fr; }

  .cert-domain-grid {
    grid-template-columns: 1fr;
  }
}
</style>
