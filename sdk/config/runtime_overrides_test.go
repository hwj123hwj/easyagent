package config

import (
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentRuntimeOverridesKeepAuditAndSettings(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"future_setting":"keep"}`), 0600))
	var wg sync.WaitGroup
	errs := make(chan error, 41)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs <- RecordApprovedApp(dir, fmt.Sprintf("app.%d", i)) }(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		enabled, policy := true, "auto"
		errs <- UpdateComputerRuntimeSettings(dir, &enabled, &policy, false)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	c := Config{}
	c.LoadRuntimeOverrides(dir)
	require.True(t, c.EnableComputerUse)
	require.Equal(t, "auto", c.ComputerApprovalPolicy)
	require.Len(t, c.ApprovedApps, 40)
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Equal(t, "keep", raw["future_setting"])
}
func TestMalformedSettingsAreNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"broken":`)
	require.NoError(t, os.WriteFile(path, original, 0600))
	require.Error(t, SaveRuntimeOverrideComputerUse(dir, true))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data)
}
