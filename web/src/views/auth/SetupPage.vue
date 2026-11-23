<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { setupApi } from '@/api/auth';
import { settingsApi } from '@/api/settings';
import Button from '@/components/ui/Button.vue';
import Input from '@/components/ui/Input.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import FormField from '@/components/patterns/FormField.vue';
import { useI18n } from '@/i18n';
import { useSessionStore } from '@/stores/session';

const router = useRouter();
const session = useSessionStore();
const { t } = useI18n();
const branding = ref({ loginTitle: '', loginSubtitle: '' });
const username = ref('admin');
const password = ref('admin');
const confirmPassword = ref('admin');
const loading = ref(false);
const error = ref('');
const title = computed(() => branding.value.loginTitle || t('app.name'));
const subtitle = computed(() => branding.value.loginSubtitle || t('app.subtitle'));

onMounted(async () => {
  try {
    branding.value = await settingsApi.publicBranding();
  } catch {
    branding.value = { loginTitle: '', loginSubtitle: '' };
  }
});

async function submit() {
  error.value = '';
  if (!username.value.trim() || !password.value) {
    error.value = t('auth.required');
    return;
  }
  if (password.value !== confirmPassword.value) {
    error.value = t('auth.passwordsDoNotMatch');
    return;
  }
  loading.value = true;
  try {
    await setupApi.create(username.value.trim(), password.value);
    session.markSetupComplete();
    await router.push({ path: '/login', query: { setup: '1' } });
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('auth.setupFailed');
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <main class="grid min-h-dvh place-items-center p-6 pb-24">
    <section class="w-full max-w-md surface-card p-5">
      <div class="mb-6 flex items-center gap-3">
        <img src="/favicon.svg" class="size-10 rounded-control border border-divider" alt="" aria-hidden="true" />
        <div>
          <strong class="block text-sm font-semibold tracking-tight text-foreground">{{ title }}</strong>
          <span class="block text-xs text-secondary-foreground">{{ subtitle }}</span>
        </div>
      </div>
      <h1 class="m-0 text-title font-semibold tracking-tight text-foreground">{{ t('auth.setupTitle') }}</h1>
      <p class="m-0 mt-2 text-sm text-muted-foreground">{{ t('auth.setupHint') }}</p>
      <form class="mt-6 grid gap-4" @submit.prevent="submit">
        <FormField :label="t('auth.username')">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" v-model="username" autocomplete="username" :aria-describedby="describedBy" :invalid="invalid" />
          </template>
        </FormField>
        <FormField :label="t('auth.password')">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" v-model="password" type="password" autocomplete="new-password" :aria-describedby="describedBy" :invalid="invalid" />
          </template>
        </FormField>
        <FormField :label="t('auth.confirmPassword')">
          <template #default="{ id, describedBy, invalid }">
            <Input :id="id" v-model="confirmPassword" type="password" autocomplete="new-password" :aria-describedby="describedBy" :invalid="invalid" />
          </template>
        </FormField>
        <InfoBar v-if="error" tone="danger">{{ error }}</InfoBar>
        <Button type="submit" variant="primary" :loading="loading">{{ t('auth.setupSubmit') }}</Button>
      </form>
    </section>
  </main>
</template>
