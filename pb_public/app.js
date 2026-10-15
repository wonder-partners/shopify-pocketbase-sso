// Official SDK 0.28.1, copied locally to avoid third-party scripts on the callback.
import PocketBase from './vendor/pocketbase.es.mjs';

const pb = new PocketBase(window.location.origin);
const redirectURL = new URL('/auth/callback', window.location.origin).href;
const status = document.querySelector('#status');
const apiStatus = document.querySelector('#api-status');
const buttons = Object.fromEntries(['login', 'sso', 'verify', 'logout'].map(id => [id, document.querySelector('#' + id)]));
let busy = false;

document.querySelector('#callback').textContent = redirectURL;

async function loginWithShopify(silent = false) {
  const methods = await pb.collection('users').listAuthMethods();
  const provider = methods.oauth2.providers.find(p => p.name === 'shopify');
  if (!provider) throw new Error('Shopify non configuré.');

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
    throw new Error('Retour Shopify invalide ou expiré.');
  }

  if (params.has('error')) return { error: params.get('error') };
  const code = params.get('code');
  if (!code) throw new Error('Code Shopify manquant.');

  // Empêcher la liaison avec un autre compte déjà connecté.
  pb.authStore.clear();
  return pb.collection('users').authWithOAuth2Code(
    'shopify', code, flow.verifier, redirectURL,
  );
}

function render() {
  const user = pb.authStore.isValid ? pb.authStore.record : null;
  document.querySelector('#authenticated').textContent = user ? 'Oui' : 'Non';
  document.querySelector('#email').textContent = user?.email || '—';
  document.querySelector('#user-id').textContent = user?.id || '—';
  buttons.login.disabled = buttons.sso.disabled = busy;
  buttons.verify.disabled = buttons.logout.disabled = busy || !user;
}

async function verifyAPI() {
  await pb.collection('users').authRefresh();
  await pb.collection('users').getOne(pb.authStore.record.id);
  apiStatus.textContent = 'API PocketBase : accès confirmé avec le SDK officiel.';
}

async function run(action) {
  if (busy) return;
  busy = true;
  status.dataset.error = 'false';
  status.textContent = 'Vérification en cours…';
  render();
  try {
    await action();
  } catch (error) {
    status.dataset.error = 'true';
    // Never log the SDK error object: it can contain OAuth request parameters.
    status.textContent = error.response?.mfaId
      ? 'Un deuxième facteur PocketBase est requis. Ce frontend minimal ne gère pas la MFA.'
      : error.response?.message || error.message || 'La vérification a échoué.';
    apiStatus.textContent = 'Accès API non confirmé.';
  } finally {
    busy = false;
    render();
  }
}

buttons.login.addEventListener('click', () => run(() => loginWithShopify(false)));
buttons.sso.addEventListener('click', () => run(() => loginWithShopify(true)));
buttons.verify.addEventListener('click', () => run(async () => {
  await verifyAPI();
  status.textContent = 'Le token PocketBase et la règle de lecture du profil sont acceptés.';
}));
buttons.logout.addEventListener('click', () => {
  pb.authStore.clear();
  sessionStorage.removeItem('shopify.oauth');
  history.replaceState(null, '', '/');
  status.dataset.error = 'false';
  status.textContent = 'Déconnecté localement. La session Shopify est inchangée.';
  apiStatus.textContent = 'Accès API non vérifié.';
});
pb.authStore.onChange(render);

render();
if (location.pathname === new URL(redirectURL).pathname) {
  run(async () => {
    const result = await completeShopifyLogin();
    if (result.error) {
      status.textContent = result.error === 'access_denied'
        ? 'Connexion annulée.'
        : 'Une connexion Shopify interactive est nécessaire. Utilise le bouton de connexion.';
      return; // Never restart prompt=none from the callback.
    }
    status.textContent = 'Connecté avec Shopify. Auth store PocketBase mis à jour.';
    await verifyAPI();
  });
} else if (new URLSearchParams(location.search).has('code') || new URLSearchParams(location.search).has('state')) {
  history.replaceState(null, '', location.pathname);
  sessionStorage.removeItem('shopify.oauth');
  status.dataset.error = 'true';
  status.textContent = 'Le callback reçu ne correspond pas au chemin /auth/callback de ce frontend.';
} else if (pb.authStore.isValid) {
  run(async () => {
    await verifyAPI();
    status.textContent = 'Session PocketBase restaurée et vérifiée.';
  });
} else {
  status.textContent = 'Prêt. Choisis le test SSO silencieux ou la connexion interactive.';
}
