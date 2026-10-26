package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daviddwlee84/lazycred/internal/config"
)

type memoryClipboard struct {
	value         string
	reads, writes int
	afterRead     func()
}

func (m *memoryClipboard) Read(context.Context) (string, error) {
	m.reads++
	if m.afterRead != nil {
		m.afterRead()
	}
	return m.value, nil
}
func (m *memoryClipboard) Write(_ context.Context, value string) error {
	m.writes++
	m.value = value
	return nil
}

func TestClipboardClearPreservesNewContentAndOwner(t *testing.T) {
	for _, scenario := range []string{"owned", "new-content", "new-owner", "owner-changed-during-read"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			path := filepath.Join(config.Dir("cache"), "clipboard", "owner")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			token := strings.Repeat("a", 32)
			if err := os.WriteFile(path, []byte(token), 0600); err != nil {
				t.Fatal(err)
			}
			clip := &memoryClipboard{value: "fixture-only"}
			switch scenario {
			case "new-content":
				clip.value = "newer copy"
			case "new-owner":
				os.WriteFile(path, []byte(strings.Repeat("b", 32)), 0600)
			case "owner-changed-during-read":
				clip.afterRead = func() { os.WriteFile(path, []byte(strings.Repeat("b", 32)), 0600) }
			}
			if err := clearOwnedNow(context.Background(), clip, ClearRequest{Value: "fixture-only", Marker: path, Token: token}); err != nil {
				t.Fatal(err)
			}
			if scenario == "owned" && (clip.writes != 1 || clip.value != "") {
				t.Fatal("owned content not cleared")
			}
			if scenario != "owned" && clip.writes != 0 {
				t.Fatal("overwrote newer clipboard ownership/content")
			}
		})
	}
}

func TestClipboardCancelledTimerDoesNotRead(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	clip := &memoryClipboard{}
	request := ClearRequest{Seconds: 30, Marker: filepath.Join(config.Dir("cache"), "clipboard", "owner"), Token: strings.Repeat("a", 32)}
	if err := ClearIfOwned(ctx, clip, request); err == nil || clip.reads != 0 {
		t.Fatal("cancelled timer touched clipboard")
	}
}
