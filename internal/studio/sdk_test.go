package studio

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOfficialPocketBaseJavaScriptSDK(t *testing.T) {
	if os.Getenv("RUN_JS_SDK_TEST") != "1" {
		t.Skip("run mise run test to include the official JS SDK integrations")
	}
	f := setup(t, "numeric:")
	documentsCollection(f)
	f.login()
	token, shop := f.authRecord()
	native := nativeSignup(f, "sdk-native@example.test")
	nativeID := native.Record["id"].(string)
	shopDoc := createDocument(f, token, shop.Id)
	nativeDoc := createDocument(f, native.Token, nativeID)
	input, _ := json.Marshal(map[string]any{"url": f.api.URL, "accounts": []map[string]string{
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
		t.Skip("run mise run test to include the official JS SDK integrations")
	}
	f := setup(t, "numeric:")
	input := map[string]string{"url": f.api.URL, "shopURL": f.shop.URL, "redirectURL": f.s.cfg.RedirectURL}
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
