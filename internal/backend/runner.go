package backend

import (
	"bytes"
	"context"
	"github.com/daviddwlee84/lazycred/internal/model"
	"os"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(context.Context, string, []string, []byte, map[string]string) ([]byte, error)
}
type NativeRunner struct{}

func (NativeRunner) Run(ctx context.Context, exe string, args []string, input []byte, extra map[string]string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Stdin = bytes.NewReader(input)
	// Only explicitly requested children receive a session or unlock password.
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if key != "BW_SESSION" && !strings.HasPrefix(key, "LAZYCRED_SECRET_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	// Native errors can contain credentials. Never replay stderr or raw errors.
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, model.Fail("cancelled", "Operation cancelled or timed out; a write may already have completed.")
		}
		return nil, model.Fail("backend_failed", "Backend failed; check setup, authentication and device connection.")
	}
	return out.Bytes(), nil
}
