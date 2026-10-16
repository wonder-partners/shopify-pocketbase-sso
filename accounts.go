package shopifysso

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const accountsCollectionName = "_shopifyIdentities"

// The underscore is a naming convention, not an access-control mechanism.
// Locked API rules protect the records; System groups and protects the schema.
func ensureAccountsCollection(app core.App, users *core.Collection) error {
	c, err := app.FindCollectionByNameOrId(accountsCollectionName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		c = core.NewBaseCollection(accountsCollectionName)
		c.System = true
		c.Fields.Add(&core.RelationField{Name: "user", CollectionId: users.Id, MaxSelect: 1, Required: true, CascadeDelete: true})
		for _, field := range []string{"issuer", "subject", "customerId"} {
			c.Fields.Add(&core.TextField{Name: field, Required: true, Max: 1000})
		}
	}
	if !c.IsBase() || !c.System {
		return errors.New("_shopifyIdentities must be a system base collection")
	}
	relation, ok := c.Fields.GetByName("user").(*core.RelationField)
	if !ok || relation.CollectionId != users.Id || relation.MaxSelect != 1 || !relation.Required || !relation.CascadeDelete {
		return errors.New("_shopifyIdentities.user must be a required cascading relation to the configured auth collection")
	}
	for _, field := range []string{"issuer", "subject", "customerId"} {
		f, ok := c.Fields.GetByName(field).(*core.TextField)
		if !ok || !f.Required {
			return fmt.Errorf("_shopifyIdentities.%s must be a required text field", field)
		}
	}
	c.ListRule, c.ViewRule, c.CreateRule, c.UpdateRule, c.DeleteRule = nil, nil, nil, nil, nil
	c.AddIndex("idx_"+c.Id+"_identity", true, "issuer, subject", "")
	c.AddIndex("idx_"+c.Id+"_user_issuer", true, "user, issuer", "")
	return app.Save(c)
}

func putAccount(app core.App, user, issuer, subject, customerID string) error {
	a, err := app.FindFirstRecordByFilter(accountsCollectionName,
		"issuer = {:issuer} && subject = {:subject}", dbx.Params{"issuer": issuer, "subject": subject})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if a != nil && a.GetString("user") != user {
		return errors.New("Shopify identity already belongs to another user")
	}
	if a == nil {
		c, err := app.FindCollectionByNameOrId(accountsCollectionName)
		if err != nil {
			return err
		}
		a = core.NewRecord(c)
	}
	a.Set("user", user)
	a.Set("issuer", issuer)
	a.Set("subject", subject)
	a.Set("customerId", customerID)
	return app.Save(a)
}
