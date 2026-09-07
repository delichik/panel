import { describe, expect, it } from 'vitest';
import { activeNavKey, navGroups } from './navModel';

describe('shell navigation model', () => {
  it('exposes the task center and activates it for task routes', () => {
    const taskItem = navGroups.flatMap((group) => group.items).find((item) => item.key === 'tasks');

    expect(taskItem).toMatchObject({
      titleKey: 'routes.tasks.title',
      to: '/tasks',
    });
    expect(activeNavKey('/tasks')).toBe('tasks');
  });
});
