package shopifysso

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

// A Shopify identity ties one account to one customer of one Shopify store
// (issuer). It always pairs with the account's Shopify OAuth link, whose
// provider ID is derived from the same issuer and subject.
const identitiesCollectionName = "_shopifyIdentities"

// shopifyIdentity is a stored Shopify identity. Callers only learn its account.
type shopifyIdentity struct{ record *core.Record }

func (i *shopifyIdentity) account() string { return i.record.GetString("user") }

// oauthLinkProviderID is the provider ID of the OAuth link paired with a
// Shopify identity. It namespaces identities by issuer without rounding numeric
// Shopify subjects.
func oauthLinkProviderID(issuer, subject string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(issuer+"\x00"+subject)))
}

// ensureIdentityStore creates the private identity collection for accounts, or
// checks that an existing one still has the expected schema and locked rules.
// The underscore is a naming convention, not an access-control mechanism.
// Locked API rules protect the records; System groups and protects the schema.
func ensureIdentityStore(app core.App, accounts *core.Collection) error {
	c, err := app.FindCollectionByNameOrId(identitiesCollectionName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		c = core.NewBaseCollection(identitiesCollectionName)
		c.System = true
		c.Fields.Add(&core.RelationField{Name: "user", CollectionId: accounts.Id, MaxSelect: 1, Required: true, CascadeDelete: true})
		for _, field := range []string{"issuer", "subject", "customerId"} {
			c.Fields.Add(&core.TextField{Name: field, Required: true, Max: 1000})
		}
	}
	if !c.IsBase() || !c.System {
		return errors.New("_shopifyIdentities must be a system base collection")
	}
	relation, ok := c.Fields.GetByName("user").(*core.RelationField)
	if !ok || relation.CollectionId != accounts.Id || relation.MaxSelect != 1 || !relation.Required || !relation.CascadeDelete {
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

// findIdentity returns the Shopify identity of a store customer, or nil.
func findIdentity(app core.App, issuer, subject string) (*shopifyIdentity, error) {
	r, err := app.FindFirstRecordByFilter(identitiesCollectionName,
		"issuer = {:issuer} && subject = {:subject}", dbx.Params{"issuer": issuer, "subject": subject})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &shopifyIdentity{record: r}, nil
}

// saveIdentity records that account is this store customer. An identity never
// moves to another account. previous is the identity this login found before
// a security purge within the same transaction; only it may be restored.
func saveIdentity(app core.App, account, issuer, subject, customerID string, previous *shopifyIdentity) error {
	identity, err := findIdentity(app, issuer, subject)
	if err != nil {
		return err
	}
	var r *core.Record
	switch {
	case identity != nil:
		if identity.account() != account {
			return errors.New("Shopify identity already belongs to another user")
		}
		r = identity.record
	case previous != nil:
		r = previous.record
		r.MarkAsNew()
	default:
		c, err := app.FindCollectionByNameOrId(identitiesCollectionName)
		if err != nil {
			return err
		}
		r = core.NewRecord(c)
	}
	r.Set("user", account)
	r.Set("issuer", issuer)
	r.Set("subject", subject)
	r.Set("customerId", customerID)
	return app.Save(r)
}

// bindIdentityRevocation keeps Shopify identities paired with their OAuth links:
// deleting a Shopify OAuth link deletes its identity, and verifying an account's
// email revokes all its identities and sessions. Model hooks also cover native
// verification, OTP and password recovery purges.
func bindIdentityRevocation(app core.App, accounts string) {
	app.OnRecordDeleteExecute(core.CollectionNameExternalAuths).Bind(&hook.Handler[*core.RecordEvent]{
		Id: "shopify_revoke_with_oauth_link", Priority: runBeforeHostHooks, Func: func(e *core.RecordEvent) error {
			return revokeIdentityWithOAuthLink(e, accounts)
		},
	})
	app.OnRecordUpdateExecute(accounts).Bind(&hook.Handler[*core.RecordEvent]{
		Id: "shopify_revoke_on_verification", Priority: runBeforeHostHooks, Func: revokeIdentitiesOnVerification,
	})
}

func revokeIdentityWithOAuthLink(e *core.RecordEvent, accounts string) error {
	if e.Record.GetString("provider") != providerName || e.Record.GetString("collectionRef") != accounts {
		return e.Next()
	}
	linkID := e.Record.Id
	account, providerID := e.Record.GetString("recordRef"), e.Record.GetString("providerId")
	return inTransaction(e, func(tx core.App) error {
		if err := e.Next(); err != nil {
			return err
		}
		// A host model hook may stop the chain without deleting the link.
		if _, err := tx.FindRecordById(core.CollectionNameExternalAuths, linkID); err == nil {
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		identities, err := tx.FindRecordsByFilter(identitiesCollectionName, "user = {:user}", "", 0, 0, dbx.Params{"user": account})
		if err != nil {
			return err
		}
		for _, r := range identities {
			// Use persisted identity data so unlink also works during discovery
			// outages and leaves identities from other issuers untouched.
			if oauthLinkProviderID(r.GetString("issuer"), r.GetString("subject")) == providerID {
				if err := tx.Delete(r); err != nil {
					return router.NewInternalServerError("Unable to unlink the private Shopify identity.", err)
				}
			}
		}
		return nil
	})
}

// Also revoke orphaned identities left by older versions without an OAuth
// link. PocketBase cannot find those rows during its external-auth purge.
func revokeIdentitiesOnVerification(e *core.RecordEvent) error {
	if e.Record.Original().Verified() || !e.Record.Verified() {
		return e.Next()
	}
	return inTransaction(e, func(tx core.App) error {
		count, err := tx.CountRecords(identitiesCollectionName, dbx.HashExp{"user": e.Record.Id})
		if err != nil {
			return err
		}
		if count > 0 {
			// PocketBase only rotates the key when it finds OAuth links.
			// Include legacy identities in the same verification update.
			e.Record.RefreshTokenKey()
		}
		if err := e.Next(); err != nil {
			return err
		}
		identities, err := tx.FindRecordsByFilter(identitiesCollectionName, "user = {:user}", "", 0, 0, dbx.Params{"user": e.Record.Id})
		if err != nil {
			return err
		}
		for _, r := range identities {
			if err := tx.Delete(r); err != nil {
				return router.NewInternalServerError("Unable to revoke the private Shopify identity.", err)
			}
		}
		return nil
	})
}

// inTransaction runs fn, which continues the model hook chain, in one
// transaction that is also e.App meanwhile.
func inTransaction(e *core.RecordEvent, fn func(tx core.App) error) error {
	originalApp := e.App
	defer func() { e.App = originalApp }()
	return originalApp.RunInTransaction(func(tx core.App) error {
		e.App = tx
		return fn(tx)
	})
}

// Keep project request hooks in the same transaction as both identity deletes.
func (s *Service) unlinkShopifyIdentity(e *core.RecordRequestEvent) error {
	if e.Record.GetString("provider") != providerName || e.Record.GetString("collectionRef") != s.collectionID {
		return e.Next()
	}
	// Native deletion writes its 204 from a transaction completion callback.
	return respondAfterCommit(e.RequestEvent, commitRules{responseWrittenOnComplete: true}, func(core.App) error {
		return e.Next()
	})
}
