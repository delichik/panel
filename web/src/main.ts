import { createApp } from 'vue';
import { createPinia } from 'pinia';
import { setAuthTokenProvider } from './api/client';
import App from './app/App.vue';
import { router } from './router';
import { useSessionStore } from './stores/session';
import './styles/main.css';

void bootstrap();

async function bootstrap() {
  try {
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
  } catch (error) {
    console.error('Failed to bootstrap Seamark:', error);
    const root = document.getElementById('app');
    if (root) {
      root.innerHTML = [
        '<div style="min-height:100dvh;display:grid;place-items:center;padding:2rem;background:var(--panel-bg,#f8fafc);color:var(--panel-text,#0f172a);font-family:system-ui,sans-serif;text-align:center">',
        '  <div>',
        '    <h1 style="font-size:1.125rem;margin:0 0 0.5rem">Seamark failed to start</h1>',
        '    <p style="margin:0;font-size:0.875rem;opacity:0.75">The application could not be loaded. Refresh the page to try again.</p>',
        '  </div>',
        '</div>',
      ].join('');
    }
  }
}