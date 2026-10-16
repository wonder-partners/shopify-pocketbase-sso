package shopifysso

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func useNewDevice(f *fixture) {
	f.client.Transport = oauthRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set("User-Agent", "Shopify alert test device")
		return http.DefaultTransport.RoundTrip(r)
	})
}

func TestShopifyAuthAlertDoesNotBlockDatabaseWritesOrLogin(t *testing.T) {
	f := setup(t, "")
	f.login() // Establish the initial device so that the next login needs an alert.
	useNewDevice(f)
	started, release, finished := make(chan bool, 1), make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseMail := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		releaseMail()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("alert sender did not finish")
		}
	})
	f.app.OnMailerRecordAuthAlertSend().BindFunc(func(e *core.MailerRecordEvent) error {
		defer close(finished)
		started <- e.App.TxInfo() != nil
		<-release
		return nil
	})
	f.loginShop()
	callback := f.authorize(false)
	loginDone := make(chan response, 1)
	go func() { loginDone <- f.completeCallback(callback) }()
	select {
	case inTransaction := <-started:
		if inTransaction {
			t.Error("alert delivery ran with the transactional app")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("alert did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := f.app.NonconcurrentDB().NewQuery("UPDATE users SET emailVisibility = emailVisibility").WithContext(ctx).Execute()
	if err != nil {
		t.Fatal("slow alert delivery blocked an independent database write")
	}
	select {
	case result := <-loginDone:
		f.expect(result, http.StatusOK)
		decodeAuth(t, result)
	case <-time.After(time.Second):
		t.Fatal("slow alert delivery blocked the login response")
	}
	releaseMail()
}

func TestRejectedShopifyAuthDoesNotSendAlert(t *testing.T) {
	for _, stage := range []string{"oauth_after", "auth_after", "http_error_after", "commit_failure"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t, "")
			f.login()
			useNewDevice(f)
			var sent atomic.Int32
			f.app.OnMailerRecordAuthAlertSend().BindFunc(func(e *core.MailerRecordEvent) error {
				sent.Add(1)
				return nil
			})
			if stage == "oauth_after" {
				f.app.OnRecordAuthWithOAuth2Request("users").BindFunc(func(e *core.RecordAuthWithOAuth2RequestEvent) error {
					if err := e.Next(); err != nil {
						return err
					}
					return e.ForbiddenError("Project policy denied login.", nil)
				})
			} else {
				if stage == "commit_failure" {
					for _, query := range []string{
						"CREATE TABLE alert_parent (id TEXT PRIMARY KEY)",
						"CREATE TABLE alert_child (parent_id TEXT REFERENCES alert_parent(id) DEFERRABLE INITIALLY DEFERRED)",
					} {
						if _, err := f.app.DB().NewQuery(query).Execute(); err != nil {
							t.Fatal(err)
						}
					}
				}
				f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
					if stage == "commit_failure" {
						if _, err := e.App.DB().NewQuery("INSERT INTO alert_child (parent_id) VALUES ('missing')").Execute(); err != nil {
							return err
						}
					}
					if err := e.Next(); err != nil {
						return err
					}
					switch stage {
					case "auth_after":
						return e.ForbiddenError("Project policy denied login.", nil)
					case "http_error_after":
						return e.JSON(http.StatusForbidden, map[string]string{"message": "Project policy denied login."})
					default:
						return nil
					}
				})
			}
			f.loginShop()
			result := f.completeCallback(f.authorize(false))
			if stage == "commit_failure" {
				if result.status < http.StatusBadRequest {
					t.Fatal("commit unexpectedly succeeded")
				}
			} else {
				f.expect(result, http.StatusForbidden)
			}
			if sent.Load() != 0 {
				t.Fatal("rejected login sent a new-device alert")
			}
		})
	}
}

func TestShopifyAuthAlertFailureDoesNotRejectLogin(t *testing.T) {
	f := setup(t, "")
	f.login()
	useNewDevice(f)
	delivered := make(chan bool, 1)
	f.app.OnMailerRecordAuthAlertSend().BindFunc(func(e *core.MailerRecordEvent) error {
		_, err := e.App.FindFirstRecordByFilter(accountsCollectionName, "user = {:user}", map[string]any{"user": e.Record.Id})
		delivered <- e.App.TxInfo() == nil && err == nil
		return errors.New("simulated mail delivery failure")
	})
	f.login()
	select {
	case committed := <-delivered:
		if !committed {
			t.Fatal("alert did not use the committed identity and original app")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("alert was not attempted")
	}
}

func TestNativePasswordAuthAlertKeepsItsHookBehavior(t *testing.T) {
	f := setup(t, "")
	nativeSignup(f, "native@example.test")
	useNewDevice(f)
	var sent atomic.Int32
	f.app.OnMailerRecordAuthAlertSend().BindFunc(func(e *core.MailerRecordEvent) error {
		sent.Add(1)
		return nil
	})
	result := f.request("POST", f.api.URL+"/api/collections/users/auth-with-password", "", `{"identity":"native@example.test","password":"TestPassword12345"}`, "")
	f.expect(result, http.StatusOK)
	if sent.Load() != 1 {
		t.Fatal("native password auth did not run the mail hook before responding")
	}
}

func TestNestedShopifyTransactionDefersAuthAlert(t *testing.T) {
	f := setup(t, "")
	f.login()
	useNewDevice(f)
	f.app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
		return withNestedAuthTransaction(e.RequestEvent, e.Next)
	})
	delivered := make(chan bool, 1)
	f.app.OnMailerRecordAuthAlertSend().BindFunc(func(e *core.MailerRecordEvent) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := e.App.NonconcurrentDB().NewQuery("UPDATE users SET emailVisibility = emailVisibility").WithContext(ctx).Execute()
		delivered <- e.App.TxInfo() == nil && err == nil
		return nil
	})
	f.login()
	select {
	case afterCommit := <-delivered:
		if !afterCommit {
			t.Fatal("nested auth transaction sent an alert before commit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nested transaction alert was not sent")
	}
}

func TestOuterShopifyTransactionUsesCommittedAppForAuthAlert(t *testing.T) {
	f := setup(t, "")
	f.login()
	useNewDevice(f)
	delivered := make(chan bool, 1)
	f.app.OnMailerRecordAuthAlertSend().BindFunc(func(e *core.MailerRecordEvent) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := e.App.NonconcurrentDB().NewQuery("UPDATE users SET emailVisibility = emailVisibility").WithContext(ctx).Execute()
		delivered <- e.App.TxInfo() == nil && err == nil
		return nil
	})
	serverURL := projectTransactionServer(t, f, "success")
	f.loginShop()
	data := callbackData(t, f.callback(f.authorize(false)).body)
	f.expect(f.request("POST", serverURL+"/api/collections/users/auth-with-oauth2", f.api.URL, nativeOAuthBody(t, data), ""), http.StatusOK)
	select {
	case afterCommit := <-delivered:
		if !afterCommit {
			t.Fatal("outer transaction alert used a closed transaction")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("outer transaction alert was not sent")
	}
}
