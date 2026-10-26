package config

import (
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	BW               string            `toml:"bw" json:"bw"`
	UV               string            `toml:"uv" json:"uv"`
	IdleSeconds      int               `toml:"idle_seconds" json:"idle_seconds"`
	RevealSeconds    int               `toml:"reveal_seconds" json:"reveal_seconds"`
	ClipboardSeconds int               `toml:"clipboard_seconds" json:"clipboard_seconds"`
	Aliases          map[string]string `toml:"aliases" json:"aliases,omitempty"`
	Path             string            `toml:"-" json:"path"`
}

func Dir(kind string) string {
	env, fallback := "XDG_CONFIG_HOME", ".config"
	if kind == "cache" {
		env, fallback = "XDG_CACHE_HOME", ".cache"
	}
	if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
		return filepath.Join(v, "lazycred")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, fallback, "lazycred")
}
func Load(path string) (Config, error) {
	c := Config{BW: "bw", UV: "uv", IdleSeconds: 300, RevealSeconds: 15, ClipboardSeconds: 30, Aliases: map[string]string{}}
	explicit := path != ""
	if path == "" {
		if dir := Dir("config"); dir != "" {
			path = filepath.Join(dir, "config.toml")
		}
	}
	c.Path = path
	if path != "" {
		b, err := os.ReadFile(path)
		if err == nil {
			if err = toml.Unmarshal(b, &c); err != nil {
				return c, fmt.Errorf("invalid config TOML")
			}
		} else if explicit || !os.IsNotExist(err) {
			return c, fmt.Errorf("cannot read selected config")
		}
	}
	if s := os.Getenv("LAZYCRED_BW"); s != "" {
		c.BW = s
	}
	if s := os.Getenv("LAZYCRED_UV"); s != "" {
		c.UV = s
	}
	if c.BW == "" || c.UV == "" || c.IdleSeconds < 1 || c.RevealSeconds < 1 || c.ClipboardSeconds < 1 || c.ClipboardSeconds > 600 {
		return c, fmt.Errorf("invalid tool path or timeout (clipboard: 1–600 seconds)")
	}
	return c, nil
}
func (c Config) Idle() time.Duration { return time.Duration(c.IdleSeconds) * time.Second }
