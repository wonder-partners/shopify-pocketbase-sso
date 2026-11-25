package shopifysso

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// commitRules are the only differences between the Shopify requests that use
// respondAfterCommit. The zero value suits a request that must write its
// response inside the transaction and has no multi-factor exception.
type commitRules struct {
	// A native MFA challenge (ErrMFA with a written 401) accepts the Shopify
	// factor: its changes commit so that the second factor can use them.
	acceptMFAChallenge bool
	// PocketBase writes the response itself from a transaction completion
	// callback (native record deletion writes its 204 that way). The response
	// may therefore be missing inside the transaction and stays buffered until
	// the outermost transaction has completed.
	responseWrittenOnComplete bool
}

// respondAfterCommit runs one Shopify request inside a database transaction and
// sends its response only once the matching outcome is final:
//
//   - the response is buffered while run executes;
//   - an error, or a written HTTP error response, rolls the changes back;
//   - the buffered response is sent after the commit, or after a rollback
//     caused by its own written HTTP error, never after any other failure;
//   - inside a host transaction, delivery waits for that transaction to
//     complete, and a written HTTP error is returned so the host rolls back.
//
// run receives the transactional app, which is also e.App while it runs. The
// event's App and Response are restored when the response is final.
func respondAfterCommit(e *core.RequestEvent, rules commitRules, run func(tx core.App) error) error {
	original, originalApp := e.Response, e.App
	captured := &authResponseBuffer{header: original.Header().Clone()}
	e.Response = captured
	keepCaptured := false
	defer func() {
		e.App = originalApp
		if !keepCaptured {
			e.Response = original
		}
	}()
	rejected := errors.New("Shopify hook wrote an error response")
	mfaAccepted := false
	err := originalApp.RunInTransaction(func(tx core.App) error {
		e.App = tx
		runErr := run(tx)
		// Exact equality prevents a joined MFA + policy error from committing.
		if rules.acceptMFAChallenge && runErr == apis.ErrMFA && captured.status == http.StatusUnauthorized {
			mfaAccepted = true
			return nil
		}
		if runErr != nil {
			return runErr
		}
		// A hook can stop the chain with nil without producing a response.
		// Reject it before commit so the resulting HTTP 500 leaves no changes.
		if !rules.responseWrittenOnComplete && !captured.Written() {
			return incompleteResponse(e)
		}
		if captured.status >= http.StatusBadRequest {
			return rejected
		}
		return nil
	})
	e.App = originalApp
	if !rules.responseWrittenOnComplete {
		e.Response = original
		if err != nil && err != rejected {
			return err
		}
		if !captured.Written() {
			return incompleteResponse(e)
		}
	}
	if outer := originalApp.TxInfo(); outer != nil {
		// The nested transaction has not committed yet. Keep a late native
		// response attached to the buffer until the host transaction completes.
		keepCaptured = rules.responseWrittenOnComplete
		outer.OnComplete(func(txErr error) error {
			if keepCaptured {
				e.Response = original
			}
			if captured.Written() && ((err == nil && txErr == nil) || (err == rejected && errors.Is(txErr, rejected))) {
				return captured.sendTo(original)
			}
			return nil
		})
		// A written rejection must reach the host so it rolls back too. ErrMFA
		// is not returned: it would roll back the accepted first factor.
		return err
	}
	if err != nil && err != rejected {
		return err
	}
	if !captured.Written() {
		return nil
	}
	writeErr := captured.sendTo(original)
	if mfaAccepted {
		return apis.ErrMFA
	}
	return writeErr
}

func incompleteResponse(e *core.RequestEvent) error {
	return e.InternalServerError("PocketBase auth response was not completed.", nil)
}

type authResponseBuffer struct {
	bytes.Buffer
	header http.Header
	status int
}

func (b *authResponseBuffer) sendTo(w http.ResponseWriter) error {
	for key, values := range b.header {
		w.Header()[key] = values
	}
	w.WriteHeader(b.status)
	_, err := w.Write(b.Bytes())
	return err
}

func (b *authResponseBuffer) Header() http.Header { return b.header }
func (b *authResponseBuffer) Written() bool       { return b.status != 0 }
func (b *authResponseBuffer) Status() int         { return b.status }
func (b *authResponseBuffer) WriteHeader(status int) {
	if !b.Written() {
		b.status = status
	}
}
func (b *authResponseBuffer) Write(data []byte) (int, error) {
	if !b.Written() {
		b.WriteHeader(http.StatusOK)
	}
	return b.Buffer.Write(data)
}
