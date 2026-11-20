// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installMockApi } from './browser';

const nativeFetch = globalThis.fetch;

describe('mock auth mode', () => {
  beforeEach(() => {
    vi.unstubAllEnvs();
    installMockApi();
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    globalThis.fetch = nativeFetch;
  });

  it('does not require auth validation unless VITE_PANEL_TEST_AUTH is enabled', async () => {
    const session = await fetch('/api/v1/auth/session');
    const sessionEnvelope = await session.json();
    expect(sessionEnvelope.data.authenticated).toBe(true);
    expect(sessionEnvelope.data.passwordChangeRequired).toBe(false);

    const jwtSecret = await fetch('/api/v1/auth/jwt-secret', {
      method: 'POST',
      body: JSON.stringify({ jwtSecret: 'short' }),
    });
    expect(jwtSecret.status).toBe(200);
  });
});
