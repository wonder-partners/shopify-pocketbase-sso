package studio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	ShopURL           string
	StudioURL         string
	ClientID          string
	ClientSecret      string // Optional: leave empty for a public client with PKCE.
	AuthCollection    string // Defaults to the application's users auth collection.
	AllowLoopbackHTTP bool   // Only for the local mock and automated tests.
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
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	EmailAddress *struct {
		Email string `json:"emailAddress"`
	} `json:"emailAddress"`
}

type claims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
}

// The trusted shop URL is operator configuration, never a request parameter.
func checkURL(raw string, allowHTTP, originOnly bool) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid configured URL")
	}
	if originOnly && u.Path != "" {
		return errors.New("configure an origin without a path or trailing slash")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if allowHTTP && u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("HTTPS required; HTTP is allowed only on explicit loopback development URLs")
}

type shopTransport struct{ origin string }

func (t shopTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header = req.Header.Clone()
	r.Header.Set("Origin", t.origin)
	r.Header.Set("User-Agent", "studio-pocketbase-sso/0.1")
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

func (s *Service) discover(ctx context.Context) error {
	var d discovery
	req, _ := http.NewRequestWithContext(ctx, "GET", s.cfg.ShopURL+"/.well-known/openid-configuration", nil)
	if err := readJSON(s.client, req, &d); err != nil {
		return fmt.Errorf("OpenID discovery: %w", err)
	}
	for _, endpoint := range []string{d.Issuer, d.AuthURL, d.TokenURL, d.JWKSURL} {
		if err := checkURL(endpoint, s.cfg.AllowLoopbackHTTP, false); err != nil {
			return fmt.Errorf("discovery endpoint: %w", err)
		}
	}
	var api struct {
		GraphQL string `json:"graphql_api"`
	}
	req, _ = http.NewRequestWithContext(ctx, "GET", s.cfg.ShopURL+"/.well-known/customer-account-api", nil)
	if err := readJSON(s.client, req, &api); err != nil {
		return fmt.Errorf("API discovery: %w", err)
	}
	if err := checkURL(api.GraphQL, s.cfg.AllowLoopbackHTTP, false); err != nil {
		return err
	}
	s.issuer, s.graphql = d.Issuer, api.GraphQL
	style := oauth2.AuthStyleInParams
	if s.cfg.ClientSecret != "" {
		style = oauth2.AuthStyleInHeader
	}
	s.oauth = oauth2.Config{
		ClientID: s.cfg.ClientID, ClientSecret: s.cfg.ClientSecret,
		RedirectURL: s.cfg.StudioURL + "/studio/callback",
		Scopes:      []string{"openid", "email", "customer-account-api:full"},
		Endpoint:    oauth2.Endpoint{AuthURL: d.AuthURL, TokenURL: d.TokenURL, AuthStyle: style},
	}
	keyCtx := oidc.ClientContext(context.Background(), s.client)
	s.verifier = oidc.NewVerifier(d.Issuer, shopifySubjectKeySet{oidc.NewRemoteKeySet(keyCtx, d.JWKSURL)}, &oidc.Config{
		ClientID: s.cfg.ClientID, SupportedSigningAlgs: d.Algorithms,
	})
	return nil
}

// Shopify can return a JSON number for sub, contrary to OIDC's string type.
// This adapter is only used INSIDE IDTokenVerifier: it checks the allowed JWT
// algorithm, then calls this method to verify the ORIGINAL signed token.
// Only after signature verification do we normalize the verified sub for its
// claims decoder. Issuer, audience, expiry, nonce and at_hash checks stay active.
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
		return payload, nil // Keep standard string subjects unchanged.
	}
	if len(sub) > 255 || sub[0] < '1' || sub[0] > '9' {
		return nil, errors.New("Shopify subject must be a string or positive decimal integer")
	}
	for _, digit := range sub {
		if digit < '0' || digit > '9' {
			return nil, errors.New("Shopify subject must be a string or positive decimal integer")
		}
	}
	// Use the exact decimal bytes, never float64, to preserve large Shopify IDs.
	fields["sub"], _ = json.Marshal(string(sub))
	return json.Marshal(fields)
}

func (s *Service) fetchCustomer(ctx context.Context, accessToken string) (*customer, error) {
	body, _ := json.Marshal(map[string]string{"query": `query StudioCustomer { customer { id firstName lastName emailAddress { emailAddress } } }`})
	req, _ := http.NewRequestWithContext(ctx, "POST", s.graphql, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Shopify Customer Account API uses the raw token here, without "Bearer".
	req.Header.Set("Authorization", accessToken)
	var result struct {
		Data struct {
			Customer *customer `json:"customer"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := readJSON(s.client, req, &result); err != nil {
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

// Return only fixed labels: verifier errors can contain claims or upstream bodies.
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

// Diagnostic only. Report JSON types of known OIDC fields, never their values.
// This unverified payload must never be used to authenticate or create a session.
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
