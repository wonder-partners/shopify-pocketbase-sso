package shopifysso

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

func projectTransactionServer(t *testing.T, f *fixture, scenario string) string {
	t.Helper()
	if scenario == "commit_failure" {
		for _, query := range []string{
			"CREATE TABLE project_parent (id TEXT PRIMARY KEY)",
			"CREATE TABLE project_child (parent_id TEXT REFERENCES project_parent(id) DEFERRABLE INITIALLY DEFERRED)",
		} {
			if _, err := f.app.DB().NewQuery(query).Execute(); err != nil {
				t.Fatal(err)
			}
		}
	}
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
			if scenario == "commit_failure" {
				_, err := tx.DB().NewQuery("INSERT INTO project_child (parent_id) VALUES ('missing')").Execute()
				return err
			}
			return nil
		})
	}})
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
	for _, mode := range []string{"native", "custom", "http_denied", "error"} {
		for _, scenario := range []string{"success", "denied", "commit_failure"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				f := setup(t, "")
				f.login()
				token, user := f.authRecord()
				accountID := privateAccount(f, user.Id).Id
				link, err := f.app.FindFirstExternalAuthByExpr(dbx.HashExp{"provider": providerName, "recordRef": user.Id})
				if err != nil {
					t.Fatal(err)
				}
				if mode != "native" {
					f.app.OnRecordDeleteRequest(core.CollectionNameExternalAuths).BindFunc(func(e *core.RecordRequestEvent) error {
						if err := e.Next(); err != nil {
							return err
						}
						if mode == "error" {
							return e.ForbiddenError("Project denied unlink.", nil)
						}
						status := http.StatusOK
						if mode == "http_denied" {
							status = http.StatusForbidden
						}
						e.Response.Header().Set("X-Project-Unlink", "custom")
						return e.JSON(status, map[string]string{"message": mode})
					})
				}
				serverURL := projectTransactionServer(t, f, scenario)
				result := f.request("DELETE", serverURL+"/api/collections/_externalAuths/records/"+link.Id, "", "", token)
				accepted := scenario == "success" && (mode == "native" || mode == "custom")
				if accepted {
					status := http.StatusNoContent
					if mode == "custom" {
						status = http.StatusOK
					}
					f.expect(result, status)
				} else {
					if result.status < http.StatusBadRequest {
						t.Fatal("outer rollback exposed successful unlink", result.status, result.body)
					}
					if scenario == "denied" || mode == "http_denied" || mode == "error" {
						f.expect(result, http.StatusForbidden)
					}
				}
				if mode == "http_denied" || (mode == "custom" && accepted) {
					if result.body != `{"message":"`+mode+`"}` || result.headers.Get("X-Project-Unlink") != "custom" {
						t.Fatal("outer transaction lost the custom response", result.body)
					}
				}
				for _, target := range []struct{ collection, id string }{{core.CollectionNameExternalAuths, link.Id}, {accountsCollectionName, accountID}} {
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
}

func TestShopifyAuthWaitsForOuterTransaction(t *testing.T) {
	for _, mode := range []string{"login", "signup", "mfa", "http_denial"} {
		for _, scenario := range []string{"success", "denied", "commit_failure"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				f := setup(t, "email")
				var before *core.Record
				if mode == "login" {
					f.login()
					_, before = f.authRecord()
				}
				if mode == "mfa" {
					c, _ := f.app.FindCollectionByNameOrId("users")
					c.MFA.Enabled, c.OTP.Enabled = true, true
					if err := f.app.Save(c); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "http_denial" {
					f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
						if err := e.Next(); err != nil {
							return err
						}
						return e.JSON(http.StatusForbidden, map[string]string{"message": "Inner policy denied login."})
					})
				}
				f.mock.Fault = ""
				f.mock.ProfileEmail = "changed@example.test"
				serverURL := projectTransactionServer(t, f, scenario)
				f.loginShop()
				data := callbackData(t, f.callback(f.authorize(false)).body)
				result := f.request("POST", serverURL+"/api/collections/users/auth-with-oauth2", f.api.URL, nativeOAuthBody(t, data), "")
				accepted := scenario == "success" && mode != "http_denial"
				if accepted {
					status := http.StatusOK
					if mode == "mfa" {
						status = http.StatusUnauthorized
					}
					f.expect(result, status)
					if mode == "mfa" {
						var challenge struct {
							MFAID string `json:"mfaId"`
						}
						if err := json.Unmarshal([]byte(result.body), &challenge); err != nil {
							t.Fatal(err)
						}
						if _, err := f.app.FindMFAById(challenge.MFAID); err != nil {
							t.Fatal("outer commit lost the MFA challenge", err)
						}
					} else if _, err := f.app.FindAuthRecordByToken(decodeAuth(t, result).Token, core.TokenTypeAuth); err != nil {
						t.Fatal("outer commit returned an invalid token", err)
					}
				} else {
					if result.status < 400 || strings.Contains(result.body, `"token"`) || strings.Contains(result.body, "mfaId") {
						t.Fatal("outer rollback exposed authentication", result.status, result.body)
					}
					if scenario == "denied" || mode == "http_denial" {
						f.expect(result, http.StatusForbidden)
					}
				}
				if before != nil {
					after, err := f.app.FindRecordById("users", before.Id)
					if err != nil {
						t.Fatal(err)
					}
					expected := before.Email()
					if accepted {
						expected = f.mock.ProfileEmail
					}
					if after.Email() != expected {
						t.Fatal("outer transaction persisted the wrong profile")
					}
				} else {
					for _, collection := range []string{"users", core.CollectionNameExternalAuths, accountsCollectionName, core.CollectionNameMFAs} {
						want := 0
						if accepted && (collection != core.CollectionNameMFAs || mode == "mfa") {
							want = 1
						}
						if count, err := f.app.CountRecords(collection); err != nil || count != int64(want) {
							t.Fatalf("unexpected records in %s: %d, %v", collection, count, err)
						}
					}
				}
			})
		}
	}
}
