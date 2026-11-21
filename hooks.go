package shopifysso

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

const runBeforeHostHooks = -1000

// runBeforeGzip makes a router middleware run inside PocketBase's native gzip
// middleware (priority 0), so it can rewrite the uncompressed JSON response.
const runBeforeGzip = 1

func (s *Service) bindHooks() {
	s.app.OnRecordAuthWithOAuth2Request(s.collectionID).Bind(&hook.Handler[*core.RecordAuthWithOAuth2RequestEvent]{
		Id: "shopify_login_start", Priority: runBeforeHostHooks, Func: s.startLoginHook,
	})
	s.app.OnRecordAuthRequest(s.collectionID).Bind(&hook.Handler[*core.RecordAuthRequestEvent]{
		Id: "shopify_login_finish", Priority: runBeforeHostHooks, Func: s.finishLoginHook,
	})
	s.app.OnMailerRecordAuthAlertSend().Bind(&hook.Handler[*core.MailerRecordEvent]{
		Id: "shopify_login_alert", Priority: runBeforeHostHooks, Func: deferShopifyAuthAlert,
	})
	s.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).Bind(&hook.Handler[*core.RecordRequestEvent]{
		Id: "shopify_unlink", Priority: runBeforeHostHooks, Func: s.unlinkShopifyIdentity,
	})
	bindIdentityRevocation(s.app, s.collectionID)
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
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_oauth2", Func: s.nativeOAuthMiddleware})
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_auth_methods", Priority: runBeforeGzip, Func: s.nativeAuthMethodsMiddleware})
}
