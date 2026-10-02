package mode

import (
	"log/slog"
	"net/http"

	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/internal/server"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// ServeMode handles the HTTP server mode.
// It uses App to support multi-session routing.
type ServeMode struct {
	app       *app.App
	slashCmds *slashcmd.Registry
	extraMux  *http.ServeMux // optional extra routes (e.g. music audio proxy)
}

// NewServeMode creates a new serve mode.
func NewServeMode(application *app.App, slashCmds *slashcmd.Registry) *ServeMode {
	return &ServeMode{
		app:       application,
		slashCmds: slashCmds,
	}
}

// SetExtraRoutes sets an additional ServeMux to be merged into the server's routes.
// This is used by music-agent to register audio proxy endpoints without modifying server.go.
func (m *ServeMode) SetExtraRoutes(mux *http.ServeMux) {
	m.extraMux = mux
}

// Run starts the HTTP server.
func (m *ServeMode) Run(listenAddr string) error {
	srv := server.New(m.app, m.slashCmds)
	srv.SetVersion(Version)
	if m.extraMux != nil {
		srv.SetExtraRoutes(m.extraMux)
	}
	// Enable API key auth if configured
	if m.app.Config().APIKey != "" {
		srv.SetAPIKey(m.app.Config().APIKey)
		slog.Info("auth: API key enabled")
	} else if config.Env("EA_ALLOW_NO_AUTH") == "1" {
		slog.Warn("auth: open access mode (EA_ALLOW_NO_AUTH=1) — all requests allowed; never expose this port")
	} else {
		slog.Info("auth: default posture — loopback requests allowed, remote requests require EA_API_KEY")
	}
	return srv.ListenAndServe(listenAddr)
}

// Version is set by main via SetVersion.
var Version = "dev"

// SetVersion sets the build version.
func SetVersion(v string) {
	Version = v
}
