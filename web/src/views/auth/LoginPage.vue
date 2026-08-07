<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { settingsApi } from '@/api/settings';
import Button from '@/components/ui/Button.vue';
import Input from '@/components/ui/Input.vue';
import { useErrorToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';

const router = useRouter();
const route = useRoute();
const session = useSessionStore();
const { t } = useI18n();
const notifyError = useErrorToast();
const username = ref('');
const password = ref('');
const accountUsername = ref('');
const currentPassword = ref('');
const newPassword = ref('');
const confirmPassword = ref('');
const loading = ref(false);
const error = ref('');
const branding = ref({ loginTitle: '', loginSubtitle: '' });
const title = computed(() => branding.value.loginTitle || t('app.name'));
const subtitle = computed(() => branding.value.loginSubtitle || t('app.subtitle'));
const changingPassword = computed(() => session.authenticated && session.passwordChangeRequired);
const pageTitle = computed(() => changingPassword.value ? t('routes.changePassword.title') : t('routes.login.title'));

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

async function submit() {
  error.value = '';
  if (!username.value || !password.value) {
    error.value = t('auth.required');
    return;
  }
  loading.value = true;
  try {
    await session.signIn(username.value, password.value);
    if (session.passwordChangeRequired) {
      accountUsername.value = session.username || username.value;
      currentPassword.value = password.value;
      newPassword.value = '';
      confirmPassword.value = '';
      return;
    }
    await router.push(String(route.query.redirect || '/overview'));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('auth.signInFailed'));
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
    await router.push(String(route.query.redirect || '/overview'));
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('auth.changePasswordFailed'));
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <main class="grid min-h-dvh place-items-center bg-background p-6 pb-24">
    <section class="w-full max-w-[420px] rounded-2xl border border-border bg-card p-6 shadow-2xl shadow-black/[0.04]">
      <div class="mb-6 flex items-center gap-3">
        <img src="/favicon.svg" class="size-10 rounded-xl" alt="" aria-hidden="true" />
        <div>
          <strong class="block text-sm font-semibold text-foreground">{{ title }}</strong>
          <span class="block text-xs text-muted-foreground">{{ subtitle }}</span>
        </div>
      </div>
      <h1 class="m-0 text-xl font-semibold text-foreground">{{ pageTitle }}</h1>
      <p v-if="changingPassword" class="m-0 mt-2 text-sm text-muted-foreground">{{ t('auth.passwordChangeRequired') }}</p>
      <form v-if="!changingPassword" class="mt-6 grid gap-4" @submit.prevent="submit">
        <label class="grid gap-1.5 text-sm font-medium text-foreground">
          {{ t('auth.username') }}
          <Input v-model="username" autocomplete="username" autofocus />
        </label>
        <label class="grid gap-1.5 text-sm font-medium text-foreground">
          {{ t('auth.password') }}
          <Input v-model="password" type="password" autocomplete="current-password" />
        </label>
        <p v-if="error" class="m-0 rounded-xl border border-danger-border bg-danger-bg px-3 py-2 text-sm text-danger">{{ error }}</p>
        <Button type="submit" variant="primary" :loading="loading">{{ t('auth.signIn') }}</Button>
      </form>
      <form v-else class="mt-6 grid gap-4" @submit.prevent="updateAccount">
        <label class="grid gap-1.5 text-sm font-medium text-foreground">
          {{ t('auth.username') }}
          <Input v-model="accountUsername" autocomplete="username" />
        </label>
        <label class="grid gap-1.5 text-sm font-medium text-foreground">
          {{ t('auth.currentPassword') }}
          <Input v-model="currentPassword" type="password" autocomplete="current-password" />
        </label>
        <label class="grid gap-1.5 text-sm font-medium text-foreground">
          {{ t('auth.newPassword') }}
          <Input v-model="newPassword" type="password" autocomplete="new-password" />
        </label>
        <label class="grid gap-1.5 text-sm font-medium text-foreground">
          {{ t('auth.confirmPassword') }}
          <Input v-model="confirmPassword" type="password" autocomplete="new-password" />
        </label>
        <p v-if="error" class="m-0 rounded-xl border border-danger-border bg-danger-bg px-3 py-2 text-sm text-danger">{{ error }}</p>
        <Button type="submit" variant="primary" :loading="loading">{{ t('auth.updatePassword') }}</Button>
      </form>
    </section>
    <footer class="fixed bottom-5 left-1/2 z-20 flex -translate-x-1/2 items-center gap-2">
      <img src="/favicon.svg" class="size-7 rounded-lg" alt="" aria-hidden="true" />
      <span class="text-[13px] font-semibold text-foreground/70">{{ t('app.name') }}</span>
    </footer>
  </main>
</template>
