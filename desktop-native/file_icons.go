package main

import (
	"github.com/egoist/mygo/ui"
	"strings"
)

// File badges share the extension mapping of the original desktop UI.
var nativeFileBadges = map[string]string{
	"go":            "🐹",
	"rs":            "🦀",
	"ts":            "📘",
	"tsx":           "📘",
	"js":            "📙",
	"mjs":           "📙",
	"cjs":           "📙",
	"jsx":           "📙",
	"py":            "🐍",
	"java":          "☕",
	"kt":            "☕",
	"kts":           "☕",
	"rb":            "💎",
	"php":           "🐘",
	"swift":         "🍎",
	"c":             "🔵",
	"h":             "🔵",
	"cpp":           "🔵",
	"cc":            "🔵",
	"cxx":           "🔵",
	"hpp":           "🔵",
	"cs":            "🟣",
	"lua":           "🌙",
	"r":             "📊",
	"scala":         "🔴",
	"clj":           "🟢",
	"ex":            "💧",
	"exs":           "💧",
	"erl":           "abra",
	"json":          "🔧",
	"jsonc":         "🔧",
	"yml":           "⚙️",
	"yaml":          "⚙️",
	"toml":          "⚙️",
	"ini":           "⚙️",
	"conf":          "⚙️",
	"env":           "🔐",
	"lock":          "🔒",
	"html":          "🌐",
	"htm":           "🌐",
	"css":           "🎨",
	"scss":          "🎨",
	"less":          "🎨",
	"vue":           "💚",
	"svelte":        "🔥",
	"md":            "📝",
	"markdown":      "📝",
	"txt":           "📄",
	"pdf":           "📕",
	"png":           "🖼️",
	"jpg":           "🖼️",
	"jpeg":          "🖼️",
	"gif":           "🖼️",
	"webp":          "🖼️",
	"bmp":           "🖼️",
	"svg":           "🖼️",
	"ico":           "🖼️",
	"mp3":           "🎵",
	"wav":           "🎵",
	"flac":          "🎵",
	"ogg":           "🎵",
	"m4a":           "🎵",
	"mp4":           "🎬",
	"webm":          "🎬",
	"sql":           "🗄️",
	"graphql":       "⟡",
	"gql":           "⟡",
	"proto":         "🔧",
	"dockerfile":    "🐳",
	"sh":            "💲",
	"bash":          "💲",
	"zsh":           "💲",
	"fish":          "💲",
	"ps1":           "💲",
	"gitignore":     "🚫",
	"gitattributes": "🚫",
	"gitmodules":    "🚫",
	"makefile":      "🔨",
}

func fileBadge(c *ui.Context, name string) {
	base := strings.ToLower(portableBase(name))
	ext := base
	if at := strings.LastIndex(base, "."); at >= 0 {
		ext = base[at+1:]
	}
	if symbol := nativeFileBadges[ext]; symbol != "" {
		ui.Text(c, symbol).FontSize(13).Width(16)
	} else {
		icon(c, "file")
	}
}
func fileButton(c *ui.Context, label string) ui.Element {
	e := ui.ButtonBase(c).Label(label).Gap(6).Padding(5, 7).Radius(6).TextColor(c.Theme().TextMuted).FontSize(12)
	if e.Hovered() {
		e.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
	}
	e.Children(func() { fileBadge(c, label); ui.Text(c, label).SingleLine().MinWidth(0) })
	return e
}
