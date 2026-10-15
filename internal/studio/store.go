package studio

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/pocketbase/pocketbase/tools/types"
)

const collectionName = "users"

// Don't add profile fields to the host auth collection. Move old Shopify
// identities transactionally; preserve all user ids and rules.
func ensureCollection(app core.App, name string) error {
	var target string
	err := app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(name)
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
		if err := tx.Save(c); err != nil {
			return err
		}
		if err := ensureAccountsCollection(tx, c); err != nil {
			return err
		}
		if err := migrateLegacyAccounts(tx, c); err != nil {
			return err
		}
		target = c.Name
		return nil
	})
	if err != nil {
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
		// Reject legacy field submissions too; these keys no longer belong to users.
		for _, field := range []string{"shopify_issuer", "shopify_sub", "shopify_customer_id"} {
			if _, exists := body[field]; exists {
				return e.ForbiddenError("Shopify identity fields are managed by the server.", nil)
			}
		}
		return e.Next()
	}
	app.OnRecordCreateRequest(target).Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "studio_shopify_identity", Func: guard})
	app.OnRecordUpdateRequest(target).Bind(&hook.Handler[*core.RecordRequestEvent]{Id: "studio_shopify_identity", Func: guard})
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
	s.app.OnRecordAuthRequest(c.Name).Bind(&hook.Handler[*core.RecordAuthRequestEvent]{
		Id: "studio_shopify_account", Priority: -1000, Func: func(e *core.RecordAuthRequestEvent) error {
			p, ok := e.Get(trustedProfileKey).(*shopifyProvider)
			if ok && p.service == s && e.AuthMethod == core.MFAMethodOAuth2 {
				if err := putAccount(e.App, e.Record.Id, s.issuer, p.subject, p.profile.ID); err != nil {
					return e.InternalServerError("Unable to save the private Shopify identity.", nil)
				}
			}
			return e.Next()
		},
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
	a, err := s.app.FindFirstRecordByFilter(accountsCollectionName,
		"issuer = {:issuer} && subject = {:subject}", dbx.Params{"issuer": s.issuer, "subject": p.subject})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var r *core.Record
	if a != nil {
		r, err = s.app.FindRecordById(s.cfg.AuthCollection, a.GetString("user"))
		if err != nil {
			return err
		}
	}
	// A previous native login may have committed its external link before an
	// interruption prevented saving our private row. Recover only from that
	// exact native identity link, never from an email/header fallback candidate.
	link, err := s.app.FindFirstExternalAuthByExpr(dbx.HashExp{
		"collectionRef": e.Collection.Id, "provider": providerName,
		"providerId": identityID(s.issuer, p.subject),
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if link != nil {
		if r != nil && r.Id != link.RecordRef() {
			return e.ForbiddenError("Conflicting Shopify identity links.", nil)
		}
		r, err = s.app.FindRecordById(s.cfg.AuthCollection, link.RecordRef())
		if err != nil {
			return err
		}
	}
	if e.Record != nil && (r == nil || e.Record.Id != r.Id) {
		return router.NewApiError(http.StatusConflict, "Shopify cannot automatically link an existing account.", nil)
	}
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
	e.Set(trustedProfileKey, p)
	if r == nil {
		if e.CreateData == nil {
			e.CreateData = map[string]any{}
		}
		e.CreateData["email"] = p.profile.EmailAddress.Email
		delete(e.CreateData, "verified")
		delete(e.CreateData, "password")
		delete(e.CreateData, "passwordConfirm")
		for _, field := range legacyIdentityFields {
			delete(e.CreateData, field)
		}
	} else {
		keepVerified := r.Verified() && strings.EqualFold(r.Email(), p.profile.EmailAddress.Email)
		r.SetEmail(p.profile.EmailAddress.Email)
		r.SetVerified(keepVerified || e.OAuth2User.Email != "")
		if err := s.app.Save(r); err != nil {
			return router.NewApiError(http.StatusConflict, "Unable to synchronize the Shopify profile.", nil)
		}
	}
	return e.Next()
}
