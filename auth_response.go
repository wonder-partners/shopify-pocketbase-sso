package shopifysso

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Commit the profile and native identity links only when the entire OAuth chain
// accepts the request. A native MFA challenge accepts the Shopify factor and
// must persist so that the second factor can use it. Buffer responses until the
// transaction finishes, including errors written by hooks without returning one.
func transactionalAuthResponse(e *core.RequestEvent, action func() error) error {
	original := e.Response
	originalApp := e.App
	captured := &authResponseBuffer{header: original.Header().Clone()}
	e.Response = captured
	defer func() {
		e.Response, e.App = original, originalApp
	}()
	var actionErr error
	rejectedResponse := errors.New("OAuth hook wrote an error response")
	err := originalApp.RunInTransaction(func(tx core.App) error {
		e.App = tx
		actionErr = action()
		// Exact equality prevents a joined MFA + policy error from committing.
		if actionErr == apis.ErrMFA && captured.status == http.StatusUnauthorized {
			return nil
		}
		if actionErr != nil {
			return actionErr
		}
		if captured.status >= http.StatusBadRequest {
			return rejectedResponse
		}
		return nil
	})
	e.Response = original
	e.App = originalApp
	if err != nil && err != rejectedResponse {
		return err
	}
	if !captured.Written() {
		return e.InternalServerError("PocketBase auth response was not completed.", nil)
	}
	for key, values := range captured.header {
		original.Header()[key] = values
	}
	original.WriteHeader(captured.status)
	_, writeErr := original.Write(captured.Bytes())
	if actionErr != nil {
		return actionErr
	}
	return writeErr
}

type authResponseBuffer struct {
	bytes.Buffer
	header http.Header
	status int
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
