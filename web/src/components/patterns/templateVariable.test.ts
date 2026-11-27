import { describe, expect, it } from 'vitest';
import { insertTemplateVariable } from './templateVariable';

describe('template variable insertion', () => {
  it('inserts at the current selection and replaces selected text', () => {
    const expression = '{{ (index .applications "app-redis").containerName }}';
    expect(insertTemplateVariable('redis://host:6379', expression, 8, 12)).toEqual({
      value: `redis://${expression}:6379`,
      cursor: 8 + expression.length,
    });
  });

  it('appends when the input has no selection', () => {
    const expression = '{{ .app.name }}';
    expect(insertTemplateVariable('prefix-', expression)).toEqual({
      value: `prefix-${expression}`,
      cursor: 7 + expression.length,
    });
  });
});
