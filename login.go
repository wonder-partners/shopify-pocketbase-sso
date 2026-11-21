package shopifysso

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

const shopifyLoginKey = "shopify_login"

type verifiedCustomer struct {
	issuer, subject, customerID, email string
	emailVerified                      bool
}

type shopifyLogin struct {
	customer verifiedCustomer
	existing *core.Record
	previous *shopifyIdentity
	service  *Service
}

func startShopifyLogin(app core.App, accounts string, customer verifiedCustomer, proposed *core.Record) (*shopifyLogin, error) {
	identity, err := findIdentity(app, customer.issuer, customer.subject)
	if err != nil {
		return nil, err
	}
	var r *core.Record
	if identity != nil {
		r, err = app.FindRecordById(accounts, identity.account())
		if err != nil {
			return nil, err
		}
	}
	link, err := app.FindFirstExternalAuthByExpr(dbx.HashExp{
		"collectionRef": accounts, "provider": providerName,
		"providerId": oauthLinkProviderID(customer.issuer, customer.subject),
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if link != nil {
		if r != nil && r.Id != link.RecordRef() {
			return nil, router.NewForbiddenError("Conflicting Shopify identity links.", nil)
		}
		r, err = app.FindRecordById(accounts, link.RecordRef())
		if err != nil {
			return nil, err
		}
	}
	if proposed != nil && (r == nil || proposed.Id != r.Id) {
		return nil, router.NewApiError(http.StatusConflict, "Shopify cannot automatically link an existing account.", nil)
	}
	emailOwner, err := app.FindAuthRecordByEmail(accounts, customer.email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if emailOwner != nil && (r == nil || emailOwner.Id != r.Id) {
		return nil, router.NewApiError(http.StatusConflict, "Shopify email conflicts with an existing account.", nil)
	}
	login := &shopifyLogin{customer: customer, existing: r, previous: identity}
	if r == nil {
		return login, nil
	}
	upgradeVerified := !r.Verified() && customer.emailVerified
	keepVerified := r.Verified() && strings.EqualFold(r.Email(), customer.email)
	r.SetEmail(customer.email)
	r.SetVerified(keepVerified || customer.emailVerified)
	if err := app.Save(r); err != nil {
		return nil, router.NewApiError(http.StatusConflict, "Unable to synchronize the Shopify profile.", nil)
	}
	// Verification revokes all old OAuth links. PocketBase still holds the
	// deleted Shopify link and won't recreate it, so restore only this exact
	// link within the auth transaction. Keep other links revoked.
	if upgradeVerified && link != nil {
		link.MarkAsNew()
		if err := app.Save(link); err != nil {
			return nil, router.NewApiError(http.StatusConflict, "Unable to restore the Shopify identity link.", nil)
		}
	}
	return login, nil
}

func (l *shopifyLogin) account() *core.Record { return l.existing }

func (l *shopifyLogin) newAccountData(data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}
	data["email"] = l.customer.email
	delete(data, "verified")
	delete(data, "password")
	delete(data, "passwordConfirm")
	return data
}

func (l *shopifyLogin) finish(app core.App, account string) error {
	return saveIdentity(app, account, l.customer.issuer, l.customer.subject, l.customer.customerID, l.previous)
}

func (s *Service) startLoginHook(e *core.RecordAuthWithOAuth2RequestEvent) error {
	if e.ProviderName != providerName {
		return e.Next()
	}
	p, ok := e.ProviderClient.(*shopifyProvider)
	if !ok || p.service != s || p.token == nil {
		return e.ForbiddenError("Missing verified Shopify identity.", nil)
	}
	login, err := startShopifyLogin(e.App, s.collectionID, p.customer, e.Record)
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

func (s *Service) finishLoginHook(e *core.RecordAuthRequestEvent) error {
	login, ok := e.Get(shopifyLoginKey).(*shopifyLogin)
	if ok && login.service == s && e.AuthMethod == core.MFAMethodOAuth2 {
		if err := login.finish(e.App, e.Record.Id); err != nil {
			return e.InternalServerError("Unable to save the private Shopify identity.", nil)
		}
	}
	return e.Next()
}
