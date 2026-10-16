package shopifysso

import (
	"database/sql"
	"errors"
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
