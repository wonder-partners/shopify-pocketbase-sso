# Shopify SSO for PocketBase

Lets customers of one Shopify shop sign in to a PocketBase app through PocketBase's native OAuth2 flow.

## Language

**Committed response**:
The reply to a Shopify login or unlink, which the browser receives only once the account changes behind it are final: saved, or rolled back by that reply's own rejection.
_Avoid_: buffered response, deferred response, transactional response
