package shopifysso

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

func committedResponseApp(t *testing.T) core.App {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.ResetBootstrapState() })
	for _, query := range []string{
		"CREATE TABLE marks (id TEXT PRIMARY KEY)",
		"CREATE TABLE commit_parent (id TEXT PRIMARY KEY)",
		"CREATE TABLE commit_child (parent_id TEXT REFERENCES commit_parent(id) DEFERRABLE INITIALLY DEFERRED)",
	} {
		if _, err := app.DB().NewQuery(query).Execute(); err != nil {
			t.Fatal(err)
		}
	}
	return app
}

func committed(t *testing.T, app core.App) bool {
	t.Helper()
	var count int
	if err := app.DB().NewQuery("SELECT COUNT(*) FROM marks").Row(&count); err != nil {
		t.Fatal(err)
	}
	return count > 0
}

var errStorage = errors.New("storage unavailable")

type serverLikeRecorder struct{ *httptest.ResponseRecorder }

func (r serverLikeRecorder) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	return r.ResponseRecorder.Write(data)
}

var committedResponseSteps = map[string]func(e *core.RequestEvent, tx core.App) error{
	"ok": func(e *core.RequestEvent, _ core.App) error {
		return e.JSON(http.StatusOK, map[string]bool{"ok": true})
	},
	"written_rejection": func(e *core.RequestEvent, _ core.App) error {
		return e.JSON(http.StatusForbidden, map[string]string{"message": "denied"})
	},
	"error": func(*core.RequestEvent, core.App) error {
		return errStorage
	},
	"no_response": func(*core.RequestEvent, core.App) error {
		return nil
	},
	"mfa": func(e *core.RequestEvent, _ core.App) error {
		if err := e.JSON(http.StatusUnauthorized, map[string]string{"mfaId": "x"}); err != nil {
			return err
		}
		return apis.ErrMFA
	},
	"mfa_without_challenge": func(*core.RequestEvent, core.App) error {
		return apis.ErrMFA
	},
	"written_on_complete": func(e *core.RequestEvent, tx core.App) error {
		tx.TxInfo().OnComplete(func(txErr error) error {
			if txErr == nil {
				return e.NoContent(http.StatusNoContent)
			}
			return nil
		})
		return nil
	},
	"written_then_error": func(e *core.RequestEvent, _ core.App) error {
		if err := e.NoContent(http.StatusNoContent); err != nil {
			return err
		}
		return errStorage
	},
	"mfa_commit_failure": func(e *core.RequestEvent, tx core.App) error {
		if _, err := tx.DB().NewQuery("INSERT INTO commit_child (parent_id) VALUES ('missing')").Execute(); err != nil {
			return err
		}
		if err := e.JSON(http.StatusUnauthorized, map[string]string{"mfaId": "x"}); err != nil {
			return err
		}
		return apis.ErrMFA
	},
	"commit_failure": func(e *core.RequestEvent, tx core.App) error {
		if _, err := tx.DB().NewQuery("INSERT INTO commit_child (parent_id) VALUES ('missing')").Execute(); err != nil {
			return err
		}
		return e.JSON(http.StatusOK, map[string]bool{"ok": true})
	},
}

func TestRespondAfterCommit(t *testing.T) {
	login := commitRules{acceptMFAChallenge: true}
	unlink := commitRules{responseWrittenOnComplete: true}
	type want struct {
		committed bool
		status    int
		err       error
	}
	errAny := errors.New("any error")
	cases := []struct {
		name  string
		rules commitRules
		step  string
		outer string
		want  want
	}{
		{"login ok", login, "ok", "", want{true, 200, nil}},
		{"login written rejection", login, "written_rejection", "", want{false, 403, nil}},
		{"login error", login, "error", "", want{false, 0, errStorage}},
		{"login without response", login, "no_response", "", want{false, 0, errAny}},
		{"login MFA challenge", login, "mfa", "", want{true, 401, apis.ErrMFA}},
		{"login MFA without challenge", login, "mfa_without_challenge", "", want{false, 0, apis.ErrMFA}},
		{"login commit failure", login, "commit_failure", "", want{false, 0, errAny}},
		{"login MFA challenge, commit failure", login, "mfa_commit_failure", "", want{false, 0, errAny}},
		{"login written then error", login, "written_then_error", "", want{false, 0, errStorage}},
		{"unlink written on complete", unlink, "written_on_complete", "", want{true, 204, nil}},
		{"unlink without response", unlink, "no_response", "", want{true, 0, nil}},
		{"unlink ok", unlink, "ok", "", want{true, 200, nil}},
		{"unlink written rejection", unlink, "written_rejection", "", want{false, 403, nil}},
		{"unlink error", unlink, "error", "", want{false, 0, errStorage}},
		{"unlink commit failure", unlink, "commit_failure", "", want{false, 0, errAny}},
		{"unlink written then error", unlink, "written_then_error", "", want{false, 0, errStorage}},
		{"unlink MFA is not accepted", unlink, "mfa", "", want{false, 0, apis.ErrMFA}},

		{"login ok, host commits", login, "ok", "commit", want{true, 200, nil}},
		{"login ok, host rolls back", login, "ok", "rollback", want{false, 0, errAny}},
		{"login written rejection, host propagates", login, "written_rejection", "propagate", want{false, 403, errAny}},
		{"login written rejection, host rolls back", login, "written_rejection", "rollback", want{false, 0, errAny}},
		{"login error, host propagates", login, "error", "propagate", want{false, 0, errStorage}},
		{"login MFA challenge, host commits", login, "mfa", "commit", want{true, 401, nil}},
		{"login MFA challenge, host rolls back", login, "mfa", "rollback", want{false, 0, errAny}},
		{"unlink written on complete, host commits", unlink, "written_on_complete", "commit", want{true, 204, nil}},
		{"unlink written on complete, host rolls back", unlink, "written_on_complete", "rollback", want{false, 0, errAny}},
		{"unlink written rejection, host propagates", unlink, "written_rejection", "propagate", want{false, 403, errAny}},
		{"unlink error, host propagates", unlink, "error", "propagate", want{false, 0, errStorage}},
		{"unlink ok, host writes then rolls back", unlink, "written_on_complete", "write_then_rollback", want{false, 0, errAny}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			app := committedResponseApp(t)
			recorder := httptest.NewRecorder()
			e := &core.RequestEvent{App: app}
			writer := serverLikeRecorder{recorder}
			e.Response = writer
			e.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			run := func(tx core.App) error {
				if e.App != tx {
					t.Error("run did not receive the transactional app as e.App")
				}
				if _, err := tx.DB().NewQuery("INSERT INTO marks (id) VALUES ('m')").Execute(); err != nil {
					return err
				}
				return committedResponseSteps[c.step](e, tx)
			}

			var err error
			if c.outer == "" {
				err = respondAfterCommit(e, c.rules, run)
			} else {
				hostErr := errors.New("host rolled back")
				err = app.RunInTransaction(func(tx core.App) error {
					e.App = tx
					innerErr := respondAfterCommit(e, c.rules, run)
					if recorder.Code != http.StatusOK || recorder.Body.Len() > 0 || len(recorder.Header()) > 0 {
						t.Error("response was sent before the host transaction completed")
					}
					switch c.outer {
					case "write_then_rollback":
						if err := e.JSON(http.StatusOK, map[string]string{"host": "done"}); err != nil {
							t.Fatal(err)
						}
						return hostErr
					case "rollback":
						return hostErr
					case "propagate":
						return innerErr
					}
					if innerErr != nil {
						t.Errorf("unexpected inner error: %v", innerErr)
					}
					return nil
				})
				e.App = app
			}

			switch {
			case c.want.err == nil && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.want.err == errAny && err == nil:
				t.Fatal("expected an error")
			case c.want.err != nil && c.want.err != errAny && !errors.Is(err, c.want.err):
				t.Fatalf("error = %v, want %v", err, c.want.err)
			}
			if got := committed(t, app); got != c.want.committed {
				t.Fatalf("committed = %v, want %v", got, c.want.committed)
			}
			sent := recorder.Body.Len() > 0 || len(recorder.Header()) > 0 || recorder.Code != http.StatusOK
			switch {
			case c.want.status == 0 && sent:
				t.Fatalf("unexpected response %d %q", recorder.Code, recorder.Body.String())
			case c.want.status != 0 && recorder.Code != c.want.status:
				t.Fatalf("status = %d, want %d", recorder.Code, c.want.status)
			case c.want.status != 0 && c.want.status != http.StatusNoContent && recorder.Body.Len() == 0:
				t.Fatal("buffered response body was lost")
			}
			if e.Response != http.ResponseWriter(writer) || e.App != app {
				t.Fatal("event App or Response was not restored")
			}
		})
	}
}
