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
