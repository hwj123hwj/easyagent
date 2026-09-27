package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hwj123hwj/easyagent/internal/agents/coding"
	"github.com/hwj123hwj/easyagent/internal/agents/coding/commands"
	kbapp "github.com/hwj123hwj/easyagent/internal/agents/kb"
	musicapp "github.com/hwj123hwj/easyagent/internal/agents/music"
	"github.com/hwj123hwj/easyagent/internal/app"
	"github.com/hwj123hwj/easyagent/internal/mode"
	music "github.com/hwj123hwj/easyagent/internal/music"
	"github.com/hwj123hwj/easyagent/internal/music/bilibili"
	"github.com/hwj123hwj/easyagent/internal/music/netease"
	userprofile "github.com/hwj123hwj/easyagent/internal/profile"
	"github.com/hwj123hwj/easyagent/internal/scheduler"
	"github.com/hwj123hwj/easyagent/internal/tui"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// version is the build version, injected via -ldflags during release builds.
var version = "dev"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Printf("easyagent %s\n", version)
		return
	}
	cfg := config.Default()

	// Version flag (injectable via -ldflags "-X main.version=...")
	versionFlag := flag.Bool("version", false, "Print version and exit")

	// Config file flag (YAML, loaded before .env and env vars)
	configFile := flag.String("config", "", "Path to YAML config file (e.g. easyagent.yaml)")

	// Load .env files (ignore if missing). EA_ENV_FILE allows a custom path;
	// otherwise use the CWD and home files.
	envFile := config.Env("EA_ENV_FILE")
	if envFile != "" {
		_ = config.LoadDotEnv(envFile)
		_ = config.LoadDotEnv(envFile + ".local")
	} else {
		_ = config.LoadDotEnv(".env")
		_ = config.LoadDotEnv(".env.local")
		_ = config.LoadDotEnv(filepath.Join(config.HomeDir(), ".env"))
		_ = config.LoadDotEnv(filepath.Join(config.HomeDir(), ".env.local"))
	}

	// Load YAML config first (lowest priority), then env vars override
	if *configFile == "" {
		candidates := []string{"easyagent.yaml", filepath.Join(config.HomeDir(), "config.yaml")}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				*configFile = candidate
				break
			}
		}
	}
	if *configFile != "" {
		if err := cfg.LoadFromYAML(*configFile); err != nil {
			slog.Warn("failed to load config file", "path", *configFile, "error", err)
		} else {
			slog.Info("loaded config file", "path", *configFile)
		}
	}

	cfg.LoadFromEnv()

	// Handle --version
	if *versionFlag {
		fmt.Printf("easyagent %s\n", version)
		return
	}

	// No subcommand starts the interactive TUI. Prompt flags preserve the
	// one-shot behavior for `easyagent -p ...` and `easyagent --prompt ...`.
	args := os.Args[1:]
	modeFlag := flag.String("mode", inferredDefaultMode(args), "run, chat, interactive, or serve (default: chat; prompt flags select run)")
	listen := flag.String("listen", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port), "HTTP listen address")
	input := "hello"
	flag.StringVar(&input, "prompt", input, "prompt for run mode")
	flag.StringVar(&input, "p", input, "short form of -prompt")
	sessionFlag := flag.String("session", "", "session ID (empty = new session)")
	skillDir := flag.String("skill-dir", "", "directory containing skills (SKILL.md files)")
	legacyTUI := flag.Bool("legacy", false, "Use legacy linear CLI instead of Bubble Tea TUI")
	flag.BoolVar(&cfg.AllowOutsideWorkspace, "allow-outside-workspace", cfg.AllowOutsideWorkspace, "Allow file tools outside workspace (independent of -y; OS permissions still apply)")
	yolo := flag.Bool("y", false, "全权模式：初始跳过危险工具确认（会话内 /confirm on|off 随时切换）")
	if mode := modeForSubcommand(args); mode != "" {
		os.Args = append([]string{os.Args[0], "--mode", mode}, args[1:]...)
	}
	flag.Parse()

	// Sync the actual listen port back to config so MusicApplication
	// generates correct audio proxy URLs (the desktop app uses random ports).
	if _, portStr, err := net.SplitHostPort(*listen); err == nil {
		if p, err := strconv.Atoi(portStr); err == nil {
			cfg.Port = p
		}
	}
	if host, _, err := net.SplitHostPort(*listen); err == nil && host != "" {
		cfg.Host = host
	}

	// -y 全权模式：等价 auto_approve: true，仅决定初始状态（/confirm 可切）
	if *yolo {
		cfg.AutoApprove = true
	}

	// Set log level based on mode
	switch *modeFlag {
	case "interactive", "chat":
		// Suppress INFO logs in TUI mode for cleaner output
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))
	default:
		// Keep default INFO level for other modes
	}

	// Propagate version to mode package
	mode.SetVersion(version)

	// Create music dependencies
	neClient := netease.NewClient()
	biliClient := bilibili.NewClient()
	musicCache := music.NewCache()
	musicRouter := music.NewSourceRouter(music.SourceNetease,
		music.NewNetEaseAdapter(neClient),
		music.NewBilibiliAdapter(biliClient),
	)

	// Create unified user profile (shared across agents — but each agent only
	// sees categories relevant to its domain via SummaryForCategories)
	profilePath := filepath.Join(cfg.DataDir, "user_profile.json")
	userProfile := userprofile.NewStore(profilePath)

	// Resolve KB repo path from config (default: ~/agent-lessons)
	kbRepoPath := cfg.KBRepoPath
	if kbRepoPath == "" {
		homeDir, _ := os.UserHomeDir()
		kbRepoPath = homeDir + "/agent-lessons"
	}

	// Create App (thin assembly layer) with coding, music, and kb applications.
	// Coding agent does NOT receive the profile (it relies on .llm-wiki + project context).
	// Music agent receives music + general categories only.
	// KB agent receives the full profile (it's the second brain).
	application, err := app.New(app.AppOptions{
		Config:      cfg,
		SkillDirs:   skillDirs(*skillDir),
		Application: coding.NewCodingApplication(cfg),
		Profile:     userProfile,
		Applications: map[string]runtime.Application{
			"coding": coding.NewCodingApplication(cfg),
			"music":  musicapp.NewMusicApplication(cfg, musicRouter, musicCache, userProfile),
			"kb":     kbapp.NewKBApplicationWithProfile(cfg, kbRepoPath, userProfile),
		},
	})
	if err != nil {
		slog.Error("failed to create app", "error", err)
		os.Exit(1)
	}
	defer application.Close()

	switch *modeFlag {
	case "interactive", "chat":
		sess, err := application.LoadOrCreateSession(context.Background(), *sessionFlag)
		must(err)
		cmds := buildSlashRegistry(application.LoopManager())
		if *legacyTUI {
			must(mode.NewInteractiveMode(sess, cmds, application).Run(context.Background()))
		} else {
			must(tui.Run(sess, cmds, application, cfg.AutoApprove))
		}

	case "run":
		sess, err := application.LoadOrCreateSession(context.Background(), *sessionFlag)
		must(err)
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()
		must(mode.NewPrintMode(sess).Run(ctx, input))

	case "serve":
		cmds := buildSlashRegistry(application.LoopManager())
		// Music audio proxy routes
		musicHandler := music.NewHandler(musicRouter, musicCache)
		extraMux := http.NewServeMux()
		musicHandler.RegisterRoutes(extraMux)
		srv := mode.NewServeMode(application, cmds)
		srv.SetExtraRoutes(extraMux)
		slog.Info("starting easyagent server", "listen", *listen)
		if err := srv.Run(*listen); err != nil {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *modeFlag)
		os.Exit(2)
	}
}

func inferredDefaultMode(args []string) string {
	if mode := modeForSubcommand(args); mode != "" {
		return mode
	}
	for _, arg := range args {
		switch {
		case arg == "-p", arg == "-prompt", arg == "--prompt",
			strings.HasPrefix(arg, "-p="), strings.HasPrefix(arg, "-prompt="), strings.HasPrefix(arg, "--prompt="):
			return "run"
		}
	}
	return "chat"
}

func modeForSubcommand(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "chat", "interactive":
		return "chat"
	case "serve", "server":
		return "serve"
	case "run":
		return "run"
	default:
		return ""
	}
}

// buildSlashRegistry creates the slash command registry with built-in commands.
func buildSlashRegistry(loopMgr *scheduler.LoopManager) *slashcmd.Registry {
	registry := slashcmd.NewRegistry()
	coding.RegisterCommands(registry)
	if loopMgr != nil {
		commands.RegisterLoopCommands(registry, loopMgr)
	}
	commands.RegisterTaskCommands(registry)
	commands.RegisterUndoCommands(registry)
	commands.RegisterFeishuCommands(registry)
	return registry
}

// skillDirs returns the skill directory list from a flag value.
func skillDirs(flag string) []string {
	if flag != "" {
		return []string{flag}
	}
	return nil
}

// must panics on error.
func must(err error) {
	if err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}
