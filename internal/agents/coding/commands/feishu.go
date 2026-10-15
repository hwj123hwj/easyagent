package commands

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/internal/feishu"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// feishuGatewayMgr is the package-level feishu gateway manager.
var feishuGatewayMgr = feishu.NewGatewayManager()

// RegisterFeishuCommands registers /feishu and /lark slash commands.
func RegisterFeishuCommands(registry *slashcmd.Registry) {
	registerFeishuCommand(registry, "feishu")
}

func registerFeishuCommand(registry *slashcmd.Registry, name string) {
	registry.Register(slashcmd.Command{
		Name:        name,
		Description: "Configure and control the Feishu bot",
		Interactive: func(args string) bool {
			fields := strings.Fields(args)
			return len(fields) == 0 || (fields[0] == "setup" && (len(fields) < 2 || fields[1] != "--manual"))
		},
		Subcommands: []slashcmd.Subcommand{
			{Name: "setup", Description: "Choose QR, browser or link login (or --manual <AppId> <AppSecret>)"},
			{Name: "start", Description: "Start the Feishu bot and send its startup guide"},
			{Name: "stop", Description: "Stop the bot"},
			{Name: "status", Description: "Show current status"},
			{Name: "logout", Description: "Clear credentials and disconnect"},
		},
		Handler: func(ctx slashcmd.Context, args string) (slashcmd.CommandResult, error) {
			args = strings.TrimSpace(args)
			subCmd := strings.Fields(args)

			switch {
			case len(subCmd) == 0:
				return handleFeishuSetup(ctx, nil)

			case subCmd[0] == "setup":
				return handleFeishuSetup(ctx, subCmd[1:])

			case subCmd[0] == "start":
				return handleFeishuStart()

			case subCmd[0] == "stop":
				return handleFeishuStop()

			case subCmd[0] == "status":
				return handleFeishuStatus()

			case subCmd[0] == "logout":
				return handleFeishuLogout()

			default:
				return slashcmd.CommandResult{Output: feishuHelp()}, nil
			}
		},
	})
}

func handleFeishuSetup(ctx slashcmd.Context, args []string) (slashcmd.CommandResult, error) {
	// Mode: manual (--manual <appId> <appSecret>)
	if len(args) >= 1 && args[0] == "--manual" {
		if len(args) < 3 {
			return slashcmd.CommandResult{Output: "Usage: /feishu setup --manual <AppId> <AppSecret>"}, nil
		}
		appID := args[1]
		appSecret := args[2]

		// Probe to verify + get bot info
		botName, botOpenID, probeErr := feishu.ProbeCredentialsContext(ctx.Ctx, appID, appSecret, "feishu")
		if probeErr != nil {
			return slashcmd.CommandResult{Output: fmt.Sprintf("⚠️ Credentials validation failed: %v\nCredentials saved anyway.", probeErr)}, nil
		}

		creds := feishu.Credentials{
			AppID:     appID,
			AppSecret: appSecret,
			BotName:   botName,
			BotOpenID: botOpenID,
			Platform:  "feishu",
		}
		if err := feishu.SaveCredentials(creds); err != nil {
			return slashcmd.CommandResult{}, fmt.Errorf("save credentials: %w", err)
		}

		return slashcmd.CommandResult{
			Output: fmt.Sprintf("✅ Feishu credentials saved!\n  App ID: %s\n  Bot: %s\n  Saved to: %s/feishu-credentials.json\n\nNext: run /feishu start to start the bot", appID, botName, config.HomeDir()),
		}, nil
	}

	return runFeishuSetup(ctx, args, defaultFeishuSetupServices())
}

func handleFeishuStart() (slashcmd.CommandResult, error) {
	creds, err := configuredFeishuCredentials()
	if err != nil {
		return slashcmd.CommandResult{Output: fmt.Sprintf("❌ Could not load Feishu credentials: %v", err)}, nil
	}
	if creds == nil || creds.AppID == "" || creds.AppSecret == "" {
		return slashcmd.CommandResult{
			Output: `⚠️ Feishu credentials not found.

Please configure first:
  /feishu setup              # Scan QR to auto-login
  or
  /feishu setup --manual <AppId> <AppSecret>`,
		}, nil
	}

	if systemdBridgeUnitInstalled() {
		if state, err := feishuBridgeServiceState(); err == nil && state == "active" {
			return slashcmd.CommandResult{Output: "✅ 飞书机器人已经连接中。"}, nil
		}
		if output, err := exec.Command("systemctl", "start", feishuBridgeServiceName).CombinedOutput(); err != nil {
			return slashcmd.CommandResult{Output: fmt.Sprintf("❌ 启动飞书桥接服务失败：%s", strings.TrimSpace(string(output)))}, nil
		}
		state, err := feishuBridgeServiceState()
		if err != nil || state != "active" {
			return slashcmd.CommandResult{Output: "⚠️ 飞书桥接服务没有启动。请确认凭据已配置，然后查看 `systemctl status easyagent-bridge`。"}, nil
		}
		return feishuStartedMessage(*creds), nil
	}

	if feishuGatewayMgr.IsRunning() {
		return slashcmd.CommandResult{Output: "✅ 飞书机器人已经连接中。"}, nil
	}
	client := feishu.NewClient(creds.AppID, creds.AppSecret)
	handler := feishu.NewHandler(piAgentURL(), creds.AppID, client, config.Env("EA_WORKSPACE"))
	if err := feishuGatewayMgr.StartWithHandler(*creds, client, handler); err != nil {
		return slashcmd.CommandResult{Output: fmt.Sprintf("❌ 启动飞书机器人失败：%v", err)}, nil
	}
	return feishuStartedMessage(*creds), nil
}

func feishuStartedMessage(creds feishu.Credentials) slashcmd.CommandResult {
	message := fmt.Sprintf("✅ 飞书机器人已启动（App ID: %s）。长连接就绪后会向完成扫码的账号发送欢迎语和权限提示。", creds.AppID)
	if creds.UserOpenID == "" && strings.TrimSpace(os.Getenv("FEISHU_OWNER_OPEN_ID")) == "" {
		message += "\n⚠️ 当前没有配置接收欢迎语的用户：手动配置凭据时请设置 `FEISHU_OWNER_OPEN_ID`。"
	}
	return slashcmd.CommandResult{Output: message}
}

func configuredFeishuCredentials() (*feishu.Credentials, error) {
	creds, err := feishu.LoadCredentials()
	if err != nil {
		return nil, err
	}
	if creds == nil {
		creds = &feishu.Credentials{Platform: "feishu"}
	}
	envAppID := strings.TrimSpace(os.Getenv("FEISHU_APP_ID"))
	if envAppID != "" {
		if creds.AppID != "" && creds.AppID != envAppID {
			creds.UserOpenID = ""
		}
		creds.AppID = envAppID
	}
	if appSecret := strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET")); appSecret != "" {
		creds.AppSecret = appSecret
	}
	if ownerOpenID := strings.TrimSpace(os.Getenv("FEISHU_OWNER_OPEN_ID")); ownerOpenID != "" {
		creds.UserOpenID = ownerOpenID
	}
	return creds, nil
}

func piAgentURL() string {
	if url := strings.TrimRight(strings.TrimSpace(os.Getenv("PI_AGENT_URL")), "/"); url != "" {
		return url
	}
	host := strings.TrimSpace(config.Env("EA_HOST"))
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	port := strings.TrimSpace(config.Env("EA_PORT"))
	if port == "" {
		port = "8080"
	}
	return "http://" + net.JoinHostPort(host, port)
}

const feishuBridgeServiceName = "easyagent-bridge.service"

func systemdBridgeUnitInstalled() bool {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	output, err := exec.Command("systemctl", "show", feishuBridgeServiceName, "--property=LoadState", "--value").Output()
	return err == nil && strings.TrimSpace(string(output)) == "loaded"
}

func feishuBridgeServiceState() (string, error) {
	output, err := exec.Command("systemctl", "show", feishuBridgeServiceName, "--property=ActiveState", "--value").Output()
	return strings.TrimSpace(string(output)), err
}

func stopSystemdFeishuBridgeAfterReply() {
	go func() {
		// A /feishu stop command may itself arrive through this bridge. Give it
		// time to send the command result back to Feishu before stopping the WS.
		time.Sleep(3 * time.Second)
		if err := exec.Command("systemctl", "stop", feishuBridgeServiceName).Run(); err != nil {
			slog.Warn("failed to stop Feishu bridge service", "error", err)
		}
	}()
}

func handleFeishuStop() (slashcmd.CommandResult, error) {
	if systemdBridgeUnitInstalled() {
		state, err := feishuBridgeServiceState()
		if err == nil && (state == "active" || state == "activating") {
			stopSystemdFeishuBridgeAfterReply()
			return slashcmd.CommandResult{Output: "✅ 正在停止飞书机器人。"}, nil
		}
	}
	if !feishuGatewayMgr.IsRunning() {
		return slashcmd.CommandResult{Output: "⚠️ Feishu Bot is not running."}, nil
	}

	feishuGatewayMgr.Stop()
	return slashcmd.CommandResult{Output: "✅ Feishu Bot stopped."}, nil
}

func handleFeishuStatus() (slashcmd.CommandResult, error) {
	creds, _ := configuredFeishuCredentials()
	if creds == nil || creds.AppID == "" || creds.AppSecret == "" {
		return slashcmd.CommandResult{Output: "📊 Feishu Status: Not configured\n\nRun /feishu setup to get started."}, nil
	}

	status := "stopped"
	if systemdBridgeUnitInstalled() {
		if state, err := feishuBridgeServiceState(); err == nil && (state == "active" || state == "activating") {
			status = "running"
		}
	} else if feishuGatewayMgr.IsRunning() {
		status = "running"
	}

	botName := creds.BotName
	if botName == "" {
		botName = "(unknown)"
	}

	return slashcmd.CommandResult{
		Output: fmt.Sprintf(`📊 Feishu Bot Status

  Status:    %s
  App ID:    %s
  Bot Name:  %s
  Platform:  %s
  Credentials: %s/feishu-credentials.json`, status, creds.AppID, botName, creds.Platform, config.HomeDir()),
	}, nil
}

func handleFeishuLogout() (slashcmd.CommandResult, error) {
	if systemdBridgeUnitInstalled() {
		if state, err := feishuBridgeServiceState(); err == nil && state == "active" {
			stopSystemdFeishuBridgeAfterReply()
		}
	}
	if feishuGatewayMgr.IsRunning() {
		feishuGatewayMgr.Stop()
	}

	if err := feishu.DeleteCredentials(); err != nil {
		return slashcmd.CommandResult{}, fmt.Errorf("delete credentials: %w", err)
	}

	return slashcmd.CommandResult{
		Output: "✅ Feishu credentials cleared. Run /feishu setup to reconfigure.",
	}, nil
}

func feishuHelp() string {
	return strings.TrimSpace(fmt.Sprintf(`
🤖 /feishu — Feishu Bot Integration

Usage:
  /feishu                        Interactive setup (QR scan login)
  /feishu setup                  Choose QR, browser or link login
  /feishu setup --qr             Show a QR code; never open a browser
  /feishu setup --link           Show a link for another device
  /feishu setup --browser        Explicitly open the local browser
  /feishu setup --manual <AppId> <AppSecret>  Manual credentials
  /feishu start                 Start the Feishu connection and send the startup guide
  /feishu stop                  Stop the bot
  /feishu status                Show current status
  /feishu logout                Clear credentials and disconnect

Workflow:
  1. /feishu setup              # Scan QR (you become the owner)
  2. /feishu start              # Start the Feishu connection
  3. Send a message to the bot in Feishu

Credentials are saved to %s/feishu-credentials.json
`, config.HomeDir()))
}
