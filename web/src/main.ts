import { createApp } from 'vue';
import 'vuetify/styles';
import { createVuetify } from 'vuetify';
import * as components from 'vuetify/components';
import * as directives from 'vuetify/directives';
import '@mdi/font/css/materialdesignicons.css';
import './styles/main.css';
import App from './app/App.vue';
import { router } from './router';
import { pinia } from './stores';
import { syncDocumentLanguage } from './i18n';
import { getInitialTheme, syncThemeAttribute } from './theme';

const initialTheme = getInitialTheme();
syncThemeAttribute(initialTheme);
syncDocumentLanguage();

const vuetify = createVuetify({
  components,
  directives,
  theme: {
    defaultTheme: initialTheme,
    themes: {
      light: {
        dark: false,
        colors: {
          background: '#f7f8f5',
          surface: '#ffffff',
          'surface-variant': '#eef3ed',
          'surface-container': '#f9faf7',
          'surface-muted': '#eef1ec',
          primary: '#0f766e',
          secondary: '#6b7280',
          success: '#16a34a',
          warning: '#b45309',
          error: '#dc2626',
          info: '#2563eb',
          'on-background': '#181d1b',
          'on-surface': '#1f2724',
          'on-surface-variant': '#5f6f68',
        },
        variables: {
          'border-color': '#d9e2dc',
          'border-opacity': 1,
        },
      },
      dark: {
        dark: true,
        colors: {
          background: '#0d100e',
          surface: '#151a17',
          'surface-variant': '#202720',
          'surface-container': '#171d1a',
          'surface-muted': '#222a25',
          primary: '#2dd4bf',
          secondary: '#b5beb7',
          success: '#4ade80',
          warning: '#f59e0b',
          error: '#fb7185',
          info: '#60a5fa',
          'on-background': '#f3f6f1',
          'on-surface': '#e7ece6',
          'on-surface-variant': '#b3beb6',
        },
        variables: {
          'border-color': '#334139',
          'border-opacity': 1,
        },
      },
    },
  },
});

createApp(App).use(pinia).use(router).use(vuetify).mount('#app');
