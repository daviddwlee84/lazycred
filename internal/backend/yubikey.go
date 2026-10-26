package backend

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
	"os"
	"path/filepath"
	"time"
)

//go:embed helper.py
var helper []byte

func HelperPath() (string, error) {
	dir := config.Dir("cache")
	if dir == "" {
		return "", model.Fail("setup_required", "Cannot resolve the user cache directory.")
	}
	hash := sha256.Sum256(helper)
	return filepath.Join(dir, "helpers", hex.EncodeToString(hash[:8]), "yubikey.py"), nil
}
func Prepare(ctx context.Context, r Runner, uv string) error {
	path, err := HelperPath()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err = os.WriteFile(path, helper, 0600); err != nil {
		return err
	}
	if _, err = r.Run(ctx, uv, []string{"sync", "--script", path}, nil, nil); err != nil {
		return err
	}
	return os.WriteFile(path+".ready", []byte("ready\n"), 0600)
}

type YubiRequest struct {
	Op        string `json:"op"`
	Serial    string `json:"serial,omitempty"`
	Category  string `json:"category,omitempty"`
	ID        string `json:"id,omitempty"`
	Auth      string `json:"auth,omitempty"`
	NewSecret string `json:"new_secret,omitempty"`
	Name      string `json:"name,omitempty"`
	Seed      string `json:"seed,omitempty"`
	Touch     bool   `json:"touch,omitempty"`
	Confirm   string `json:"confirm,omitempty"`
}
type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *model.Error    `json:"error"`
}

func Yubi(ctx context.Context, r Runner, uv string, request YubiRequest, target any) error {
	path, err := HelperPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(helper) {
		return model.Fail("setup_required", "Run lazycred setup yubikey to prepare the official SDK helper.")
	}
	if _, err = os.Stat(path + ".ready"); err != nil {
		return model.Fail("setup_required", "Run lazycred setup yubikey before using this backend.")
	}
	input, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	raw, err := r.Run(ctx, uv, []string{"run", "--offline", "--script", path}, input, nil)
	if err != nil {
		return err
	}
	var response envelope
	if json.Unmarshal(raw, &response) != nil {
		return model.Fail("protocol_error", "Invalid helper response; raw output suppressed.")
	}
	if !response.OK {
		if response.Error != nil {
			return response.Error
		}
		return model.Fail("backend_failed", "YubiKey operation failed.")
	}
	if target != nil && json.Unmarshal(response.Result, target) != nil {
		return model.Fail("protocol_error", "Invalid helper result; raw output suppressed.")
	}
	return nil
}
