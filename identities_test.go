package shopifysso

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func privateIdentity(f *fixture, user string) *core.Record {
	f.t.Helper()
	a, err := f.app.FindFirstRecordByFilter(identitiesCollectionName, "user = {:user}", dbx.Params{"user": user})
	if err != nil {
		f.t.Fatal("missing private Shopify identity:", err)
	}
	return a
}

func TestSetupRequiresTheHostUsersCollection(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.ResetBootstrapState() })
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(users); err != nil {
		t.Fatal(err)
	}
	if err := ensureCollection(app, "users"); err == nil {
		t.Fatal("setup silently accepted a missing host auth collection")
	}
	if _, err := app.FindCollectionByNameOrId("users"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("setup created users or chose another auth collection")
	}
	if _, err := app.FindCollectionByNameOrId(identitiesCollectionName); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed setup provisioned a partial identities schema")
	}
}

func TestSetupRejectsNonAuthAndSuperuserCollections(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	base := core.NewBaseCollection("documents")
	if err := app.Save(base); err != nil {
		t.Fatal(err)
	}
	superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{base.Name, base.Id, superusers.Name, superusers.Id} {
		if err := ensureCollection(app, target); err == nil {
			t.Fatalf("setup accepted invalid auth collection %q", target)
		}
	}
	if _, err := app.FindCollectionByNameOrId(identitiesCollectionName); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("invalid setup created private storage")
	}
}

func TestSetupPreservesUserSchemaIdentityAndNativeLinks(t *testing.T) {
	f := setup(t, "")
	f.login()
	token, user := f.authRecord()
	documentsCollection(f)
	doc := createDocument(f, token, user.Id)
	a := privateIdentity(f, user.Id)
	link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
	if err != nil {
		t.Fatal(err)
	}
	linkID := link.Id
	users, _ := f.app.FindCollectionByNameOrId("users")
	users.Fields.Add(&core.TextField{Name: "shopify_sub"})
	users.ViewRule = types.Pointer("id = @request.auth.id && shopify_sub != ''")
	if err := f.app.Save(users); err != nil {
		t.Fatal(err)
	}
	user, _ = f.app.FindRecordById("users", user.Id)
	user.Set("shopify_sub", "host-owned-value")
	if err := f.app.Save(user); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ensureCollection(f.app, "users"); err != nil {
			t.Fatal(err)
		}
	}
	users, _ = f.app.FindCollectionByNameOrId("users")
	user, _ = f.app.FindRecordById("users", user.Id)
	if users.Fields.GetByName("shopify_sub") == nil || user.GetString("shopify_sub") != "host-owned-value" || *users.ViewRule != "id = @request.auth.id && shopify_sub != ''" || privateIdentity(f, user.Id).Id != a.Id {
		t.Fatal("setup changed the host schema, data or identity")
	}
	link, err = f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
	if err != nil || link.Id != linkID {
		t.Fatal("setup replaced the native OAuth link")
	}
	f.expect(f.request("GET", f.api.URL+"/api/collections/documents/records/"+doc, "", "", token), 200)
	if result := decodeAuth(t, f.login()); result.Record["id"] != user.Id || result.Meta["isNew"] != false {
		t.Fatal("setup changed the account used by native OAuth")
	}
	nativeSignup(f, "password-only@example.test")
	count, err := f.app.CountRecords(identitiesCollectionName)
	if err != nil || count != 1 {
		t.Fatal("setup duplicated an identity or linked a password account")
	}
}

func TestShopifyIdentitiesAreNotPublicThroughRecordsOrExpansion(t *testing.T) {
	f := setup(t, "")
	result := f.login()
	token, user := f.authRecord()
	a := privateIdentity(f, user.Id)
	c := a.Collection()
	if !c.IsBase() || !c.System || c.ListRule != nil || c.ViewRule != nil || c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
		t.Fatal("private Shopify collection has a public API rule")
	}
	for _, collection := range []string{c.Name, c.Id} {
		base := f.api.URL + "/api/collections/" + collection + "/records"
		for _, auth := range []string{"", token} {
			f.expect(f.request("GET", base, "", "", auth), 403)
			f.expect(f.request("GET", base+"/"+a.Id, "", "", auth), 403)
			f.expect(f.request("POST", base, "", string(mustJSON(t, a.PublicExport())), auth), 403)
			f.expect(f.request("PATCH", base+"/"+a.Id, "", `{"subject":"forged"}`, auth), 403)
			f.expect(f.request("DELETE", base+"/"+a.Id, "", "", auth), 403)
		}
	}
	profile := f.request("GET", f.api.URL+"/api/collections/users/records/"+user.Id+"?expand=_shopifyIdentities_via_user", "", "", token)
	f.expect(profile, 200)
	for _, body := range []string{result.body, profile.body} {
		if strings.Contains(body, a.GetString("subject")) || strings.Contains(body, a.GetString("customerId")) {
			t.Fatal("raw Shopify identifier leaked in OAuth or profile expansion")
		}
	}
}

func TestSystemCollectionProtectionsRemainNative(t *testing.T) {
	f := setup(t, "")
	c, _ := f.app.FindCollectionByNameOrId(identitiesCollectionName)
	if err := f.app.Delete(c); err == nil {
		t.Fatal("system collection could be deleted")
	}
	c.Name = "renamedIdentities"
	if err := f.app.Save(c); err == nil {
		t.Fatal("system collection could be renamed")
	}
	c.Name = identitiesCollectionName
	c.ListRule = types.Pointer("")
	if err := f.app.Save(c); err == nil {
		t.Fatal("system collection API rule could be opened")
	}
}

func TestShopifyUnlinkRemovesOnlyTheMatchingPrivateIdentity(t *testing.T) {
	for _, collection := range []string{"users", "renamedMembers"} {
		t.Run(collection, func(t *testing.T) {
			f := setupCollection(t, "", "users", false)
			if collection != "users" {
				c, _ := f.app.FindCollectionByNameOrId(f.s.collectionID)
				c.Name = collection
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
				f.s.cfg.AuthCollection = collection
			}
			f.login()
			_, user := f.authRecord()
			user.SetPassword("TestPassword12345")
			if err := f.app.Save(user); err != nil {
				t.Fatal(err)
			}
			token, err := user.NewAuthToken()
			if err != nil {
				t.Fatal(err)
			}
			identity := privateIdentity(f, user.Id)
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
			if err != nil {
				t.Fatal(err)
			}
			if err := saveIdentity(f.app, user.Id, "https://other.example.test/identity", "other-subject", "other-customer", nil); err != nil {
				t.Fatal(err)
			}
			other, err := f.app.FindFirstRecordByFilter(identitiesCollectionName,
				"issuer = 'https://other.example.test/identity'")
			if err != nil {
				t.Fatal(err)
			}
			endpoint := f.api.URL + "/api/collections/_externalAuths/records/" + link.Id
			f.expect(f.request("DELETE", endpoint, "", "", ""), 404)
			f.expect(f.request("DELETE", endpoint, "", "", token), 204)
			for _, target := range []struct{ collection, id string }{
				{core.CollectionNameExternalAuths, link.Id}, {identitiesCollectionName, identity.Id},
			} {
				if _, err := f.app.FindRecordById(target.collection, target.id); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("unlink kept an identity link", err)
				}
			}
			if _, err := f.app.FindRecordById(identitiesCollectionName, other.Id); err != nil {
				t.Fatal("unlink removed another issuer's private identity", err)
			}
			password := f.request("POST", f.api.URL+"/api/collections/"+collection+"/auth-with-password", "",
				`{"identity":"camille@example.test","password":"TestPassword12345"}`, "")
			f.expect(password, 200)
			if decodeAuth(t, password).Record["id"] != user.Id {
				t.Fatal("unlink changed the password account")
			}
			f.loginStore()
			f.expect(f.completeCallback(f.authorize(false)), 409)
			f.mock.ProfileEmail = "new-account@example.test"
			result := decodeAuth(t, f.login())
			if result.Record["id"] == user.Id || result.Meta["isNew"] != true {
				t.Fatal("unlinked Shopify identity recovered its previous account")
			}
		})
	}
}

func TestShopifyUnlinkFailureKeepsBothIdentityLinks(t *testing.T) {
	for _, stage := range []string{"native_delete", "stopped"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "")
			f.login()
			token, user := f.authRecord()
			identity := privateIdentity(f, user.Id)
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
			if err != nil {
				t.Fatal(err)
			}
			status := http.StatusBadRequest
			if stage == "native_delete" {
				f.app.OnRecordDelete(core.CollectionNameExternalAuths).BindFunc(func(e *core.RecordEvent) error {
					return errors.New("storage unavailable")
				})
			} else {
				status = http.StatusNoContent
				f.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).BindFunc(func(e *core.RecordRequestEvent) error {
					return e.NoContent(http.StatusNoContent)
				})
			}
			f.expect(f.request("DELETE", f.api.URL+"/api/collections/_externalAuths/records/"+link.Id, "", "", token), status)
			for _, target := range []struct{ collection, id string }{
				{core.CollectionNameExternalAuths, link.Id}, {identitiesCollectionName, identity.Id},
			} {
				if _, err := f.app.FindRecordById(target.collection, target.id); err != nil {
					t.Fatal("failed unlink lost an identity link", err)
				}
			}
		})
	}
}

func TestShopifyUnlinkLeavesOtherProvidersUnchanged(t *testing.T) {
	f := setup(t, "")
	f.login()
	token, user := f.authRecord()
	identity := privateIdentity(f, user.Id)
	link := core.NewExternalAuth(f.app)
	link.SetCollectionRef(user.Collection().Id)
	link.SetRecordRef(user.Id)
	link.SetProvider("google")
	link.SetProviderId(oauthLinkProviderID(identity.GetString("issuer"), identity.GetString("subject")))
	if err := f.app.Save(link); err != nil {
		t.Fatal(err)
	}
	f.expect(f.request("DELETE", f.api.URL+"/api/collections/_externalAuths/records/"+link.Id, "", "", token), 204)
	if privateIdentity(f, user.Id).Id != identity.Id {
		t.Fatal("another OAuth unlink changed the Shopify identity")
	}
	if result := decodeAuth(t, f.login()); result.Record["id"] != user.Id {
		t.Fatal("another OAuth unlink changed the Shopify account")
	}
}

func TestShopifyUnlinkWorksWhileDiscoveryIsUnavailable(t *testing.T) {
	f := setupWithStoreHandler(t, "", "users", false, func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Shopify unavailable", http.StatusServiceUnavailable)
		})
	})
	users, _ := f.app.FindCollectionByNameOrId("users")
	user := core.NewRecord(users)
	user.SetEmail("existing@example.test")
	user.SetPassword("TestPassword12345")
	if err := f.app.Save(user); err != nil {
		t.Fatal(err)
	}
	issuer, subject := f.store.URL+"/identity", "previous-shopify-subject"
	if err := saveIdentity(f.app, user.Id, issuer, subject, "previous-customer", nil); err != nil {
		t.Fatal(err)
	}
	link := core.NewExternalAuth(f.app)
	link.SetCollectionRef(user.Collection().Id)
	link.SetRecordRef(user.Id)
	link.SetProvider(providerName)
	link.SetProviderId(oauthLinkProviderID(issuer, subject))
	if err := f.app.Save(link); err != nil {
		t.Fatal(err)
	}
	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}
	f.expect(f.request("DELETE", f.api.URL+"/api/collections/"+link.Collection().Id+"/records/"+link.Id, "", "", token), 204)
	if count, err := f.app.CountRecords(identitiesCollectionName); err != nil || count != 0 {
		t.Fatal("unlink during a discovery outage kept a private identity", err)
	}
}

func TestNativeAccountRecoveryRevokesShopifyIdentity(t *testing.T) {
	for _, action := range []string{"verification", "password_reset"} {
		t.Run(action, func(t *testing.T) {
			f := setup(t, "email")
			f.login()
			oldToken, user := f.authRecord()
			identityRecordID := privateIdentity(f, user.Id).Id
			var token string
			var err error
			endpoint := "/confirm-verification"
			if action != "password_reset" {
				token, err = user.NewVerificationToken()
			} else {
				token, err = user.NewPasswordResetToken()
				endpoint = "/confirm-password-reset"
			}
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"token": token, "password": "OwnerPassword12345", "passwordConfirm": "OwnerPassword12345"})
			f.expect(f.request("POST", f.api.URL+"/api/collections/users"+endpoint, "", string(body), ""), 204)
			if _, err := f.app.FindRecordById(identitiesCollectionName, identityRecordID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("native recovery kept the revoked private identity", err)
			}
			f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-refresh", "", "", oldToken), http.StatusUnauthorized)
			f.loginStore()
			f.expect(f.completeCallback(f.authorize(false)), 409)
			if count, err := f.app.CountRecords(core.CollectionNameExternalAuths); err != nil || count != 0 {
				t.Fatal("revoked Shopify login recreated an OAuth link", err)
			}
		})
	}
}

func TestNativeRecoveryRollsBackWhenShopifyRevocationFails(t *testing.T) {
	f := setup(t, "email")
	f.login()
	oldToken, user := f.authRecord()
	identityRecordID := privateIdentity(f, user.Id).Id
	f.app.OnRecordDelete(identitiesCollectionName).BindFunc(func(e *core.RecordEvent) error {
		return errors.New("private storage unavailable")
	})
	token, err := user.NewVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"token": token})
	f.expect(f.request("POST", f.api.URL+"/api/collections/users/confirm-verification", "", string(body), ""), 500)
	after, err := f.app.FindRecordById("users", user.Id)
	if err != nil || after.Verified() || after.TokenKey() != user.TokenKey() || privateIdentity(f, user.Id).Id != identityRecordID {
		t.Fatal("failed recovery persisted verification, rotated the session key, or lost the private identity", err)
	}
	f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-refresh", "", "", oldToken), http.StatusOK)
	if _, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id}); err != nil {
		t.Fatal("failed recovery lost the OAuth link", err)
	}
}

func identityStoreApp(t *testing.T) (core.App, *core.Collection) {
	t.Helper()
	app := committedResponseApp(t)
	accounts, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureIdentityStore(app, accounts); err != nil {
		t.Fatal(err)
	}
	if err := ensureIdentityStore(app, accounts); err != nil {
		t.Fatal(err)
	}
	bindIdentityRevocation(app, accounts.Id)
	return app, accounts
}

func newAccount(t *testing.T, app core.App, accounts *core.Collection, email string, verified bool) *core.Record {
	t.Helper()
	r := core.NewRecord(accounts)
	r.SetEmail(email)
	r.SetPassword("TestPassword12345")
	r.SetVerified(verified)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func newOAuthLink(t *testing.T, app core.App, account *core.Record, provider, providerID string) *core.ExternalAuth {
	t.Helper()
	link := core.NewExternalAuth(app)
	link.SetCollectionRef(account.Collection().Id)
	link.SetRecordRef(account.Id)
	link.SetProvider(provider)
	link.SetProviderId(providerID)
	if err := app.Save(link); err != nil {
		t.Fatal(err)
	}
	return link
}

func identityCount(t *testing.T, app core.App, account string) int64 {
	t.Helper()
	count, err := app.CountRecords(identitiesCollectionName, dbx.HashExp{"user": account})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

const (
	testIssuer  = "https://store.example.test/authentication"
	otherIssuer = "https://other.example.test/authentication"
)

func TestIdentityStoreFindAndSave(t *testing.T) {
	app, accounts := identityStoreApp(t)
	camille := newAccount(t, app, accounts, "camille@example.test", true)
	other := newAccount(t, app, accounts, "other@example.test", true)

	if found, err := findIdentity(app, testIssuer, "42"); err != nil || found != nil {
		t.Fatal("unknown customer has an identity", found, err)
	}
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
		t.Fatal(err)
	}
	found, err := findIdentity(app, testIssuer, "42")
	if err != nil || found == nil || found.account() != camille.Id {
		t.Fatal("saved identity not found for its account", err)
	}
	if other, err := findIdentity(app, otherIssuer, "42"); err != nil || other != nil {
		t.Fatal("the same customer ID from another store matched", err)
	}
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/43", nil); err != nil {
		t.Fatal(err)
	}
	if identityCount(t, app, camille.Id) != 1 || privateIdentityOf(t, app, camille.Id).GetString("customerId") != "gid://shopify/Customer/43" {
		t.Fatal("saving an existing identity did not update it in place")
	}
	if err := saveIdentity(app, other.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err == nil {
		t.Fatal("a Shopify identity was moved to another account")
	}
	if identityCount(t, app, other.Id) != 0 {
		t.Fatal("rejected save left an identity")
	}
}

func TestIdentityStoreRestoresPreviousIdentity(t *testing.T) {
	app, accounts := identityStoreApp(t)
	camille := newAccount(t, app, accounts, "camille@example.test", true)
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
		t.Fatal(err)
	}
	previous, err := findIdentity(app, testIssuer, "42")
	if err != nil || previous == nil {
		t.Fatal(err)
	}
	id := privateIdentityOf(t, app, camille.Id).Id
	if err := app.Delete(privateIdentityOf(t, app, camille.Id)); err != nil {
		t.Fatal(err)
	}
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", previous); err != nil {
		t.Fatal(err)
	}
	if privateIdentityOf(t, app, camille.Id).Id != id {
		t.Fatal("the login did not restore its own identity")
	}
}

func TestIdentityRevocationWithOAuthLink(t *testing.T) {
	for _, c := range []struct {
		name     string
		provider string
		owner    string
		blocked  bool
		failure  bool
		revoked  bool
	}{
		{"matching Shopify link", providerName, "", false, false, true},
		{"deletion stopped by host", providerName, "", true, false, false},
		{"identity deletion failure", providerName, "", false, true, false},
		{"other provider", "google", "", false, false, false},
		{"Shopify link of another auth collection", providerName, "members", false, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, accounts := identityStoreApp(t)
			camille := newAccount(t, app, accounts, "camille@example.test", true)
			for _, issuer := range []string{testIssuer, otherIssuer} {
				if err := saveIdentity(app, camille.Id, issuer, "42", "gid://shopify/Customer/42", nil); err != nil {
					t.Fatal(err)
				}
			}
			owner := camille
			if c.owner != "" {
				members := core.NewAuthCollection(c.owner)
				if err := app.Save(members); err != nil {
					t.Fatal(err)
				}
				owner = newAccount(t, app, members, "member@example.test", true)
			}
			link := newOAuthLink(t, app, owner, c.provider, oauthLinkProviderID(testIssuer, "42"))
			if c.blocked {
				app.OnRecordDeleteExecute(core.CollectionNameExternalAuths).BindFunc(func(*core.RecordEvent) error {
					return nil
				})
			}
			if c.failure {
				app.OnRecordDelete(identitiesCollectionName).BindFunc(func(*core.RecordEvent) error {
					return errors.New("identity storage unavailable")
				})
				if err := app.Delete(link); err == nil {
					t.Fatal("identity deletion failure did not fail the link deletion")
				}
				if _, err := app.FindRecordById(core.CollectionNameExternalAuths, link.Id); err != nil {
					t.Fatal("failed revocation lost the OAuth link", err)
				}
			} else if err := app.Delete(link); err != nil {
				t.Fatal(err)
			}
			remaining, err := findIdentity(app, testIssuer, "42")
			if err != nil || (remaining == nil) != c.revoked {
				t.Fatalf("identity revoked = %v, want %v (%v)", remaining == nil, c.revoked, err)
			}
			if kept, err := findIdentity(app, otherIssuer, "42"); err != nil || kept == nil {
				t.Fatal("revocation removed the identity from another store", err)
			}
		})
	}
}

func TestIdentityRevocationEdgeCases(t *testing.T) {
	app, accounts := identityStoreApp(t)
	camille := newAccount(t, app, accounts, "camille@example.test", true)
	if err := app.Delete(newOAuthLink(t, app, camille, providerName, oauthLinkProviderID(testIssuer, "42"))); err != nil {
		t.Fatal(err)
	}
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
		t.Fatal(err)
	}
	if err := app.Delete(camille); err != nil {
		t.Fatal(err)
	}
	if identityCount(t, app, camille.Id) != 0 {
		t.Fatal("deleting an account left its Shopify identity behind")
	}
}

func TestIdentityRevocationOnVerification(t *testing.T) {
	for _, c := range []struct {
		name       string
		identities bool
		failure    bool
	}{
		{"with identities", true, false},
		{"without identities", false, false},
		{"revocation failure", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, accounts := identityStoreApp(t)
			camille := newAccount(t, app, accounts, "camille@example.test", false)
			if c.identities {
				for _, issuer := range []string{testIssuer, otherIssuer} {
					if err := saveIdentity(app, camille.Id, issuer, "42", "gid://shopify/Customer/42", nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			if c.failure {
				app.OnRecordDelete(identitiesCollectionName).BindFunc(func(*core.RecordEvent) error {
					return errors.New("identity storage unavailable")
				})
			}
			tokenKey := camille.TokenKey()
			camille.SetVerified(true)
			err := app.Save(camille)
			after, findErr := app.FindRecordById(accounts.Id, camille.Id)
			if findErr != nil {
				t.Fatal(findErr)
			}
			if c.failure {
				if err == nil || after.Verified() || after.TokenKey() != tokenKey || identityCount(t, app, camille.Id) != 2 {
					t.Fatal("failed revocation did not roll back the verification")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if identityCount(t, app, camille.Id) != 0 {
				t.Fatal("verification kept a Shopify identity")
			}
			if rotated := after.TokenKey() != tokenKey; rotated != c.identities {
				t.Fatalf("sessions invalidated = %v, want %v", rotated, c.identities)
			}
		})
	}
}

func privateIdentityOf(t *testing.T, app core.App, account string) *core.Record {
	t.Helper()
	r, err := app.FindFirstRecordByFilter(identitiesCollectionName, "user = {:user} && issuer = {:issuer}", dbx.Params{"user": account, "issuer": testIssuer})
	if err != nil {
		t.Fatal("missing private Shopify identity:", err)
	}
	return r
}
