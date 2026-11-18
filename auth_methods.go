package shopifysso

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

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
	ready := s.store.ready()
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
				provider["authURL"] = s.store.authURL() + "?" + query
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
