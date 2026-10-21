# Integration reference

Optional details for applications that customize PocketBase hooks, middleware or account management. The complete setup guide is in the [README](../README.md).

## Installation options

Run Go commands from the folder containing your application's `go.mod`. Go downloads dependencies and updates `go.mod` and `go.sum` for you.

For a private repository, give Git read access and mark the module as private before installing:

```sh
go env -w GOPRIVATE=github.com/wonder-partners/shopify-pocketbase-sso
```

If `GOPRIVATE` already has entries, append this path to its comma-separated list. Configure access on every build machine, including CI.

To install a specific version or commit, replace `<version-or-commit>`:

```sh
go get github.com/wonder-partners/shopify-pocketbase-sso@<version-or-commit>
```

To develop the library from a local checkout, replace the path below:

```sh
go mod edit -replace=github.com/wonder-partners/shopify-pocketbase-sso=/path/to/shopify-pocketbase-sso
go mod tidy
```

## Configuration

Pass configuration to `shopifysso.New` through `shopifysso.Config`. The library does not read environment variables itself.

- `ShopURL`: store HTTPS origin, without a path or trailing slash.
- `RedirectURL`: frontend callback URL registered with Shopify.
- `ClientID`: public Customer Account API client ID. Authentication uses PKCE and needs no client secret.
- `AuthCollection`: existing auth collection name or ID. Defaults to `users`.
- `AllowLoopbackHTTP`: allows loopback HTTP for tests. Disabled by default.

## Collections and account identity

The host project owns its fields and rules. The library enables the `shopify` provider and creates the private `_shopifyIdentities` collection. Provider configuration reads and updates the auth collection in one transaction to preserve concurrent host changes. The auth collection's create rule determines whether a first login can create an account.

Accounts are linked by Shopify identity. Matching email addresses do not merge accounts; a conflict returns HTTP 409. Shopify tokens are neither stored nor exposed to the frontend.

## Startup and endpoint discovery

`New` validates local configuration and database setup synchronously, then discovers Shopify's endpoints in the background. A discovery outage does not prevent PocketBase from starting. Until discovery succeeds, `listAuthMethods()` omits Shopify while preserving other auth methods, and explicit Shopify login requests return HTTP 503 with `Retry-After`.

- Each discovery attempt has a 20-second deadline. Retry delays increase from 1 second to a maximum of 60 seconds.
- `New` installs provider configuration and hooks synchronously. Discovery publishes endpoints in memory without acquiring the database writer.
- After discovery succeeds, Shopify becomes available automatically. `listAuthMethods()` uses the discovered endpoint and preserves PocketBase's state, PKCE and nonce parameters.
- The context passed to `New` controls the service lifetime. Cancellation or PocketBase termination stops requests and retries, even while another transaction holds the database writer. Use a lifetime context rather than a short startup timeout.

## Auth methods and compression middleware

The native `fields` query parameter on `listAuthMethods()` is applied after Shopify filtering and URL rewriting, including for the legacy `authProviders` response.

PocketBase's native `apis.Gzip()` can be registered before or after `Service.Register`. The auth-methods middleware runs at priority 1 so it rewrites JSON before native compression. Custom compression middleware should run at a lower priority to wrap the rewritten response.

## Unlinking accounts

Deleting a Shopify link, through the records API or a native security purge, also removes its matching private identity in the same transaction. The user account remains available through its other authentication methods. A subsequent Shopify login cannot recover that account through the deleted identity; an email conflict still returns HTTP 409.

Unlink request responses, including custom responses written by project hooks, are buffered until the transaction completes. A storage or commit failure discards any pending success response; a project error response rolls back both identity deletions.

This also applies when a project middleware opens the transaction before the unlink request. A written HTTP rejection propagates to that transaction's owner to trigger rollback. The native 204 response and custom hook responses remain buffered until the outer transaction finishes.

## Email verification and session revocation

When a previously unverified email becomes verified, PocketBase revokes the old OAuth links and refresh tokens. Only an accepted Shopify authentication can restore its current Shopify link and private identity within that same transaction. Native email verification or account recovery leaves the old Shopify identity revoked.

Verification also invalidates existing PocketBase sessions when a legacy private Shopify identity has no native OAuth link. Token invalidation and private identity deletion share the verification transaction; a failed revocation rolls both back.

## Authentication transactions and MFA

Profile synchronization and identity links commit together after the OAuth hook chain accepts the request.

- The JSON response is serialized before commit and sent afterwards. Serialization failures, authentication rule rejections and request hook rejections roll back database changes.
- A hook that stops the chain without writing an HTTP response causes a rollback and returns HTTP 500.
- A native MFA challenge commits the profile and challenge after accepting the Shopify factor, while the second factor is still pending.
- Hooks must use their event's `e.App` for database operations to participate in the transaction. External side effects cannot be rolled back.

The native MFA response remains pending until the hook chain completes. A project hook that handles `apis.ErrMFA` and writes an error response replaces the challenge entirely, preserving its status, headers and body while rolling back the profile, links and MFA challenge.

Project hooks may nest `e.App.RunInTransaction` calls using the callback's app for subsequent hooks. Shopify response tracking and deferred alerts remain attached to the same authentication transaction.

When a project middleware starts a transaction before Shopify authentication, the response waits for that outer transaction to commit. A rollback discards the pending JWT or MFA challenge. An accepted MFA first factor returns no error to the transaction owner so its challenge can commit; the client still receives PocketBase's HTTP 401 MFA response after commit. Middleware must return its policy errors to the transaction owner to trigger rollback.

## New-device email alerts

For Shopify logins, native new-device email alerts and their mail hooks run asynchronously after a successful commit. Slow mail delivery holds neither the database transaction nor the login response. A rollback discards the alert; delivery failures are logged without rejecting the login. Other auth methods keep PocketBase's normal alert behavior.
