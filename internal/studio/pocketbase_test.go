package studio

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"github.com/pocketbase/pocketbase/tools/types"
)

type authResult struct {
	Token  string         `json:"token"`
	Record map[string]any `json:"record"`
	Meta   map[string]any `json:"meta"`
}

func decodeAuth(t *testing.T, r response) authResult {
	t.Helper()
	var result authResult
	if json.Unmarshal([]byte(r.body), &result) != nil || result.Token == "" {
		t.Fatal("missing standard PocketBase auth response")
	}
	return result
}

func nativeSignup(f *fixture, email string) authResult {
	f.t.Helper()
	body := `{"email":"` + email + `","password":"TestPassword12345","passwordConfirm":"TestPassword12345"}`
	f.expect(f.request("POST", f.studio.URL+"/api/collections/users/records", "", body, ""), 200)
	r := f.request("POST", f.studio.URL+"/api/collections/users/auth-with-password", "", `{"identity":"`+email+`","password":"TestPassword12345"}`, "")
	f.expect(r, 200)
	return decodeAuth(f.t, r)
}

func documentsCollection(f *fixture) {
	f.t.Helper()
	users, _ := f.app.FindCollectionByNameOrId("users")
	c := core.NewBaseCollection("documents")
	c.Fields.Add(&core.TextField{Name: "title", Required: true})
	c.Fields.Add(&core.RelationField{Name: "owner", Required: true, CollectionId: users.Id, MaxSelect: 1})
	rule := "@request.auth.id != '' && owner = @request.auth.id"
	c.CreateRule, c.ListRule, c.ViewRule, c.DeleteRule = types.Pointer(rule), types.Pointer(rule), types.Pointer(rule), types.Pointer(rule)
	c.UpdateRule = types.Pointer(rule + " && @request.body.owner:changed = false")
	if err := f.app.Save(c); err != nil {
		f.t.Fatal(err)
	}
}

func createDocument(f *fixture, token, owner string) string {
	f.t.Helper()
	r := f.request("POST", f.studio.URL+"/api/collections/documents/records", "", `{"title":"Private document","owner":"`+owner+`"}`, token)
	f.expect(r, 200)
	var doc struct{ Id string }
	if json.Unmarshal([]byte(r.body), &doc) != nil || doc.Id == "" {
		f.t.Fatal("missing document")
	}
	return doc.Id
}

func TestShopifyAndBuiltInAccountsUseTheSameAPIRules(t *testing.T) {
	f := setup(t, "", "")
	documentsCollection(f)
	f.loginStudio()
	shopToken, shopUser := f.session()
	native := nativeSignup(f, "native@example.test")
	// A second unlinked account must not conflict with the Shopify identity index.
	nativeSignup(f, "another-native@example.test")
	shopDoc := createDocument(f, shopToken, shopUser.Id)
	nativeID := native.Record["id"].(string)
	nativeDoc := createDocument(f, native.Token, nativeID)
	for _, tc := range []struct{ name, token, ownID, ownDoc, otherDoc, otherID string }{
		{"shopify", shopToken, shopUser.Id, shopDoc, nativeDoc, nativeID},
		{"password", native.Token, nativeID, nativeDoc, shopDoc, shopUser.Id},
	} {
		t.Run(tc.name, func(t *testing.T) {
			who := f.request("GET", f.studio.URL+"/native/whoami", "", "", tc.token)
			f.expect(who, 200)
			if who.body != tc.ownID {
				t.Fatal("native RequireAuth middleware did not load e.Auth")
			}
			base := f.studio.URL + "/api/collections/documents/records"
			f.expect(f.request("GET", base+"/"+tc.ownDoc, "", "", tc.token), 200)
			f.expect(f.request("GET", base+"/"+tc.otherDoc, "", "", tc.token), 404)
			f.expect(f.request("PATCH", base+"/"+tc.ownDoc, "", `{"title":"Updated"}`, tc.token), 200)
			f.expect(f.request("PATCH", base+"/"+tc.otherDoc, "", `{"title":"Denied"}`, tc.token), 404)
			f.expect(f.request("PATCH", base+"/"+tc.ownDoc, "", `{"owner":"`+tc.otherID+`"}`, tc.token), 404)
			f.expect(f.request("POST", base, "", `{"title":"Denied","owner":"`+tc.otherID+`"}`, tc.token), 400)
			f.expect(f.request("DELETE", base+"/"+tc.otherDoc, "", "", tc.token), 404)
			list := f.request("GET", base, "", "", tc.token)
			f.expect(list, 200)
			var page struct {
				Items []struct{ Id string }
			}
			if json.Unmarshal([]byte(list.body), &page) != nil || len(page.Items) != 1 || page.Items[0].Id != tc.ownDoc {
				t.Fatal("ListRule leaked another user's document")
			}
			refresh := f.request("POST", f.studio.URL+"/api/collections/users/auth-refresh", "", "", tc.token)
			f.expect(refresh, 200)
			claims, err := security.ParseUnverifiedJWT(decodeAuth(t, refresh).Token)
			if err != nil || claims[core.TokenClaimRefreshable] != true {
				t.Fatal("account does not receive a normal renewable auth token")
			}
		})
	}
	f.expect(f.request("GET", f.studio.URL+"/api/collections/documents/records/"+shopDoc, "", "", ""), 404)
	f.expect(f.request("GET", f.studio.URL+"/native/whoami", "", "", ""), 401)
}

func TestNativeAuthHooksAndAuthRuleAreNotBypassed(t *testing.T) {
	for _, denyAfterNext := range []bool{false, true} {
		f := setup(t, "", "")
		called := 0
		f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
			called++
			if denyAfterNext {
				if err := e.Next(); err != nil {
					return err
				}
			}
			return e.ForbiddenError("Blocked by application auth hook.", nil)
		})
		f.loginShop()
		r := f.completeCallback(f.authorize("/studio"))
		f.expect(r, 403)
		if called != 1 || strings.Contains(r.body, `"token"`) {
			t.Fatal("auth hook was bypassed or a denied token escaped")
		}
		f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
	}
	f := setup(t, "", "email")
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.AuthRule = types.Pointer("verified = true")
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	f.loginShop()
	f.expect(f.completeCallback(f.authorize("/studio")), 403)
	f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
}

func TestNativeMFAIsRequiredBeforeIssuingAShopifySession(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	_, user := f.session()
	f.expect(f.request("POST", f.studio.URL+"/studio/logout", f.studio.URL, "", ""), 303)
	user.SetPassword("TestPassword12345")
	if err := f.app.Save(user); err != nil {
		t.Fatal(err)
	}
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.MFA.Enabled = true
	c.OTP.Enabled = true
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	f.loginShop()
	r := f.completeCallback(f.authorize("/studio"))
	f.expect(r, 401)
	var challenge struct {
		MFAId string `json:"mfaId"`
	}
	if json.Unmarshal([]byte(r.body), &challenge) != nil || challenge.MFAId == "" || strings.Contains(r.body, `"token"`) {
		t.Fatal("MFA was not enforced")
	}
	f.expect(f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""), 401)
	r = f.request("POST", f.studio.URL+"/api/collections/users/auth-with-password", "", `{"identity":"camille@example.test","password":"TestPassword12345","mfaId":"`+challenge.MFAId+`"}`, "")
	f.expect(r, 200)
	if decodeAuth(t, r).Record["id"] != user.Id {
		t.Fatal("native second MFA factor returned another account")
	}
}

func TestCreateRuleCanDisableShopifySignup(t *testing.T) {
	f := setup(t, "", "")
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.CreateRule = nil
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	f.loginShop()
	f.expect(f.completeCallback(f.authorize("/studio")), 403)
	count, err := f.app.CountRecords("users")
	if err != nil || count != 0 {
		t.Fatal("a locked CreateRule must prevent provisioning a Shopify account")
	}
}

func TestExistingAuthCollectionPoliciesArePreserved(t *testing.T) {
	f := setup(t, "", "")
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.AuthToken.Duration = 1200
	c.UpdateRule = types.Pointer("id = @request.auth.id && verified = true")
	c.AuthRule = types.Pointer("verified = true")
	c.OTP.Enabled = true
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	if err := ensureCollection(f.app); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := f.app.FindCollectionByNameOrId("users")
	if reloaded.AuthToken.Duration != 1200 || !reloaded.OTP.Enabled || !reloaded.PasswordAuth.Enabled || *reloaded.AuthRule != *c.AuthRule || *reloaded.UpdateRule != *c.UpdateRule {
		t.Fatal("Shopify setup overwrote the application's auth policies")
	}
}

func TestShopifyDoesNotClaimAnExistingPasswordAccountByEmail(t *testing.T) {
	f := setup(t, "", "")
	native := nativeSignup(f, "camille@example.test")
	f.loginShop()
	f.expect(f.completeCallback(f.authorize("/studio")), 409)
	r, err := f.app.FindRecordById("users", native.Record["id"].(string))
	count, countErr := f.app.CountRecords(accountsCollectionName)
	if err != nil || r.Email() != "camille@example.test" || countErr != nil || count != 0 {
		t.Fatal("password account was silently linked by email")
	}
}

func TestNativeAuthEnrichmentAndModifiedTokenAreHonored(t *testing.T) {
	f := setup(t, "", "")
	modifiedToken := ""
	f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
		if e.AuthMethod == core.MFAMethodOAuth2 {
			var err error
			modifiedToken, err = e.Record.NewAuthToken()
			if err != nil {
				return err
			}
			e.Token = modifiedToken
		}
		e.Meta = map[string]string{"applicationHook": "called"}
		return e.Next()
	})
	f.app.OnRecordEnrich("users").BindFunc(func(e *core.RecordEnrichEvent) error {
		e.Record.WithCustomData(true)
		e.Record.Set("applicationField", "enriched")
		return e.Next()
	})
	r := f.loginStudio()
	cookieFound := false
	for _, cookie := range r.cookies {
		if cookie.Name == f.s.cookieName("session") {
			cookieFound = cookie.Value == modifiedToken && modifiedToken != ""
		}
	}
	if !cookieFound {
		t.Fatal("browser cookie ignored the auth hook's token")
	}
	result := decodeAuth(t, f.request("POST", f.studio.URL+"/studio/session", f.studio.URL, "", ""))
	if result.Meta["applicationHook"] != "called" || result.Record["applicationField"] != "enriched" {
		t.Fatal("SDK bootstrap skipped native auth or enrich hooks")
	}
}
