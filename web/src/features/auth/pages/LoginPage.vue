<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Lock, User } from '@element-plus/icons-vue';
import { useAuthStore } from '@/stores/auth';

const auth = useAuthStore();
const router = useRouter();
const route = useRoute();
const username = ref('');
const password = ref('');

async function submit() {
  await auth.login(username.value, password.value);
  await router.push(String(route.query.redirect || '/overview'));
}
</script>

<template>
  <main class="login-page">
    <section class="login-card">
      <h1>Linux Server Panel</h1>
      <p>Sign in to manage registered Debian servers.</p>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="Username">
          <el-input v-model="username" :prefix-icon="User" autocomplete="username" />
        </el-form-item>
        <el-form-item label="Password">
          <el-input
            v-model="password"
            :prefix-icon="Lock"
            type="password"
            autocomplete="current-password"
            show-password
          />
        </el-form-item>
        <el-alert v-if="auth.error" type="error" :title="auth.error" show-icon />
        <el-button class="login-button" type="primary" native-type="submit" :loading="auth.loading">
          Login
        </el-button>
      </el-form>
    </section>
  </main>
</template>

<style scoped>
.login-page {
  display: grid;
  min-height: 100vh;
  place-items: center;
  padding: 24px;
  background: #eef2f6;
}

.login-card {
  width: min(420px, 100%);
  padding: 28px;
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  background: #fff;
}

h1 {
  margin: 0;
  font-size: 26px;
}

p {
  margin: 8px 0 24px;
  color: #667085;
}

.login-button {
  width: 100%;
  margin-top: 12px;
}
</style>
