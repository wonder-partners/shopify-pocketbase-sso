package shopifysso

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestShopifyAuthMethodsApplyFieldsAfterEndpointRewrite(t *testing.T) {
	for _, state := range []string{"ready", "pending"} {
		for _, format := range []string{"modern", "legacy"} {
			t.Run(state+"/"+format, func(t *testing.T) {
				var f *fixture
				if state == "ready" {
					f = setup(t, "")
				} else {
					f = setupWithShopHandler(t, "", "users", false, func(http.Handler) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
						})
					})
				}
				prefix, urlField := "oauth2.providers", "authURL"
				if format == "legacy" {
					prefix, urlField = "authProviders", "authUrl"
				}
				fields := prefix + "." + urlField + "," + prefix + ".state," + prefix + ".codeVerifier"
				result := f.request("GET", f.api.URL+"/api/collections/users/auth-methods?fields="+url.QueryEscape(fields), "", "", "")
				f.expect(result, http.StatusOK)
				var body map[string]any
				if err := json.Unmarshal([]byte(result.body), &body); err != nil {
					t.Fatal(err)
				}
				if len(body) != 1 {
					t.Fatal("field projection returned unrequested top-level fields", result.body)
				}
				var providers []any
				if format == "modern" {
					oauth, ok := body["oauth2"].(map[string]any)
					if !ok || len(oauth) != 1 {
						t.Fatal("missing projected OAuth response", result.body)
					}
					providers, _ = oauth["providers"].([]any)
				} else {
					providers, _ = body["authProviders"].([]any)
				}
				if state == "pending" {
					if len(providers) != 0 {
						t.Fatal("field projection advertised Shopify before discovery", result.body)
					}
					return
				}
				if len(providers) != 1 {
					t.Fatal("missing projected Shopify provider", result.body)
				}
				p, ok := providers[0].(map[string]any)
				if !ok || len(p) != 3 {
					t.Fatal("provider contains unrequested fields", result.body)
				}
				authURL, _ := p[urlField].(string)
				u, err := url.Parse(authURL + url.QueryEscape(f.s.cfg.RedirectURL))
				if err != nil {
					t.Fatal(err)
				}
				verifier, _ := p["codeVerifier"].(string)
				if !strings.HasPrefix(authURL, f.s.oauth.Endpoint.AuthURL+"?") || u.Query().Get("state") != p["state"] || verifier == "" || u.Query().Get("redirect_uri") != f.s.cfg.RedirectURL {
					t.Fatal("projected authorization URL lost its endpoint or native parameters")
				}
			})
		}
	}
}

func TestShopifyAuthMethodsKeepUnrelatedFieldProjection(t *testing.T) {
	f := setup(t, "")
	result := f.request("GET", f.api.URL+"/api/collections/users/auth-methods?fields=password.enabled", "", "", "")
	f.expect(result, http.StatusOK)
	if result.body != `{"password":{"enabled":true}}` {
		t.Fatal("auth methods ignored the requested fields", result.body)
	}
}
