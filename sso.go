package shopifysso

import (
	"context"
	"errors"
	"net/url"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Service connects one Shopify store to one PocketBase auth collection.
type Service struct {
	app          core.App
	cfg          Config
	collectionID string
	origin       string
	store        *store
}

// New configures the existing auth collection and private identity storage, then
// discovers Shopify's endpoints in the background. Shopify stays unavailable
// until discovery succeeds. ctx controls the service lifetime; PocketBase's
// OnTerminate hook also stops discovery. Call New after bootstrap, then Register
// with the host router before serving requests.
func New(ctx context.Context, app core.App, cfg Config) (*Service, error) {
	if cfg.AuthCollection == "" {
		cfg.AuthCollection = "users"
	}
	if err := checkURL(cfg.StoreURL, cfg.AllowLoopbackHTTP, true); err != nil {
		return nil, err
	}
	if err := checkURL(cfg.RedirectURL, cfg.AllowLoopbackHTTP, false); err != nil {
		return nil, err
	}
	if cfg.ClientID == "" {
		return nil, errors.New("Shopify ClientID is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	callback, _ := url.Parse(cfg.RedirectURL)
	origin := callback.Scheme + "://" + callback.Host
	s := &Service{app: app, cfg: cfg, origin: origin}
	if err := ensureCollection(app, cfg.AuthCollection); err != nil {
		return nil, err
	}
	if err := s.configureProvider(); err != nil {
		return nil, err
	}
	s.store = startStore(ctx, cfg, app.Logger)
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "shopify_discovery", Priority: -1000, Func: func(e *core.TerminateEvent) error {
			s.store.stop()
			return e.Next()
		},
	})
	return s, nil
}

// Register binds Shopify verification to PocketBase's native OAuth endpoint.
// No custom route or cookie is created. The frontend owns its redirect callback
// and checks the state returned by listAuthMethods.
func (s *Service) Register(r *router.Router[*core.RequestEvent]) {
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_oauth2", Func: s.nativeOAuthMiddleware})
	// PocketBase's native gzip middleware has priority 0. Rewrite its inner
	// JSON response before compression, independently of registration order.
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_auth_methods", Priority: 1, Func: s.nativeAuthMethodsMiddleware})
}

func noStore(e *core.RequestEvent) {
	e.Response.Header().Set("Cache-Control", "no-store")
	e.Response.Header().Set("X-Content-Type-Options", "nosniff")
}

func (s *Service) requireOrigin(e *core.RequestEvent) error {
	if e.Request.Header.Get("Origin") != s.origin {
		return e.ForbiddenError("Invalid origin.", nil)
	}
	return nil
}
