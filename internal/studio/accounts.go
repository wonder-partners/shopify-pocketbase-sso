package studio

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const accountsCollectionName = "_shopifyAccounts"

var legacyIdentityFields = []string{"shopify_issuer", "shopify_sub", "shopify_customer_id"}

// The underscore is a naming convention, not an access-control mechanism.
// Locked API rules make this a server-managed base collection, not an auth one.
func ensureAccountsCollection(app core.App, users *core.Collection) error {
	c, err := app.FindCollectionByNameOrId(accountsCollectionName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		c = core.NewBaseCollection(accountsCollectionName)
		c.Fields.Add(&core.RelationField{Name: "user", CollectionId: users.Id, MaxSelect: 1, Required: true, CascadeDelete: true})
		for _, field := range []string{"issuer", "subject", "customerId"} {
			c.Fields.Add(&core.TextField{Name: field, Required: true, Max: 1000})
		}
	}
	if !c.IsBase() {
		return errors.New("_shopifyAccounts must be a base collection")
	}
	relation, ok := c.Fields.GetByName("user").(*core.RelationField)
	if !ok || relation.CollectionId != users.Id || relation.MaxSelect != 1 || !relation.Required || !relation.CascadeDelete {
		return errors.New("_shopifyAccounts.user must be a required cascading relation to the configured auth collection")
	}
	for _, field := range []string{"issuer", "subject", "customerId"} {
		f, ok := c.Fields.GetByName(field).(*core.TextField)
		if !ok || !f.Required {
			return fmt.Errorf("_shopifyAccounts.%s must be a required text field", field)
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

// Caller runs this together with schema creation in one transaction. Any
// incomplete or conflicting old identity aborts it before fields are removed.
func migrateLegacyAccounts(app core.App, users *core.Collection) error {
	present := 0
	for _, field := range legacyIdentityFields {
		if f := users.Fields.GetByName(field); f != nil {
			present++
			if f.Type() != core.FieldTypeText {
				return errors.New("legacy Shopify identity fields must be text fields")
			}
		}
	}
	if present == 0 {
		return nil
	}
	if present != len(legacyIdentityFields) {
		return errors.New("incomplete legacy Shopify identity schema; migration aborted")
	}
	records, err := app.FindAllRecords(users, dbx.Or(
		dbx.NewExp("shopify_issuer != ''"), dbx.NewExp("shopify_sub != ''"), dbx.NewExp("shopify_customer_id != ''"),
	))
	if err != nil {
		return err
	}
	for _, r := range records {
		issuer, subject, customerID := r.GetString("shopify_issuer"), r.GetString("shopify_sub"), r.GetString("shopify_customer_id")
		if issuer == "" || subject == "" || customerID == "" {
			return errors.New("incomplete legacy Shopify identity; migration aborted")
		}
		if err := putAccount(app, r.Id, issuer, subject, customerID); err != nil {
			return err
		}
	}
	users.RemoveIndex("idx_" + users.Id + "_shopify_identity")
	for _, field := range legacyIdentityFields {
		users.Fields.RemoveByName(field)
	}
	return app.Save(users)
}
