package studio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
	"golang.org/x/oauth2"
	"studio-sso/internal/mockshop"
)

type fixture struct {
	t            *testing.T
	studio, shop *httptest.Server
	app          *pocketbase.PocketBase
	s            *Service
	client       *http.Client
}

func setup(t *testing.T, secret, fault string) *fixture {
	t.Helper()
	f := &fixture{t: t, studio: httptest.NewUnstartedServer(nil), shop: httptest.NewUnstartedServer(nil)}
	studioURL := "http://" + f.studio.Listener.Addr().String()
	shopURL := "http://" + f.shop.Listener.Addr().String()
	m, err := mockshop.New(shopURL, studioURL+"/studio/callback", secret, fault)
	if err != nil {
		t.Fatal(err)
	}
	f.shop.Config.Handler = m
	f.shop.Start()
	t.Cleanup(f.shop.Close)
	f.app = pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := f.app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	f.app.Settings().Logs.MaxDays = 0
	t.Cleanup(func() {
		if err := f.app.ResetBootstrapState(); err != nil {
			t.Error(err)
		}
	})
	f.s, err = New(context.Background(), f.app, Config{ShopURL: shopURL, StudioURL: studioURL, ClientID: mockshop.ClientID, ClientSecret: secret, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatal(err)
	}
	f.s.Register(r)
	r.GET("/native/whoami", func(e *core.RequestEvent) error {
		return e.String(200, e.Auth.Id)
	}).Bind(apis.RequireAuth())
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	f.studio.Config.Handler = mux
	f.studio.Start()
	t.Cleanup(f.studio.Close)
	jar, _ := cookiejar.New(nil)
	f.client = &http.Client{Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return f
}

type response struct {
	status         int
	body, location string
	cookies        []*http.Cookie
	headers        http.Header
}

func (f *fixture) request(method, target, origin, body, token string) response {
	f.t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res, err := f.client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	return response{res.StatusCode, string(b), res.Header.Get("Location"), res.Cookies(), res.Header.Clone()}
}

func (f *fixture) expect(r response, status int) {
	f.t.Helper()
	if r.status != status {
		f.t.Fatalf("expected HTTP %d, got %d: %s", status, r.status, r.body)
	}
}

func (f *fixture) loginShop() {
	f.t.Helper()
	f.expect(f.request("POST", f.shop.URL+"/login", "", "", ""), 303)
}

func (f *fixture) authorize(path string) string {
	f.t.Helper()
	start := f.request("GET", f.studio.URL+path, "", "", "")
	f.expect(start, 302)
	a := f.request("GET", start.location, "", "", "")
	f.expect(a, 302)
	return a.location
}

func (f *fixture) loginStudio() response {
	f.t.Helper()
	f.loginShop()
	cb := f.authorize("/studio")
	r := f.request("GET", cb, "", "", "")
	f.expect(r, 303)
	return r
}

func (f *fixture) session() (string, *core.Record) {
	f.t.Helper()
	r := f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", "")
	f.expect(r, 200)
	var data struct {
		Token  string `json:"token"`
		Record struct {
			ID string `json:"id"`
		} `json:"record"`
	}
	if err := json.Unmarshal([]byte(r.body), &data); err != nil {
		f.t.Fatal(err)
	}
	record, err := f.app.FindRecordById(collectionName, data.Record.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return data.Token, record
}

func TestExistingShopSessionCreatesPocketBaseSession(t *testing.T) {
	for _, secret := range []string{"", "demo-secret"} {
		name := "public_PKCE"
		if secret != "" {
			name = "confidential_basic_auth"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t, secret, "")
			callback := f.loginStudio()
			if callback.location != "/studio" {
				t.Fatal(callback.location)
			}
			token, record := f.session()
			if record.Email() != mockshop.Email || record.GetString("first_name") != "Camille" || !record.Verified() {
				t.Fatalf("wrong profile: %v", record.PublicExport())
			}
			if strings.Contains(callback.location, token) {
				t.Fatal("token leaked to URL")
			}
			// Use the issued token against the actual PocketBase CRUD API.
			r := f.request("GET", f.studio.URL+"/api/collections/"+collectionName+"/records/"+record.Id, "", "", token)
			f.expect(r, 200)
			home := f.request("GET", f.studio.URL+"/studio", "", "", "")
			f.expect(home, 200)
			if home.headers.Get("Cache-Control") != "no-store, no-transform" || !strings.Contains(home.body, mockshop.Email) {
				t.Fatal("profile page must preserve the email and prohibit intermediary transformations")
			}
			for _, c := range callback.cookies {
				if c.Name == f.s.cookieName("session") && (!c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.MaxAge != int(record.Collection().AuthToken.DurationTime().Seconds())) {
					t.Fatal("incorrect session cookie flags")
				}
			}
		})
	}
}

func TestAnonymousVisitorSeesShopifyLoginLink(t *testing.T) {
	f := setup(t, "", "")
	cb := f.authorize("/studio")
	if !strings.Contains(cb, "error=login_required") {
		t.Fatal(cb)
	}
	r := f.request("GET", cb, "", "", "")
	f.expect(r, 303)
	if r.location != "/studio?checked=1" {
		t.Fatal(r.location)
	}
	page := f.request("GET", f.studio.URL+r.location, "", "", "")
	f.expect(page, 200)
	if !strings.Contains(page.body, `href="/studio/auth"`) {
		t.Fatal("missing Shopify login link")
	}
	interactive := f.request("GET", f.studio.URL+"/studio/auth", "", "", "")
	f.expect(interactive, 302)
	u, _ := url.Parse(interactive.location)
	if u.Query().Get("prompt") != "" {
		t.Fatal("interactive login must not use prompt=none")
	}
	f.expect(f.request("GET", interactive.location, "", "", ""), 200)
	// Simulate completing the provider's login form in the same browser.
	f.request("POST", f.shop.URL+"/login", "", "", "")
	auth := f.request("GET", interactive.location, "", "", "")
	f.expect(auth, 302)
	f.expect(f.request("GET", auth.location, "", "", ""), 303)
	f.session()
}

func TestCallbackRequiresStateAndBrowserBinding(t *testing.T) {
	f := setup(t, "", "")
	f.expect(f.request("GET", f.studio.URL+"/studio/callback?state=forged&code=fake", "", "", ""), 400)
	f.loginShop()
	cb := f.authorize("/studio")
	u, _ := url.Parse(cb)
	q := u.Query()
	q.Set("state", "wrong")
	u.RawQuery = q.Encode()
	f.expect(f.request("GET", u.String(), "", "", ""), 400)
	// A second browser cannot redeem another browser's callback.
	jar, _ := cookiejar.New(nil)
	original := f.client.Jar
	f.client.Jar = jar
	f.expect(f.request("GET", cb, "", "", ""), 400)
	f.client.Jar = original
	f.expect(f.request("GET", cb, "", "", ""), 303)
	// Replaying a previously consumed callback must fail.
	f.expect(f.request("GET", cb, "", "", ""), 400)
}

func TestInvalidIdentityTokensRejected(t *testing.T) {
	for _, fault := range []string{"nonce", "audience", "issuer", "expired", "signature"} {
		t.Run(fault, func(t *testing.T) {
			f := setup(t, "", fault)
			f.loginShop()
			cb := f.authorize("/studio")
			f.expect(f.request("GET", cb, "", "", ""), 401)
			f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
		})
	}
}

func TestNumericShopifySubjectCreatesPocketBaseSession(t *testing.T) {
	for _, tc := range []struct{ fault, subject string }{
		{"subject_number", "12345"},
		{"numeric:", "900719925474099312345"},
	} {
		t.Run(tc.fault, func(t *testing.T) {
			f := setup(t, "", tc.fault)
			f.loginStudio()
			_, record := f.session()
			if record.GetString("shopify_sub") != tc.subject || record.Email() != mockshop.Email {
				t.Fatal("numeric subject or profile was not preserved exactly")
			}
		})
	}
}

func TestNumericSubjectsDoNotBypassTokenChecks(t *testing.T) {
	for _, fault := range []string{"signature", "algorithm", "issuer", "audience", "expired", "nonce", "not_before"} {
		t.Run(fault, func(t *testing.T) {
			f := setup(t, "", "numeric:"+fault)
			f.loginShop()
			cb := f.authorize("/studio")
			f.expect(f.request("GET", cb, "", "", ""), 401)
			f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
		})
	}
}

type verifiedPayloadKeySet struct {
	payload []byte
	err     error
}

func (k verifiedPayloadKeySet) VerifySignature(context.Context, string) ([]byte, error) {
	return k.payload, k.err
}

func TestShopifySubjectNormalizationIsExactAndLimited(t *testing.T) {
	for _, tc := range []struct {
		sub, want string
		ok        bool
	}{
		{`"unchanged"`, "unchanged", true},
		{`12345`, "12345", true},
		{`900719925474099312345`, "900719925474099312345", true},
		{`0`, "", false},
		{`-1`, "", false},
		{`1.5`, "", false},
		{`1e3`, "", false},
		{`null`, "", false},
		{`true`, "", false},
		{`{}`, "", false},
		{`[]`, "", false},
		{strings.Repeat("1", 256), "", false},
	} {
		input := []byte(`{"sub":` + tc.sub + `,"aud":"client","exp":9999999999,"nonce":"unchanged"}`)
		keys := shopifySubjectKeySet{verifiedPayloadKeySet{payload: input}}
		payload, err := keys.VerifySignature(context.Background(), "not-used-by-the-test-key-set")
		if (err == nil) != tc.ok {
			t.Fatalf("sub %s: unexpected acceptance: %v", tc.sub, err)
		}
		if !tc.ok {
			continue
		}
		var got struct {
			Sub, Aud, Nonce string
			Exp             int64
		}
		if err := json.Unmarshal(payload, &got); err != nil || got.Sub != tc.want || got.Aud != "client" || got.Exp != 9999999999 || got.Nonce != "unchanged" {
			t.Fatal("normalization changed subject precision or another claim")
		}
	}
	denied := errors.New("signature rejected")
	keys := shopifySubjectKeySet{verifiedPayloadKeySet{payload: []byte(`{"sub":123}`), err: denied}}
	if payload, err := keys.VerifySignature(context.Background(), "invalid-token"); !errors.Is(err, denied) || payload != nil {
		t.Fatal("normalization must not run after a signature failure")
	}
}

func TestRealVerifierErrorsReceiveSafeDiagnostics(t *testing.T) {
	for _, tc := range []struct{ fault, reason string }{
		{"audience_number", "claims_decode_failed"},
		{"not_before", "not_yet_valid"},
	} {
		t.Run(tc.fault, func(t *testing.T) {
			f := setup(t, "", tc.fault)
			f.loginShop()
			cb := f.authorize("/studio")
			r := f.request("GET", cb, "", "", "")
			f.expect(r, 401)
			if !strings.Contains(r.body, "("+tc.reason+")") {
				t.Fatalf("missing safe diagnostic: %s", r.body)
			}
			f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
		})
	}
}

func TestIDTokenDiagnosticsDoNotExposeClaimsOrUpstreamBodies(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&oidc.TokenExpiredError{Expiry: time.Now()}, "expired"},
		{errors.New(`oidc: expected audience "client" got ["private-value"]`), "audience_mismatch"},
		{errors.New(`oidc: id token issued by a different provider, got "private-value"`), "issuer_mismatch"},
		{errors.New("oidc: malformed jwt: private-value"), "token_format_or_algorithm"},
		{errors.New("oidc: failed to unmarshal claims: private-value"), "claims_decode_failed"},
		{errors.New("oidc: current time private-value before the nbf (not before) time: private-value"), "not_yet_valid"},
		{errors.New("oidc: failed to obtain source from claim name"), "distributed_claims_invalid"},
		{errors.New("oidc: source does not exist"), "distributed_claims_invalid"},
		{errors.New("oidc: invalid configuration, clientID must be provided or SkipClientIDCheck must be set"), "verifier_configuration_invalid"},
		{errors.New("oidc: id token not signed"), "token_signature_format"},
		{errors.New("oidc: multiple signatures on id token not supported"), "token_signature_format"},
		{errors.New("failed to verify signature: fetching keys context canceled"), "verification_cancelled"},
		{errors.New("failed to verify signature: fetching keys context deadline exceeded"), "verification_timeout"},
		{errors.New("failed to verify signature: fetching keys oidc: get keys failed: private-value"), "jwks_fetch_failed"},
		{errors.New("failed to verify signature: fetching keys oidc: failed to decode keys: private-value"), "jwks_decode_failed"},
		{errors.New("failed to verify signature: failed to verify id token signature"), "signature_mismatch"},
		{errors.New("failed to verify signature: fetching keys unable to read response body: private-value"), "jwks_read_failed"},
		{errors.New("private-value"), "verification_failed"},
	} {
		if got := idTokenFailureReason(tc.err); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestIDTokenClaimTypeDiagnosticsNeverReturnValues(t *testing.T) {
	payload := `{"iss":"private-value","sub":123,"aud":["private-value"],"exp":123,"iat":null,"nonce":true,"_claim_names":{"private-value":"private-value"},"email":"private-value","private-value":"private-value"}`
	raw := "unused." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".unused"
	types := idTokenClaimTypes(raw)
	if len(types) != 7 || types["iss"] != "string" || types["sub"] != "number" || types["aud"] != "array" || types["iat"] != "null" || types["nonce"] != "boolean" || types["_claim_names"] != "object" {
		t.Fatalf("unexpected type labels: %v", types)
	}
	encoded, _ := json.Marshal(types)
	if strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "email") {
		t.Fatal("diagnostic exposed a value or an unlisted field")
	}
	for _, malformed := range []string{"", "not-a-jwt", "x.!.x", "x." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".x", "x." + strings.Repeat("x", 64*1024+1) + ".x"} {
		if idTokenClaimTypes(malformed) != nil {
			t.Fatal("invalid input should not produce diagnostics")
		}
	}
}

func TestGraphQLErrorsDoNotCreateSession(t *testing.T) {
	f := setup(t, "", "graphql")
	f.loginShop()
	cb := f.authorize("/studio")
	f.expect(f.request("GET", cb, "", "", ""), 502)
	f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
}

func TestShopifySubjectSurvivesEmailChangeAndNeverMerges(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	_, r := f.session()
	c := &customer{ID: r.GetString("shopify_customer_id"), FirstName: "Updated"}
	c.EmailAddress = &struct {
		Email string `json:"emailAddress"`
	}{Email: "new@example.test"}
	updated, err := f.s.saveCustomer(r.GetString("shopify_sub"), c, claims{Email: "new@example.test", EmailVerified: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Id != r.Id || updated.Email() != "new@example.test" {
		t.Fatal("email change created a new identity")
	}
	_, err = f.s.saveCustomer("different-shopify-subject", c, claims{}, nil)
	if err == nil {
		t.Fatal("another subject must not take over the existing email")
	}
	unchanged, err := f.app.FindRecordById(collectionName, r.Id)
	if err != nil || unchanged.GetString("shopify_sub") != r.GetString("shopify_sub") {
		t.Fatal("existing account changed")
	}
}

func TestEmailVerificationRequiresMatchingClaim(t *testing.T) {
	f := setup(t, "", "email")
	f.loginStudio()
	_, r := f.session()
	if r.Verified() {
		t.Fatal("must not mark a mismatching email as verified")
	}
}

func TestPocketBasePermissionsAndSessionExpiry(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	token, r := f.session()
	// Shopify identity is server-managed, even though regular signup is allowed.
	f.expect(f.request("PATCH", f.studio.URL+"/api/collections/"+collectionName+"/records/"+r.Id, "", `{"shopify_sub":"attacker"}`, token), 403)
	f.expect(f.request("POST", f.studio.URL+"/api/collections/"+collectionName+"/records", "", `{"email":"attacker@example.test","password":"longpassword123","passwordConfirm":"longpassword123","shopify_sub":"preclaimed"}`, ""), 403)
	refresh := f.request("POST", f.studio.URL+"/api/collections/"+collectionName+"/auth-refresh", "", "", token)
	f.expect(refresh, 200)
	var refreshed struct {
		Token string `json:"token"`
	}
	json.Unmarshal([]byte(refresh.body), &refreshed)
	if refreshed.Token == "" {
		t.Fatal("native token renewal returned no token")
	}
	// A correctly signed but expired PB token must fail too.
	expired, err := r.NewStaticAuthToken(time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.studio.URL)
	f.client.Jar.SetCookies(u, []*http.Cookie{{Name: f.s.cookieName("session"), Value: expired, Path: "/"}})
	f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
}

func TestCrossOriginAndLogout(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	f.expect(f.request("POST", f.studio.URL+"/studio/session", "https://evil.example", "", ""), 403)
	f.expect(f.request("POST", f.studio.URL+"/studio/logout", "https://evil.example", "", ""), 403)
	f.session()
	f.expect(f.request("POST", f.studio.URL+"/studio/logout", f.studio.URL, "", ""), 303)
	f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
}

func TestExpiredLoginTransaction(t *testing.T) {
	f := setup(t, "", "")
	f.loginShop()
	cb := f.authorize("/studio")
	f.s.mu.Lock()
	for k, p := range f.s.pending {
		p.Expires = time.Now().Add(-time.Minute)
		f.s.pending[k] = p
	}
	f.s.mu.Unlock()
	f.expect(f.request("GET", cb, "", "", ""), 400)
}

func TestProductionURLAndCookieRequirements(t *testing.T) {
	for _, raw := range []string{"http://shop.example", "https://u:p@shop.example", "https://shop.example/path", "https://shop.example?shop=evil", ""} {
		if checkURL(raw, false, true) == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if checkURL("http://127.0.0.1:8090", false, true) == nil {
		t.Fatal("loopback HTTP must be explicitly enabled")
	}
	if checkURL("http://127.0.0.1:8090", true, true) != nil {
		t.Fatal("local mock should be allowed")
	}
	s := &Service{cfg: Config{StudioURL: "https://studio.example"}}
	rr := httptest.NewRecorder()
	e := &core.RequestEvent{}
	e.Response = rr
	s.setCookie(e, "session", "test", 900)
	c := rr.Result().Cookies()[0]
	if c.Name != "__Host-studio_session" || !c.Secure || !c.HttpOnly || c.Domain != "" || c.Path != "/" {
		t.Fatal("missing production cookie protections")
	}
}

func TestPKCETamperingRejected(t *testing.T) {
	f := setup(t, "", "")
	f.loginShop()
	start := f.request("GET", f.studio.URL+"/studio", "", "", "")
	f.expect(start, 302)
	u, _ := url.Parse(start.location)
	q := u.Query()
	q.Set("code_challenge", "attacker-challenge")
	u.RawQuery = q.Encode()
	a := f.request("GET", u.String(), "", "", "")
	f.expect(a, 302)
	f.expect(f.request("GET", a.location, "", "", ""), 502)
}

// PocketBase already has an OIDC provider. Verify its compatibility separately
// and show why profile enrichment still needs the Shopify GraphQL API.
func TestNativePocketBaseOIDCProvider(t *testing.T) {
	f := setup(t, "", "")
	f.loginShop()
	p := auth.NewOIDCProvider()
	p.SetContext(context.Background())
	p.SetClientId(mockshop.ClientID)
	p.SetAuthURL(f.s.oauth.Endpoint.AuthURL)
	p.SetTokenURL(f.s.oauth.Endpoint.TokenURL)
	p.SetRedirectURL(f.s.oauth.RedirectURL)
	p.SetScopes(f.s.oauth.Scopes)
	p.SetExtra(map[string]any{"issuers": []string{f.s.issuer}, "jwksURL": f.shop.URL + "/jwks"})
	verifier := oauth2.GenerateVerifier()
	a := f.request("GET", p.BuildAuthURL(random(), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", random())), "", "", "")
	f.expect(a, 302)
	u, _ := url.Parse(a.location)
	tok, err := p.FetchToken(u.Query().Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		t.Fatal(err)
	}
	user, err := p.FetchAuthUser(tok)
	if err != nil {
		t.Fatal(err)
	}
	if user.Id == "" || user.Email != mockshop.Email {
		t.Fatal("native OIDC provider did not extract the identity")
	}
	if user.Name != "" {
		t.Fatal("mock intentionally omits names in the ID token")
	}
}
