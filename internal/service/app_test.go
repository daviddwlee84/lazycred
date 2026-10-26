package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/daviddwlee84/lazycred/internal/backend"
	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
)

type fakeRunner func(context.Context, string, []string, []byte, map[string]string) ([]byte, error)

func (f fakeRunner) Run(c context.Context, e string, a []string, b []byte, v map[string]string) ([]byte, error) {
	return f(c, e, a, b, v)
}
func testApp(t *testing.T, demo, readonly bool) *App {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	c, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	return New(c, demo, readonly)
}
func TestDemoNeverRunsBackendsOrClipboard(t *testing.T) {
	a := testApp(t, true, false)
	a.Runner = fakeRunner(func(context.Context, string, []string, []byte, map[string]string) ([]byte, error) {
		t.Fatal("demo touched backend")
		return nil, nil
	})
	rows, err := a.Items(context.Background(), "bitwarden", "all")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Copy(context.Background(), rows[0], "password", ""); err != nil {
		t.Fatal(err)
	}
	if err = a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Sources(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestReadOnlyRejectsWritesAndHOTPBeforeInvocation(t *testing.T) {
	a := testApp(t, false, true)
	a.Runner = fakeRunner(func(context.Context, string, []string, []byte, map[string]string) ([]byte, error) {
		t.Fatal("read-only invoked mutation")
		return nil, nil
	})
	if _, err := a.Write(context.Background(), "yubikey:11111111", "oath-delete", "id", "", "", "", "11111111"); model.Code(err) != "read_only" {
		t.Fatal("write accepted")
	}
	if err := a.Setup(context.Background()); model.Code(err) != "read_only" {
		t.Fatal("setup accepted")
	}
	item := model.Item{ID: "public-test", Source: "yubikey:11111111", Fields: []model.Field{{ID: "oath_type", Value: "HOTP"}, {ID: "otp", Secret: true, Copyable: true}}}
	if _, err := a.Value(context.Background(), item, "otp", "11111111"); model.Code(err) != "read_only" {
		t.Fatal("HOTP advanced")
	}
}
func TestLockWinsAgainstLateBWUnlock(t *testing.T) {
	a := testApp(t, false, false)
	started, release := make(chan struct{}), make(chan struct{})
	a.Runner = fakeRunner(func(_ context.Context, _ string, args []string, _ []byte, env map[string]string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "fixture-password") {
			t.Error("password in argv")
		}
		if env["LAZYCRED_SECRET_PASSWORD"] != "fixture-password" {
			t.Error("missing child-only password")
		}
		close(started)
		<-release
		return []byte("fixture-session"), nil
	})
	done := make(chan error, 1)
	go func() { done <- a.UnlockBW(context.Background(), "fixture-password") }()
	<-started
	a.Lock()
	close(release)
	if err := <-done; model.Code(err) != "cancelled" {
		t.Fatal("late unlock succeeded")
	}
	if a.Session() != "" {
		t.Fatal("session resurrected after lock")
	}
}
func TestTransferPreviewUsesExactTargetAndKeepsSource(t *testing.T) {
	a := testApp(t, false, false)
	var calls []string
	var request backend.YubiRequest
	a.Runner = fakeRunner(func(_ context.Context, _ string, args []string, input []byte, _ map[string]string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "sync" {
			return nil, nil
		}
		if strings.Contains(strings.Join(args, " "), "get item") {
			return []byte(`{"id":"11111111-1111-4111-8111-111111111111","name":"Example","type":1,"login":{"username":"alice","totp":"PUBLIC-TEST-SEED"}}`), nil
		}
		json.Unmarshal(input, &request)
		return []byte(`{"ok":true,"result":{"message":"Preview only"}}`), nil
	})
	if err := backend.Prepare(context.Background(), a.Runner, "uv"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Transfer(context.Background(), "11111111-1111-4111-8111-111111111111", "yubikey:22222222", "new", "fixture-password", "", true); err != nil {
		t.Fatal(err)
	}
	if request.Op != "oath-preview" || request.Serial != "22222222" || request.Auth != "fixture-password" {
		t.Fatal("wrong transfer target")
	}
	for _, call := range calls {
		if strings.Contains(call, "PUBLIC-TEST-SEED") || strings.Contains(call, "fixture-password") || strings.Contains(call, "delete") || strings.Contains(call, "edit") {
			t.Fatal("unsafe transfer invocation")
		}
	}
}
func TestPublicExportNoOverwriteOrPrivateExport(t *testing.T) {
	a := testApp(t, false, false)
	dir := t.TempDir()
	path := filepath.Join(dir, "public.pem")
	item := model.Item{Source: "yubikey:11111111", Fields: []model.Field{{ID: "public_key", Value: "PUBLIC TEST MATERIAL", Copyable: true}, {ID: "private_key", Value: "fixture-only", Secret: true, Copyable: true}}}
	if err := a.Export(context.Background(), item, "public_key", path); err != nil {
		t.Fatal(err)
	}
	if err := a.Export(context.Background(), item, "public_key", path); err == nil {
		t.Fatal("overwrote existing file")
	}
	if err := a.Export(context.Background(), item, "private_key", filepath.Join(dir, "private")); err == nil {
		t.Fatal("exported private field")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "PUBLIC TEST MATERIAL" {
		t.Fatal("public export changed")
	}
}
func TestDemoConcurrentReadsAndLock(t *testing.T) {
	a := testApp(t, true, false)
	var wait sync.WaitGroup
	for range 10 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			a.Sources(context.Background())
			a.Items(context.Background(), "overview", "status")
			a.Lock()
		}()
	}
	wait.Wait()
}
