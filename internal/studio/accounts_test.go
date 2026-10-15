package studio

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func privateAccount(f *fixture, user string) *core.Record {
	f.t.Helper()
	a, err := f.app.FindFirstRecordByFilter(accountsCollectionName, "user = {:user}", dbx.Params{"user": user})
	if err != nil {
		f.t.Fatal("missing private Shopify account:", err)
	}
	return a
}

func addLegacyIdentitySchema(f *fixture) *core.Collection {
	f.t.Helper()
	c, err := f.app.FindCollectionByNameOrId("users")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, field := range legacyIdentityFields {
		c.Fields.Add(&core.TextField{Name: field, Max: 1000})
	}
	c.AddIndex("idx_"+c.Id+"_shopify_identity", true, "shopify_issuer, shopify_sub", "shopify_issuer != '' AND shopify_sub != ''")
	if err := f.app.Save(c); err != nil {
		f.t.Fatal(err)
	}
	return c
}

func TestPrivateAccountsMigrationPreservesIdentityAndNativeLinks(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	token, user := f.session()
	documentsCollection(f)
	doc := createDocument(f, token, user.Id)
	a := privateAccount(f, user.Id)
	link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
	if err != nil {
		t.Fatal(err)
	}
	linkID := link.Id
	c := addLegacyIdentitySchema(f)
	old, _ := f.app.FindRecordById(c, user.Id)
	old.Set("shopify_issuer", a.GetString("issuer"))
	old.Set("shopify_sub", a.GetString("subject"))
	old.Set("shopify_customer_id", a.GetString("customerId"))
	if err := f.app.Save(old); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Delete(a); err != nil {
		t.Fatal(err)
	}
	for range 2 { // Migration must also be safe on subsequent startups.
		if err := ensureCollection(f.app); err != nil {
			t.Fatal(err)
		}
	}
	got := privateAccount(f, user.Id)
	for _, field := range []string{"issuer", "subject", "customerId"} {
		if got.GetString(field) != a.GetString(field) {
			t.Fatal("migration changed a Shopify identifier")
		}
	}
	c, _ = f.app.FindCollectionByNameOrId("users")
	for _, field := range legacyIdentityFields {
		if c.Fields.GetByName(field) != nil {
			t.Fatal("migration left technical identity fields in users")
		}
	}
	link, err = f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
	if err != nil || link.Id != linkID {
		t.Fatal("migration replaced the native OAuth link")
	}
	f.expect(f.request("GET", f.studio.URL+"/api/collections/documents/records/"+doc, "", "", token), 200)
	if result := decodeAuth(t, f.loginStudio()); result.Record["id"] != user.Id || result.Meta["isNew"] != false {
		t.Fatal("migration changed the account used by native OAuth")
	}
	nativeSignup(f, "password-only@example.test")
	count, err := f.app.CountRecords(accountsCollectionName)
	if err != nil || count != 1 {
		t.Fatal("migration duplicated an identity or linked a password-only account")
	}
}

func TestPrivateAccountsMigrationRollsBackInvalidLegacyData(t *testing.T) {
	for _, fault := range []string{"incomplete_identity", "partial_schema", "identity_conflict", "rule_dependency"} {
		t.Run(fault, func(t *testing.T) {
			f := setup(t, "", "")
			native := nativeSignup(f, "legacy@example.test")
			c := addLegacyIdentitySchema(f)
			r, _ := f.app.FindRecordById(c, native.Record["id"].(string))
			r.Set("shopify_issuer", f.s.issuer)
			r.Set("shopify_sub", "legacy-subject")
			if fault != "incomplete_identity" {
				r.Set("shopify_customer_id", "gid://shopify/Customer/12345")
			}
			if err := f.app.Save(r); err != nil {
				t.Fatal(err)
			}
			if fault == "partial_schema" {
				c.Fields.RemoveByName("shopify_customer_id")
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "identity_conflict" {
				other := nativeSignup(f, "other@example.test")
				if err := putAccount(f.app, other.Record["id"].(string), f.s.issuer, "legacy-subject", "gid://shopify/Customer/12345"); err != nil {
					t.Fatal(err)
				}
			}
			if fault == "rule_dependency" {
				c.ViewRule = types.Pointer("shopify_sub != '' && id = @request.auth.id")
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := f.app.CountRecords(accountsCollectionName)
			if err := ensureCollection(f.app); err == nil {
				t.Fatal("unsafe legacy migration unexpectedly succeeded")
			}
			c, _ = f.app.FindCollectionByNameOrId("users")
			r, _ = f.app.FindRecordById(c, r.Id)
			after, _ := f.app.CountRecords(accountsCollectionName)
			if c.Fields.GetByName("shopify_sub") == nil || r.GetString("shopify_sub") != "legacy-subject" || before != after {
				t.Fatal("failed migration partially changed the legacy identity or private storage")
			}
			if fault == "rule_dependency" && *c.ViewRule != "shopify_sub != '' && id = @request.auth.id" {
				t.Fatal("migration silently rewrote the application's access rule")
			}
		})
	}
}

func TestPrivateAccountsAreNotPublicThroughRecordsOrExpansion(t *testing.T) {
	f := setup(t, "", "")
	result := f.loginStudio()
	token, user := f.session()
	a := privateAccount(f, user.Id)
	c := a.Collection()
	if !c.IsBase() || !c.System || c.ListRule != nil || c.ViewRule != nil || c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
		t.Fatal("private Shopify collection has a public API rule")
	}
	for _, collection := range []string{c.Name, c.Id} {
		base := f.studio.URL + "/api/collections/" + collection + "/records"
		for _, auth := range []string{"", token} {
			f.expect(f.request("GET", base, "", "", auth), 403)
			f.expect(f.request("GET", base+"/"+a.Id, "", "", auth), 403)
			f.expect(f.request("POST", base, "", string(mustJSON(t, a.PublicExport())), auth), 403)
			f.expect(f.request("PATCH", base+"/"+a.Id, "", `{"subject":"forged"}`, auth), 403)
			f.expect(f.request("DELETE", base+"/"+a.Id, "", "", auth), 403)
		}
	}
	profile := f.request("GET", f.studio.URL+"/api/collections/users/records/"+user.Id+"?expand=_shopifyIdentities_via_user", "", "", token)
	f.expect(profile, 200)
	for _, body := range []string{result.body, profile.body, f.request("GET", f.studio.URL+"/studio", "", "", "").body} {
		if strings.Contains(body, a.GetString("subject")) || strings.Contains(body, a.GetString("customerId")) {
			t.Fatal("raw Shopify identifier leaked in OAuth, profile expansion or HTML")
		}
	}
}

func TestExistingPrivateAccountsArePromotedToSystemWithoutRecreation(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	_, user := f.session()
	a := privateAccount(f, user.Id)
	c := a.Collection()
	collectionID, accountID := c.Id, a.Id
	// Reproduce the previous prototype's ordinary private base collection.
	c.System = false
	if err := f.app.SaveNoValidate(c); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ensureCollection(f.app); err != nil {
			t.Fatal(err)
		}
	}
	c, _ = f.app.FindCollectionByNameOrId(accountsCollectionName)
	if !c.System || c.Id != collectionID || privateAccount(f, user.Id).Id != accountID {
		t.Fatal("promotion recreated the collection or lost its account")
	}
	// The normal schema API/Save protections must apply after promotion.
	if err := f.app.Delete(c); err == nil {
		t.Fatal("system collection could be deleted")
	}
	c.Name = "renamedShopifyAccounts"
	if err := f.app.Save(c); err == nil {
		t.Fatal("system collection could be renamed")
	}
	c.Name = accountsCollectionName
	c.ListRule = types.Pointer("")
	if err := f.app.Save(c); err == nil {
		t.Fatal("system collection API rule could be opened")
	}
	if result := decodeAuth(t, f.loginStudio()); result.Record["id"] != user.Id {
		t.Fatal("promotion changed the authenticated account")
	}
}

func TestPrivateAccountRecoveryUsesOnlyTheNativeIdentityLink(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	_, user := f.session()
	if err := f.app.Delete(privateAccount(f, user.Id)); err != nil {
		t.Fatal(err)
	}
	if result := decodeAuth(t, f.loginStudio()); result.Record["id"] != user.Id || result.Meta["isNew"] != false {
		t.Fatal("exact native OAuth link did not recover the private identity")
	}
	privateAccount(f, user.Id)
	other := nativeSignup(f, "other-owner@example.test")
	a := privateAccount(f, user.Id)
	a.Set("user", other.Record["id"])
	if err := f.app.Save(a); err != nil {
		t.Fatal(err)
	}
	f.expect(f.completeCallback(f.authorize("/studio/auth")), 403)
}

func TestPrivateAccountDeletionCascadesFromUser(t *testing.T) {
	f := setup(t, "", "")
	f.loginStudio()
	_, user := f.session()
	a := privateAccount(f, user.Id)
	if err := f.app.Delete(user); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.FindRecordById(accountsCollectionName, a.Id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("deleting a user left its private Shopify identity behind")
	}
}
