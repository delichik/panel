import {
  setLocale,
  translateApplicationFileKind,
  translateApplicationRestartPolicy,
  translateRuntimeDesiredState,
  translateRuntimeStatus,
  translateTaskStage,
  translateTaskType,
} from './index';

describe('i18n translation helpers', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => null),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    });
    vi.stubGlobal('document', {
      documentElement: {
        lang: 'en',
      },
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('translates application and runtime enums into simplified Chinese', () => {
    setLocale('zh-CN');

    expect(translateApplicationRestartPolicy('unless-stopped')).toBe('除非手动停止');
    expect(translateApplicationFileKind('binary')).toBe('二进制');
    expect(translateApplicationFileKind('archive')).toBe('压缩包');
    expect(translateRuntimeStatus('running')).toBe('运行中');
    expect(translateRuntimeDesiredState('stopped')).toBe('停止');
    expect(translateTaskType('key_asset_import')).toBe('密钥资产导入');
    expect(translateTaskStage('verifying_local')).toBe('检查本地 API');
  });

  it('falls back to humanized English for unknown enum values', () => {
    setLocale('en');

    expect(translateRuntimeStatus('draining_now')).toBe('draining now');
    expect(translateApplicationRestartPolicy('custom-policy')).toBe('custom policy');
  });
});
