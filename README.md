# Shopify SSO for PocketBase

The Go package `shopifysso` adds Shopify Customer Account authentication to PocketBase's native OAuth2 flow using a public PKCE client. PocketBase keeps its access rules, hooks and MFA.

Tested with PocketBase `0.40.5` and Go `1.27.2`. One store and one auth collection per application.

## Set up the Shopify app

1. In the store's Shopify admin, open **Settings > Customer accounts** and make sure the store uses customer accounts with email-code sign-in. Note the store origin, such as `https://my-store.myshopify.com`, for `ShopURL`. [Customer accounts](https://help.shopify.com/en/manual/customers/customer-accounts/manage).
2. In the [Dev Dashboard](https://dev.shopify.com/dashboard), select **Apps > Create app > Start from Dev Dashboard** and fill in the creation form:

   - **App name**: choose a name, such as `PocketBase SSO`.
   - **Version name**: leave empty or use a label such as `v1`.
   - **App URL**: enter your frontend's HTTPS URL, such as `https://my-frontend.example`. If you do not have one yet, use `https://example.com` temporarily and replace it before testing sign-in. A public HTTPS tunnel to your local frontend works for development.
   - **Embed app in Shopify admin**: uncheck this and keep it unchecked after configuring the real URLs. The frontend runs as a standalone site; this SSO flow does not need to render inside Shopify admin.
   - **Allowed redirection URL(s)**: leave empty for this integration. This field configures app installation OAuth redirects; the customer sign-in callback is configured separately in step 4.
   - **Use legacy install flow**: leave unchecked.
   - **Scopes**: enter `customer_read_customers` to read the signed-in customer's identity and email address.
   - **Webhooks API version**: keep the default. This package does not subscribe to Shopify webhooks.

   Click **Create app**, then open **App settings** and copy the **Client ID** from the credentials card into `ClientID`. [Create an app](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard).
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

   Replace the example URLs with your actual frontend and callback URLs before testing sign-in. The callback must match `RedirectURL` exactly, including the value supplied through `SHOPIFY_REDIRECT_URL` in the integration example below. Adding it to the dashboard's **Allowed redirection URL(s)** field does not register it for customer sign-in; it must be listed under `customer_authentication.redirect_uris`. Keep `embedded = false` when updating the URLs. [Shopify configuration](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication).
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

These two pages follow PocketBase's [manual OAuth2 code exchange example](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), with an automatic silent login attempt for Shopify. The `authWithOAuth2` popup flow is not supported by this provider.

Save both files in `pb_public` and serve the application over HTTPS. Set `SHOPIFY_REDIRECT_URL` to `https://your-domain/redirect.html` and register that exact URL in Shopify. The examples use the current origin for PocketBase and the `users` auth collection.

### `pb_public/index.html`

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

### `pb_public/redirect.html`

Validate the callback, then exchange the code through PocketBase. Returning to the first page skips the silent attempt once, so `login_required` displays the sign-in link instead of causing a redirect loop.

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

The SDK stores the PocketBase session in `pb.authStore`. Add your application's error handling and, if enabled, the [MFA second-factor step](https://pocketbase.io/docs/authentication/#multi-factor-authentication). These examples leave errors to the browser rather than implementing an error UI.

Serve the callback with `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. Protect the pages from script injection and do not log codes or tokens. If the frontend and API use different origins, inject their URLs through your frontend configuration and enable CORS. The backend expects the request's `Origin` to match the callback origin; browser JavaScript cannot read Go environment variables directly.

## Tests

`mise run check` runs the Go tests, the official SDK OAuth test, Go static analysis and compilation. Mise and Node are development tools; only Go is needed to use the library. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
