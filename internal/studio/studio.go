package studio

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

type Service struct {
	app             core.App
	cfg             Config
	origin          string
	client          *http.Client
	oauth           oauth2.Config
	verifier        *oidc.IDTokenVerifier
	issuer, graphql string
}

func New(ctx context.Context, app core.App, cfg Config) (*Service, error) {
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
	if err := ensureCollection(app); err != nil {
		return nil, err
	}
	if err := s.configureProvider(); err != nil {
		return nil, err
	}
	return s, nil
}

// Register binds Shopify verification to PocketBase's native OAuth endpoint.
// No custom route, login transaction or cookie is created. The frontend owns
// its redirect callback and checks the state returned by listAuthMethods.
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
