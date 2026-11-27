<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { settingsApi } from '@/api/settings';
import { defaultLandingPath } from '@/components/shell/navModel';
import Button from '@/components/ui/Button.vue';
import Input from '@/components/ui/Input.vue';
import Tabs from '@/components/ui/Tabs.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import FormField from '@/components/patterns/FormField.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';

const router = useRouter();
const route = useRoute();
const session = useSessionStore();
const { t } = useI18n();
const notifyError = useErrorDialog();
const loginMode = ref<'password' | 'credential'>('password');
const loginTabs = computed(() => [
  { label: t('auth.loginMode.password'), value: 'password' },
  { label: t('auth.loginMode.credential'), value: 'credential' },
]);
const username = ref('');
const password = ref('');
const credentialToken = ref('');
const accountUsername = ref('');
const currentPassword = ref('');
const newPassword = ref('');
const confirmPassword = ref('');
const loading = ref(false);
const error = ref('');
const branding = ref({ loginTitle: '', loginSubtitle: '' });
const title = computed(() => branding.value.loginTitle || t('app.name'));
const subtitle = computed(() => branding.value.loginSubtitle || t('app.subtitle'));
const changingPassword = computed(() => session.authenticated && session.mustChangePassword);
const pageTitle = computed(() => changingPassword.value ? t('routes.changePassword.title') : t('routes.login.title'));
const notice = computed(() => {
  if (route.query.setup === '1') return t('auth.setupComplete');
  if (route.query.changed === '1') return t('auth.passwordChangedRelogin');
  return '';
});

watch(changingPassword, (required) => {
  if (required && !accountUsername.value) accountUsername.value = session.username || username.value;
}, { immediate: true });

onMounted(async () => {
  try {
    branding.value = await settingsApi.publicBranding();
  } catch {
    branding.value = { loginTitle: '', loginSubtitle: '' };
  }
});

function safeRedirect(value: unknown): string {
  const fallback = defaultLandingPath((permission) => session.can(permission));
  if (typeof value !== 'string') return fallback;
  if (!value.startsWith('/') || value.startsWith('//')) return fallback;
  return value;
}

async function signOut() {
  loading.value = true;
  try {
    await session.logout();
    password.value = '';
    currentPassword.value = '';
    newPassword.value = '';
    confirmPassword.value = '';
    error.value = '';
  } finally {
    loading.value = false;
  }
}

watch(loginMode, () => {
  error.value = '';
});

async function submitCredential() {
  error.value = '';
  const token = credentialToken.value.trim();
  if (!token) {
    error.value = t('auth.credentialRequired');
    return;
  }
  loading.value = true;
  try {
    await session.signInWithCredential(token);
    await router.push(safeRedirect(route.query.redirect));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('auth.signInFailed'), err);
    credentialToken.value = '';
  } finally {
    loading.value = false;
  }
}

async function submit() {
  error.value = '';
  if (!username.value || !password.value) {
    error.value = t('auth.required');
    return;
  }
  loading.value = true;
  try {
    await session.signIn(username.value, password.value);
    if (session.mustChangePassword) {
      accountUsername.value = session.username || username.value;
      currentPassword.value = password.value;
      newPassword.value = '';
      confirmPassword.value = '';
      return;
    }
    await router.push(safeRedirect(route.query.redirect));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('auth.signInFailed'), err);
    password.value = '';
  } finally {
    loading.value = false;
  }
}

async function updateAccount() {
  error.value = '';
  if (!accountUsername.value || !currentPassword.value || !newPassword.value) {
    error.value = t('auth.changePasswordRequiredFields');
    return;
  }
  if (newPassword.value.length < 8) {
    error.value = t('auth.newPasswordTooShort');
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    error.value = t('auth.passwordsDoNotMatch');
    return;
  }
  loading.value = true;
  try {
    await session.updateAccount({
      username: accountUsername.value,
      currentPassword: currentPassword.value,
      newPassword: newPassword.value,
    });
    await router.push({ path: '/login', query: { changed: '1' } });
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('auth.changePasswordFailed'), err);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <!--
    聚焦面例外（检查表登记；文档由 Lead 统一更新）：登录 / 首次改密是无 AppShell 的
    独立聚焦页，品牌卡按视口水平 + 垂直居中（place-items-center），不套用 AppShell
    页面的 max-w-content-narrow(1000) 左对齐内容列；maintenance 已改为左对齐 1000 列。
  -->
  <main class="grid min-h-dvh place-items-center p-6 pb-24">
    <section class="w-full max-w-md surface-card p-5">
      <div class="mb-6 flex items-center gap-3">
        <img src="/favicon.svg" class="size-10 rounded-control border border-divider" alt="" aria-hidden="true" />
        <div>
          <strong class="block text-sm font-semibold tracking-tight text-foreground">{{ title }}</strong>
          <span class="block text-xs text-secondary-foreground">{{ subtitle }}</span>
        </div>
      </div>
      <h1 class="m-0 text-title font-semibold tracking-tight text-foreground">{{ pageTitle }}</h1>
      <p v-if="changingPassword" class="m-0 mt-2 text-sm text-muted-foreground">{{ t('auth.passwordChangeRequired') }}</p>
      <InfoBar v-if="notice" class="mt-4" tone="success">{{ notice }}</InfoBar>
      <template v-if="!changingPassword">
        <Tabs
          class="mt-4"
          :tabs="loginTabs"
          :model-value="loginMode"
          @update:model-value="(value) => { loginMode = value as 'password' | 'credential'; }"
        />
        <form v-if="loginMode === 'password'" class="mt-5 grid gap-4" @submit.prevent="submit">
          <FormField :label="t('auth.username')">
            <template #default="{ id, describedBy, invalid }">
              <Input :id="id" v-model="username" autocomplete="username" autofocus :aria-describedby="describedBy" :invalid="invalid" />
            </template>
          </FormField>
          <FormField :label="t('auth.password')">
            <template #default="{ id, describedBy, invalid }">
              <Input :id="id" v-model="password" type="password" autocomplete="current-password" :aria-describedby="describedBy" :invalid="invalid" />
            </template>
          </FormField>
          <InfoBar v-if="error" tone="danger">{{ error }}</InfoBar>
          <Button type="submit" variant="primary" :loading="loading">{{ t('auth.signIn') }}</Button>
        </form>
        <form v-else class="mt-5 grid gap-4" @submit.prevent="submitCredential">
          <FormField :label="t('auth.credential')" :hint="t('auth.credentialHint')">
            <template #default="{ id, describedBy, invalid }">
              <Input
                :id="id"
                v-model="credentialToken"
                type="password"
                autocomplete="off"
                :placeholder="t('auth.credentialPlaceholder')"
                :aria-describedby="describedBy"
                :invalid="invalid"
              />
            </template>
          </FormField>
          <InfoBar v-if="error" tone="danger">{{ error }}</InfoBar>
          <Button type="submit" variant="primary" :loading="loading">{{ t('auth.signIn') }}</Button>
        </form>
      </template>
      <form v-else class="mt-6 grid gap-4" @submit.prevent="updateAccount">
        <FormField :label="t('auth.username')">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" v-model="accountUsername" autocomplete="username" :aria-describedby="describedBy" :invalid="invalid" />
          </template>
        </FormField>
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
        <InfoBar v-if="error" tone="danger">{{ error }}</InfoBar>
        <div class="grid gap-2">
          <Button type="submit" variant="primary" :loading="loading">{{ t('auth.updatePassword') }}</Button>
          <Button type="button" variant="ghost" :disabled="loading" @click="signOut">{{ t('layout.logout') }}</Button>
        </div>
      </form>
    </section>
    <footer class="fixed bottom-5 left-1/2 z-20 flex -translate-x-1/2 items-center gap-2">
      <img src="/favicon.svg" class="size-7 rounded-control" alt="" aria-hidden="true" />
      <span class="text-sm font-semibold text-foreground/70">{{ t('app.name') }}</span>
    </footer>
  </main>
</template>
