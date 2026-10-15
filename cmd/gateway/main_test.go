package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGatewayPublishesOnlySelectedCollectionAPIs(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	handler := gatewayHandler(target, []string{"users", "documents", "_superusers"})
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/studio", 204}, {"/studio/session", 204}, {"/studio/client.js", 204},
		{"/api/collections/users/records", 204}, {"/api/collections/users/auth-refresh", 204},
		{"/api/collections/documents/records/123", 204}, {"/api/realtime", 204},
		{"/_/", 404}, {"/api/settings", 404}, {"/api/collections", 404},
		{"/api/collections/users", 404}, {"/api/collections/unknown/records", 404},
		{"/api/collections/_superusers/auth-with-password", 404},
		{"/studio/../_/", 404}, {"/api/collections/users/../../settings", 404},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest("GET", tc.path, nil))
		if rr.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.path, rr.Code, tc.want)
		}
	}
}
