package studio

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/oauth2"
)

type pending struct {
	Browser, Nonce, Verifier string
	Expires                  time.Time
	Silent                   bool
}

type Service struct {
	app             core.App
	cfg             Config
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
	if err := checkURL(cfg.StudioURL, cfg.AllowLoopbackHTTP, true); err != nil {
		return nil, err
	}
	if cfg.ClientID == "" {
		return nil, errors.New("SHOPIFY_CUSTOMER_CLIENT_ID is required")
	}
	if cfg.AuthCollection == "" {
		cfg.AuthCollection = collectionName
	}
	s := &Service{app: app, cfg: cfg, pending: map[string]pending{}, ready: map[string]pending{}, client: &http.Client{
		Timeout: 10 * time.Second, Transport: shopTransport{origin: cfg.StudioURL},
		// A redirect must not leak access tokens to another endpoint.
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

func (s *Service) Register(r *router.Router[*core.RequestEvent]) {
	r.Bind(&hook.Handler[*core.RequestEvent]{Id: "studio_shopify_oauth2", Func: s.nativeOAuthMiddleware})
	g := r.Group("/studio")
	// Authorization callbacks carry a one-time code. Do not log their query strings.
	g.Unbind(apis.DefaultActivityLoggerMiddlewareId)
	g.GET("", s.home)
	g.GET("/auth", func(e *core.RequestEvent) error { return s.begin(e, false) })
	g.GET("/callback", s.callback)
	g.POST("/session", s.session)
	g.POST("/logout", s.logout)
	for _, name := range []string{"client.js", "callback.js", "pocketbase.es.mjs"} {
		g.GET("/"+name, func(e *core.RequestEvent) error {
			body, err := browserAssets.ReadFile("web/" + name)
			if err != nil {
				return err
			}
			noStore(e)
			return e.Blob(http.StatusOK, "text/javascript; charset=utf-8", body)
		})
	}
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) cookieName(name string) string {
	if strings.HasPrefix(s.cfg.StudioURL, "https://") {
		return "__Host-studio_" + name
	}
	return "studio_" + name
}

func (s *Service) setCookie(e *core.RequestEvent, name, value string, age int) {
	http.SetCookie(e.Response, &http.Cookie{
		Name: s.cookieName(name), Value: value, Path: "/", MaxAge: age,
		Secure: strings.HasPrefix(s.cfg.StudioURL, "https://"), HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func noStore(e *core.RequestEvent) {
	// Keep intermediaries from rewriting account HTML, e.g. email obfuscation.
	e.Response.Header().Set("Cache-Control", "no-store, no-transform")
	e.Response.Header().Set("Referrer-Policy", "no-referrer")
	e.Response.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (s *Service) begin(e *core.RequestEvent, silent bool) error {
	noStore(e)
	state, browser := random(), random()
	p := pending{Browser: browser, Nonce: random(), Verifier: oauth2.GenerateVerifier(), Expires: time.Now().Add(5 * time.Minute), Silent: silent}
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
	s.setCookie(e, "flow", browser, 300)
	opts := []oauth2.AuthCodeOption{oidc.Nonce(p.Nonce), oauth2.S256ChallengeOption(p.Verifier)}
	if silent {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "none"))
	}
	return e.Redirect(http.StatusFound, s.oauth.AuthCodeURL(state, opts...))
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

func (s *Service) callback(e *core.RequestEvent) error {
	noStore(e)
	cookie, err := e.Request.Cookie(s.cookieName("flow"))
	if err != nil {
		return e.BadRequestError("Missing login cookie.", nil)
	}
	q := e.Request.URL.Query()
	p, ok := s.consume(q.Get("state"), cookie.Value)
	if !ok {
		return e.BadRequestError("Invalid or expired login state.", nil)
	}
	if q.Get("error") != "" {
		s.setCookie(e, "flow", "", -1)
		if p.Silent && (q.Get("error") == "login_required" || q.Get("error") == "interaction_required" || q.Get("error") == "consent_required") {
			return e.Redirect(http.StatusSeeOther, "/studio?checked=1")
		}
		return e.BadRequestError("Shopify did not authorize the login.", nil)
	}
	if q.Get("code") == "" {
		return e.BadRequestError("Missing authorization code.", nil)
	}
	// Only this browser-bound callback can prepare a code for the native endpoint.
	// The provider still exchanges the real Shopify code and validates its ID token.
	s.mu.Lock()
	s.ready[q.Get("code")] = p
	s.mu.Unlock()
	var b bytes.Buffer
	err = callbackPage.Execute(&b, map[string]string{
		"Code": q.Get("code"), "Verifier": p.Verifier,
		"RedirectURL": s.oauth.RedirectURL, "Collection": s.cfg.AuthCollection,
	})
	if err != nil {
		return err
	}
	return e.HTML(http.StatusOK, b.String())
}

func (s *Service) current(e *core.RequestEvent) (*core.Record, string) {
	c, err := e.Request.Cookie(s.cookieName("session"))
	if err != nil {
		return nil, ""
	}
	r, err := s.app.FindAuthRecordByToken(c.Value, core.TokenTypeAuth)
	if err != nil || r.Collection().Name != s.cfg.AuthCollection {
		return nil, ""
	}
	e.Auth = r
	info, err := e.RequestInfo()
	if err != nil {
		return nil, ""
	}
	allowed, err := s.app.CanAccessRecord(r, info, r.Collection().AuthRule)
	if err != nil || !allowed {
		return nil, ""
	}
	return r, c.Value
}

func (s *Service) home(e *core.RequestEvent) error {
	noStore(e)
	r, _ := s.current(e)
	if r == nil && e.Request.URL.Query().Get("checked") != "1" {
		return s.begin(e, true)
	}
	data := map[string]string{}
	if r != nil {
		data["Email"] = r.Email()
	}
	var b bytes.Buffer
	if err := page.Execute(&b, data); err != nil {
		return err
	}
	return e.HTML(http.StatusOK, b.String())
}

func (s *Service) session(e *core.RequestEvent) error {
	noStore(e)
	if e.Request.Header.Get("Origin") != s.cfg.StudioURL {
		return e.ForbiddenError("Invalid origin.", nil)
	}
	r, _ := s.current(e)
	if r == nil {
		return e.UnauthorizedError("Sign in with Shopify.", nil)
	}
	// Bootstrap the SDK from an existing authenticated cookie. An empty method,
	// like native auth-refresh, avoids counting this as a new MFA first factor.
	return s.authResponse(e, r, "")
}

func (s *Service) logout(e *core.RequestEvent) error {
	noStore(e)
	if e.Request.Header.Get("Origin") != s.cfg.StudioURL {
		return e.ForbiddenError("Invalid origin.", nil)
	}
	s.setCookie(e, "session", "", -1)
	return e.Redirect(http.StatusSeeOther, "/studio?checked=1")
}

var page = template.Must(template.New("studio").Parse(`<!doctype html>
<html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Studio · connexion Shopify</title>
<style>body{font:18px system-ui;max-width:680px;margin:12vh auto;padding:24px;line-height:1.6}a,button{color:#075e46}small{color:#555}button{font:inherit;padding:8px 16px}</style>
<main><h1>Le studio</h1>{{if .Email}}
<p>Connecté avec Shopify.</p><p>{{.Email}}</p>
<p id="api-status">Vérification de l'accès à l'API PocketBase…</p><script type="module" src="/studio/client.js"></script>
<form method="post" action="/studio/logout"><p><button>Se déconnecter du studio</button></p></form>
{{else}}<p>Connecte-toi avec ton compte de la boutique pour ouvrir le studio.</p>
<p><a href="/studio/auth">Se connecter avec Shopify</a></p>{{end}}</main></html>`))

//go:embed web/*
var browserAssets embed.FS

var callbackPage = template.Must(template.New("callback").Parse(`<!doctype html>
<html lang="fr"><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>Connexion au studio</title><main id="oauth" data-code="{{.Code}}" data-verifier="{{.Verifier}}" data-redirect="{{.RedirectURL}}" data-collection="{{.Collection}}">
<p id="oauth-status">Connexion à PocketBase en cours…</p><a href="/studio?checked=1">Retour au studio</a></main>
<script type="module" src="/studio/callback.js"></script></html>`))
