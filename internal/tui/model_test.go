package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
	"github.com/daviddwlee84/lazycred/internal/service"
)

func demo(t *testing.T) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	a := service.New(c, true, false)
	m := New(a, "bitwarden", "all")
	m.sources, _ = a.Sources(context.Background())
	rows, _ := a.Items(context.Background(), "bitwarden", "all")
	m.pane().Rows = rows
	m.pane().Selected = itemKey(rows[0])
	m.detail, _ = a.Detail(context.Background(), rows[0])
	m.updateViewport()
	return m
}
func press(s string) tea.KeyPressMsg {
	if s == "enter" {
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	if s == "tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}
func TestTypingOwnsKeysAndDoesNotQuit(t *testing.T) {
	m := demo(t)
	m.activate("unlock")
	for _, r := range "jkhql/?" {
		m.Update(press(string(r)))
	}
	if m.modal == nil || m.modal.Inputs[0].Input.Value() != "jkhql/?" {
		t.Fatal("shortcut consumed form text")
	}
	if strings.Contains(m.View().Content, "jkhql/?") {
		t.Fatal("secret form input was echoed")
	}
}
func TestDefaultReviewEnterCancels(t *testing.T) {
	m := demo(t)
	m.sourceID = "yubikey:11111111"
	m.category = "oath"
	m.activate("pin")
	for i := range m.modal.Inputs {
		switch m.modal.Inputs[i].ID {
		case "secret", "repeat":
			m.modal.Inputs[i].Input.SetValue("fixture-only")
		case "confirm":
			m.modal.Inputs[i].Input.SetValue("11111111")
		}
	}
	m.reviewForm()
	if !m.modal.Review || m.modal.Apply {
		t.Fatal("review did not default to Cancel")
	}
	cmd := m.dialogKey(press("enter"))
	if cmd != nil || m.busy || m.modal != nil {
		t.Fatal("Enter applied a default destructive action")
	}
}
func TestStaleReadsAndErrorsCannotReplaceNewView(t *testing.T) {
	m := demo(t)
	m.load()
	m.load()
	key := m.key()
	before := len(m.pane().Rows)
	m.Update(itemsMsg{Key: key, Generation: m.generations[key] - 1, Epoch: m.app.Epoch(), Rows: nil, Err: model.Fail("failed", "old")})
	if len(m.pane().Rows) != before || m.pane().Error != "" {
		t.Fatal("stale failure replaced rows")
	}
}
func TestLockDiscardsLateSecretDetails(t *testing.T) {
	m := demo(t)
	generation, epoch := m.detailGeneration, m.app.Epoch()
	item := m.detail
	m.lock()
	m.Update(detailMsg{generation, epoch, item, nil})
	if m.detail.ID != "" || m.revealedValue != "" {
		t.Fatal("late details resurrected secrets")
	}
}
func TestSelectionChangeDiscardsLateReveal(t *testing.T) {
	m := demo(t)
	cmd := m.startJob("reveal", func(context.Context) jobMsg { return jobMsg{Field: model.Field{ID: "password", Value: "fixture-only"}} })
	result := cmd().(jobMsg)
	m.modal = nil
	m.loadDetail()
	m.Update(result)
	if m.revealedValue != "" || m.busy {
		t.Fatal("late reveal exposed a previous item's secret")
	}
}
func TestSameItemIDAcrossSourcesKeepsExactSelection(t *testing.T) {
	m := demo(t)
	m.sourceID = "all"
	m.category = "all"
	m.focus = 1
	m.pane().Rows = []model.Item{{ID: "same", Source: "yubikey:11111111", Name: "A"}, {ID: "same", Source: "yubikey:22222222", Name: "B"}}
	m.pane().Selected = itemKey(m.pane().Rows[0])
	m.move(1)
	item, _ := m.selected()
	if item.Source != "yubikey:22222222" {
		t.Fatal("cross-source identity collision")
	}
}
func TestResizeAndUnicodeStayWithinTerminalCells(t *testing.T) {
	m := demo(t)
	for _, size := range [][2]int{{1, 1}, {40, 12}, {80, 24}, {140, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		for _, line := range strings.Split(view.Content, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("line wider than %d", size[0])
			}
		}
	}
}
func TestViewDoesNotAllocatePaneOrPerformIO(t *testing.T) {
	m := demo(t)
	m.sourceID = "never-loaded"
	before := len(m.panes)
	m.View()
	if len(m.panes) != before {
		t.Fatal("View mutated state")
	}
}
func TestFilterEnterLeavesInputWithoutOpeningItem(t *testing.T) {
	m := demo(t)
	m.Update(press("/"))
	for _, r := range "Example" {
		m.Update(press(string(r)))
	}
	m.Update(press("enter"))
	if m.filtering || m.modal != nil || m.focus != 1 {
		t.Fatal("filter Enter activated a row")
	}
}
func TestIdleLockAndRevealExpiry(t *testing.T) {
	m := demo(t)
	m.revealedID = "password"
	m.revealedValue = "fixture-value"
	m.revealUntil = time.Now().Add(-time.Second)
	m.Update(clockMsg(time.Now()))
	if m.revealedValue != "" {
		t.Fatal("reveal did not expire")
	}
	m.lastActivity = time.Now().Add(-10 * time.Minute)
	m.Update(clockMsg(time.Now()))
	if m.detail.ID != "" {
		t.Fatal("idle lock retained details")
	}
}
func TestReadOnlyActionsDoNotAdvertiseWrites(t *testing.T) {
	m := demo(t)
	m.app.ReadOnly = true
	m.sourceID = "yubikey:11111111"
	m.category = "oath"
	for _, a := range m.actions() {
		if (a.ID == "add" || a.ID == "delete" || a.ID == "pin" || a.ID == "transfer" || a.ID == "setup") && a.Enabled {
			t.Fatal("read-only advertised write")
		}
	}
}
