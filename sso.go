package shopifysso

import (
	"context"
	"errors"
	"net/url"

	"github.com/pocketbase/pocketbase/core"
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
	id, err := saveProviderConfig(app, cfg.AuthCollection, cfg.ClientID)
	if err != nil {
		return nil, err
	}
	s.collectionID = id
	s.bindHooks()
	s.store = startStore(ctx, cfg, app.Logger)
	return s, nil
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
