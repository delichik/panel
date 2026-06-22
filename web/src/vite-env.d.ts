/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_PANEL_TEST_MODE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
