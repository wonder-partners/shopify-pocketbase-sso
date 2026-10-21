# Shopify SSO for PocketBase

Let customers sign in to your PocketBase application with their Shopify account. PocketBase keeps its access rules, hooks and MFA. One store and one auth collection are supported per application.

## Before you start

You need:

- An existing PocketBase Go application with an auth collection. This guide uses `users`.
- Access to your Shopify store's admin and the [Dev Dashboard](https://dev.shopify.com/dashboard) to create and install an app.
- [Shopify CLI](https://shopify.dev/docs/apps/build/cli-for-apps) installed.
- A public HTTPS address for your PocketBase application, through your hosting or a local tunnel.

The steps below add two sign-in pages to your PocketBase server. Everything runs on the same HTTPS address. Replace the example domains with yours throughout the guide.

Tested with PocketBase `0.40.5` and Go `1.27.2`.

## 1. Prepare your store and URLs

In Shopify admin, open **Settings > Customer accounts** and enable customer accounts with email-code sign-in.

Keep these values ready:

- Store URL: `https://my-store.myshopify.com`. Find the `*.myshopify.com` domain in **Settings > Domains**. Use no path or trailing slash.
- Application URL: `https://my-frontend.example`. Your HTTPS hosting or tunnel must forward to PocketBase.
- Callback URL: `https://my-frontend.example/redirect.html`. Shopify will return customers to this page after sign-in.

You will create the callback page in step 6.

## 2. Create the Shopify app

In the [Dev Dashboard](https://dev.shopify.com/dashboard), open **Apps > Create app > Start from Dev Dashboard**, name the app and create it.

In its version settings, use:

- **App URL**: your application URL from step 1.
- **Embed app in Shopify admin**: unchecked.
- **Scopes**: `customer_read_customers`.
- **Allowed redirection URL(s)**: leave empty. The customer callback is configured in the next step.
- **Use legacy install flow**: unchecked.

Leave the other settings at their defaults and release this initial version.

Open **App settings > Credentials** and save the **Client ID**. No client secret or Admin API token is needed.

## 3. Configure and install the Shopify app

Keep the Shopify app configuration in the same repository as the PocketBase application that uses this library, ideally in a dedicated subfolder. Commit the generated `shopify.app.toml` so that Shopify URLs and permissions are versioned alongside the application code. Its `client_id` is public and can be committed.

From the root of your PocketBase application repository, create that subfolder and link it to the Shopify app you just created:

```sh
mkdir shopify
cd shopify
shopify app config link
```

In the generated `shopify.app.toml`, update these values with your URLs. Keep the other generated settings, including `client_id`:

```toml
application_url = "https://my-frontend.example"
embedded = false

[access_scopes]
scopes = "customer_read_customers"

[customer_authentication]
redirect_uris = ["https://my-frontend.example/redirect.html"]
```

Edit existing entries rather than adding duplicate sections. The callback belongs in [`customer_authentication.redirect_uris`](https://shopify.dev/docs/apps/build/cli-for-apps/app-configuration#customer_authentication), not in the dashboard's installation redirect field.

Publish the configuration from this folder:

```sh
shopify app deploy
```

Return to the app's **Home** page in the Dev Dashboard and install it on your store:

- If there is a **Distribution** card, select **Select distribution method > Custom distribution** and confirm. Then open **Manage custom install link**, enter the store domain, generate the link and have the store owner use it to install the app. The distribution choice is permanent.
- Otherwise, on the **Installs** card, select **Install app**, choose the store and approve installation.

This integration needs access to customer email addresses. [Shopify grants this access to custom apps](https://shopify.dev/docs/apps/launch/protected-customer-data).

## 4. Set the PocketBase environment variables

Return to your PocketBase application's folder, the one containing `go.mod`.

Set the following variables in the terminal you will use to start PocketBase. Replace the store URL, callback URL and Client ID with the values from steps 1 to 3.

With Bash:

```sh
export SHOPIFY_STORE_URL="https://my-store.myshopify.com"
export SHOPIFY_CUSTOMER_CALLBACK_URL="https://my-frontend.example/redirect.html"
export SHOPIFY_APP_CLIENT_ID="your-client-id"
```

With PowerShell:

```powershell
$env:SHOPIFY_STORE_URL = "https://my-store.myshopify.com"
$env:SHOPIFY_CUSTOMER_CALLBACK_URL = "https://my-frontend.example/redirect.html"
$env:SHOPIFY_APP_CLIENT_ID = "your-client-id"
```

The callback must match the URL deployed in step 3 exactly. For a hosted deployment, set these same variables in your hosting platform. A `.env` file alone is not enough; Go does not load it automatically.

## 5. Add the service to PocketBase

From the folder containing `go.mod`, install the library.

If the repository is private, first configure Git with read access and run `go env -w GOPRIVATE=github.com/wonder-partners/shopify-pocketbase-sso`. If `GOPRIVATE` already contains entries, append this path to its comma-separated list instead.

```sh
go get github.com/wonder-partners/shopify-pocketbase-sso
```

Add the service to your application's `OnServe` hook. This complete `main.go` also serves the pages you will create next:

```go
package main

import (
    "context"
    "log"
    "os"

    shopifysso "github.com/wonder-partners/shopify-pocketbase-sso"
    "github.com/pocketbase/pocketbase"
    "github.com/pocketbase/pocketbase/apis"
    "github.com/pocketbase/pocketbase/core"
)

func main() {
    app := pocketbase.New()
    app.OnServe().BindFunc(func(e *core.ServeEvent) error {
        sso, err := shopifysso.New(context.Background(), app, shopifysso.Config{
            ShopURL:        os.Getenv("SHOPIFY_STORE_URL"),
            RedirectURL:    os.Getenv("SHOPIFY_CUSTOMER_CALLBACK_URL"),
            ClientID:       os.Getenv("SHOPIFY_APP_CLIENT_ID"),
            AuthCollection: "users",
        })
        if err != nil {
            return err
        }
        sso.Register(e.Router)
        e.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))
        return e.Next()
    })
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

The `users` auth collection must exist before this hook runs. If yours has another name, change `AuthCollection` above and `users` in both HTML files below. To allow new customers to sign up, its create rule must allow account creation.

If you already have an `OnServe` hook or frontend routes, merge these additions into your existing setup. Keep a context that lasts for the service's lifetime, as `context.Background()` does here.

## 6. Add the sign-in pages

Create a `pb_public` folder beside `go.mod`, then save the following two files inside it.

The first page tries to sign in with an existing Shopify session. If the customer needs to sign in, it shows a link. The callback page checks Shopify's response and exchanges the code for a PocketBase session.

### `pb_public/index.html`

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

This flow uses `authWithOAuth2Code`. The `authWithOAuth2` popup flow is not supported. For an application with MFA enabled, also handle PocketBase's second-factor challenge.

## 7. Start and test

In your PocketBase application's folder, using the same terminal as step 4, run:

```sh
go mod tidy
go run . serve
```

Keep your hosting or HTTPS tunnel pointing to this server. Open `https://my-frontend.example/index.html` and sign in with a customer account from your Shopify store.

After sign-in, the browser returns to `index.html` and the PocketBase SDK stores the session in `pb.authStore`. You can check it in the browser's local storage under `pocketbase_auth`.

If sign-in fails:

- Shopify is unavailable: check the PocketBase logs. The server retries connecting automatically; Shopify stays hidden from `listAuthMethods()` until ready, and direct login requests return HTTP 503.
- HTTP 409: a PocketBase account already uses that email. Accounts are linked by Shopify identity and are not merged by email.
- Invalid callback: check that the Shopify callback, `SHOPIFY_CUSTOMER_CALLBACK_URL` and the page's HTTPS URL match exactly.

Before using these pages in production, add visible error handling. Serve the callback with `Cache-Control: no-store` and `Referrer-Policy: no-referrer`, protect the pages from script injection and do not log codes or tokens. Shopify tokens are neither stored nor sent to the frontend.

If the HTTPS domain changes, update `application_url` and `customer_authentication.redirect_uris` in `shopify.app.toml`, run `shopify app deploy`, update `SHOPIFY_CUSTOMER_CALLBACK_URL` and restart PocketBase.

## Reference and development

The setup above is complete. The [integration reference](docs/integration.md) covers optional configuration, custom hooks, middleware and account management.

To work on this library, run `mise run check`. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
