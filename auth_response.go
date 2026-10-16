package shopifysso

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// PocketBase may retain Shopify's provider configuration across restarts.
// Filter only Shopify while discovery is pending; all native methods and
// response fields remain available, including the legacy providers list.
func hideShopifyAuthMethod(e *core.RequestEvent) error {
	original := e.Response
	captured := &authResponseBuffer{header: original.Header().Clone()}
	e.Response = captured
	defer func() { e.Response = original }()
	err := e.Next()
	e.Response = original
	if err != nil {
		return err
	}
	if !captured.Written() {
		return e.InternalServerError("PocketBase auth methods response was not completed.", nil)
	}
	if captured.status != http.StatusOK {
		return captured.sendTo(original)
	}
	var result map[string]any
	if json.Unmarshal(captured.Bytes(), &result) != nil || result == nil {
		return e.InternalServerError("Unable to read the authentication methods response.", nil)
	}
	filter := func(value any) []any {
		providers, _ := value.([]any)
		filtered := make([]any, 0, len(providers))
		for _, item := range providers {
			provider, _ := item.(map[string]any)
			if provider["name"] != providerName {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}
	if oauth, ok := result["oauth2"].(map[string]any); ok {
		oauth["providers"] = filter(oauth["providers"])
	}
	if providers, ok := result["authProviders"]; ok {
		result["authProviders"] = filter(providers)
	}
	for key, values := range captured.header {
		original.Header()[key] = values
	}
	original.Header().Del("Content-Length")
	return e.JSON(http.StatusOK, result)
}

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
		e.App = &shopifyAuthTransaction{App: tx, original: originalApp}
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
	writeErr := captured.sendTo(original)
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
