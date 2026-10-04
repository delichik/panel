// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { openSearchPanel, searchPanelOpen } from '@codemirror/search';
import { EditorView } from '@codemirror/view';
import { nextTick } from 'vue';
import { describe, expect, it } from 'vitest';
import CodeEditor from './CodeEditor.vue';

describe('CodeEditor model synchronization', () => {
  it('does not emit for external model updates and still emits for editor changes', async () => {
    const wrapper = mount(CodeEditor, {
      attachTo: document.body,
      props: { modelValue: 'first', language: 'yaml', editorLabel: 'YAML source' },
    });
    const view = EditorView.findFromDOM(wrapper.element as HTMLElement);
    expect(view?.state.doc.toString()).toBe('first');

    await wrapper.setProps({ modelValue: 'generated' });
    await nextTick();
    expect(view?.state.doc.toString()).toBe('generated');
    expect(wrapper.emitted('update:modelValue')).toBeUndefined();

    view?.dispatch({ changes: { from: view.state.doc.length, insert: '\nuser: edit' } });
    expect(wrapper.emitted('update:modelValue')).toEqual([['generated\nuser: edit']]);

    wrapper.unmount();
  });
});

describe('CodeEditor search and replace', () => {
  it('installs the search extension and opens the find panel', async () => {
    if (!globalThis.requestAnimationFrame) {
      globalThis.requestAnimationFrame = ((callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0) as unknown as number) as typeof requestAnimationFrame;
      globalThis.cancelAnimationFrame = ((id: number) => clearTimeout(id)) as typeof cancelAnimationFrame;
    }
    const wrapper = mount(CodeEditor, {
      attachTo: document.body,
      props: { modelValue: 'alpha\nbeta', language: 'yaml', editorLabel: 'YAML source' },
    });
    const view = EditorView.findFromDOM(wrapper.element as HTMLElement);
    expect(view).toBeTruthy();
    expect(view?.state.phrase('Find')).toBe('Find');

    openSearchPanel(view!);
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(searchPanelOpen(view!.state)).toBe(true);
    const panel = wrapper.element.querySelector('.cm-panel.cm-search');
    expect(panel).toBeTruthy();
    expect(panel?.querySelector('input[name="search"]')).toBeTruthy();
    expect(panel?.querySelector('input[name="replace"]')).toBeTruthy();

    wrapper.unmount();
  });
});