# Shopify SSO for PocketBase

Let customers sign in to PocketBase with their Shopify account. The Go package `shopifysso` uses PocketBase's OAuth2 flow and keeps your access rules, hooks and MFA.

Tested with PocketBase `0.40.5` and Go `1.27.2`. Supports one Shopify store and one auth collection per application. No Shopify client secret is needed.

## Integration

You need an existing PocketBase Go application and an auth collection. The examples use `users`. If yours has another name, change it in the Go and frontend examples. Its create rule must allow account creation for new customers to sign up.

### 1. Install the SSO library

Run this from your application's folder containing `go.mod`:

```sh
go get github.com/wonder-partners/shopify-pocketbase-sso
```

For private repository access, a specific version or a local checkout, see [installation options](docs/integration.md#installation-options).

### 2. Prepare the frontend and HTTPS URLs

Serve your frontend and PocketBase API on the same HTTPS origin, using your deployment or a public HTTPS tunnel for local development. For example:

- Frontend: `https://my-frontend.example`
- Sign-in callback: `https://my-frontend.example/redirect.html`

The callback is the page Shopify sends customers back to after sign-in. You can use the [two-page frontend example](docs/frontend.md), served from `pb_public` by the Go code below.

### 3. Create and install the Shopify app

Follow the [Shopify setup guide](docs/shopify-setup.md) with the URLs from step 2. It covers app creation, permissions, callback registration and installation on your store. Keep **Embed app in Shopify admin** unchecked and save the app's **Client ID**.

### 4. Configure the environment

Set these variables in the shell or deployment platform that starts PocketBase. Replace the example values:

```dotenv
SHOPIFY_STORE_URL=https://my-store.myshopify.com
SHOPIFY_CUSTOMER_CALLBACK_URL=https://my-frontend.example/redirect.html
SHOPIFY_APP_CLIENT_ID=my-public-client
```

- Store URL: your `*.myshopify.com` domain with `https://`, without a path or trailing slash.
- Callback URL: copy the URL registered in `shopify.app.toml` under `customer_authentication.redirect_uris` exactly.
- Client ID: copy it from your app's **App settings > Credentials** in the [Dev Dashboard](https://dev.shopify.com/dashboard).

The Go example reads these variables with `os.Getenv`. If you use a `.env` file, configure your runner to load it; Go does not load it automatically.

### 5. Register the service

Add the service to your application's `OnServe` hook, after migrations have created the auth collection. This `main.go` also serves the frontend from `pb_public`. If you already serve your frontend, keep your existing routes.

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

Keep `context.Background()` or use a context that lasts for the service's lifetime. A short startup timeout would stop Shopify connection retries.

### 6. Restart PocketBase and test sign-in

With the environment variables loaded, run:

```sh
go mod tidy
go run . serve
```

Open your frontend over HTTPS and sign in with a customer account from your Shopify store. For the frontend example, open `https://my-frontend.example/index.html`. Keep your deployment or tunnel pointing to the PocketBase server.

If your domain changes, update Shopify's **App URL**, `customer_authentication.redirect_uris`, `SHOPIFY_CUSTOMER_CALLBACK_URL` and any frontend configuration. Deploy the Shopify configuration and restart PocketBase.

## Frontend

Use the [frontend example](docs/frontend.md) for the sign-in page, callback and deployment precautions. It uses `authWithOAuth2Code`; the `authWithOAuth2` popup flow is not supported. Add error handling and the second-factor step if you enable MFA.

## Account and startup behavior

The library enables the `shopify` provider and creates a private `_shopifyIdentities` collection. It links accounts by Shopify identity. An existing account with the same email is not merged; login returns HTTP 409. Shopify tokens are neither stored nor sent to the frontend.

PocketBase starts even if Shopify is unavailable and retries the connection automatically. Until it succeeds, Shopify is absent from `listAuthMethods()` and direct Shopify login requests return HTTP 503 with `Retry-After`.

See the [integration reference](docs/integration.md) for custom hooks, middleware, MFA and account management.

## Tests

Run `mise run check` for Go tests, the SDK OAuth test, static analysis and compilation. Mise and Node are only needed for development. See the [testing guide](tests/README.md).

## License

MIT. See [LICENSE](LICENSE).
