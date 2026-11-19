package shopifysso

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/wonder-partners/shopify-pocketbase-sso/internal/mockstore"
	"golang.org/x/oauth2"
)

const testCallback = "http://127.0.0.1/oauth/callback"

// A store talking to mockstore over HTTP, without PocketBase.
type storeFixture struct {
	t      *testing.T
	store  *store
	mock   *mockstore.Mock
	server *httptest.Server
	logs   *syncBuffer
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func newStoreFixture(t *testing.T, fault string, wrap func(http.Handler) http.Handler) *storeFixture {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	mock, err := mockstore.New("http://"+server.Listener.Addr().String(), testCallback, fault)
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = mock
	if wrap != nil {
		server.Config.Handler = wrap(mock)
	}
	server.Start()
	t.Cleanup(server.Close)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	cfg := Config{StoreURL: server.URL, RedirectURL: testCallback, ClientID: mockstore.ClientID, AllowLoopbackHTTP: true}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st := startStore(ctx, cfg, func() *slog.Logger { return logger })
	t.Cleanup(st.stop)
	return &storeFixture{t: t, store: st, mock: mock, server: server, logs: logs}
}

func waitUntil(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (f *storeFixture) waitReady() {
	f.t.Helper()
	waitUntil(f.t, "discovery", f.store.ready)
}

// code signs in to mockstore and returns an authorization code bound to verifier.
func (f *storeFixture) code(verifier string) string {
	f.t.Helper()
	challenge := oauth2.S256ChallengeFromVerifier(verifier)
	q := url.Values{
		"client_id": {mockstore.ClientID}, "redirect_uri": {testCallback}, "response_type": {"code"},
		"code_challenge_method": {"S256"}, "code_challenge": {challenge}, "nonce": {challenge}, "state": {"state"},
	}
	req, _ := http.NewRequest(http.MethodGet, f.store.authURL()+"?"+q.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: "mock_store_session", Value: "yes"})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	res.Body.Close()
	location, err := url.Parse(res.Header.Get("Location"))
	if err != nil || location.Query().Get("code") == "" {
		f.t.Fatalf("mockstore issued no code: %d %v", res.StatusCode, err)
	}
	return location.Query().Get("code")
}

func TestStoreVerifiesALogin(t *testing.T) {
	for _, c := range []struct {
		fault, subject string
		emailVerified  bool
	}{
		{"", "shopify-customer-sub-12345", true},
		// The ID token's email differs from the customer profile's.
		{"email", "shopify-customer-sub-12345", false},
		// Shopify may send sub as a JSON number; keep its exact digits.
		{"subject_number", "12345", true},
		{"numeric:", "900719925474099312345", true},
	} {
		t.Run(c.fault, func(t *testing.T) {
			f := newStoreFixture(t, c.fault, nil)
			f.waitReady()
			if f.store.authURL() != f.server.URL+"/authorize" {
				t.Fatal("unexpected authorization URL", f.store.authURL())
			}
			verifier := oauth2.GenerateVerifier()
			customer, token, err := f.store.verify(context.Background(), f.code(verifier), verifier)
			if err != nil {
				t.Fatal(err)
			}
			want := verifiedCustomer{
				issuer: f.server.URL + "/identity", subject: c.subject, customerID: "gid://shopify/Customer/12345",
				email: mockstore.Email, emailVerified: c.emailVerified,
			}
			if customer != want || token == nil || token.AccessToken == "" {
				t.Fatalf("customer = %+v, want %+v", customer, want)
			}
		})
	}
}

func TestStoreRejectsInvalidLogins(t *testing.T) {
	for _, c := range []struct {
		fault   string
		status  int
		message string
	}{
		{"nonce", 401, "Invalid Shopify nonce or subject."},
		{"nonce_missing", 401, "Invalid Shopify nonce or subject."},
		{"at_hash", 401, "Invalid access token hash."},
		{"audience", 401, "Invalid Shopify ID token (audience_mismatch)."},
		{"issuer", 401, "Invalid Shopify ID token (issuer_mismatch)."},
		{"expired", 401, "Invalid Shopify ID token (expired)."},
		{"not_before", 401, "Invalid Shopify ID token (not_yet_valid)."},
		{"signature", 401, "Invalid Shopify ID token (signature_mismatch)."},
		{"algorithm", 401, "Invalid Shopify ID token (token_format_or_algorithm)."},
		{"audience_number", 401, "Invalid Shopify ID token (claims_decode_failed)."},
		// A numeric subject must not bypass any other check.
		{"numeric:signature", 401, "Invalid Shopify ID token (signature_mismatch)."},
		{"numeric:algorithm", 401, "Invalid Shopify ID token (token_format_or_algorithm)."},
		{"numeric:issuer", 401, "Invalid Shopify ID token (issuer_mismatch)."},
		{"numeric:audience", 401, "Invalid Shopify ID token (audience_mismatch)."},
		{"numeric:expired", 401, "Invalid Shopify ID token (expired)."},
		{"numeric:not_before", 401, "Invalid Shopify ID token (not_yet_valid)."},
		{"numeric:nonce", 401, "Invalid Shopify nonce or subject."},
		{"numeric:nonce_missing", 401, "Invalid Shopify nonce or subject."},
		{"numeric:at_hash", 401, "Invalid access token hash."},
		{"code_expired", 502, "Shopify token exchange failed."},
		{"graphql", 502, "Unable to read the Shopify customer. Check Customer Account API permissions."},
	} {
		t.Run(c.fault, func(t *testing.T) {
			f := newStoreFixture(t, c.fault, nil)
			f.waitReady()
			verifier := oauth2.GenerateVerifier()
			_, token, err := f.store.verify(context.Background(), f.code(verifier), verifier)
			var apiErr *router.ApiError
			if token != nil || !errors.As(err, &apiErr) || apiErr.Status != c.status || apiErr.Message != c.message {
				t.Fatalf("error = %v, want HTTP %d %q", err, c.status, c.message)
			}
			// Rejections are logged with safe labels only, never token values.
			if c.status == 401 && strings.Contains(c.message, "ID token") &&
				(!strings.Contains(f.logs.String(), "Shopify ID token rejected") || strings.Contains(f.logs.String(), mockstore.Email)) {
				t.Fatal("ID token rejection was not logged safely:", f.logs.String())
			}
		})
	}
	t.Run("wrong PKCE verifier", func(t *testing.T) {
		f := newStoreFixture(t, "", nil)
		f.waitReady()
		code := f.code(oauth2.GenerateVerifier())
		if _, _, err := f.store.verify(context.Background(), code, oauth2.GenerateVerifier()); err == nil {
			t.Fatal("a code was accepted with another PKCE verifier")
		}
	})
	t.Run("code used twice", func(t *testing.T) {
		f := newStoreFixture(t, "", nil)
		f.waitReady()
		verifier := oauth2.GenerateVerifier()
		code := f.code(verifier)
		if _, _, err := f.store.verify(context.Background(), code, verifier); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.store.verify(context.Background(), code, verifier); err == nil {
			t.Fatal("a code was accepted twice")
		}
	})
}

func TestStoreDiscoveryRetriesAndRecovers(t *testing.T) {
	for _, failing := range []string{"/.well-known/openid-configuration", "/.well-known/customer-account-api"} {
		t.Run(failing, func(t *testing.T) {
			var healthy atomic.Bool
			var attempts atomic.Int32
			f := newStoreFixture(t, "", func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == failing && !healthy.Load() {
						attempts.Add(1)
						http.Error(w, "temporary outage", http.StatusServiceUnavailable)
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			waitUntil(t, "a failed discovery attempt", func() bool { return attempts.Load() > 0 })
			// Nothing from a partial or failed discovery is published.
			if f.store.ready() || f.store.authURL() != "" {
				t.Fatal("a failed discovery was published")
			}
			if _, _, err := f.store.verify(context.Background(), "code", oauth2.GenerateVerifier()); err == nil {
				t.Fatal("a store that is not ready verified a login")
			}
			waitUntil(t, "a retry warning", func() bool { return strings.Contains(f.logs.String(), "Shopify discovery unavailable; retrying") })
			healthy.Store(true)
			f.waitReady()
			waitUntil(t, "the recovery log", func() bool { return strings.Contains(f.logs.String(), "Shopify authentication is ready") })
		})
	}
}

func TestStoreDiscoveryRejectsUnsafeEndpoints(t *testing.T) {
	for _, c := range []struct{ path, body string }{
		{"/.well-known/openid-configuration", `{"issuer":"%[1]s/identity","authorization_endpoint":"%[1]s/authorize","token_endpoint":"http://store.example/token","jwks_uri":"%[1]s/jwks","id_token_signing_alg_values_supported":["RS256"]}`},
		{"/.well-known/customer-account-api", `{"graphql_api":"http://store.example/graphql"}`},
	} {
		t.Run(c.path, func(t *testing.T) {
			f := newStoreFixture(t, "", func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == c.path {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, c.body, "http://"+r.Host)
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			waitUntil(t, "a rejected discovery", func() bool { return strings.Contains(f.logs.String(), "HTTPS required") })
			if f.store.ready() {
				t.Fatal("a store advertising plain HTTP endpoints became ready")
			}
		})
	}
}

func TestStoreStopCancelsDiscovery(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	f := newStoreFixture(t, "", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(canceled)
		})
	})
	<-started
	stopped := make(chan struct{})
	go func() { f.store.stop(); close(stopped) }()
	for _, c := range []struct {
		name string
		ch   chan struct{}
	}{{"HTTP request cancellation", canceled}, {"stop", stopped}} {
		select {
		case <-c.ch:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for " + c.name)
		}
	}
	if f.store.ready() {
		t.Fatal("a stopped store is ready")
	}
	f.store.stop() // stopping twice is harmless
}

func TestStoreStopsBeingReadyWhenStopped(t *testing.T) {
	f := newStoreFixture(t, "", nil)
	f.waitReady()
	f.store.stop()
	if f.store.ready() || f.store.authURL() != "" {
		t.Fatal("a stopped store still accepts logins")
	}
}
