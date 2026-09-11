import { describe, expect, it } from 'vitest';
import { inferCodeEditorLanguage } from './codeEditorLanguage';

describe('inferCodeEditorLanguage', () => {
  it.each([
    ['config/app.yaml', '', 'yaml'],
    ['config/app', 'application/json', 'json'],
    ['scripts/deploy.sh', '', 'shell'],
    ['nginx.conf', '', 'nginx'],
    ['config/app.ini', '', 'properties'],
    ['Dockerfile.production', '', 'dockerfile'],
    ['README', 'text/plain', 'plain'],
  ])('infers %s', (path, contentType, expected) => {
    expect(inferCodeEditorLanguage(path, contentType)).toBe(expected);
  });
});
