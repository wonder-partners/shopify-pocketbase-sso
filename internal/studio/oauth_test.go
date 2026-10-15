package studio

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/auth"
)

func nativeOAuthBody(t *testing.T, data map[string]string) string {
	t.Helper()
	delete(data, "collection")
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestNativeOAuthHooksCreationAndExternalAuthLink(t *testing.T) {
	f := setup(t, "", "numeric:")
	oauthCalls, createCalls := 0, 0
	f.app.OnRecordAuthWithOAuth2Request("users").BindFunc(func(e *core.RecordAuthWithOAuth2RequestEvent) error {
		oauthCalls++
		if e.ProviderName != providerName || e.OAuth2User.Id == "" {
			t.Fatal("native OAuth hook is missing the verified identity")
		}
		return e.Next()
	})
	f.app.OnRecordCreateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
		createCalls++
		info, err := e.RequestInfo()
		if err != nil || info.Context != core.RequestInfoContextOAuth2 {
			t.Fatal("signup did not use the native OAuth records API context")
		}
		return e.Next()
	})
	first := decodeAuth(t, f.loginStudio())
	_, user := f.session()
	if oauthCalls != 1 || createCalls != 1 || first.Meta["isNew"] != true {
		t.Fatal("native OAuth signup or creation hook was bypassed")
	}
	link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{
		"provider": providerName, "recordRef": user.Id,
	})
	if err != nil || link.ProviderId() != identityID(f.s.issuer, user.GetString("shopify_sub")) {
		t.Fatal("missing native identity link")
	}
	if first.Meta["accessToken"] != "" || first.Meta["refreshToken"] != "" || strings.Contains(string(mustJSON(t, first.Meta)), "id_token") {
		t.Fatal("upstream credentials leaked in auth meta")
	}
	second := decodeAuth(t, f.loginStudio())
	if second.Record["id"] != user.Id || second.Meta["isNew"] != false || oauthCalls != 2 || createCalls != 1 {
		t.Fatal("repeat OAuth login duplicated or recreated the user")
	}
	count, _ := f.app.CountRecords(core.CollectionNameExternalAuths)
	if count != 1 {
		t.Fatal("repeat login duplicated the native link")
	}
}

func mustJSON(t *testing.T, data any) []byte {
	t.Helper()
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNativeOAuthAndCreateHooksCanRejectShopify(t *testing.T) {
	for _, stage := range []string{"oauth_before", "oauth_after", "create_before", "create_after"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "", "")
			if strings.HasPrefix(stage, "oauth") {
				f.app.OnRecordAuthWithOAuth2Request("users").BindFunc(func(e *core.RecordAuthWithOAuth2RequestEvent) error {
					if stage == "oauth_after" {
						if err := e.Next(); err != nil {
							return err
						}
					}
					return e.ForbiddenError("OAuth rejected by the project.", nil)
				})
			} else {
				f.app.OnRecordCreateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
					if stage == "create_after" {
						if err := e.Next(); err != nil {
							return err
						}
					}
					return e.ForbiddenError("Signup rejected by the project.", nil)
				})
			}
			f.loginShop()
			r := f.completeCallback(f.authorize("/studio/auth"))
			f.expect(r, 403)
			if strings.Contains(r.body, `"token"`) {
				t.Fatal("denied OAuth exposed a JWT")
			}
			f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
			if strings.HasPrefix(stage, "create") {
				count, _ := f.app.CountRecords("users")
				if count != 0 {
					t.Fatal("rejected native signup was not rolled back")
				}
			}
		})
	}
}

func TestNativeOAuthCodeIsBrowserBoundSingleUseAndOriginProtected(t *testing.T) {
	f := setup(t, "", "")
	f.loginShop()
	cb := f.authorize("/studio/auth")
	u, _ := url.Parse(cb)
	f.s.mu.Lock()
	pending := f.s.pending[u.Query().Get("state")]
	f.s.mu.Unlock()
	data := map[string]string{"provider": providerName, "code": u.Query().Get("code"), "codeVerifier": pending.Verifier, "redirectURL": f.s.oauth.RedirectURL}
	body := nativeOAuthBody(t, data)
	endpoint := f.studio.URL + "/api/collections/users/auth-with-oauth2"
	// Even a real code and verifier cannot bypass the state-checked callback.
	f.expect(f.request("POST", endpoint, f.studio.URL, body, ""), 400)
	page := f.request("GET", cb, "", "", "")
	f.expect(page, 200)
	if page.headers.Get("Cache-Control") != "no-store, no-transform" || !strings.Contains(page.body, "/studio/callback.js") {
		t.Fatal("unsafe callback document")
	}
	f.expect(f.request("POST", endpoint, "https://evil.example", body, ""), 403)
	jar := f.client.Jar
	f.client.Jar, _ = cookiejar.New(nil)
	f.expect(f.request("POST", endpoint, f.studio.URL, body, ""), 400)
	u2, _ := url.Parse(f.studio.URL)
	f.client.Jar.SetCookies(u2, []*http.Cookie{{Name: f.s.cookieName("flow"), Value: "another-browser", Path: "/"}})
	f.expect(f.request("POST", endpoint, f.studio.URL, body, ""), 400)
	f.client.Jar = jar
	data["redirectURL"] = "https://evil.example/callback"
	f.expect(f.request("POST", endpoint, f.studio.URL, nativeOAuthBody(t, data), ""), 400)
	data["redirectURL"] = f.s.oauth.RedirectURL
	f.expect(f.request("POST", endpoint, f.studio.URL, nativeOAuthBody(t, data), ""), 200)
	f.expect(f.request("POST", endpoint, f.studio.URL, body, ""), 400)
}

func TestNativeOAuthRejectsExpiredCompletionAndWrongVerifier(t *testing.T) {
	for _, fault := range []string{"expired", "pkce"} {
		t.Run(fault, func(t *testing.T) {
			f := setup(t, "", "")
			f.loginShop()
			page := f.request("GET", f.authorize("/studio/auth"), "", "", "")
			f.expect(page, 200)
			data := callbackData(t, page.body)
			status := 502
			if fault == "expired" {
				f.s.mu.Lock()
				p := f.s.ready[data["code"]]
				p.Expires = time.Now().Add(-time.Minute)
				f.s.ready[data["code"]] = p
				f.s.mu.Unlock()
				status = 400
			} else {
				data["codeVerifier"] = "wrong-verifier"
			}
			f.expect(f.request("POST", f.studio.URL+"/api/collections/users/auth-with-oauth2", f.studio.URL, nativeOAuthBody(t, data), ""), status)
			f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
		})
	}
}

func TestNativeOAuthDoesNotLinkToTheAuthorizationHeaderAccount(t *testing.T) {
	f := setup(t, "", "")
	native := nativeSignup(f, "unrelated@example.test")
	f.loginShop()
	page := f.request("GET", f.authorize("/studio/auth"), "", "", "")
	data := callbackData(t, page.body)
	f.expect(f.request("POST", f.studio.URL+"/api/collections/users/auth-with-oauth2", f.studio.URL, nativeOAuthBody(t, data), native.Token), 409)
	count, _ := f.app.CountRecords(core.CollectionNameExternalAuths)
	if count != 0 {
		t.Fatal("OAuth linked to an unrelated authenticated account")
	}
}

func TestNativeOAuthPreservesPrototypeAccountAndRelations(t *testing.T) {
	f := setup(t, "", "")
	documentsCollection(f)
	c, _ := f.app.FindCollectionByNameOrId("users")
	legacy := core.NewRecord(c)
	legacy.SetEmail("camille@example.test")
	legacy.SetPassword("TestPassword12345")
	legacy.Set("shopify_issuer", f.s.issuer)
	legacy.Set("shopify_sub", "shopify-customer-sub-12345")
	if err := f.app.Save(legacy); err != nil {
		t.Fatal(err)
	}
	token, _ := legacy.NewAuthToken()
	doc := createDocument(f, token, legacy.Id)
	result := decodeAuth(t, f.loginStudio())
	if result.Record["id"] != legacy.Id || result.Meta["isNew"] != false {
		t.Fatal("prototype identity was not preserved")
	}
	f.expect(f.request("GET", f.studio.URL+"/api/collections/documents/records/"+doc, "", "", result.Token), 200)
	link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": legacy.Id})
	if err != nil || link.ProviderId() == "" {
		t.Fatal("native link was not created for the prototype user")
	}
}

func TestShopifyProviderRegistrationAndIdentityNamespace(t *testing.T) {
	p, err := auth.NewProviderByName(providerName)
	if err != nil || !p.PKCE() {
		t.Fatal("Shopify provider was not registered")
	}
	if identityID("issuer-a", "123") == identityID("issuer-b", "123") {
		t.Fatal("provider identity ignores its issuer")
	}
	if _, err := p.FetchAuthUser(nil); err == nil {
		t.Fatal("unverified identity accepted")
	}
	p.SetContext(t.Context())
	if _, err := p.FetchToken("unprepared"); err == nil {
		t.Fatal("provider accepted a request outside the bound flow")
	}
}

func TestShopifyProviderConfigurationPreservesOtherProvidersAndPolicies(t *testing.T) {
	f := setup(t, "", "")
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.OAuth2.Providers = append(c.OAuth2.Providers, core.OAuth2ProviderConfig{Name: "google", ClientId: "test-client", ClientSecret: "test-secret"})
	c.AuthToken.Duration = 1200
	c.PasswordAuth.Enabled = false
	c.OTP.Enabled, c.MFA.Enabled = true, true
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	if err := f.s.configureProvider(); err != nil {
		t.Fatal(err)
	}
	c, _ = f.app.FindCollectionByNameOrId("users")
	google, found := c.OAuth2.GetProviderConfig("google")
	if !found || google.ClientId != "test-client" || google.ClientSecret != "test-secret" || len(c.OAuth2.Providers) != 2 {
		t.Fatal("Shopify replaced another provider or duplicated its configuration")
	}
	if c.AuthToken.Duration != 1200 || c.PasswordAuth.Enabled || !c.OTP.Enabled || !c.MFA.Enabled {
		t.Fatal("Shopify provider configuration changed unrelated auth policies")
	}
}

func TestOAuthCreateDataCannotVerifyEmailOrChooseTheShopifyPassword(t *testing.T) {
	f := setup(t, "", "email") // The verified claim does not match the profile email.
	f.loginShop()
	page := f.request("GET", f.authorize("/studio/auth"), "", "", "")
	f.expect(page, 200)
	data := callbackData(t, page.body)
	delete(data, "collection")
	body := map[string]any{}
	for key, value := range data {
		body[key] = value
	}
	body["createData"] = map[string]any{
		"email": "forged@example.test", "verified": true, "shopify_sub": "forged-subject",
		"password": "ChosenPassword12345", "passwordConfirm": "ChosenPassword12345",
	}
	r := f.request("POST", f.studio.URL+"/api/collections/users/auth-with-oauth2", f.studio.URL, string(mustJSON(t, body)), "")
	f.expect(r, 200)
	result := decodeAuth(t, r)
	if result.Record["verified"] != false || result.Record["email"] != "camille@example.test" || result.Record["shopify_sub"] != "shopify-customer-sub-12345" {
		t.Fatal("client-controlled createData overrode the verified provider profile")
	}
	f.expect(f.request("POST", f.studio.URL+"/api/collections/users/auth-with-password", "", `{"identity":"camille@example.test","password":"ChosenPassword12345"}`, ""), 400)
}
