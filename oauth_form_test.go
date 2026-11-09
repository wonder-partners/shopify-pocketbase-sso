package shopifysso

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestShopifyOAuthFormRequests(t *testing.T) {
	for _, format := range []string{"urlencoded", "multipart"} {
		for _, fault := range []string{"", "origin", "pkce"} {
			t.Run(format+"/"+fault, func(t *testing.T) {
				f := setup(t, "")
				f.loginShop()
				data := callbackData(t, f.callback(f.authorize(false)).body)
				delete(data, "collection")
				if fault == "pkce" {
					data["codeVerifier"] = strings.Repeat("x", 43)
				}
				var body io.Reader
				contentType := "application/x-www-form-urlencoded"
				if format == "multipart" {
					var buffer bytes.Buffer
					writer := multipart.NewWriter(&buffer)
					for key, value := range data {
						if err := writer.WriteField(key, value); err != nil {
							t.Fatal(err)
						}
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					contentType = writer.FormDataContentType()
					body = &buffer
				} else {
					values := url.Values{}
					for key, value := range data {
						values.Set(key, value)
					}
					body = strings.NewReader(values.Encode())
				}
				req, err := http.NewRequest("POST", f.api.URL+"/api/collections/users/auth-with-oauth2", body)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", contentType)
				origin := f.api.URL
				if fault == "origin" {
					origin = "https://untrusted.example"
				}
				req.Header.Set("Origin", origin)
				res, err := f.client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				raw, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				expected := http.StatusOK
				if fault == "origin" {
					expected = http.StatusForbidden
				}
				if fault == "pkce" {
					expected = http.StatusBadGateway
				}
				result := response{status: res.StatusCode, body: string(raw)}
				f.expect(result, expected)
				if fault == "" {
					decodeAuth(t, result)
				} else {
					count, err := f.app.CountRecords("users")
					if err != nil || count != 0 {
						t.Fatal("rejected form created a user", err)
					}
				}
			})
		}
	}
}
