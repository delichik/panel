import { describe, expect, it } from 'vitest';
import { secretPayload, validateCredentialInput } from './model';

describe('credential model', () => {
  it('keeps secrets out of edit payloads when fields are blank', () => {
    expect(secretPayload({ name: 'deploy', type: 'password', username: 'root', password: '' }, true)).toEqual({
      name: 'deploy',
      type: 'password',
      username: 'root',
    });
  });

  it('requires a secret when creating credentials', () => {
    expect(validateCredentialInput({ name: 'deploy', type: 'private_key', username: 'root', privateKey: '' }, false)).toMatchObject({
      privateKey: 'credentialsPage.validationPrivateKey',
    });
  });
});
