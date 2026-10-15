package main

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// MyGo keeps the parsed document, selection and syntax tokens on the view.
// Image loading uses the active core and its workspace access checks.
type markdownDocument struct {
	MediaScope string
	Anchor     string
	options    markdown.Options
	ready      bool
}

func (d *markdownDocument) render(c *ui.Context, reply string, streaming ...bool) {
	if !d.ready {
		d.ready = true
		d.options.Code = map[string]func(*ui.Context, *markdown.CodeBlockView){"mermaid": nativeDiagram}
		d.options.Reveal = &markdown.Reveal{Unit: markdown.AtOnce, Fade: -1}
		d.options.CodeBlock = func(c *ui.Context, b *markdown.CodeBlockView) {
			ui.Column(c).Gap(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(6).MinWidth(0).Children(func() {
				ui.Row(c).Padding(8, 12, 0, 12).Children(func() {
					ui.Text(c, b.Language).FontSize(12).TextColor(c.Theme().TextMuted)
					ui.Spacer(c)
					ui.Button(c, "复制代码").OnClick(func() { c.WriteClipboard(strings.TrimSuffix(b.Code(), "\n")) })
				})
				b.Body(c)
			})
		}
	}
	d.options.Theme = &markdown.Theme{FontSize: 14, LineHeight: 1.75, BlockSpacing: .85}
	if len(streaming) > 0 && streaming[0] {
		markdown.Stream(c, reply, &d.options).MinWidth(0)
	} else {
		markdown.View(c, reply, &d.options).MinWidth(0)
	}
}

func safeMarkdownLink(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	return ((u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "") || (u.Scheme == "mailto" && u.Opaque != "")
}
