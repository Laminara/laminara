package tui

type iconSet struct {
	install string
	builds  string
	update  string
	publish string
	remove  string
	logs    string
	help    string
	quit    string
	cursor  string
	search  string
	dot     string
}

func unicodeIcons() iconSet {
	return iconSet{
		install: "↧",
		builds:  "▤",
		update:  "⟳",
		publish: "↥",
		remove:  "🗑",
		logs:    "≡",
		help:    "?",
		quit:    "✕",
		cursor:  "▸",
		search:  "⌕",
		dot:     "●",
	}
}

func nerdIcons() iconSet {
	return iconSet{
		install: "",
		builds:  "",
		update:  "",
		publish: "",
		logs:    "",
		help:    "",
		quit:    "",
		cursor:  "",
		search:  "",
		dot:     "",
	}
}

func icons(nerd bool) iconSet {
	if nerd {
		return nerdIcons()
	}
	return unicodeIcons()
}
