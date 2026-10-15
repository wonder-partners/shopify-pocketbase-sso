package studio

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
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
	service *Service
	browser string
}

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
	service *Service
	subject string
	profile *customer
	claims  claims
	token   *oauth2.Token
}

var _ auth.Provider = (*shopifyProvider)(nil)

func identityID(issuer, subject string) string {
	// Namespace identities by issuer without rounding numeric Shopify subjects.
	return fmt.Sprintf("%x", sha256.Sum256([]byte(issuer+"\x00"+subject)))
}

func (p *shopifyProvider) FetchToken(code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	flow, ok := p.Context().Value(providerContextKey{}).(providerContext)
	if !ok || flow.service == nil || flow.browser == "" {
		return nil, errors.New("start the Shopify login from the studio")
	}
	s := flow.service
	if p.RedirectURL() != s.oauth.RedirectURL || p.ClientId() != s.cfg.ClientID {
		return nil, errors.New("invalid Shopify client or redirect URL")
	}
	s.mu.Lock()
	pending, found := s.ready[code]
	if found && subtle.ConstantTimeCompare([]byte(pending.Browser), []byte(flow.browser)) == 1 {
		delete(s.ready, code)
	} else {
		found = false
	}
	s.mu.Unlock()
	if !found || time.Now().After(pending.Expires) {
		return nil, errors.New("invalid or expired browser-bound Shopify code")
	}
	ctx := oidc.ClientContext(p.Context(), s.client)
	// Use the configured public/confidential Shopify client, not PocketBase's
	// mandatory config placeholder for public clients. Never send that placeholder.
	token, err := s.oauth.Exchange(ctx, code, opts...)
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
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(pending.Nonce)) != 1 || id.Subject == "" {
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
		Provider string `json:"provider"`
	}
	if err := e.BindBody(&body); err != nil {
		return err
	}
	if body.Provider != providerName {
		return e.Next()
	}
	noStore(e)
	if e.Request.Header.Get("Origin") != s.cfg.StudioURL {
		return e.ForbiddenError("Invalid origin.", nil)
	}
	cookie, err := e.Request.Cookie(s.cookieName("flow"))
	if err != nil {
		return e.BadRequestError("Missing login cookie.", nil)
	}
	c, err := s.app.FindCollectionByNameOrId(s.cfg.AuthCollection)
	if err != nil {
		return err
	}
	parts := strings.Split(e.Request.URL.Path, "/")
	if len(parts) != 5 || (parts[3] != c.Name && parts[3] != c.Id) {
		return e.ForbiddenError("Shopify is not configured for this auth collection.", nil)
	}
	e.Request = e.Request.WithContext(context.WithValue(e.Request.Context(), providerContextKey{}, providerContext{s, cookie.Value}))
	return s.captureAuthResponse(e, c, func() error {
		err := e.Next()
		// The code was consumed by the provider. Remove the temporary flow cookie.
		// A failed login must never produce a session cookie, handled by the buffer.
		s.setCookie(e, "flow", "", -1)
		return err
	})
}
