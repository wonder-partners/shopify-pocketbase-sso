# Shopify SSO for PocketBase

The Go package `shopifysso` adds Shopify Customer Account authentication to PocketBase's native OAuth2 flow using a public PKCE client. PocketBase keeps its access rules, hooks and MFA.

Tested with PocketBase `0.40.5` and Go `1.27.2`. One store and one auth collection per application.

## Integration

Follow these steps in order: start PocketBase, prepare the frontend and its HTTPS URLs, configure Shopify, then enable SSO.

### 1. Create the PocketBase application

Use Go `1.27.2` or later. This library runs inside a Go application that imports PocketBase. If you already have one, keep your existing project and make sure it has an auth collection before continuing.

For a new application, create a separate project directory:

```sh
mkdir my-app
cd my-app
go mod init example.com/my-app
```

Create `main.go` with a basic PocketBase server and a route for frontend files:

```go
package main

import (
    "log"
    "os"

    "github.com/pocketbase/pocketbase"
    "github.com/pocketbase/pocketbase/apis"
    "github.com/pocketbase/pocketbase/core"
)

func main() {
    app := pocketbase.New()
    app.OnServe().BindFunc(func(e *core.ServeEvent) error {
        e.Router.GET("/{path...}", apis.Static(os.DirFS("./pb_public"), false))
        return e.Next()
    })
    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

Install the tested PocketBase version and start the server:

```sh
go get github.com/pocketbase/pocketbase@v0.40.5
go mod tidy
go run . serve
```

Open the local dashboard at `http://127.0.0.1:8090/_/` and follow PocketBase's initial superuser setup. Create an auth collection named `users` if it does not already exist. Your application owns its fields and access rules. Its create rule controls whether a first Shopify login can create an account; a locked create rule prevents sign-up. Choose rules that match your application's account policy. See [PocketBase API rules](https://pocketbase.io/docs/api-rules-and-filters/).

The examples use `users` throughout. If your auth collection has a different name, update `AuthCollection` in the Go configuration and the collection name in both frontend pages.

Keep this basic server running while you prepare the frontend. It does not need Shopify credentials yet. For more about using PocketBase as a Go package, see the [PocketBase Go guide](https://pocketbase.io/docs/go-overview/).

### 2. Install the SSO library

Run these commands from your PocketBase application directory, in a second terminal if the server is still running.

For a private repository, configure Git with read access to the repository and mark the module as private before downloading it:

```sh
go env -w GOPRIVATE=github.com/wonder-partners/shopify-pocketbase-sso
```

If `GOPRIVATE` already contains other entries, append this module path to the existing comma-separated list. Configure access on each machine that builds the application, including CI.

```sh
go get github.com/wonder-partners/shopify-pocketbase-sso@<version-or-commit>
```

To use a local checkout, add a `replace` in the host project:

```sh
go mod edit -replace=github.com/wonder-partners/shopify-pocketbase-sso=/path/to/shopify-pocketbase-sso
go get github.com/wonder-partners/shopify-pocketbase-sso
```

### 3. Prepare the frontend and HTTPS URLs

Create a `pb_public` directory in your PocketBase project and save the two pages below in it. The static route from step 1 serves these files.

Expose your running application through an HTTPS deployment or a public HTTPS tunnel to `http://127.0.0.1:8090`. You need these addresses before configuring Shopify:

- Frontend URL, for example `https://my-frontend.example`, for Shopify's **App URL**.
- Callback URL, for example `https://my-frontend.example/redirect.html`, for Shopify customer authentication and `SHOPIFY_REDIRECT_URL`.

Replace the example domain with your deployment or tunnel domain. Use the same origin for the frontend and PocketBase API when following these examples. Check that the server serves both pages at the chosen HTTPS addresses. Shopify sign-in will work only after the remaining steps are complete.

#### Minimal frontend example

These two pages follow PocketBase's [manual OAuth2 code exchange example](https://pocketbase.io/docs/authentication/#authenticate-with-oauth2), with an automatic silent login attempt for Shopify. The `authWithOAuth2` popup flow is not supported by this provider.

Save both files in `pb_public`. The examples use the current origin for PocketBase and the `users` auth collection. They will be ready to use once you finish the Shopify configuration and register the SSO service in the following steps.

##### `pb_public/index.html`

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

##### `pb_public/redirect.html`

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

### 4. Create and install the Shopify app

With your frontend and callback URLs ready, follow the [Shopify app setup guide](docs/shopify-setup.md) to create, configure and install the app on your store. Use the URLs from step 3 and keep **Embed app in Shopify admin** unchecked.

When you finish that guide, you will have the store origin, the registered customer callback URL and the app's Client ID. Return here to configure PocketBase.

### 5. Configure the environment

Set these environment variables in the shell or deployment platform that starts your PocketBase application. Replace the example values with your store, registered callback URL and app Client ID:

```dotenv
SHOPIFY_SHOP_URL=https://my-store.myshopify.com
SHOPIFY_REDIRECT_URL=https://my-frontend.example/redirect.html
SHOPIFY_CLIENT_ID=my-public-client
```

`os.Getenv` reads the process environment. If you use a `.env` file, configure your application runner to load it; Go does not load it automatically. No Shopify client secret is needed.

### 6. Register the service

Replace the initial `main.go` with this version, keeping the static file route and adding the SSO service in `OnServe`. The `users` auth collection from step 1 must already exist. If you manage collections with migrations, run those migrations before initializing the service. The application reads the environment variables and passes their values to the library:

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
            ShopURL:        os.Getenv("SHOPIFY_SHOP_URL"),
            RedirectURL:    os.Getenv("SHOPIFY_REDIRECT_URL"),
            ClientID:       os.Getenv("SHOPIFY_CLIENT_ID"),
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

### 7. Restart PocketBase and test sign-in

Stop the basic server from step 1. Start the updated application with the environment variables from step 5 loaded:

```sh
go mod tidy
go run . serve
```

Keep the HTTPS deployment or tunnel pointing to this server. Open `https://my-frontend.example/index.html` using your actual frontend domain, then sign in with a customer account from the configured Shopify store.

If you change your frontend or tunnel domain later, update Shopify's **App URL**, `customer_authentication.redirect_uris`, `SHOPIFY_REDIRECT_URL` and any frontend configuration together, then deploy the Shopify configuration and restart PocketBase.

## Account rules and startup behavior

The library enables the `shopify` provider and creates a private `_shopifyIdentities` collection. Accounts are linked by Shopify identity. An existing account with the same email is not merged automatically; the login returns HTTP 409. Shopify tokens are neither stored nor exposed to the frontend.

Shopify endpoints are discovered in the background. If Shopify is unavailable at startup, PocketBase still starts and retries discovery automatically. Until discovery succeeds, Shopify is absent from `listAuthMethods()` and direct Shopify login requests return HTTP 503 with `Retry-After`.

For transaction guarantees, MFA, unlinking, email verification and custom hooks or middleware, see the [integration reference](docs/integration.md).

## Tests

`mise run check` runs the Go tests, the official SDK OAuth test, Go static analysis and compilation. Mise and Node are development tools; only Go is needed to use the library. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
