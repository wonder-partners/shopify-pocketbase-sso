package shopifysso

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/types"
)

func TestRejectedShopifyLoginRollsBackProfile(t *testing.T) {
	for _, stage := range []string{"oauth_before", "oauth_after", "early_oauth_after", "auth_before", "auth_after", "enrich", "auth_rule", "http_error"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "email")
			f.login()
			_, before := f.authRecord()
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": before.Id})
			if err != nil {
				t.Fatal(err)
			}
			f.mock.Fault = ""
			f.mock.ProfileEmail = "changed@example.test"

			switch stage {
			case "oauth_before", "oauth_after", "early_oauth_after":
				priority := 0
				if stage == "early_oauth_after" {
					priority = -2000
				}
				f.app.OnRecordAuthWithOAuth2Request("users").Bind(&hook.Handler[*core.RecordAuthWithOAuth2RequestEvent]{
					Priority: priority, Func: func(e *core.RecordAuthWithOAuth2RequestEvent) error {
						if stage != "oauth_before" {
							if err := e.Next(); err != nil {
								return err
							}
						}
						return e.ForbiddenError("OAuth denied by the project.", nil)
					},
				})
			case "auth_before", "auth_after", "http_error":
				f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
					if stage == "auth_after" {
						if err := e.Next(); err != nil {
							return err
						}
					}
					if stage == "http_error" {
						return e.JSON(http.StatusForbidden, map[string]string{"message": "Project policy denied login."})
					}
					return e.ForbiddenError("Authentication denied by the project.", nil)
				})
			case "enrich":
				f.app.OnRecordEnrich("users").BindFunc(func(e *core.RecordEnrichEvent) error {
					return errors.New("profile enrichment denied")
				})
			case "auth_rule":
				c, _ := f.app.FindCollectionByNameOrId("users")
				c.AuthRule = types.Pointer("email = 'camille@example.test'")
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
			}

			f.loginShop()
			result := f.completeCallback(f.authorize(false))
			status := http.StatusForbidden
			if stage == "enrich" {
				status = http.StatusBadRequest
			}
			f.expect(result, status)
			after, err := f.app.FindRecordById("users", before.Id)
			if err != nil {
				t.Fatal(err)
			}
			if after.Email() != before.Email() || after.Verified() != before.Verified() {
				t.Fatal("rejected login persisted the Shopify email or verification change")
			}
			if _, err := f.app.FindRecordById(core.CollectionNameExternalAuths, link.Id); err != nil {
				t.Fatal("rejected verification upgrade lost the original OAuth link", err)
			}
		})
	}
}

func TestRejectedShopifySignupRollsBackAccountAndIdentities(t *testing.T) {
	for _, stage := range []string{"oauth_after", "auth_before", "auth_after", "auth_rule", "identity_storage"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "")
			switch stage {
			case "oauth_after":
				f.app.OnRecordAuthWithOAuth2Request("users").BindFunc(func(e *core.RecordAuthWithOAuth2RequestEvent) error {
					if err := e.Next(); err != nil {
						return err
					}
					return e.ForbiddenError("OAuth denied by the project.", nil)
				})
			case "auth_before", "auth_after":
				f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
					if stage == "auth_after" {
						if err := e.Next(); err != nil {
							return err
						}
					}
					return e.ForbiddenError("Authentication denied by the project.", nil)
				})
			case "auth_rule":
				c, _ := f.app.FindCollectionByNameOrId("users")
				c.AuthRule = types.Pointer("email = 'allowed@example.test'")
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
			case "identity_storage":
				f.app.OnRecordCreate(accountsCollectionName).BindFunc(func(e *core.RecordEvent) error {
					return errors.New("identity storage unavailable")
				})
			}
			f.loginShop()
			status := http.StatusForbidden
			if stage == "identity_storage" {
				status = http.StatusInternalServerError
			}
			f.expect(f.completeCallback(f.authorize(false)), status)
			for _, collection := range []string{"users", core.CollectionNameExternalAuths, accountsCollectionName} {
				count, err := f.app.CountRecords(collection)
				if err != nil || count != 0 {
					t.Fatalf("rejected signup left records in %s", collection)
				}
			}
		})
	}
}

func TestShopifyMFAPersistsProfileAndUsableChallenge(t *testing.T) {
	f := setup(t, "email")
	f.login()
	_, user := f.authRecord()
	user.SetPassword("TestPassword12345")
	if err := f.app.Save(user); err != nil {
		t.Fatal(err)
	}
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.MFA.Enabled, c.OTP.Enabled = true, true
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	f.mock.Fault = ""
	f.mock.ProfileEmail = "changed@example.test"
	f.loginShop()
	result := f.completeCallback(f.authorize(false))
	f.expect(result, http.StatusUnauthorized)
	var challenge struct {
		MFAID string `json:"mfaId"`
	}
	if json.Unmarshal([]byte(result.body), &challenge) != nil || challenge.MFAID == "" {
		t.Fatal("missing native MFA challenge")
	}
	if _, err := f.app.FindMFAById(challenge.MFAID); err != nil {
		t.Fatal("MFA challenge was rolled back", err)
	}
	after, err := f.app.FindRecordById("users", user.Id)
	if err != nil || after.Email() != f.mock.ProfileEmail || !after.Verified() {
		t.Fatal("accepted Shopify factor did not synchronize the profile")
	}
	privateAccount(f, user.Id)
	if _, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id}); err != nil {
		t.Fatal("verification upgrade lost the Shopify link during MFA", err)
	}
	body := `{"identity":"changed@example.test","password":"TestPassword12345","mfaId":"` + challenge.MFAID + `"}`
	second := f.request("POST", f.api.URL+"/api/collections/users/auth-with-password", "", body, "")
	f.expect(second, http.StatusOK)
	if decodeAuth(t, second).Record["id"] != user.Id {
		t.Fatal("second factor authenticated another account")
	}
}

func TestShopifyDenialAfterMFAIsRolledBack(t *testing.T) {
	f := setup(t, "")
	c, _ := f.app.FindCollectionByNameOrId("users")
	c.MFA.Enabled, c.OTP.Enabled = true, true
	if err := f.app.Save(c); err != nil {
		t.Fatal(err)
	}
	f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
		if err := e.Next(); err != apis.ErrMFA {
			t.Fatal("expected a native MFA challenge")
		}
		return errors.Join(apis.ErrMFA, e.ForbiddenError("Project denied the Shopify factor.", nil))
	})
	f.loginShop()
	result := f.completeCallback(f.authorize(false))
	f.expect(result, http.StatusForbidden)
	if strings.Contains(result.body, "mfaId") {
		t.Fatal("rejected MFA attempt exposed a challenge or auth token")
	}
	for _, collection := range []string{"users", core.CollectionNameExternalAuths, accountsCollectionName, core.CollectionNameMFAs} {
		count, err := f.app.CountRecords(collection)
		if err != nil || count != 0 {
			t.Fatalf("rejected MFA attempt left records in %s", collection)
		}
	}
}

func TestShopifyCommitFailureDoesNotExposeAuthResponse(t *testing.T) {
	for _, mfa := range []bool{false, true} {
		name := "session"
		if mfa {
			name = "mfa"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t, "")
			if mfa {
				c, _ := f.app.FindCollectionByNameOrId("users")
				c.MFA.Enabled, c.OTP.Enabled = true, true
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
			}
			// A deferred foreign key lets all auth hooks succeed but rejects COMMIT.
			for _, query := range []string{
				"CREATE TABLE oauth_commit_parent (id TEXT PRIMARY KEY)",
				"CREATE TABLE oauth_commit_child (parent_id TEXT REFERENCES oauth_commit_parent(id) DEFERRABLE INITIALLY DEFERRED)",
			} {
				if _, err := f.app.DB().NewQuery(query).Execute(); err != nil {
					t.Fatal(err)
				}
			}
			inserted := false
			f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
				if _, err := e.App.DB().NewQuery("INSERT INTO oauth_commit_child (parent_id) VALUES ('missing')").Execute(); err != nil {
					return err
				}
				inserted = true
				return e.Next()
			})
			f.loginShop()
			result := f.completeCallback(f.authorize(false))
			if !inserted || result.status < http.StatusBadRequest || strings.Contains(result.body, `"token"`) || strings.Contains(result.body, "mfaId") {
				t.Fatal("failed commit exposed a JWT or an unusable MFA challenge")
			}
			for _, collection := range []string{"users", core.CollectionNameExternalAuths, accountsCollectionName, core.CollectionNameMFAs} {
				count, err := f.app.CountRecords(collection)
				if err != nil || count != 0 {
					t.Fatalf("failed commit left records in %s", collection)
				}
			}
		})
	}
}

type oauthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f oauthRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestShopifyRequestsRunOutsideTheAuthTransaction(t *testing.T) {
	f := setup(t, "")
	original := f.s.client.Transport
	tokenRequests := 0
	f.s.client.Transport = oauthRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		// This pool has one connection. An open auth transaction would hold it
		// and prevent this query from completing before its deadline.
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		var result int
		if err := f.app.NonconcurrentDB().NewQuery("SELECT 1").WithContext(ctx).Row(&result); err != nil {
			return nil, errors.New("Shopify request ran while the database transaction was open")
		}
		if r.URL.Path == "/token" {
			tokenRequests++
		}
		return original.RoundTrip(r)
	})
	f.login()
	if tokenRequests != 1 {
		t.Fatalf("expected one token exchange, got %d", tokenRequests)
	}
}
