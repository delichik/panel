// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
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
