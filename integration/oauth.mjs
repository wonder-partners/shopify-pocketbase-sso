import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import PocketBase from 'pocketbase';

const input = JSON.parse(readFileSync(0, 'utf8'));
// Browser Web Storage contract, without a demo page or custom auth store.
const storage = new Map();
globalThis.window = {
  localStorage: {
    getItem: key => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, String(value)),
    removeItem: key => storage.delete(key),
  },
  addEventListener() {},
};
const originalFetch = globalThis.fetch;
globalThis.fetch = async (url, options) => {
  assert.ok(new URL(url).pathname.startsWith('/api/collections/'), 'SDK must use native collection APIs only');
  const response = await originalFetch(url, options);
  for (const cookie of response.headers.getSetCookie()) {
    assert.equal(cookie.split('=')[0], 'shopify_oauth', 'Unexpected session cookie');
  }
  return response;
};
const pb = new PocketBase(input.url);
let authChanges = 0;
pb.authStore.onChange(() => authChanges++);
const result = await pb.collection(input.collection).authWithOAuth2Code(
  'shopify', input.code, input.codeVerifier, input.redirectURL,
  {email: 'forged@example.test', shopify_sub: 'forged-subject', verified: true},
  {headers: {Origin: input.url, Cookie: input.cookie}},
);
assert.equal(pb.authStore.isValid, true);
assert.equal(pb.authStore.record.id, result.record.id);
assert.equal(authChanges, 1);
assert.equal(result.record.email, 'camille@example.test');
for (const field of ['shopify_sub', 'shopify_issuer', 'shopify_customer_id']) {
  assert.equal(Object.hasOwn(result.record, field), false);
}
assert.equal(Object.hasOwn(result.meta.rawUser, 'customerId'), false);
assert.equal(result.meta.isNew, true);
assert.equal(result.meta.accessToken, '');
assert.equal(result.meta.refreshToken, '');
await pb.collection(input.collection).getOne(result.record.id);
await assert.rejects(
  pb.collection(input.collection).authWithOAuth2Code(
    'shopify', input.code, input.codeVerifier, input.redirectURL, {},
    {headers: {Origin: input.url, Cookie: input.cookie}},
  ), e => e.status === 400,
);
// A new frontend SDK instance restores its state from the default LocalAuthStore.
// It never needs a server-side session/bootstrap endpoint.
const reloaded = new PocketBase(input.url);
assert.equal(reloaded.authStore.token, result.token);
assert.equal(reloaded.authStore.record.id, result.record.id);
assert.equal(reloaded.authStore.isValid, true);
await reloaded.collection(input.collection).authRefresh();
await reloaded.collection(input.collection).getOne(result.record.id);
reloaded.authStore.clear();
assert.equal(new PocketBase(input.url).authStore.isValid, false);
console.log('Official SDK OAuth, persistence, native refresh, local logout and replay rejection passed without a session bridge.');
