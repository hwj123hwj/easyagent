//go:build darwin

package main

import (
	"os"
	"testing"
)

func TestNativeSystemKeychainRoundTrip(t *testing.T) {
	if os.Getenv("EASYAGENT_NATIVE_KEYCHAIN_TEST") != "1" {
		t.Skip("opt-in synthetic keychain check")
	}
	store := systemSecrets{}
	account := "integration/" + requestID()
	defer store.Delete(account)
	if err := store.Set(account, "synthetic-native-test"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(account)
	if err != nil || value != "synthetic-native-test" {
		t.Fatalf("keychain roundtrip failed: %v", err)
	}
	if err = store.Set(account, "synthetic-updated"); err != nil {
		t.Fatal(err)
	}
	value, err = store.Get(account)
	if err != nil || value != "synthetic-updated" {
		t.Fatalf("keychain update failed: %v", err)
	}
	if err = store.Delete(account); err != nil {
		t.Fatal(err)
	}
	value, err = store.Get(account)
	if err != nil || value != "" {
		t.Fatalf("keychain delete failed: %v", err)
	}
}
