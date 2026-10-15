package studio

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficialPocketBaseJavaScriptSDK(t *testing.T) {
	if os.Getenv("RUN_JS_SDK_TEST") != "1" {
		t.Skip("npm ci --prefix integration, then RUN_JS_SDK_TEST=1 go test ./internal/studio")
	}
	f := setup(t, "", "numeric:")
	documentsCollection(f)
	f.loginStudio()
	token, shop := f.session()
	native := nativeSignup(f, "sdk-native@example.test")
	nativeID := native.Record["id"].(string)
	shopDoc := createDocument(f, token, shop.Id)
	nativeDoc := createDocument(f, native.Token, nativeID)
	input, _ := json.Marshal(map[string]any{"url": f.studio.URL, "accounts": []map[string]string{
		{"id": shop.Id, "token": token, "otherDoc": nativeDoc},
		{"id": nativeID, "token": native.Token, "otherDoc": shopDoc},
	}})
	script, _ := filepath.Abs("../../integration/sdk.mjs")
	cmd := exec.Command("node", script)
	cmd.Stdin = bytes.NewReader(input)
	// Do not print error bodies: SDK exceptions can include request auth data.
	if err := cmd.Run(); err != nil {
		t.Fatal("official PocketBase JS SDK integration failed:", err)
	}
}

func TestOfficialSDKUsesNativeOAuth2Code(t *testing.T) {
	if os.Getenv("RUN_JS_SDK_TEST") != "1" {
		t.Skip("set RUN_JS_SDK_TEST=1 to execute the official JS SDK")
	}
	f := setup(t, "", "numeric:")
	f.loginShop()
	page := f.request("GET", f.authorize("/studio/auth"), "", "", "")
	f.expect(page, 200)
	input := callbackData(t, page.body)
	input["url"] = f.studio.URL
	u, _ := url.Parse(f.studio.URL)
	var cookies []string
	for _, c := range f.client.Jar.Cookies(u) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	input["cookie"] = strings.Join(cookies, "; ")
	data, _ := json.Marshal(input)
	script, _ := filepath.Abs("../../integration/oauth.mjs")
	cmd := exec.Command("node", script)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		t.Fatal("official SDK authWithOAuth2Code integration failed:", err)
	}
	users, err := f.app.FindAllRecords("users")
	if err != nil || len(users) != 1 || privateAccount(f, users[0].Id).GetString("subject") != "900719925474099312345" {
		t.Fatal("official SDK login did not preserve the exact subject in private storage")
	}
}
