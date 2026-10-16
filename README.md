# Shopify SSO for PocketBase

The Go package `shopifysso` adds Shopify Customer Account authentication to PocketBase's native OAuth2 flow using a public PKCE client. PocketBase keeps its access rules, hooks and MFA.

Tested with PocketBase `0.40.5` and Go `1.27.2`. One store and one auth collection per application.

## Set up the Shopify app

1. In the store's Shopify admin, open **Settings > Customer accounts** and make sure the store uses customer accounts with email-code sign-in. Note the store origin, such as `https://my-store.myshopify.com`, for `ShopURL`. [Customer accounts](https://help.shopify.com/en/manual/customers/customer-accounts/manage).
2. In the [Dev Dashboard](https://dev.shopify.com/dashboard), select **Apps > Create app > Start from Dev Dashboard** and name the app. Open **App settings** and copy the **Client ID** from the credentials card into `ClientID`. [Create an app](https://shopify.dev/docs/apps/build/dev-dashboard/create-apps-using-dev-dashboard).
3. With [Shopify CLI](https://shopify.dev/docs/apps/build/cli-for-apps/migrate-from-dashboard), run `shopify app config link` in a folder dedicated to the Shopify configuration and select this app. In the generated `shopify.app.toml`, set `application_url` to your frontend's HTTPS URL, set `embedded = false` and update these sections:

   ```toml
   [access_scopes]
   scopes = "customer_read_customers"

   [customer_authentication]
   redirect_uris = ["https://my-frontend.example/redirect.html"]
   ```

   The callback must match `RedirectURL` exactly. Configure it under `customer_authentication`, separately from the app installation OAuth redirects. [Shopify configuration](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication).
4. Ensure the app can access [protected customer data](https://shopify.dev/docs/apps/launch/protected-customer-data), including **Email**. For public apps, select a distribution method, then open your app in the Partner Dashboard under **Apps > your app > API access requests**. Under **Protected customer data access**, select **Request access** and declare the fields you need.
5. Publish the configuration with `shopify app deploy`. In the Dev Dashboard, select **Install app** on the **Installs** card, choose the target store and select **Install** to approve its permissions.

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

Initialize the service in `OnServe`, after the project's migrations:

```go
package main

import (
    "context"
    "log"

    shopifysso "github.com/Sov3rain/shopify-pocketbase-sso"
    "github.com/pocketbase/pocketbase"
    "github.com/pocketbase/pocketbase/core"
)

func main() {
    app := pocketbase.New()
    app.OnServe().BindFunc(func(e *core.ServeEvent) error {
        sso, err := shopifysso.New(context.Background(), app, shopifysso.Config{
            ShopURL:        "https://my-store.myshopify.com",
            RedirectURL:    "https://my-frontend.example/redirect.html",
            ClientID:       "my-public-client",
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

The host project owns its fields and rules. The library enables the `shopify` provider and creates the private `_shopifyIdentities` collection. Provider configuration reads and updates the auth collection in one transaction to preserve concurrent host changes. The auth collection's create rule determines whether a first login can create an account.

`New` validates local configuration and database setup synchronously, then discovers Shopify's endpoints in the background. A discovery outage does not prevent PocketBase from starting. Until discovery succeeds, `listAuthMethods()` omits Shopify while preserving other auth methods, and explicit Shopify login requests return HTTP 503 with `Retry-After`.

Each discovery attempt has a 20-second deadline, with retry delays increasing from 1 second to a maximum of 60 seconds. The provider becomes available automatically after discovery and provider configuration succeed. The context passed to `New` controls the service lifetime; cancellation or PocketBase termination stops pending requests and retries. Use a lifetime context, such as `context.Background()` in the example, rather than a short startup timeout.

Accounts are linked by Shopify identity. Matching email addresses do not merge accounts; a conflict returns HTTP 409. Shopify tokens are neither stored nor exposed to the frontend.

Deleting a Shopify link, through the records API or a native security purge, also removes its matching private identity in the same transaction. The user account remains available through its other authentication methods. A subsequent Shopify login cannot recover that account through the deleted identity; an email conflict still returns HTTP 409.

When a previously unverified email becomes verified, PocketBase revokes the old OAuth links and refresh tokens. Only an accepted Shopify authentication can restore its current Shopify link and private identity within that same transaction. Native email verification or account recovery leaves the old Shopify identity revoked.

Profile synchronization and identity links are committed together after the OAuth hook chain accepts the request. Before commit, an authentication rule or request hook rejection rolls back these database changes. A hook that stops the chain without writing an HTTP response also causes a rollback and returns HTTP 500. A native MFA challenge accepts the Shopify factor and commits the profile and challenge while the second factor is still pending. Hooks must use their event's `e.App` for database operations to participate in the transaction; external side effects cannot be rolled back.

For Shopify logins, native new-device email alerts and their mail hooks run asynchronously after a successful commit. Slow mail delivery holds neither the database transaction nor the login response. A rollback discards the alert; delivery failures are logged without rejecting the login. Other auth methods keep PocketBase's normal alert behavior.

## Minimal frontend example

Use the official PocketBase SDK. Replace `users` with your chosen collection and serve the callback at the URL configured in both `RedirectURL` and Shopify.

```javascript
import PocketBase from 'pocketbase';

const pb = new PocketBase(window.location.origin);
const redirectURL = new URL('/redirect.html', window.location.origin).href;
```

<!-- native-redirect-example:start -->
```javascript
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
```
<!-- native-redirect-example:end -->

Call `loginWithShopify()` on a button click, then `completeShopifyLogin()` on the callback page. The SDK updates `pb.authStore`. To attempt silent sign-in, call `loginWithShopify(true)` once; if it fails, offer interactive sign-in without automatically retrying.

Serve the callback with `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. Protect PKCE storage from script injection and do not log codes or tokens. The `Origin` header must match the origin of `RedirectURL`; configure CORS if the frontend and API use different origins.

The UI must handle errors and MFA challenges. `pb.authStore.clear()` signs out of the application without signing out of Shopify. The `authWithOAuth2` popup flow is not supported.

The SDK test executes these functions directly.

## Tests

`mise run check` runs the Go tests, the official SDK OAuth test, Go static analysis and compilation. Mise and Node are development tools; only Go is needed to use the library. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
