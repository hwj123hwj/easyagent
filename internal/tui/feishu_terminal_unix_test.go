//go:build !windows

package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hwj123hwj/easyagent/internal/agents/coding/commands"
	"github.com/hwj123hwj/easyagent/sdk/runtime"
	"github.com/hwj123hwj/easyagent/sdk/slashcmd"
)

// The child runs the real TUI and Feishu handler; only Feishu HTTP is redirected
// to a local test server. It never creates an upstream app or writes credentials.
func TestFeishuTerminalHelper(t *testing.T) {
	endpoint := os.Getenv("EA_TEST_FEISHU_TERMINAL")
	if endpoint == "" {
		return
	}
	base, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = &feishuTestTransport{base: base, original: http.DefaultTransport}
	registry := slashcmd.NewRegistry()
	commands.RegisterFeishuCommands(registry)
	registry.Register(slashcmd.Command{Name: "alive", Handler: func(slashcmd.Context, string) (slashcmd.CommandResult, error) {
		return slashcmd.CommandResult{Output: "TERMINAL_RESTORED"}, nil
	}})
	m := New(&runtime.AgentSession{}, registry, true)
	p := newProgram(m)
	m.SetProgram(p)
	defer m.cancelSlashCmd()
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
}

type feishuTestTransport struct {
	base     *url.URL
	original http.RoundTripper
}

func (t *feishuTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.URL.Scheme, copy.URL.Host = t.base.Scheme, t.base.Host
	return t.original.RoundTrip(copy)
}

func TestFeishuTerminalWizardCancelRestoresTUI(t *testing.T) {
	for _, stage := range []string{"choice", "poll"} {
		t.Run(stage, func(t *testing.T) {
			polling := make(chan struct{}, 8)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				w.Header().Set("Content-Type", "application/json")
				switch r.Form.Get("action") {
				case "init":
					fmt.Fprint(w, `{"supported_auth_methods":["client_secret"]}`)
				case "begin":
					fmt.Fprint(w, `{"device_code":"test-only-code","verification_uri_complete":"https://open.feishu.cn/page/launcher?user_code=test-only","interval":600,"expires_in":600}`)
				case "poll":
					polling <- struct{}{}
					fmt.Fprint(w, `{"error":"authorization_pending"}`)
				default:
					t.Error("unexpected registration request")
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFeishuTerminalHelper$")
			cmd.Env = append(os.Environ(), "EA_TEST_FEISHU_TERMINAL="+srv.URL, "TERM=xterm-256color", "SSH_CONNECTION=test-server")
			terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 37, Cols: 103})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { terminal.Close(); cmd.Process.Kill(); cmd.Wait() }()
			chunks := make(chan string, 256)
			go func() {
				defer close(chunks)
				b := make([]byte, 4096)
				for {
					n, err := terminal.Read(b)
					if n > 0 {
						select {
						case chunks <- string(b[:n]):
						case <-ctx.Done():
							return
						}
					}
					if err != nil {
						return
					}
				}
			}()
			var output strings.Builder
			wait := func(text string) {
				t.Helper()
				for !strings.Contains(output.String(), text) {
					select {
					case chunk, ok := <-chunks:
						if !ok {
							t.Fatalf("TUI exited waiting for %q: %s", text, output.String())
						}
						output.WriteString(chunk)
					case <-ctx.Done():
						t.Fatalf("TUI hung waiting for %q: %s", text, output.String())
					}
				}
			}
			wait("What would you like")
			// A trailing space dismisses the subcommand autocomplete before Enter.
			io.WriteString(terminal, "/feishu setup \r")
			wait("请选择")
			choiceAt := strings.Index(output.String(), "选择授权方式")
			exitAt := strings.LastIndex(output.String()[:choiceAt], "\x1b[?1049l")
			if exitAt < 0 {
				t.Fatal("wizard wrote into active TUI renderer")
			}
			if stage == "poll" {
				io.WriteString(terminal, "\r")
				wait("\x1b[30;47m")
				wait("等待授权完成")
				select {
				case <-polling:
				case <-ctx.Done():
					t.Fatal("registration never polled")
				}
			}
			io.WriteString(terminal, "\x03")
			wait("飞书配置已取消")
			io.WriteString(terminal, "/alive \r")
			wait("TERMINAL_RESTORED")
			if strings.Contains(output.String(), "Waiting...") {
				t.Fatal("background spinner leaked into wizard")
			}
		})
	}
}
