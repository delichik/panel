import type { Appearance, AppearanceUpdate, PublicAppearance } from '@/types/appearance';

/** 1×1 PNG：Mock 模式下的背景缩略图/原图占位，避免测试依赖真实图片文件。 */
const BACKGROUND_PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
export const MOCK_BACKGROUND_THUMB = `data:image/png;base64,${BACKGROUND_PNG_BASE64}`;

export function mockBackgroundBytes(): Uint8Array {
  return Uint8Array.from(atob(BACKGROUND_PNG_BASE64), (char) => char.charCodeAt(0));
}

/** Blob for the mocked image content endpoint (typed as Blob avoids TS 5.7 ArrayBuffer generics). */
export function mockBackgroundBlob(): Blob {
  const bytes = mockBackgroundBytes();
  return new Blob([bytes.buffer as ArrayBuffer], { type: 'image/png' });
}

function now() {
  return new Date().toISOString();
}

export let mockAppearance: Appearance = {
  themeMode: 'system',
  mica: true,
  accent: 'blue',
  background: { kind: 'preset', presetId: 'mist', assetId: null, revision: 0, hasImage: false },
  thumbDataUrl: null,
  initialized: true,
  updatedAt: '2026-11-01T00:00:00.000Z',
};

export function saveMockAppearance(input: AppearanceUpdate): Appearance {
  if (input.background && input.background.kind === 'image' && !mockAppearance.background.hasImage) {
    const error = new Error('Upload a background image first.');
    (error as Error & { code?: string }).code = 'background_required';
    throw error;
  }
  const next: Appearance = {
    ...mockAppearance,
    themeMode: input.themeMode,
    mica: input.mica,
    accent: input.accent,
    initialized: true,
    updatedAt: now(),
  };
  if (input.background) {
    next.background = {
      ...mockAppearance.background,
      kind: input.background.kind,
      presetId: input.background.presetId,
    };
    // Uploaded assets survive solid/preset switches so the image can be re-enabled.
    next.thumbDataUrl = next.background.hasImage ? MOCK_BACKGROUND_THUMB : null;
  }
  mockAppearance = next;
  return next;
}

export function uploadMockBackground(): Appearance {
  mockAppearance = {
    ...mockAppearance,
    background: {
      ...mockAppearance.background,
      kind: 'image',
      presetId: null,
      assetId: 'mock-background-1',
      revision: mockAppearance.background.revision + 1,
      hasImage: true,
    },
    thumbDataUrl: MOCK_BACKGROUND_THUMB,
    initialized: true,
    updatedAt: now(),
  };
  return mockAppearance;
}

export function removeMockBackground(): Appearance {
  mockAppearance = {
    ...mockAppearance,
    background: {
      ...mockAppearance.background,
      kind: 'none',
      presetId: null,
      assetId: null,
      revision: mockAppearance.background.revision + 1,
      hasImage: false,
    },
    thumbDataUrl: null,
    initialized: true,
    updatedAt: now(),
  };
  return mockAppearance;
}

export function publicMockAppearance(): PublicAppearance {
  return {
    themeMode: mockAppearance.themeMode,
    mica: mockAppearance.mica,
    accent: mockAppearance.accent,
    background: {
      kind: mockAppearance.background.kind,
      presetId: mockAppearance.background.presetId,
      hasImage: mockAppearance.background.hasImage,
    },
    thumbDataUrl: mockAppearance.background.kind === 'image' ? MOCK_BACKGROUND_THUMB : null,
  };
}
