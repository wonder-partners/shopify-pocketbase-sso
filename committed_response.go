package shopifysso

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

type commitRules struct {
	acceptMFAChallenge        bool
	responseWrittenOnComplete bool
}

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
		if rules.acceptMFAChallenge && isPureMFAChallenge(runErr, captured.status) {
			mfaAccepted = true
			return nil
		}
		if runErr != nil {
			return runErr
		}
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

func isPureMFAChallenge(err error, status int) bool {
	return err == apis.ErrMFA && status == http.StatusUnauthorized
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
