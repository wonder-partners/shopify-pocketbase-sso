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

func privateAccount(f *fixture, user string) *core.Record {
	f.t.Helper()
	a, err := f.app.FindFirstRecordByFilter(accountsCollectionName, "user = {:user}", dbx.Params{"user": user})
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
	if _, err := app.FindCollectionByNameOrId(accountsCollectionName); !errors.Is(err, sql.ErrNoRows) {
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
	if _, err := app.FindCollectionByNameOrId(accountsCollectionName); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("invalid setup created private storage")
	}
}

func TestSetupPreservesUserSchemaIdentityAndNativeLinks(t *testing.T) {
	f := setup(t, "")
	f.login()
	token, user := f.authRecord()
	documentsCollection(f)
	doc := createDocument(f, token, user.Id)
	a := privateAccount(f, user.Id)
	link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
	if err != nil {
		t.Fatal(err)
	}
	linkID := link.Id
	users, _ := f.app.FindCollectionByNameOrId("users")
	// Even old field names now belong to the host, not to this integration.
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
	if users.Fields.GetByName("shopify_sub") == nil || user.GetString("shopify_sub") != "host-owned-value" || *users.ViewRule != "id = @request.auth.id && shopify_sub != ''" || privateAccount(f, user.Id).Id != a.Id {
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
	count, err := f.app.CountRecords(accountsCollectionName)
	if err != nil || count != 1 {
		t.Fatal("setup duplicated an identity or linked a password account")
	}
}

func TestPrivateAccountsAreNotPublicThroughRecordsOrExpansion(t *testing.T) {
	f := setup(t, "")
	result := f.login()
	token, user := f.authRecord()
	a := privateAccount(f, user.Id)
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
	c, _ := f.app.FindCollectionByNameOrId(accountsCollectionName)
	if err := f.app.Delete(c); err == nil {
		t.Fatal("system collection could be deleted")
	}
	c.Name = "renamedIdentities"
	if err := f.app.Save(c); err == nil {
		t.Fatal("system collection could be renamed")
	}
	c.Name = accountsCollectionName
	c.ListRule = types.Pointer("")
	if err := f.app.Save(c); err == nil {
		t.Fatal("system collection API rule could be opened")
	}
}

func TestPrivateAccountRecoveryUsesOnlyTheNativeIdentityLink(t *testing.T) {
	f := setup(t, "")
	f.login()
	_, user := f.authRecord()
	if err := f.app.Delete(privateAccount(f, user.Id)); err != nil {
		t.Fatal(err)
	}
	if result := decodeAuth(t, f.login()); result.Record["id"] != user.Id || result.Meta["isNew"] != false {
		t.Fatal("exact native OAuth link did not recover the private identity")
	}
	other := nativeSignup(f, "other-owner@example.test")
	a := privateAccount(f, user.Id)
	a.Set("user", other.Record["id"])
	if err := f.app.Save(a); err != nil {
		t.Fatal(err)
	}
	f.expect(f.completeCallback(f.authorize(false)), 403)
}

func TestPrivateAccountDeletionCascadesFromUser(t *testing.T) {
	f := setup(t, "")
	f.login()
	_, user := f.authRecord()
	a := privateAccount(f, user.Id)
	if err := f.app.Delete(user); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.FindRecordById(accountsCollectionName, a.Id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleting a user left its private Shopify identity behind")
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
			account := privateAccount(f, user.Id)
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
			if err != nil {
				t.Fatal(err)
			}
			// A historical identity from a different issuer must stay untouched.
			if err := putAccount(f.app, user.Id, "https://other.example.test/identity", "other-subject", "other-customer", nil); err != nil {
				t.Fatal(err)
			}
			other, err := f.app.FindFirstRecordByFilter(accountsCollectionName,
				"issuer = 'https://other.example.test/identity'")
			if err != nil {
				t.Fatal(err)
			}
			endpoint := f.api.URL + "/api/collections/_externalAuths/records/" + link.Id
			f.expect(f.request("DELETE", endpoint, "", "", ""), 404)
			f.expect(f.request("DELETE", endpoint, "", "", token), 204)
			for _, target := range []struct{ collection, id string }{
				{core.CollectionNameExternalAuths, link.Id}, {accountsCollectionName, account.Id},
			} {
				if _, err := f.app.FindRecordById(target.collection, target.id); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("unlink kept an identity link", err)
				}
			}
			if _, err := f.app.FindRecordById(accountsCollectionName, other.Id); err != nil {
				t.Fatal("unlink removed another issuer's private identity", err)
			}
			password := f.request("POST", f.api.URL+"/api/collections/"+collection+"/auth-with-password", "",
				`{"identity":"camille@example.test","password":"TestPassword12345"}`, "")
			f.expect(password, 200)
			if decodeAuth(t, password).Record["id"] != user.Id {
				t.Fatal("unlink changed the password account")
			}
			f.loginShop()
			f.expect(f.completeCallback(f.authorize(false)), 409)
			// A new email may create a new account, but cannot recover the old one.
			f.mock.ProfileEmail = "new-account@example.test"
			result := decodeAuth(t, f.login())
			if result.Record["id"] == user.Id || result.Meta["isNew"] != true {
				t.Fatal("unlinked Shopify identity recovered its previous account")
			}
		})
	}
}

func TestShopifyUnlinkFailureKeepsBothIdentityLinks(t *testing.T) {
	for _, stage := range []string{"private_delete", "native_delete", "denied_after", "http_error_after", "stopped"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "")
			f.login()
			token, user := f.authRecord()
			account := privateAccount(f, user.Id)
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
			if err != nil {
				t.Fatal(err)
			}
			switch stage {
			case "private_delete", "native_delete":
				collection := accountsCollectionName
				if stage == "native_delete" {
					collection = core.CollectionNameExternalAuths
				}
				f.app.OnRecordDelete(collection).BindFunc(func(e *core.RecordEvent) error {
					return errors.New("storage unavailable")
				})
			default:
				f.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).BindFunc(func(e *core.RecordRequestEvent) error {
					if stage == "stopped" {
						return e.NoContent(http.StatusNoContent)
					}
					if err := e.Next(); err != nil {
						return err
					}
					if stage == "http_error_after" {
						return e.JSON(http.StatusForbidden, map[string]string{"message": "Unlink denied."})
					}
					return e.ForbiddenError("Unlink denied.", nil)
				})
			}
			status := http.StatusForbidden
			switch stage {
			case "private_delete":
				status = http.StatusInternalServerError
			case "native_delete":
				status = http.StatusBadRequest
			case "stopped":
				status = http.StatusNoContent
			}
			f.expect(f.request("DELETE", f.api.URL+"/api/collections/_externalAuths/records/"+link.Id, "", "", token), status)
			for _, target := range []struct{ collection, id string }{
				{core.CollectionNameExternalAuths, link.Id}, {accountsCollectionName, account.Id},
			} {
				if _, err := f.app.FindRecordById(target.collection, target.id); err != nil {
					t.Fatal("failed unlink lost an identity link", err)
				}
			}
		})
	}
}

func TestShopifyUnlinkLeavesOtherProvidersAndCollectionsUnchanged(t *testing.T) {
	for _, provider := range []string{"google", providerName} {
		t.Run(provider, func(t *testing.T) {
			f := setup(t, "")
			f.login()
			token, user := f.authRecord()
			account := privateAccount(f, user.Id)
			owner := user
			if provider == providerName {
				members := core.NewAuthCollection("members")
				if err := f.app.Save(members); err != nil {
					t.Fatal(err)
				}
				owner = core.NewRecord(members)
				owner.SetEmail("other@example.test")
				owner.SetPassword("TestPassword12345")
				if err := f.app.Save(owner); err != nil {
					t.Fatal(err)
				}
				var err error
				token, err = owner.NewAuthToken()
				if err != nil {
					t.Fatal(err)
				}
			}
			link := core.NewExternalAuth(f.app)
			link.SetCollectionRef(owner.Collection().Id)
			link.SetRecordRef(owner.Id)
			link.SetProvider(provider)
			link.SetProviderId(identityID(account.GetString("issuer"), account.GetString("subject")))
			if err := f.app.Save(link); err != nil {
				t.Fatal(err)
			}
			endpoint := f.api.URL + "/api/collections/_externalAuths/records/" + link.Id
			if provider == providerName {
				f.expect(f.request("DELETE", endpoint, "", "", f.auth.Token), 404)
			}
			f.expect(f.request("DELETE", endpoint, "", "", token), 204)
			if privateAccount(f, user.Id).Id != account.Id {
				t.Fatal("another OAuth unlink changed the Shopify identity")
			}
			if result := decodeAuth(t, f.login()); result.Record["id"] != user.Id {
				t.Fatal("another OAuth unlink changed the Shopify account")
			}
		})
	}
}

func TestShopifyUnlinkWorksWhileDiscoveryIsUnavailable(t *testing.T) {
	for _, withPrivateIdentity := range []bool{false, true} {
		name := "missing_private_identity"
		if withPrivateIdentity {
			name = "existing_private_identity"
		}
		t.Run(name, func(t *testing.T) {
			f := setupWithShopHandler(t, "", "users", false, func(http.Handler) http.Handler {
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
			issuer, subject := f.shop.URL+"/identity", "previous-shopify-subject"
			if withPrivateIdentity {
				if err := putAccount(f.app, user.Id, issuer, subject, "previous-customer", nil); err != nil {
					t.Fatal(err)
				}
			}
			link := core.NewExternalAuth(f.app)
			link.SetCollectionRef(user.Collection().Id)
			link.SetRecordRef(user.Id)
			link.SetProvider(providerName)
			link.SetProviderId(identityID(issuer, subject))
			if err := f.app.Save(link); err != nil {
				t.Fatal(err)
			}
			token, err := user.NewAuthToken()
			if err != nil {
				t.Fatal(err)
			}
			f.expect(f.request("DELETE", f.api.URL+"/api/collections/"+link.Collection().Id+"/records/"+link.Id, "", "", token), 204)
			if count, err := f.app.CountRecords(accountsCollectionName); err != nil || count != 0 {
				t.Fatal("unlink during a discovery outage kept a private identity", err)
			}
		})
	}
}

func TestNativeAccountRecoveryRevokesShopifyIdentity(t *testing.T) {
	for _, action := range []string{"verification", "password_reset", "orphaned_identity"} {
		t.Run(action, func(t *testing.T) {
			f := setup(t, "email")
			f.login()
			oldToken, user := f.authRecord()
			accountID := privateAccount(f, user.Id).Id
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
			if action == "orphaned_identity" {
				// Reproduce an older database where verification had deleted the
				// native link without clearing the private identity.
				if _, err := f.app.DB().NewQuery("DELETE FROM _externalAuths").Execute(); err != nil {
					t.Fatal(err)
				}
			}
			body, _ := json.Marshal(map[string]string{"token": token, "password": "OwnerPassword12345", "passwordConfirm": "OwnerPassword12345"})
			f.expect(f.request("POST", f.api.URL+"/api/collections/users"+endpoint, "", string(body), ""), 204)
			if _, err := f.app.FindRecordById(accountsCollectionName, accountID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("native recovery kept the revoked private identity", err)
			}
			f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-refresh", "", "", oldToken), http.StatusUnauthorized)
			f.loginShop()
			f.expect(f.completeCallback(f.authorize(false)), 409)
			if count, err := f.app.CountRecords(core.CollectionNameExternalAuths); err != nil || count != 0 {
				t.Fatal("revoked Shopify login recreated an OAuth link", err)
			}
		})
	}
}

func TestNativeRecoveryRollsBackWhenShopifyRevocationFails(t *testing.T) {
	for _, scenario := range []string{"native_link", "orphaned_identity"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "email")
			f.login()
			oldToken, user := f.authRecord()
			accountID := privateAccount(f, user.Id).Id
			if scenario == "orphaned_identity" {
				if _, err := f.app.DB().NewQuery("DELETE FROM _externalAuths").Execute(); err != nil {
					t.Fatal(err)
				}
			}
			f.app.OnRecordDelete(accountsCollectionName).BindFunc(func(e *core.RecordEvent) error {
				return errors.New("private storage unavailable")
			})
			token, err := user.NewVerificationToken()
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(map[string]string{"token": token})
			f.expect(f.request("POST", f.api.URL+"/api/collections/users/confirm-verification", "", string(body), ""), 500)
			after, err := f.app.FindRecordById("users", user.Id)
			if err != nil || after.Verified() || after.TokenKey() != user.TokenKey() || privateAccount(f, user.Id).Id != accountID {
				t.Fatal("failed recovery persisted verification, rotated the session key, or lost the private identity", err)
			}
			f.expect(f.request("POST", f.api.URL+"/api/collections/users/auth-refresh", "", "", oldToken), http.StatusOK)
			if scenario == "native_link" {
				if _, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id}); err != nil {
					t.Fatal("failed recovery lost the native identity", err)
				}
			}
		})
	}
}

func TestShopifyUnlinkBuffersProjectResponsesUntilCommit(t *testing.T) {
	for _, scenario := range []string{"storage_error", "commit_failure", "success", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "")
			f.login()
			token, user := f.authRecord()
			accountID := privateAccount(f, user.Id).Id
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "storage_error" {
				f.app.OnRecordDelete(accountsCollectionName).BindFunc(func(e *core.RecordEvent) error {
					return errors.New("private storage unavailable")
				})
			}
			if scenario == "commit_failure" {
				for _, query := range []string{
					"CREATE TABLE unlink_parent (id TEXT PRIMARY KEY)",
					"CREATE TABLE unlink_child (parent_id TEXT REFERENCES unlink_parent(id) DEFERRABLE INITIALLY DEFERRED)",
				} {
					if _, err := f.app.DB().NewQuery(query).Execute(); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).BindFunc(func(e *core.RecordRequestEvent) error {
				if scenario == "storage_error" {
					if err := e.NoContent(http.StatusNoContent); err != nil {
						return err
					}
				}
				if err := e.Next(); err != nil {
					return err
				}
				if scenario == "commit_failure" {
					if _, err := e.App.DB().NewQuery("INSERT INTO unlink_child (parent_id) VALUES ('missing')").Execute(); err != nil {
						return err
					}
					return e.NoContent(http.StatusNoContent)
				}
				e.Response.Header().Set("X-Project-Unlink", "custom")
				if scenario == "rejected" {
					return e.JSON(http.StatusForbidden, map[string]string{"message": "Project denied unlink."})
				}
				return e.JSON(http.StatusOK, map[string]bool{"unlinked": true})
			})
			result := f.request("DELETE", f.api.URL+"/api/collections/_externalAuths/records/"+link.Id, "", "", token)
			status := http.StatusInternalServerError
			if scenario == "success" {
				status = http.StatusOK
			} else if scenario == "rejected" {
				status = http.StatusForbidden
			} else if scenario == "commit_failure" {
				status = http.StatusBadRequest
			}
			f.expect(result, status)
			for _, target := range []struct{ collection, id string }{
				{core.CollectionNameExternalAuths, link.Id}, {accountsCollectionName, accountID},
			} {
				_, err := f.app.FindRecordById(target.collection, target.id)
				if scenario == "success" {
					if !errors.Is(err, sql.ErrNoRows) {
						t.Fatal("successful unlink kept an identity", err)
					}
				} else if err != nil {
					t.Fatal("failed unlink lost an identity", err)
				}
			}
			if scenario == "success" && (result.body != `{"unlinked":true}` || result.headers.Get("X-Project-Unlink") != "custom") {
				t.Fatal("committed unlink lost its custom response")
			}
			if scenario == "rejected" && !strings.Contains(result.body, "Project denied unlink.") {
				t.Fatal("rejected unlink lost its project response")
			}
		})
	}
}
