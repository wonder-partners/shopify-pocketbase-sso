package migrations

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	// This cleanup belongs to the demo, not the reusable auth integration.
	// PocketBase records it once, so future host-defined fields are left alone.
	m.Register(removePrototypeNames, func(core.App) error {
		return errors.New("removed profile values cannot be restored by a down migration; restore the pre-migration backup")
	})
}

func removePrototypeNames(app core.App) error {
	name := os.Getenv("STUDIO_AUTH_COLLECTION")
	if name == "" {
		name = "users"
	}
	c, err := app.FindCollectionByNameOrId(name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Fresh installation: the integration won't create these fields.
	}
	if err != nil {
		return err
	}
	if !c.IsAuth() || c.System {
		return errors.New("profile cleanup requires a non-system auth collection")
	}
	changed := false
	for _, name := range []string{"first_name", "last_name"} {
		if field := c.Fields.GetByName(name); field != nil {
			if field.Type() != core.FieldTypeText || field.GetSystem() {
				return fmt.Errorf("refusing to remove incompatible prototype field %s", name)
			}
			c.Fields.RemoveByName(name)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	// Keep validation: a project rule depending on a removed field must fail
	// the transaction, never be rewritten or bypassed by this cleanup.
	return app.Save(c)
}
