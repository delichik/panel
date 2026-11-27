import { describe, expect, it } from 'vitest';
import { activityApi } from './activity';

describe('activity API', () => {
  it('throws for retired product activity routes', () => {
    expect(() => activityApi.events()).toThrow(/retired/);
    expect(() => activityApi.operations()).toThrow(/retired/);
    expect(() => activityApi.operation()).toThrow(/retired/);
  });
});
