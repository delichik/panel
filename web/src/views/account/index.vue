<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { ClipboardList, Plus, Trash2 } from '@/icons';
import { accessCredentialsApi, type AccessCredential, type AccessCredentialCreated as CreatedCredential } from '@/api/access';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import RadioButton from '@/components/ui/RadioButton.vue';
import Checkbox from '@/components/ui/Checkbox.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const session = useSessionStore();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const currentPassword = ref('');
const newPassword = ref('');
const confirmPassword = ref('');
const savingPassword = ref(false);
const passwordError = ref('');

const credentials = ref<AccessCredential[]>([]);
const credentialsLoading = ref(false);
const createOpen = ref(false);
const creating = ref(false);
const created = ref<CreatedCredential | null>(null);
const name = ref('');
const minutes = ref(15);
const scope = ref<'readonly' | 'readwrite'>('readonly');
const allowManagement = ref(false);
const revokeTarget = ref<AccessCredential | null>(null);

const canManageAccess = computed(() => session.canAny(['access.users.manage', 'access.roles.manage', 'access.credentials.manage']));

watch(createOpen, (open) => {
  if (!open) created.value = null;
});

const credentialColumns = computed(() => [
  { key: 'name', label: t('account.credentials.name') },
  { key: 'scope', label: t('account.credentials.scope') },
  { key: 'status', label: t('common.status') },
  { key: 'expiresAt', label: t('account.credentials.expiresAt') },
  { key: 'actions', label: '', align: 'right' as const },
]);

async function savePassword() {
  passwordError.value = '';
  if (!currentPassword.value || !newPassword.value) {
    passwordError.value = t('auth.changePasswordRequiredFields');
    return;
  }
  if (newPassword.value.length < 8) {
    passwordError.value = t('auth.newPasswordTooShort');
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = t('auth.passwordsDoNotMatch');
    return;
  }
  savingPassword.value = true;
  try {
    await session.updateAccount({ currentPassword: currentPassword.value, newPassword: newPassword.value });
    notifySuccess(t('auth.passwordChangedRelogin'));
    window.location.assign('/login?changed=1');
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('auth.changePasswordFailed'), err);
  } finally {
    savingPassword.value = false;
  }
}

async function loadCredentials() {
  credentialsLoading.value = true;
  try {
    credentials.value = (await accessCredentialsApi.list()).items;
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('account.credentials.loadFailed'), err);
  } finally {
    credentialsLoading.value = false;
  }
}

function openCreate() {
  name.value = '';
  minutes.value = 15;
  scope.value = 'readonly';
  allowManagement.value = false;
  created.value = null;
  createOpen.value = true;
}

async function createCredential() {
  if (!name.value.trim()) return;
  creating.value = true;
  try {
    created.value = await accessCredentialsApi.create({
      name: name.value.trim(),
      expiresInSeconds: Math.min(30, Math.max(1, minutes.value)) * 60,
      scope: scope.value,
      allowManagement: scope.value === 'readwrite' && allowManagement.value,
    });
    await loadCredentials();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('account.credentials.createFailed'), err);
  } finally {
    creating.value = false;
  }
}

async function copyToken() {
  if (!created.value) return;
  try {
    await navigator.clipboard.writeText(created.value.token);
    notifySuccess(t('common.copied'));
  } catch {
    // Clipboard access may be denied; the token stays selectable in the field.
  }
}

async function revoke() {
  if (!revokeTarget.value) return;
  try {
    await accessCredentialsApi.revoke(revokeTarget.value.id);
    notifySuccess(t('account.credentials.revoked'));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('account.credentials.revokeFailed'), err);
  } finally {
    revokeTarget.value = null;
    await loadCredentials();
  }
}

onMounted(loadCredentials);
</script>

<template>
  <ConsolePage :title="t('routes.account.title')" :description="t('account.description')" width="narrow">
    <div class="grid min-h-0 gap-6 overflow-auto">
      <SettingsSection :title="t('account.profile.title')" :description="t('account.profile.description')">
        <SettingsCard>
          <div class="grid gap-1">
            <span class="text-sm font-medium text-foreground">{{ session.user?.username }}</span>
            <span class="text-xs text-muted-foreground">{{ session.user?.displayName }}</span>
            <span class="text-xs text-muted-foreground">{{ t('account.profile.roles') }}: {{ session.roles.map((role) => role.name).join(', ') || '—' }}</span>
          </div>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection :title="t('account.password.title')" :description="t('account.password.description')">
        <SettingsCard>
          <form class="grid gap-4" @submit.prevent="savePassword">
            <FormField :label="t('auth.currentPassword')">
              <template #default="{ id, describedBy, invalid }">
                <Input :id="id" v-model="currentPassword" type="password" autocomplete="current-password" :aria-describedby="describedBy" :invalid="invalid" />
              </template>
            </FormField>
            <FormField :label="t('auth.newPassword')">
              <template #default="{ id, describedBy, invalid }">
                <Input :id="id" v-model="newPassword" type="password" autocomplete="new-password" :aria-describedby="describedBy" :invalid="invalid" />
              </template>
            </FormField>
            <FormField :label="t('auth.confirmPassword')">
              <template #default="{ id, describedBy, invalid }">
                <Input :id="id" v-model="confirmPassword" type="password" autocomplete="new-password" :aria-describedby="describedBy" :invalid="invalid" />
              </template>
            </FormField>
            <InfoBar v-if="passwordError" tone="danger">{{ passwordError }}</InfoBar>
            <div>
              <Button type="submit" variant="primary" :loading="savingPassword">{{ t('common.save') }}</Button>
            </div>
          </form>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection :title="t('account.credentials.title')" :description="t('account.credentials.description')">
        <div class="grid gap-3">
          <div class="flex justify-end">
            <Button variant="primary" @click="openCreate">
              <Plus />{{ t('account.credentials.create') }}
            </Button>
          </div>
          <Table v-if="credentials.length || credentialsLoading" :columns="credentialColumns" :rows="credentials" row-key="id" :loading="credentialsLoading && !credentials.length" :loading-label="t('common.loading')">
            <template #name="{ row }">{{ row.name }}</template>
            <template #scope="{ row }">{{ t(`account.credentials.scopeValue.${row.scope}`) }}</template>
            <template #status="{ row }">{{ t(`account.credentials.statusValue.${row.status}`) }}</template>
            <template #expiresAt="{ row }">{{ formatDateTime(row.expiresAt) }}</template>
            <template #actions="{ row }">
              <IconButton :label="t('account.credentials.revoke')" :disabled="row.status !== 'active'" @click="revokeTarget = row">
                <Trash2 />
              </IconButton>
            </template>
          </Table>
          <EmptyState v-else-if="!credentialsLoading" :title="t('account.credentials.empty')" />
        </div>
      </SettingsSection>
    </div>

    <Dialog v-model:open="createOpen" :title="t('account.credentials.createTitle')" width="wide">
      <div class="grid gap-4">
        <template v-if="!created">
          <FormField :label="t('account.credentials.name')">
            <template #default="{ id, describedBy, invalid }">
              <Input :id="id" v-model="name" :aria-describedby="describedBy" :invalid="invalid" />
            </template>
          </FormField>
          <FormField :label="t('account.credentials.expiry')">
            <template #default="{ id, describedBy, invalid }">
              <NumberBox :id="id" v-model="minutes" :min="1" :max="30" :aria-describedby="describedBy" :invalid="invalid" />
            </template>
          </FormField>
          <div class="grid gap-2">
            <span class="text-sm font-medium text-foreground">{{ t('account.credentials.scope') }}</span>
            <RadioButton v-model="scope" value="readonly" :label="t('account.credentials.scopeValue.readonly')" />
            <RadioButton v-model="scope" value="readwrite" :label="t('account.credentials.scopeValue.readwrite')" />
          </div>
          <template v-if="canManageAccess">
            <Checkbox v-model="allowManagement" :disabled="scope !== 'readwrite'" :label="t('account.credentials.allowManagement')" />
            <InfoBar tone="warning">{{ t('account.credentials.allowManagementHint') }}</InfoBar>
          </template>
        </template>
        <template v-else>
          <InfoBar tone="warning">{{ t('account.credentials.tokenOnce') }}</InfoBar>
          <div class="flex gap-2">
            <Input :model-value="created.token" readonly class="font-mono" />
            <IconButton :label="t('common.copy')" @click="copyToken"><ClipboardList /></IconButton>
          </div>
          <p class="m-0 text-xs text-muted-foreground">{{ t('account.credentials.expiresAt') }}: {{ formatDateTime(created.expiresAt) }}</p>
        </template>
      </div>
      <template #footer>
        <Button v-if="created" variant="primary" @click="createOpen = false">{{ t('common.close') }}</Button>
        <template v-else>
          <Button @click="createOpen = false">{{ t('common.cancel') }}</Button>
          <Button variant="primary" :loading="creating" @click="createCredential">{{ t('common.create') }}</Button>
        </template>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="Boolean(revokeTarget)"
      :title="t('account.credentials.revokeTitle')"
      :description="revokeTarget ? t('account.credentials.revokeConfirm', { name: revokeTarget.name }) : ''"
      :confirm-label="t('account.credentials.revoke')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @update:open="(open) => { if (!open) revokeTarget = null; }"
      @confirm="revoke"
    />
  </ConsolePage>
</template>
