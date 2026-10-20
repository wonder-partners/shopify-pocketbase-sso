# Shopify SSO for PocketBase

The Go package `shopifysso` adds Shopify Customer Account authentication to PocketBase's native OAuth2 flow using a public PKCE client. PocketBase keeps its access rules, hooks and MFA.

Tested with PocketBase `0.40.5` and Go `1.27.2`. One store and one auth collection per application.

## Integration

This guide integrates Shopify SSO into an existing PocketBase Go application. It assumes an auth collection already exists and that you can serve your frontend over HTTPS.

The examples use the `users` auth collection. If yours has a different name, update `AuthCollection` in the Go configuration and the collection name in both frontend pages. Your application owns its fields and access rules; a locked create rule prevents new accounts from signing up through Shopify.

### 1. Install the SSO library

Like PocketBase itself, this library is a regular Go module. Add its import to your application using the integration example in step 5:

```go
import shopifysso "github.com/wonder-partners/shopify-pocketbase-sso"
```

After adding the integration code, run this command from the directory containing your application's `go.mod`:

```sh
go mod tidy
```

Go resolves the imports, downloads missing dependencies from their remote repositories, and updates `go.mod` and `go.sum` automatically. You do not need to edit these files manually. If your Go module is in a subdirectory, run the command there or use `go -C apps/api mod tidy`, replacing `apps/api` with your module directory.

For a private repository, configure Git with read access to the repository and mark the module as private before downloading it:

```sh
go env -w GOPRIVATE=github.com/wonder-partners/shopify-pocketbase-sso
```

If `GOPRIVATE` already contains other entries, append this module path to the existing comma-separated list. Configure access on each machine that builds the application, including CI.

To select a specific version or commit instead of letting `go mod tidy` resolve the missing dependency, use:

```sh
go get github.com/wonder-partners/shopify-pocketbase-sso@<version-or-commit>
```

For local library development, you can optionally use a local checkout by adding a `replace` in the host project. This is unnecessary when using the remote repository:

```sh
go mod edit -replace=github.com/wonder-partners/shopify-pocketbase-sso=/path/to/shopify-pocketbase-sso
go mod tidy
```

### 2. Prepare the frontend and HTTPS URLs

Choose the sign-in page and callback route in your existing frontend. You can use the [minimal frontend example](#minimal-frontend-example) below, served from `pb_public` by the Go integration example.

Use your existing HTTPS deployment or a public HTTPS tunnel to your local application. Have these addresses ready before configuring Shopify:

- Frontend URL, for example `https://my-frontend.example`, for Shopify's **App URL**.
- Callback URL, for example `https://my-frontend.example/redirect.html`, for Shopify customer authentication and `SHOPIFY_CUSTOMER_CALLBACK_URL`.

Replace the example domain with your deployment or tunnel domain. Use the same origin for the frontend and PocketBase API when following these examples. Make sure your frontend serves the sign-in page and callback at the chosen addresses. Shopify sign-in will work only after the remaining steps are complete.

### 3. Create and install the Shopify app

With your frontend and callback URLs ready, follow the [Shopify app setup guide](docs/shopify-setup.md) to create, configure and install the app on your store. Use the URLs from step 2 and keep **Embed app in Shopify admin** unchecked.

When you finish that guide, you will have the store origin, the registered customer callback URL and the app's Client ID. Return here to configure PocketBase.

### 4. Configure the environment

Set these environment variables in the shell or deployment platform that starts your PocketBase application. Replace the example values with your store, registered callback URL and app Client ID:

```dotenv
SHOPIFY_STORE_URL=https://my-store.myshopify.com
SHOPIFY_CUSTOMER_CALLBACK_URL=https://my-frontend.example/redirect.html
SHOPIFY_APP_CLIENT_ID=my-public-client
```

- `SHOPIFY_STORE_URL`: find your store's `*.myshopify.com` domain in Shopify admin under [**Settings > Domains**](https://help.shopify.com/en/manual/domains/domain-type/change-primary-domain). Add `https://`, with no path or trailing slash.
- `SHOPIFY_CUSTOMER_CALLBACK_URL`: use the frontend callback URL chosen in step 2 and registered in your Shopify app's `shopify.app.toml` under `customer_authentication.redirect_uris`. For the frontend example below, this is your frontend's HTTPS origin followed by `/redirect.html`. Copy the registered URL exactly.
- `SHOPIFY_APP_CLIENT_ID`: in the [Dev Dashboard](https://dev.shopify.com/dashboard), open your app, then **App settings > Credentials**, and copy **Client ID**. See [Shopify app credentials](https://shopify.dev/docs/apps/build/authentication-authorization/manage-credentials).

These variable names are conventions for your host application. The example reads them with `os.Getenv` and passes their values to `shopifysso.Config`; the library does not read environment variables itself. If you use a `.env` file, configure your application runner to load it; Go does not load it automatically. No Shopify client secret is needed.

### 5. Register the service

Add the SSO service to your application's `OnServe` hook after your migrations have created the auth collection. This `main.go` shows the integration with the `pb_public` frontend example. If your application already serves the frontend, keep its existing routes. The application reads the environment variables and passes their values to the library:

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

| Parameter | Expected value |
| --- | --- |
| `ShopURL` | Store HTTPS origin, without a path or trailing slash |
| `RedirectURL` | Frontend callback URL registered with Shopify |
| `ClientID` | Public Customer Account API client ID |
| `AuthCollection` | Existing auth collection name or ID, defaults to `users` |
| `AllowLoopbackHTTP` | Allows loopback HTTP for tests, disabled by default |

Use a context that lasts as long as the service, such as `context.Background()` in the example. A short startup timeout would also stop discovery retries.

### 6. Restart PocketBase and test sign-in

Restart your application with the environment variables from step 4 loaded:

```sh
go mod tidy
go run . serve
```

Keep the HTTPS deployment or tunnel pointing to this server. Open your frontend's sign-in page, such as `https://my-frontend.example/index.html` for the example below, then sign in with a customer account from the configured Shopify store.

If you change your frontend or tunnel domain later, update Shopify's **App URL**, `customer_authentication.redirect_uris`, `SHOPIFY_CUSTOMER_CALLBACK_URL` and any frontend configuration together, then deploy the Shopify configuration and restart PocketBase.

## Minimal frontend example

These two pages follow PocketBase's [manual OAuth2 code exchange example](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), with an automatic silent login attempt for Shopify. The `authWithOAuth2` popup flow is not supported by this provider.

Save both files in `pb_public`. The examples use the current origin for PocketBase and the `users` auth collection. The static route in the Go integration example serves these files.

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

## Account rules and startup behavior

The library enables the `shopify` provider and creates a private `_shopifyIdentities` collection. Accounts are linked by Shopify identity. An existing account with the same email is not merged automatically; the login returns HTTP 409. Shopify tokens are neither stored nor exposed to the frontend.

Shopify endpoints are discovered in the background. If Shopify is unavailable at startup, PocketBase still starts and retries discovery automatically. Until discovery succeeds, Shopify is absent from `listAuthMethods()` and direct Shopify login requests return HTTP 503 with `Retry-After`.

For transaction guarantees, MFA, unlinking, email verification and custom hooks or middleware, see the [integration reference](docs/integration.md).

## Tests

`mise run check` runs the Go tests, the official SDK OAuth test, Go static analysis and compilation. Mise and Node are development tools; only Go is needed to use the library. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
