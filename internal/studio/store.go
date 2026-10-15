package studio

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

const collectionName = "users"

var errSignupForbidden = errors.New("Shopify signup does not satisfy the collection CreateRule")

// Add only the identity fields/index. Preserve existing rules and auth policies.
func ensureCollection(app core.App, name string) error {
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		c = core.NewAuthCollection(name)
		c.ViewRule = types.Pointer("id = @request.auth.id")
	}
	if !c.IsAuth() || c.Name == "_superusers" {
		return errors.New("Shopify requires a non-superuser auth collection")
	}
	for _, field := range []string{"shopify_issuer", "shopify_sub", "shopify_customer_id", "first_name", "last_name"} {
		existing := c.Fields.GetByName(field)
		if existing == nil {
			c.Fields.Add(&core.TextField{Name: field, Max: 1000})
		} else if existing.Type() != core.FieldTypeText {
			return fmt.Errorf("Shopify field %s must be a text field", field)
		}
	}
	// Unlinked, built-in accounts all have empty Shopify fields. Do not make
	// those empty pairs unique, otherwise the second password account fails.
	c.AddIndex("idx_"+c.Id+"_shopify_identity", true, "shopify_issuer, shopify_sub", "shopify_issuer != '' AND shopify_sub != ''")
	if err := app.Save(c); err != nil {
		return err
	}
	guard := func(e *core.RecordRequestEvent) error {
		if e.HasSuperuserAuth() {
			return e.Next()
		}
		var body map[string]any
		if err := e.BindBody(&body); err != nil {
			return err
		}
		for _, field := range []string{"shopify_issuer", "shopify_sub", "shopify_customer_id"} {
			if _, exists := body[field]; exists {
				return e.ForbiddenError("Shopify identity fields are managed by the server.", nil)
			}
		}
		return e.Next()
	}
	app.OnRecordCreateRequest(c.Name).Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "studio_shopify_identity", Func: guard})
	app.OnRecordUpdateRequest(c.Name).Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "studio_shopify_identity", Func: guard})
	return nil
}

func (s *Service) saveCustomer(subject string, c *customer, cl claims, info *core.RequestInfo) (*core.Record, error) {
	var saved *core.Record
	err := s.app.RunInTransaction(func(tx core.App) error {
		r, err := tx.FindFirstRecordByFilter(s.cfg.AuthCollection,
			"shopify_issuer = {:issuer} && shopify_sub = {:subject}", dbx.Params{"issuer": s.issuer, "subject": subject})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		isNew := errors.Is(err, sql.ErrNoRows)
		if isNew {
			coll, err := tx.FindCollectionByNameOrId(s.cfg.AuthCollection)
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
		keepVerified := r.Verified() && strings.EqualFold(r.Email(), c.EmailAddress.Email)
		r.SetEmail(c.EmailAddress.Email)
		if isNew {
			createInfo := &core.RequestInfo{}
			if info != nil {
				createInfo = info.Clone()
			}
			createInfo.Context, createInfo.Method = core.RequestInfoContextOAuth2, http.MethodPost
			createInfo.Body = map[string]any{"email": c.EmailAddress.Email, "first_name": c.FirstName, "last_name": c.LastName}
			allowed, ruleErr := tx.CanAccessRecord(r, createInfo, r.Collection().CreateRule)
			if ruleErr != nil || !allowed {
				return errSignupForbidden
			}
		}
		r.SetVerified(keepVerified || (cl.EmailVerified && strings.EqualFold(cl.Email, c.EmailAddress.Email)))
		if err := tx.Save(r); err != nil {
			return fmt.Errorf("save customer, including possible email conflict: %w", err)
		}
		saved = r
		return nil
	})
	return saved, err
}
