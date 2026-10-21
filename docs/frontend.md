# Frontend example

These pages use PocketBase's [manual OAuth2 code exchange](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2) to sign customers in with Shopify. They first try to sign in without asking the customer to interact. If that fails because sign-in is required, they show a sign-in link. The `authWithOAuth2` popup flow is not supported.

Create a `pb_public` folder in your PocketBase application and save both files below inside it. The [Go example](../README.md#5-register-the-service) serves these pages. They use the same origin as the PocketBase API and the `users` auth collection. If your collection has another name, replace `users` in both files.

## `pb_public/index.html`

If there is no PocketBase session, try Shopify silently. If an interactive login is needed, display the sign-in link.

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Sign in</title>
</head>
<body>
  <a id="login" hidden>Sign in with Shopify</a>

  <script type="module">
    import PocketBase from 'https://cdn.jsdelivr.net/npm/pocketbase@0.28.1/dist/pocketbase.es.mjs';

    const pb = new PocketBase(location.origin);
    const redirectURL = new URL('/redirect.html', location.origin).href;
    const login = document.getElementById('login');
    const skipSilent = sessionStorage.getItem('shopify.skipSilent') === '1';
    sessionStorage.removeItem('shopify.skipSilent');

    if (!pb.authStore.isValid) {
      const methods = await pb.collection('users').listAuthMethods();
      const provider = methods.oauth2.providers.find(p => p.name === 'shopify');
      if (!provider) throw new Error('Shopify is not available.');

      login.href = provider.authURL + encodeURIComponent(redirectURL);
      function saveProvider() {
        sessionStorage.setItem('shopify.oauth', JSON.stringify({
          ...provider,
          expires: Date.now() + 5 * 60 * 1000,
        }));
      }
      login.addEventListener('click', saveProvider);

      if (skipSilent) {
        login.hidden = false;
      } else {
        saveProvider();
        const url = new URL(login.href);
        url.searchParams.set('prompt', 'none');
        location.assign(url.href);
      }
    }
  </script>
</body>
</html>
```

## `pb_public/redirect.html`

This page checks the callback and exchanges the code for a PocketBase session. If Shopify requires an interactive login, it returns to the sign-in page and shows the link without repeating the silent attempt.

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="referrer" content="no-referrer">
  <title>Sign-in callback</title>
</head>
<body>
  <a href="/index.html">Return to sign in</a>

  <script type="module">
    import PocketBase from 'https://cdn.jsdelivr.net/npm/pocketbase@0.28.1/dist/pocketbase.es.mjs';

    const pb = new PocketBase(location.origin);
    const redirectURL = new URL('/redirect.html', location.origin).href;
    const params = new URLSearchParams(location.search);
    history.replaceState(null, '', location.pathname);

    // Do not repeat silent authorization when returning to the login page.
    sessionStorage.setItem('shopify.skipSilent', '1');
    const saved = sessionStorage.getItem('shopify.oauth');
    sessionStorage.removeItem('shopify.oauth');
    const provider = JSON.parse(saved || 'null');
    if (!provider || !Number.isFinite(provider.expires) ||
        provider.expires <= Date.now() || provider.state !== params.get('state')) {
      throw new Error('Invalid or expired Shopify callback.');
    }

    const error = params.get('error');
    if (error === 'login_required') {
      location.replace('/index.html');
    } else if (error) {
      throw new Error('Shopify did not authorize sign-in.');
    } else {
      const code = params.get('code');
      if (!code) throw new Error('Missing Shopify code.');

      // Prevent linking Shopify to an account already signed in here.
      pb.authStore.clear();
      await pb.collection('users').authWithOAuth2Code(
        provider.name, code, provider.codeVerifier, redirectURL,
      );
      location.replace('/index.html');
    }
  </script>
</body>
</html>
```

## Before deployment

The SDK stores the PocketBase session in `pb.authStore`. Add your application's error handling and, if enabled, the [MFA second-factor step](https://pocketbase.io/docs/authentication/#multi-factor-authentication). These examples leave errors to the browser rather than implementing an error UI.

Serve the callback with `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. Protect the pages from script injection and do not log codes or tokens. If the frontend and API use different origins, inject their URLs through your frontend configuration and enable CORS. The backend expects the request's `Origin` to match the callback origin; browser JavaScript cannot read Go environment variables directly.
