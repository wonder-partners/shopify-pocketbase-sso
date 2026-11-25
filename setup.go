package shopifysso

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func ensureCollection(app core.App, collectionName string) error {
	return app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(collectionName)
		if err != nil {
			return err
		}
		if !c.IsAuth() || c.Name == core.CollectionNameSuperusers {
			return errors.New("Shopify requires a non-superuser auth collection")
		}
		return ensureIdentityStore(tx, c)
	})
}

func saveProviderConfig(app core.App, collection, clientID string) (string, error) {
	var c *core.Collection
	err := app.RunInTransaction(func(tx core.App) error {
		var err error
		c, err = tx.FindCollectionByNameOrId(collection)
		if err != nil {
			return err
		}
		// PocketBase v0.40.5 requires a non-empty config field even for public
		// clients. This public marker is never used by the provider's exchange.
		config := core.OAuth2ProviderConfig{
			Name: providerName, ClientId: clientID, ClientSecret: "public-pkce-no-secret",
			DisplayName: "Shopify", PKCE: types.Pointer(true),
		}
		replaced := false
		for i := range c.OAuth2.Providers {
			if c.OAuth2.Providers[i].Name == providerName {
				c.OAuth2.Providers[i] = config
				replaced = true
			}
		}
		if !replaced {
			c.OAuth2.Providers = append(c.OAuth2.Providers, config)
		}
		c.OAuth2.Enabled = true
		return tx.Save(c)
	})
	if err != nil {
		return "", err
	}
	return c.Id, nil
}
