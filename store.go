package shopifysso

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

// The host owns the auth collection, its fields and its access rules.
// Only provision the integration's private identities collection.
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

func (s *Service) configureProvider() error {
	collection := s.collectionID
	if collection == "" {
		collection = s.cfg.AuthCollection
	}
	var c *core.Collection
	// Read through the same transaction that saves the provider, so a host
	// update cannot commit between our read and the collection replacement.
	err := s.app.RunInTransaction(func(tx core.App) error {
		var err error
		c, err = tx.FindCollectionByNameOrId(collection)
		if err != nil {
			return err
		}
		// PocketBase v0.40.5 requires a non-empty config field even for public
		// clients. This public marker is never used by the provider's exchange.
		config := core.OAuth2ProviderConfig{
			Name: providerName, ClientId: s.cfg.ClientID, ClientSecret: "public-pkce-no-secret",
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
		return err
	}
	if s.collectionID == "" {
		s.collectionID = c.Id
	}
	// Collection IDs keep these hooks active when the host renames the collection.
	s.app.OnRecordAuthWithOAuth2Request(c.Id).Bind(&hook.Handler[*core.RecordAuthWithOAuth2RequestEvent]{
		Id: "studio_shopify_profile", Priority: -1000, Func: s.prepareOAuthRecord,
	})
	s.app.OnRecordAuthRequest(c.Id).Bind(&hook.Handler[*core.RecordAuthRequestEvent]{
		Id: "studio_shopify_account", Priority: -1000, Func: func(e *core.RecordAuthRequestEvent) error {
			login, ok := e.Get(shopifyLoginKey).(*shopifyLogin)
			if ok && login.service == s && e.AuthMethod == core.MFAMethodOAuth2 {
				if err := login.finish(e.App, e.Record.Id); err != nil {
					return e.InternalServerError("Unable to save the private Shopify identity.", nil)
				}
			}
			return e.Next()
		},
	})
	s.app.OnMailerRecordAuthAlertSend().Bind(&hook.Handler[*core.MailerRecordEvent]{
		Id: "studio_shopify_auth_alert", Priority: -1000, Func: deferShopifyAuthAlert,
	})
	s.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).Bind(&hook.Handler[*core.RecordRequestEvent]{
		Id: "studio_shopify_unlink", Priority: -1000, Func: s.unlinkShopifyIdentity,
	})
	bindIdentityRevocation(s.app, c.Id)
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
	// Native OAuth proposes e.Record by verified email or current auth header.
	// The Shopify login only accepts it when it is the customer's own account.
	login, err := startShopifyLogin(e.App, s.collectionID, verifiedCustomer{
		issuer: s.issuer, subject: p.subject, customerID: p.profile.ID,
		email: p.profile.EmailAddress.Email, emailVerified: e.OAuth2User.Email != "",
	}, e.Record)
	if err != nil {
		return err
	}
	login.service = s
	e.Set(shopifyLoginKey, login)
	if account := login.account(); account != nil {
		e.Record, e.IsNewRecord = account, false
	} else {
		e.CreateData = login.newAccountData(e.CreateData)
	}
	return e.Next()
}
