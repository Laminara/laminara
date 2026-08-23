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
)

type uiState int

const (
	stateMain uiState = iota
	stateWizard
	stateBuildPick
)

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
type buildsMsg struct{ builds []*adminv1.BuildInfo }
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

	input         textinput.Model
	commandMode   bool
	confirmDelete string

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
	input.Placeholder = "введите команду (help — список)…"
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
		bar:    bprogress.New(bprogress.WithSolidFill("99"), bprogress.WithoutPercentage(), bprogress.WithFillCharacters('█', '░')),
	}
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitForLog(m.logCh), waitForProgress(m.progCh))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		if msg.err != nil {
			m.appendLog(m.styles.warn.Render(m.icons.quit + " " + msg.err.Error()))
		} else if msg.exitCode == 0 {
			m.appendLog(m.styles.good.Render("✓ " + msg.line))
		} else {
			m.appendLog(m.styles.warn.Render(fmt.Sprintf("%s %s (код %d)", m.icons.quit, msg.line, msg.exitCode)))
		}
		return m, nil
	case errMsg:
		m.appendLog(m.styles.warn.Render(m.icons.quit + " " + msg.err.Error()))
		if m.state == stateWizard {
			m.state = stateMain
		}
		return m, nil
	case buildsMsg:
		return m.onBuilds(msg)
	}

	switch m.state {
	case stateWizard:
		return m.updateWizard(msg)
	case stateBuildPick:
		return m.updateBuildPick(msg)
	default:
		return m.updateMain(msg)
	}
}

func (m Model) startCommand(line string) (tea.Model, tea.Cmd) {
	m.running = true
	m.runLabel = line
	m.started = time.Now()
	m.prog = progressMsg{}
	m.appendLog(m.styles.dim.Render(m.icons.cursor + " " + line))
	return m, tea.Batch(runCommand(m.ctx, m.client, line, m.logCh, m.progCh), tickCmd())
}

func (m Model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	if m.confirmDelete != "" {
		switch key.String() {
		case "y", "Y", "д", "Д":
			name := m.confirmDelete
			m.confirmDelete = ""
			return m.startCommand("delete " + name)
		default:
			m.confirmDelete = ""
			m.appendLog(m.styles.dim.Render("удаление отменено"))
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
	case "/", ":":
		m.commandMode = true
		return m, m.input.Focus()
	case "i":
		wiz, cmd := newWizard(m.ctx, m.client, m.icons, m.styles, "")
		m.wizard = wiz
		m.state = stateWizard
		return m, cmd
	case "b":
		m.pickerAction = "list"
		return m, fetchBuilds(m.ctx, m.client)
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
	case "publish":
		return m.startCommand("publish " + name)
	case "delete":
		m.confirmDelete = name
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
	if m.pickerAction == "list" {
		if len(msg.builds) == 0 {
			m.appendLog(m.styles.dim.Render("сборок пока нет"))
		}
		for _, build := range msg.builds {
			m.appendLog(m.icons.builds + " " + build.Name + "  " + m.styles.dim.Render(build.Status))
		}
		return m, nil
	}
	if len(msg.builds) == 0 {
		m.appendLog(m.styles.dim.Render("сборок пока нет — сначала установите клиент (i)"))
		m.state = stateMain
		return m, nil
	}
	items := make([]pickItem, 0, len(msg.builds))
	for _, build := range msg.builds {
		items = append(items, pickItem{label: build.Name, value: build.Name, hint: build.Status})
	}
	title := m.icons.publish + " Опубликовать сборку"
	switch m.pickerAction {
	case "update":
		title = m.icons.update + " Обновить сборку"
	case "delete":
		title = m.icons.remove + " Удалить сборку"
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
	extra := 4
	if m.running {
		extra = 5
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
		box := m.styles.wizardBox.Render(m.picker.View() + "\n" + m.styles.dim.Render("↑↓ · Enter · Esc — назад"))
		sections = append(sections, lipgloss.Place(m.width, m.contentHeight(), lipgloss.Center, lipgloss.Center, box))
	default:
		m.viewport.Height = m.contentHeight()
		sections = append(sections, m.viewport.View())
	}
	if m.running {
		sections = append(sections, m.progressView())
	}
	if m.state == stateMain {
		sections = append(sections, m.inputView())
	}
	sections = append(sections, m.barView())
	return strings.Join(sections, "\n")
}

func (m Model) inputView() string {
	if m.confirmDelete != "" {
		prompt := m.styles.selected.Render(m.icons.remove + " удалить сборку «" + m.confirmDelete + "»?  ")
		choice := m.styles.key.Render("y") + " " + m.styles.keyLabel.Render("удалить") + "    " + m.styles.dim.Render("любая другая — отмена")
		return m.styles.bar.Render(prompt + choice)
	}
	if m.commandMode {
		return m.styles.bar.Render(m.input.View())
	}
	hint := m.styles.dim.Render("› ") + m.styles.dim.Render(m.input.Placeholder+"   /")
	return m.styles.bar.Render(hint)
}

func (m Model) headerView() string {
	title := m.styles.headerName.Render(m.icons.dot + " Laminara")
	status := m.styles.good.Render("подключено")
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(status) - 2
	if gap < 1 {
		gap = 1
	}
	line := m.styles.header.Render(title + strings.Repeat(" ", gap) + status)
	rule := m.styles.dim.Render(strings.Repeat("─", max(0, m.width)))
	return line + "\n" + rule
}

func (m Model) progressView() string {
	spin := m.styles.selected.Render(spinnerFrames[m.spinFrame])
	phase := m.prog.phase
	if phase == "" {
		phase = m.runLabel
	}
	elapsed := m.styles.dim.Render("· " + fmtDur(time.Since(m.started)))
	parts := []string{spin, m.styles.keyLabel.Render(phase)}
	if m.prog.total > 0 {
		m.bar.Width = min(40, max(10, m.width/3))
		parts = append(parts, m.bar.ViewAs(float64(m.prog.current)/float64(m.prog.total)))
		parts = append(parts, m.styles.dim.Render(fmt.Sprintf("%d/%d", m.prog.current, m.prog.total)))
	}
	if m.prog.message != "" {
		parts = append(parts, m.styles.dim.Render(m.prog.message))
	}
	parts = append(parts, elapsed)
	return m.styles.bar.Render(strings.Join(parts, "  "))
}

func (m Model) barView() string {
	item := func(letter, icon, label string) string {
		return m.styles.key.Render(letter) + " " + icon + " " + m.styles.keyLabel.Render(label)
	}
	left := strings.Join([]string{
		item("i", m.icons.install, "Установить"),
		item("b", m.icons.builds, "Клиенты"),
		item("u", m.icons.update, "Обновить"),
		item("p", m.icons.publish, "Опубликовать"),
		item("d", m.icons.remove, "Удалить"),
	}, "    ")
	right := m.styles.key.Render("/") + " " + m.styles.keyLabel.Render("команда") + "    " + m.styles.dim.Render(m.icons.quit+" q выход")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 3 {
		gap = 3
	}
	return m.styles.bar.Render(left + strings.Repeat(" ", gap) + right)
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dм%02dс", int(d.Minutes()), int(d.Seconds())%60)
}

func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func waitForLog(ch chan string) tea.Cmd {
	return func() tea.Msg { return logMsg(<-ch) }
}

func waitForProgress(ch chan progressMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func streamLogs(ctx context.Context, client adminv1connect.AdminServiceClient, logCh chan string, st styles) {
	stream, err := client.StreamLogs(ctx, connect.NewRequest(&adminv1.StreamLogsRequest{Backscroll: 200, Follow: true}))
	if err != nil {
		logCh <- "logs: " + err.Error()
		return
	}
	for stream.Receive() {
		logCh <- formatLog(stream.Msg().Line, st)
	}
}

func formatLog(line *adminv1.LogLine, st styles) string {
	when := st.dim.Render(time.Unix(0, line.TimeUnixNanos).Format("15:04:05"))
	source := line.Source
	if source == "" {
		source = "server"
	}
	dot := st.dim.Render("·")
	switch line.Level {
	case adminv1.LogLevel_LOG_LEVEL_WARN:
		dot = st.warn.Render("·")
	case adminv1.LogLevel_LOG_LEVEL_ERROR:
		dot = st.warn.Render("•")
	}
	return fmt.Sprintf("%s %s %s %s", when, st.dim.Render(fmt.Sprintf("%-8s", source)), dot, line.Message)
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

func runCommand(ctx context.Context, client adminv1connect.AdminServiceClient, line string, logCh chan string, progCh chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		stream, err := client.Exec(ctx, connect.NewRequest(&adminv1.ExecRequest{Line: line}))
		if err != nil {
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
		return execDoneMsg{line: line, exitCode: code}
	}
}
