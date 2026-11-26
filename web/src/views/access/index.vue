<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Plus, Trash2 } from '@/icons';
import {
  permissionsApi,
  rolesApi,
  usersApi,
  type AccessRole,
  type AccessUser,
  type PermissionCatalog,
} from '@/api/access';
import Button from '@/components/ui/Button.vue';
import Checkbox from '@/components/ui/Checkbox.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import Tabs from '@/components/ui/Tabs.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import FormField from '@/components/patterns/FormField.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';
import { formatDateTime } from '@/utils/datetime';

const { t } = useI18n();
const session = useSessionStore();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const canRoles = computed(() => session.can('access.roles.manage'));
const section = computed(() => (route.params.section === 'roles' && canRoles.value ? 'roles' : 'users'));
const tabs = computed(() => [
  { label: t('access.users.tab'), value: 'users' },
  ...(canRoles.value ? [{ label: t('access.roles.tab'), value: 'roles' }] : []),
]);
const catalog = ref<PermissionCatalog>({ groups: [], permissions: [] });
const users = ref<AccessUser[]>([]);
const roles = ref<AccessRole[]>([]);
const loading = ref(false);

const userDialogOpen = ref(false);
const editingUser = ref<AccessUser | null>(null);
const userForm = ref({ username: '', displayName: '', password: '', roleIds: [] as string[] });
const userError = ref('');
const savingUser = ref(false);
const deleteUserTarget = ref<AccessUser | null>(null);
const forceLogoutTarget = ref<AccessUser | null>(null);

const resetPasswordTarget = ref<AccessUser | null>(null);
const resetPasswordValue = ref('');
const resetPasswordError = ref('');
const resettingPassword = ref(false);

const userSearch = ref('');
const userStatusFilter = ref('');
const statusOptions = computed(() => [
  { label: t('access.users.allStatuses'), value: '' },
  { label: t('access.users.statusActive'), value: 'active' },
  { label: t('access.users.statusDisabled'), value: 'disabled' },
]);

const roleDialogOpen = ref(false);
const editingRole = ref<AccessRole | null>(null);
const roleForm = ref({ name: '', description: '', permissionKeys: [] as string[] });
const roleError = ref('');
const savingRole = ref(false);
const deleteRoleTarget = ref<AccessRole | null>(null);

const userColumns = computed(() => [
  { key: 'username', label: t('access.users.username') },
  { key: 'roles', label: t('access.users.roles') },
  { key: 'status', label: t('common.status') },
  { key: 'lastLoginAt', label: t('access.users.lastLogin') },
  { key: 'actions', label: '', align: 'right' as const },
]);
const roleColumns = computed(() => [
  { key: 'name', label: t('access.roles.name') },
  { key: 'permissions', label: t('access.roles.permissions') },
  { key: 'users', label: t('access.roles.users') },
  { key: 'actions', label: '', align: 'right' as const },
]);
const groupLabel = (key: string) => {
  const group = catalog.value.groups.find((item) => item.key === key);
  return group ? t(group.labelKey) : key;
};
const groupedPermissions = computed(() => catalog.value.groups.map((group) => ({
  group,
  items: catalog.value.permissions.filter((permission) => permission.group === group.key && permission.grantable),
})).filter((entry) => entry.items.length > 0));

async function loadUsers() {
  const userPage = await usersApi.list({
    pageSize: 200,
    q: userSearch.value.trim() || undefined,
    status: userStatusFilter.value || undefined,
  });
  users.value = userPage.items;
}

async function load() {
  loading.value = true;
  try {
    const [, roleResult, catalogResult] = await Promise.all([
      loadUsers(),
      canRoles.value ? rolesApi.list() : Promise.resolve({ items: [] as AccessRole[] }),
      permissionsApi.catalog(),
    ]);
    roles.value = roleResult.items;
    catalog.value = catalogResult;
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('access.loadFailed'), err);
  } finally {
    loading.value = false;
  }
}

function switchSection(value: string) {
  void router.push({ path: `/access/${value}` });
}

function openCreateUser() {
  editingUser.value = null;
  userForm.value = { username: '', displayName: '', password: '', roleIds: [] };
  userError.value = '';
  userDialogOpen.value = true;
}

function openEditUser(user: AccessUser) {
  editingUser.value = user;
  userForm.value = {
    username: user.username,
    displayName: user.displayName,
    password: '',
    roleIds: (user.roles ?? []).map((role) => role.id),
  };
  userError.value = '';
  userDialogOpen.value = true;
}

async function saveUser() {
  userError.value = '';
  savingUser.value = true;
  try {
    if (editingUser.value) {
      await usersApi.update(editingUser.value.id, {
        displayName: userForm.value.displayName.trim(),
        roleIds: userForm.value.roleIds,
      });
      userDialogOpen.value = false;
      notifySuccess(t('access.users.updated'));
    } else {
      await usersApi.create({
        username: userForm.value.username.trim(),
        displayName: userForm.value.displayName.trim(),
        password: userForm.value.password,
        roleIds: userForm.value.roleIds,
      });
      userDialogOpen.value = false;
      notifySuccess(t('access.users.created'));
    }
    await load();
  } catch (err) {
    userError.value = err instanceof Error
      ? err.message
      : t(editingUser.value ? 'access.users.updateFailed' : 'access.users.createFailed');
  } finally {
    savingUser.value = false;
  }
}

function openResetPassword(user: AccessUser) {
  resetPasswordTarget.value = user;
  resetPasswordValue.value = '';
  resetPasswordError.value = '';
}

async function submitResetPassword() {
  if (!resetPasswordTarget.value) return;
  resetPasswordError.value = '';
  if (resetPasswordValue.value.length < 8) {
    resetPasswordError.value = t('auth.newPasswordTooShort');
    return;
  }
  resettingPassword.value = true;
  try {
    await usersApi.resetPassword(resetPasswordTarget.value.id, resetPasswordValue.value);
    resetPasswordTarget.value = null;
    resetPasswordValue.value = '';
    notifySuccess(t('access.users.passwordReset'));
    await load();
  } catch (err) {
    resetPasswordError.value = err instanceof Error ? err.message : t('access.users.resetPasswordFailed');
  } finally {
    resettingPassword.value = false;
  }
}

async function confirmForceLogout() {
  if (!forceLogoutTarget.value) return;
  try {
    await usersApi.forceLogout(forceLogoutTarget.value.id);
    notifySuccess(t('access.users.forcedLogout'));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('access.users.forceLogoutFailed'), err);
  } finally {
    forceLogoutTarget.value = null;
  }
}

async function toggleUserStatus(user: AccessUser) {
  try {
    await usersApi.update(user.id, { status: user.status === 'active' ? 'disabled' : 'active' });
    await load();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('access.users.updateFailed'), err);
  }
}

async function deleteUser() {
  if (!deleteUserTarget.value) return;
  try {
    await usersApi.remove(deleteUserTarget.value.id);
    notifySuccess(t('access.users.deleted'));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('access.users.deleteFailed'), err);
  } finally {
    deleteUserTarget.value = null;
    await load();
  }
}

function openCreateRole() {
  editingRole.value = null;
  roleForm.value = { name: '', description: '', permissionKeys: [] };
  roleError.value = '';
  roleDialogOpen.value = true;
}

function openEditRole(role: AccessRole) {
  editingRole.value = role;
  roleForm.value = { name: role.name, description: role.description, permissionKeys: [...role.permissionKeys] };
  roleError.value = '';
  roleDialogOpen.value = true;
}

function togglePermission(key: string, checked: boolean) {
  const set = new Set(roleForm.value.permissionKeys);
  const byKey = new Map(catalog.value.permissions.map((permission) => [permission.key, permission]));
  const addWithDependencies = (value: string) => {
    if (set.has(value)) return;
    set.add(value);
    for (const dependency of byKey.get(value)?.requires ?? []) addWithDependencies(dependency);
  };
  if (checked) {
    addWithDependencies(key);
  } else {
    const removal = new Set([key]);
    let changed = true;
    while (changed) {
      changed = false;
      for (const permission of catalog.value.permissions) {
        if (removal.has(permission.key)) continue;
        if ((permission.requires ?? []).some((dependency) => removal.has(dependency))) {
          removal.add(permission.key);
          changed = true;
        }
      }
    }
    for (const value of removal) set.delete(value);
  }
  roleForm.value.permissionKeys = [...set];
}

async function saveRole() {
  roleError.value = '';
  savingRole.value = true;
  try {
    const payload = {
      name: roleForm.value.name.trim(),
      description: roleForm.value.description.trim(),
      permissionKeys: roleForm.value.permissionKeys,
    };
    if (editingRole.value) await rolesApi.update(editingRole.value.id, payload);
    else await rolesApi.create(payload);
    roleDialogOpen.value = false;
    notifySuccess(t('access.roles.saved'));
    await load();
  } catch (err) {
    roleError.value = err instanceof Error ? err.message : t('access.roles.saveFailed');
  } finally {
    savingRole.value = false;
  }
}

async function deleteRole() {
  if (!deleteRoleTarget.value) return;
  try {
    await rolesApi.remove(deleteRoleTarget.value.id);
    notifySuccess(t('access.roles.deleted'));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('access.roles.deleteFailed'), err);
  } finally {
    deleteRoleTarget.value = null;
    await load();
  }
}

let searchTimer: ReturnType<typeof setTimeout> | null = null;
watch([userSearch, userStatusFilter], () => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    void loadUsers().catch((err) => {
      notifyError(err instanceof Error ? err.message : t('access.loadFailed'), err);
    });
  }, 200);
});

onMounted(load);
</script>

<template>
  <ConsolePage :title="t('routes.access.title')" :description="t('access.description')">
    <div class="grid min-h-0 gap-3 overflow-auto">
      <Tabs :tabs="tabs" :model-value="section" @update:model-value="switchSection" />
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div v-if="section === 'users'" class="flex flex-wrap items-center gap-2">
          <Input
            v-model="userSearch"
            class="w-60"
            :placeholder="t('access.users.searchPlaceholder')"
          />
          <Select
            v-model="userStatusFilter"
            class="w-36"
            :options="statusOptions"
          />
        </div>
        <div v-else />
        <Button v-if="section === 'users'" variant="primary" @click="openCreateUser"><Plus />{{ t('access.users.create') }}</Button>
        <Button v-else variant="primary" @click="openCreateRole"><Plus />{{ t('access.roles.create') }}</Button>
      </div>

      <template v-if="section === 'users'">
        <Table v-if="users.length || loading" :columns="userColumns" :rows="users" row-key="id" :loading="loading && !users.length" :loading-label="t('common.loading')">
          <template #username="{ row }">
            <span class="font-medium">{{ row.username }}</span>
            <span v-if="row.builtin" class="ml-2 text-xs text-muted-foreground">{{ t('access.users.builtin') }}</span>
          </template>
          <template #roles="{ row }">{{ (row.roles ?? []).map((role) => role.name).join(', ') || '—' }}</template>
          <template #status="{ row }">{{
            row.status === 'active' ? t('access.users.statusActive') : t('access.users.statusDisabled')
          }}</template>
          <template #lastLoginAt="{ row }">{{ row.lastLoginAt ? formatDateTime(row.lastLoginAt) : '—' }}</template>
          <template #actions="{ row }">
            <div class="flex justify-end gap-1">
              <Button v-if="!row.builtin" size="sm" @click="openEditUser(row)">{{ t('common.edit') }}</Button>
              <Button v-if="!row.builtin" size="sm" @click="openResetPassword(row)">{{ t('access.users.resetPassword') }}</Button>
              <Button v-if="!row.builtin && row.id !== session.user?.id" size="sm" @click="forceLogoutTarget = row">
                {{ t('access.users.forceLogout') }}
              </Button>
              <Button v-if="!row.builtin && row.id !== session.user?.id" size="sm" @click="toggleUserStatus(row)">
                {{ row.status === 'active' ? t('access.users.disable') : t('access.users.enable') }}
              </Button>
              <IconButton v-if="!row.builtin && row.id !== session.user?.id" :label="t('common.delete')" @click="deleteUserTarget = row"><Trash2 /></IconButton>
            </div>
          </template>
        </Table>
        <EmptyState v-else-if="!loading" :title="t('access.users.empty')" />
      </template>

      <template v-else>
        <Table v-if="roles.length || loading" :columns="roleColumns" :rows="roles" row-key="id" :loading="loading && !roles.length" :loading-label="t('common.loading')">
          <template #name="{ row }">
            <span class="font-medium">{{ row.name }}</span>
            <span v-if="row.builtin" class="ml-2 text-xs text-muted-foreground">{{ t('access.roles.builtin') }}</span>
          </template>
          <template #permissions="{ row }">{{ row.builtin ? t('access.roles.allPermissions') : (row.permissionKeys ?? []).length }}</template>
          <template #users="{ row }">{{ row.userCount }}</template>
          <template #actions="{ row }">
            <div class="flex justify-end gap-1">
              <Button v-if="!row.builtin" size="sm" @click="openEditRole(row)">{{ t('common.edit') }}</Button>
              <IconButton v-if="!row.builtin" :label="t('common.delete')" @click="deleteRoleTarget = row"><Trash2 /></IconButton>
            </div>
          </template>
        </Table>
        <EmptyState v-else-if="!loading" :title="t('access.roles.empty')" />
      </template>
    </div>

    <Dialog v-model:open="userDialogOpen" :title="editingUser ? t('access.users.editTitle') : t('access.users.createTitle')" width="wide">
      <div class="grid gap-4">
        <FormField :label="t('access.users.username')">
          <template #default="{ id }"><Input :id="id" v-model="userForm.username" :disabled="Boolean(editingUser)" autocomplete="off" /></template>
        </FormField>
        <FormField :label="t('access.users.displayName')">
          <template #default="{ id }"><Input :id="id" v-model="userForm.displayName" autocomplete="off" /></template>
        </FormField>
        <FormField v-if="!editingUser" :label="t('access.users.initialPassword')">
          <template #default="{ id }"><Input :id="id" v-model="userForm.password" type="password" autocomplete="new-password" /></template>
        </FormField>
        <div class="grid gap-2">
          <span class="text-sm font-medium text-foreground">{{ t('access.users.roles') }}</span>
          <EmptyState v-if="!roles.length" :title="t('access.users.noRoles')" />
          <Checkbox
            v-for="role in roles"
            :key="role.id"
            :model-value="userForm.roleIds.includes(role.id)"
            :label="role.name"
            :disabled="role.builtin"
            @update:model-value="(checked) => {
              const set = new Set(userForm.roleIds);
              if (checked) set.add(role.id); else set.delete(role.id);
              userForm.roleIds = [...set];
            }"
          />
        </div>
        <InfoBar v-if="userError" tone="danger">{{ userError }}</InfoBar>
      </div>
      <template #footer>
        <Button @click="userDialogOpen = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="savingUser" @click="saveUser">{{ editingUser ? t('common.save') : t('common.create') }}</Button>
      </template>
    </Dialog>

    <Dialog
      :open="Boolean(resetPasswordTarget)"
      :title="t('access.users.resetPasswordTitle')"
      @update:open="(open) => { if (!open) resetPasswordTarget = null; }"
    >
      <div class="grid gap-4">
        <FormField :label="t('auth.newPassword')">
          <template #default="{ id }">
            <Input :id="id" v-model="resetPasswordValue" type="password" autocomplete="new-password" />
          </template>
        </FormField>
        <InfoBar v-if="resetPasswordError" tone="danger">{{ resetPasswordError }}</InfoBar>
      </div>
      <template #footer>
        <Button @click="resetPasswordTarget = null">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="resettingPassword" @click="submitResetPassword">{{ t('common.save') }}</Button>
      </template>
    </Dialog>

    <Dialog v-model:open="roleDialogOpen" :title="editingRole ? t('access.roles.editTitle') : t('access.roles.createTitle')" width="wide">
      <div class="grid gap-4">
        <FormField :label="t('access.roles.name')">
          <template #default="{ id }"><Input :id="id" v-model="roleForm.name" /></template>
        </FormField>
        <FormField :label="t('access.roles.description')">
          <template #default="{ id }"><Input :id="id" v-model="roleForm.description" /></template>
        </FormField>
        <div class="grid max-h-96 gap-4 overflow-auto pr-1">
          <div v-for="entry in groupedPermissions" :key="entry.group.key" class="grid gap-2">
            <span class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{{ t(entry.group.labelKey) }}</span>
            <div v-for="permission in entry.items" :key="permission.key" class="grid gap-0.5">
              <Checkbox
                :model-value="roleForm.permissionKeys.includes(permission.key)"
                :label="t(permission.labelKey)"
                @update:model-value="(checked) => togglePermission(permission.key, checked)"
              />
              <span class="pl-7 text-xs text-muted-foreground">
                {{ permission.mode === 'read' ? t('access.roles.readOnly') : t('access.roles.readWrite') }}
                · {{ permission.ui.length }} {{ t('access.roles.uiTargets') }}
                <template v-if="permission.requires?.length">
                  · {{ t('access.roles.dependsOn') }} {{ permission.requires.map((key) => t(catalog.permissions.find((item) => item.key === key)?.labelKey ?? key)).join(', ') }}
                </template>
              </span>
            </div>
          </div>
        </div>
        <InfoBar v-if="roleError" tone="danger">{{ roleError }}</InfoBar>
      </div>
      <template #footer>
        <Button @click="roleDialogOpen = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="savingRole" @click="saveRole">{{ t('common.save') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="Boolean(forceLogoutTarget)"
      :title="t('access.users.forceLogoutTitle')"
      :description="forceLogoutTarget ? t('access.users.forceLogoutConfirm', { name: forceLogoutTarget.username }) : ''"
      :confirm-label="t('access.users.forceLogout')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @update:open="(open) => { if (!open) forceLogoutTarget = null; }"
      @confirm="confirmForceLogout"
    />
    <ConfirmDialog
      :open="Boolean(deleteUserTarget)"
      :title="t('access.users.deleteTitle')"
      :description="deleteUserTarget ? t('access.users.deleteConfirm', { name: deleteUserTarget.username }) : ''"
      :confirm-label="t('common.delete')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @update:open="(open) => { if (!open) deleteUserTarget = null; }"
      @confirm="deleteUser"
    />
    <ConfirmDialog
      :open="Boolean(deleteRoleTarget)"
      :title="t('access.roles.deleteTitle')"
      :description="deleteRoleTarget ? t('access.roles.deleteConfirm', { name: deleteRoleTarget.name }) : ''"
      :confirm-label="t('common.delete')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @update:open="(open) => { if (!open) deleteRoleTarget = null; }"
      @confirm="deleteRole"
    />
  </ConsolePage>
</template>
