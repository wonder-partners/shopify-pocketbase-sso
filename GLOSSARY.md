# Shopify SSO for PocketBase

Lets customers of one Shopify store sign in to a PocketBase app through PocketBase's native OAuth2 flow.

## Language

### Store

**Store**:
The one Shopify store whose customers can log in, configured by its URL; Shopify itself is the platform hosting it.
_Avoid_: shop

**Discovery**:
Reading the store's public sign-in endpoints and keys when the application starts; Shopify logins stay unavailable until it succeeds.
_Avoid_: bootstrap, configuration

### Identity

**Account**:
A user of the host application, stored in its auth collection.
_Avoid_: user record; "account" for anything Shopify-side

**Shopify identity**:
The private record tying one account to one customer of one Shopify store.
_Avoid_: Shopify account, private account

**OAuth link**:
PocketBase's own record of an external sign-in for an account; for Shopify it always pairs with a Shopify identity.
_Avoid_: external auth, native link

### Login

**Shopify login**:
One attempt by a customer verified by Shopify to enter the application; it resumes the account tied to their Shopify identity, or creates a new account.
_Avoid_: sign-in, OAuth login

### Responses

**Committed response**:
The reply to a Shopify login or unlink, which the browser receives only once the account changes behind it are final: saved, or rolled back by that reply's own rejection.
_Avoid_: buffered response, deferred response, transactional response
