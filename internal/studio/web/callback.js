import PocketBase from './pocketbase.es.mjs';

const element = document.getElementById('oauth');
const {code, verifier, redirect, collection} = element.dataset;
// Remove the one-time code from the address bar before any API calls.
history.replaceState(null, '', '/studio/callback');
for (const key of Object.keys(element.dataset)) delete element.dataset[key];
const pb = new PocketBase(window.location.origin);
// A previous account must not silently become the linking target of this login.
pb.authStore.clear();
try {
  await pb.collection(collection).authWithOAuth2Code('shopify', code, verifier, redirect);
  window.location.replace('/studio');
} catch (error) {
  pb.authStore.clear();
  document.getElementById('oauth-status').textContent = error.response?.mfaId
    ? 'PocketBase demande un second facteur. Son interface de saisie reste à intégrer.'
    : 'La connexion PocketBase a été refusée. Tu peux recommencer depuis le studio.';
}
