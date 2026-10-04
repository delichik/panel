export interface TemplateVariableOption {
  id: string;
  label: string;
  expression: string;
  description?: string;
}

export function insertTemplateVariable(
  currentValue: string,
  expression: string,
  selectionStart?: number | null,
  selectionEnd?: number | null,
) {
  const start = selectionStart ?? currentValue.length;
  const end = selectionEnd ?? start;
  return {
    value: `${currentValue.slice(0, start)}${expression}${currentValue.slice(end)}`,
    cursor: start + expression.length,
  };
}
