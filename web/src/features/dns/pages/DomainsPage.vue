<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { dnsApi } from '@/api/dns';
import type { DnsDomainDto, DnsDomainInput } from '@/types/api';

const domains = ref<DnsDomainDto[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const dialog = ref(false);
const editing = ref<DnsDomainDto | null>(null);
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

const form = reactive<DnsDomainInput>({
  name: '',
  provider: 'cloudflare',
  apiToken: '',
  accountId: '',
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
    accountId: domain?.accountId ?? '',
  });
  dialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    domains.value = await dnsApi.listDomains();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load domains';
  } finally {
    loading.value = false;
  }
}

async function saveDomain() {
  saving.value = true;
  try {
    const payload = { ...form };
    if (editing.value && !payload.apiToken) delete payload.apiToken;
    if (editing.value) {
      await dnsApi.updateDomain(editing.value.id, payload);
      showMessage('Domain updated');
    } else {
      await dnsApi.createDomain(payload);
      showMessage('Domain added');
    }
    dialog.value = false;
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to save domain', 'error');
  } finally {
    saving.value = false;
  }
}

async function deleteDomain(domain: DnsDomainDto) {
  if (!confirm(`Delete domain ${domain.name}?`)) return;
  try {
    await dnsApi.deleteDomain(domain.id);
    showMessage('Domain deleted');
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to delete domain', 'error');
  }
}

onMounted(load);
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Domains</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Manage DNS zones and provider credentials for certificate validation.</p>
      </div>
      <div class="d-flex" style="gap: 12px;">
        <v-btn prepend-icon="mdi-refresh" :loading="loading" variant="outlined" class="text-none font-weight-bold" @click="load">
          Refresh
        </v-btn>
        <v-btn color="primary" prepend-icon="mdi-plus" class="text-none font-weight-bold" @click="resetForm()">
          Add Domain
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card variant="outlined" class="pa-4">
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">Domain</th>
            <th class="font-weight-bold">Provider</th>
            <th class="font-weight-bold">Account</th>
            <th class="font-weight-bold">Updated</th>
            <th class="font-weight-bold text-right" style="width: 220px;">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="domains.length === 0">
            <td colspan="5" class="text-center py-6 text-grey-darken-1">No domains registered</td>
          </tr>
          <tr v-for="row in domains" :key="row.id">
            <td class="font-weight-bold">{{ row.name }}</td>
            <td><v-chip size="small" label color="primary" variant="tonal">{{ row.provider }}</v-chip></td>
            <td class="font-mono">{{ row.accountId || '-' }}</td>
            <td>{{ new Date(row.updatedAt).toLocaleString() }}</td>
            <td class="text-right">
              <div class="d-flex justify-end" style="gap: 6px;">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="resetForm(row)">Edit</v-btn>
                <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteDomain(row)">Delete</v-btn>
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-navigation-drawer v-model="dialog" location="right" temporary width="560" style="z-index: 1005;">
      <div class="pa-4 fill-height d-flex flex-column">
        <div class="d-flex justify-space-between align-center mb-4">
          <div class="text-h6 font-weight-bold">{{ editing ? 'Edit domain' : 'Add domain' }}</div>
          <div class="d-flex align-center" style="gap: 8px;">
            <v-btn color="primary" variant="flat" size="small" :loading="saving" class="text-none font-weight-bold" @click="saveDomain">
              {{ editing ? 'Save' : 'Create' }}
            </v-btn>
            <v-btn icon="mdi-close" variant="text" size="small" @click="dialog = false" />
          </div>
        </div>
        <v-divider />
        <div class="flex-grow-1 overflow-auto mt-4">
          <v-form @submit.prevent="saveDomain">
            <v-text-field v-model="form.name" label="Domain" placeholder="example.com" variant="outlined" density="comfortable" class="mb-3" />
            <v-select
              v-model="form.provider"
              :items="[{ label: 'Cloudflare', value: 'cloudflare' }]"
              item-title="label"
              item-value="value"
              label="Provider"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
            <v-text-field
              v-model="form.accountId"
              label="Account ID"
              placeholder="Optional Cloudflare account_id"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
            <v-text-field
              v-model="form.apiToken"
              type="password"
              label="API token"
              :placeholder="editing ? 'Leave blank to keep current token' : 'Cloudflare DNS API token'"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />
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
</style>
