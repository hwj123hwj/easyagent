package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/hwj123hwj/easyagent/internal/feishu"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
	qrcode "github.com/skip2/go-qrcode"
)

const setupTestURL = "https://open.feishu.cn/page/launcher?user_code=test-only-code&from=pigo&tp=pigo"

func setupTestServices(opened, saved *int) feishuSetupServices {
	return feishuSetupServices{
		begin: func(context.Context) (*feishu.BeginResult, error) {
			return &feishu.BeginResult{DeviceCode: "test-only-device", QRURL: setupTestURL, Interval: 5, ExpireIn: 600}, nil
		},
		poll: func(context.Context, *feishu.BeginResult) (*feishu.PollResult, error) {
			return &feishu.PollResult{AppID: "test-app", AppSecret: "test-only-secret", OpenID: "test-owner", Domain: "feishu"}, nil
		},
		probe: func(context.Context, string, string, string) (string, string, error) {
			return "test-bot", "test-bot-id", nil
		},
		save: func(c feishu.Credentials) error {
			*saved++
			if c.UserOpenID != "test-owner" {
				return errors.New("owner lost")
			}
			return nil
		},
		open: func(context.Context, string) error { *opened++; return nil },
	}
}

func TestFeishuSetupRequiresExplicitBrowserChoice(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "test-server")
	for _, tc := range []struct {
		name, choice, flag string
		open               bool
		qr                 bool
	}{
		{"default_scan", "\n", "", false, true},
		{"scan", "1\n", "", false, true},
		{"browser", "2\n", "", true, false},
		{"other_device", "3\n", "", false, false},
		{"explicit_scan", "", "--qr", false, true},
		{"explicit_link", "", "--link", false, false},
		{"explicit_browser", "", "--browser", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened, saved := 0, 0
			var out bytes.Buffer
			var args []string
			if tc.flag != "" {
				args = []string{tc.flag}
			}
			result, err := runFeishuSetup(slashcmd.Context{Ctx: t.Context(), Stdin: strings.NewReader(tc.choice), Stdout: &out}, args, setupTestServices(&opened, &saved))
			if err != nil || saved != 1 || (opened == 1) != tc.open {
				t.Fatalf("result=%+v err=%v opened=%d saved=%d", result, err, opened, saved)
			}
			if !strings.Contains(out.String(), setupTestURL) {
				t.Fatal("copyable URL missing")
			}
			if strings.Contains(out.String(), "\x1b[30;47m") != tc.qr {
				t.Fatal("wrong QR presentation")
			}
			if strings.Contains(result.Output, "test-only-secret") {
				t.Fatal("secret exposed in transcript")
			}
		})
	}
}

func TestFeishuSetupCancelAndEOFDoNotStartRegistration(t *testing.T) {
	for _, choice := range []string{"0\n", ""} {
		opened, saved := 0, 0
		services := setupTestServices(&opened, &saved)
		services.begin = func(context.Context) (*feishu.BeginResult, error) {
			t.Fatal("registration started without choice")
			return nil, nil
		}
		var out bytes.Buffer
		result, err := runFeishuSetup(slashcmd.Context{Ctx: t.Context(), Stdin: strings.NewReader(choice), Stdout: &out}, nil, services)
		if err != nil || result.Output == "" || opened != 0 || saved != 0 {
			t.Fatal(result, err)
		}
	}
	result, err := runFeishuSetup(slashcmd.Context{Ctx: t.Context()}, nil, feishuSetupServices{})
	if err != nil || !strings.Contains(result.Output, "终端交互模式") {
		t.Fatal("HTTP/background invocation must not launch a wizard", result, err)
	}
}

func TestFeishuSetupCancellationAtEveryNetworkStage(t *testing.T) {
	for _, stage := range []string{"begin", "browser", "poll", "probe"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			wait := func(ctx context.Context) { close(entered); <-ctx.Done() }
			opened, saved := 0, 0
			services := setupTestServices(&opened, &saved)
			flag := "--qr"
			switch stage {
			case "begin":
				services.begin = func(ctx context.Context) (*feishu.BeginResult, error) { wait(ctx); return nil, ctx.Err() }
			case "browser":
				flag = "--browser"
				services.open = func(ctx context.Context, _ string) error { wait(ctx); return ctx.Err() }
			case "poll":
				services.poll = func(ctx context.Context, _ *feishu.BeginResult) (*feishu.PollResult, error) {
					wait(ctx)
					return nil, ctx.Err()
				}
			case "probe":
				services.probe = func(ctx context.Context, _, _, _ string) (string, string, error) { wait(ctx); return "", "", ctx.Err() }
			}
			done := make(chan error, 1)
			go func() {
				result, err := runFeishuSetup(slashcmd.Context{Ctx: ctx, Stdout: io.Discard}, []string{flag}, services)
				if err == nil && !strings.Contains(result.Output, "已取消") {
					err = errors.New("missing cancellation result")
				}
				done <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("stage not reached")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancel hung")
			}
			if saved != 0 {
				t.Fatal("cancelled setup overwrote credentials")
			}
		})
	}
}

func TestFeishuChoiceCancellationDoesNotConsumeNextInput(t *testing.T) {
	in, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := chooseFeishuLogin(ctx, in, io.Discard); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input cancellation hung")
	}
	writer.Write([]byte("next\n"))
	b := make([]byte, 5)
	if _, err := io.ReadFull(in, b); err != nil || string(b) != "next\n" {
		t.Fatal("cancelled reader stole subsequent input", string(b), err)
	}
}

func TestFeishuQRPreservesModulesQuietZoneAndContrast(t *testing.T) {
	var out bytes.Buffer
	if err := renderFeishuQR(&out, setupTestURL); err != nil {
		t.Fatal(err)
	}
	code, _ := qrcode.New(setupTestURL, qrcode.Medium)
	bits := code.Bitmap()
	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(out.String(), "\n"), "\n"), "\n")
	if len(lines) != (len(bits)+1)/2 {
		t.Fatal("QR height changed")
	}
	for y, line := range lines {
		if !strings.HasPrefix(line, "\x1b[30;47m") || !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatal("QR depends on terminal theme")
		}
		modules := []rune(ansi.Strip(line))
		if len(modules) != len(bits) || len(modules) > 103 {
			t.Fatal("QR wrapped or quiet zone lost")
		}
		for x, glyph := range modules {
			top, bottom := glyph == '█' || glyph == '▀', glyph == '█' || glyph == '▄'
			if top != bits[y*2][x] || (y*2+1 < len(bits) && bottom != bits[y*2+1][x]) {
				t.Fatal("QR modules corrupted")
			}
		}
	}
}
