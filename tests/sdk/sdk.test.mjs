import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import PocketBase from 'pocketbase';

// All identities and tokens come from an isolated Go test fixture through stdin.
// Never pass real customer tokens on a command line or print them.
const input = JSON.parse(readFileSync(0, 'utf8'));
for (const current of input.accounts) {
  const pb = new PocketBase(input.url);
  pb.authStore.save(current.token, {id: current.id, collectionName: 'users'});
  assert.equal(pb.authStore.isValid, true);
  const refreshed = await pb.collection('users').authRefresh();
  assert.equal(refreshed.record.id, current.id);
  assert.equal(pb.authStore.record.id, current.id);
  const profile = await pb.collection('users').getOne(current.id);
  assert.equal(profile.id, current.id);
  const docs = await pb.collection('documents').getFullList();
  assert.equal(docs.length, 1);
  assert.equal(docs[0].owner, current.id);
  await assert.rejects(pb.collection('documents').getOne(current.otherDoc), e => e.status === 404);
  const doc = await pb.collection('documents').create({title: 'SDK document', owner: current.id});
  assert.equal(doc.owner, current.id);
  await pb.collection('documents').update(doc.id, {title: 'SDK update'});
  await pb.collection('documents').delete(doc.id);
  pb.authStore.clear();
  assert.equal(pb.authStore.isValid, false);
  await assert.rejects(pb.collection('users').getOne(current.id), e => e.status === 404);
}
console.log('SDK PocketBase: authStore, authRefresh, CRUD and owner rules passed for Shopify and password accounts.');
