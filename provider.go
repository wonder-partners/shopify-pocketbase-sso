package shopifysso

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/oauth2"
)

const providerName = "shopify"

// The event store key carrying a started Shopify login to its second step.
const shopifyLoginKey = "studio_shopify_verified_profile"

type providerContextKey struct{}
type providerContext struct {
	service  *Service
	verifier string
	code     string
	prepared *shopifyProvider
}

var pkceVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// A factory must not capture a global store/app: multiple PocketBase instances
// can coexist. The native request context selects the service for this request.
func init() {
	auth.Providers[providerName] = func() auth.Provider {
		p := &shopifyProvider{}
		p.SetPKCE(true)
		p.SetScopes([]string{"openid", "email", "customer-account-api:full"})
		p.SetDisplayName("Shopify")
		return p
	}
}

type shopifyProvider struct {
	auth.BaseProvider
	service  *Service
	customer verifiedCustomer
	token    *oauth2.Token
}

var _ auth.Provider = (*shopifyProvider)(nil)

// PocketBase generates a fresh S256 challenge in listAuthMethods. Reuse that
// random public value as the OIDC nonce, never the secret verifier. This lets
// FetchToken check the nonce against the submitted verifier without server state.
func (p *shopifyProvider) BuildAuthURL(state string, opts ...oauth2.AuthCodeOption) string {
	u, err := url.Parse(p.BaseProvider.BuildAuthURL(state, opts...))
	if err != nil {
		return ""
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		return ""
	}
	q.Set("nonce", q.Get("code_challenge"))
	u.RawQuery = q.Encode()
	return u.String()
}

func (p *shopifyProvider) FetchToken(code string, _ ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	flow, ok := p.Context().Value(providerContextKey{}).(providerContext)
	if !ok || flow.service == nil || !pkceVerifierPattern.MatchString(flow.verifier) {
		return nil, errors.New("a native Shopify OAuth request with PKCE is required")
	}
	s := flow.service
	if p.RedirectURL() != s.cfg.RedirectURL || p.ClientId() != s.cfg.ClientID {
		return nil, errors.New("invalid Shopify client or redirect URL")
	}
	if flow.prepared != nil {
		if code != flow.code {
			return nil, errors.New("Shopify code differs from the verified request")
		}
		verified := flow.prepared
		p.service, p.customer, p.token = verified.service, verified.customer, verified.token
		return p.token, nil
	}
	customer, token, err := s.store.verify(p.Context(), code, flow.verifier)
	if err != nil {
		return nil, err
	}
	p.service, p.customer, p.token = s, customer, token
	return token, nil
}

func (p *shopifyProvider) FetchAuthUser(token *oauth2.Token) (*auth.AuthUser, error) {
	if p.token == nil || p.token != token {
		return nil, errors.New("Shopify identity has not been verified")
	}
	u := &auth.AuthUser{
		Id:      oauthLinkProviderID(p.customer.issuer, p.customer.subject),
		RawUser: map[string]any{}, // Technical identifiers stay in the private collection.
	}
	// PocketBase treats AuthUser.Email as verified. Keep an unverified email
	// exclusively in the trusted record payload, never in this verified field.
	if p.customer.emailVerified {
		u.Email = p.customer.email
	}
	// Do not expose upstream access/refresh tokens in PocketBase's auth meta.
	u.Expiry.Scan(token.Expiry)
	return u, nil
}

func (s *Service) nativeOAuthMiddleware(e *core.RequestEvent) error {
	if e.Request.Method != http.MethodPost || !strings.HasPrefix(e.Request.URL.Path, "/api/collections/") || !strings.HasSuffix(e.Request.URL.Path, "/auth-with-oauth2") {
		return e.Next()
	}
	var body struct {
		Provider     string `json:"provider" form:"provider"`
		CodeVerifier string `json:"codeVerifier" form:"codeVerifier"`
		Code         string `json:"code" form:"code"`
		RedirectURL  string `json:"redirectURL" form:"redirectURL"`
		RedirectUrl  string `json:"redirectUrl" form:"redirectUrl"` // PocketBase's deprecated alias.
	}
	if err := e.BindBody(&body); err != nil {
		return err
	}
	if body.Provider != providerName {
		return e.Next()
	}
	noStore(e)
	if err := s.requireOrigin(e); err != nil {
		return err
	}
	c, err := s.app.FindCollectionByNameOrId(s.collectionID)
	if err != nil {
		return err
	}
	parts := strings.Split(e.Request.URL.Path, "/")
	if len(parts) != 5 || (parts[3] != c.Name && parts[3] != c.Id) {
		return e.ForbiddenError("Shopify is not configured for this auth collection.", nil)
	}
	if !s.store.ready() {
		e.Response.Header().Set("Retry-After", "5")
		return router.NewApiError(http.StatusServiceUnavailable, "Shopify authentication is temporarily unavailable.", nil)
	}
	if !pkceVerifierPattern.MatchString(body.CodeVerifier) {
		return e.BadRequestError("A valid PKCE verifier is required.", nil)
	}
	config, ok := c.OAuth2.GetProviderConfig(providerName)
	if !c.OAuth2.Enabled || !ok || body.Code == "" {
		return e.Next() // Let PocketBase return its native configuration/form error.
	}
	provider, err := config.InitProvider()
	if err != nil {
		return e.InternalServerError("Failed to initialize the Shopify provider.", nil)
	}
	prepared, ok := provider.(*shopifyProvider)
	if !ok {
		return e.ForbiddenError("Missing Shopify provider.", nil)
	}
	if body.RedirectURL == "" {
		body.RedirectURL = body.RedirectUrl
	}
	// Complete all Shopify I/O before acquiring the database write transaction.
	// The native provider reuses this request-local verified result in its normal
	// token/user lookup, without exchanging the code a second time.
	ctx, cancel := context.WithTimeout(e.Request.Context(), 30*time.Second)
	defer cancel()
	flow := providerContext{service: s, verifier: body.CodeVerifier, code: body.Code}
	prepared.SetContext(context.WithValue(ctx, providerContextKey{}, flow))
	prepared.SetRedirectURL(body.RedirectURL)
	if _, err := prepared.FetchToken(body.Code); err != nil {
		return err
	}
	flow.prepared = prepared
	e.Request = e.Request.WithContext(context.WithValue(ctx, providerContextKey{}, flow))
	return s.respondToOAuth(e)
}
