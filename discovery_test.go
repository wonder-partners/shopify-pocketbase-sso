package shopifysso

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

func TestDiscoveryFailureKeepsPocketBaseAvailableAndRecovers(t *testing.T) {
	var healthy atomic.Bool
	attempted := make(chan struct{}, 10)
	f := setupWithShopHandler(t, "", "users", false, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/.well-known/") {
				select {
				case attempted <- struct{}{}:
				default:
				}
				if !healthy.Load() {
					http.Error(w, "temporary outage", http.StatusServiceUnavailable)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("background discovery did not start")
	}
	if f.s.isReady() {
		t.Fatal("Shopify became ready during a discovery outage")
	}
	c, _ := f.app.FindCollectionByNameOrId("users")
	// A persisted endpoint from an earlier run must never be advertised as ready.
	c.OAuth2.Providers[0].AuthURL = "https://old.example/authorize"
	c.OAuth2.Providers = append(c.OAuth2.Providers, core.OAuth2ProviderConfig{Name: "google", ClientId: "google-client", ClientSecret: "google-secret"})
	c.OTP.Enabled = true
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		methods := f.request("GET", f.api.URL+"/api/collections/users/auth-methods", "", "", "")
		f.expect(methods, http.StatusOK)
		if methods.headers.Get("Cache-Control") != "no-store" {
			t.Fatal("pending auth methods response was cacheable")
		}
		var result struct {
			Password struct{ Enabled bool }            `json:"password"`
			OTP      struct{ Enabled bool }            `json:"otp"`
			OAuth2   struct{ Providers []browserFlow } `json:"oauth2"`
			Legacy   []browserFlow                     `json:"authProviders"`
		}
		if json.Unmarshal([]byte(methods.body), &result) != nil || !result.Password.Enabled || !result.OTP.Enabled {
			t.Fatal("discovery outage hid native authentication methods")
		}
		for _, providers := range [][]browserFlow{result.OAuth2.Providers, result.Legacy} {
			if len(providers) != 1 || providers[0].Name != "google" || providers[0].AuthURL == "" {
				t.Fatal("discovery outage advertised Shopify or hid another OAuth provider")
			}
		}
	}
	native := nativeSignup(f, "native@example.test")
	f.expect(f.request("GET", f.api.URL+"/native/whoami", "", "", native.Token), http.StatusOK)
	login := f.request("POST", f.api.URL+"/api/collections/users/auth-with-oauth2", f.api.URL, `{"provider":"shopify","code":"pending"}`, "")
	f.expect(login, http.StatusServiceUnavailable)
	if login.headers.Get("Retry-After") == "" || login.headers.Get("Cache-Control") != "no-store" {
		t.Fatal("unavailable Shopify response omitted retry or cache headers")
	}
	if f.s.oauth.Endpoint.AuthURL != "" || f.s.verifier != nil {
		t.Fatal("failed discovery published partially initialized endpoints")
	}
	healthy.Store(true)
	waitForDiscovery(t, f.s)
	if !f.s.isReady() {
		t.Fatal("Shopify did not recover")
	}
	f.login()
	c, _ = f.app.FindCollectionByNameOrId("users")
	if len(c.OAuth2.Providers) != 2 || !c.OTP.Enabled {
		t.Fatal("recovery changed unrelated authentication settings")
	}
}

func TestDiscoveryDoesNotPublishPartialResults(t *testing.T) {
	var healthy atomic.Bool
	apiRequested := make(chan struct{}, 1)
	f := setupWithShopHandler(t, "", "members", true, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/customer-account-api" && !healthy.Load() {
				select {
				case apiRequested <- struct{}{}:
				default:
				}
				http.Error(w, "temporary API discovery outage", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	select {
	case <-apiRequested:
	case <-time.After(time.Second):
		t.Fatal("Customer Account API discovery did not start")
	}
	if f.s.isReady() || f.s.issuer != "" || f.s.graphql != "" || f.s.verifier != nil {
		t.Fatal("partial discovery became available")
	}
	c, _ := f.app.FindCollectionByNameOrId(f.s.collectionID)
	c.Name = "renamedMembers"
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	healthy.Store(true)
	waitForDiscovery(t, f.s)
	f.login()
	_, user := f.authRecord()
	if user.Collection().Name != "renamedMembers" {
		t.Fatal("discovery recovery lost the renamed collection")
	}
}

func TestTerminationCancelsInFlightDiscovery(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	f := setupWithShopHandler(t, "", "users", false, func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(canceled)
		})
	})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("discovery did not start")
	}
	terminated := make(chan error, 1)
	go func() { terminated <- f.app.OnTerminate().Trigger(&core.TerminateEvent{App: f.app}) }()
	select {
	case err := <-terminated:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("termination did not stop discovery promptly")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("termination did not cancel the HTTP request")
	}
	if f.s.isReady() {
		t.Fatal("terminated service advertised Shopify as ready")
	}
}

func TestLocalSetupErrorsFailWithoutDiscovery(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	for _, collection := range []string{"missing", "_superusers"} {
		_, err := New(context.Background(), app, Config{
			ShopURL: "http://127.0.0.1:1", RedirectURL: "https://frontend.example/callback",
			ClientID: "public-client", AuthCollection: collection, AllowLoopbackHTTP: true,
		})
		if err == nil || strings.Contains(err.Error(), "discovery") {
			t.Fatalf("invalid local collection %s did not fail locally", collection)
		}
	}
}
