<script setup lang="ts">
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { defaultHighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { Annotation, Compartment, EditorState } from '@codemirror/state';
import { drawSelection, EditorView, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers } from '@codemirror/view';
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
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

const theme = EditorView.theme({
  '&': { height: '100%', minHeight: '0', backgroundColor: 'var(--panel-bg)', color: 'var(--panel-text)' },
  '&.cm-focused': { outline: '2px solid var(--panel-ring)', outlineOffset: '-2px' },
  '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono, ui-monospace, monospace)', lineHeight: '1.55' },
  '.cm-content': { padding: '0.75rem 0' },
  '.cm-gutters': { backgroundColor: 'var(--panel-muted)', color: 'var(--panel-text-muted)', borderRight: '1px solid var(--panel-border)' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'var(--panel-hover)' },
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
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        language.of(codeEditorLanguageExtension(props.language)),
        editable.of([EditorView.editable.of(!props.disabled), EditorState.readOnly.of(Boolean(props.disabled))]),
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

onBeforeUnmount(() => editor?.destroy());
</script>

<template>
  <div
    ref="host"
    class="min-h-0 overflow-hidden rounded-lg border"
    :class="[
      size === 'large' ? 'h-[min(620px,calc(100dvh-360px))] min-h-[420px]' : 'h-full',
      invalid ? 'border-danger-border' : 'border-border',
    ]"
  />
</template>
