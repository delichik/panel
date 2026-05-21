import { serviceBodyYamlToVisual, visualToServiceBodyYaml } from './serviceBodyYaml';

describe('service body YAML helpers', () => {
  it('serializes visual service fields without a top-level services entry', () => {
    const yaml = visualToServiceBodyYaml({
      name: 'web',
      image: 'nginx:latest',
      restart: 'unless-stopped',
      ports: ['8080:80'],
      environment: { APP_ENV: 'prod' },
    });

    expect(yaml).not.toContain('services:');
    expect(yaml).not.toContain('web:');
    expect(yaml).toContain('image: nginx:latest');
    expect(yaml).toContain('restart: unless-stopped');
    expect(yaml).toContain('ports:');
    expect(yaml).toContain('- "8080:80"');
  });

  it('parses service body YAML back into visual fields', () => {
    const visual = serviceBodyYamlToVisual(
      'image: postgres:16\nrestart: unless-stopped\nports:\n  - "5432:5432"\nenvironment:\n  POSTGRES_DB: app\n',
    );

    expect(visual.image).toBe('postgres:16');
    expect(visual.restart).toBe('unless-stopped');
    expect(visual.ports).toEqual(['5432:5432']);
    expect(visual.environment).toEqual({ POSTGRES_DB: 'app' });
  });
});
