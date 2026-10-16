package shopifysso

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/oauth2"
)

const providerName = "shopify"
const trustedProfileKey = "studio_shopify_verified_profile"

type providerContextKey struct{}
type providerContext struct {
	service  *Service
	verifier string
	code     string
	prepared *shopifyProvider
}

var pkceVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// A factory must not capture a global shop/app: multiple PocketBase instances
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
	service         *Service
	subject         string
	profile         *customer
	claims          claims
	token           *oauth2.Token
	previousAccount *core.Record
}

var _ auth.Provider = (*shopifyProvider)(nil)

func identityID(issuer, subject string) string {
	// Namespace identities by issuer without rounding numeric Shopify subjects.
	return fmt.Sprintf("%x", sha256.Sum256([]byte(issuer+"\x00"+subject)))
}

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
	if p.RedirectURL() != s.oauth.RedirectURL || p.ClientId() != s.cfg.ClientID {
		return nil, errors.New("invalid Shopify client or redirect URL")
	}
	if flow.prepared != nil {
		if code != flow.code {
			return nil, errors.New("Shopify code differs from the verified request")
		}
		verified := flow.prepared
		p.service, p.subject = verified.service, verified.subject
		p.profile, p.claims, p.token = verified.profile, verified.claims, verified.token
		return p.token, nil
	}
	ctx := oidc.ClientContext(p.Context(), s.client)
	// Exchange as a public PKCE client, never using PocketBase's config marker.
	token, err := s.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.verifier))
	if err != nil {
		return nil, router.NewApiError(http.StatusBadGateway, "Shopify token exchange failed.", nil)
	}
	raw, _ := token.Extra("id_token").(string)
	id, err := s.verifier.Verify(ctx, raw)
	if err != nil {
		reason := idTokenFailureReason(err)
		s.app.Logger().Warn("Shopify ID token rejected", "reason", reason, "claim_types", idTokenClaimTypes(raw))
		return nil, router.NewApiError(http.StatusUnauthorized, "Invalid Shopify ID token ("+reason+").", nil)
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(oauth2.S256ChallengeFromVerifier(flow.verifier))) != 1 || id.Subject == "" {
		return nil, router.NewApiError(http.StatusUnauthorized, "Invalid Shopify nonce or subject.", nil)
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return nil, router.NewApiError(http.StatusUnauthorized, "Invalid access token hash.", nil)
	}
	if id.Claims(&p.claims) != nil {
		return nil, router.NewApiError(http.StatusUnauthorized, "Invalid Shopify claims.", nil)
	}
	p.profile, err = s.fetchCustomer(ctx, token.AccessToken)
	if err != nil {
		return nil, router.NewApiError(http.StatusBadGateway, "Unable to read the Shopify customer. Check Customer Account API permissions.", nil)
	}
	p.service, p.subject, p.token = s, id.Subject, token
	return token, nil
}

func (p *shopifyProvider) FetchAuthUser(token *oauth2.Token) (*auth.AuthUser, error) {
	if p.profile == nil || p.token != token {
		return nil, errors.New("Shopify identity has not been verified")
	}
	u := &auth.AuthUser{
		Id:      identityID(p.service.issuer, p.subject),
		RawUser: map[string]any{}, // Technical identifiers stay in the private collection.
	}
	// PocketBase treats AuthUser.Email as verified. Keep an unverified email
	// exclusively in the trusted record payload, never in this verified field.
	if p.claims.EmailVerified && strings.EqualFold(p.claims.Email, p.profile.EmailAddress.Email) {
		u.Email = p.profile.EmailAddress.Email
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
		Provider     string `json:"provider"`
		CodeVerifier string `json:"codeVerifier"`
		Code         string `json:"code"`
		RedirectURL  string `json:"redirectURL"`
		RedirectUrl  string `json:"redirectUrl"` // PocketBase's deprecated alias.
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
	if !s.isReady() {
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
	return s.transactionalAuthResponse(e, e.Next)
}
