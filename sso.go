package shopifysso

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/oauth2"
)

// Service connects one Shopify shop to one PocketBase auth collection.
type Service struct {
	app             core.App
	cfg             Config
	collectionID    string
	origin          string
	client          *http.Client
	oauth           oauth2.Config
	verifier        *oidc.IDTokenVerifier
	issuer, graphql string
}

// New discovers Shopify's endpoints and configures the existing auth collection
// and private identity storage. Call it after PocketBase has bootstrapped, then
// call Register with the host router before serving requests.
func New(ctx context.Context, app core.App, cfg Config) (*Service, error) {
	if cfg.AuthCollection == "" {
		cfg.AuthCollection = "users"
	}
	if err := checkURL(cfg.ShopURL, cfg.AllowLoopbackHTTP, true); err != nil {
		return nil, err
	}
	if err := checkURL(cfg.RedirectURL, cfg.AllowLoopbackHTTP, false); err != nil {
		return nil, err
	}
	if cfg.ClientID == "" {
		return nil, errors.New("Shopify ClientID is required")
	}
	callback, _ := url.Parse(cfg.RedirectURL)
	origin := callback.Scheme + "://" + callback.Host
	s := &Service{app: app, cfg: cfg, origin: origin, client: &http.Client{
		Timeout: 10 * time.Second, Transport: shopTransport{origin: origin},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if err := s.discover(ctx); err != nil {
		return nil, err
	}
	if err := ensureCollection(app, cfg.AuthCollection); err != nil {
		return nil, err
	}
	if err := s.configureProvider(); err != nil {
		return nil, err
	}
	return s, nil
}

// Register binds Shopify verification to PocketBase's native OAuth endpoint.
// No custom route or cookie is created. The frontend owns its redirect callback
// and checks the state returned by listAuthMethods.
func (s *Service) Register(r *router.Router[*core.RequestEvent]) {
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_oauth2", Func: s.nativeOAuthMiddleware})
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
