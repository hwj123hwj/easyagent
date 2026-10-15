package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme holds the color scheme and lipgloss styles for the TUI.
// It auto-adapts to light/dark terminal backgrounds via AdaptiveColor.
type Theme struct {
	// Role labels
	UserLabel      lipgloss.Style
	AssistantLabel lipgloss.Style
	SystemLabel    lipgloss.Style

	// Message content
	UserContent      lipgloss.Style
	AssistantContent lipgloss.Style
	SystemContent    lipgloss.Style

	// Tool panels
	ToolHeader       lipgloss.Style
	ToolBody         lipgloss.Style
	ToolErrorBorder  lipgloss.Style
	ToolActiveBorder lipgloss.Style
	ToolDoneBorder   lipgloss.Style

	// Diff
	DiffAdded   lipgloss.Style
	DiffRemoved lipgloss.Style
	DiffContext lipgloss.Style
	DiffHeader  lipgloss.Style

	// Input
	InputPrompt  lipgloss.Style
	InputBox     lipgloss.Style
	InputDimText lipgloss.Style

	// Status bar
	StatusBar    lipgloss.Style
	StatusReady  lipgloss.Style
	StatusBusy   lipgloss.Style
	StatusError  lipgloss.Style
	StatusDim    lipgloss.Style
	StatusAccent lipgloss.Style

	// General
	Separator   lipgloss.Style
	Spinner     lipgloss.Style
	Timestamp   lipgloss.Style
	ErrorText   lipgloss.Style
	SuccessText lipgloss.Style
	WarnText    lipgloss.Style
	HelpText    lipgloss.Style
}

var defaultTheme *Theme

// DefaultTheme returns the cached default theme.
func DefaultTheme() *Theme {
	if defaultTheme != nil {
		return defaultTheme
	}
	defaultTheme = newDefaultTheme()
	return defaultTheme
}

func newDefaultTheme() *Theme {
	// Color palette — adaptive (light/dark)
	cPrimary := lipgloss.AdaptiveColor{Light: "#087F78", Dark: "#82D5CA"}
	cSuccess := lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	cError := lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	cWarn := lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	cDim := lipgloss.AdaptiveColor{Light: "#656D76", Dark: "#8B949E"}
	cBorder := lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	cBorderErr := lipgloss.AdaptiveColor{Light: "#FFB8AE", Dark: "#F85149"}
	cBorderActive := lipgloss.AdaptiveColor{Light: "#087F78", Dark: "#82D5CA"}
	cBorderDone := lipgloss.AdaptiveColor{Light: "#4AC26B", Dark: "#3FB950"}

	t := &Theme{
		// ── Role labels ──
		UserLabel: lipgloss.NewStyle().
			Foreground(cPrimary).
			Bold(true),
		AssistantLabel: lipgloss.NewStyle().
			Foreground(cPrimary).
			Bold(true),
		SystemLabel: lipgloss.NewStyle().
			Foreground(cWarn).
			Bold(true),

		// ── Content ──
		UserContent: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#E6EDF3"}),
		AssistantContent: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#E6EDF3"}),
		SystemContent: lipgloss.NewStyle().
			Foreground(cDim).
			Italic(true),

		// ── Tool panels ──
		ToolHeader: lipgloss.NewStyle().
			Foreground(cPrimary).
			Bold(true),
		ToolBody: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#C9D1D9"}),
		ToolErrorBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorderErr).
			Padding(0, 1),
		ToolActiveBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorderActive).
			Padding(0, 1),
		ToolDoneBorder: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorderDone).
			Padding(0, 1),

		// ── Diff ──
		DiffAdded: lipgloss.NewStyle().
			Foreground(cSuccess),
		DiffRemoved: lipgloss.NewStyle().
			Foreground(cError),
		DiffContext: lipgloss.NewStyle().
			Foreground(cDim),
		DiffHeader: lipgloss.NewStyle().
			Foreground(cPrimary).
			Bold(true),

		// ── Input ──
		InputPrompt: lipgloss.NewStyle().
			Foreground(cPrimary).
			Bold(true),
		InputBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1),
		InputDimText: lipgloss.NewStyle().
			Foreground(cDim),

		// ── Status bar ──
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#24292F", Dark: "#8B949E"}),
		StatusReady: lipgloss.NewStyle().
			Foreground(cSuccess).
			Bold(true),
		StatusBusy: lipgloss.NewStyle().
			Foreground(cWarn).
			Bold(true),
		StatusError: lipgloss.NewStyle().
			Foreground(cError).
			Bold(true),
		StatusDim: lipgloss.NewStyle().
			Foreground(cDim),
		StatusAccent: lipgloss.NewStyle().
			Foreground(cPrimary).
			Bold(true),

		// ── General ──
		Separator: lipgloss.NewStyle().
			Foreground(cBorder),
		Spinner: lipgloss.NewStyle().
			Foreground(cWarn),
		Timestamp: lipgloss.NewStyle().
			Foreground(cDim),
		ErrorText: lipgloss.NewStyle().
			Foreground(cError).
			Bold(true),
		SuccessText: lipgloss.NewStyle().
			Foreground(cSuccess),
		WarnText: lipgloss.NewStyle().
			Foreground(cWarn).
			Bold(true),
		HelpText: lipgloss.NewStyle().
			Foreground(cDim).
			Italic(true),
	}

	return t
}
