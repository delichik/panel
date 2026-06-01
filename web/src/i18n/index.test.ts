import {
  setLocale,
  translateApplicationFileKind,
  translateApplicationRestartPolicy,
  translateNomadAllocationDesiredStatus,
  translateNomadNodeKind,
  translateNomadNodeRole,
  translateNomadNodeStatus,
  translateNomadRuntimeStatus,
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

  it('translates application and Nomad enums into simplified Chinese', () => {
    setLocale('zh-CN');

    expect(translateApplicationRestartPolicy('unless-stopped')).toBe('除非手动停止');
    expect(translateApplicationFileKind('binary')).toBe('二进制');
    expect(translateNomadNodeRole('client')).toBe('客户端');
    expect(translateNomadNodeStatus('nomad_unreachable')).toBe('Nomad 不可达');
    expect(translateNomadNodeKind('managed')).toBe('已托管');
    expect(translateNomadRuntimeStatus('running')).toBe('运行中');
    expect(translateNomadAllocationDesiredStatus('evict')).toBe('驱逐');
  });

  it('falls back to humanized English for unknown enum values', () => {
    setLocale('en');

    expect(translateNomadRuntimeStatus('draining_now')).toBe('draining now');
    expect(translateApplicationRestartPolicy('custom-policy')).toBe('custom policy');
  });
});
