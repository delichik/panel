// @vitest-environment jsdom
import { mount, type VueWrapper } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import CredentialFormFields from './CredentialFormFields.vue';
import { emptyCredentialInput, type CredentialFormLabels } from './credentialForm';
import { useI18n } from '@/i18n';
import type { CredentialInput } from '@/types/credentials';

const labels: CredentialFormLabels = {
  name: 'Name',
  type: 'Type',
  username: 'Username',
  password: 'Password',
  privateKey: 'Private key',
  passphrase: 'Passphrase',
  leaveSecretBlank: 'Leave empty to keep current secret',
  typeChangedRequiresSecret: 'New secret required',
  blankSecretKeepsCurrent: 'Blank keeps current secret',
};
const typeOptions = [
  { value: 'password', label: 'Password' },
  { value: 'private_key', label: 'Private key' },
];

let wrapper: VueWrapper | undefined;
async function render(input: CredentialInput, errors: Partial<Record<keyof CredentialInput, string>> = {}, editing = false, typeChanged = false) {
  wrapper = mount(CredentialFormFields, {
    props: { input, editing, typeChanged, typeOptions, errors, labels },
    global: {
      stubs: {
        Input: { props: ['modelValue', 'type', 'placeholder', 'invalid'], emits: ['update:modelValue'], template: '<input :type="type" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
        Select: { props: ['modelValue', 'options'], emits: ['update:modelValue'], template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>' },
        CodeEditor: { props: ['modelValue', 'language', 'editorLabel', 'invalid'], emits: ['update:modelValue'], template: '<textarea :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />' },
      },
    },
  });
  return wrapper;
}

beforeEach(() => { useI18n().setLocale('en'); });
afterEach(() => { wrapper?.unmount(); wrapper = undefined; });

describe('credential form fields', () => {
  it('renders password fields with inline errors and emits field updates', async () => {
    await render({ ...emptyCredentialInput(), password: '' }, { name: 'credentialsPage.validationName', password: 'credentialsPage.validationPassword' });
    expect(wrapper!.text()).toContain('Credential name is required.');
    expect(wrapper!.text()).toContain('Password is required.');
    expect(wrapper!.text()).not.toContain('Passphrase');
    const nameInput = wrapper!.find('input');
    await nameInput.setValue('deploy');
    const emitted = wrapper!.emitted('update:input');
    expect(emitted?.at(-1)?.[0]).toMatchObject({ name: 'deploy' });
  });

  it('switches to the private key layout with the key editor and hides password-only banners when creating', async () => {
    await render({ ...emptyCredentialInput(), type: 'private_key', privateKey: '', passphrase: '' }, { privateKey: 'credentialsPage.validationPrivateKey' });
    expect(wrapper!.text()).toContain('Private key is required.');
    expect(wrapper!.text()).toContain('Passphrase');
    expect(wrapper!.text()).not.toContain('Password is required.');
    expect(wrapper!.text()).not.toContain('Blank keeps current secret');
  });

  it('shows editing secret hints only in edit mode', async () => {
    await render({ ...emptyCredentialInput(), password: '' }, {}, true, false);
    expect(wrapper!.text()).toContain('Blank keeps current secret');
    await wrapper!.setProps({ typeChanged: true });
    expect(wrapper!.text()).toContain('New secret required');
  });
});
