package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazycred/internal/model"
)

var (
	accent        = lipgloss.NewStyle().Foreground(lipgloss.Color("#5EEAD4")).Bold(true)
	muted         = lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F0FDFA")).Background(lipgloss.Color("#164E63")).Bold(true)
	warning       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FCD34D"))
	danger        = lipgloss.NewStyle().Foreground(lipgloss.Color("#FDA4AF"))
)

type layout struct{ sources, items, details, height int }

func (m *Model) layout() layout {
	h := max(3, m.height-5)
	if m.width >= 110 {
		left := 26
		middle := max(34, min(45, m.width/3))
		return layout{left, middle, m.width - left - middle, h}
	}
	if m.width >= 70 {
		return layout{23, m.width - 23, 0, h}
	}
	return layout{0, m.width, 0, h}
}
func crop(s string, width int) string { return ansi.Truncate(s, max(1, width), "…") }
func linesFit(s string, width, height int) string {
	if width < 1 || height < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = crop(lines[i], width)
	}
	return strings.Join(lines, "\n")
}
func frame(title, body string, width, height int, focused bool) string {
	border := lipgloss.Color("#334155")
	if focused {
		border = lipgloss.Color("#5EEAD4")
	}
	content := accent.Render(crop(title, max(1, width-4))) + "\n" + linesFit(body, max(1, width-4), max(1, height-3))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(max(1, width)).Height(max(1, height)).Render(content)
}
func (m *Model) listOffset() int {
	rows := m.filtered()
	visible := max(1, m.layout().height-3)
	index := 0
	for i, r := range rows {
		if itemKey(r) == m.panes[m.key()].Selected {
			index = i
		}
	}
	offset := 0
	if p := m.panes[m.key()]; p != nil {
		offset = p.Offset
	}
	if index < offset {
		offset = index
	}
	if index >= offset+visible {
		offset = index - visible + 1
	}
	return max(0, min(offset, max(0, len(rows)-visible)))
}
func (m *Model) sourceView() string {
	var lines []string
	for _, source := range m.sources {
		label := "  " + model.Safe(source.Name)
		if source.ID == m.sourceID {
			label = selectedStyle.Render("› " + model.Safe(source.Name))
		}
		lines = append(lines, label)
	}
	if len(lines) == 0 {
		lines = append(lines, muted.Render("Discovering…"))
	}
	return strings.Join(lines, "\n")
}
func (m *Model) itemView() string {
	p := m.panes[m.key()]
	if p == nil {
		return muted.Render("Loading…")
	}
	rows := m.filtered()
	if len(rows) == 0 {
		if p.Loading {
			return muted.Render("Loading…\nNavigation remains available.")
		}
		if p.Error != "" {
			var hints []string
			for _, a := range m.actions() {
				if a.Enabled && (a.ID == "unlock" || a.ID == "setup" || a.ID == "refresh") {
					hints = append(hints, a.Key+" "+a.Label)
				}
			}
			return danger.Render(ansi.Wrap(model.Safe(p.Error), max(10, m.layout().items-5), "")) + "\n\n" + strings.Join(hints, " · ")
		}
		if p.Query != "" {
			return "No matching items.\nEsc from filter clears the query."
		}
		if m.source().Kind == "yubikey" && m.category == "oath" && !m.app.ReadOnly {
			return "No OATH accounts.\na Add TOTP · c Category · : Actions"
		}
		return "No items in this category.\nc Choose category · : Actions"
	}
	var lines []string
	offset := m.listOffset()
	height := max(1, m.layout().height-3)
	selected, _ := m.selected()
	for _, item := range rows[offset:min(len(rows), offset+height)] {
		label := "  " + model.Safe(item.Name)
		if itemKey(item) == itemKey(selected) {
			label = "› " + model.Safe(item.Name)
		}
		suffix := ""
		if item.State != "" && item.State != "ready" {
			suffix = " [" + model.Safe(item.State) + "]"
		}
		label = crop(label+suffix, max(1, m.layout().items-5))
		if itemKey(item) == itemKey(selected) {
			label = selectedStyle.Render(label)
		}
		lines = append(lines, label)
	}
	return strings.Join(lines, "\n")
}
func (m *Model) detailText(width int) string {
	if m.detail.ID == "" {
		return muted.Render("Select an item to inspect.\nSecrets stay masked by default.")
	}
	var lines []string
	lines = append(lines, accent.Render(model.Safe(m.detail.Name)), muted.Render(model.Safe(m.detail.Source)+" · "+model.Safe(m.detail.Kind)+" · "+model.Safe(m.detail.State)), "")
	for _, field := range m.detail.Fields {
		value := field.Value
		if field.Secret {
			value = "••••••••"
			if field.ID == "otp" {
				value = "Generate on demand"
			}
			if field.ID == m.revealedID {
				value = m.revealedValue
			}
		}
		lines = append(lines, muted.Render(model.Safe(field.Label)), ansi.Hardwrap(model.Safe(value), max(8, width), false), "")
	}
	if len(m.detail.Fields) == 0 {
		lines = append(lines, "No readable fields.")
	}
	return strings.Join(lines, "\n")
}
func (m *Model) updateViewport() {
	l := m.layout()
	width := l.details
	if width == 0 {
		width = m.width
	}
	m.viewport.SetWidth(max(8, width-5))
	m.viewport.SetHeight(max(2, l.height-3))
	m.viewport.SetContent(m.detailText(max(8, width-5)))
}
func (m *Model) footer() string {
	var hints []string
	for _, a := range m.actions() {
		if !a.Enabled {
			continue
		}
		switch a.ID {
		case "copy", "actions", "help", "refresh", "unlock", "category", "add":
			hints = append(hints, a.Key+" "+a.Label)
		}
	}
	return muted.Render(crop("↑↓/jk Select · Tab Focus · / Filter · "+strings.Join(hints, " · ")+" · q Quit", m.width))
}
func (m *Model) modalView() string {
	d := m.modal
	w := max(10, m.width-8)
	h := max(3, m.height-9)
	var lines []string
	switch d.Kind {
	case "form":
		lines = append(lines, muted.Render("Target: "+model.Safe(d.Source)+" · "+model.Safe(d.Item.Name)+" · "+model.Safe(d.Item.ID)), "")
		if d.Review {
			for _, f := range d.Inputs {
				value := f.Input.Value()
				if f.Secret && value != "" {
					value = "[entered; not displayed]"
				}
				lines = append(lines, model.Safe(f.Label)+": "+model.Safe(value))
			}
			lines = append(lines, "", warning.Render("Review exact source and destination. Completed writes are not automatically rolled back."), "", choice(d.Apply), muted.Render("←/→ choose · Enter submit · Backspace edit · Esc cancel"))
		} else {
			start := max(0, d.Focus-max(1, (h-7)/3)+1)
			for i := start; i < len(d.Inputs); i++ {
				f := d.Inputs[i]
				label := model.Safe(f.Label)
				if i == d.Focus {
					label = accent.Render("› " + label)
				}
				lines = append(lines, label, f.Input.View(), "")
			}
			lines = append(lines, muted.Render("Tab/Enter next · Ctrl+S review · Esc cancel"))
		}
		if d.Error != "" {
			lines = append([]string{danger.Render(d.Error), ""}, lines...)
		}
	case "help":
		lines = []string{"See → select → act → see the result", "", "↑↓ / j,k   Navigate the focused pane", "Tab / h,l   Move pane focus", "Enter       Inspect; Overview opens the source", "/           Filter; text owns printable keys", "gg / G      First / last item", "Esc         Back/cancel nearest interaction", "Ctrl+C      Cancel pending work, then exit", "", "Current actions:"}
		for _, a := range m.actions() {
			if a.Enabled {
				lines = append(lines, fmt.Sprintf("%-4s %s", a.Key, a.Label))
			}
		}
		lines = append(lines, "", "Secret fields: v reveals briefly; y copies explicitly.", "Copy cleanup preserves newer clipboard content.", "Read-only blocks management and HOTP counters.", "FIDO lists discoverable credentials only.", "Raw private keys / OATH seeds on hardware are not exportable.")
		offset := max(0, min(d.Selection, max(0, len(lines)-h)))
		lines = lines[offset:]
		lines = append(lines, "Esc / Enter / q Back")
	case "detail":
		return frame(d.Title, m.viewport.View()+"\n"+muted.Render("↑↓ scroll · y Copy · v Reveal · Esc Back"), m.width-4, m.height-5, true)
	case "setup-review":
		lines = []string{d.Text, "", choice(d.Apply)}
	case "waiting", "result":
		lines = []string{ansi.Hardwrap(model.Safe(d.Text), w, false), "", muted.Render("Esc / Enter Back")}
	default:
		visible := max(1, h-3)
		offset := max(0, d.Selection-visible+1)
		for i := offset; i < min(len(d.Options), offset+visible); i++ {
			option := d.Options[i]
			line := "  " + model.Safe(option.Label)
			if i == d.Selection {
				line = selectedStyle.Render("› " + model.Safe(option.Label))
			}
			lines = append(lines, line)
		}
		if len(d.Options) == 0 {
			lines = append(lines, "No available actions.")
		}
		lines = append(lines, "", muted.Render("↑↓/jk Select · Enter choose · Esc cancel"))
	}
	return frame(d.Title, strings.Join(lines, "\n"), m.width-4, m.height-5, true)
}
func choice(apply bool) string {
	if apply {
		return "  Cancel     " + selectedStyle.Render(" Apply ")
	}
	return selectedStyle.Render(" Cancel ") + "    Apply"
}
func (m *Model) View() tea.View {
	mode := ""
	if m.app.Demo {
		mode = warning.Render(" DEMO · fictional data ")
	}
	if m.app.ReadOnly {
		mode += " READ ONLY"
	}
	header := accent.Render(" lazycred ") + mode + muted.Render("  credential workbench")
	source := m.source()
	contextLine := crop(" "+model.Safe(source.Name)+"  /  "+model.Safe(m.category)+"  ·  "+model.Safe(source.State), m.width)
	var body string
	if m.modal != nil {
		body = m.modalView()
	} else {
		l := m.layout()
		itemsTitle := "Items · " + m.category + fmt.Sprintf(" (%d)", len(m.filtered()))
		if p := m.panes[m.key()]; p != nil {
			if p.Loading {
				itemsTitle += " · loading"
			} else if p.Error != "" {
				itemsTitle += " · stale/error"
			} else if !p.Updated.IsZero() {
				itemsTitle += " · " + p.Updated.Format("15:04:05")
			}
		}
		if l.sources == 0 {
			if m.focus == 2 {
				body = frame("Details", m.viewport.View(), l.items, l.height, true)
			} else {
				body = frame(itemsTitle, m.itemView(), l.items, l.height, true)
			}
		} else if l.details == 0 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, frame("Sources", m.sourceView(), l.sources, l.height, m.focus == 0), frame(itemsTitle, m.itemView(), l.items, l.height, m.focus == 1))
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top, frame("Sources", m.sourceView(), l.sources, l.height, m.focus == 0), frame(itemsTitle, m.itemView(), l.items, l.height, m.focus == 1), frame("Details", m.viewport.View(), l.details, l.height, m.focus == 2))
		}
	}
	status := model.Safe(m.status)
	if m.revealedID != "" {
		status = fmt.Sprintf("Secret visible for %ds · ", max(0, int(time.Until(m.revealUntil).Seconds()))) + status
	}
	filterLine := ""
	if m.filtering {
		filterLine = m.filter.View() + "\n"
	}
	content := header + "\n" + contextLine + "\n" + body + "\n" + filterLine + muted.Render(crop(status, m.width)) + "\n" + m.footer()
	content = linesFit(content, m.width, m.height)
	view := tea.NewView(content)
	view.AltScreen = true
	if m.mouse {
		view.MouseMode = tea.MouseModeCellMotion
	}
	return view
}
