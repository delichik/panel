import type { CredentialInput, CredentialType } from '@/types/credentials';

export function validateCredentialInput(input: CredentialInput, editing: boolean, typeChanged = false) {
  const errors: Partial<Record<keyof CredentialInput, string>> = {};
  if (!input.name.trim()) errors.name = 'credentialsPage.validationName';
  if (!input.username.trim()) errors.username = 'credentialsPage.validationUsername';
  if (input.type === 'password' && (!editing || typeChanged) && !input.password?.trim()) errors.password = 'credentialsPage.validationPassword';
  if (input.type === 'private_key' && (!editing || typeChanged) && !input.privateKey?.trim()) errors.privateKey = 'credentialsPage.validationPrivateKey';
  return errors;
}

export function secretPayload(input: CredentialInput, editing: boolean): CredentialInput {
  const base: CredentialInput = { name: input.name, username: input.username, type: input.type as CredentialType };
  if (input.type === 'password') {
    if (!editing || input.password?.trim()) base.password = input.password ?? '';
    return base;
  }
  if (!editing || input.privateKey?.trim()) base.privateKey = input.privateKey ?? '';
  if (input.passphrase?.trim()) base.passphrase = input.passphrase;
  return base;
}
