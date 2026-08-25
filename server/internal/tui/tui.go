package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	bprogress "github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/buildview"
	"github.com/laminara/laminara/server/internal/humanize"
)

type uiState int

const (
	stateMain uiState = iota
	stateWizard
	stateBuildPick
	statePlayers
	stateCard
	stateSettings
	stateHelp
)

type confirmPrompt struct {
	question string
	verb     string
	command  string
	icon     string
}

func (c confirmPrompt) empty() bool { return c.command == "" }

func deleteConfirm(name string, set iconSet) confirmPrompt {
	return confirmPrompt{
		question: "удалить сборку «" + name + "»?",
		verb:     "удалять",
		command:  "delete " + name,
		icon:     set.remove,
	}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type logMsg string
type tickMsg struct{}
type progressMsg struct {
	phase   string
	current int64
	total   int64
	message string
}
type versionsMsg struct {
	versions       []*adminv1.VersionInfo
	latestRelease  string
	latestSnapshot string
}
type loadersMsg struct{ loaders []*adminv1.LoaderInfo }
type statusTickMsg struct{}
type commandsMsg struct{ names []string }
type statusMsg struct {
	version   string
	startedAt time.Time
	modules   uint32
	builds    int
	err       error
}
type buildsMsg struct{ builds []*adminv1.BuildInfo }
type playersMsg struct {
	list []*adminv1.BuildPlayers
	err  error
}
type playersTickMsg struct{}
type settingsMsg struct {
	page *adminv1.ListSettingsResponse
	err  error
}
type settingsSavedMsg struct {
	path      string
	err       error
	added     bool
	removed   bool
	restarted bool
}
type execDoneMsg struct {
	line     string
	exitCode int32
	err      error
}
type errMsg struct{ err error }

type Model struct {
	ctx    context.Context
	client adminv1connect.AdminServiceClient
	icons  iconSet
	styles styles

	width, height int
	viewport      viewport.Model
	logs          []string
	logCh         chan string
	progCh        chan progressMsg

	state        uiState
	wizard       wizard
	picker       picker
	pickerAction string

	input       textinput.Model
	commandMode bool
	confirm     confirmPrompt
	commands    []string
	history     []string
	historyAt   int

	help     menu
	builds   []*adminv1.BuildInfo
	players  playersState
	card     cardState
	settings settingsState

	status    statusMsg
	running   bool
	runLabel  string
	started   time.Time
	spinFrame int
	prog      progressMsg
	bar       bprogress.Model

	quitting bool
}

func Run(ctx context.Context, client adminv1connect.AdminServiceClient, nerd bool) error {
	logCh := make(chan string, 512)
	st := newStyles()
	go streamLogs(ctx, client, logCh, st)

	input := textinput.New()
	input.Placeholder = "команда проекта — help покажет список"
	input.Prompt = "› "
	input.CharLimit = 512

	model := Model{
		ctx:    ctx,
		client: client,
		icons:  icons(nerd),
		styles: st,
		logCh:  logCh,
		progCh: make(chan progressMsg, 128),
		input:  input,
		bar:    bprogress.New(bprogress.WithSolidFill("#ecc275"), bprogress.WithoutPercentage(), bprogress.WithFillCharacters('█', '░')),
	}
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForLog(m.logCh), waitForProgress(m.progCh), fetchStatus(m.ctx, m.client), statusTick(), fetchCommands(m.ctx, m.client))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.settings.menu.resize(msg.Width, m.contentHeight()-4)
		m.players.menu.resize(msg.Width, m.contentHeight()-4)
		m.card.menu.resize(msg.Width, m.contentHeight()-4)
		m.help.resize(msg.Width, m.contentHeight()-4)
		m.viewport = viewport.New(msg.Width, m.contentHeight())
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
		m.viewport.GotoBottom()
		m.bar.Width = min(46, msg.Width/3)
		return m, nil
	case logMsg:
		m.appendLog(string(msg))
		return m, waitForLog(m.logCh)
	case progressMsg:
		m.prog = msg
		return m, waitForProgress(m.progCh)
	case tickMsg:
		if !m.running {
			return m, nil
		}
		m.spinFrame = (m.spinFrame + 1) % len(spinnerFrames)
		return m, tickCmd()
	case execDoneMsg:
		m.running = false
		m.prog = progressMsg{}
		return m, nil
	case errMsg:
		m.appendLog(m.styles.warn.Render(m.icons.quit + " " + msg.err.Error()))
		if m.state == stateWizard {
			m.state = stateMain
		}
		return m, nil
	case statusMsg:
		if msg.err == nil || m.status.version == "" {
			m.status = msg
		} else {
			m.status.err = msg.err
		}
		return m, nil
	case statusTickMsg:
		return m, tea.Batch(fetchStatus(m.ctx, m.client), statusTick())
	case commandsMsg:
		m.commands = msg.names
		return m, nil
	case buildsMsg:
		return m.onBuilds(msg)
	case playersMsg:
		m.fillPlayers(msg)
		if m.card.build != nil {
			m.card.players = pickPlayers(msg.list, m.card.build.Name)
			m.fillCard()
		}
		return m, nil
	case playersTickMsg:
		if m.state != statePlayers {
			return m, nil
		}
		return m, tea.Batch(fetchPlayers(m.ctx, m.client), playersTick())
	case settingsMsg:
		if msg.err != nil {
			m.settings.menu.problem = errorText(msg.err)
			return m, nil
		}
		m.fillSettings(msg.page)
		return m, nil
	case settingsSavedMsg:
		return m.onSettingsSaved(msg)
	}

	switch m.state {
	case stateWizard:
		return m.updateWizard(msg)
	case stateBuildPick:
		return m.updateBuildPick(msg)
	case statePlayers:
		return m.updatePlayers(msg)
	case stateCard:
		return m.updateCard(msg)
	case stateSettings:
		return m.updateSettings(msg)
	case stateHelp:
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "esc", "?", "q", "enter":
				m.state = stateMain
			}
		}
		return m, nil
	default:
		return m.updateMain(msg)
	}
}

func (m Model) startCommand(line string) (tea.Model, tea.Cmd) {
	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
		if len(m.history) > 50 {
			m.history = m.history[1:]
		}
	}
	m.historyAt = len(m.history)
	m.running = true
	m.runLabel = line
	m.started = time.Now()
	m.prog = progressMsg{}
	m.appendLog(m.styles.echo.Render(m.icons.cursor+" ") + m.styles.keyLabel.Render(line))
	return m, tea.Batch(runCommand(m.ctx, m.client, line, m.logCh, m.progCh, m.styles, m.started), tickCmd())
}

func (m Model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	if !m.confirm.empty() {
		switch key.String() {
		case "y", "Y", "д", "Д":
			line := m.confirm.command
			m.confirm = confirmPrompt{}
			return m.startCommand(line)
		default:
			verb := m.confirm.verb
			m.confirm = confirmPrompt{}
			m.appendLog(m.styles.dim.Render("не стал " + verb))
			return m, nil
		}
	}
	if m.commandMode {
		switch key.String() {
		case "esc":
			m.commandMode = false
			m.input.Blur()
			m.input.SetValue("")
			return m, nil
		case "tab":
			if completed, ok := complete(m.input.Value(), m.commands); ok {
				m.input.SetValue(completed)
				m.input.CursorEnd()
			}
			return m, nil
		case "up":
			if len(m.history) > 0 && m.historyAt > 0 {
				m.historyAt--
				m.input.SetValue(m.history[m.historyAt])
				m.input.CursorEnd()
			}
			return m, nil
		case "down":
			if m.historyAt < len(m.history)-1 {
				m.historyAt++
				m.input.SetValue(m.history[m.historyAt])
				m.input.CursorEnd()
			} else {
				m.historyAt = len(m.history)
				m.input.SetValue("")
			}
			return m, nil
		case "enter":
			line := strings.TrimSpace(m.input.Value())
			m.commandMode = false
			m.input.Blur()
			m.input.SetValue("")
			if line == "" {
				return m, nil
			}
			return m.startCommand(line)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	switch key.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.state = stateHelp
		m.help = newMenu("Клавиши консоли")
		m.help.resize(m.width, m.contentHeight()-4)
		m.fillHelp()
		return m, nil
	case "/", ":":
		m.commandMode = true
		return m, m.input.Focus()
	case "i":
		wiz, cmd := newWizard(m.ctx, m.client, m.icons, m.styles, "")
		m.wizard = wiz
		m.state = stateWizard
		return m, cmd
	case "b":
		m.pickerAction = "card"
		return m, fetchBuilds(m.ctx, m.client)
	case "l":
		return m.enterPlayers()
	case "s":
		return m.enterSettings()
	case "p":
		m.pickerAction = "publish"
		return m, fetchBuilds(m.ctx, m.client)
	case "u":
		m.pickerAction = "update"
		return m, fetchBuilds(m.ctx, m.client)
	case "d":
		m.pickerAction = "delete"
		return m, fetchBuilds(m.ctx, m.client)
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) updateWizard(msg tea.Msg) (tea.Model, tea.Cmd) {
	wiz, cmd := m.wizard.Update(msg)
	m.wizard = wiz
	if wiz.cancel {
		m.state = stateMain
		return m, nil
	}
	if wiz.done {
		m.state = stateMain
		return m.startCommand(wiz.commandLine)
	}
	return m, cmd
}

func (m Model) updateBuildPick(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
		m.state = stateMain
		return m, nil
	}
	pick, selected := m.picker.Update(msg)
	m.picker = pick
	if selected == nil {
		return m, nil
	}
	name := selected.value
	m.state = stateMain
	switch m.pickerAction {
	case "card":
		m.card = cardState{menu: newMenu(""), build: findBuild(m.builds, name), players: pickPlayers(m.players.list, name)}
		m.card.menu.resize(m.width, m.contentHeight()-4)
		m.fillCard()
		m.state = stateCard
		return m, fetchPlayers(m.ctx, m.client)
	case "publish":
		return m.startCommand("publish " + name)
	case "delete":
		m.confirm = deleteConfirm(name, m.icons)
		return m, nil
	case "update":
		wiz, cmd := newWizard(m.ctx, m.client, m.icons, m.styles, name)
		m.wizard = wiz
		m.state = stateWizard
		return m, cmd
	}
	return m, nil
}

func (m Model) onBuilds(msg buildsMsg) (tea.Model, tea.Cmd) {
	m.builds = msg.builds
	if len(msg.builds) == 0 {
		m.appendLog(m.styles.dim.Render("Сборок пока нет — соберите первую клавишей ") + m.styles.key.Render("i"))
		m.state = stateMain
		return m, nil
	}
	items := make([]pickItem, 0, len(msg.builds))
	for _, build := range msg.builds {
		items = append(items, pickItem{label: build.Name, value: build.Name, hint: buildview.StatusWord(build.Status)})
	}
	title := "Какую сборку опубликовать?"
	switch m.pickerAction {
	case "card":
		title = "Какая сборка интересует?"
	case "update":
		title = "Какую сборку пересобрать?"
	case "delete":
		title = "Какую сборку удалить?"
	}
	m.picker = newPicker(title, items, m.icons, m.styles)
	m.state = stateBuildPick
	return m, nil
}

func (m *Model) appendLog(line string) {
	m.logs = append(m.logs, line)
	if len(m.logs) > 2000 {
		m.logs = m.logs[len(m.logs)-2000:]
	}
	m.viewport.SetContent(strings.Join(m.logs, "\n"))
	m.viewport.GotoBottom()
}

func (m Model) contentHeight() int {
	extra := 6
	if m.running {
		extra = 7
	}
	h := m.height - extra
	if h < 1 {
		return 1
	}
	return h
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	sections := []string{m.headerView()}
	switch m.state {
	case stateWizard:
		sections = append(sections, lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, m.wizard.View()))
	case stateBuildPick:
		box := m.styles.wizardBox.Render(m.picker.View())
		sections = append(sections, lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, box))
	case statePlayers:
		sections = append(sections, m.playersView())
	case stateSettings:
		sections = append(sections, m.settingsView())
	case stateCard:
		sections = append(sections, m.cardView())
	case stateHelp:
		sections = append(sections, m.helpView())
	default:
		if len(m.logs) == 0 {
			sections = append(sections, m.emptyHint())
			break
		}
		m.viewport.Height = m.contentHeight()
		sections = append(sections, m.styles.logPane.Render(m.viewport.View()))
	}
	if m.running {
		sections = append(sections, m.progressView())
	}
	if m.state == stateMain || m.state == stateHelp || !m.confirm.empty() {
		sections = append(sections, m.styles.rule.Render(strings.Repeat("─", max(0, m.width))), m.inputView())
	}
	sections = append(sections, m.barView())
	return strings.Join(sections, "\n")
}

func (m Model) inputView() string {
	if !m.confirm.empty() {
		prompt := m.styles.selected.Render(m.confirm.icon + " " + m.confirm.question + "  ")
		choice := m.styles.key.Render("y") + " " + m.styles.keyLabel.Render(m.confirm.verb) + "    " + m.styles.dim.Render("любая другая — отмена")
		return m.styles.bar.Render(prompt + choice)
	}
	if m.commandMode {
		return m.styles.bar.Render(m.input.View())
	}
	hint := m.styles.faint.Render("› ") + m.styles.dim.Render("нажмите ") + m.styles.key.Render("/") + m.styles.dim.Render(" и наберите команду · ") + m.styles.summaryKey.Render("help") + m.styles.dim.Render(" — весь список")
	return m.styles.bar.Render(hint)
}

func (m Model) headerView() string {
	title := m.styles.headerName.Render(m.icons.dot + " Laminara")
	state := m.styles.good.Render("на связи ●")
	if m.status.err != nil {
		state = m.styles.bad.Render("нет связи ●")
	}
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(state) - 4
	if gap < 1 {
		gap = 1
	}
	line := m.styles.header.Render(title + strings.Repeat(" ", gap) + state)
	rule := m.styles.rule.Render(strings.Repeat("─", max(0, m.width)))
	return line + "\n" + m.summaryView() + "\n" + rule
}

func (m Model) summaryView() string {
	if m.status.version == "" {
		return m.styles.summary.Render("собираю сведения о проекте…")
	}
	field := func(label, value string) string {
		return m.styles.dim.Render(label+" ") + m.styles.summaryKey.Render(value)
	}
	parts := []string{
		field("версия", m.status.version),
		field("в работе", fmtDur(time.Since(m.status.startedAt))),
		field("сборок", fmt.Sprint(m.status.builds)),
		field("модулей", fmt.Sprint(m.status.modules)),
	}
	return m.styles.summary.Render(strings.Join(parts, m.styles.faint.Render("   ")))
}

var helpKeys = [][2]string{
	{"i", "собрать клиент — мастер спросит версию и загрузчик"},
	{"b", "карточка сборки — всё о ней на одном экране"},
	{"l", "кто сейчас в игре — список игроков по сборкам"},
	{"s", "настройки проекта — правятся прямо здесь"},
	{"u", "пересобрать сборку под новую версию"},
	{"p", "опубликовать сборку — лаунчеры увидят обновление"},
	{"d", "удалить сборку из проекта"},
	{"/", "ввести команду вручную — Tab дополняет, ↑↓ повторяют прошлые"},
	{"↑↓", "прокрутить ленту логов"},
	{"?", "эта справка"},
	{"q", "выйти из консоли — проект продолжит работать"},
}

func (m *Model) fillHelp() {
	items := make([]menuItem, 0, len(helpKeys)+2)
	for _, pair := range helpKeys {
		items = append(items, menuItem{id: "key:" + pair[0], label: pair[0], value: pair[1], static: true})
	}
	items = append(items,
		menuItem{id: "gap", static: true},
		menuItem{id: "commands", label: "Команды", value: "help — весь список; status, builds, build, players, settings, restart,", static: true, tone: toneMuted},
		menuItem{id: "commands2", label: "", value: "versions, loaders, install, publish, delete, auth, access, hwid, bans", static: true, tone: toneMuted},
	)
	m.help.title = "Клавиши консоли"
	m.help.subtitle = "Esc или ? — закрыть"
	m.help.setItems(items)
}

func (m Model) helpView() string {
	return m.centered(m.help.View(m.styles, m.icons))
}

func (m Model) emptyHint() string {
	return lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, m.styles.faint.Render("Здесь пойдут события проекта."))
}

func (m Model) progressView() string {
	spin := m.styles.selected.Render(spinnerFrames[m.spinFrame])
	phase := m.prog.phase
	if phase == "" {
		phase = m.runLabel
	}
	elapsed := m.styles.faint.Render(fmtDur(time.Since(m.started)))
	parts := []string{spin, m.styles.keyLabel.Render(phase)}
	if m.prog.total > 0 {
		m.bar.Width = min(40, max(10, m.width/3))
		share := float64(m.prog.current) / float64(m.prog.total)
		parts = append(parts, m.bar.ViewAs(share))
		parts = append(parts, m.styles.selected.Render(fmt.Sprintf("%3.0f%%", share*100)))
		parts = append(parts, m.styles.dim.Render(fmt.Sprintf("%d из %d", m.prog.current, m.prog.total)))
	}
	if m.prog.message != "" {
		parts = append(parts, m.styles.dim.Render(m.prog.message))
	}
	parts = append(parts, elapsed)
	return m.styles.bar.Render(strings.Join(parts, "  "))
}

func (m Model) barView() string {
	item := func(letter, label string) string {
		return m.styles.key.Render(letter) + " " + m.styles.keyLabel.Render(label)
	}
	switch m.state {
	case stateWizard, stateBuildPick:
		return m.styles.bar.Render(strings.Join([]string{
			item("↑↓", "выбрать"),
			item("Enter", "принять"),
			item("Esc", "назад"),
		}, "   "))
	case stateSettings:
		return m.settingsBar()
	case statePlayers:
		return m.playersBar()
	case stateCard:
		return m.styles.bar.Render(strings.Join([]string{
			item("p", "опубликовать"),
			item("u", "пересобрать"),
			item("d", "удалить"),
			item("Esc", "назад"),
		}, "   "))
	case stateHelp:
		return m.styles.bar.Render(item("Esc", "закрыть справку"))
	}
	if m.commandMode {
		return m.styles.bar.Render(strings.Join([]string{
			item("Enter", "выполнить"),
			item("Tab", "дополнить"),
			item("↑↓", "прошлые команды"),
			item("Esc", "отменить"),
		}, "   "))
	}
	left := []string{
		item("i", "собрать"),
		item("b", "сборки"),
		item("l", "игроки"),
		item("s", "настройки"),
		item("u", "обновить"),
		item("p", "опубликовать"),
		item("d", "удалить"),
	}
	right := []string{
		item("/", "команда"),
		item("?", "справка"),
		item("q", "выход"),
	}
	return m.styles.bar.Render(fitBar(left, right, m.width-4))
}

func fitBar(left, right []string, width int) string {
	const gutter = "   "
	tail := strings.Join(right, gutter)
	for len(left) > 0 {
		head := strings.Join(left, gutter)
		gap := width - lipgloss.Width(head) - lipgloss.Width(tail)
		if gap >= 2 {
			return head + strings.Repeat(" ", gap) + tail
		}
		left = left[:len(left)-1]
	}
	return tail
}

func (m Model) centered(body string) string {
	return lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, m.styles.wizardBox.Render(body))
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	for _, prefix := range []string{"invalid_argument: ", "unknown: ", "internal: ", "unimplemented: "} {
		text = strings.TrimPrefix(text, prefix)
	}
	return text
}

func fmtDur(d time.Duration) string {
	return humanize.Duration(d)
}

func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func statusTick() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return statusTickMsg{} })
}

func fetchStatus(ctx context.Context, client adminv1connect.AdminServiceClient) tea.Cmd {
	return func() tea.Msg {
		status, err := client.Status(ctx, connect.NewRequest(&adminv1.StatusRequest{}))
		if err != nil {
			return statusMsg{err: err}
		}
		out := statusMsg{
			version:   status.Msg.Version,
			startedAt: time.Unix(0, status.Msg.StartedAtUnixNanos),
			modules:   status.Msg.ModulesLoaded,
		}
		if builds, err := client.ListBuilds(ctx, connect.NewRequest(&adminv1.ListBuildsRequest{})); err == nil {
			out.builds = len(builds.Msg.Builds)
		}
		return out
	}
}

func waitForLog(ch chan string) tea.Cmd {
	return func() tea.Msg { return logMsg(<-ch) }
}

func waitForProgress(ch chan progressMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func streamLogs(ctx context.Context, client adminv1connect.AdminServiceClient, logCh chan string, st styles) {
	backscroll := uint32(200)
	lost := false
	for ctx.Err() == nil {
		stream, err := client.StreamLogs(ctx, connect.NewRequest(&adminv1.StreamLogsRequest{Backscroll: backscroll, Follow: true}))
		if err == nil {
			if lost {
				logCh <- st.good.Render("проект снова на связи")
				lost = false
			}
			for stream.Receive() {
				logCh <- formatLog(stream.Msg().Line, st)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if !lost {
			logCh <- st.faint.Render("связь с проектом прервалась — жду, пока он поднимется…")
			lost = true
		}
		backscroll = 0
		time.Sleep(time.Second)
	}
}

func formatLog(line *adminv1.LogLine, st styles) string {
	when := st.faint.Render(time.Unix(0, line.TimeUnixNanos).Format("15:04:05"))
	source := line.Source
	if source == "" {
		source = "server"
	}
	mark := st.faint.Render("│")
	message := line.Message
	switch line.Level {
	case adminv1.LogLevel_LOG_LEVEL_WARN:
		mark = st.warn.Render("!")
		message = st.warn.Render(message)
	case adminv1.LogLevel_LOG_LEVEL_ERROR:
		mark = st.bad.Render("✕")
		message = st.bad.Render(message)
	}
	return fmt.Sprintf("%s %s %s %s", when, st.source.Render(fmt.Sprintf("%-8s", source)), mark, message)
}

func fetchVersions(ctx context.Context, client adminv1connect.AdminServiceClient) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListVersions(ctx, connect.NewRequest(&adminv1.ListVersionsRequest{}))
		if err != nil {
			return errMsg{err}
		}
		return versionsMsg{versions: resp.Msg.Versions, latestRelease: resp.Msg.LatestRelease, latestSnapshot: resp.Msg.LatestSnapshot}
	}
}

func fetchLoaders(ctx context.Context, client adminv1connect.AdminServiceClient, mc string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListLoaders(ctx, connect.NewRequest(&adminv1.ListLoadersRequest{McVersion: mc}))
		if err != nil {
			return errMsg{err}
		}
		return loadersMsg{loaders: resp.Msg.Loaders}
	}
}

func fetchBuilds(ctx context.Context, client adminv1connect.AdminServiceClient) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListBuilds(ctx, connect.NewRequest(&adminv1.ListBuildsRequest{}))
		if err != nil {
			return errMsg{err}
		}
		return buildsMsg{builds: resp.Msg.Builds}
	}
}

func runCommand(ctx context.Context, client adminv1connect.AdminServiceClient, line string, logCh chan string, progCh chan progressMsg, st styles, started time.Time) tea.Cmd {
	return func() tea.Msg {
		stream, err := client.Exec(ctx, connect.NewRequest(&adminv1.ExecRequest{Line: line}))
		if err != nil {
			logCh <- st.bad.Render("✕ ") + st.keyLabel.Render(err.Error())
			return execDoneMsg{line: line, err: err}
		}
		var code int32
		for stream.Receive() {
			event := stream.Msg()
			if output := event.GetOutput(); output != nil {
				for _, part := range strings.Split(strings.TrimRight(output.Text, "\n"), "\n") {
					if part != "" {
						logCh <- part
					}
				}
			}
			if p := event.GetProgress(); p != nil {
				select {
				case progCh <- progressMsg{phase: p.Phase, current: p.Current, total: p.Total, message: p.Message}:
				default:
				}
			}
			if result := event.GetResult(); result != nil {
				code = result.ExitCode
			}
		}
		logCh <- commandResultLine(line, code, time.Since(started), st)
		return execDoneMsg{line: line, exitCode: code}
	}
}

func commandResultLine(line string, code int32, took time.Duration, st styles) string {
	spent := st.faint.Render(" · " + humanize.Duration(took))
	if code == 0 {
		return st.good.Render("✓ ") + st.keyLabel.Render(line) + spent
	}
	return st.bad.Render("✕ ") + st.keyLabel.Render(fmt.Sprintf("%s — код %d", line, code)) + spent
}

func statusLabel(status string, st styles) string {
	word := buildview.StatusWord(status)
	switch status {
	case "published":
		return st.good.Render(word)
	case "prepared":
		return st.warn.Render(word)
	default:
		return st.dim.Render(word)
	}
}

func fetchCommands(ctx context.Context, client adminv1connect.AdminServiceClient) tea.Cmd {
	return func() tea.Msg {
		stream, err := client.Exec(ctx, connect.NewRequest(&adminv1.ExecRequest{Line: "help"}))
		if err != nil {
			return commandsMsg{}
		}
		var names []string
		for stream.Receive() {
			output := stream.Msg().GetOutput()
			if output == nil {
				continue
			}
			for _, line := range strings.Split(output.Text, "\n") {
				name, _, found := strings.Cut(strings.TrimSpace(line), " ")
				if found && name != "" {
					names = append(names, name)
				}
			}
		}
		return commandsMsg{names: names}
	}
}

func complete(typed string, commands []string) (string, bool) {
	word := strings.TrimLeft(typed, " ")
	if word == "" || strings.Contains(word, " ") {
		return "", false
	}
	var matches []string
	for _, name := range commands {
		if strings.HasPrefix(name, word) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", false
	}
	shared := matches[0]
	for _, candidate := range matches[1:] {
		for !strings.HasPrefix(candidate, shared) {
			shared = shared[:len(shared)-1]
		}
	}
	if shared == word {
		return "", false
	}
	if len(matches) == 1 {
		return shared + " ", true
	}
	return shared, true
}
