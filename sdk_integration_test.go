package shopifysso

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOfficialSDKUsesNativeOAuth2Code(t *testing.T) {
	if os.Getenv("RUN_JS_SDK_TEST") != "1" {
		t.Skip("run mise run test to include the official JS SDK OAuth integration")
	}
	f := setup(t, "numeric:")
	input := map[string]string{"url": f.api.URL, "shopURL": f.shop.URL, "redirectURL": f.s.cfg.RedirectURL}
	data, _ := json.Marshal(input)
	script, _ := filepath.Abs("tests/sdk/oauth.test.mjs")
	cmd := exec.Command("node", script)
	cmd.Stdin = bytes.NewReader(data)
	// Do not print subprocess output: SDK errors can include request auth data.
	if err := cmd.Run(); err != nil {
		t.Fatal("official SDK authWithOAuth2Code integration failed:", err)
	}
	users, err := f.app.FindAllRecords("users")
	if err != nil || len(users) != 1 || privateAccount(f, users[0].Id).GetString("subject") != "900719925474099312345" {
		t.Fatal("official SDK login did not preserve the exact subject in private storage")
	}
}
