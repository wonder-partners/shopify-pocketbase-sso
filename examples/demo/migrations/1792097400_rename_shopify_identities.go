package migrations

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		return renamePrivateShopifyCollection(app, "_shopifyAccounts", "_shopifyIdentities")
	}, func(app core.App) error {
		return renamePrivateShopifyCollection(app, "_shopifyIdentities", "_shopifyAccounts")
	})
}

func renamePrivateShopifyCollection(app core.App, from, to string) error {
	c, err := app.FindCollectionByNameOrId(from)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // Fresh installation, or already renamed.
	}
	if err != nil {
		return err
	}
	if _, err := app.FindCollectionByNameOrId(to); err == nil {
		return fmt.Errorf("both %s and %s exist; refusing to merge or overwrite identities", from, to)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !c.IsBase() || c.ListRule != nil || c.ViewRule != nil || c.CreateRule != nil || c.UpdateRule != nil || c.DeleteRule != nil {
		return errors.New("Shopify identity rename requires a locked base collection")
	}
	if err := app.Save(c); err != nil {
		return err
	}
	// The normal validator forbids renaming a system collection. Change only
	// its name after validation; native save hooks rename the table and indexes.
	// The PocketBase migration transaction rolls back any failure.
	c.Name = to
	if err := app.SaveNoValidate(c); err != nil {
		return err
	}
	return app.Save(c)
}
