package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hwj123hwj/easyagent/internal/feishu"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/joho/godotenv"
)

func main() {
	// Load the configured or standard .env file (ignore missing files).
	envFile := config.Env("EA_ENV_FILE")
	if envFile != "" {
		_ = godotenv.Load(envFile)
	} else {
		_ = godotenv.Load(".env", filepath.Join(config.HomeDir(), ".env"))
	}

	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")
	ownerOpenID := os.Getenv("FEISHU_OWNER_OPEN_ID")

	// Credentials saved by /feishu setup also contain the user who registered
	// the app. Use that account for the startup DM unless explicitly overridden.
	storedCredentials, _ := feishu.LoadCredentials()
	if storedCredentials != nil {
		if appID == "" {
			appID = storedCredentials.AppID
		}
		if appSecret == "" {
			appSecret = storedCredentials.AppSecret
		}
		if ownerOpenID == "" && storedCredentials.AppID == appID {
			ownerOpenID = storedCredentials.UserOpenID
		}
		if appID == storedCredentials.AppID {
			slog.Info("loaded feishu credentials from file", "app_id", appID)
		}
	}

	piAgentURL := os.Getenv("PI_AGENT_URL")
	workspace := config.Env("EA_WORKSPACE")
	callbackURL := os.Getenv("BRIDGE_CALLBACK_URL")
	callbackAddr := os.Getenv("BRIDGE_CALLBACK_ADDR")

	if appID == "" || appSecret == "" {
		slog.Error("FEISHU_APP_ID and FEISHU_APP_SECRET are required.\n" +
			"Run /feishu setup to configure via QR scan,\n" +
			"or set FEISHU_APP_ID and FEISHU_APP_SECRET env vars.")
		os.Exit(1)
	}
	if piAgentURL == "" {
		piAgentURL = "http://127.0.0.1:8080"
	}
	if callbackAddr == "" {
		callbackAddr = ":9090"
	}

	slog.Info("starting easyagent-bridge",
		"piAgentURL", piAgentURL,
		"workspace", workspace,
		"callbackURL", callbackURL,
	)

	accessPath := os.Getenv("FEISHU_OWNER_STATE_FILE")
	if accessPath == "" {
		accessPath = filepath.Join(config.HomeDir(), "feishu-owner.json")
	}
	access, err := feishu.OpenOwnerAccess(accessPath, ownerOpenID)
	if err != nil {
		slog.Error("owner access initialization failed")
		os.Exit(1)
	}

	// Create components
	client := feishu.NewClient(appID, appSecret)
	handler := feishu.NewHandler(piAgentURL, appID, client, workspace)
	handler.SetOwnerAccess(access)

	// Wrap handler for gateway
	msgHandler := func(ctx context.Context, msg feishu.Message) {
		handler.Handle(ctx, msg)
	}

	gateway := feishu.NewGateway(appID, appSecret, client, msgHandler)
	handler.SetGateway(gateway)
	gateway.SetOwnerAccess(access)
	gateway.SetCardActionHandler(handler.HandleCardAction)

	// Start tool callback HTTP server
	if callbackURL != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/tool-callback", handler.HandleToolCallback)
		go func() {
			slog.Info("starting tool callback server", "addr", callbackAddr)
			if err := http.ListenAndServe(callbackAddr, mux); err != nil && err != http.ErrServerClosed {
				slog.Error("tool callback server stopped", "error", err)
			}
		}()

		// Register tool with easyagent
		fullCallbackURL := callbackURL + "/tool-callback"
		if err := feishu.RegisterTool(piAgentURL, fullCallbackURL); err != nil {
			slog.Warn("failed to register tool (will work without agent tool)", "error", err)
		}
	}

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Start gateway (blocks until ctx cancelled)
	if err := gateway.Start(ctx); err != nil {
		slog.Error("gateway stopped", "error", err)
	}

	slog.Info("easyagent-bridge exited")
}
