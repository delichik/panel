import { describe, expect, it } from 'vitest';
import { emptyCredentialInput, secretPayload, validateCredentialInput } from './credentialForm';

describe('credential form model', () => {
  it('keeps secrets out of edit payloads when fields are blank', () => {
    expect(secretPayload({ name: 'deploy', type: 'password', username: 'root', password: '' }, true)).toEqual({
      name: 'deploy',
      type: 'password',
      username: 'root',
    });
    expect(secretPayload({ name: 'deploy', type: 'private_key', username: 'root', password: 'hidden', privateKey: '', passphrase: '' }, true)).toEqual({
      name: 'deploy',
      type: 'private_key',
      username: 'root',
    });
  });

  it('requires a secret when creating credentials', () => {
    expect(validateCredentialInput({ name: 'deploy', type: 'private_key', username: 'root', privateKey: '' }, false)).toMatchObject({
      privateKey: 'credentialsPage.validationPrivateKey',
    });
  });

  it('starts from an empty password credential', () => {
    expect(emptyCredentialInput()).toEqual({ name: '', type: 'password', username: '', password: '', privateKey: '', passphrase: '' });
  });
});
