package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daviddwlee84/lazycred/internal/model"
)

type fakeRunner func(context.Context, string, []string, []byte, map[string]string) ([]byte, error)

func (f fakeRunner) Run(c context.Context, e string, a []string, b []byte, v map[string]string) ([]byte, error) {
	return f(c, e, a, b, v)
}
func TestBridgeSecretsTravelOnlyInStdin(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var invocation int
	r := fakeRunner(func(_ context.Context, _ string, args []string, input []byte, _ map[string]string) ([]byte, error) {
		invocation++
		for _, arg := range args {
			if strings.Contains(arg, "fixture-pin") {
				t.Fatal("PIN in argv")
			}
		}
		if invocation == 1 {
			return nil, nil
		}
		if !strings.Contains(string(input), "fixture-pin") {
			t.Fatal("missing stdin auth")
		}
		return []byte(`{"ok":true,"result":[]}`), nil
	})
	if err := Prepare(context.Background(), r, "uv"); err != nil {
		t.Fatal(err)
	}
	var rows []model.Item
	if err := Yubi(context.Background(), r, "uv", YubiRequest{Op: "items", Serial: "11111111", Auth: "fixture-pin"}, &rows); err != nil {
		t.Fatal(err)
	}
}
func TestBridgeDoesNotBootstrapDuringRead(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	r := fakeRunner(func(context.Context, string, []string, []byte, map[string]string) ([]byte, error) {
		t.Fatal("backend executed without setup")
		return nil, nil
	})
	err := Yubi(context.Background(), r, "uv", YubiRequest{Op: "sources"}, nil)
	if model.Code(err) != "setup_required" {
		t.Fatal("missing setup not diagnosed")
	}
}
func TestRunnerSuppressesStderrAndFiltersSession(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "fake")
	text := "#!/bin/sh\nif test -n \"${BW_SESSION:-}\"; then exit 4; fi\nprintf 'fixture-only-secret' >&2\nexit 7\n"
	if err := os.WriteFile(program, []byte(text), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BW_SESSION", "fixture-session")
	_, err := (NativeRunner{}).Run(context.Background(), program, nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), "fixture-only-secret") {
		t.Fatal("raw stderr replayed")
	}
}
func TestBWDetailsDoNotExposePasskeyPrivateMaterial(t *testing.T) {
	var item BWItem
	raw := `{"id":"11111111-1111-4111-8111-111111111111","type":1,"name":"Example","login":{"username":"alice","fido2Credentials":[{"rpId":"example.invalid","userName":"alice","credentialId":"public-id","keyValue":"MUST-NOT-BECOME-A-FIELD"}]}}`
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatal(err)
	}
	item.Login.Password = strings.Join([]string{"fixture", "password"}, "-")
	details := BWDetail(item)
	out, _ := json.Marshal(details)
	if strings.Contains(string(out), "fixture-password") || strings.Contains(string(out), "MUST-NOT-BECOME-A-FIELD") {
		t.Fatal("secret serialization")
	}
	if _, ok := details.Field("password"); !ok {
		t.Fatal("password unavailable for explicit copy")
	}
}
