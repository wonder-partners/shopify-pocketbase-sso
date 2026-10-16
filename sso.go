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
	app          core.App
	cfg          Config
	collectionID string
	origin       string
	client       *http.Client
	ctx          context.Context
	cancel       context.CancelFunc
	ready, done  chan struct{}
	// Discovery fields are published by closing ready, then remain immutable.
	oauth           oauth2.Config
	verifier        *oidc.IDTokenVerifier
	issuer, graphql string
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
	if err := checkURL(cfg.ShopURL, cfg.AllowLoopbackHTTP, true); err != nil {
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
	s := &Service{app: app, cfg: cfg, origin: origin, client: &http.Client{
		Timeout: 10 * time.Second, Transport: shopTransport{origin: origin},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if err := ensureCollection(app, cfg.AuthCollection); err != nil {
		return nil, err
	}
	if err := s.configureProvider(); err != nil {
		return nil, err
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.ready, s.done = make(chan struct{}), make(chan struct{})
	app.OnTerminate().Bind(&hook.Handler[*core.TerminateEvent]{
		Id: "shopify_discovery", Priority: -1000, Func: func(e *core.TerminateEvent) error {
			s.cancel()
			<-s.done
			return e.Next()
		},
	})
	go s.discoverInBackground()
	return s, nil
}

func (s *Service) isReady() bool {
	if s.ctx.Err() != nil {
		return false
	}
	select {
	case <-s.ready:
		return true
	default:
		return false
	}
}

func (s *Service) discoverInBackground() {
	defer close(s.done)
	delay := time.Second
	for s.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		err := s.discover(ctx)
		cancel()
		if err == nil && s.ctx.Err() == nil {
			err = s.configureProvider()
			if err == nil && s.ctx.Err() == nil {
				close(s.ready)
				s.app.Logger().Info("Shopify authentication is ready")
				return
			}
		}
		if s.ctx.Err() != nil {
			return
		}
		s.app.Logger().Warn("Shopify discovery unavailable; retrying", "error", err, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(2*delay, time.Minute)
	}
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
