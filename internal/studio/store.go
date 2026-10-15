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
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/types"
)

const collectionName = "users"

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
		// Native OAuth signup is an internal records API request. PocketBase
		// copies the event store, not the HTTP context, to that request.
		if _, trusted := e.Get(trustedProfileKey).(*shopifyProvider); trusted {
			return e.Next()
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

func (s *Service) configureProvider() error {
	c, err := s.app.FindCollectionByNameOrId(s.cfg.AuthCollection)
	if err != nil {
		return err
	}
	s.cfg.AuthCollection = c.Name
	secret := s.cfg.ClientSecret
	if secret == "" {
		// PocketBase v0.40.5 requires a non-empty config field even for public
		// clients. This public marker is never used by the provider's exchange.
		secret = "public-pkce-no-secret"
	}
	config := core.OAuth2ProviderConfig{
		Name: providerName, ClientId: s.cfg.ClientID, ClientSecret: secret,
		AuthURL: s.oauth.Endpoint.AuthURL, TokenURL: s.oauth.Endpoint.TokenURL,
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
	if err := s.app.Save(c); err != nil {
		return err
	}
	s.app.OnRecordAuthWithOAuth2Request(c.Name).Bind(&hook.Handler[*core.RecordAuthWithOAuth2RequestEvent]{
		Id: "studio_shopify_profile", Priority: -1000, Func: s.prepareOAuthRecord,
	})
	return nil
}

func (s *Service) prepareOAuthRecord(e *core.RecordAuthWithOAuth2RequestEvent) error {
	if e.ProviderName != providerName {
		return e.Next()
	}
	p, ok := e.ProviderClient.(*shopifyProvider)
	if !ok || p.service != s || p.profile == nil {
		return e.ForbiddenError("Missing verified Shopify identity.", nil)
	}
	// Native OAuth can find a record by verified email or current auth header.
	// Neither is sufficient for our policy: require an existing Shopify identity.
	r, err := s.app.FindFirstRecordByFilter(s.cfg.AuthCollection,
		"shopify_issuer = {:issuer} && shopify_sub = {:subject}", dbx.Params{"issuer": s.issuer, "subject": p.subject})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if e.Record != nil && (r == nil || e.Record.Id != r.Id) {
		return router.NewApiError(http.StatusConflict, "Shopify cannot automatically link an existing account.", nil)
	}
	// Lazily preserve the prototype user's id and all relations on first login.
	if r != nil {
		e.Record, e.IsNewRecord = r, false
	}
	emailOwner, err := s.app.FindAuthRecordByEmail(s.cfg.AuthCollection, p.profile.EmailAddress.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if emailOwner != nil && (r == nil || emailOwner.Id != r.Id) {
		return router.NewApiError(http.StatusConflict, "Shopify email conflicts with an existing account.", nil)
	}
	values := map[string]any{
		"email": p.profile.EmailAddress.Email, "first_name": p.profile.FirstName, "last_name": p.profile.LastName,
		"shopify_issuer": s.issuer, "shopify_sub": p.subject, "shopify_customer_id": p.profile.ID,
	}
	e.Set(trustedProfileKey, p)
	if r == nil {
		if e.CreateData == nil {
			e.CreateData = map[string]any{}
		}
		for field, value := range values {
			e.CreateData[field] = value
		}
		delete(e.CreateData, "verified")
		delete(e.CreateData, "password")
		delete(e.CreateData, "passwordConfirm")
	} else {
		keepVerified := r.Verified() && strings.EqualFold(r.Email(), p.profile.EmailAddress.Email)
		for field, value := range values {
			r.Set(field, value)
		}
		r.SetVerified(keepVerified || e.OAuth2User.Email != "")
		if err := s.app.Save(r); err != nil {
			return router.NewApiError(http.StatusConflict, "Unable to synchronize the Shopify profile.", nil)
		}
	}
	return e.Next()
}
