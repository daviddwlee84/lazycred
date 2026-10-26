package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
)

type Clipboard interface {
	Read(context.Context) (string, error)
	Write(context.Context, string) error
}
type NativeClipboard struct{}

func clipboardCommands() (string, []string, string, []string, error) {
	if runtime.GOOS == "darwin" {
		return "pbcopy", nil, "pbpaste", nil, nil
	}
	if runtime.GOOS == "linux" {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if _, e := exec.LookPath("wl-copy"); e == nil {
				if _, e = exec.LookPath("wl-paste"); e == nil {
					return "wl-copy", nil, "wl-paste", []string{"--no-newline"}, nil
				}
			}
		}
		if os.Getenv("DISPLAY") != "" {
			if _, e := exec.LookPath("xclip"); e == nil {
				return "xclip", []string{"-selection", "clipboard"}, "xclip", []string{"-selection", "clipboard", "-o"}, nil
			}
		}
	}
	return "", nil, "", nil, model.Fail("clipboard_unavailable", "Use a local clipboard: pbcopy/pbpaste, wl-copy/wl-paste or xclip. No OSC52 fallback.")
}
func (NativeClipboard) Write(ctx context.Context, value string) error {
	exe, args, _, _, err := clipboardCommands()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdin = strings.NewReader(value)
	if cmd.Run() != nil {
		return model.Fail("clipboard_failed", "Cannot write the clipboard.")
	}
	return nil
}
func (NativeClipboard) Read(ctx context.Context) (string, error) {
	_, _, exe, args, err := clipboardCommands()
	if err != nil {
		return "", err
	}
	data, err := exec.CommandContext(ctx, exe, args...).Output()
	if err != nil {
		return "", model.Fail("clipboard_failed", "Cannot read back the clipboard.")
	}
	return string(data), nil
}

type ClearRequest struct {
	Value   string `json:"value"`
	Seconds int    `json:"seconds"`
	Marker  string `json:"marker"`
	Token   string `json:"token"`
}

// ScheduleClear keeps the secret only in a bounded child process, never argv or disk.
// A newer copy replaces the non-secret owner token; old timers cannot clear it.
func ScheduleClear(value string, seconds int) error {
	dir := config.Dir("cache")
	if dir == "" {
		return model.Fail("clipboard_cleanup_failed", "Cannot resolve clipboard cleanup state.")
	}
	dir = filepath.Join(dir, "clipboard")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)
	marker := filepath.Join(dir, "owner")
	scratch, err := os.CreateTemp(dir, ".owner-")
	if err != nil {
		return err
	}
	name := scratch.Name()
	defer os.Remove(name)
	if _, err = io.WriteString(scratch, token); err != nil {
		scratch.Close()
		return err
	}
	if err = scratch.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, marker); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := json.Marshal(ClearRequest{value, seconds, marker, token})
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "__clipboard-clear")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "BW_SESSION" && !strings.HasPrefix(key, "LAZYCRED_SECRET_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		return err
	}
	_, writeErr := io.Copy(input, bytes.NewReader(data))
	input.Close()
	if writeErr != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return writeErr
	}
	// Reap asynchronously while the caller is alive; the child does not depend
	// on the parent's renderer, session state or stdout after this return.
	go func() { _ = cmd.Wait() }()
	return nil
}
func ClearIfOwned(ctx context.Context, clip Clipboard, request ClearRequest) error {
	if request.Seconds < 1 || request.Seconds > 600 || len(request.Token) != 32 {
		return model.Fail("invalid", "Invalid clipboard cleanup request.")
	}
	dir := config.Dir("cache")
	if dir == "" || request.Marker != filepath.Join(dir, "clipboard", "owner") {
		return model.Fail("invalid", "Invalid cleanup marker.")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(request.Seconds) * time.Second):
	}
	return clearOwnedNow(ctx, clip, request)
}

func clearOwnedNow(ctx context.Context, clip Clipboard, request ClearRequest) error {
	token, err := os.ReadFile(request.Marker)
	if err != nil || string(token) != request.Token {
		return nil
	}
	current, err := clip.Read(ctx)
	if err != nil {
		return err
	}
	if current != request.Value {
		return nil
	}
	// Recheck owner after reading; leave later clipboard content alone.
	token, err = os.ReadFile(request.Marker)
	if err != nil || string(token) != request.Token {
		return nil
	}
	return clip.Write(ctx, "")
}
func (a *App) Copy(ctx context.Context, item model.Item, field, confirm string) (string, error) {
	f, err := a.Value(ctx, item, field, confirm)
	if err != nil {
		return "", err
	}
	if a.Demo {
		return "Demo copy simulated; the system clipboard was not accessed.", nil
	}
	clip := NativeClipboard{}
	if err = clip.Write(ctx, f.Value); err != nil {
		return "", err
	}
	// Even public copies rotate ownership so an old timer cannot clear a newer
	// intentional copy of the same text.
	ttl := a.Config.ClipboardSeconds
	if err = ScheduleClear(f.Value, ttl); err != nil {
		return "", model.Fail("clipboard_cleanup_failed", "Copied, but automatic cleanup could not start. Clear the clipboard manually.")
	}
	return "Copied. Clipboard cleanup is scheduled; newer clipboard content is preserved.", nil
}
func (a *App) Export(ctx context.Context, item model.Item, field, path string) error {
	f, err := a.Value(ctx, item, field, "")
	if err != nil {
		return err
	}
	if f.Secret || !(field == "certificate" || field == "public_key" || field == "fingerprint" || field == "fingerprint_sha256") {
		return model.Fail("invalid", "Only public certificates, public keys and fingerprints can be exported.")
	}
	if a.Demo {
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return model.Fail("output_exists", "Choose a new output path in an existing private directory.")
	}
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err = io.WriteString(file, f.Value); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}
