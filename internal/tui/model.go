package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/daviddwlee84/lazycred/internal/model"
	"github.com/daviddwlee84/lazycred/internal/service"
	"golang.org/x/term"
)

type pane struct {
	Rows     []model.Item
	Selected string
	Query    string
	Offset   int
	Loading  bool
	Error    string
	Updated  time.Time
}

func itemKey(item model.Item) string { return item.Source + "\x00" + item.ID }

type sourcesMsg struct {
	Generation uint64
	Rows       []model.Source
	Err        error
}
type itemsMsg struct {
	Key        string
	Generation uint64
	Epoch      uint64
	Rows       []model.Item
	Err        error
}
type detailMsg struct {
	Generation uint64
	Epoch      uint64
	Item       model.Item
	Err        error
}
type clockMsg time.Time
type jobMsg struct {
	ID               uint64
	DetailGeneration uint64
	Epoch            uint64
	Kind             string
	Message          string
	Field            model.Field
	Rows             []model.Item
	Err              error
}
type inputField struct {
	ID, Label string
	Secret    bool
	Input     textinput.Model
}
type dialog struct {
	Kind, Title, Action string
	Options             []Action
	Selection           int
	Inputs              []inputField
	Focus               int
	Review              bool
	Apply               bool
	Item                model.Item
	Source, Category    string
	Text, Error         string
}

type Model struct {
	app                       *service.App
	ctx                       context.Context
	width, height             int
	sourceID, category        string
	focus                     int
	sources                   []model.Source
	sourcesGeneration         uint64
	panes                     map[string]*pane
	generations               map[string]uint64
	detail                    model.Item
	detailGeneration          uint64
	viewport                  viewport.Model
	filter                    textinput.Model
	filtering                 bool
	modal                     *dialog
	status                    string
	lastActivity              time.Time
	busy                      bool
	jobID                     uint64
	cancel                    context.CancelFunc
	revealedID, revealedValue string
	revealUntil               time.Time
	mouse                     bool
	pendingG                  bool
}

func New(app *service.App, source, category string) *Model {
	if source == "" {
		source = "overview"
	}
	if category == "" {
		category = "status"
	}
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "Filter names, kind or username"
	filter.CharLimit = 256
	return &Model{app: app, ctx: context.Background(), width: 100, height: 28, sourceID: source, category: category, panes: map[string]*pane{}, generations: map[string]uint64{}, viewport: viewport.New(), filter: filter, lastActivity: time.Now(), status: "Discovering local sources…", mouse: true}
}
func (m *Model) key() string { return m.sourceID + "/" + m.category }
func (m *Model) pane() *pane {
	key := m.key()
	if m.panes[key] == nil {
		m.panes[key] = &pane{}
	}
	return m.panes[key]
}
func (m *Model) source() model.Source {
	for _, s := range m.sources {
		if s.ID == m.sourceID {
			return s
		}
	}
	return model.Source{ID: m.sourceID, Kind: "unknown", Name: m.sourceID}
}
func (m *Model) filtered() []model.Item {
	var rows []model.Item
	p := m.panes[m.key()]
	if p == nil {
		return nil
	}
	query := strings.ToLower(p.Query)
	for _, r := range p.Rows {
		if query == "" || strings.Contains(strings.ToLower(r.Name+" "+r.Subtitle+" "+r.Kind+" "+r.Source), query) {
			rows = append(rows, r)
		}
	}
	return rows
}
func (m *Model) selected() (model.Item, bool) {
	rows := m.filtered()
	if len(rows) == 0 {
		return model.Item{}, false
	}
	for _, r := range rows {
		if itemKey(r) == m.pane().Selected {
			return r, true
		}
	}
	return rows[0], true
}
func (m *Model) discovery() tea.Cmd {
	m.sourcesGeneration++
	generation := m.sourcesGeneration
	ctx, app := m.ctx, m.app
	return func() tea.Msg { rows, err := app.Sources(ctx); return sourcesMsg{generation, rows, err} }
}
func (m *Model) load() tea.Cmd {
	m.detailGeneration++
	m.detail = model.Item{}
	m.revealedID, m.revealedValue = "", ""
	m.updateViewport()
	key := m.key()
	m.generations[key]++
	generation := m.generations[key]
	m.pane().Loading = true
	source, category, epoch, ctx, app := m.sourceID, m.category, m.app.Epoch(), m.ctx, m.app
	return func() tea.Msg {
		rows, err := app.Items(ctx, source, category)
		return itemsMsg{key, generation, epoch, rows, err}
	}
}
func (m *Model) loadDetail() tea.Cmd {
	m.detailGeneration++
	generation := m.detailGeneration
	m.detail = model.Item{}
	m.revealedID = ""
	m.revealedValue = ""
	m.viewport.GotoTop()
	m.updateViewport()
	item, ok := m.selected()
	if !ok {
		return nil
	}
	ctx, app, epoch := m.ctx, m.app, m.app.Epoch()
	return func() tea.Msg { detail, err := app.Detail(ctx, item); return detailMsg{generation, epoch, detail, err} }
}
func tick() tea.Cmd            { return tea.Tick(time.Second, func(t time.Time) tea.Msg { return clockMsg(t) }) }
func (m *Model) Init() tea.Cmd { return tea.Batch(m.discovery(), tick()) }
func (m *Model) startJob(kind string, work func(context.Context) jobMsg) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	m.jobID++
	id := m.jobID
	detailGeneration, epoch := m.detailGeneration, m.app.Epoch()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.status = "Working: " + kind + " · hardware touch may be required"
	m.modal = &dialog{Kind: "waiting", Title: kind, Text: "Working…\nTouch the selected key if required.\nEsc cancels waiting; a write may already have completed."}
	return func() tea.Msg {
		defer cancel()
		result := work(ctx)
		result.ID, result.Kind = id, kind
		result.DetailGeneration, result.Epoch = detailGeneration, epoch
		if ctx.Err() != nil && kind == "reveal" {
			result.Field = model.Field{}
			result.Err = model.Fail("cancelled", "Reveal cancelled.")
		}
		return result
	}
}
func (m *Model) move(delta int) tea.Cmd {
	if m.focus == 0 {
		index := 0
		for i, s := range m.sources {
			if s.ID == m.sourceID {
				index = i
			}
		}
		index = max(0, min(len(m.sources)-1, index+delta))
		if len(m.sources) == 0 {
			return nil
		}
		m.sourceID = m.sources[index].ID
		m.category = "status"
		m.filtering = false
		m.filter.SetValue(m.pane().Query)
		return tea.Batch(m.load(), m.loadDetail())
	}
	if m.focus == 2 {
		if delta > 0 {
			m.viewport.ScrollDown(1)
		} else {
			m.viewport.ScrollUp(1)
		}
		return nil
	}
	rows := m.filtered()
	if len(rows) == 0 {
		return nil
	}
	index := 0
	for i, r := range rows {
		if itemKey(r) == m.pane().Selected {
			index = i
		}
	}
	index = max(0, min(len(rows)-1, index+delta))
	m.pane().Selected = itemKey(rows[index])
	return m.loadDetail()
}
func (m *Model) lock() {
	m.app.Lock()
	m.jobID++
	if m.cancel != nil {
		m.cancel()
	}
	m.busy = false
	for key, p := range m.panes {
		m.generations[key]++
		p.Loading = false
		if strings.HasPrefix(key, "bitwarden/") || strings.HasPrefix(key, "all/") {
			p.Rows = nil
			p.Error = "Locked; refresh after unlocking."
		}
	}
	m.detail = model.Item{}
	m.detailGeneration++
	m.modal = nil
	m.revealedID = ""
	m.revealedValue = ""
	m.status = "Locked local credentials; other applications were not logged out."
	m.updateViewport()
}
func (m *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		m.filter.SetWidth(max(8, m.width-12))
		m.updateViewport()
		return m, nil
	case sourcesMsg:
		if msg.Generation != m.sourcesGeneration {
			return m, nil
		}
		if msg.Err != nil {
			m.status = msg.Err.Error()
			return m, nil
		}
		m.sources = msg.Rows
		found := false
		for _, s := range m.sources {
			if s.ID == m.sourceID {
				found = true
			}
		}
		if !found && len(m.sources) > 0 {
			m.sourceID = m.sources[0].ID
			m.category = "status"
		}
		m.status = "Local sources discovered · refresh explicitly to reconcile external changes"
		return m, m.load()
	case itemsMsg:
		if msg.Generation != m.generations[msg.Key] || msg.Epoch != m.app.Epoch() {
			return m, nil
		}
		p := m.panes[msg.Key]
		if p == nil {
			return m, nil
		}
		p.Loading = false
		if msg.Err != nil {
			p.Error = msg.Err.Error()
			if msg.Key == m.key() {
				m.status = p.Error
			}
			return m, nil
		}
		p.Rows = msg.Rows
		p.Error = ""
		p.Updated = time.Now()
		found := false
		for _, i := range p.Rows {
			if itemKey(i) == p.Selected {
				found = true
			}
		}
		if !found {
			p.Selected = ""
			if len(p.Rows) > 0 {
				p.Selected = itemKey(p.Rows[0])
			}
		}
		if msg.Key == m.key() {
			return m, m.loadDetail()
		}
		return m, nil
	case detailMsg:
		if msg.Generation != m.detailGeneration || msg.Epoch != m.app.Epoch() {
			return m, nil
		}
		if msg.Err != nil {
			m.status = msg.Err.Error()
			return m, nil
		}
		selected, ok := m.selected()
		if !ok || selected.ID != msg.Item.ID || selected.Source != msg.Item.Source {
			return m, nil
		}
		m.detail = msg.Item
		m.updateViewport()
		return m, nil
	case clockMsg:
		now := time.Time(msg)
		if now.Sub(m.lastActivity) >= m.app.Config.Idle() {
			m.lock()
			m.lastActivity = now
		}
		if m.revealedID != "" && now.After(m.revealUntil) {
			m.revealedID = ""
			m.revealedValue = ""
			m.updateViewport()
		}
		return m, tick()
	case jobMsg:
		if msg.ID != m.jobID || msg.Epoch != m.app.Epoch() {
			return m, nil
		}
		m.busy = false
		m.cancel = nil
		text := msg.Message
		if msg.Err != nil {
			text = msg.Err.Error()
		}
		m.status = text
		if msg.Kind == "reveal" && msg.Err == nil {
			if msg.DetailGeneration != m.detailGeneration {
				m.status = "Selection changed; late reveal discarded."
				return m, nil
			}
			m.revealedID = msg.Field.ID
			m.revealedValue = msg.Field.Value
			m.revealUntil = time.Now().Add(time.Duration(m.app.Config.RevealSeconds) * time.Second)
			m.modal = nil
			m.updateViewport()
			return m, nil
		}
		if m.modal != nil && m.modal.Kind == "waiting" {
			m.modal = &dialog{Kind: "result", Title: "Operation result", Text: text}
		}
		if msg.Kind == "unlock" && msg.Err == nil {
			return m, tea.Batch(m.discovery(), m.load())
		}
		if msg.Kind == "manage" || msg.Kind == "transfer" || msg.Kind == "refresh" || msg.Kind == "setup" {
			return m, tea.Batch(m.discovery(), m.load())
		}
		return m, nil
	case tea.MouseMsg:
		m.lastActivity = time.Now()
		if m.modal != nil || m.filtering {
			return m, nil
		}
		mouse := msg.Mouse()
		if _, ok := msg.(tea.MouseWheelMsg); ok {
			l := m.layout()
			if l.sources > 0 && mouse.X < l.sources {
				m.focus = 0
			} else if l.details > 0 && mouse.X >= l.sources+l.items {
				m.focus = 2
			} else {
				m.focus = 1
			}
			delta := 1
			if mouse.Button == tea.MouseWheelUp {
				delta = -1
			}
			return m, m.move(delta)
		}
		if _, ok := msg.(tea.MouseClickMsg); ok {
			layout := m.layout()
			y := mouse.Y - 3
			if layout.sources > 0 && mouse.X < layout.sources {
				if y >= 0 && y < len(m.sources) {
					m.focus = 0
					m.sourceID = m.sources[y].ID
					m.category = "status"
					return m, m.load()
				}
			} else if mouse.X < layout.sources+layout.items {
				m.focus = 1
				rows := m.filtered()
				index := y + m.listOffset()
				if index >= 0 && index < len(rows) {
					m.pane().Selected = itemKey(rows[index])
					return m, m.loadDetail()
				}
			} else {
				m.focus = 2
			}
		}
		return m, nil
	case tea.KeyPressMsg:
		m.lastActivity = time.Now()
		if m.modal != nil {
			return m, m.dialogKey(msg)
		}
		if m.filtering {
			return m, m.filterKey(msg)
		}
		key := msg.String()
		switch key {
		case "ctrl+c":
			if m.busy && m.cancel != nil {
				m.cancel()
				m.status = "Cancelled waiting; inspect any write before retrying."
				return m, nil
			}
			m.app.Lock()
			return m, tea.Quit
		case "q":
			if m.busy {
				m.status = "Operation pending; Ctrl+C cancels waiting first."
				return m, nil
			}
			m.app.Lock()
			return m, tea.Quit
		case "up", "k":
			return m, m.move(-1)
		case "down", "j":
			return m, m.move(1)
		case "pgdown", "ctrl+d":
			return m, m.move(max(1, (m.height-7)/2))
		case "pgup", "ctrl+u":
			return m, m.move(-max(1, (m.height-7)/2))
		case "tab", "right", "l":
			m.focus = (m.focus + 1) % 3
			return m, nil
		case "shift+tab", "left", "h":
			m.focus = (m.focus + 2) % 3
			return m, nil
		case "g":
			if m.pendingG {
				m.pendingG = false
				return m, m.move(-100000)
			}
			m.pendingG = true
			return m, nil
		case "G", "end":
			m.pendingG = false
			return m, m.move(100000)
		case "home":
			return m, m.move(-100000)
		case "/":
			m.filtering = true
			m.filter.SetValue(m.pane().Query)
			return m, m.filter.Focus()
		case "esc":
			if m.focus == 2 {
				m.focus = 1
			}
			return m, nil
		case "enter":
			if item, ok := m.selected(); ok && m.sourceID == "overview" {
				m.sourceID = item.ID
				m.category = "status"
				m.focus = 1
				return m, m.load()
			}
			m.focus = 2
			if m.width < 110 {
				m.modal = &dialog{Kind: "detail", Title: "Details"}
			}
			return m, nil
		}
		m.pendingG = false
		for _, action := range m.actions() {
			if action.Key == key && action.Enabled {
				return m, m.activate(action.ID)
			}
		}
	case tea.PasteMsg:
		if m.modal != nil {
			return m, m.updateInputs(message)
		}
		if m.filtering {
			return m, m.filterInput(message)
		}
	}
	return m, nil
}
func (m *Model) filterInput(msg tea.Msg) tea.Cmd {
	previous := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.pane().Query = m.filter.Value()
	if previous != m.filter.Value() {
		m.pane().Selected = ""
		m.pane().Offset = 0
		return tea.Batch(cmd, m.loadDetail())
	}
	return cmd
}
func (m *Model) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.pane().Query = ""
		return m.loadDetail()
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.focus = 1
		return nil
	case "up":
		focus := m.focus
		m.focus = 1
		cmd := m.move(-1)
		m.focus = focus
		return cmd
	case "down":
		focus := m.focus
		m.focus = 1
		cmd := m.move(1)
		m.focus = focus
		return cmd
	}
	return m.filterInput(msg)
}
func Run(ctx context.Context, app *service.App, source, category string) error {
	// Preserve the entry state across renderer/input-reader teardown as well as
	// normal quit, signals and initialization failures.
	if state, err := term.GetState(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), state)
	}
	m := New(app, source, category)
	m.ctx = ctx
	defer app.Lock()
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}
func (m *Model) debugState() string { return fmt.Sprintf("%s/%s/%d", m.sourceID, m.category, m.focus) }
