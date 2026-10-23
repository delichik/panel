import { describe, expect, it } from 'vitest';
import { activeNavKey, navGroups } from './navModel';

describe('shell navigation model', () => {
  it('exposes separate operations, tasks, and logs entries', () => {
    const items = navGroups.flatMap(group => group.items);
    expect(items.find(item => item.key === 'operations')).toMatchObject({ titleKey: 'routes.operations.title', to: '/operations' });
    expect(items.find(item => item.key === 'tasks')).toMatchObject({ titleKey: 'routes.tasks.title', to: '/tasks' });
    expect(items.find(item => item.key === 'logs')).toMatchObject({ titleKey: 'routes.logs.title', to: '/logs' });
    expect(items.some(item => item.key === 'activity')).toBe(false);
    expect(items.some(item => ['application-operations', 'system-events'].includes(item.key))).toBe(false);
    expect(activeNavKey('/operations')).toBe('operations');
    expect(activeNavKey('/tasks')).toBe('tasks');
    expect(activeNavKey('/logs')).toBe('logs');
  });
});
