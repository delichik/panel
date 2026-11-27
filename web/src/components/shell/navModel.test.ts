import { describe, expect, it } from 'vitest';
import { activeNavKey, navGroups } from './navModel';

describe('navModel observability entries', () => {
  it('exposes events and logs entries', () => {
    const items = navGroups.flatMap((group) => group.items);
    expect(items.find(item => item.key === 'events')).toMatchObject({ titleKey: 'routes.events.title', to: '/events' });
    expect(items.find(item => item.key === 'logs')).toMatchObject({ titleKey: 'routes.logs.title', to: '/logs' });
    expect(items.some(item => ['operations', 'tasks'].includes(item.key))).toBe(false);
    expect(activeNavKey('/events')).toBe('events');
    expect(activeNavKey('/logs')).toBe('logs');
  });
});
