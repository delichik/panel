import { describe, expect, it } from 'vitest';
import { activeNavKey, navGroups } from './navModel';

describe('shell navigation model', () => {
  it('provides one unified log entry and retires the three history entries', () => {
    const items = navGroups.flatMap(group => group.items);
    expect(items.find(item => item.key === 'activity')).toMatchObject({ titleKey: 'routes.activity.title', to: '/activity' });
    expect(items.some(item => ['tasks', 'application-operations', 'system-events'].includes(item.key))).toBe(false);
    expect(activeNavKey('/activity')).toBe('activity');
  });
});
