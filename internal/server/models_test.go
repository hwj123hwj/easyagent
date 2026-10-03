package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func modelTestApp(t *testing.T, configure func(*config.Config)) *app.App {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Provider = "openai"
	cfg.OpenAIAPIKey = "test-key"
	configure(&cfg)
	application, err := app.New(app.AppOptions{Config: cfg})
	require.NoError(t, err)
	t.Cleanup(func() { application.Close() })
	return application
}

func TestModelCatalogUsesGatewayAndAcceptsVersionedURL(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"real-model","display_name":"Real Model"},{"id":""}]}`))
	}))
	defer gateway.Close()
	application := modelTestApp(t, func(cfg *config.Config) {
		cfg.OpenAIBaseURL = gateway.URL + "/v1/"
		cfg.OpenAIModel = "real-model"
	})
	srv := New(application, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/models", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var catalog ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))
	assert.Equal(t, "gateway", catalog.Source)
	require.Len(t, catalog.Models, 1)
	assert.Equal(t, "real-model", catalog.Models[0].ID)
	assert.Equal(t, "Real Model", catalog.Models[0].Name)
	assert.Equal(t, "real-model", catalog.Current.ID)
}

func TestModelCatalogFailureNeverInventsModels(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("sensitive upstream error"))
	}))
	defer gateway.Close()
	application := modelTestApp(t, func(cfg *config.Config) {
		cfg.OpenAIBaseURL = gateway.URL
		cfg.OpenAIModel = "my-configured-model"
	})
	srv := New(application, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/models", nil))
	var catalog ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))
	assert.Equal(t, "configured", catalog.Source)
	require.Len(t, catalog.Models, 1)
	assert.Equal(t, "my-configured-model", catalog.Models[0].ID)
	assert.Equal(t, "model discovery returned HTTP 401", catalog.DiscoveryError)
	assert.NotContains(t, w.Body.String(), "sensitive")
}

func TestModelCatalogUnconfiguredIsEmpty(t *testing.T) {
	application := modelTestApp(t, func(cfg *config.Config) {
		cfg.OpenAIModel = ""
		cfg.OpenAIBaseURL = ""
	})
	srv := New(application, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/models", nil))
	var catalog ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))
	assert.Equal(t, "unconfigured", catalog.Source)
	assert.Empty(t, catalog.Models)
	assert.Nil(t, catalog.Current)
	assert.Contains(t, w.Body.String(), `"models":[]`)
}

func TestAnthropicModelCatalogDoesNotQueryUnrelatedOpenAIConfig(t *testing.T) {
	calls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":[{"id":"unrelated-openai-model"}]}`))
	}))
	defer gateway.Close()
	application := modelTestApp(t, func(cfg *config.Config) {
		cfg.Provider = "anthropic"
		cfg.AnthropicAPIKey = "test-anthropic-key"
		cfg.AnthropicModel = "my-anthropic-model"
		cfg.OpenAIBaseURL = gateway.URL
	})
	calls = 0 // Count discovery by this endpoint, after application initialization.
	srv := New(application, nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, localReq(http.MethodGet, "/models", nil))
	var catalog ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &catalog))
	assert.Equal(t, 0, calls)
	assert.Equal(t, "configured", catalog.Source)
	require.Len(t, catalog.Models, 1)
	assert.Equal(t, "my-anthropic-model", catalog.Models[0].ID)
}

func TestModelDiscoveryDoesNotForwardCredentialsThroughRedirect(t *testing.T) {
	redirected := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected++
		_, _ = w.Write([]byte(`{"data":[{"id":"unexpected"}]}`))
	}))
	defer target.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/models", http.StatusFound)
	}))
	defer gateway.Close()
	models, err := (&Server{}).fetchGatewayModels(context.Background(), gateway.URL, "test-key")
	require.Error(t, err)
	assert.Nil(t, models)
	assert.Equal(t, 0, redirected)
}
