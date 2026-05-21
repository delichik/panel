<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/auth';

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const username = ref('');
const password = ref('');
const showPassword = ref(false);

async function submit() {
  await auth.login(username.value, password.value);
  await router.push(String(route.query.redirect || '/overview'));
}
</script>

<template>
  <main class="login-page">
    <v-card class="login-card pa-8" variant="outlined">
      <div class="d-flex align-center justify-center mb-6">
        <div class="brand-logo">LP</div>
      </div>

      <div class="text-center mb-6">
        <h1 class="text-h5 font-weight-bold tracking-tight mb-1">Linux Server Panel</h1>
        <p class="text-body-2 text-medium-emphasis">SSH control plane for Debian servers</p>
      </div>

      <v-form @submit.prevent="submit">
        <v-text-field
          v-model="username"
          label="Username"
          prepend-inner-icon="mdi-account"
          variant="outlined"
          density="comfortable"
          autocomplete="username"
          class="mb-4"
          hide-details="auto"
        />

        <v-text-field
          v-model="password"
          label="Password"
          prepend-inner-icon="mdi-lock"
          :append-inner-icon="showPassword ? 'mdi-eye' : 'mdi-eye-off'"
          :type="showPassword ? 'text' : 'password'"
          @click:append-inner="showPassword = !showPassword"
          variant="outlined"
          density="comfortable"
          autocomplete="current-password"
          class="mb-5"
          hide-details="auto"
        />

        <v-alert v-if="auth.error" type="error" variant="tonal" class="mb-4 text-body-2" density="comfortable">
          {{ auth.error }}
        </v-alert>

        <v-btn
          color="primary"
          block
          size="large"
          type="submit"
          :loading="auth.loading"
          class="text-none font-weight-bold shadow-glow"
        >
          Login to Console
        </v-btn>
      </v-form>
    </v-card>
  </main>
</template>

<style scoped>
.login-page {
  display: grid;
  min-height: 100vh;
  place-items: center;
  padding: 24px;
  background: radial-gradient(circle at top, rgba(var(--v-theme-primary), 0.05) 0%, rgb(var(--v-theme-background)) 70%);
}

.login-card {
  width: min(440px, 100%);
  border-radius: 14px !important;
  background-color: rgb(var(--v-theme-surface)) !important;
  border: 1px solid rgba(var(--v-border-color), 0.06) !important;
  box-shadow: 0 10px 30px -10px rgba(0, 0, 0, 0.06), 0 1px 3px 0 rgba(0, 0, 0, 0.02) !important;
}

.brand-logo {
  display: grid;
  place-items: center;
  width: 48px;
  height: 48px;
  border-radius: 10px;
  background: linear-gradient(135deg, rgb(var(--v-theme-primary)) 0%, #4f46e5 100%);
  color: #ffffff;
  font-size: 1.25rem;
  font-weight: 800;
  box-shadow: 0 4px 12px rgba(var(--v-theme-primary), 0.3);
}

.tracking-tight {
  letter-spacing: -0.02em;
}

.shadow-glow {
  box-shadow: 0 4px 14px rgba(var(--v-theme-primary), 0.3) !important;
}

.shadow-glow:hover {
  box-shadow: 0 6px 18px rgba(var(--v-theme-primary), 0.45) !important;
}
</style>
