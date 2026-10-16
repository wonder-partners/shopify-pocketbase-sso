// Package shopifysso adds Shopify Customer Account authentication to PocketBase's
// native OAuth2 endpoints. The host owns its auth collection, access rules,
// frontend callback and application lifecycle.
//
// Initialize a Service with New from the host's OnServe hook, then call Register
// with that event's router. One shop and one auth collection are supported per
// PocketBase app. Importing this package registers the shopify provider factory
// with PocketBase; shop configuration stays local to each Service.
package shopifysso
