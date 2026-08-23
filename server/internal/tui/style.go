package tui

import "github.com/charmbracelet/lipgloss"

var (
	accent = lipgloss.Color("99")
	muted  = lipgloss.Color("240")
	good   = lipgloss.Color("42")
	warn   = lipgloss.Color("214")
	fg     = lipgloss.Color("252")
)

type styles struct {
	header      lipgloss.Style
	headerName  lipgloss.Style
	status      lipgloss.Style
	logPane     lipgloss.Style
	bar         lipgloss.Style
	key         lipgloss.Style
	keyLabel    lipgloss.Style
	dim         lipgloss.Style
	selected    lipgloss.Style
	wizardTitle lipgloss.Style
	wizardBox   lipgloss.Style
	good        lipgloss.Style
	warn        lipgloss.Style
}

func newStyles() styles {
	return styles{
		header:      lipgloss.NewStyle().Padding(0, 1),
		headerName:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		status:      lipgloss.NewStyle().Foreground(muted),
		logPane:     lipgloss.NewStyle().Padding(0, 1),
		bar:         lipgloss.NewStyle().Padding(0, 1).Foreground(fg),
		key:         lipgloss.NewStyle().Foreground(accent).Bold(true),
		keyLabel:    lipgloss.NewStyle().Foreground(fg),
		dim:         lipgloss.NewStyle().Foreground(muted),
		selected:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		wizardTitle: lipgloss.NewStyle().Bold(true).Foreground(accent).MarginBottom(1),
		wizardBox:   lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		good:        lipgloss.NewStyle().Foreground(good),
		warn:        lipgloss.NewStyle().Foreground(warn),
	}
}
