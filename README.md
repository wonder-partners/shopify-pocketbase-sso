# Shopify SSO for PocketBase

The Go package `shopifysso` adds Shopify Customer Account authentication to PocketBase's native OAuth2 flow using a public PKCE client. PocketBase keeps its access rules, hooks and MFA.

Tested with PocketBase `0.40.5` and Go `1.27.2`. One store and one auth collection per application.

## Set up the Shopify app

1. In the store's Shopify admin, open **Settings > Customer accounts** and make sure the store uses customer accounts with email-code sign-in. Note the store origin, such as `https://my-store.myshopify.com`, for `ShopURL`. [Customer accounts](https://help.shopify.com/en/manual/customers/customer-accounts/manage).
2. In the [Dev Dashboard](https://dev.shopify.com/dashboard), select **Apps > Create app > Start from Dev Dashboard** and name the app. Open **App settings** and copy the **Client ID** from the credentials card into `ClientID`. [Create an app](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard).
3. Before installing the app on the target store, choose how it will be distributed. This setting controls which stores can install the app. For an app dedicated to one merchant's store, use **Custom distribution**:

   - In the Dev Dashboard, open the app you just created and go to its **Home** page.
   - Find the **Distribution** card and click **Select distribution method**.
   - Select **Custom distribution**, then confirm. This lets you generate an installation link for the target store without publishing the app on the Shopify App Store.

   Shopify does not allow you to change this choice later. If you select **Public distribution**, Shopify approval is required; moving to custom distribution later means creating a new app and replacing `ClientID` in your application.

   The **Distribution** card only appears for apps owned by a Shopify Partner organization, such as a developer or agency organization. If you created the app in the store's own organization, there is no distribution choice to make: use the **Installs** card at step 6 instead. See [Select a distribution method](https://shopify.dev/docs/apps/launch/distribution/select-distribution-method).

4. With [Shopify CLI](https://shopify.dev/docs/apps/build/cli-for-apps/migrate-from-dashboard), run `shopify app config link` in a folder dedicated to the Shopify configuration and select this app. In the generated `shopify.app.toml`, set `application_url` to your frontend's HTTPS URL, set `embedded = false` and update these sections:

   ```toml
   [access_scopes]
   scopes = "customer_read_customers"
   
   [customer_authentication]
   redirect_uris = ["https://my-frontend.example/redirect.html"]
   ```

   The callback must match `RedirectURL` exactly. Configure it under `customer_authentication`, separately from the app installation OAuth redirects. [Shopify configuration](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication).
5. Ensure the app can access [protected customer data](https://shopify.dev/docs/apps/launch/protected-customer-data), including **Email**. For public apps, open your app in the Partner Dashboard under **Apps > your app > API access requests**. Under **Protected customer data access**, select **Request access** and declare the fields you need.
6. Publish the configuration with `shopify app deploy`, then install the app:

   - For **Custom distribution**, open the app's **Home** page in the Dev Dashboard, then select **Manage custom install link** on the **Distribution** card. Enter the target store's domain, generate the link and have the store owner open it to approve installation.
   - For an app created in the store's own organization, select **Install app** on the **Installs** card, choose the target store and select **Install** to approve its permissions.

This flow uses the app's Client ID with PKCE. No client secret or Admin API token is needed. [Customer Account API authentication](https://shopify.dev/docs/api/customer/2026-10#authentication).

## Installation

```sh
go get github.com/Sov3rain/shopify-pocketbase-sso@<version-or-commit>
```

To use a local checkout, add a `replace` in the host project:

```sh
go mod edit -replace=github.com/Sov3rain/shopify-pocketbase-sso=/path/to/shopify-pocketbase-sso
go get github.com/Sov3rain/shopify-pocketbase-sso
```

## Integration

### 1. Configure the environment

Set these environment variables in the shell or deployment platform that starts your PocketBase application. Replace the example values with your store, registered callback URL and app Client ID:

```dotenv
SHOPIFY_SHOP_URL=https://my-store.myshopify.com
SHOPIFY_REDIRECT_URL=https://my-frontend.example/redirect.html
SHOPIFY_CLIENT_ID=my-public-client
```

`os.Getenv` reads the process environment. If you use a `.env` file, configure your application runner to load it; Go does not load it automatically. No Shopify client secret is needed.

### 2. Register the service

Initialize the service in `OnServe`, after your migrations have created the auth collection. The application reads the environment variables and passes their values to the library:

```go
package main

import (
    "context"
    "log"
    "os"

    shopifysso "github.com/Sov3rain/shopify-pocketbase-sso"
    "github.com/pocketbase/pocketbase"
    "github.com/pocketbase/pocketbase/core"
)

func main() {
    app := pocketbase.New()
    app.OnServe().BindFunc(func(e *core.ServeEvent) error {
        sso, err := shopifysso.New(context.Background(), app, shopifysso.Config{
            ShopURL:        os.Getenv("SHOPIFY_SHOP_URL"),
            RedirectURL:    os.Getenv("SHOPIFY_REDIRECT_URL"),
            ClientID:       os.Getenv("SHOPIFY_CLIENT_ID"),
            AuthCollection: "users",
        })
        if err != nil {
            return err
        }
        sso.Register(e.Router)
        return e.Next()
    })
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

| Parameter | Expected value |
| --- | --- |
| `ShopURL` | Store HTTPS origin, without a path or trailing slash |
| `RedirectURL` | Frontend callback URL registered with Shopify |
| `ClientID` | Public Customer Account API client ID |
| `AuthCollection` | Existing auth collection name or ID, defaults to `users` |
| `AllowLoopbackHTTP` | Allows loopback HTTP for tests, disabled by default |

### 3. Check account rules and startup behavior

Create the auth collection before initializing the service. The example uses `users`; change `AuthCollection` if your collection has another name. Your application owns its fields and access rules. In particular, its create rule controls whether a first Shopify login can create an account.

The library enables the `shopify` provider and creates a private `_shopifyIdentities` collection. Accounts are linked by Shopify identity. An existing account with the same email is not merged automatically; the login returns HTTP 409. Shopify tokens are neither stored nor exposed to the frontend.

Shopify endpoints are discovered in the background. If Shopify is unavailable at startup, PocketBase still starts and retries discovery automatically. Until discovery succeeds, Shopify is absent from `listAuthMethods()` and direct Shopify login requests return HTTP 503 with `Retry-After`.

Use a context that lasts as long as the service, such as `context.Background()` in the example. A short startup timeout would also stop discovery retries.

For transaction guarantees, MFA, unlinking, email verification and custom hooks or middleware, see the [integration reference](docs/integration.md).

## Minimal frontend example

Following PocketBase's [manual OAuth2 code exchange example](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), use two pages: one to start login and one to handle the callback. This Shopify provider uses full-page redirects with PKCE; PocketBase's `authWithOAuth2` popup flow is not supported.

Save these files in your application's `pb_public` directory. PocketBase serves them at `/index.html` and `/redirect.html`. Serve the application over HTTPS and set `SHOPIFY_REDIRECT_URL` to that origin followed by `/redirect.html`. Register the exact same URL in Shopify.

Both pages use the current origin for PocketBase and the callback. If your API is hosted elsewhere, inject the API and callback URLs through your frontend's build or runtime configuration. Browser JavaScript cannot read the Go process's environment variables.

The examples use the `users` collection and a pinned version of the official PocketBase SDK. Replace `users` in both files if your auth collection has another name.

### `pb_public/index.html`

The login buttons start either interactive login or a silent attempt using an existing Shopify session. Errors appear on the page without exposing codes or tokens.

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="referrer" content="no-referrer">
  <title>Shopify sign-in</title>
</head>
<body>
  <h1>Sign in</h1>
  <button id="login" type="button">Sign in with Shopify</button>
  <button id="silent" type="button">Use existing Shopify session</button>
  <button id="logout" type="button">Sign out of this application</button>
  <p id="status" role="status"></p>

  <script type="module">
    import PocketBase from 'https://cdn.jsdelivr.net/npm/pocketbase@0.28.1/dist/pocketbase.es.mjs';

    const pb = new PocketBase(location.origin);
    const redirectURL = new URL('/redirect.html', location.origin).href;
    const status = document.getElementById('status');
    status.textContent = pb.authStore.isValid ? 'You are signed in.' : 'You are signed out.';

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

    async function startLogin(silent) {
      try {
        await loginWithShopify(silent);
      } catch {
        status.textContent = 'Unable to start Shopify sign-in. Please try again.';
      }
    }

    document.getElementById('login').addEventListener('click', () => startLogin(false));
    document.getElementById('silent').addEventListener('click', () => startLogin(true));
    document.getElementById('logout').addEventListener('click', () => {
      pb.authStore.clear();
      status.textContent = 'You are signed out of this application.';
    });
  </script>
</body>
</html>
```

### `pb_public/redirect.html`

This page checks the saved state and PKCE data, then exchanges the code through PocketBase. If Shopify returns `login_required`, the user can return to the login page and choose interactive sign-in. It does not retry silently in a loop.

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="referrer" content="no-referrer">
  <title>Shopify sign-in result</title>
</head>
<body>
  <h1>Shopify sign-in</h1>
  <p id="status" role="status">Completing sign-in...</p>
  <a href="/index.html">Return to the sign-in page</a>

  <script type="module">
    import PocketBase from 'https://cdn.jsdelivr.net/npm/pocketbase@0.28.1/dist/pocketbase.es.mjs';

    const pb = new PocketBase(location.origin);
    const redirectURL = new URL('/redirect.html', location.origin).href;
    const status = document.getElementById('status');

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

    try {
      const result = await completeShopifyLogin();
      if (result.error === 'login_required') {
        status.textContent = 'No active Shopify session. Return to the sign-in page and choose "Sign in with Shopify".';
      } else if (result.error) {
        status.textContent = 'Shopify did not authorize sign-in. Return to the sign-in page to try again.';
      } else {
        status.textContent = 'You are signed in. You can return to the application.';
      }
    } catch (error) {
      status.textContent = error.response?.mfaId
        ? 'A second authentication factor is required. This minimal example does not implement that step.'
        : 'Sign-in failed or the callback expired. Return to the sign-in page to try again.';
    }
  </script>
</body>
</html>
```

Configure your server or reverse proxy to send `Cache-Control: no-store` and `Referrer-Policy: no-referrer` for the callback page. Protect the pages from script injection and do not log codes or tokens. If the API is on another origin, configure CORS; the backend expects the request's `Origin` to match the callback origin.

The SDK persists the PocketBase session in `pb.authStore`. Signing out clears that application session and leaves the Shopify session active. If MFA is enabled, extend the callback page with a second-factor form using PocketBase's [MFA flow](https://pocketbase.io/docs/authentication/#multi-factor-authentication).

## Tests

`mise run check` runs the Go tests, the official SDK OAuth test, Go static analysis and compilation. Mise and Node are development tools; only Go is needed to use the library. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
