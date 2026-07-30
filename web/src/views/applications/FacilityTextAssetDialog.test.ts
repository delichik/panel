// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import FacilityTextAssetDialog from './FacilityTextAssetDialog.vue';

const labels = { editTitle: 'Edit', newTitle: 'New', close: 'Close', name: 'Name', filename: 'Filename', loading: 'Loading', content: 'Content', cancel: 'Cancel', save: 'Save', reload: 'Discard unsaved text and reload session' };
const dialogStub = { props: ['open'], emits: ['update:open'], template: '<section v-if="open"><slot/><slot name="footer"/></section>' };
const inputStub = { props: ['modelValue'], emits: ['update:modelValue'], template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' };
const editorStub = { props: ['modelValue'], emits: ['update:modelValue'], template: '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' };

function mountDialog(overrides: Record<string, unknown> = {}) {
  return mount(FacilityTextAssetDialog, {
    props: { open: true, editing: false, assetKey: 'asset-temp', name: '', filename: 'empty.txt', content: '', loading: false, saving: false, error: '', conflict: false, labels, ...overrides },
    global: { stubs: { Dialog: dialogStub, Input: inputStub, CodeEditor: editorStub } },
  });
}

describe('FacilityTextAssetDialog', () => {
  it('submits an empty new text file with its stable temporary key', async () => {
    const wrapper = mountDialog();
    await wrapper.get('button:last-of-type').trigger('click');
    expect(wrapper.emitted('save')?.[0]?.[0]).toEqual({ assetKey: 'asset-temp', name: '', filename: 'empty.txt', content: '', contentMode: 'text', kind: 'uploaded_file' });
  });

  it('loads editable content and submits the same existing key', async () => {
    const wrapper = mountDialog({ editing: true, assetKey: 'asset-existing', name: 'config', filename: 'config.txt', content: 'loaded' });
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('loaded');
    await wrapper.get('textarea').setValue('changed');
    await wrapper.setProps({ content: 'changed' });
    await wrapper.get('button:last-of-type').trigger('click');
    expect((wrapper.emitted('save')?.[0]?.[0] as { assetKey: string; content: string })).toMatchObject({ assetKey: 'asset-existing', content: 'changed' });
  });

  it('closes when the successful save owner updates the open model', async () => {
    let wrapper: ReturnType<typeof mountDialog>;
    wrapper = mount(FacilityTextAssetDialog, {
      props: {
        open: true, editing: false, assetKey: 'asset-temp', name: '', filename: 'empty.txt', content: '', loading: false, saving: false, error: '', conflict: false, labels,
        onSave: () => wrapper.setProps({ open: false }),
      },
      global: { stubs: { Dialog: dialogStub, Input: inputStub, CodeEditor: editorStub } },
    });
    await wrapper.get('button:last-of-type').trigger('click');
    expect(wrapper.find('section').exists()).toBe(false);
  });

  it('keeps content visible on failure and exposes conflict recovery', async () => {
    const wrapper = mountDialog({ content: 'unsaved', error: 'Revision changed', conflict: true });
    expect(wrapper.get('[role="alert"]').text()).toContain('Revision changed');
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('unsaved');
    const reload = wrapper.findAll('button').find((button) => button.text().includes('Discard unsaved'))!;
    await reload.trigger('click');
    expect(wrapper.emitted('reload')).toHaveLength(1);
  });
});
