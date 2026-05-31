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
          background: '#f6f8fb',
          surface: '#ffffff',
          'surface-variant': '#eef2f7',
          'surface-container': '#f8fafc',
          'surface-muted': '#f1f5f9',
          primary: '#4f46e5',
          secondary: '#64748b',
          success: '#059669',
          warning: '#d97706',
          error: '#dc2626',
          info: '#2563eb',
          'on-background': '#101828',
          'on-surface': '#182230',
          'on-surface-variant': '#475467',
        },
        variables: {
          'border-color': '#d7dee8',
          'border-opacity': 1,
        },
      },
      dark: {
        dark: true,
        colors: {
          background: '#090b10',
          surface: '#121722',
          'surface-variant': '#1b2230',
          'surface-container': '#151c28',
          'surface-muted': '#202a38',
          primary: '#818cf8',
          secondary: '#a7b1c2',
          success: '#34d399',
          warning: '#fbbf24',
          error: '#f87171',
          info: '#60a5fa',
          'on-background': '#f5f7fb',
          'on-surface': '#e6edf7',
          'on-surface-variant': '#aab5c4',
        },
        variables: {
          'border-color': '#2d3748',
          'border-opacity': 1,
        },
      },
    },
  },
});

createApp(App).use(pinia).use(router).use(vuetify).mount('#app');
