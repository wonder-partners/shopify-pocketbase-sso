# Library tests

Run commands from the repository root.

```powershell
mise run test
```

This installs the Go dependencies and the JavaScript SDK pinned in `sdk/package-lock.json`, then runs the Go tests and the SDK OAuth test. `mise run check` also runs Go static analysis, builds the library and checks JavaScript syntax.

## Test locations

- Root-level `*_test.go` files cover OAuth/OIDC, private identities, and PocketBase's rules, hooks, MFA and realtime events.
- `tests/sdk/oauth.test.mjs` checks the OAuth flow with the official JavaScript SDK.
- `internal/mockshop` simulates Shopify's discovery, OAuth, JWKS and GraphQL endpoints locally.

## OAuth test with the official SDK

`sdk/oauth.test.mjs` executes the frontend functions from the [library README](../README.md#minimal-frontend-example). It checks silent and interactive sign-in, state validation, temporary storage, replay rejection, auth store updates, session persistence and refresh.

`sdk_integration_test.go` starts HTTP servers and temporary SQLite databases, then passes their parameters to the Node script through standard input. The script cannot be run on its own.

The Go tests cover CRUD operations and owner rules for Shopify and native accounts. The JavaScript test focuses on OAuth compatibility with the SDK.

The SDK and HTTP requests are real. Browser navigation and storage are simulated in Node. These tests use no customer data or local Shopify configuration, and do not replace testing in a browser or against a real Shopify store.

## Run only the Go tests

```powershell
mise exec -- go test ./... -count=1
```

Without `RUN_JS_SDK_TEST=1`, the SDK OAuth test is skipped. Node is required for the full test suite, but not to use the Go library. The npm dependency in `sdk/package.json` is used only for tests.

The demo and its tests live in the separate `shopify-pocketbase-sso-demo` project. No files from that project are needed to test the library.
