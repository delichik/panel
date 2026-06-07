<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useI18n } from '@/i18n';
import { useAuthStore } from '@/stores/auth';
import { useSettingsStore } from '@/stores/settings';
import type { RuntimeSettingsDto, RuntimeSettingsUpdate, TokenExpiration } from '@/types/api';

type SettingsCategory = 'general' | 'security' | 'nomad' | 'certificates' | 'system';

const route = useRoute();
const auth = useAuthStore();
const settingsStore = useSettingsStore();
const { t, translateCleanupSchedule } = useI18n();

const settings = ref<RuntimeSettingsDto | null>(null);
const loading = ref(false);
const saving = ref(false);
const accountSaving = ref(false);
const jwtSaving = ref(false);
const error = ref('');
const jwtSecret = ref('');
const showCurrentPassword = ref(false);
const showNewPassword = ref(false);
const showConfirmPassword = ref(false);
const showJwtSecret = ref(false);

const category = computed<SettingsCategory>(() => {
  const value = String(route.meta.settingsCategory || 'general');
  return ['general', 'security', 'nomad', 'certificates', 'system'].includes(value) ? value as SettingsCategory : 'general';
});

const form = reactive<RuntimeSettingsUpdate>({
  metricsRetentionDays: 7,
  metricsCollectionIntervalSeconds: 60,
  cleanupSchedule: 'daily',
  tokenExpiration: '1d',
  language: 'en',
  remoteCommandTimeoutSeconds: 30,
  nomad: {
    namespace: 'default',
    region: 'global',
    datacenter: 'dc1',
  },
  certificates: {
    email: '',
    dnsPropagationDelaySeconds: 30,
  },
});

const accountForm = reactive({
  username: auth.username,
  currentPassword: '',
  newPassword: '',
  confirmPassword: '',
});

const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function syncForm(next: RuntimeSettingsDto) {
  form.metricsRetentionDays = next.metricsRetentionDays;
  form.metricsCollectionIntervalSeconds = next.metricsCollectionIntervalSeconds;
  form.cleanupSchedule = next.cleanupSchedule;
  form.tokenExpiration = next.tokenExpiration || '1d';
  form.language = next.language;
  form.remoteCommandTimeoutSeconds = next.remoteCommandTimeoutSeconds;
  form.nomad = { ...next.nomad };
  form.certificates = { ...next.certificates };
  accountForm.username = auth.username;
}

async function loadSettings() {
  loading.value = true;
  try {
    const next = await settingsStore.loadRuntime(true);
    if (next) {
      settings.value = next;
      syncForm(next);
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function saveRuntimeSettings() {
  saving.value = true;
  try {
    const next = await settingsStore.updateRuntime({
      ...form,
      nomad: { ...form.nomad },
      certificates: { ...form.certificates },
    });
    settings.value = next;
    syncForm(next);
    error.value = '';
    showMessage(t('settingsPage.saveSuccess'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.saveFailed');
  } finally {
    saving.value = false;
  }
}

async function saveAccount() {
  if (accountForm.newPassword !== accountForm.confirmPassword) {
    error.value = t('changePassword.passwordMismatch');
    return;
  }
  accountSaving.value = true;
  try {
    await auth.updateAccount({
      currentPassword: accountForm.currentPassword,
      username: accountForm.username,
      newPassword: accountForm.newPassword,
    });
    accountForm.currentPassword = '';
    accountForm.newPassword = '';
    accountForm.confirmPassword = '';
    accountForm.username = auth.username;
    error.value = '';
    showMessage(t('settingsPage.accountSaveSuccess'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.accountSaveFailed');
  } finally {
    accountSaving.value = false;
  }
}

async function saveJwtSecret() {
  if (!jwtSecret.value.trim()) {
    error.value = t('settingsPage.jwtSecretRequired');
    return;
  }
  jwtSaving.value = true;
  try {
    await auth.updateJwtSecret(jwtSecret.value);
    jwtSecret.value = '';
    const next = await settingsStore.loadRuntime(true);
    if (next) {
      settings.value = next;
      syncForm(next);
    }
    error.value = '';
    showMessage(t('settingsPage.jwtSecretSaveSuccess'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.jwtSecretSaveFailed');
  } finally {
    jwtSaving.value = false;
  }
}

function generateJwtSecret() {
  if (!globalThis.crypto?.getRandomValues) {
    error.value = t('settingsPage.jwtSecretGenerateFailed');
    return;
  }
  const bytes = new Uint8Array(32);
  globalThis.crypto.getRandomValues(bytes);
  const raw = Array.from(bytes, (byte) => String.fromCharCode(byte)).join('');
  jwtSecret.value = btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

function tokenExpirationItems(): Array<{ title: string; value: TokenExpiration }> {
  return [
    { title: t('tokenExpiration.10m'), value: '10m' },
    { title: t('tokenExpiration.1h'), value: '1h' },
    { title: t('tokenExpiration.1d'), value: '1d' },
    { title: t('tokenExpiration.5d'), value: '5d' },
    { title: t('tokenExpiration.30d'), value: '30d' },
    { title: t('tokenExpiration.never'), value: 'never' },
  ];
}

watch(() => auth.username, (username) => {
  accountForm.username = username;
});

onMounted(loadSettings);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card :loading="loading" variant="outlined" class="settings-surface pa-6">
      <template v-if="settings">
        <div class="settings-header">
          <div>
            <div class="text-overline text-medium-emphasis">{{ t('layout.nav.settings') }}</div>
            <h2 class="settings-title">{{ t(`settingsPage.categories.${category}`) }}</h2>
          </div>

          <v-btn
            v-if="category !== 'security' && category !== 'system'"
            color="primary"
            prepend-icon="mdi-content-save"
            :loading="saving"
            class="text-none font-weight-bold action-btn"
            @click="saveRuntimeSettings"
          >
            {{ t('common.save') }}
          </v-btn>
        </div>

        <v-form v-if="category === 'general'" class="settings-form">
          <v-text-field
            v-model.number="form.metricsRetentionDays"
            type="number"
            min="1"
            max="3650"
            :label="t('settingsPage.metricsRetention')"
            :suffix="t('settingsPage.days')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <v-text-field
            v-model.number="form.metricsCollectionIntervalSeconds"
            type="number"
            min="10"
            max="86400"
            :label="t('settingsPage.collectionInterval')"
            :suffix="t('settingsPage.seconds')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <v-text-field
            v-model.number="form.remoteCommandTimeoutSeconds"
            type="number"
            min="1"
            max="3600"
            :label="t('settingsPage.remoteCommandTimeout')"
            :suffix="t('settingsPage.seconds')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <div>
            <div class="text-subtitle-2 mb-2 text-medium-emphasis">{{ t('settingsPage.cleanupSchedule') }}</div>
            <v-btn-toggle v-model="form.cleanupSchedule" mandatory color="primary" density="compact">
              <v-btn value="hourly" class="text-none">{{ translateCleanupSchedule('hourly') }}</v-btn>
              <v-btn value="daily" class="text-none">{{ translateCleanupSchedule('daily') }}</v-btn>
              <v-btn value="weekly" class="text-none">{{ translateCleanupSchedule('weekly') }}</v-btn>
            </v-btn-toggle>
          </div>

          <v-select
            v-model="form.tokenExpiration"
            :items="tokenExpirationItems()"
            :label="t('settingsPage.tokenExpiration')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <v-select
            v-model="form.language"
            :items="[
              { title: t('languages.en'), value: 'en' },
              { title: t('languages.zh-CN'), value: 'zh-CN' },
            ]"
            :label="t('settingsPage.language')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />
        </v-form>

        <v-form v-else-if="category === 'security'" class="settings-form">
          <div class="section-title">{{ t('settingsPage.account') }}</div>

          <v-text-field
            v-model="accountForm.username"
            :label="t('login.username')"
            prepend-inner-icon="mdi-account"
            variant="outlined"
            density="comfortable"
            autocomplete="username"
            hide-details="auto"
          />

          <v-text-field
            v-model="accountForm.currentPassword"
            :label="t('changePassword.currentPassword')"
            prepend-inner-icon="mdi-lock"
            :append-inner-icon="showCurrentPassword ? 'mdi-eye' : 'mdi-eye-off'"
            :type="showCurrentPassword ? 'text' : 'password'"
            @click:append-inner="showCurrentPassword = !showCurrentPassword"
            variant="outlined"
            density="comfortable"
            autocomplete="current-password"
            hide-details="auto"
          />

          <v-text-field
            v-model="accountForm.newPassword"
            :label="t('changePassword.newPassword')"
            prepend-inner-icon="mdi-shield-key"
            :append-inner-icon="showNewPassword ? 'mdi-eye' : 'mdi-eye-off'"
            :type="showNewPassword ? 'text' : 'password'"
            @click:append-inner="showNewPassword = !showNewPassword"
            variant="outlined"
            density="comfortable"
            autocomplete="new-password"
            hide-details="auto"
          />

          <v-text-field
            v-model="accountForm.confirmPassword"
            :label="t('changePassword.confirmPassword')"
            prepend-inner-icon="mdi-check-decagram"
            :append-inner-icon="showConfirmPassword ? 'mdi-eye' : 'mdi-eye-off'"
            :type="showConfirmPassword ? 'text' : 'password'"
            @click:append-inner="showConfirmPassword = !showConfirmPassword"
            variant="outlined"
            density="comfortable"
            autocomplete="new-password"
            hide-details="auto"
          />

          <div class="form-actions">
            <v-btn color="primary" prepend-icon="mdi-account-check" :loading="accountSaving" class="text-none font-weight-bold" @click="saveAccount">
              {{ t('settingsPage.saveAccount') }}
            </v-btn>
          </div>

          <v-divider class="my-2" />

          <div class="section-title">{{ t('settingsPage.jwtSecret') }}</div>

          <v-text-field
            v-model="jwtSecret"
            :label="t('settingsPage.jwtSecret')"
            :placeholder="t('settingsPage.jwtSecretPlaceholder')"
            prepend-inner-icon="mdi-key-variant"
            :append-inner-icon="showJwtSecret ? 'mdi-eye' : 'mdi-eye-off'"
            :type="showJwtSecret ? 'text' : 'password'"
            @click:append-inner="showJwtSecret = !showJwtSecret"
            variant="outlined"
            density="comfortable"
            autocomplete="off"
            hide-details="auto"
          >
            <template #append>
              <v-tooltip :text="t('settingsPage.generateJwtSecret')">
                <template #activator="{ props }">
                  <v-btn v-bind="props" icon size="small" variant="text" class="utility-btn" @click="generateJwtSecret">
                    <v-icon>mdi-dice-5-outline</v-icon>
                  </v-btn>
                </template>
              </v-tooltip>
            </template>
          </v-text-field>

          <div class="text-caption text-medium-emphasis">
            {{ settings.jwtSecretConfigured ? t('settingsPage.jwtSecretConfigured') : t('settingsPage.jwtSecretNotConfigured') }}
          </div>

          <div class="form-actions">
            <v-btn color="primary" prepend-icon="mdi-key-change" :loading="jwtSaving" class="text-none font-weight-bold" @click="saveJwtSecret">
              {{ t('settingsPage.saveJwtSecret') }}
            </v-btn>
          </div>
        </v-form>

        <v-form v-else-if="category === 'nomad'" class="settings-form">
          <v-text-field
            v-model="form.nomad.namespace"
            :label="t('settingsPage.nomadNamespace')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <v-text-field
            v-model="form.nomad.region"
            :label="t('settingsPage.nomadRegion')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <v-text-field
            v-model="form.nomad.datacenter"
            :label="t('settingsPage.nomadDatacenter')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />
        </v-form>

        <v-form v-else-if="category === 'certificates'" class="settings-form">
          <v-text-field
            v-model="form.certificates.email"
            type="email"
            :label="t('settingsPage.certificateEmail')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />

          <v-text-field
            v-model.number="form.certificates.dnsPropagationDelaySeconds"
            type="number"
            min="0"
            max="3600"
            :label="t('settingsPage.dnsPropagationDelay')"
            :suffix="t('settingsPage.seconds')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />
        </v-form>

        <div v-else class="system-table">
          <v-table density="compact" style="background: transparent;" class="text-left">
            <tbody>
              <tr>
                <td class="system-label">{{ t('settingsPage.listenAddress') }}</td>
                <td>{{ settings.listenAddress }}</td>
              </tr>
              <tr>
                <td class="system-label">{{ t('settingsPage.applicationDatabase') }}</td>
                <td class="font-mono text-caption">{{ settings.appDatabase }}</td>
              </tr>
              <tr>
                <td class="system-label">{{ t('settingsPage.metricsDatabase') }}</td>
                <td class="font-mono text-caption">{{ settings.metricsDatabase }}</td>
              </tr>
              <tr>
                <td class="system-label">{{ t('settingsPage.dataRoot') }}</td>
                <td class="font-mono text-caption">{{ settings.dataRoot }}</td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </template>

      <div v-else class="text-center py-10 text-medium-emphasis">
        <v-icon size="40" class="mb-2" color="medium-emphasis">mdi-cog-off-outline</v-icon>
        <div>{{ t('settingsPage.unavailable') }}</div>
      </div>
    </v-card>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template #actions>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.settings-surface {
  background-color: var(--lp-surface) !important;
  border-color: var(--lp-border) !important;
}

.settings-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}

.settings-title {
  margin: 0;
  color: var(--lp-text);
  font-size: 1.18rem;
  font-weight: 760;
  line-height: 1.2;
  letter-spacing: 0;
}

.settings-form {
  display: grid;
  max-width: 560px;
  gap: 16px;
}

.section-title {
  color: var(--lp-text);
  font-size: 0.96rem;
  font-weight: 720;
}

.form-actions {
  display: flex;
  justify-content: flex-start;
}

.system-table {
  max-width: 860px;
}

.system-label {
  width: 220px;
  padding: 12px 16px 12px 0;
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  font-weight: 740;
  text-transform: uppercase;
}

.utility-btn {
  color: var(--lp-text-muted);
}

@media (max-width: 720px) {
  .settings-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .action-btn {
    width: 100%;
  }

  .system-label {
    width: 150px;
  }
}
</style>
