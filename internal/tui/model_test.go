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

func TestRefreshRecoversExactReaderWithoutRestart(t *testing.T) {
	m := demo(t)
	reader := model.Source{ID: "reader:Yubico Demo CCID 0", Kind: "reader", Name: "Demo reader", State: "unavailable"}
	m.sources = []model.Source{{ID: "overview", Kind: "overview"}, reader}
	m.sourceID, m.category = reader.ID, "status"
	m.pane().Error = "Reader unavailable"
	oldGeneration := m.generations[m.key()]
	command := m.activate("refresh")
	if command == nil {
		t.Fatal("refresh did not discover sources")
	}
	// A read that started before refresh must not overwrite the recovery.
	m.Update(itemsMsg{Key: m.key(), Generation: oldGeneration, Epoch: m.app.Epoch(), Err: model.Fail("unavailable", "late reader failure")})
	if m.pane().Error == "late reader failure" {
		t.Fatal("old error survived refresh")
	}
	key := model.Source{ID: "yubikey:22222222", Kind: "yubikey", Name: "Recovered key", State: "ready", ReaderIDs: []string{reader.ID}}
	unrelated := model.Source{ID: "yubikey:11111111", Kind: "yubikey", Name: "Other key", State: "ready"}
	_, read := m.Update(sourcesMsg{Generation: m.sourcesGeneration, Rows: []model.Source{{ID: "overview", Kind: "overview"}, unrelated, key}})
	if m.sourceID != key.ID || m.category != "status" || read == nil {
		t.Fatal("refresh did not follow the exact reader to its key")
	}
	items := read().(itemsMsg)
	_, detail := m.Update(items)
	if detail == nil {
		t.Fatal("recovery did not load details")
	}
	m.Update(detail())
	if m.detail.ID == "" || m.detail.Source != key.ID || m.pane().Error != "" {
		t.Fatal("recovered details were not displayed")
	}
}

func TestRefreshNeverGuessesReplacementKey(t *testing.T) {
	m := demo(t)
	reader := model.Source{ID: "reader:Yubico Demo CCID 0", Kind: "reader"}
	m.sources = []model.Source{{ID: "overview", Kind: "overview"}, reader}
	m.sourceID = reader.ID
	m.discovery()
	m.Update(sourcesMsg{Generation: m.sourcesGeneration, Rows: []model.Source{{ID: "overview", Kind: "overview"}, {ID: "yubikey:11111111", Kind: "yubikey"}}})
	if m.sourceID != "overview" {
		t.Fatal("unassociated key selected automatically")
	}
}

func TestRefreshKeepsCategoryAndSelectionForSameKey(t *testing.T) {
	m := demo(t)
	m.sourceID, m.category = "yubikey:11111111", "oath"
	rows, _ := m.app.Items(context.Background(), m.sourceID, m.category)
	m.pane().Rows, m.pane().Selected, m.pane().Query = rows, itemKey(rows[0]), "Example"
	m.discovery()
	_, read := m.Update(sourcesMsg{Generation: m.sourcesGeneration, Rows: m.sources})
	m.Update(read())
	if m.category != "oath" || m.pane().Query != "Example" || m.pane().Selected != itemKey(rows[0]) {
		t.Fatal("refresh lost per-key browsing context")
	}
}

func TestEjectedKeyReturnsToItsReader(t *testing.T) {
	m := demo(t)
	readerID := "reader:Yubico Demo CCID 0"
	key := model.Source{ID: "yubikey:11111111", Kind: "yubikey", ReaderIDs: []string{readerID}}
	m.sources, m.sourceID, m.category = []model.Source{key}, key.ID, "oath"
	m.discovery()
	m.Update(sourcesMsg{Generation: m.sourcesGeneration, Rows: []model.Source{{ID: "overview", Kind: "overview"}, {ID: readerID, Kind: "reader", State: "unavailable"}}})
	if m.sourceID != readerID || m.category != "status" {
		t.Fatal("ejected key lost its reader context")
	}
}

func TestFrameUsesOuterDimensionsWithoutWrappingSourceRows(t *testing.T) {
	view := frame("Sources", "› A very long fictional reader name\n  Bitwarden", 26, 10, true)
	lines := strings.Split(ansi.Strip(view), "\n")
	if len(lines) != 10 {
		t.Fatalf("frame height = %d", len(lines))
	}
	for _, line := range lines {
		if ansi.StringWidth(line) != 26 {
			t.Fatalf("frame width = %d", ansi.StringWidth(line))
		}
	}
	if !strings.Contains(lines[3], "Bitwarden") {
		t.Fatal("long source name wrapped into the next row")
	}
}

func TestUnavailableReaderDoesNotOfferUnlock(t *testing.T) {
	m := demo(t)
	m.sources = []model.Source{{ID: "reader:Demo", Kind: "reader"}}
	m.sourceID = "reader:Demo"
	m.pane().Error = "Reader unavailable"
	if strings.Contains(m.itemView(), "u Unlock") {
		t.Fatal("reader error offered a PIN action")
	}
}
