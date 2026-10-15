package studio

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Buffer only the standard auth response. Do not set an authenticated cookie
// until PocketBase has completed its AuthRule, hooks and MFA checks successfully.
// Browser navigation after OAuth is handled by the SDK, never with a JWT in a URL.
func (s *Service) authResponse(e *core.RequestEvent, record *core.Record, method string) error {
	return s.captureAuthResponse(e, record.Collection(), func() error {
		return apis.RecordAuthResponse(e, record, method, nil)
	})
}

// The same buffer also wraps the actual native OAuth2 endpoint, including its
// specific hooks and record creation. No token escapes if a hook later rejects it.
func (s *Service) captureAuthResponse(e *core.RequestEvent, collection *core.Collection, action func() error) error {
	original := e.Response
	captured := &authResponseBuffer{header: original.Header().Clone()}
	e.Response = captured
	err := action()
	e.Response = original
	if err != nil && (!captured.Written() || captured.status < http.StatusBadRequest) {
		return err
	}
	if captured.status == http.StatusOK && err == nil {
		var result struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(captured.Bytes(), &result) != nil || result.Token == "" {
			return e.InternalServerError("PocketBase did not return an auth token.", nil)
		}
		for key, values := range captured.header {
			original.Header()[key] = values
		}
		s.setCookie(e, "session", result.Token, int(collection.AuthToken.DurationTime().Seconds()))
	} else if !captured.Written() {
		return e.InternalServerError("PocketBase auth response was not completed.", nil)
	}
	for key, values := range captured.header {
		// Keep the session cookie added after success, while preserving hook headers.
		if key != "Set-Cookie" {
			original.Header()[key] = values
		}
	}
	if captured.status != http.StatusOK || err != nil {
		// Native MFA responses and custom hook responses pass through unchanged.
		original.Header()["Set-Cookie"] = captured.header.Values("Set-Cookie")
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
