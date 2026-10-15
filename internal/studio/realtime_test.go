package studio

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNativeRealtimeUsesTheSameOwnerRules(t *testing.T) {
	for _, method := range []string{"shopify", "password"} {
		t.Run(method, func(t *testing.T) {
			f := setup(t, "", "")
			documentsCollection(f)
			f.login()
			shopToken, shop := f.authRecord()
			native := nativeSignup(f, "realtime-native@example.test")
			nativeID := native.Record["id"].(string)
			token, own, otherToken, other := shopToken, shop.Id, native.Token, nativeID
			if method == "password" {
				token, own, otherToken, other = native.Token, nativeID, shopToken, shop.Id
			}
			ownDoc := createDocument(f, token, own)
			otherDoc := createDocument(f, otherToken, other)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", f.api.URL+"/api/realtime", nil)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			events := make(chan []byte, 5)
			go func() {
				scanner := bufio.NewScanner(res.Body)
				for scanner.Scan() {
					if line := scanner.Text(); strings.HasPrefix(line, "data:") {
						select {
						case events <- []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))):
						case <-ctx.Done():
							return
						}
					}
				}
			}()
			var connect struct {
				ClientID string `json:"clientId"`
			}
			select {
			case data := <-events:
				if json.Unmarshal(data, &connect) != nil || connect.ClientID == "" {
					t.Fatal("missing realtime client ID")
				}
			case <-ctx.Done():
				t.Fatal("realtime connect timed out")
			}
			account := privateAccount(f, shop.Id)
			body := `{"clientId":"` + connect.ClientID + `","subscriptions":["documents/*","_shopifyIdentities/*","_shopifyIdentities/` + account.Id + `"]}`
			f.expect(f.request("POST", f.api.URL+"/api/realtime", "", body, token), 204)
			account.Set("customerId", "gid://shopify/Customer/67890")
			if err := f.app.Save(account); err != nil {
				t.Fatal(err)
			}
			f.expect(f.request("PATCH", f.api.URL+"/api/collections/documents/records/"+otherDoc, "", `{"title":"Other user update"}`, otherToken), 200)
			select {
			case <-events:
				t.Fatal("realtime leaked a private Shopify identity or another user's record")
			case <-time.After(100 * time.Millisecond):
			}
			f.expect(f.request("PATCH", f.api.URL+"/api/collections/documents/records/"+ownDoc, "", `{"title":"Own update"}`, token), 200)
			select {
			case data := <-events:
				var event struct {
					Action string
					Record struct{ Id string }
				}
				if json.Unmarshal(data, &event) != nil || event.Action != "update" || event.Record.Id != ownDoc {
					t.Fatal("unexpected realtime event")
				}
			case <-ctx.Done():
				t.Fatal("own realtime update was not delivered")
			}
		})
	}
}
