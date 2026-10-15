package studio

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const collectionName = "studio_customers"

// This isolated collection avoids silently changing an existing users collection.
func ensureCollection(app core.App) error {
	_, err := app.FindCollectionByNameOrId(collectionName)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	c := core.NewAuthCollection(collectionName)
	c.PasswordAuth.Enabled = false
	c.OTP.Enabled = false
	c.OAuth2.Enabled = false
	c.AuthAlert.Enabled = false
	c.AuthToken.Duration = int64(sessionDuration.Seconds())
	c.ViewRule = types.Pointer("id = @request.auth.id")
	// Create, list, update, delete, manage stay locked to superusers.
	for _, name := range []string{"shopify_issuer", "shopify_sub", "shopify_customer_id", "first_name", "last_name"} {
		c.Fields.Add(&core.TextField{Name: name, Max: 1000})
	}
	c.AddIndex("idx_studio_shopify_identity", true, "shopify_issuer, shopify_sub", "")
	return app.Save(c)
}

func (s *Service) saveCustomer(subject string, c *customer, cl claims) (*core.Record, error) {
	var saved *core.Record
	err := s.app.RunInTransaction(func(tx core.App) error {
		r, err := tx.FindFirstRecordByFilter(collectionName,
			"shopify_issuer = {:issuer} && shopify_sub = {:subject}", dbx.Params{"issuer": s.issuer, "subject": subject})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if errors.Is(err, sql.ErrNoRows) {
			coll, err := tx.FindCollectionByNameOrId(collectionName)
			if err != nil {
				return err
			}
			r = core.NewRecord(coll)
			r.SetPassword(random())
		}
		// Never identify or merge an existing account by email.
		r.Set("shopify_issuer", s.issuer)
		r.Set("shopify_sub", subject)
		r.Set("shopify_customer_id", c.ID)
		r.Set("first_name", c.FirstName)
		r.Set("last_name", c.LastName)
		r.SetEmail(c.EmailAddress.Email)
		r.SetVerified(cl.EmailVerified && strings.EqualFold(cl.Email, c.EmailAddress.Email))
		if err := tx.Save(r); err != nil {
			return fmt.Errorf("save customer, including possible email conflict: %w", err)
		}
		saved = r
		return nil
	})
	return saved, err
}
