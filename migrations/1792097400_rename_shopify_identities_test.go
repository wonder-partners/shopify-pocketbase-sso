package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestShopifyIdentityRenamePreservesRecordsRelationsAndIndexes(t *testing.T) {
	t.Setenv("STUDIO_AUTH_COLLECTION", "") // Historical profile cleanup migration.
	app := migrationApp(t)
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		users = core.NewAuthCollection("users")
		if err := app.Save(users); err != nil {
			t.Fatal(err)
		}
	}
	user := core.NewRecord(users)
	user.SetEmail("identity-migration@example.test")
	user.SetPassword("TestPassword12345")
	if err := app.Save(user); err != nil {
		t.Fatal(err)
	}
	legacy := core.NewBaseCollection("_shopifyAccounts")
	legacy.System = true
	legacy.Fields.Add(&core.RelationField{Name: "user", CollectionId: users.Id, MaxSelect: 1, CascadeDelete: true, Required: true})
	for _, name := range []string{"issuer", "subject", "customerId"} {
		legacy.Fields.Add(&core.TextField{Name: name, Required: true})
	}
	legacy.AddIndex("idx_"+legacy.Id+"_identity", true, "issuer, subject", "")
	if err := app.Save(legacy); err != nil {
		t.Fatal(err)
	}
	identity := core.NewRecord(legacy)
	identity.Set("user", user.Id)
	identity.Set("issuer", "https://shop.example/identity")
	identity.Set("subject", "900719925474099312345")
	identity.Set("customerId", "gid://shopify/Customer/12345")
	if err := app.Save(identity); err != nil {
		t.Fatal(err)
	}
	references := core.NewBaseCollection("identity_references")
	references.Fields.Add(&core.RelationField{Name: "identity", CollectionId: legacy.Id, MaxSelect: 1})
	if err := app.Save(references); err != nil {
		t.Fatal(err)
	}
	reference := core.NewRecord(references)
	reference.Set("identity", identity.Id)
	if err := app.Save(reference); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := app.RunAppMigrations(); err != nil {
			t.Fatal(err)
		}
	}
	current, err := app.FindCollectionByNameOrId("_shopifyIdentities")
	if err != nil || current.Id != legacy.Id || !current.System || app.HasTable("_shopifyAccounts") {
		t.Fatal("identity migration recreated the collection or left its old table behind")
	}
	saved, err := app.FindRecordById(current, identity.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"user", "issuer", "subject", "customerId"} {
		if saved.GetString(name) != identity.GetString(name) {
			t.Fatal("identity migration changed an identifier or its owner")
		}
	}
	references, _ = app.FindCollectionByNameOrId(references.Id)
	reference, _ = app.FindRecordById(references, reference.Id)
	if references.Fields.GetByName("identity").(*core.RelationField).CollectionId != current.Id || reference.GetString("identity") != saved.Id {
		t.Fatal("migration broke a relation targeting the private identity")
	}
	duplicate := core.NewRecord(current)
	for _, name := range []string{"user", "issuer", "subject", "customerId"} {
		duplicate.Set(name, saved.GetString(name))
	}
	if err := app.Save(duplicate); err == nil {
		t.Fatal("identity uniqueness index was lost during rename")
	}
	// The registered down migration uses the same operation in reverse.
	if err := app.RunInTransaction(func(tx core.App) error {
		return renamePrivateShopifyCollection(tx, "_shopifyIdentities", "_shopifyAccounts")
	}); err != nil {
		t.Fatal(err)
	}
	if restored, err := app.FindCollectionByNameOrId("_shopifyAccounts"); err != nil || restored.Id != legacy.Id {
		t.Fatal("reverse rename did not preserve the original collection")
	}
}

func TestShopifyIdentityRenameRejectsConflictingOrPublicCollections(t *testing.T) {
	for _, fault := range []string{"both_names", "public_rule", "wrong_type"} {
		t.Run(fault, func(t *testing.T) {
			app := migrationApp(t)
			legacy := core.NewBaseCollection("_shopifyAccounts")
			if fault == "wrong_type" {
				legacy = core.NewAuthCollection("_shopifyAccounts")
			}
			if fault == "public_rule" {
				legacy.ListRule = types.Pointer("")
			}
			if err := app.Save(legacy); err != nil {
				t.Fatal(err)
			}
			if fault == "both_names" {
				if err := app.Save(core.NewBaseCollection("_shopifyIdentities")); err != nil {
					t.Fatal(err)
				}
			}
			if err := app.RunInTransaction(func(tx core.App) error {
				return renamePrivateShopifyCollection(tx, "_shopifyAccounts", "_shopifyIdentities")
			}); err == nil {
				t.Fatal("unsafe identity rename unexpectedly succeeded")
			}
			if _, err := app.FindCollectionByNameOrId("_shopifyAccounts"); err != nil {
				t.Fatal("failed rename modified the source collection")
			}
		})
	}
}
