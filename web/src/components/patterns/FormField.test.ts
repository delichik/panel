// @vitest-environment jsdom
import { h } from 'vue';
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import FormField from './FormField.vue';

enableAutoUnmount(afterEach);

/** 插槽属性契约：调用方把这些值绑到实际控件上。 */
type FormFieldSlot = {
  id: string;
  describedBy?: string;
  invalid: boolean;
  required: boolean;
  disabled: boolean;
};

function control(props: Record<string, unknown>) {
  const slot = props as FormFieldSlot;
  return h('input', {
    id: slot.id,
    'aria-describedby': slot.describedBy,
    'aria-invalid': slot.invalid ? 'true' : undefined,
    required: slot.required,
    disabled: slot.disabled,
  });
}

function render(props: Record<string, unknown> = {}) {
  return mount(FormField, {
    props: { label: 'Name', ...props },
    slots: { default: control },
  });
}

describe('FormField', () => {
  it('wires label, generated id and hint via aria-describedby', () => {
    const wrapper = render({ hint: 'Shown to operators' });
    const input = wrapper.get('input');
    const fieldId = input.attributes('id');
    expect(fieldId).toBeTruthy();

    const label = wrapper.get('label');
    expect(label.attributes('for')).toBe(fieldId);
    expect(label.classes()).toContain('text-caption');
    expect(label.classes()).toContain('text-muted-foreground');

    const hint = wrapper.get(`#${fieldId}-hint`);
    expect(hint.text()).toBe('Shown to operators');
    expect(hint.classes()).toContain('text-caption');
    expect(hint.classes()).toContain('text-secondary-foreground');

    expect(input.attributes('aria-describedby')).toBe(hint.attributes('id'));
    expect(input.attributes('aria-invalid')).toBeUndefined();
    expect(wrapper.get('[data-form-field]').attributes('data-invalid')).toBeUndefined();
  });

  it('reports error state: danger label, aria-invalid and error in aria-describedby', () => {
    const wrapper = render({ hint: 'Hint', error: 'Name is required' });
    const input = wrapper.get('input');
    const fieldId = input.attributes('id')!;

    expect(input.attributes('aria-invalid')).toBe('true');
    expect(input.attributes('aria-describedby')).toBe(`${fieldId}-error`);
    // 错误优先：hint 让位，避免 aria-describedby 指向已卸载节点。
    expect(wrapper.find(`#${fieldId}-hint`).exists()).toBe(false);

    const error = wrapper.get(`#${fieldId}-error`);
    expect(error.text()).toBe('Name is required');
    expect(error.classes()).toContain('text-caption');
    expect(error.classes()).toContain('text-danger');
    expect(wrapper.get('label').classes()).toContain('text-danger');
    expect(wrapper.get('[data-form-field]').attributes('data-invalid')).toBe('true');
  });

  it('exposes required / disabled to the control and keeps the field id stable', () => {
    const wrapper = render({ required: true, disabled: true });
    const input = wrapper.get('input');
    expect(input.attributes('required')).toBeDefined();
    expect(input.attributes('disabled')).toBeDefined();
    expect(wrapper.get('label').find('span[aria-hidden="true"]').text()).toBe('*');
    expect(wrapper.get('[data-form-field]').attributes('data-disabled')).toBe('true');
    expect(wrapper.get('label').attributes('for')).toBe(input.attributes('id'));
  });

  it('generates an id when none is provided and honours an explicit id', () => {
    const generated = render();
    const generatedId = generated.get('input').attributes('id');
    expect(generatedId).toBeTruthy();
    expect(generatedId).not.toBe('undefined');
    expect(generatedId).toMatch(/-field$/);
    expect(generated.get('label').attributes('for')).toBe(generatedId);

    const explicit = render({ id: 'custom-field' });
    expect(explicit.get('input').attributes('id')).toBe('custom-field');
    expect(explicit.get('label').attributes('for')).toBe('custom-field');
  });

  it('lays out vertically by default and switches to the SettingsCard-style horizontal row', () => {
    const vertical = render();
    expect(vertical.get('[data-form-field]').attributes('data-layout')).toBe('vertical');
    expect(vertical.get('[data-form-field]').classes()).not.toContain('sm:grid-cols-field');

    const horizontal = render({ layout: 'horizontal' });
    expect(horizontal.get('[data-form-field]').attributes('data-layout')).toBe('horizontal');
    expect(horizontal.get('[data-form-field]').classes()).toContain('sm:grid-cols-field');
    // 横向布局下 label 仍然是独立的 label 元素，包住控件的只有控件列。
    expect(horizontal.get('label').find('input').exists()).toBe(false);
  });
});
