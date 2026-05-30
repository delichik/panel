<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import type { CertificateDto, CertificateIssueInput, CertificateScope, DnsDomainDto } from '@/types/api';

const certificates = ref<CertificateDto[]>([]);
const domains = ref<DnsDomainDto[]>([]);
const loading = ref(false);
const issuing = ref(false);
const error = ref('');
const dialog = ref(false);
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

const form = reactive<CertificateIssueInput>({
  name: '',
  domainId: '',
  prefix: '@',
  scope: 'single',
  variableName: '',
});

const scopeItems: Array<{ label: string; value: CertificateScope }> = [
  { label: 'Single domain', value: 'single' },
  { label: 'Wildcard', value: 'wildcard' },
];

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
    error.value = err instanceof Error ? err.message : 'Unable to load certificates';
  } finally {
    loading.value = false;
  }
}

async function issueCertificate() {
  issuing.value = true;
  try {
    await certificatesApi.issue({ ...form });
    dialog.value = false;
    showMessage('Certificate issued');
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to issue certificate', 'error');
  } finally {
    issuing.value = false;
  }
}

async function deleteCertificate(certificate: CertificateDto) {
  if (!confirm(`Delete certificate ${certificate.name}?`)) return;
  try {
    await certificatesApi.delete(certificate.id);
    showMessage('Certificate deleted');
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to delete certificate', 'error');
  }
}

function formatDate(value?: string) {
  if (!value) return 'unknown';
  return new Date(value).toLocaleString();
}

onMounted(load);
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Certificates</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Issue ACME HTTPS certificates using managed DNS domains.</p>
      </div>
      <div class="d-flex" style="gap: 12px;">
        <v-btn prepend-icon="mdi-refresh" :loading="loading" variant="outlined" class="text-none font-weight-bold" @click="load">
          Refresh
        </v-btn>
        <v-btn color="primary" prepend-icon="mdi-certificate" class="text-none font-weight-bold" @click="resetForm">
          Issue Certificate
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card variant="outlined" class="pa-4">
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">Name</th>
            <th class="font-weight-bold">Domains</th>
            <th class="font-weight-bold">Variable</th>
            <th class="font-weight-bold">Renewal</th>
            <th class="font-weight-bold text-right" style="width: 140px;">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="certificates.length === 0">
            <td colspan="5" class="text-center py-6 text-grey-darken-1">No certificates issued</td>
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
            <td class="font-mono">certs.{{ row.variableName }}</td>
            <td>
              <div>{{ formatDate(row.notAfter) }}</div>
              <div class="text-caption text-medium-emphasis">next {{ formatDate(row.nextRenewAt) }}</div>
            </td>
            <td class="text-right">
              <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteCertificate(row)">Delete</v-btn>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-navigation-drawer v-model="dialog" location="right" temporary width="560" style="z-index: 1005;">
      <div class="pa-4 fill-height d-flex flex-column">
        <div class="d-flex justify-space-between align-center mb-4">
          <div class="text-h6 font-weight-bold">Issue certificate</div>
          <div class="d-flex align-center" style="gap: 8px;">
            <v-btn color="primary" variant="flat" size="small" :loading="issuing" class="text-none font-weight-bold" @click="issueCertificate">
              Issue
            </v-btn>
            <v-btn icon="mdi-close" variant="text" size="small" @click="dialog = false" />
          </div>
        </div>
        <v-divider />
        <div class="flex-grow-1 overflow-auto mt-4">
          <v-form @submit.prevent="issueCertificate">
            <v-alert v-if="domains.length === 0" type="warning" variant="tonal" density="comfortable" class="mb-3">
              Add a DNS domain before issuing certificates.
            </v-alert>
            <v-text-field v-model="form.name" label="Name" variant="outlined" density="comfortable" class="mb-3" />
            <div class="cert-domain-grid mb-3">
              <v-text-field v-model="form.prefix" label="Prefix" placeholder="@, www, api.v1" variant="outlined" density="comfortable" hide-details />
              <v-select
                v-model="form.domainId"
                :items="domainOptions"
                item-title="label"
                item-value="value"
                label="Managed domain"
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
              label="Certificate scope"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
            <v-text-field v-model="form.variableName" label="Variable name" placeholder="example_com" variant="outlined" density="comfortable" class="mb-3" />
            <div v-if="previewDomains.length" class="preview pa-3 mb-3">
              <div class="text-caption text-medium-emphasis mb-2">Requested DNS names</div>
              <div class="d-flex flex-wrap" style="gap: 4px;">
                <v-chip v-for="domain in previewDomains" :key="domain" size="small" label color="primary" variant="tonal">
                  {{ domain }}
                </v-chip>
              </div>
            </div>
            <v-alert type="info" variant="tonal" density="comfortable">
              Templates can read PEM values from certs.&lt;variable&gt;.certificatePem and certs.&lt;variable&gt;.privateKeyPem.
            </v-alert>
          </v-form>
        </div>
      </div>
    </v-navigation-drawer>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">Close</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.font-mono {
  font-family: monospace !important;
}

.cert-domain-grid {
  display: grid;
  grid-template-columns: minmax(120px, 0.45fr) minmax(180px, 0.55fr);
  gap: 8px;
}

.preview {
  border: 1px solid rgba(var(--v-border-color), 0.12);
  border-radius: 8px;
  background: rgba(var(--v-theme-surface-variant), 0.24);
}

@media (max-width: 720px) {
  .cert-domain-grid {
    grid-template-columns: 1fr;
  }
}
</style>
