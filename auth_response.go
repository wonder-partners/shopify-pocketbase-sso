package shopifysso

import (
	"bytes"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// Buffer the native OAuth response until the complete hook chain succeeds.
// A hook rejecting after e.Next() must not leak a previously written JWT.
// This does not create cookies, mint tokens or replace PocketBase's response.
func captureAuthResponse(e *core.RequestEvent, action func() error) error {
	original := e.Response
	captured := &authResponseBuffer{header: original.Header().Clone()}
	e.Response = captured
	err := action()
	e.Response = original
	if err != nil && (!captured.Written() || captured.status < http.StatusBadRequest) {
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
	if err != nil {
		return err
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
