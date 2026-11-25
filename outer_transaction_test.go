package shopifysso

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

func projectTransactionServer(t *testing.T, f *fixture, scenario string) string {
	t.Helper()
	r, err := apis.NewRouter(f.app)
	if err != nil {
		t.Fatal(err)
	}
	r.Bind(&hook.Handler[*core.RequestEvent]{Priority: -1, Func: func(e *core.RequestEvent) error {
		original := e.App
		response := e.Response
		defer func() { e.App = original }()
		return e.App.RunInTransaction(func(tx core.App) error {
			e.App = tx
			if err := e.Next(); err != nil {
				return err
			}
			if tracker, ok := response.(interface{ Written() bool }); ok && tracker.Written() {
				t.Error("Shopify response was sent before the outer transaction completed")
			}
			if scenario == "denied" {
				return e.ForbiddenError("Outer transaction denied the request.", nil)
			}
			return nil
		})
	}})
	compression := apis.Gzip()
	compression.Priority = -2
	r.Bind(compression)
	f.s.Register(r)
	mux, err := r.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL
}

func TestShopifyUnlinkWaitsForOuterTransaction(t *testing.T) {
	for _, scenario := range []string{"success", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t, "")
			f.login()
			token, user := f.authRecord()
			identityRecordID := privateIdentity(f, user.Id).Id
			link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
			if err != nil {
				t.Fatal(err)
			}
			serverURL := projectTransactionServer(t, f, scenario)
			result := f.request("DELETE", serverURL+"/api/collections/_externalAuths/records/"+link.Id, "", "", token)
			accepted := scenario == "success"
			if accepted {
				f.expect(result, http.StatusNoContent)
			} else {
				f.expect(result, http.StatusForbidden)
			}
			for _, target := range []struct{ collection, id string }{{core.CollectionNameExternalAuths, link.Id}, {identitiesCollectionName, identityRecordID}} {
				_, err := f.app.FindRecordById(target.collection, target.id)
				if accepted {
					if !errors.Is(err, sql.ErrNoRows) {
						t.Fatal("committed unlink retained an identity", err)
					}
				} else if err != nil {
					t.Fatal("rejected unlink deleted an identity", err)
				}
			}
		})
	}
}

func TestShopifyAuthWaitsForOuterTransaction(t *testing.T) {
	for _, mode := range []string{"login", "mfa"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t, "email")
			var before *core.Record
			if mode == "login" {
				f.login()
				_, before = f.authRecord()
			} else {
				c, _ := f.app.FindCollectionByNameOrId("users")
				c.MFA.Enabled, c.OTP.Enabled = true, true
				if err := f.app.Save(c); err != nil {
					t.Fatal(err)
				}
			}
			f.mock.Fault = ""
			f.mock.ProfileEmail = "changed@example.test"
			serverURL := projectTransactionServer(t, f, "success")
			f.loginStore()
			data := callbackData(t, f.callback(f.authorize(false)).body)
			result := f.request("POST", serverURL+"/api/collections/users/auth-with-oauth2", f.api.URL, nativeOAuthBody(t, data), "")
			if mode == "login" {
				f.expect(result, http.StatusOK)
				if _, err := f.app.FindAuthRecordByToken(decodeAuth(t, result).Token, core.TokenTypeAuth); err != nil {
					t.Fatal("outer commit returned an invalid token", err)
				}
				after, err := f.app.FindRecordById("users", before.Id)
				if err != nil {
					t.Fatal(err)
				}
				if after.Email() != f.mock.ProfileEmail {
					t.Fatal("outer transaction persisted the wrong profile")
				}
				return
			}
			f.expect(result, http.StatusUnauthorized)
			var challenge struct {
				MFAID string `json:"mfaId"`
			}
			if err := json.Unmarshal([]byte(result.body), &challenge); err != nil {
				t.Fatal(err)
			}
			if _, err := f.app.FindMFAById(challenge.MFAID); err != nil {
				t.Fatal("outer commit lost the MFA challenge", err)
			}
			for _, collection := range []string{"users", core.CollectionNameExternalAuths, identitiesCollectionName, core.CollectionNameMFAs} {
				if count, err := f.app.CountRecords(collection); err != nil || count != 1 {
					t.Fatalf("unexpected records in %s: %d, %v", collection, count, err)
				}
			}
		})
	}
}
