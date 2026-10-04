import type { Extension } from '@codemirror/state';
import { StreamLanguage } from '@codemirror/language';
import { json } from '@codemirror/lang-json';
import { yaml } from '@codemirror/lang-yaml';
import { dockerFile } from '@codemirror/legacy-modes/mode/dockerfile';
import { nginx } from '@codemirror/legacy-modes/mode/nginx';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { shell } from '@codemirror/legacy-modes/mode/shell';

export type CodeEditorLanguage = 'plain' | 'yaml' | 'json' | 'shell' | 'nginx' | 'properties' | 'dockerfile';

export function inferCodeEditorLanguage(path: string, contentType = ''): CodeEditorLanguage {
  const name = path.trim().toLowerCase().split('/').pop() ?? '';
  const mime = contentType.toLowerCase();
  if (name === 'dockerfile' || name.startsWith('dockerfile.')) return 'dockerfile';
  if (name === 'nginx.conf' || name.endsWith('.nginx') || mime.includes('nginx')) return 'nginx';
  if (/\.(ya?ml)$/.test(name) || mime.includes('yaml')) return 'yaml';
  if (name.endsWith('.json') || mime.includes('json')) return 'json';
  if (/\.(sh|bash|zsh|fish)$/.test(name) || mime.includes('shell')) return 'shell';
  if (/\.(ini|cfg|conf|properties)$/.test(name) || mime.includes('properties') || mime.includes('ini')) return 'properties';
  return 'plain';
}

export function codeEditorLanguageExtension(language: CodeEditorLanguage): Extension {
  if (language === 'yaml') return yaml();
  if (language === 'json') return json();
  if (language === 'shell') return StreamLanguage.define(shell);
  if (language === 'nginx') return StreamLanguage.define(nginx);
  if (language === 'properties') return StreamLanguage.define(properties);
  if (language === 'dockerfile') return StreamLanguage.define(dockerFile);
  return [];
}
