<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useI18n } from '@/i18n';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import type { CertificateDto, CertificateIssueInput, CertificateScope, DnsDomainDto } from '@/types/api';

const certificates = ref<CertificateDto[]>([]);
const domains = ref<DnsDomainDto[]>([]);
const loading = ref(false);
const issuing = ref(false);
const error = ref('');
const dialog = ref(false);
const deleteDialog = ref(false);
const deleting = ref(false);
const deletingCertificate = ref<CertificateDto | null>(null);
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

    <v-card variant="outlined" class="table-card">
      <div class="app-card-header">
        <v-btn color="primary" prepend-icon="mdi-certificate" class="text-none font-weight-bold action-btn" @click="resetForm">
          {{ t('certificatesPage.issueCertificate') }}
        </v-btn>
      </div>
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">{{ t('common.name') }}</th>
            <th class="font-weight-bold">{{ t('certificatesPage.domains') }}</th>
            <th class="font-weight-bold">{{ t('common.status') }}</th>
            <th class="font-weight-bold">{{ t('certificatesPage.variable') }}</th>
            <th class="font-weight-bold">{{ t('certificatesPage.renewal') }}</th>
            <th class="font-weight-bold text-right" style="width: 140px;">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="certificates.length === 0">
            <td colspan="6" class="text-center py-6 text-medium-emphasis">{{ t('certificatesPage.noCertificates') }}</td>
          </tr>
          <tr v-for="row in certificates" :key="row.id">
            <td class="font-weight-bold">
              <div>{{ row.name }}</div>
              <div class="text-caption text-medium-emphasis">{{ row.prefix }} / {{ row.issuer || 'acme' }}</div>
            </td>
            <td>
              <div class="d-flex flex-wrap" style="gap: 4px;">
                <v-chip v-for="domain in row.domains" :key="domain" size="small" label variant="tonal">
                  {{ domain }}
                </v-chip>
              </div>
            </td>
            <td>
              <v-chip :color="statusColor(row.status)" size="small" label variant="tonal">{{ statusLabel(row.status) }}</v-chip>
              <div v-if="row.lastError" class="text-caption text-error mt-1">{{ row.lastError }}</div>
            </td>
            <td class="font-mono">certs.{{ row.variableName }}</td>
            <td>
              <div>{{ formatDate(row.notAfter) }}</div>
              <div class="text-caption text-medium-emphasis">{{ t('certificatesPage.nextRenewal', { value: formatDate(row.nextRenewAt) }) }}</div>
            </td>
            <td class="text-right">
              <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="askDeleteCertificate(row)">{{ t('common.delete') }}</v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

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

.table-card {
  overflow: hidden;
}

.preview {
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: color-mix(in srgb, var(--lp-surface-muted), transparent 36%);
}

@media (max-width: 720px) {
  .cert-domain-grid {
    grid-template-columns: 1fr;
  }
}
</style>
