package shopifysso

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pocketbase/pocketbase/tools/router"
	"golang.org/x/oauth2"
)

type store struct {
	cfg           Config
	client        *http.Client
	logger        func() *slog.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	readyCh, done chan struct{}

	oauth           oauth2.Config
	verifier        *oidc.IDTokenVerifier
	issuer, graphql string
}

func startStore(ctx context.Context, cfg Config, logger func() *slog.Logger) *store {
	callback, _ := url.Parse(cfg.RedirectURL)
	origin := callback.Scheme + "://" + callback.Host
	st := &store{cfg: cfg, logger: logger, client: &http.Client{
		Timeout: 10 * time.Second, Transport: storeTransport{origin: origin},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	st.ctx, st.cancel = context.WithCancel(ctx)
	st.readyCh, st.done = make(chan struct{}), make(chan struct{})
	go st.discoverInBackground()
	return st
}

func (st *store) ready() bool {
	if st.ctx.Err() != nil {
		return false
	}
	select {
	case <-st.readyCh:
		return true
	default:
		return false
	}
}

func (st *store) authURL() string {
	if !st.ready() {
		return ""
	}
	return st.oauth.Endpoint.AuthURL
}

func (st *store) stop() {
	st.cancel()
	<-st.done
}

func (st *store) discoverInBackground() {
	defer close(st.done)
	delay := time.Second
	for st.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(st.ctx, 20*time.Second)
		err := st.discover(ctx)
		cancel()
		if err == nil && st.ctx.Err() == nil {
			close(st.readyCh)
			st.logger().Info("Shopify authentication is ready")
			return
		}
		if st.ctx.Err() != nil {
			return
		}
		st.logger().Warn("Shopify discovery unavailable; retrying", "error", err, "retry_in", delay)
		timer := time.NewTimer(delay)
		select {
		case <-st.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(2*delay, time.Minute)
	}
}

func (st *store) verify(ctx context.Context, code, codeVerifier string) (verifiedCustomer, *oauth2.Token, error) {
	if !st.ready() {
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusServiceUnavailable, "Shopify authentication is temporarily unavailable.", nil)
	}
	ctx = oidc.ClientContext(ctx, st.client)
	token, err := st.oauth.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusBadGateway, "Shopify token exchange failed.", nil)
	}
	raw, _ := token.Extra("id_token").(string)
	id, err := st.verifier.Verify(ctx, raw)
	if err != nil {
		reason := idTokenFailureReason(err)
		st.logger().Warn("Shopify ID token rejected", "reason", reason, "claim_types", idTokenClaimTypes(raw))
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusUnauthorized, "Invalid Shopify ID token ("+reason+").", nil)
	}
	if subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(expectedStatelessOIDCNonce(codeVerifier))) != 1 || id.Subject == "" {
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusUnauthorized, "Invalid Shopify nonce or subject.", nil)
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusUnauthorized, "Invalid access token hash.", nil)
	}
	var c claims
	if id.Claims(&c) != nil {
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusUnauthorized, "Invalid Shopify claims.", nil)
	}
	profile, err := st.fetchCustomer(ctx, token.AccessToken)
	if err != nil {
		return verifiedCustomer{}, nil, router.NewApiError(http.StatusBadGateway, "Unable to read the Shopify customer. Check Customer Account API permissions.", nil)
	}
	return verifiedCustomer{
		issuer: st.issuer, subject: id.Subject, customerID: profile.ID, email: profile.EmailAddress.Email,
		emailVerified: c.EmailVerified && strings.EqualFold(c.Email, profile.EmailAddress.Email),
	}, token, nil
}

type discovery struct {
	Issuer     string   `json:"issuer"`
	AuthURL    string   `json:"authorization_endpoint"`
	TokenURL   string   `json:"token_endpoint"`
	JWKSURL    string   `json:"jwks_uri"`
	Algorithms []string `json:"id_token_signing_alg_values_supported"`
}

type customer struct {
	ID           string `json:"id"`
	EmailAddress *struct {
		Email string `json:"emailAddress"`
	} `json:"emailAddress"`
}

type claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

type storeTransport struct{ origin string }

func (t storeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header = req.Header.Clone()
	r.Header.Set("Origin", t.origin)
	r.Header.Set("User-Agent", "pocketbase-shopify")
	return http.DefaultTransport.RoundTrip(r)
}

func readJSON(client *http.Client, req *http.Request, target any) error {
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream HTTP %d", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(target)
}

func (st *store) discover(ctx context.Context) error {
	var d discovery
	req, _ := http.NewRequestWithContext(ctx, "GET", st.cfg.StoreURL+"/.well-known/openid-configuration", nil)
	if err := readJSON(st.client, req, &d); err != nil {
		return fmt.Errorf("OpenID discovery: %w", err)
	}
	for _, endpoint := range []string{d.Issuer, d.AuthURL, d.TokenURL, d.JWKSURL} {
		if err := checkURL(endpoint, st.cfg.AllowLoopbackHTTP, false); err != nil {
			return fmt.Errorf("discovery endpoint: %w", err)
		}
	}
	var api struct {
		GraphQL string `json:"graphql_api"`
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", st.cfg.StoreURL+"/.well-known/customer-account-api", nil)
	if err := readJSON(st.client, req, &api); err != nil {
		return fmt.Errorf("API discovery: %w", err)
	}
	if err := checkURL(api.GraphQL, st.cfg.AllowLoopbackHTTP, false); err != nil {
		return err
	}
	st.issuer, st.graphql = d.Issuer, api.GraphQL
	st.oauth = oauth2.Config{
		ClientID:    st.cfg.ClientID,
		RedirectURL: st.cfg.RedirectURL,
		Scopes:      []string{"openid", "email", "customer-account-api:full"},
		Endpoint:    oauth2.Endpoint{AuthURL: d.AuthURL, TokenURL: d.TokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
	keyCtx := oidc.ClientContext(st.ctx, st.client)
	st.verifier = oidc.NewVerifier(d.Issuer, shopifySubjectKeySet{oidc.NewRemoteKeySet(keyCtx, d.JWKSURL)}, &oidc.Config{
		ClientID: st.cfg.ClientID, SupportedSigningAlgs: d.Algorithms,
	})
	return nil
}

// Shopify can return a JSON number for sub, contrary to OIDC's string type.
type shopifySubjectKeySet struct{ oidc.KeySet }

func (k shopifySubjectKeySet) VerifySignature(ctx context.Context, raw string) ([]byte, error) {
	payload, err := k.KeySet.VerifySignature(ctx, raw)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, errors.New("Shopify subject payload is invalid JSON")
	}
	sub := bytes.TrimSpace(fields["sub"])
	if len(sub) == 0 || sub[0] == '"' {
		return payload, nil
	}
	if len(sub) > 255 || sub[0] < '1' || sub[0] > '9' {
		return nil, errors.New("Shopify subject must be a string or positive decimal integer")
	}
	for _, digit := range sub {
		if digit < '0' || digit > '9' {
			return nil, errors.New("Shopify subject must be a string or positive decimal integer")
		}
	}
	fields["sub"], _ = json.Marshal(string(sub))
	return json.Marshal(fields)
}

func (st *store) fetchCustomer(ctx context.Context, accessToken string) (*customer, error) {
	body, _ := json.Marshal(map[string]string{"query": `query ShopifyCustomer { customer { id emailAddress { emailAddress } } }`})
	req, _ := http.NewRequestWithContext(ctx, "POST", st.graphql, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Shopify Customer Account API uses the raw token here, without "Bearer".
	req.Header.Set("Authorization", accessToken)
	var result struct {
		Data struct {
			Customer *customer `json:"customer"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := readJSON(st.client, req, &result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 {
		return nil, errors.New("Customer Account API returned GraphQL errors; check customer permissions")
	}
	c := result.Data.Customer
	if c == nil || !strings.HasPrefix(c.ID, "gid://shopify/Customer/") || c.EmailAddress == nil || c.EmailAddress.Email == "" {
		return nil, errors.New("missing customer identity or email")
	}
	return c, nil
}

func idTokenFailureReason(err error) string {
	var expired *oidc.TokenExpiredError
	switch {
	case errors.As(err, &expired):
		return "expired"
	case strings.Contains(err.Error(), "expected audience"):
		return "audience_mismatch"
	case strings.Contains(err.Error(), "different provider"):
		return "issuer_mismatch"
	case strings.Contains(err.Error(), "Shopify subject"):
		return "subject_format_invalid"
	case strings.Contains(err.Error(), "malformed jwt"):
		return "token_format_or_algorithm"
	case strings.HasPrefix(err.Error(), "oidc: failed to unmarshal claims:"):
		return "claims_decode_failed"
	case strings.HasPrefix(err.Error(), "oidc: current time "):
		return "not_yet_valid"
	case strings.HasPrefix(err.Error(), "oidc: failed to obtain source from claim name"), strings.HasPrefix(err.Error(), "oidc: source does not exist"):
		return "distributed_claims_invalid"
	case strings.HasPrefix(err.Error(), "oidc: invalid configuration"):
		return "verifier_configuration_invalid"
	case strings.HasPrefix(err.Error(), "oidc: id token not signed"), strings.HasPrefix(err.Error(), "oidc: multiple signatures"):
		return "token_signature_format"
	case strings.Contains(err.Error(), "context canceled"):
		return "verification_cancelled"
	case strings.Contains(err.Error(), "context deadline exceeded"):
		return "verification_timeout"
	case strings.Contains(err.Error(), "get keys failed"):
		return "jwks_fetch_failed"
	case strings.Contains(err.Error(), "failed to decode keys"):
		return "jwks_decode_failed"
	case strings.Contains(err.Error(), "failed to verify id token signature"):
		return "signature_mismatch"
	case strings.Contains(err.Error(), "fetching keys"):
		return "jwks_read_failed"
	default:
		return "verification_failed"
	}
}

func idTokenClaimTypes(raw string) map[string]string {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || len(parts[1]) > 64*1024 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(payload, &values) != nil {
		return nil
	}
	types := map[string]string{}
	for _, field := range []string{"iss", "sub", "aud", "exp", "iat", "nbf", "nonce", "at_hash", "_claim_names", "_claim_sources"} {
		value := bytes.TrimSpace(values[field])
		if len(value) == 0 {
			continue
		}
		kind := "number"
		switch value[0] {
		case '"':
			kind = "string"
		case '[':
			kind = "array"
		case '{':
			kind = "object"
		case 't', 'f':
			kind = "boolean"
		case 'n':
			kind = "null"
		}
		types[field] = kind
	}
	return types
}
