package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifySignedRelease(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	config := filepath.Join(dir, "mygo.json")
	b, _ := json.Marshal(map[string]any{"updates": map[string]string{"publicKey": base64.StdEncoding.EncodeToString(pub)}})
	os.WriteFile(config, b, 0600)
	archive := []byte("native bundle fixture")
	sum := sha256.Sum256(archive)
	manifest := map[string]any{"version": "1.2.3", "url": "https://github.com/hwj123hwj/easyagent/releases/download/v1.2.3/easyagent-1.2.3-darwin-arm64.tar.gz", "size": len(archive), "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sum[:]))}
	save := func() {
		b, _ := json.Marshal(manifest)
		os.WriteFile(filepath.Join(dir, "update-darwin-arm64.json"), b, 0600)
		os.WriteFile(filepath.Join(dir, "easyagent-1.2.3-darwin-arm64.tar.gz"), archive, 0600)
	}
	save()
	if err := verify(config, dir, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "url", "signature", "size"} {
		original := manifest[key]
		manifest[key] = "invalid"
		save()
		if verify(config, dir, "1.2.3") == nil {
			t.Fatalf("accepted corrupt %s", key)
		}
		manifest[key] = original
	}
	archive[0] ^= 1
	save()
	if verify(config, dir, "1.2.3") == nil {
		t.Fatal("accepted tampered archive")
	}
}
