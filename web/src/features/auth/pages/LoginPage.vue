<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { settingsApi } from '@/api/settings';
import { useAuthStore } from '@/stores/auth';
import { useI18n } from '@/i18n';

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const { t } = useI18n();
const username = ref('');
const password = ref('');
const showPassword = ref(false);
const customTitle = ref('');
const customSubtitle = ref('');
const loginTitle = computed(() => customTitle.value || t('login.title'));
const loginSubtitle = computed(() => customSubtitle.value || t('login.subtitle'));

onMounted(async () => {
  try {
    const branding = await settingsApi.publicBranding();
    customTitle.value = branding.loginTitle.trim();
    customSubtitle.value = branding.loginSubtitle.trim();
  } catch {
    // The localized defaults keep login available if public settings cannot be loaded.
  }
});

async function submit() {
  await auth.login(username.value, password.value);
  if (auth.passwordChangeRequired) {
    await router.push({ path: '/change-password', query: { redirect: String(route.query.redirect || '/overview') } });
    return;
  }
  await router.push(String(route.query.redirect || '/overview'));
}
</script>

<template>
  <main class="login-page">
    <div class="login-backdrop" aria-hidden="true">
      <div class="login-orb login-orb--primary" />
      <div class="login-orb login-orb--secondary" />
      <div class="login-grid" />
    </div>

    <section class="login-shell">
      <div class="brand-mark" aria-hidden="true">
        <v-icon icon="mdi-console-line" size="24" />
      </div>

      <div class="login-heading">
        <h1>{{ loginTitle }}</h1>
        <p>{{ loginSubtitle }}</p>
      </div>

      <v-card class="login-card" variant="flat">
        <v-form class="login-form" @submit.prevent="submit">
          <v-text-field
            v-model="username"
            :label="t('login.username')"
            prepend-inner-icon="mdi-account-outline"
            variant="outlined"
            density="comfortable"
            autocomplete="username"
            hide-details="auto"
          />

          <v-text-field
            v-model="password"
            :label="t('login.password')"
            prepend-inner-icon="mdi-lock-outline"
            :append-inner-icon="showPassword ? 'mdi-eye-outline' : 'mdi-eye-off-outline'"
            :type="showPassword ? 'text' : 'password'"
            variant="outlined"
            density="comfortable"
            autocomplete="current-password"
            hide-details="auto"
            @click:append-inner="showPassword = !showPassword"
          />

          <v-alert v-if="auth.error" type="error" variant="tonal" class="login-alert" density="comfortable">
            {{ auth.error }}
          </v-alert>

          <v-btn
            color="primary"
            block
            size="large"
            type="submit"
            :loading="auth.loading"
            class="login-submit text-none"
          >
            {{ t('login.submit') }}
            <v-icon icon="mdi-arrow-right" end size="18" />
          </v-btn>
        </v-form>
      </v-card>
    </section>
  </main>
</template>

<style scoped>
.login-page {
  position: relative;
  display: flex;
  min-height: 100dvh;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  padding: 48px 24px;
  background: var(--lp-background);
}

.login-backdrop,
.login-grid {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

.login-backdrop {
  overflow: hidden;
}

.login-grid {
  opacity: 0.42;
  background-image:
    linear-gradient(color-mix(in srgb, var(--lp-border), transparent 58%) 1px, transparent 1px),
    linear-gradient(90deg, color-mix(in srgb, var(--lp-border), transparent 58%) 1px, transparent 1px);
  background-size: 56px 56px;
  mask-image: radial-gradient(circle at center, black, transparent 68%);
}

.login-orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(4px);
  opacity: 0.18;
}

.login-orb--primary {
  top: -18rem;
  left: 50%;
  width: 44rem;
  height: 44rem;
  background: rgb(var(--v-theme-primary));
  transform: translateX(-50%);
}

.login-orb--secondary {
  right: -12rem;
  bottom: -18rem;
  width: 34rem;
  height: 34rem;
  background: rgb(var(--v-theme-info));
  opacity: 0.08;
}

.login-shell {
  position: relative;
  z-index: 1;
  width: min(408px, 100%);
  text-align: center;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 52px;
  height: 52px;
  margin: 0 auto 24px;
  border: 1px solid rgba(var(--v-theme-primary), 0.28);
  border-radius: 16px;
  background:
    linear-gradient(145deg, rgba(255, 255, 255, 0.2), transparent),
    rgb(var(--v-theme-primary));
  color: #ffffff;
  box-shadow:
    0 12px 28px rgba(var(--v-theme-primary), 0.22),
    inset 0 1px 0 rgba(255, 255, 255, 0.3);
}

.login-heading {
  margin-bottom: 28px;
}

.login-heading h1 {
  margin: 0;
  color: var(--lp-text);
  font-size: clamp(1.7rem, 4vw, 2rem);
  font-weight: 720;
  letter-spacing: -0.04em;
  line-height: 1.15;
}

.login-heading p {
  max-width: 360px;
  margin: 10px auto 0;
  color: var(--lp-text-muted);
  font-size: 0.9rem;
  line-height: 1.6;
}

.login-card {
  overflow: hidden;
  padding: 28px;
  border: 1px solid var(--lp-border) !important;
  border-radius: 18px !important;
  background: color-mix(in srgb, var(--lp-surface), transparent 4%) !important;
  box-shadow:
    0 1px 2px rgba(31, 39, 36, 0.04),
    0 24px 64px rgba(31, 39, 36, 0.1) !important;
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.login-card:hover {
  border-color: var(--lp-border) !important;
  box-shadow:
    0 1px 2px rgba(31, 39, 36, 0.04),
    0 24px 64px rgba(31, 39, 36, 0.1) !important;
}

.login-form {
  display: grid;
  gap: 16px;
}

.login-form :deep(.v-field) {
  min-height: 52px;
  background: color-mix(in srgb, var(--lp-surface-container), transparent 18%);
}

.login-form :deep(.v-field__prepend-inner) {
  color: var(--lp-text-muted);
}

.login-alert {
  text-align: left;
  font-size: 0.85rem;
}

.login-submit {
  min-height: 50px;
  margin-top: 4px;
  font-weight: 700;
  letter-spacing: -0.01em;
  box-shadow: 0 10px 24px rgba(var(--v-theme-primary), 0.2) !important;
}

.login-submit:hover {
  box-shadow: 0 14px 30px rgba(var(--v-theme-primary), 0.28) !important;
}

:global(:root[data-theme='dark']) .login-card {
  box-shadow:
    0 1px 0 rgba(255, 255, 255, 0.03),
    0 28px 72px rgba(0, 0, 0, 0.36) !important;
}

@media (max-width: 600px) {
  .login-page {
    align-items: flex-start;
    padding: max(48px, 9dvh) 18px 32px;
  }

  .brand-mark {
    width: 48px;
    height: 48px;
    margin-bottom: 20px;
    border-radius: 14px;
  }

  .login-heading {
    margin-bottom: 24px;
  }

  .login-card {
    padding: 22px;
    border-radius: 16px !important;
  }
}
</style>
