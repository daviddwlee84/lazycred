package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
	"github.com/daviddwlee84/lazycred/internal/service"
)

func invoke(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cmd := Command()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestJSONNeverRevealsSecrets(t *testing.T) {
	out, err := invoke(t, "--demo", "--source", "bitwarden", "items", "show", "11111111-1111-4111-8111-111111111111", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(out)) || !strings.Contains(out, "[redacted]") || strings.Contains(out, "demo-only-value") {
		t.Fatalf("unsafe JSON: %s", out)
	}
}

func TestInvalidSelectionsAndEmptyOverridesFailBeforeIO(t *testing.T) {
	for _, args := range [][]string{
		{"--demo", "--source", "typo", "sources"},
		{"--demo", "--category", "typo", "items", "list"},
		{"--demo", "--source", "bitwarden", "--category", "oath", "items", "list"},
		{"--demo", "--bw=", "sources"},
	} {
		if _, err := invoke(t, args...); model.Code(err) != "invalid" {
			t.Fatalf("accepted %v: %v", args, err)
		}
	}
}

func TestNonInteractiveManagementDoesNotPrompt(t *testing.T) {
	out, err := invoke(t, "--demo", "--source", "yubikey:11111111", "manage", "oath-add", "--name", "Example", "--json")
	if model.Code(err) != "interactive_required" || strings.Contains(out, "Current application") {
		t.Fatal("JSON management prompted")
	}
	if _, err = invoke(t, "--demo", "--read-only", "--source", "yubikey:11111111", "manage", "oath-add", "--name", "Example"); model.Code(err) != "read_only" {
		t.Fatal("read-only management accepted")
	}
}

func TestExactIDOnly(t *testing.T) {
	a := service.New(config.Config{}, true, false)
	if _, err := find(context.Background(), a, "bitwarden", "all", "1111"); model.Code(err) != "not_found" {
		t.Fatal("substring selected an item")
	}
}
