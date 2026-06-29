import { parseSpecYaml, toSpecYaml } from './appSpecYaml';

describe('appSpecYaml', () => {
  it('parses colon-prefixed command items as strings', () => {
    const parsed = parseSpecYaml(`command:
  - :9443
  - ":9555"
  - --listen=:9666
`);

    expect(parsed?.command).toEqual([':9443', ':9555', '--listen=:9666']);
  });

  it('round-trips generated command items through the standard YAML parser', () => {
    const yaml = toSpecYaml({
      name: 'web',
      image: 'nginx:1.27',
      command: [':9443', '--listen=:9555'],
      restart: { policy: 'unless-stopped' },
    });

    const parsed = parseSpecYaml(yaml);

    expect(parsed?.command).toEqual([':9443', '--listen=:9555']);
  });
});
