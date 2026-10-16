package shopifysso

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

const accountsCollectionName = "_shopifyIdentities"

// Only explicit API deletion unlinks the private identity. PocketBase's internal
// security purges also delete external auths, but must preserve Shopify identity
// continuity during verification upgrades and subsequent Shopify logins.
func (s *Service) unlinkShopifyAccount(e *core.RecordRequestEvent) error {
	if e.Record.GetString("provider") != providerName || e.Record.GetString("collectionRef") != s.collectionID {
		return e.Next()
	}
	linkID := e.Record.Id
	user, providerID := e.Record.GetString("recordRef"), e.Record.GetString("providerId")
	originalApp := e.App
	defer func() { e.App = originalApp }()
	return originalApp.RunInTransaction(func(tx core.App) error {
		e.App = tx
		if err := e.Next(); err != nil {
			return err
		}
		if e.Status() >= http.StatusBadRequest {
			return router.NewApiError(e.Status(), "Shopify unlink request was rejected.", nil)
		}
		// A host hook may stop the chain without deleting the native link.
		if _, err := tx.FindRecordById(e.Collection.Id, linkID); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		accounts, err := tx.FindRecordsByFilter(accountsCollectionName, "user = {:user}", "", 0, 0, dbx.Params{"user": user})
		if err != nil {
			return err
		}
		for _, a := range accounts {
			// Use persisted identity data so unlink also works during discovery
			// outages and leaves identities from other issuers untouched.
			if identityID(a.GetString("issuer"), a.GetString("subject")) == providerID {
				if err := tx.Delete(a); err != nil {
					return e.InternalServerError("Unable to unlink the private Shopify identity.", err)
				}
			}
		}
		return nil
	})
}

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
