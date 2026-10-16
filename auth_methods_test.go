package shopifysso

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
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

func TestShopifyAuthMethodsRewriteBeforeNativeGzip(t *testing.T) {
	for _, order := range []string{"gzip_before_plugin", "gzip_after_plugin"} {
		for _, mode := range []string{"full", "fields", "pending", "below_threshold"} {
			t.Run(order+"/"+mode, func(t *testing.T) {
				var f *fixture
				if mode == "pending" {
					f = setupWithShopHandler(t, "", "users", false, func(http.Handler) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
						})
					})
				} else {
					f = setup(t, "")
				}
				r, err := apis.NewRouter(f.app)
				if err != nil {
					t.Fatal(err)
				}
				compression := apis.Gzip()
				if mode == "below_threshold" {
					compression = apis.GzipWithConfig(apis.GzipConfig{MinLength: 10000})
				}
				if order == "gzip_before_plugin" {
					r.Bind(compression)
				}
				f.s.Register(r)
				if order == "gzip_after_plugin" {
					r.Bind(compression)
				}
				mux, err := r.BuildMux()
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(mux)
				t.Cleanup(server.Close)
				target := server.URL + "/api/collections/users/auth-methods"
				if mode == "fields" {
					target += "?fields=" + url.QueryEscape("oauth2.providers.authURL,oauth2.providers.state,oauth2.providers.codeVerifier")
				}
				req, err := http.NewRequest("GET", target, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Accept-Encoding", "gzip")
				res, err := f.client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				if res.StatusCode != http.StatusOK {
					t.Fatal("native gzip broke auth methods", res.StatusCode)
				}
				if res.Header.Get("Cache-Control") != "no-store" {
					t.Fatal("gzip response became cacheable")
				}
				var reader io.Reader = res.Body
				if mode == "below_threshold" {
					if res.Header.Get("Content-Encoding") != "" {
						t.Fatal("small response was unexpectedly compressed")
					}
				} else {
					if res.Header.Get("Content-Encoding") != "gzip" {
						t.Fatal("response lost native compression")
					}
					compressed, err := gzip.NewReader(res.Body)
					if err != nil {
						t.Fatal(err)
					}
					defer compressed.Close()
					reader = compressed
				}
				body, err := io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				var methods struct {
					OAuth2 struct{ Providers []browserFlow }
				}
				if err := json.Unmarshal(body, &methods); err != nil {
					t.Fatal("gzip response contained invalid JSON", err)
				}
				if mode == "pending" {
					if len(methods.OAuth2.Providers) != 0 {
						t.Fatal("gzip response advertised unavailable Shopify")
					}
				} else {
					if len(methods.OAuth2.Providers) != 1 {
						t.Fatal("gzip response lost the Shopify provider")
					}
					p := methods.OAuth2.Providers[0]
					if !strings.HasPrefix(p.AuthURL, f.s.oauth.Endpoint.AuthURL+"?") || p.State == "" || p.CodeVerifier == "" {
						t.Fatal("gzip response lost the rewritten URL or native parameters")
					}
					if mode == "fields" && p.Name != "" {
						t.Fatal("gzip response ignored field projection")
					}
				}
			})
		}
	}
}
