# Customize your PocketBase integration

Start with the [README](../README.md) to set up sign-in. Use this guide when you need to change the configuration, add hooks or middleware, or manage linked accounts.

## Choose a library version

Run these commands from the folder containing your application's `go.mod`. For private repository access, follow [step 5 of the README](../README.md#5-add-the-service-to-pocketbase), including on your CI build machine.

To use a specific version or commit:

```sh
go get github.com/wonder-partners/shopify-pocketbase-sso@<version-or-commit>
```

To test changes from a local copy of the library, replace the example path:

```sh
go mod edit -replace=github.com/wonder-partners/shopify-pocketbase-sso=/path/to/shopify-pocketbase-sso
go mod tidy
```

Remove the replacement when you want to use the published library again:

```sh
go mod edit -dropreplace=github.com/wonder-partners/shopify-pocketbase-sso
go mod tidy
```

## Configure the service

Your application passes a `shopifysso.Config` to `shopifysso.New`. The library does not read environment variables itself.

- `ShopURL`: the store's HTTPS URL, such as `https://my-store.myshopify.com`, without a path or trailing slash.
- `RedirectURL`: the full URL of your frontend callback page, registered with Shopify.
- `ClientID`: your Shopify app's public client ID. No client secret is needed.
- `AuthCollection`: the name or ID of an existing auth collection. Defaults to `users`.
- `AllowLoopbackHTTP`: allows HTTP on `localhost` or a loopback IP for local tests. Defaults to `false`; other URLs still require HTTPS.

Your application controls the auth collection's fields and rules. The library enables the `shopify` provider and creates a private `_shopifyIdentities` collection to track Shopify identities. It updates the provider configuration in a transaction so concurrent changes to the collection are preserved.

## Understand startup behavior

`New` checks the configuration, prepares the database and registers hooks before returning. It then looks up Shopify's authentication and API URLs in the background.

If Shopify is unavailable, PocketBase still starts. Shopify stays hidden from `listAuthMethods()`, and direct Shopify login requests return HTTP 503 with `Retry-After`. Other auth methods remain available.

Each lookup attempt has a 20-second timeout. Retries start after 1 second and increase to a maximum delay of 60 seconds. Once a lookup succeeds, Shopify sign-in becomes available automatically. This lookup does not hold a database write lock.

Use a context that lasts as long as the service, such as `context.Background()` in the README. A short startup timeout would also stop the service when it expires. Cancelling the context or stopping PocketBase stops Shopify requests and retries.

## Add authentication hooks

The library saves the customer profile and identity links in one database transaction. It sends the login response only after the transaction commits, meaning the database has accepted all changes.

When adding hooks:

- Use the event's `e.App` for database operations so they join the same transaction.
- Return your errors so PocketBase can roll back the changes.
- Continue the hook chain unless your hook writes the final response. Stopping it without a response causes a rollback and HTTP 500.
- Keep in mind that a database rollback cannot undo an external API call or an email your own code has already sent.

A rejected auth rule, a rejected request hook or a failure to encode the JSON response rolls back the profile and identity changes.

### Handle MFA

With MFA enabled, accepting Shopify as the first factor saves the profile, identity links and second-factor challenge together. The client receives PocketBase's HTTP 401 MFA response and must complete the second factor before signing in.

If your hook handles `apis.ErrMFA` and writes an error response, that response replaces the MFA challenge. The profile, identity changes and challenge are rolled back.

### Open your own transaction

If a hook calls `e.App.RunInTransaction`, use the app passed to its callback for database work and subsequent hooks.

If middleware opens a transaction around authentication, the login response waits for that outer transaction to commit. A rollback discards the pending token or MFA challenge. Return middleware errors to the code managing that transaction so it can roll back.

An accepted MFA first factor returns no error to the transaction owner so the challenge can be saved. The client still receives HTTP 401 for the second-factor step after commit.

### Send new-device alerts

For Shopify logins, PocketBase's new-device emails and their mail hooks run in the background after commit. They do not delay the login response or hold the database transaction open. A rollback cancels the alert; email delivery failures are logged without rejecting the login.

Other auth methods keep PocketBase's usual alert behavior.

## Add compression middleware

You can register PocketBase's `apis.Gzip()` before or after `Service.Register`.

For custom compression, use a middleware priority lower than 1 so compression wraps the library's JSON response changes. The library's auth-methods middleware uses priority 1.

The `fields` parameter on `listAuthMethods()` still works. PocketBase applies it after the library filters the provider list and updates Shopify's login URL, including in the legacy `authProviders` response.

## Manage linked accounts

Accounts are linked by Shopify identity. An email match does not merge accounts; a conflicting email returns HTTP 409. Shopify tokens are not stored or sent to the frontend.

### Remove a Shopify link

Deleting a Shopify OAuth link through the records API or PocketBase's security cleanup also deletes its private identity in the same transaction. The user can still sign in through any other auth methods they have.

A later Shopify login cannot restore access through the deleted identity. If the email still belongs to the existing account, login returns HTTP 409.

The unlink response waits for the transaction to commit, including when middleware opened an outer transaction. A database failure discards a pending success response. A hook's error response rolls back both deletions and also signals failure to the outer transaction, if there is one.

### Verify an email or recover an account

When an unverified email becomes verified, PocketBase revokes the old OAuth links and refresh tokens. The library also removes the private Shopify identity in the same transaction.

If this happens during an accepted Shopify login, that login can restore its current Shopify link and identity within the transaction. Native email verification or account recovery leaves the old Shopify identity revoked.

For older records that have a private Shopify identity but no native OAuth link, verification also invalidates existing PocketBase sessions. If any part of the revocation fails, the database changes are rolled back together.
