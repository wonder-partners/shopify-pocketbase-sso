package shopifysso

import (
	"errors"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

func testCustomer(email string, emailVerified bool) verifiedCustomer {
	return verifiedCustomer{
		issuer: testIssuer, subject: "42", customerID: "gid://shopify/Customer/42",
		email: email, emailVerified: emailVerified,
	}
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	var apiErr *router.ApiError
	if !errors.As(err, &apiErr) || apiErr.Status != status {
		t.Fatalf("error = %v, want HTTP %d", err, status)
	}
}

func reload(t *testing.T, app core.App, r *core.Record) *core.Record {
	t.Helper()
	fresh, err := app.FindRecordById(r.Collection().Id, r.Id)
	if err != nil {
		t.Fatal(err)
	}
	return fresh
}

func TestShopifyLoginCreatesANewAccount(t *testing.T) {
	app, accounts := identityStoreApp(t)
	login, err := startShopifyLogin(app, accounts.Id, testCustomer("camille@example.test", true), nil)
	if err != nil {
		t.Fatal(err)
	}
	if login.account() != nil {
		t.Fatal("a new customer resumed an account")
	}
	data := login.newAccountData(map[string]any{
		"email": "chosen@example.test", "verified": true,
		"password": "Chosen12345678", "passwordConfirm": "Chosen12345678", "name": "Camille",
	})
	if data["email"] != "camille@example.test" || data["name"] != "Camille" {
		t.Fatal("new account data lost the Shopify email or other fields", data)
	}
	for _, field := range []string{"verified", "password", "passwordConfirm"} {
		if _, ok := data[field]; ok {
			t.Fatalf("the browser could choose %s for a Shopify account", field)
		}
	}
	if data := (&shopifyLogin{customer: testCustomer("camille@example.test", true)}).newAccountData(nil); data["email"] != "camille@example.test" {
		t.Fatal("missing browser data did not get the Shopify email")
	}
	// PocketBase creates the account between both steps.
	account := newAccount(t, app, accounts, "camille@example.test", true)
	if err := login.finish(app, account.Id); err != nil {
		t.Fatal(err)
	}
	if found, err := findIdentity(app, testIssuer, "42"); err != nil || found == nil || found.account() != account.Id {
		t.Fatal("finishing the login did not save its Shopify identity", err)
	}
}

func TestShopifyLoginResumesTheAccount(t *testing.T) {
	for _, c := range []struct {
		name     string
		identity bool // the account has a Shopify identity
		link     bool // the account has the matching OAuth link
	}{
		{"by Shopify identity", true, true},
		{"by Shopify identity without OAuth link", true, false},
		// An interrupted login committed the OAuth link but not the identity.
		{"by exact OAuth link only", false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, accounts := identityStoreApp(t)
			camille := newAccount(t, app, accounts, "camille@example.test", true)
			if c.identity {
				if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
					t.Fatal(err)
				}
			}
			if c.link {
				newOAuthLink(t, app, camille, providerName, oauthLinkProviderID(testIssuer, "42"))
			}
			login, err := startShopifyLogin(app, accounts.Id, testCustomer("camille@example.test", true), nil)
			if err != nil {
				t.Fatal(err)
			}
			if login.account() == nil || login.account().Id != camille.Id {
				t.Fatal("the customer did not resume their account")
			}
			if err := login.finish(app, camille.Id); err != nil {
				t.Fatal(err)
			}
			if identityCount(t, app, camille.Id) != 1 {
				t.Fatal("finishing the login did not keep exactly one identity")
			}
		})
	}
}

func TestShopifyLoginRefusals(t *testing.T) {
	for _, c := range []struct {
		name   string
		setup  func(t *testing.T, app core.App, accounts *core.Collection) (candidate *core.Record)
		status int
	}{
		{"identity and OAuth link disagree", func(t *testing.T, app core.App, accounts *core.Collection) *core.Record {
			camille := newAccount(t, app, accounts, "camille@example.test", true)
			other := newAccount(t, app, accounts, "other@example.test", true)
			if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
				t.Fatal(err)
			}
			newOAuthLink(t, app, other, providerName, oauthLinkProviderID(testIssuer, "42"))
			return nil
		}, http.StatusForbidden},
		{"email belongs to an unlinked account", func(t *testing.T, app core.App, accounts *core.Collection) *core.Record {
			newAccount(t, app, accounts, "camille@example.test", true)
			return nil
		}, http.StatusConflict},
		{"PocketBase proposes an unlinked account", func(t *testing.T, app core.App, accounts *core.Collection) *core.Record {
			return newAccount(t, app, accounts, "signed-in@example.test", true)
		}, http.StatusConflict},
		{"PocketBase proposes another account than the identity's", func(t *testing.T, app core.App, accounts *core.Collection) *core.Record {
			camille := newAccount(t, app, accounts, "camille@example.test", true)
			if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
				t.Fatal(err)
			}
			return newAccount(t, app, accounts, "signed-in@example.test", true)
		}, http.StatusConflict},
		{"new Shopify email belongs to another account", func(t *testing.T, app core.App, accounts *core.Collection) *core.Record {
			camille := newAccount(t, app, accounts, "old@example.test", true)
			if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
				t.Fatal(err)
			}
			newAccount(t, app, accounts, "camille@example.test", true)
			return nil
		}, http.StatusConflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, accounts := identityStoreApp(t)
			candidate := c.setup(t, app, accounts)
			login, err := startShopifyLogin(app, accounts.Id, testCustomer("camille@example.test", true), candidate)
			if login != nil {
				t.Fatal("a refused login was started")
			}
			wantStatus(t, err, c.status)
		})
	}
}

func TestShopifyLoginAcceptsTheProposedAccountWhenItIsTheSame(t *testing.T) {
	app, accounts := identityStoreApp(t)
	camille := newAccount(t, app, accounts, "camille@example.test", true)
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
		t.Fatal(err)
	}
	login, err := startShopifyLogin(app, accounts.Id, testCustomer("camille@example.test", true), camille)
	if err != nil || login.account().Id != camille.Id {
		t.Fatal("the account PocketBase proposed was refused although it is the customer's", err)
	}
}

func TestShopifyLoginSynchronizesTheAccount(t *testing.T) {
	for _, c := range []struct {
		name          string
		accountEmail  string
		verified      bool
		shopifyEmail  string
		emailVerified bool
		wantVerified  bool
	}{
		{"verified by Shopify", "camille@example.test", false, "camille@example.test", true, true},
		{"same email stays verified", "Camille@Example.test", true, "camille@example.test", false, true},
		{"changed unverified email", "old@example.test", true, "camille@example.test", false, false},
		{"changed verified email", "old@example.test", true, "camille@example.test", true, true},
		{"still unverified", "camille@example.test", false, "camille@example.test", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			app, accounts := identityStoreApp(t)
			camille := newAccount(t, app, accounts, c.accountEmail, c.verified)
			if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
				t.Fatal(err)
			}
			link := newOAuthLink(t, app, camille, providerName, oauthLinkProviderID(testIssuer, "42"))
			otherLink := newOAuthLink(t, app, camille, "google", "google-id")
			identityID := privateIdentityOf(t, app, camille.Id).Id
			login, err := startShopifyLogin(app, accounts.Id, testCustomer(c.shopifyEmail, c.emailVerified), nil)
			if err != nil {
				t.Fatal(err)
			}
			after := reload(t, app, camille)
			if after.Email() != c.shopifyEmail || after.Verified() != c.wantVerified {
				t.Fatalf("account = %s verified=%v, want %s verified=%v", after.Email(), after.Verified(), c.shopifyEmail, c.wantVerified)
			}
			// Becoming verified revokes all OAuth links; only this login's comes back.
			upgraded := !c.verified && c.wantVerified
			if _, err := app.FindRecordById(core.CollectionNameExternalAuths, link.Id); err != nil {
				t.Fatal("the login's own OAuth link was lost", err)
			}
			if _, err := app.FindRecordById(core.CollectionNameExternalAuths, otherLink.Id); (err == nil) == upgraded {
				t.Fatal("verification kept another OAuth link, or a plain login removed it", err)
			}
			if err := login.finish(app, camille.Id); err != nil {
				t.Fatal(err)
			}
			if privateIdentityOf(t, app, camille.Id).Id != identityID {
				t.Fatal("the login did not keep its own Shopify identity")
			}
		})
	}
}

func TestShopifyLoginRefusesWhenTheOAuthLinkCannotBeRestored(t *testing.T) {
	app, accounts := identityStoreApp(t)
	camille := newAccount(t, app, accounts, "camille@example.test", false)
	if err := saveIdentity(app, camille.Id, testIssuer, "42", "gid://shopify/Customer/42", nil); err != nil {
		t.Fatal(err)
	}
	link := newOAuthLink(t, app, camille, providerName, oauthLinkProviderID(testIssuer, "42"))
	app.OnRecordCreate(core.CollectionNameExternalAuths).BindFunc(func(*core.RecordEvent) error {
		return errors.New("storage unavailable")
	})
	// The auth transaction rolls the verification back with the refusal.
	err := app.RunInTransaction(func(tx core.App) error {
		_, err := startShopifyLogin(tx, accounts.Id, testCustomer("camille@example.test", true), nil)
		return err
	})
	wantStatus(t, err, http.StatusConflict)
	if after := reload(t, app, camille); after.Verified() {
		t.Fatal("a refused login kept the verification")
	}
	if _, err := app.FindRecordById(core.CollectionNameExternalAuths, link.Id); err != nil {
		t.Fatal("a refused login lost the OAuth link", err)
	}
}
