import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import PocketBase from 'pocketbase';

const input = JSON.parse(readFileSync(0, 'utf8'));
const html = readFileSync(new URL('../pb_public/index.html', import.meta.url), 'utf8');
const source = html.match(/<script type="module">\s*([\s\S]*?)<\/script>/)[1]
  .replace(/^import PocketBase[^\n]+\n/, '');
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const execute = new AsyncFunction('PocketBase', 'document', 'location', 'history', 'sessionStorage', source);
function storage() {
  const data = new Map();
  return {
    getItem: key => data.get(key) ?? null,
    setItem: (key, value) => data.set(key, String(value)),
    removeItem: key => data.delete(key),
  };
}
const localStorage = storage();
const sessionStorage = storage();
globalThis.window = {localStorage, location: {origin: input.url}, addEventListener() {}};
const nativeFetch = globalThis.fetch;
let exchanges = 0;
let injectedResponse;
let page;
let shopCookie = '';
let phase = 'initialize';
globalThis.fetch = async (url, options = {}) => {
  const target = new URL(url);
  assert.equal(target.origin, input.url);
  assert.ok(target.pathname.startsWith('/api/collections/'));
  const headers = new Headers(options.headers);
  assert.equal(headers.has('Cookie'), false);
  if (options.method === 'POST') headers.set('Origin', input.url);
  if (target.pathname.endsWith('/auth-with-oauth2')) {
    assert.equal(page.location.search, '');
    assert.equal(sessionStorage.getItem('shopify.oauth'), null);
    exchanges++;
    if (injectedResponse) return injectedResponse;
  }
  const result = await nativeFetch(url, {...options, headers});
  assert.equal(result.headers.getSetCookie().length, 0);
  return result;
};

async function load(url = '/') {
  const current = new URL(url, input.url);
  const elements = new Map(['status', 'account', 'login', 'silent', 'verify', 'logout'].map(id => [id, {
    textContent: '', hidden: false, disabled: true,
    addEventListener(type, handler) { this[type] = handler; },
    set innerHTML(value) { throw new Error('HTML rendering forbidden'); },
  }]));
  const location = {
    pathname: current.pathname, search: current.search,
    assign(destination) { this.destination = destination; },
  };
  page = {elements, location};
  const history = {replaceState(_, __, url) {
    const next = new URL(url, input.url);
    location.pathname = next.pathname;
    location.search = next.search;
  }};
  await execute(PocketBase, {getElementById: id => elements.get(id)}, location, history, sessionStorage);
  return page;
}
const text = () => page.elements.get('status').textContent;
async function click(id) {
  const button = page.elements.get(id);
  assert.equal(button.disabled, false);
  await button.click();
}
async function authorize() {
  const result = await nativeFetch(page.location.destination, {
    redirect: 'manual', headers: shopCookie ? {Cookie: shopCookie} : {},
  });
  assert.equal(result.status, 302);
  return new URL(result.headers.get('location'));
}

async function main() {
  phase = 'silent anonymous';
  await load();
  assert.equal(new URL(page.location.destination).searchParams.get('prompt'), 'none');
  await load(await authorize());
  assert.match(text(), /bouton de connexion/);
  assert.equal(page.location.destination, undefined, 'No silent loop from callback');
  assert.equal(exchanges, 0);

  phase = 'interactive native auth and API';
  await click('login');
  assert.equal(new URL(page.location.destination).searchParams.has('prompt'), false);
  const form = await nativeFetch(page.location.destination, {redirect: 'manual'});
  assert.equal(form.status, 200);
  const login = await nativeFetch(input.shopURL + '/login', {method: 'POST', redirect: 'manual'});
  shopCookie = login.headers.getSetCookie()[0].split(';')[0];
  const callback = await authorize();
  await load(callback);
  assert.equal(exchanges, 1);
  assert.match(text(), /API PocketBase : accès confirmé/);
  assert.equal(page.location.pathname, '/');
  assert.equal(page.location.search, '');
  assert.equal(page.elements.get('account').hidden, false);
  assert.ok(page.elements.get('account').textContent.includes('@'));
  assert.equal(new PocketBase(input.url).authStore.isValid, true);

  phase = 'restored auth store';
  await load();
  assert.match(text(), /accès confirmé/);
  assert.equal(page.location.destination, undefined);
  await click('verify');
  assert.match(text(), /accès confirmé/);

  phase = 'replay and local logout';
  await load(callback);
  assert.equal(exchanges, 1);
  assert.match(text(), /invalide ou expiré/);
  await click('logout');
  assert.equal(new PocketBase(input.url).authStore.isValid, false);
  assert.equal(page.elements.get('account').hidden, true);
  assert.equal(page.elements.get('account').textContent, '');
  assert.equal(sessionStorage.getItem('shopify.oauth'), null);
  assert.equal(page.location.destination, undefined, 'Logout does not auto-login');
  await click('silent');
  await load(await authorize());
  assert.match(text(), /accès confirmé/);
  assert.equal(exchanges, 2);

  phase = 'bad state rejected before exchange';
  await click('logout');
  await click('silent');
  const forged = await authorize();
  forged.searchParams.set('state', 'wrong');
  await load(forged);
  assert.equal(exchanges, 2);
  assert.match(text(), /invalide ou expiré/);
  assert.equal(sessionStorage.getItem('shopify.oauth'), null);

  phase = 'OAuth error rendered safely';
  await click('silent');
  const denied = await authorize();
  denied.searchParams.set('error', '<img src=x onerror=alert(1)>');
  await load(denied);
  assert.equal(text().includes('<img'), false);
  assert.equal(page.location.destination, undefined);
  assert.equal(exchanges, 2);

  phase = 'MFA challenge';
  await click('login');
  injectedResponse = new Response(JSON.stringify({mfaId: 'test-mfa'}), {status: 401});
  await load(await authorize());
  assert.match(text(), /second facteur MFA/);
  assert.equal(new PocketBase(input.url).authStore.isValid, false);

  phase = 'account conflict';
  await click('login');
  injectedResponse = new Response('{}', {status: 409});
  await load(await authorize());
  assert.match(text(), /Aucune fusion automatique/);
  assert.equal(new PocketBase(input.url).authStore.isValid, false);
}

// Print only our fixed phase names, never an SDK error or credentials.
await main().catch(() => {
  process.stderr.write(`Frontend check failed at: ${phase}\n`);
  process.exitCode = 1;
});
