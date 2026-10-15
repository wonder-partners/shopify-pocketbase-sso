import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import PocketBase from 'pocketbase';

const input = JSON.parse(readFileSync(0, 'utf8'));
const pb = new PocketBase(input.url);
const result = await pb.collection(input.collection).authWithOAuth2Code(
  'shopify', input.code, input.codeVerifier, input.redirectURL,
  {email: 'forged@example.test', shopify_sub: 'forged-subject', verified: true},
  {headers: {Origin: input.url, Cookie: input.cookie}},
);
assert.equal(pb.authStore.isValid, true);
assert.equal(pb.authStore.record.id, result.record.id);
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
console.log('Official SDK authWithOAuth2Code, native signup, protected profile and replay rejection passed.');
