package tui

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/buildview"
)

type playersState struct {
	menu    menu
	list    []*adminv1.BuildPlayers
	updated time.Time
}

func fetchPlayers(ctx context.Context, client adminv1connect.AdminServiceClient) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.ListPlayers(ctx, connect.NewRequest(&adminv1.ListPlayersRequest{}))
		if err != nil {
			return playersMsg{err: err}
		}
		return playersMsg{list: resp.Msg.Builds}
	}
}

func playersTick() tea.Cmd {
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return playersTickMsg{} })
}

func (m Model) enterPlayers() (tea.Model, tea.Cmd) {
	m.state = statePlayers
	m.players = playersState{menu: newMenu("Кто сейчас в игре")}
	m.players.menu.empty = "Опрашивать пока некого — в проекте нет сборок."
	m.players.menu.resize(m.width, m.contentHeight()-4)
	return m, tea.Batch(fetchPlayers(m.ctx, m.client), playersTick())
}

func (m *Model) fillPlayers(msg playersMsg) {
	if msg.err != nil {
		m.players.menu.problem = errorText(msg.err)
		return
	}
	m.players.menu.problem = ""
	m.players.list = msg.list
	m.players.updated = time.Now()

	var items []menuItem
	missingAddress := false
	for _, build := range msg.list {
		value, hint := buildview.PlayersWord(build)
		tone := toneGood
		if build.Address == "" || !build.Reachable || build.Online == 0 {
			tone = toneMuted
		}
		if build.Address == "" {
			missingAddress = true
		}
		row := menuItem{
			id:     "build:" + build.Build,
			label:  build.Build,
			value:  value,
			note:   hint,
			tone:   tone,
			static: true,
		}
		if build.Address == "" {
			row.note = buildview.AddressHint
		}
		items = append(items, row)
		for _, name := range build.Names {
			items = append(items, menuItem{
				id:    "player:" + name,
				label: "  " + name,
				hint:  "Enter — забанить игрока на сборке «" + build.Build + "»",
				tone:  toneNormal,
			})
		}
	}
	_ = missingAddress
	m.players.menu.subtitle = "обновлено " + m.players.updated.Format("15:04:05") + " · обновляется само каждые 10 с"
	m.players.menu.setItems(items)
}

func (m Model) updatePlayers(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, event := m.players.menu.Update(msg)
	m.players.menu = updated
	kind, value := splitID(event.id)

	switch event.kind {
	case menuOpened:
		if kind != "player" {
			return m, nil
		}
		m.state = stateMain
		m.confirm = confirmPrompt{
			question: "забанить игрока «" + value + "»?",
			verb:     "забанить",
			command:  "ban " + value,
			icon:     m.icons.remove,
		}
		return m, nil
	case menuPressed:
		switch event.key {
		case "esc", "q", "l":
			m.state = stateMain
			return m, nil
		case "r":
			return m, fetchPlayers(m.ctx, m.client)
		}
	}
	return m, nil
}

func (m Model) playersView() string {
	return m.centered(m.players.menu.View(m.styles, m.icons))
}

func (m Model) playersBar() string {
	item := func(letter, label string) string {
		return m.styles.key.Render(letter) + " " + m.styles.keyLabel.Render(label)
	}
	return m.styles.bar.Render(strings.Join([]string{
		item("↑↓", "выбрать игрока"),
		item("Enter", "забанить"),
		item("r", "обновить"),
		item("Esc", "назад"),
	}, "   "))
}

type cardState struct {
	menu    menu
	build   *adminv1.BuildInfo
	players *adminv1.BuildPlayers
}

func findBuild(builds []*adminv1.BuildInfo, name string) *adminv1.BuildInfo {
	for _, build := range builds {
		if build.Name == name {
			return build
		}
	}
	return nil
}

func pickPlayers(list []*adminv1.BuildPlayers, name string) *adminv1.BuildPlayers {
	for _, entry := range list {
		if entry.Build == name {
			return entry
		}
	}
	return nil
}

func (m *Model) fillCard() {
	build := m.card.build
	if build == nil {
		return
	}
	m.card.menu.title = "Сборка «" + build.Name + "»"
	m.card.menu.subtitle = buildview.StatusWord(build.Status)
	var items []menuItem
	for _, field := range buildview.Fields(build, m.card.players) {
		items = append(items, menuItem{
			id:     "field:" + field.Label,
			label:  field.Label,
			value:  field.Value,
			note:   field.Hint,
			tone:   toneNormal,
			static: true,
		})
	}
	if build.Status == "prepared" {
		items = append(items, menuItem{
			id:     "note",
			label:  "Лаунчеры её ещё не видят",
			value:  "опубликуйте клавишей p",
			tone:   toneWarn,
			static: true,
		})
	}
	m.card.menu.setItems(items)
}

func (m Model) updateCard(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, event := m.card.menu.Update(msg)
	m.card.menu = updated
	if event.kind != menuPressed {
		return m, nil
	}
	name := ""
	if m.card.build != nil {
		name = m.card.build.Name
	}
	switch event.key {
	case "esc", "q", "b":
		m.state = stateMain
		return m, nil
	case "p":
		m.state = stateMain
		return m.startCommand("publish " + name)
	case "u":
		wiz, cmd := newWizard(m.ctx, m.client, m.icons, m.styles, name)
		m.wizard = wiz
		m.state = stateWizard
		return m, cmd
	case "d":
		m.state = stateMain
		m.confirm = deleteConfirm(name, m.icons)
		return m, nil
	}
	return m, nil
}

func (m Model) cardView() string {
	return m.centered(m.card.menu.View(m.styles, m.icons))
}
