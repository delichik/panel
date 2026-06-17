import { parseSpecYaml, toSpecYaml } from './appSpecYaml';

describe('appSpecYaml', () => {
  it('parses colon-prefixed args as strings', () => {
    const parsed = parseSpecYaml(`args:
  - :9443
  - ":9555"
  - --listen=:9666
`);

    expect(parsed?.args).toEqual([':9443', ':9555', '--listen=:9666']);
  });

  it('round-trips generated args through the standard YAML parser', () => {
    const yaml = toSpecYaml({
      name: 'web',
      image: 'nginx:1.27',
      args: [':9443', '--listen=:9555'],
      restart: { policy: 'unless-stopped' },
    });

    const parsed = parseSpecYaml(yaml);

    expect(parsed?.args).toEqual([':9443', '--listen=:9555']);
  });
});
