package shopifysso

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Every PocketBase hook this package binds is listed in this file.

// runBeforeHostHooks makes a hook run before the host's own hooks for the same
// event, so it can wrap them (in a transaction, or around their response).
const runBeforeHostHooks = -1000

// runBeforeGzip makes a router middleware run inside PocketBase's native gzip
// middleware (priority 0), so it can rewrite the uncompressed JSON response.
const runBeforeGzip = 1

// bindHooks binds the app hooks of a Shopify login, unlink and shutdown.
// Collection IDs keep these hooks active when the host renames the collection.
func (s *Service) bindHooks() {
	// Login, step 1: choose the account of a verified Shopify customer.
	s.app.OnRecordAuthWithOAuth2Request(s.collectionID).Bind(&hook.Handler[*core.RecordAuthWithOAuth2RequestEvent]{
		Id: "shopify_login_start", Priority: runBeforeHostHooks, Func: s.startLoginHook,
	})
	// Login, step 2: save the Shopify identity once the account exists.
	s.app.OnRecordAuthRequest(s.collectionID).Bind(&hook.Handler[*core.RecordAuthRequestEvent]{
		Id: "shopify_login_finish", Priority: runBeforeHostHooks, Func: s.finishLoginHook,
	})
	// Send the new-device alert only after the login has committed.
	s.app.OnMailerRecordAuthAlertSend().Bind(&hook.Handler[*core.MailerRecordEvent]{
		Id: "shopify_login_alert", Priority: runBeforeHostHooks, Func: deferShopifyAuthAlert,
	})
	// Unlink: send the response only after the matching commit.
	s.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).Bind(&hook.Handler[*core.RecordRequestEvent]{
		Id: "shopify_unlink", Priority: runBeforeHostHooks, Func: s.unlinkShopifyIdentity,
	})
	// Keep Shopify identities paired with their OAuth links: shopify_revoke_with_oauth_link
	// and shopify_revoke_on_verification, owned by the identity store.
	bindIdentityRevocation(s.app, s.collectionID)
	// Shutdown: stop discovery and wait for it.
	s.app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "shopify_discovery", Priority: runBeforeHostHooks, Func: func(e *core.TerminateEvent) error {
			s.store.stop()
			return e.Next()
		},
	})
}

// Register binds Shopify verification to PocketBase's native OAuth endpoint.
// No custom route or cookie is created. The frontend owns its redirect callback
// and checks the state returned by listAuthMethods.
func (s *Service) Register(r *router.Router[*core.RequestEvent]) {
	// Verify a Shopify code before PocketBase's auth-with-oauth2 handler runs.
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_oauth2", Func: s.nativeOAuthMiddleware})
	// Advertise Shopify in auth-methods only when the store is ready.
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_auth_methods", Priority: runBeforeGzip, Func: s.nativeAuthMethodsMiddleware})
}
