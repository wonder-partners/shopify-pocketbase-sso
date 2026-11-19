package shopifysso

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestDiscoveryFailureKeepsPocketBaseAvailableAndRecovers(t *testing.T) {
	var healthy atomic.Bool
	attempted := make(chan struct{}, 10)
	f := setupWithStoreHandler(t, "", "users", false, func(next http.Handler) http.Handler {
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
	if f.s.store.ready() {
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
	if f.s.store.authURL() != "" {
		t.Fatal("failed discovery published partially initialized endpoints")
	}
	healthy.Store(true)
	waitForDiscovery(t, f.s)
	if !f.s.store.ready() {
		t.Fatal("Shopify did not recover")
	}
	f.login()
	c, _ = f.app.FindCollectionByNameOrId("users")
	if len(c.OAuth2.Providers) != 2 || !c.OTP.Enabled {
		t.Fatal("recovery changed unrelated authentication settings")
	}
}

func TestTerminationCancelsInFlightDiscovery(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	f := setupWithStoreHandler(t, "", "users", false, func(http.Handler) http.Handler {
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
	if f.s.store.ready() {
		t.Fatal("terminated service advertised Shopify as ready")
	}
}

func TestDiscoveryAndTerminationDoNotWaitForDatabaseWriter(t *testing.T) {
	for _, action := range []string{"ready", "terminate"} {
		t.Run(action, func(t *testing.T) {
			apiStarted, allowAPI := make(chan struct{}), make(chan struct{})
			f := setupWithStoreHandler(t, "", "users", false, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/.well-known/customer-account-api" {
						close(apiStarted)
						select {
						case <-allowAPI:
						case <-r.Context().Done():
							return
						}
					}
					next.ServeHTTP(w, r)
				})
			})
			t.Cleanup(func() {
				select {
				case <-allowAPI:
				default:
					close(allowAPI)
				}
			})
			select {
			case <-apiStarted:
			case <-time.After(time.Second):
				t.Fatal("API discovery did not start")
			}
			var saves atomic.Int32
			f.app.OnCollectionUpdate(f.s.collectionID).BindFunc(func(e *core.CollectionEvent) error {
				saves.Add(1)
				return e.Next()
			})
			writer := f.app.NonconcurrentDB().(*dbx.DB).DB()
			var terminated chan error
			err := f.app.RunInTransaction(func(core.App) error {
				waits := writer.Stats().WaitCount
				close(allowAPI)
				if action == "ready" {
					for deadline := time.Now().Add(time.Second); !f.s.store.ready(); time.Sleep(time.Millisecond) {
						if time.Now().After(deadline) {
							return errors.New("discovery waited for the occupied database writer")
						}
					}
					return nil
				}
				// Wait until discovery has returned from HTTP and either finished
				// or reached the old, non-cancellable transaction acquisition.
				deadline := time.NewTimer(time.Second)
				defer deadline.Stop()
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for writer.Stats().WaitCount == waits && !f.s.store.ready() {
					select {
					case <-tick.C:
					case <-deadline.C:
						return errors.New("discovery did not finish its HTTP phase")
					}
				}
				terminated = make(chan error, 1)
				go func() { terminated <- f.app.OnTerminate().Trigger(&core.TerminateEvent{App: f.app}) }()
				select {
				case err := <-terminated:
					terminated = nil
					return err
				case <-time.After(time.Second):
					return errors.New("termination waited for the occupied database writer")
				}
			})
			if terminated != nil {
				select {
				case <-terminated:
				case <-time.After(2 * time.Second):
					t.Fatal("termination did not finish after writer release")
				}
			}
			stopped := make(chan struct{})
			go func() { f.s.store.stop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("discovery did not finish after writer release")
			}
			if err != nil {
				t.Fatal(err)
			}
			if saves.Load() != 0 {
				t.Fatal("discovery wrote the provider configuration again")
			}
		})
	}
}

func TestAuthMethodsUseDiscoveredEndpointWithNativePKCE(t *testing.T) {
	f := setup(t, "")
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.OAuth2.Providers[0].AuthURL = "https://stale.example/authorize"
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	result := f.request("GET", f.api.URL+"/api/collections/users/auth-methods", "", "", "")
	f.expect(result, http.StatusOK)
	type method struct {
		Name, State, CodeVerifier, CodeChallenge, CodeChallengeMethod string
		AuthURL                                                       string `json:"authURL"`
		AuthUrl                                                       string `json:"authUrl"`
	}
	var methods struct {
		OAuth2 struct {
			Providers []method
		}
		AuthProviders []method
	}
	if err := json.Unmarshal([]byte(result.body), &methods); err != nil {
		t.Fatal(err)
	}
	for _, providers := range [][]method{methods.OAuth2.Providers, methods.AuthProviders} {
		if len(providers) != 1 {
			t.Fatal("missing Shopify provider")
		}
		p := providers[0]
		u, err := url.Parse(p.AuthURL + url.QueryEscape(f.s.cfg.RedirectURL))
		if err != nil {
			t.Fatal(err)
		}
		if u.Scheme+"://"+u.Host+u.Path != f.s.store.authURL() || p.AuthUrl != p.AuthURL {
			t.Fatal("auth methods advertised a stale endpoint")
		}
		q := u.Query()
		if q.Get("state") != p.State || q.Get("code_challenge") != p.CodeChallenge || q.Get("code_challenge_method") != "S256" || q.Get("nonce") != p.CodeChallenge || q.Get("redirect_uri") != f.s.cfg.RedirectURL || p.CodeVerifier == "" {
			t.Fatal("rewritten endpoint changed native OAuth parameters")
		}
	}
	f.login()
}

func TestLocalSetupErrorsFailWithoutDiscovery(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	for _, collection := range []string{"missing", "_superusers"} {
		_, err := New(context.Background(), app, Config{
			StoreURL: "http://127.0.0.1:1", RedirectURL: "https://frontend.example/callback",
			ClientID: "public-client", AuthCollection: collection, AllowLoopbackHTTP: true,
		})
		if err == nil || strings.Contains(err.Error(), "discovery") {
			t.Fatalf("invalid local collection %s did not fail locally", collection)
		}
	}
}

func TestProviderConfigurationPreservesConcurrentCollectionChanges(t *testing.T) {
	f := setup(t, "")
	writer := f.app.NonconcurrentDB().(*dbx.DB).DB()
	configured := make(chan error, 1)
	var started bool
	err := f.app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(f.s.collectionID)
		if err != nil {
			return err
		}
		c.Name = "renamedUsers"
		c.MFA.Enabled, c.OTP.Enabled = true, true
		c.PasswordAuth.Enabled = false
		c.AuthRule = types.Pointer("verified = true")
		c.OAuth2.Providers = append(c.OAuth2.Providers, core.OAuth2ProviderConfig{
			Name: "google", ClientId: "host-client", ClientSecret: "host-secret",
		})
		if err := tx.Save(c); err != nil {
			return err
		}
		waits := writer.Stats().WaitCount
		started = true
		go func() {
			_, err := saveProviderConfig(f.app, f.s.collectionID, f.s.cfg.ClientID)
			configured <- err
		}()
		// Wait until configuration needs the occupied writer connection.
		// A read outside its transaction would already have loaded stale data.
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for writer.Stats().WaitCount == waits {
			select {
			case <-tick.C:
			case <-deadline.C:
				return errors.New("provider configuration did not wait for the host transaction")
			}
		}
		return nil // Commit the host changes before configuration continues.
	})
	if started {
		select {
		case configureErr := <-configured:
			if configureErr != nil {
				t.Fatal(configureErr)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("provider configuration did not finish")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.app.FindCollectionByNameOrId(f.s.collectionID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "renamedUsers" || !c.MFA.Enabled || !c.OTP.Enabled || c.PasswordAuth.Enabled || c.AuthRule == nil || *c.AuthRule != "verified = true" {
		t.Fatal("provider configuration overwrote concurrent host authentication settings")
	}
	google, ok := c.OAuth2.GetProviderConfig("google")
	if !ok || google.ClientId != "host-client" || google.ClientSecret != "host-secret" {
		t.Fatal("provider configuration overwrote another OAuth provider")
	}
	shopify, ok := c.OAuth2.GetProviderConfig(providerName)
	if !ok || shopify.ClientId != f.s.cfg.ClientID || shopify.AuthURL != "" || shopify.TokenURL != "" {
		t.Fatal("Shopify provider configuration was not saved")
	}
}

func TestProviderConfigurationRollsBackOnSaveHookFailure(t *testing.T) {
	f := setup(t, "")
	before, err := f.app.FindCollectionByNameOrId(f.s.collectionID)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := before.OAuth2.GetProviderConfig(providerName)
	f.s.cfg.ClientID = "replacement-client"
	fail := true
	f.app.OnCollectionUpdate(f.s.collectionID).BindFunc(func(e *core.CollectionEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if fail {
			return errors.New("host rejected the provider update")
		}
		return nil
	})
	if _, err := saveProviderConfig(f.app, f.s.collectionID, f.s.cfg.ClientID); err == nil {
		t.Fatal("provider configuration ignored the host save hook failure")
	}
	after, err := f.app.FindCollectionByNameOrId(f.s.collectionID)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := after.OAuth2.GetProviderConfig(providerName)
	if !ok || provider.ClientId != original.ClientId {
		t.Fatal("failed provider configuration persisted a partial update")
	}
	fail = false
	if _, err := saveProviderConfig(f.app, f.s.collectionID, f.s.cfg.ClientID); err != nil {
		t.Fatal("provider configuration did not recover on retry", err)
	}
	after, err = f.app.FindCollectionByNameOrId(f.s.collectionID)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok = after.OAuth2.GetProviderConfig(providerName)
	if !ok || provider.ClientId != f.s.cfg.ClientID {
		t.Fatal("provider configuration retry did not persist its update")
	}
}
