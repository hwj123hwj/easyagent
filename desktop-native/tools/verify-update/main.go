// Verify the exact signed archive staged by the release build before upload.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func verify(config, dir, version string) error {
	var c struct{ Updates struct{ PublicKey string } }
	b, err := os.ReadFile(config)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return err
	}
	pub, err := base64.StdEncoding.DecodeString(c.Updates.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid update public key")
	}
	var m struct {
		Version, URL, Signature string
		Size                    int64
	}
	b, err = os.ReadFile(filepath.Join(dir, "update-darwin-arm64.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	name := "easyagent-" + version + "-darwin-arm64.tar.gz"
	if m.Version != version || m.URL != "https://github.com/hwj123hwj/easyagent/releases/download/v"+version+"/"+name {
		return fmt.Errorf("update manifest version or URL mismatch")
	}
	b, err = os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	if int64(len(b)) != m.Size || m.Size == 0 {
		return fmt.Errorf("update archive size mismatch")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	if !ed25519.Verify(pub, sum[:], sig) {
		return fmt.Errorf("update archive signature mismatch")
	}
	return nil
}
func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: verify-update config directory version")
		os.Exit(2)
	}
	if err := verify(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
