package shopifysso

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Track prepared responses and deferred alerts only for Shopify transactions.
type shopifyAuthTransaction struct {
	core.App
	original         core.App
	preparedResponse *authResponseBuffer
}

// PocketBase reuses its active transaction for nested calls. Keep this request's
// wrapper too, so response tracking and deferred alerts survive project hooks.
func (tx *shopifyAuthTransaction) RunInTransaction(fn func(core.App) error) error {
	return tx.App.RunInTransaction(func(core.App) error { return fn(tx) })
}

// Prepare native JSON before commit, retaining it separately so a project hook
// can still reject the request or replace its response after calling Next.
func (tx *shopifyAuthTransaction) OnRecordAuthRequest(tags ...string) *hook.TaggedHook[*core.RecordAuthRequestEvent] {
	local := &hook.Hook[*core.RecordAuthRequestEvent]{}
	local.BindFunc(func(e *core.RecordAuthRequestEvent) error {
		finalizer := e.Event
		return tx.App.OnRecordAuthRequest(tags...).Trigger(e, func(e *core.RecordAuthRequestEvent) error {
			if e.Written() {
				return finalizer.Next()
			}
			originalApp, originalResponse := e.App, e.Response
			prepared := &authResponseBuffer{header: originalResponse.Header().Clone()}
			e.App = &authResponseApp{App: originalApp, transaction: tx}
			e.Response = prepared
			defer func() { e.App, e.Response = originalApp, originalResponse }()
			err := finalizer.Next()
			if (err == nil || err == apis.ErrMFA) && prepared.Written() {
				// Keep the MFA challenge pending too. A surrounding hook can
				// replace it with its final policy response after calling Next.
				tx.preparedResponse = prepared
			}
			return err
		})
	})
	return hook.NewTaggedHook(local, tags...)
}

// Only the native response finalizer sees TxInfo=nil, which makes PocketBase
// serialize immediately. Promoted DB methods still use the actual transaction.
// Project enrich hooks receive the original app with its real transaction info.
type authResponseApp struct {
	core.App
	transaction *shopifyAuthTransaction
}

func (a *authResponseApp) TxInfo() *core.TxAppInfo { return nil }

func (a *authResponseApp) OnRecordEnrich(tags ...string) *hook.TaggedHook[*core.RecordEnrichEvent] {
	local := &hook.Hook[*core.RecordEnrichEvent]{}
	local.BindFunc(func(e *core.RecordEnrichEvent) error {
		finalizer := e.Event
		original := e.App
		e.App = a.App
		defer func() { e.App = original }()
		return a.App.OnRecordEnrich(tags...).Trigger(e, func(e *core.RecordEnrichEvent) error {
			return finalizer.Next()
		})
	})
	return hook.NewTaggedHook(local, tags...)
}

// Keep discovered endpoints in memory, so discovery never needs the DB writer.
// Preserve PocketBase's state, PKCE, nonce and all other native response fields.
func (s *Service) nativeAuthMethodsMiddleware(e *core.RequestEvent) error {
	if e.Request.Method != http.MethodGet || !strings.HasPrefix(e.Request.URL.Path, "/api/collections/") || !strings.HasSuffix(e.Request.URL.Path, "/auth-methods") {
		return e.Next()
	}
	// The native response contains a fresh PKCE verifier, not public metadata.
	noStore(e)
	return s.shopifyAuthMethods(e)
}

func (s *Service) shopifyAuthMethods(e *core.RequestEvent) error {
	original := e.Response
	originalRequest := e.Request
	if query := e.Request.URL.Query(); query.Has("fields") {
		// Keep provider names and URLs until filtering and rewriting finish.
		// The final JSON response applies PocketBase's native field picker.
		e.Request = e.Request.Clone(e.Request.Context())
		query.Del("fields")
		e.Request.URL.RawQuery = query.Encode()
	}
	captured := &authResponseBuffer{header: original.Header().Clone()}
	e.Response = captured
	defer func() { e.Response, e.Request = original, originalRequest }()
	err := e.Next()
	e.Response, e.Request = original, originalRequest
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
	ready := s.isReady()
	filter := func(value any) []any {
		providers, _ := value.([]any)
		filtered := make([]any, 0, len(providers))
		for _, item := range providers {
			provider, _ := item.(map[string]any)
			if provider["name"] == providerName {
				if !ready {
					continue
				}
				authURL, _ := provider["authURL"].(string)
				_, query, ok := strings.Cut(authURL, "?")
				if !ok {
					continue // Do not advertise an incomplete native PKCE URL.
				}
				provider["authURL"] = s.oauth.Endpoint.AuthURL + "?" + query
				provider["authUrl"] = provider["authURL"]
			}
			filtered = append(filtered, item)
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
// must persist so that the second factor can use it.
func (s *Service) respondToOAuth(e *core.RequestEvent) error {
	return respondAfterCommit(e, commitRules{acceptMFAChallenge: true}, func(tx core.App) error {
		authTx := &shopifyAuthTransaction{App: tx, original: s.app}
		e.App = authTx
		err := e.Next()
		// Send the native response prepared before commit, unless a project hook
		// has already written its own final response.
		if !e.Written() && authTx.preparedResponse != nil {
			if writeErr := authTx.preparedResponse.sendTo(e.Response); writeErr != nil {
				return writeErr
			}
		}
		return err
	})
}
