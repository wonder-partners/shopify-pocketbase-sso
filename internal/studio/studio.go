package studio

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/oauth2"
)

type pending struct {
	Browser, Nonce, Verifier string
	Expires                  time.Time
}

type Service struct {
	app             core.App
	cfg             Config
	origin          string
	client          *http.Client
	oauth           oauth2.Config
	verifier        *oidc.IDTokenVerifier
	issuer, graphql string
	mu              sync.Mutex
	pending         map[string]pending
	ready           map[string]pending
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
	s := &Service{app: app, cfg: cfg, origin: origin, pending: map[string]pending{}, ready: map[string]pending{}, client: &http.Client{
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

// Register mounts only the JSON endpoints that secure the Shopify OAuth flow.
// The host application owns its callback page, auth store, navigation and logout.
func (s *Service) Register(r *router.Router[*core.RequestEvent]) {
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "shopify_oauth2", Func: s.nativeOAuthMiddleware})
	g := r.Group("/api/shopify/auth")
	g.POST("/start", s.start)
	g.POST("/complete", s.complete)
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) flowCookieName() string {
	if strings.HasPrefix(s.origin, "https://") {
		return "__Host-shopify_oauth"
	}
	return "shopify_oauth"
}

func (s *Service) setFlowCookie(e *core.RequestEvent, value string, age int) {
	http.SetCookie(e.Response, &http.Cookie{
		Name: s.flowCookieName(), Value: value, Path: "/", MaxAge: age,
		Secure: strings.HasPrefix(s.origin, "https://"), HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
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

func (s *Service) start(e *core.RequestEvent) error {
	noStore(e)
	if err := s.requireOrigin(e); err != nil {
		return err
	}
	var body struct {
		Silent bool `json:"silent"`
	}
	if err := e.BindBody(&body); err != nil {
		return err
	}
	state, browser := random(), random()
	p := pending{Browser: browser, Nonce: random(), Verifier: oauth2.GenerateVerifier(), Expires: time.Now().Add(5 * time.Minute)}
	s.mu.Lock()
	for k, v := range s.pending {
		if time.Now().After(v.Expires) {
			delete(s.pending, k)
		}
	}
	for k, v := range s.ready {
		if time.Now().After(v.Expires) {
			delete(s.ready, k)
		}
	}
	if len(s.pending)+len(s.ready) >= 10000 {
		s.mu.Unlock()
		return e.TooManyRequestsError("Too many login attempts.", nil)
	}
	s.pending[state] = p
	s.mu.Unlock()
	s.setFlowCookie(e, browser, 300)
	opts := []oauth2.AuthCodeOption{oidc.Nonce(p.Nonce), oauth2.S256ChallengeOption(p.Verifier)}
	if body.Silent {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "none"))
	}
	return e.JSON(http.StatusOK, map[string]string{"url": s.oauth.AuthCodeURL(state, opts...)})
}

func (s *Service) consume(state, browser string) (pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pending[state]
	if !ok || browser == "" || subtle.ConstantTimeCompare([]byte(p.Browser), []byte(browser)) != 1 {
		return pending{}, false
	}
	delete(s.pending, state)
	return p, time.Now().Before(p.Expires)
}

func (s *Service) complete(e *core.RequestEvent) error {
	noStore(e)
	if err := s.requireOrigin(e); err != nil {
		return err
	}
	cookie, err := e.Request.Cookie(s.flowCookieName())
	if err != nil {
		return e.BadRequestError("Missing login cookie.", nil)
	}
	var body struct {
		State string `json:"state"`
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := e.BindBody(&body); err != nil {
		return err
	}
	p, ok := s.consume(body.State, cookie.Value)
	if !ok {
		return e.BadRequestError("Invalid or expired login state.", nil)
	}
	if body.Error != "" {
		s.setFlowCookie(e, "", -1)
		switch body.Error {
		case "login_required", "interaction_required", "consent_required", "access_denied":
			return e.JSON(http.StatusOK, map[string]string{"error": body.Error})
		default:
			return e.BadRequestError("Shopify did not authorize the login.", nil)
		}
	}
	if body.Code == "" {
		return e.BadRequestError("Missing authorization code.", nil)
	}
	// No PocketBase token is minted here. Only the native OAuth endpoint can
	// consume this browser-bound code and issue an authenticated response.
	s.mu.Lock()
	s.ready[body.Code] = p
	s.mu.Unlock()
	return e.JSON(http.StatusOK, map[string]string{
		"provider": providerName, "collection": collectionName,
		"code": body.Code, "codeVerifier": p.Verifier, "redirectURL": s.oauth.RedirectURL,
	})
}
