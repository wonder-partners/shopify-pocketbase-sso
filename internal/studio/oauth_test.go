package studio

import (
	"encoding/json"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

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

func TestShopifyAuthenticationIsBoundToUsers(t *testing.T) {
	// A leftover setting from the prototype must not redirect authentication.
	t.Setenv("STUDIO_AUTH_COLLECTION", "members")
	f := setup(t, "")
	members := core.NewAuthCollection("members")
	if err := f.app.Save(members); err != nil {
		t.Fatal(err)
	}
	superusers, err := f.app.FindCollectionByNameOrId("_superusers")
	if err != nil {
		t.Fatal(err)
	}
	f.loginShop()
	page := f.callback(f.authorize(false))
	f.expect(page, 200)
	data := callbackData(t, page.body)
	if data["collection"] != "users" {
		t.Fatal("callback targeted a collection other than users")
	}
	body := nativeOAuthBody(t, data)
	for _, target := range []string{members.Name, members.Id, superusers.Name, superusers.Id} {
		f.expect(f.request("POST", f.api.URL+"/api/collections/"+target+"/auth-with-oauth2", f.api.URL, body, ""), 403)
	}
	users, _ := f.app.FindCollectionByNameOrId("users")
	result := f.request("POST", f.api.URL+"/api/collections/"+users.Id+"/auth-with-oauth2", f.api.URL, body, "")
	f.expect(result, 200)
	userID := decodeAuth(t, result).Record["id"].(string)
	a := privateAccount(f, userID)
	relation := a.Collection().Fields.GetByName("user").(*core.RelationField)
	if relation.CollectionId != users.Id {
		t.Fatal("private Shopify identity is not linked to users")
	}
}

func TestNativeOAuthHooksCreationAndExternalAuthLink(t *testing.T) {
	f := setup(t, "numeric:")
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
	first := decodeAuth(t, f.login())
	_, user := f.authRecord()
	if oauthCalls != 1 || createCalls != 1 || first.Meta["isNew"] != true {
		t.Fatal("native OAuth signup or creation hook was bypassed")
	}
	link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{
		"provider": providerName, "recordRef": user.Id,
	})
	if err != nil || link.ProviderId() != identityID(f.s.issuer, privateAccount(f, user.Id).GetString("subject")) {
		t.Fatal("missing native identity link")
	}
	if first.Meta["accessToken"] != "" || first.Meta["refreshToken"] != "" || strings.Contains(string(mustJSON(t, first.Meta)), "id_token") {
		t.Fatal("upstream credentials leaked in auth meta")
	}
	second := decodeAuth(t, f.login())
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
			f := setup(t, "")
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
			r := f.completeCallback(f.authorize(false))
			f.expect(r, 403)
			if strings.Contains(r.body, `"token"`) {
				t.Fatal("denied OAuth exposed a JWT")
			}
			if strings.HasPrefix(stage, "create") {
				count, _ := f.app.CountRecords("users")
				if count != 0 {
					t.Fatal("rejected native signup was not rolled back")
				}
			}
		})
	}
}

func TestNativeOAuthCodeIsPKCEBoundSingleUseAndOriginProtected(t *testing.T) {
	f := setup(t, "")
	f.loginShop()
	cb := f.authorize(false)
	endpoint := f.api.URL + "/api/collections/users/auth-with-oauth2"
	page := f.callback(cb)
	f.expect(page, 200)
	data := callbackData(t, page.body)
	body := nativeOAuthBody(t, data)
	f.expect(f.request("POST", endpoint, "https://evil.example", body, ""), 403)
	// Knowing the correct verifier is sufficient; there is no OAuth cookie.
	f.client.Jar, _ = cookiejar.New(nil)
	data["redirectURL"] = "https://evil.example/callback"
	f.expect(f.request("POST", endpoint, f.api.URL, nativeOAuthBody(t, data), ""), 400)
	data["redirectURL"] = f.s.oauth.RedirectURL
	r := f.request("POST", endpoint, f.api.URL, nativeOAuthBody(t, data), "")
	f.expect(r, 200)
	if len(r.cookies) != 0 || r.headers.Get("Cache-Control") != "no-store" {
		t.Fatal("native OAuth must not set cookies or allow caching")
	}
	f.expect(f.request("POST", endpoint, f.api.URL, body, ""), 502)
}

func TestNativeOAuthRejectsExpiredCodeAndWrongVerifier(t *testing.T) {
	for _, fault := range []string{"code_expired", "missing", "short", "long", "characters", "wrong"} {
		t.Run(fault, func(t *testing.T) {
			f := setup(t, "")
			f.loginShop()
			page := f.callback(f.authorize(false))
			f.expect(page, 200)
			data := callbackData(t, page.body)
			status := 400
			switch fault {
			case "code_expired":
				f.mock.Fault = fault
				status = 502
			case "missing":
				data["codeVerifier"] = ""
			case "short":
				data["codeVerifier"] = "short"
			case "long":
				data["codeVerifier"] = strings.Repeat("a", 129)
			case "characters":
				data["codeVerifier"] = strings.Repeat("!", 43)
			case "wrong":
				data["codeVerifier"] = strings.Repeat("a", 43)
				status = 502
			}
			f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-with-oauth2", f.api.URL, nativeOAuthBody(t, data), ""), status)
		})
	}
}

func TestNativeAuthMethodsBindNonceToFreshPKCEChallenge(t *testing.T) {
	f := setup(t, "")
	first, second := f.start(false), f.start(false)
	a, _ := url.Parse(first.location)
	b, _ := url.Parse(second.location)
	q := a.Query()
	if q.Get("nonce") == "" || q.Get("nonce") != q.Get("code_challenge") || q.Get("code_challenge_method") != "S256" || q.Get("state") == b.Query().Get("state") || q.Get("nonce") == b.Query().Get("nonce") {
		t.Fatal("native methods did not generate fresh PKCE-bound nonces and states")
	}
	if strings.Contains(first.location, f.flows[q.Get("state")].CodeVerifier) {
		t.Fatal("secret verifier leaked into authorization URL")
	}
	p := &shopifyProvider{}
	p.SetAuthURL(f.s.oauth.Endpoint.AuthURL)
	if p.BuildAuthURL("state") != "" {
		t.Fatal("provider allowed authorization without S256 PKCE")
	}
}

func TestNativeOAuthRejectsNonceSubstitutionWithValidPKCE(t *testing.T) {
	f := setup(t, "")
	f.loginShop()
	start := f.start(false)
	u, _ := url.Parse(start.location)
	q := u.Query()
	q.Set("nonce", "a-different-authorization-request")
	u.RawQuery = q.Encode()
	// Shopify accepts the code and the matching verifier, but our provider
	// rejects the signed ID token because its nonce is from a different flow.
	a := f.request("GET", u.String(), "", "", "")
	f.expect(a, 302)
	f.expect(f.completeCallback(a.location), 401)
	count, _ := f.app.CountRecords("users")
	if count != 0 {
		t.Fatal("nonce substitution created an account")
	}
}

func TestNativeOAuthDoesNotLinkToTheAuthorizationHeaderAccount(t *testing.T) {
	f := setup(t, "")
	native := nativeSignup(f, "unrelated@example.test")
	f.loginShop()
	page := f.callback(f.authorize(false))
	data := callbackData(t, page.body)
	f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-with-oauth2", f.api.URL, nativeOAuthBody(t, data), native.Token), 409)
	count, _ := f.app.CountRecords(core.CollectionNameExternalAuths)
	if count != 0 {
		t.Fatal("OAuth linked to an unrelated authenticated account")
	}
}

func TestNativeOAuthPreservesLinkedAccountAndRelations(t *testing.T) {
	f := setup(t, "")
	documentsCollection(f)
	c, _ := f.app.FindCollectionByNameOrId("users")
	legacy := core.NewRecord(c)
	legacy.SetEmail("camille@example.test")
	legacy.SetPassword("TestPassword12345")
	if err := f.app.Save(legacy); err != nil {
		t.Fatal(err)
	}
	if err := putAccount(f.app, legacy.Id, f.s.issuer, "shopify-customer-sub-12345", "gid://shopify/Customer/12345"); err != nil {
		t.Fatal(err)
	}
	token, _ := legacy.NewAuthToken()
	doc := createDocument(f, token, legacy.Id)
	if err := ensureCollection(f.app); err != nil {
		t.Fatal(err)
	}
	result := decodeAuth(t, f.login())
	if result.Record["id"] != legacy.Id || result.Meta["isNew"] != false {
		t.Fatal("prototype identity was not preserved")
	}
	f.expect(f.request("GET", f.api.URL+"/api/collections/documents/records/"+doc, "", "", result.Token), 200)
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
	f := setup(t, "")
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
	f := setup(t, "email") // The verified claim does not match the profile email.
	f.loginShop()
	page := f.callback(f.authorize(false))
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
	r := f.request("POST", f.api.URL+"/api/collections/users/auth-with-oauth2", f.api.URL, string(mustJSON(t, body)), "")
	f.expect(r, 200)
	result := decodeAuth(t, r)
	if result.Record["verified"] != false || result.Record["email"] != "camille@example.test" || privateAccount(f, result.Record["id"].(string)).GetString("subject") != "shopify-customer-sub-12345" {
		t.Fatal("client-controlled createData overrode the verified provider profile")
	}
	for _, field := range []string{"shopify_issuer", "shopify_sub", "shopify_customer_id"} {
		if _, exists := result.Record[field]; exists {
			t.Fatal("legacy identity field leaked in the native OAuth response")
		}
	}
	f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-with-password", "", `{"identity":"camille@example.test","password":"ChosenPassword12345"}`, ""), 400)
}
