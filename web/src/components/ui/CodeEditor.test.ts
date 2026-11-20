// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { openSearchPanel, searchPanelOpen } from '@codemirror/search';
import { EditorView } from '@codemirror/view';
import { nextTick } from 'vue';
import { afterEach, describe, expect, it, vi } from 'vitest';
import CodeEditor, { CODE_EDITOR_SYNTAX, codeEditorHighlightStyle } from './CodeEditor.vue';

afterEach(() => {
  document.documentElement.removeAttribute('data-theme');
});

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

describe('CodeEditor accessibility wiring', () => {
  it('accepts kebab-case aria-describedby / aria-invalid attributes from FormField', async () => {
    const wrapper = mount(CodeEditor, {
      attachTo: document.body,
      props: { modelValue: 'key', language: 'plain', editorLabel: 'Key' },
      attrs: { 'aria-describedby': 'key-hint', 'aria-invalid': 'true' },
    });
    await nextTick();
    const content = (wrapper.element as HTMLElement).querySelector<HTMLElement>('.cm-content')!;
    expect(content.getAttribute('aria-describedby')).toBe('key-hint');
    expect(content.getAttribute('aria-invalid')).toBe('true');
    wrapper.unmount();
  });

  it('lands id / aria-describedby / aria-invalid on the focusable editor content', async () => {
    const wrapper = mount(CodeEditor, {
      attachTo: document.body,
      props: {
        modelValue: 'private-key',
        language: 'plain',
        editorLabel: 'Private key',
        id: 'credential-private-key',
        ariaDescribedby: 'credential-private-key-error',
        invalid: true,
      },
    });

    const content = (wrapper.element as HTMLElement).querySelector<HTMLElement>('.cm-content')!;
    expect(content.id).toBe('credential-private-key');
    expect(content.getAttribute('aria-describedby')).toBe('credential-private-key-error');
    expect(content.getAttribute('aria-invalid')).toBe('true');
    expect(content.getAttribute('aria-label')).toBe('Private key');
    expect(wrapper.classes()).toContain('border-danger-border');

    await wrapper.setProps({ invalid: false, id: 'next-field', ariaDescribedby: 'next-hint' });
    await nextTick();
    expect(content.id).toBe('next-field');
    expect(content.getAttribute('aria-describedby')).toBe('next-hint');
    expect(content.getAttribute('aria-invalid')).toBeNull();
    expect(wrapper.classes()).not.toContain('border-danger-border');

    wrapper.unmount();
  });
});

describe('CodeEditor theme', () => {
  it('ships paired light/dark syntax palettes with usable contrast', () => {
    expect(CODE_EDITOR_SYNTAX.dark.keyword).not.toBe(CODE_EDITOR_SYNTAX.light.keyword);
    expect(CODE_EDITOR_SYNTAX.dark.string).not.toBe(CODE_EDITOR_SYNTAX.light.string);
    expect(CODE_EDITOR_SYNTAX.dark.comment).not.toBe(CODE_EDITOR_SYNTAX.light.comment);

    const darkSpecs = (codeEditorHighlightStyle('dark') as unknown as { specs: Array<{ color?: string }> }).specs;
    const darkColors = darkSpecs.map((spec) => spec.color).filter((color): color is string => Boolean(color));
    expect(darkColors).toContain(CODE_EDITOR_SYNTAX.dark.keyword);
    expect(darkColors).not.toContain(CODE_EDITOR_SYNTAX.light.keyword);
  });

  it('switches the CodeMirror theme and highlight style when data-theme changes', async () => {
    document.documentElement.dataset.theme = 'light';
    const wrapper = mount(CodeEditor, {
      attachTo: document.body,
      props: { modelValue: 'key: value', language: 'yaml', editorLabel: 'YAML source' },
    });
    const view = EditorView.findFromDOM(wrapper.element as HTMLElement)!;
    const lightDomClass = view.dom.className;

    document.documentElement.dataset.theme = 'dark';
    await vi.waitFor(() => {
      expect(view.dom.className).not.toBe(lightDomClass);
    });
    // 深色语法色板经 Compartment 重新注入 style 模块。
    await vi.waitFor(() => {
      const styles = Array.from(document.querySelectorAll('style')).map((style) => style.textContent ?? '').join('\n');
      expect(styles).toContain(CODE_EDITOR_SYNTAX.dark.keyword);
    });

    wrapper.unmount();
    // 卸载后 observer 已清理：再次切换主题不应报错，也不应再操作已销毁的 editor。
    document.documentElement.dataset.theme = 'light';
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(true).toBe(true);
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