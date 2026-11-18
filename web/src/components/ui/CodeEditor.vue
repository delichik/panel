<script lang="ts">
/**
 * CodeMirror 主题 / 语法高亮（light/dark）。
 *
 * CodeMirror 的内置 `defaultHighlightStyle` 是浅色配色，深色主题下语法 token 与
 * 光标/选区会不可读；这里按 `html[data-theme]` 提供两套完整主题，并通过
 * Compartment 在运行时切换（见组件 setup 中的 MutationObserver）。
 *
 * 表面/正文/焦点仍使用 main.css 的 `--panel-*` token（随主题自动生效）；
 * 语法色板与光标/选区是编辑器内部绘制值，集中在本文件保证两种主题成对可读。
 */
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { closeSearchPanel, findNext, findPrevious, highlightSelectionMatches, openSearchPanel, search, selectSelectionMatches } from '@codemirror/search';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { Annotation, Compartment, EditorState } from '@codemirror/state';
import { drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers, type KeyBinding } from '@codemirror/view';
import { tags } from '@lezer/highlight';

export type CodeEditorThemeMode = 'light' | 'dark';

export interface CodeEditorSyntaxPalette {
  keyword: string;
  string: string;
  number: string;
  comment: string;
  variable: string;
  function: string;
  type: string;
  operator: string;
  tag: string;
  meta: string;
  link: string;
  heading: string;
  invalid: string;
  escape: string;
  regexp: string;
}

/** 深色配色以 #202020 背景为基准、浅色以 #F3F3F3 为基准，均满足正文可读对比。 */
export const CODE_EDITOR_SYNTAX: Record<CodeEditorThemeMode, CodeEditorSyntaxPalette> = {
  light: {
    keyword: '#0550ae',
    string: '#0a7d3b',
    number: '#9a6700',
    comment: '#6e7781',
    variable: '#24292f',
    function: '#8250df',
    type: '#953800',
    operator: '#57606a',
    tag: '#116329',
    meta: '#57606a',
    link: '#0969da',
    heading: '#0550ae',
    invalid: '#cf222e',
    escape: '#b35900',
    regexp: '#0a7d3b',
  },
  dark: {
    keyword: '#ff7b72',
    string: '#a5d6ff',
    number: '#79c0ff',
    comment: '#8b949e',
    variable: '#c9d1d9',
    function: '#d2a8ff',
    type: '#ffa657',
    operator: '#c9d1d9',
    tag: '#7ee787',
    meta: '#8b949e',
    link: '#58a6ff',
    heading: '#79c0ff',
    invalid: '#ffa198',
    escape: '#ffa657',
    regexp: '#a5d6ff',
  },
};

export function codeEditorHighlightStyle(mode: CodeEditorThemeMode): HighlightStyle {
  const c = CODE_EDITOR_SYNTAX[mode];
  return HighlightStyle.define([
    { tag: [tags.keyword, tags.controlKeyword, tags.definitionKeyword, tags.moduleKeyword, tags.operatorKeyword], color: c.keyword },
    { tag: [tags.string, tags.special(tags.string)], color: c.string },
    { tag: [tags.number, tags.integer, tags.float, tags.bool, tags.null, tags.atom], color: c.number },
    { tag: [tags.comment, tags.lineComment, tags.blockComment, tags.docComment], color: c.comment, fontStyle: 'italic' },
    { tag: [tags.variableName, tags.propertyName, tags.attributeName], color: c.variable },
    { tag: [tags.function(tags.variableName), tags.function(tags.propertyName)], color: c.function },
    { tag: [tags.typeName, tags.className, tags.namespace], color: c.type },
    { tag: [tags.operator, tags.punctuation, tags.bracket, tags.separator, tags.angleBracket], color: c.operator },
    { tag: [tags.tagName], color: c.tag },
    { tag: [tags.meta, tags.processingInstruction], color: c.meta },
    { tag: [tags.link, tags.url], color: c.link, textDecoration: 'underline' },
    { tag: [tags.heading], color: c.heading, fontWeight: '600' },
    { tag: [tags.invalid], color: c.invalid, textDecoration: 'underline wavy' },
    { tag: [tags.escape], color: c.escape },
    { tag: [tags.regexp], color: c.regexp },
    { tag: [tags.quote], color: c.string },
    { tag: [tags.emphasis], fontStyle: 'italic' },
    { tag: [tags.strong], fontWeight: '600' },
  ]);
}

/**
 * 主题：表面/正文 token 自动随 `data-theme` 变化；光标/选区用主题内的显式值，
 * 避免深色下默认黑色光标、浅色选区底不可读。
 */
export function codeEditorTheme(mode: CodeEditorThemeMode): ReturnType<typeof EditorView.theme> {
  const dark = mode === 'dark';
  const selection = dark ? 'rgba(255, 255, 255, 0.22)' : 'rgba(0, 120, 212, 0.20)';
  const selectionMatch = dark ? 'rgba(255, 255, 255, 0.14)' : 'rgba(0, 120, 212, 0.12)';
  const searchMatch = dark ? 'rgba(255, 214, 102, 0.28)' : 'rgba(255, 221, 87, 0.45)';
  const searchMatchSelected = dark ? 'rgba(255, 214, 102, 0.42)' : 'rgba(255, 221, 87, 0.65)';
  return EditorView.theme({
    '&': {
      height: '100%',
      minHeight: '0',
      backgroundColor: 'var(--panel-control-fill)',
      color: 'var(--panel-text)',
    },
    // 焦点为 2px 实色前景色（WinUI 焦点框），不引入 accent ring。
    '&.cm-focused': { outline: '2px solid var(--panel-text)', outlineOffset: '-2px' },
    '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono, ui-monospace, monospace)', lineHeight: '1.55' },
    '.cm-content': { padding: '0.75rem 0', caretColor: 'var(--panel-text)' },
    // 深色下默认黑色光标不可读：统一用前景色，2px 宽便于定位。
    '.cm-cursor': { borderLeftColor: 'var(--panel-text)', borderLeftWidth: '2px' },
    '.cm-dropCursor': { borderLeftColor: 'var(--panel-text)', borderLeftWidth: '2px' },
    '.cm-selectionBackground': { backgroundColor: selection },
    '.cm-content ::selection': { backgroundColor: selection },
    '.cm-selectionMatch': { backgroundColor: selectionMatch },
    '.cm-searchMatch': { backgroundColor: searchMatch, outline: `1px solid ${searchMatchSelected}` },
    '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: searchMatchSelected },
    '.cm-matchingBracket': { backgroundColor: selectionMatch, outline: '1px dashed var(--panel-text)' },
    '.cm-nonmatchingBracket': { color: dark ? '#ffa198' : '#cf222e' },
    '.cm-gutters': { backgroundColor: 'var(--panel-layer)', color: 'var(--panel-text-muted)', borderRight: '1px solid var(--panel-stroke)' },
    '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'var(--panel-hover)' },
    '.cm-panels': { backgroundColor: 'var(--panel-flyout)', color: 'var(--panel-text)' },
    '.cm-panel.cm-search': {
      backgroundColor: 'var(--panel-flyout)',
      color: 'var(--panel-text)',
      borderBottom: '1px solid var(--panel-stroke)',
      padding: '0.5rem',
      display: 'flex',
      flexWrap: 'wrap',
      alignItems: 'center',
      gap: '0.5rem',
      fontSize: '0.8125rem',
    },
    '.cm-panel.cm-search input.cm-textfield': {
      backgroundColor: 'var(--panel-control-fill)',
      border: '1px solid var(--panel-stroke)',
      borderRadius: '4px',
      color: 'var(--panel-text)',
      padding: '0.25rem 0.5rem',
      fontFamily: 'inherit',
    },
    '.cm-panel.cm-search input.cm-textfield:focus': { outline: '2px solid var(--panel-text)', outlineOffset: '0' },
    '.cm-panel.cm-search .cm-button': {
      backgroundColor: 'var(--panel-control-fill)',
      border: '1px solid var(--panel-stroke)',
      borderRadius: '4px',
      color: 'var(--panel-text)',
      cursor: 'pointer',
      padding: '0.25rem 0.625rem',
      fontFamily: 'inherit',
      fontSize: 'inherit',
    },
    '.cm-panel.cm-search .cm-button:hover': { backgroundColor: 'var(--panel-hover)' },
    '.cm-panel.cm-search label': { display: 'inline-flex', alignItems: 'center', gap: '0.25rem', whiteSpace: 'nowrap' },
    '.cm-panel.cm-search input[type="checkbox"]': { accentColor: 'var(--panel-primary)' },
    '.cm-tooltip': {
      backgroundColor: 'var(--panel-popover)',
      color: 'var(--panel-text)',
      border: '1px solid var(--panel-border-strong)',
    },
  });
}

/** 读取当前 `html[data-theme]`；system 已由 appearance store 解析为 light/dark。 */
export function resolveCodeEditorThemeMode(): CodeEditorThemeMode {
  if (typeof document === 'undefined') return 'light';
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';
}
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { codeEditorLanguageExtension, type CodeEditorLanguage } from './codeEditorLanguage';

const props = defineProps<{
  modelValue: string;
  language: CodeEditorLanguage;
  editorLabel: string;
  disabled?: boolean;
  invalid?: boolean;
  size?: 'default' | 'large';
  /** 供 FormField 等外部 label 关联（落到内部可聚焦的 contenteditable）。 */
  id?: string;
  /** 供 FormField 的 hint/error 关联（落到内部可编辑区）。 */
  ariaDescribedby?: string;
  /** 外部已计算好的无效态（与 invalid 取并集）。 */
  ariaInvalid?: boolean | string;
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();
const host = ref<HTMLElement | null>(null);
const language = new Compartment();
const editable = new Compartment();
const contentAttributes = new Compartment();
const themeCompartment = new Compartment();
const highlightCompartment = new Compartment();
let editor: EditorView | null = null;
let themeObserver: MutationObserver | undefined;
const externalModelValueSync = Annotation.define<boolean>();
const { t, locale } = useI18n();
const phrases = new Compartment();
const themeMode = ref<CodeEditorThemeMode>(resolveCodeEditorThemeMode());

/** invalid / aria-invalid 两种来源合并，保证 FormField 显式传 aria-invalid 时只出现一次。 */
const isInvalid = computed(() => props.invalid || props.ariaInvalid === true || props.ariaInvalid === 'true');

/** 无障碍属性落到 CodeMirror 的内部可编辑区（.cm-content），而不是外层装饰容器。 */
const contentAriaAttributes = computed<Record<string, string>>(() => {
  const attributes: Record<string, string> = { 'aria-label': props.editorLabel };
  if (props.id) attributes.id = props.id;
  if (props.ariaDescribedby) attributes['aria-describedby'] = props.ariaDescribedby;
  if (isInvalid.value) attributes['aria-invalid'] = 'true';
  return attributes;
});

const searchBindings: KeyBinding[] = [
  { key: 'Mod-f', run: openSearchPanel, scope: 'editor search-panel' },
  { key: 'Mod-h', run: (view) => { openSearchPanel(view); return true; }, scope: 'editor search-panel', preventDefault: true },
  { key: 'F3', run: findNext, shift: findPrevious, scope: 'editor search-panel', preventDefault: true },
  { key: 'Mod-g', run: findNext, shift: findPrevious, scope: 'editor search-panel', preventDefault: true },
  { key: 'Escape', run: closeSearchPanel, scope: 'editor search-panel' },
  { key: 'Mod-Shift-l', run: selectSelectionMatches },
];

function editorPhrases(): Record<string, string> {
  return {
    Find: t('codeEditor.find'),
    Replace: t('codeEditor.replaceInput'),
    next: t('codeEditor.next'),
    previous: t('codeEditor.previous'),
    all: t('codeEditor.all'),
    'match case': t('codeEditor.matchCase'),
    regexp: t('codeEditor.regexp'),
    'by word': t('codeEditor.byWord'),
    replace: t('codeEditor.replace'),
    'replace all': t('codeEditor.replaceAll'),
    close: t('codeEditor.close'),
    'current match': t('codeEditor.currentMatch'),
    'on line': t('codeEditor.onLine'),
    'replaced $ matches': t('codeEditor.replacedMatches'),
    'replaced match on line $': t('codeEditor.replacedMatchOnLine'),
  };
}

/** 运行时主题切换：同时重配 CodeMirror 主题与语法高亮，不动文档/编辑行为。 */
function applyThemeMode(next: CodeEditorThemeMode) {
  themeMode.value = next;
  editor?.dispatch({
    effects: [
      themeCompartment.reconfigure(codeEditorTheme(next)),
      highlightCompartment.reconfigure(syntaxHighlighting(codeEditorHighlightStyle(next))),
    ],
  });
}

onMounted(() => {
  if (!host.value) return;
  themeMode.value = resolveCodeEditorThemeMode();
  editor = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        lineNumbers(), highlightActiveLineGutter(), history(), drawSelection(), highlightActiveLine(),
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab, ...searchBindings]),
        language.of(codeEditorLanguageExtension(props.language)),
        editable.of([EditorView.editable.of(!props.disabled), EditorState.readOnly.of(Boolean(props.disabled))]),
        phrases.of(EditorState.phrases.of(editorPhrases())),
        search({ top: true }),
        highlightSelectionMatches(),
        contentAttributes.of(EditorView.contentAttributes.of(contentAriaAttributes.value)),
        themeCompartment.of(codeEditorTheme(themeMode.value)),
        highlightCompartment.of(syntaxHighlighting(codeEditorHighlightStyle(themeMode.value))),
        EditorView.updateListener.of((update) => {
          const externalSync = update.transactions.some((transaction) => transaction.annotation(externalModelValueSync));
          if (update.docChanged && !externalSync) emit('update:modelValue', update.state.doc.toString());
        }),
      ],
    }),
  });

  themeObserver = new MutationObserver(() => {
    const next = resolveCodeEditorThemeMode();
    if (next !== themeMode.value) applyThemeMode(next);
  });
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
});

watch(() => props.modelValue, (value) => {
  if (!editor || value === editor.state.doc.toString()) return;
  editor.dispatch({
    changes: { from: 0, to: editor.state.doc.length, insert: value },
    annotations: externalModelValueSync.of(true),
  });
});

watch(() => props.language, (value) => {
  editor?.dispatch({ effects: language.reconfigure(codeEditorLanguageExtension(value)) });
});

watch(() => props.disabled, (value) => {
  editor?.dispatch({ effects: editable.reconfigure([EditorView.editable.of(!value), EditorState.readOnly.of(Boolean(value))]) });
});

watch(contentAriaAttributes, (value) => {
  editor?.dispatch({ effects: contentAttributes.reconfigure(EditorView.contentAttributes.of(value)) });
});

watch(locale, () => {
  editor?.dispatch({ effects: phrases.reconfigure(EditorState.phrases.of(editorPhrases())) });
});

onBeforeUnmount(() => {
  themeObserver?.disconnect();
  themeObserver = undefined;
  editor?.destroy();
  editor = null;
});
</script>

<template>
  <div
    ref="host"
    class="w-full min-w-0 max-w-full min-h-0 overflow-hidden rounded-control border"
    :class="[
      size === 'large' ? 'h-[min(620px,calc(100dvh-360px))] min-h-105' : 'h-full',
      isInvalid ? 'border-danger-border' : 'border-stroke',
    ]"
  />
</template>
