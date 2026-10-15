package main

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

var quotedFilePath = regexp.MustCompile("`((?:/|~/|[A-Za-z]:[\\\\/])[^`\\n]+)`")

func markdownFileReferences(source string) string {
	lines := strings.Split(source, "\n")
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		lines[i] = quotedFilePath.ReplaceAllStringFunc(line, func(token string) string {
			value := strings.Trim(token, "`")
			if strings.ContainsAny(value, "\x00\r\n") {
				return token
			}
			return "[" + token + "](ea-file:" + url.PathEscape(value) + ")"
		})
	}
	return strings.Join(lines, "\n")
}
func resolveMarkdownFile(workspace, base, href string) (string, error) {
	if strings.HasPrefix(href, "ea-file:") {
		var err error
		href, err = url.PathUnescape(strings.TrimPrefix(href, "ea-file:"))
		if err != nil {
			return "", err
		}
	}
	if strings.ContainsAny(href, "\x00\r\n") || strings.HasPrefix(href, "//") {
		return "", errors.New("文件路径无效")
	}
	if u, err := url.Parse(href); err == nil && u.Scheme != "" && !(len(u.Scheme) == 1 && len(href) > 2 && href[1] == ':') {
		return "", errors.New("文件链接协议无效")
	}
	href = strings.ReplaceAll(href, "\\", "/")
	if strings.HasPrefix(href, "~/") {
		return "", errors.New("请使用当前主机上的绝对路径")
	}
	if !strings.HasPrefix(href, "/") && !(len(href) > 2 && href[1] == ':') {
		directory := strings.ReplaceAll(workspace, "\\", "/")
		if base != "" {
			directory = path.Dir(strings.ReplaceAll(base, "\\", "/"))
		}
		href = path.Join(directory, href)
	}
	return path.Clean(href), nil
}
func decodeNativeImage(data []byte, name string) (ui.ImageSource, error) {
	if len(data) > 8<<20 {
		return nil, errors.New("图片超过 8 MiB")
	}
	if strings.EqualFold(path.Ext(name), ".svg") || strings.Contains(string(data[:min(len(data), 300)]), "<svg") {
		return ui.ParseSVG(data)
	}
	return ui.DecodeBitmap(data)
}
func (a *preview) renderSessionMarkdown(c *ui.Context, d *markdownDocument, text string, s *session, base string, streaming bool) {
	if a.Live == nil {
		d.render(c, text, streaming)
		return
	}
	l := a.Live
	scope := strconv.FormatUint(l.epoch, 10) + "\x00" + l.Config.ActiveHost + "\x00" + s.RemoteID + "\x00" + s.Workspace + "\x00" + base
	if d.MediaScope != scope {
		d.MediaScope = scope
		d.options = markdown.Options{}
		d.ready = false
		ctx, b, sid, workspace := l.hostCtx, l.Client, s.RemoteID, s.Workspace
		d.options.OnLink = func(href string) {
			if safeMarkdownLink(href) {
				go mygo.Shell.OpenExternal(href)
				return
			}
			if strings.HasPrefix(href, "#") {
				d.Anchor = strings.ToLower(strings.TrimPrefix(href, "#"))
				return
			}
			target, err := resolveMarkdownFile(workspace, base, href)
			if err != nil {
				l.Error = err.Error()
				return
			}
			if a.current() != s {
				return
			}
			a.openResource("files")
			a.openNativeFile(target)
		}
		d.options.Block = func(c *ui.Context, doc *parser.Document, index int) bool {
			if doc.Block(index).Kind == parser.Heading && d.Anchor != "" && markdown.Slug(parser.PlainText(doc.Inlines(index))) == d.Anchor {
				ui.Box(c).Height(0).ScrollIntoView()
				d.Anchor = ""
			}
			return false
		}
		d.options.LoadImage = func(href string) (ui.ImageSource, error) {
			if strings.HasPrefix(href, "data:image/") {
				meta, encoded, ok := strings.Cut(href, ",")
				if !ok || !strings.HasSuffix(meta, ";base64") || len(encoded) > 12<<20 {
					return nil, errors.New("图片数据无效")
				}
				data, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return nil, err
				}
				return decodeNativeImage(data, meta)
			}
			if u, err := url.Parse(href); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil {
				request, err := http.NewRequestWithContext(ctx, "GET", href, nil)
				if err != nil {
					return nil, err
				}
				client := http.Client{Timeout: 20 * time.Second}
				response, err := client.Do(request)
				if err != nil {
					return nil, err
				}
				defer response.Body.Close()
				if response.StatusCode != 200 {
					return nil, errors.New("图片无法读取")
				}
				data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
				if err != nil {
					return nil, err
				}
				return decodeNativeImage(data, u.Path)
			}
			target, err := resolveMarkdownFile(workspace, base, href)
			if err != nil {
				return nil, err
			}
			if b == nil || sid == "" {
				return nil, errors.New("未连接工作区")
			}
			var result map[string]any
			if err = b.request(ctx, http.MethodGet, a.workspacePath("file-data", target, sid), nil, &result); err != nil {
				return nil, err
			}
			data, err := base64.StdEncoding.DecodeString(str(result["data"]))
			if err != nil {
				return nil, err
			}
			return decodeNativeImage(data, target)
		}
	}
	d.render(c, markdownFileReferences(text), streaming)
}
