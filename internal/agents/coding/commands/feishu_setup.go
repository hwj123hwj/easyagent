package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/hwj123hwj/easyagent/internal/feishu"
	"github.com/hwj123hwj/easyagent/sdk/config"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	"github.com/muesli/cancelreader"
	qrcode "github.com/skip2/go-qrcode"
)

const feishuSetupUsage = "用法：/feishu setup [--qr | --link | --browser]\n扫码或链接可用于没有浏览器的服务器；只有 --browser 会打开本机浏览器。"

type feishuSetupServices struct {
	begin func(context.Context) (*feishu.BeginResult, error)
	poll  func(context.Context, *feishu.BeginResult) (*feishu.PollResult, error)
	probe func(context.Context, string, string, string) (string, string, error)
	save  func(feishu.Credentials) error
	open  func(context.Context, string) error
}

func defaultFeishuSetupServices() feishuSetupServices {
	return feishuSetupServices{
		begin: func(ctx context.Context) (*feishu.BeginResult, error) {
			if err := feishu.InitRegistrationContext(ctx, "feishu"); err != nil {
				return nil, err
			}
			return feishu.BeginRegistrationContext(ctx, "feishu")
		},
		poll: func(ctx context.Context, b *feishu.BeginResult) (*feishu.PollResult, error) {
			return feishu.PollRegistrationContext(ctx, b.DeviceCode, b.Interval, b.ExpireIn, "feishu", nil)
		},
		probe: feishu.ProbeCredentialsContext, save: feishu.SaveCredentials, open: feishu.OpenBrowserContext,
	}
}

func runFeishuSetup(cmd slashcmd.Context, args []string, services feishuSetupServices) (slashcmd.CommandResult, error) {
	if len(args) > 1 || (len(args) == 1 && args[0] != "--qr" && args[0] != "--link" && args[0] != "--browser") {
		return slashcmd.CommandResult{Output: feishuSetupUsage}, nil
	}
	if cmd.Stdout == nil {
		return slashcmd.CommandResult{Output: "请在终端交互模式运行 /feishu setup，或使用桌面设置中的飞书扫码入口。\n" + feishuSetupUsage}, nil
	}
	parent := cmd.Ctx
	if parent == nil {
		parent = context.Background()
	}
	// tea.Exec has restored cooked terminal mode: Ctrl+C is now SIGINT.
	// Scope it to this wizard so cancelling returns to the conversation.
	ctx, stop := signal.NotifyContext(parent, os.Interrupt)
	defer stop()
	cancelled := func() (slashcmd.CommandResult, error) {
		return slashcmd.CommandResult{Output: "飞书配置已取消，未保存新的凭据。"}, nil
	}
	mode := ""
	if len(args) == 1 {
		mode = strings.TrimPrefix(args[0], "--")
	} else {
		var err error
		mode, err = chooseFeishuLogin(ctx, cmd.Stdin, cmd.Stdout)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return cancelled()
		}
		if err != nil {
			return slashcmd.CommandResult{Output: "未开始飞书配置：" + err.Error() + "\n" + feishuSetupUsage}, nil
		}
	}
	if ctx.Err() != nil {
		return cancelled()
	}
	fmt.Fprintln(cmd.Stdout, "\n正在连接飞书…（Ctrl+C 取消）")
	begin, err := services.begin(ctx)
	if ctx.Err() != nil {
		return cancelled()
	}
	if err != nil {
		return slashcmd.CommandResult{}, fmt.Errorf("初始化飞书扫码授权：%w", err)
	}
	registrationCtx, expire := context.WithTimeout(ctx, time.Duration(begin.ExpireIn)*time.Second)
	defer expire()
	fmt.Fprintln(cmd.Stdout, "\n授权链接（可复制到另一台设备）：\n"+begin.QRURL)
	if mode == "qr" {
		if err := renderFeishuQR(cmd.Stdout, begin.QRURL); err != nil {
			return slashcmd.CommandResult{}, err
		}
		fmt.Fprintln(cmd.Stdout, "请用手机飞书扫描上方二维码。")
	}
	if mode == "browser" {
		if err := services.open(registrationCtx, begin.QRURL); err != nil {
			if ctx.Err() != nil {
				return cancelled()
			}
			fmt.Fprintln(cmd.Stdout, "无法打开本机浏览器，请在其他设备打开上方链接。")
		}
	}
	fmt.Fprintln(cmd.Stdout, "等待授权完成…（Ctrl+C 取消）")
	poll, err := services.poll(registrationCtx, begin)
	if ctx.Err() != nil {
		return cancelled()
	}
	if err != nil {
		return slashcmd.CommandResult{}, fmt.Errorf("飞书授权未完成（可重新运行 /feishu setup）：%w", err)
	}
	if poll == nil {
		return slashcmd.CommandResult{}, fmt.Errorf("飞书授权未返回凭据")
	}
	botName, botOpenID, _ := services.probe(ctx, poll.AppID, poll.AppSecret, poll.Domain)
	if ctx.Err() != nil {
		return cancelled()
	}
	if botName == "" {
		botName = "(unknown)"
	}
	if err := services.save(feishu.Credentials{AppID: poll.AppID, AppSecret: poll.AppSecret, UserOpenID: poll.OpenID, BotName: botName, BotOpenID: botOpenID, Platform: poll.Domain}); err != nil {
		return slashcmd.CommandResult{}, fmt.Errorf("保存飞书凭据：%w", err)
	}
	return slashcmd.CommandResult{Output: fmt.Sprintf("飞书配置成功！机器人：%s\nApp ID: %s\n凭据已保存至 %s/feishu-credentials.json\n运行 /feishu start 启动机器人。", botName, poll.AppID, config.HomeDir())}, nil
}

func chooseFeishuLogin(ctx context.Context, in io.Reader, out io.Writer) (string, error) {
	if in == nil {
		return "", io.EOF
	}
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return "", err
	}
	finished := make(chan struct{})
	watchDone := make(chan struct{})
	defer func() {
		close(finished)
		<-watchDone
		reader.Close()
	}()
	go func() {
		defer close(watchDone)
		select {
		case <-ctx.Done():
			reader.Cancel()
		case <-finished:
		}
	}()
	fmt.Fprintln(out, "\n飞书配置 · 选择授权方式\n  1. 手机扫码（默认，适用于服务器）\n  2. 在本机浏览器打开\n  3. 只显示链接（在其他设备打开）\n  0. 取消\nCtrl+C 随时取消；不会自动打开浏览器。")
	lines := bufio.NewReader(reader)
	for {
		fmt.Fprint(out, "请选择 [1/2/3/0，默认 1]：")
		line, err := lines.ReadString('\n')
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			return "", err
		}
		switch strings.TrimSpace(line) {
		case "", "1":
			return "qr", nil
		case "2":
			return "browser", nil
		case "3":
			return "link", nil
		case "0":
			return "", context.Canceled
		default:
			fmt.Fprintln(out, "请输入 1、2、3 或 0。")
		}
	}
}

// Pair rows into half-blocks and fix black-on-white colors independently of
// the terminal theme. Bitmap includes the QR quiet zone; never wrap its rows.
func renderFeishuQR(out io.Writer, link string) error {
	code, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("生成飞书二维码：%w", err)
	}
	bits := code.Bitmap()
	fmt.Fprintln(out)
	for y := 0; y < len(bits); y += 2 {
		fmt.Fprint(out, "\x1b[30;47m")
		for x := range bits[y] {
			top, bottom := bits[y][x], y+1 < len(bits) && bits[y+1][x]
			glyph := ' '
			switch {
			case top && bottom:
				glyph = '█'
			case top:
				glyph = '▀'
			case bottom:
				glyph = '▄'
			}
			fmt.Fprint(out, string(glyph))
		}
		fmt.Fprint(out, "\x1b[0m\n")
	}
	return nil
}
