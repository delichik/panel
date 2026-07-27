import { describe, expect, it } from 'vitest';
import { isSameFileDialogContext } from './fileDialogContext';

describe('isSameFileDialogContext', () => {
  it('does not apply an old save completion to a newly opened file dialog', () => {
    const oldSave = { generation: 3, fileKey: 'file-a' };
    const newDialog = { generation: 4, fileKey: 'file-b' };
    expect(isSameFileDialogContext(oldSave, newDialog)).toBe(false);
  });

  it('allows completion for the unchanged dialog context', () => {
    const context = { generation: 3, fileKey: 'file-a' };
    expect(isSameFileDialogContext(context, { ...context })).toBe(true);
  });
});
