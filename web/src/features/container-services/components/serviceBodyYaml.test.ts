import { serviceBodyYamlToVisual, visualToServiceBodyYaml } from './serviceBodyYaml';

describe('container service body YAML helpers', () => {
  it('serializes common visual fields as a Compose service body only', () => {
    const yaml = visualToServiceBodyYaml({
      image: 'nginx:latest',
      restart: 'unless-stopped',
      ports: ['8080:80'],
      environment: { APP_ENV: 'prod' },
      labels: { 'panel.claims.ports': '8080' },
    });

    expect(yaml).toContain('image: nginx:latest');
    expect(yaml).toContain('ports:');
    expect(yaml).toContain('panel.claims.ports: "8080"');
    expect(yaml).not.toContain('services:');
    expect(yaml).not.toContain('container_name:');
  });

  it('roundtrips common fields from YAML back to the visual editor', () => {
    const visual = serviceBodyYamlToVisual(
      'image: postgres:16\nrestart: unless-stopped\nports:\n  - "5432:5432"\ndepends_on:\n  - redis\nenvironment:\n  POSTGRES_DB: app\n',
    );

    expect(visual.image).toBe('postgres:16');
    expect(visual.restart).toBe('unless-stopped');
    expect(visual.ports).toEqual(['5432:5432']);
    expect(visual.dependsOn).toEqual(['redis']);
    expect(visual.environment).toEqual({ POSTGRES_DB: 'app' });
  });
});
