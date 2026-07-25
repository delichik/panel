import { createApp } from 'vue';
import { createPinia } from 'pinia';
import { setAuthTokenProvider } from './api/client';
import App from './app/App.vue';
import { router } from './router';
import { useSessionStore } from './stores/session';
import './styles/main.css';

void bootstrap();

async function bootstrap() {
  if (import.meta.env.VITE_PANEL_TEST_MODE === 'true') {
    const { installMockApi } = await import('./mocks/browser');
    installMockApi();
  }

  const pinia = createPinia();
  const app = createApp(App);
  app.use(pinia);

  const session = useSessionStore(pinia);
  setAuthTokenProvider(() => session.token);
  await session.restore();

  app.use(router).mount('#app');
}
