package tui

import "github.com/charmbracelet/lipgloss"

var (
	accent   = lipgloss.CompleteColor{TrueColor: "#ecc275", ANSI256: "180", ANSI: "3"}
	accentIn = lipgloss.CompleteColor{TrueColor: "#241705", ANSI256: "235", ANSI: "0"}
	muted    = lipgloss.CompleteColor{TrueColor: "#8a8079", ANSI256: "245", ANSI: "7"}
	faint    = lipgloss.CompleteColor{TrueColor: "#5f5750", ANSI256: "240", ANSI: "8"}
	good     = lipgloss.CompleteColor{TrueColor: "#5fd08a", ANSI256: "78", ANSI: "2"}
	warn     = lipgloss.CompleteColor{TrueColor: "#e0a94a", ANSI256: "179", ANSI: "3"}
	bad      = lipgloss.CompleteColor{TrueColor: "#e4695a", ANSI256: "167", ANSI: "1"}
	fg       = lipgloss.CompleteColor{TrueColor: "#e6e0d8", ANSI256: "253", ANSI: "7"}
)

type styles struct {
	header      lipgloss.Style
	headerName  lipgloss.Style
	summary     lipgloss.Style
	summaryKey  lipgloss.Style
	rule        lipgloss.Style
	logPane     lipgloss.Style
	bar         lipgloss.Style
	key         lipgloss.Style
	keyLabel    lipgloss.Style
	dim         lipgloss.Style
	faint       lipgloss.Style
	selected    lipgloss.Style
	wizardTitle lipgloss.Style
	wizardBox   lipgloss.Style
	good        lipgloss.Style
	warn        lipgloss.Style
	bad         lipgloss.Style
	echo        lipgloss.Style
	source      lipgloss.Style
}

func newStyles() styles {
	return styles{
		header:      lipgloss.NewStyle().Padding(0, 2),
		headerName:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		summary:     lipgloss.NewStyle().Padding(0, 2).Foreground(muted),
		summaryKey:  lipgloss.NewStyle().Foreground(fg),
		rule:        lipgloss.NewStyle().Foreground(faint),
		logPane:     lipgloss.NewStyle().Padding(0, 2),
		bar:         lipgloss.NewStyle().Padding(0, 2).Foreground(fg),
		key:         lipgloss.NewStyle().Bold(true).Foreground(accentIn).Background(accent).Padding(0, 1),
		keyLabel:    lipgloss.NewStyle().Foreground(fg),
		dim:         lipgloss.NewStyle().Foreground(muted),
		faint:       lipgloss.NewStyle().Foreground(faint),
		selected:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		wizardTitle: lipgloss.NewStyle().Bold(true).Foreground(accent).MarginBottom(1),
		wizardBox:   lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(accent),
		good:        lipgloss.NewStyle().Foreground(good),
		warn:        lipgloss.NewStyle().Foreground(warn),
		bad:         lipgloss.NewStyle().Foreground(bad),
		echo:        lipgloss.NewStyle().Foreground(accent),
		source:      lipgloss.NewStyle().Foreground(faint),
	}
}
