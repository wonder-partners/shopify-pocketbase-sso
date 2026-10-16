package migrations

import (
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func migrationApp(t *testing.T) *pocketbase.PocketBase {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	app.Settings().Logs.MaxDays = 0
	t.Cleanup(func() {
		if err := app.ResetBootstrapState(); err != nil {
			t.Error(err)
		}
	})
	return app
}

func TestNameCleanupIsOneTimeAndPreservesTheAccount(t *testing.T) {
	for _, name := range []string{"users", "members"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("STUDIO_AUTH_COLLECTION", "")
			if name != "users" {
				t.Setenv("STUDIO_AUTH_COLLECTION", name)
			}
			app := migrationApp(t)
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				c = core.NewAuthCollection(name)
			}
			c.Fields.Add(&core.TextField{Name: "first_name"}, &core.TextField{Name: "last_name"}, &core.TextField{Name: "display_name"})
			c.ViewRule = types.Pointer("id = @request.auth.id")
			c.AuthToken.Duration = 1234
			if err := app.Save(c); err != nil {
				t.Fatal(err)
			}
			u := core.NewRecord(c)
			u.SetEmail("migration@example.test")
			u.SetPassword("TestPassword12345")
			u.Set("first_name", "FormerFirst")
			u.Set("last_name", "FormerLast")
			u.Set("display_name", "Preserved display name")
			if err := app.Save(u); err != nil {
				t.Fatal(err)
			}
			token, _ := u.NewAuthToken()
			if err := app.RunAppMigrations(); err != nil {
				t.Fatal(err)
			}
			c, _ = app.FindCollectionByNameOrId(name)
			if c.Fields.GetByName("first_name") != nil || c.Fields.GetByName("last_name") != nil || c.AuthToken.Duration != 1234 || *c.ViewRule != "id = @request.auth.id" {
				t.Fatal("cleanup did not remove only the prototype fields")
			}
			current, err := app.FindAuthRecordByToken(token, core.TokenTypeAuth)
			if err != nil || current.Id != u.Id || current.Email() != u.Email() || current.GetString("display_name") != "Preserved display name" || !current.ValidatePassword("TestPassword12345") {
				t.Fatal("cleanup changed the user, its session or other profile data")
			}
			// A field later owned by the host must not be removed on every boot.
			c.Fields.Add(&core.TextField{Name: "first_name"})
			if err := app.Save(c); err != nil {
				t.Fatal(err)
			}
			if err := app.RunAppMigrations(); err != nil {
				t.Fatal(err)
			}
			c, _ = app.FindCollectionByNameOrId(name)
			if c.Fields.GetByName("first_name") == nil {
				t.Fatal("one-time cleanup was reapplied on a subsequent startup")
			}
		})
	}
}

func TestNameCleanupFailsWithoutPartialSchemaChanges(t *testing.T) {
	for _, fault := range []string{"incompatible_type", "dependent_rule"} {
		t.Run(fault, func(t *testing.T) {
			t.Setenv("STUDIO_AUTH_COLLECTION", "cleanup_test")
			app := migrationApp(t)
			c := core.NewAuthCollection("cleanup_test")
			c.Fields.Add(&core.TextField{Name: "first_name"}, &core.TextField{Name: "last_name"})
			if fault == "incompatible_type" {
				c.Fields.RemoveByName("last_name")
				c.Fields.Add(&core.BoolField{Name: "last_name"})
			} else {
				c.ViewRule = types.Pointer("first_name != ''")
			}
			if err := app.Save(c); err != nil {
				t.Fatal(err)
			}
			if err := app.RunAppMigrations(); err == nil {
				t.Fatal("unsafe profile cleanup unexpectedly succeeded")
			}
			c, _ = app.FindCollectionByNameOrId(c.Id)
			if c.Fields.GetByName("first_name") == nil || c.Fields.GetByName("last_name") == nil {
				t.Fatal("failed cleanup partially removed fields")
			}
			if fault == "dependent_rule" && *c.ViewRule != "first_name != ''" {
				t.Fatal("cleanup rewrote the host project's rule")
			}
		})
	}
}

func TestNameCleanupAcceptsFreshInstallations(t *testing.T) {
	t.Setenv("STUDIO_AUTH_COLLECTION", "new_users")
	app := migrationApp(t)
	if err := app.RunAppMigrations(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FindCollectionByNameOrId("new_users"); err == nil {
		t.Fatal("cleanup created a collection on a fresh installation")
	}
}
