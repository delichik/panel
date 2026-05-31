<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useI18n } from '@/i18n';
import { dnsApi } from '@/api/dns';
import type { DnsDomainDto, DnsDomainInput } from '@/types/api';

const domains = ref<DnsDomainDto[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const dialog = ref(false);
const editing = ref<DnsDomainDto | null>(null);
const deleteDialog = ref(false);
const deleting = ref(false);
const deletingDomain = ref<DnsDomainDto | null>(null);
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const { t, formatDateTime } = useI18n();

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
    error.value = err instanceof Error ? err.message : t('domainsPage.loadFailed');
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
      showMessage(t('domainsPage.updated'));
    } else {
      await dnsApi.createDomain(payload);
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

function askDeleteDomain(domain: DnsDomainDto) {
  deletingDomain.value = domain;
  deleteDialog.value = true;
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

onMounted(load);
</script>

<template>
  <div>
    <div class="d-flex justify-end mb-4" style="gap: 12px;">
      <v-btn color="primary" prepend-icon="mdi-plus" class="text-none font-weight-bold" @click="resetForm()">
        {{ t('domainsPage.addDomain') }}
      </v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card variant="outlined" class="pa-4">
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">{{ t('domainsPage.domain') }}</th>
            <th class="font-weight-bold">{{ t('domainsPage.provider') }}</th>
            <th class="font-weight-bold">{{ t('domainsPage.account') }}</th>
            <th class="font-weight-bold">{{ t('domainsPage.updatedAt') }}</th>
            <th class="font-weight-bold text-right" style="width: 220px;">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="domains.length === 0">
            <td colspan="5" class="text-center py-6 text-medium-emphasis">{{ t('domainsPage.noDomains') }}</td>
          </tr>
          <tr v-for="row in domains" :key="row.id">
            <td class="font-weight-bold">{{ row.name }}</td>
            <td><v-chip size="small" label color="primary" variant="tonal">{{ row.provider }}</v-chip></td>
            <td class="font-mono">{{ row.accountId || '-' }}</td>
            <td>{{ formatDateTime(row.updatedAt) }}</td>
            <td class="text-right">
              <div class="d-flex justify-end" style="gap: 6px;">
                <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="resetForm(row)">{{ t('common.edit') }}</v-btn>
                <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="askDeleteDomain(row)">{{ t('common.delete') }}</v-btn>
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="dialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ editing ? t('domainsPage.editDomain') : t('domainsPage.createDomain') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="dialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-form @submit.prevent="saveDomain">
            <v-text-field v-model="form.name" :label="t('domainsPage.domain')" placeholder="example.com" variant="outlined" density="comfortable" class="mb-3" />
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
              v-model="form.accountId"
              :label="t('domainsPage.accountId')"
              :placeholder="t('domainsPage.accountIdHint')"
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

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.font-mono {
  font-family: monospace !important;
}
</style>
