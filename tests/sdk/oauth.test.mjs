import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import PocketBase from 'pocketbase';

const input = JSON.parse(readFileSync(0, 'utf8'));
function storage() {
  const data = new Map();
  return {
    getItem: key => data.get(key) ?? null,
    setItem: (key, value) => data.set(key, String(value)),
    removeItem: key => data.delete(key),
  };
}

// Simulated browser storage and navigation, but real SDK and HTTP APIs.
const localStorage = storage();
const sessionStorage = storage();
globalThis.window = {localStorage, addEventListener() {}};
const location = {
  search: '', pathname: '/studio',
  assign(url) { this.destination = url; },
};
let scrubbed = false;
const history = {replaceState() { location.search = ''; scrubbed = true; }};

const originalFetch = globalThis.fetch;
let nativeExchanges = 0;
globalThis.fetch = async (url, options = {}) => {
  const target = new URL(url);
  assert.equal(target.origin, input.url);
  assert.ok(target.pathname.startsWith('/api/collections/'), 'Only native collection APIs may be used');
  assert.equal(new Headers(options.headers).has('Cookie'), false, 'No PocketBase cookie is needed');
  if (target.pathname.endsWith('/auth-with-oauth2')) {
    assert.equal(scrubbed, true, 'Scrub callback before exchanging code');
    nativeExchanges++;
  }
  // Browsers set Origin automatically. Node needs the same header explicitly.
  const headers = new Headers(options.headers);
  if (options.method === 'POST') headers.set('Origin', input.url);
  const response = await originalFetch(url, {...options, headers});
  assert.equal(response.headers.getSetCookie().length, 0, 'No OAuth/session cookie may be set');
  if (target.pathname.endsWith('/auth-methods') || target.pathname.endsWith('/auth-with-oauth2')) {
    assert.equal(response.headers.get('cache-control'), 'no-store');
  }
  return response;
};

const pb = new PocketBase(input.url);
const redirectURL = input.redirectURL;

// Test-owned browser flow exercising the real PocketBase SDK and HTTP APIs.
async function loginWithShopify(silent = false) {
  const methods = await pb.collection('users').listAuthMethods();
  const provider = methods.oauth2.providers.find(p => p.name === 'shopify');
  if (!provider) throw new Error('Shopify is not configured.');

  sessionStorage.setItem('shopify.oauth', JSON.stringify({
    state: provider.state,
    verifier: provider.codeVerifier,
    expires: Date.now() + 5 * 60 * 1000,
  }));

  const url = new URL(provider.authURL + encodeURIComponent(redirectURL));
  if (silent) url.searchParams.set('prompt', 'none');
  location.assign(url.href);
}

async function completeShopifyLogin() {
  const params = new URLSearchParams(location.search);
  history.replaceState(null, '', location.pathname);

  const saved = sessionStorage.getItem('shopify.oauth');
  sessionStorage.removeItem('shopify.oauth');
  const flow = JSON.parse(saved || 'null');

  if (!flow || !Number.isFinite(flow.expires) ||
      flow.expires <= Date.now() || flow.state !== params.get('state')) {
    throw new Error('Invalid or expired Shopify callback.');
  }

  if (params.has('error')) return { error: params.get('error') };
  const code = params.get('code');
  if (!code) throw new Error('Missing Shopify code.');

  // Prevent linking to another account that is already signed in.
  pb.authStore.clear();
  return pb.collection('users').authWithOAuth2Code(
    'shopify', code, flow.verifier, redirectURL,
  );
}

let shopCookie = '';
async function authorize() {
  const response = await originalFetch(location.destination, {
    redirect: 'manual', headers: shopCookie ? {Cookie: shopCookie} : {},
  });
  assert.equal(response.status, 302);
  const callback = new URL(response.headers.get('location'));
  location.search = callback.search;
  location.pathname = callback.pathname;
  scrubbed = false;
  return callback;
}
function assertNotAuthenticated() {
  assert.equal(pb.authStore.isValid, false);
  assert.equal(pb.authStore.record, null);
}

// No Shopify session: prompt=none returns an error, no token exchange or loop.
await loginWithShopify(true);
const silentURL = new URL(location.destination);
assert.equal(silentURL.searchParams.get('prompt'), 'none');
assert.equal(silentURL.searchParams.get('nonce'), silentURL.searchParams.get('code_challenge'));
const anonymous = await authorize();
assert.equal(anonymous.searchParams.get('error'), 'login_required');
assert.deepEqual(await completeShopifyLogin(), {error: 'login_required'});
assert.equal(nativeExchanges, 0);
assert.equal(sessionStorage.getItem('shopify.oauth'), null);
assertNotAuthenticated();

// Interactive attempt, followed by completion of the mock Shopify login form.
await loginWithShopify(false);
assert.equal(new URL(location.destination).searchParams.has('prompt'), false);
const form = await originalFetch(location.destination, {redirect: 'manual'});
assert.equal(form.status, 200);
const login = await originalFetch(input.shopURL + '/login', {method: 'POST', redirect: 'manual'});
assert.equal(login.status, 303);
shopCookie = login.headers.getSetCookie()[0].split(';')[0];

// State/expiry/storage errors are rejected in the frontend, before any exchange.
for (const fault of ['state', 'expired', 'missing', 'malformed', 'missing_code']) {
  await loginWithShopify(true);
  const callback = await authorize();
  if (fault === 'state') {
    callback.searchParams.set('state', 'another-login');
    location.search = callback.search;
  } else if (fault === 'expired') {
    const flow = JSON.parse(sessionStorage.getItem('shopify.oauth'));
    flow.expires = Date.now() - 1;
    sessionStorage.setItem('shopify.oauth', JSON.stringify(flow));
  } else if (fault === 'missing') {
    sessionStorage.removeItem('shopify.oauth');
  } else if (fault === 'malformed') {
    sessionStorage.setItem('shopify.oauth', 'not-json');
  } else {
    callback.searchParams.delete('code');
    location.search = callback.search;
  }
  await assert.rejects(completeShopifyLogin());
  assert.equal(nativeExchanges, 0);
  assert.equal(location.search, '');
  assert.equal(sessionStorage.getItem('shopify.oauth'), null);
  assertNotAuthenticated();
}

await loginWithShopify(true);
const callback = await authorize();
const flow = JSON.parse(sessionStorage.getItem('shopify.oauth'));
let lastAuth;
pb.authStore.onChange((token, record) => { lastAuth = {token, record}; });
const result = await completeShopifyLogin();
assert.equal(nativeExchanges, 1);
assert.equal(pb.authStore.isValid, true);
assert.equal(pb.authStore.record.id, result.record.id);
assert.equal(lastAuth.token, result.token);
assert.equal(lastAuth.record.id, result.record.id);
assert.equal(result.record.email, 'camille@example.test');
assert.equal(sessionStorage.getItem('shopify.oauth'), null);
for (const field of ['shopify_sub', 'shopify_issuer', 'shopify_customer_id']) {
  assert.equal(Object.hasOwn(result.record, field), false);
}
assert.equal(Object.hasOwn(result.meta.rawUser, 'customerId'), false);
assert.equal(result.meta.isNew, true);
assert.equal(result.meta.accessToken, '');
assert.equal(result.meta.refreshToken, '');
await pb.collection('users').getOne(result.record.id);

// Local callback replay is stopped without a network request.
location.search = callback.search;
await assert.rejects(completeShopifyLogin());
assert.equal(nativeExchanges, 1);

// Bypassing the frontend replay check still fails at the upstream token endpoint.
await assert.rejects(pb.collection('users').authWithOAuth2Code(
  'shopify', callback.searchParams.get('code'), flow.verifier, input.redirectURL,
), error => error.status === 502);

const reloaded = new PocketBase(input.url);
assert.equal(reloaded.authStore.token, result.token);
assert.equal(reloaded.authStore.record.id, result.record.id);
assert.equal(reloaded.authStore.isValid, true);
await reloaded.collection('users').authRefresh();
await reloaded.collection('users').getOne(result.record.id);
reloaded.authStore.clear();
assert.equal(new PocketBase(input.url).authStore.isValid, false);
console.log('Native redirect, state/PKCE, silent/interactive login, authStore, refresh and replay rejection passed.');
