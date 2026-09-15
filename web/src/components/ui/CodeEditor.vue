<script setup lang="ts">
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { closeSearchPanel, findNext, findPrevious, highlightSelectionMatches, openSearchPanel, search, selectSelectionMatches } from '@codemirror/search';
import { defaultHighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { Annotation, Compartment, EditorState } from '@codemirror/state';
import { drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers, type KeyBinding } from '@codemirror/view';
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from '@/i18n';
import { codeEditorLanguageExtension, type CodeEditorLanguage } from './codeEditorLanguage';

const props = defineProps<{
  modelValue: string;
  language: CodeEditorLanguage;
  editorLabel: string;
  disabled?: boolean;
  invalid?: boolean;
  size?: 'default' | 'large';
}>();
const emit = defineEmits<{ 'update:modelValue': [value: string] }>();
const host = ref<HTMLElement | null>(null);
const language = new Compartment();
const editable = new Compartment();
let editor: EditorView | null = null;
const externalModelValueSync = Annotation.define<boolean>();
const { t, locale } = useI18n();
const phrases = new Compartment();

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

const theme = EditorView.theme({
  '&': { height: '100%', minHeight: '0', backgroundColor: 'var(--panel-muted)', color: 'var(--panel-text)' },
  '&.cm-focused': { outline: '2px solid var(--panel-ring)', outlineOffset: '-2px' },
  '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono, ui-monospace, monospace)', lineHeight: '1.55' },
  '.cm-content': { padding: '0.75rem 0' },
  '.cm-gutters': { backgroundColor: 'var(--panel-surface)', color: 'var(--panel-text-muted)', borderRight: '1px solid var(--panel-border)' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'var(--panel-hover)' },
  '.cm-panel.cm-search': {
    backgroundColor: 'var(--panel-surface)',
    color: 'var(--panel-text)',
    borderBottom: '1px solid var(--panel-border)',
    padding: '0.5rem',
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: '0.5rem',
    fontSize: '0.8125rem',
  },
  '.cm-panel.cm-search input.cm-textfield': {
    backgroundColor: 'var(--panel-surface)',
    border: '1px solid var(--panel-input-border)',
    borderRadius: '0.375rem',
    color: 'var(--panel-text)',
    padding: '0.25rem 0.5rem',
    fontFamily: 'inherit',
  },
  '.cm-panel.cm-search input.cm-textfield:focus': { outline: '2px solid var(--panel-ring)', outlineOffset: '0' },
  '.cm-panel.cm-search .cm-button': {
    backgroundColor: 'var(--panel-muted)',
    border: '1px solid var(--panel-border)',
    borderRadius: '0.375rem',
    color: 'var(--panel-text)',
    cursor: 'pointer',
    padding: '0.25rem 0.625rem',
    fontFamily: 'inherit',
    fontSize: 'inherit',
  },
  '.cm-panel.cm-search .cm-button:hover': { backgroundColor: 'var(--panel-hover)' },
  '.cm-panel.cm-search label': { display: 'inline-flex', alignItems: 'center', gap: '0.25rem', whiteSpace: 'nowrap' },
  '.cm-panel.cm-search input[type="checkbox"]': { accentColor: 'var(--panel-primary)' },
});

onMounted(() => {
  if (!host.value) return;
  editor = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        lineNumbers(), highlightActiveLineGutter(), history(), drawSelection(), highlightActiveLine(),
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab, ...searchBindings]),
        language.of(codeEditorLanguageExtension(props.language)),
        editable.of([EditorView.editable.of(!props.disabled), EditorState.readOnly.of(Boolean(props.disabled))]),
        phrases.of(EditorState.phrases.of(editorPhrases())),
        search({ top: true }),
        highlightSelectionMatches(),
        EditorView.contentAttributes.of({ 'aria-label': props.editorLabel }),
        EditorView.updateListener.of((update) => {
          const externalSync = update.transactions.some((transaction) => transaction.annotation(externalModelValueSync));
          if (update.docChanged && !externalSync) emit('update:modelValue', update.state.doc.toString());
        }),
        theme,
      ],
    }),
  });
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

watch(locale, () => {
  editor?.dispatch({ effects: phrases.reconfigure(EditorState.phrases.of(editorPhrases())) });
});

onBeforeUnmount(() => editor?.destroy());
</script>

<template>
  <div
    ref="host"
    class="w-full min-w-0 max-w-full min-h-0 overflow-hidden rounded-lg border"
    :class="[
      size === 'large' ? 'h-[min(620px,calc(100dvh-360px))] min-h-[420px]' : 'h-full',
      invalid ? 'border-danger-border' : 'border-border',
    ]"
  />
</template>
