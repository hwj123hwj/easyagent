package feishu

import (
	"os"
	"testing"
)

func TestCredentialsPrivateReplacement(t *testing.T) {
	t.Setenv("EA_HOME", t.TempDir())
	if err := os.WriteFile(credentialsPath(), []byte(`{"app_id":"old"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(Credentials{AppID: "new", AppSecret: "qa-only", UserOpenID: "owner"}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(credentialsPath())
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("credential permissions", st, err)
	}
	c, err := LoadCredentials()
	if err != nil || c.AppID != "new" || c.UserOpenID != "owner" {
		t.Fatal(c, err)
	}
}
