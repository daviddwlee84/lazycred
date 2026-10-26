package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPrecedenceAndNoSecretPersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "selected.toml")
	if err := os.WriteFile(path, []byte("bw='from-file'\nidle_seconds=12\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LAZYCRED_BW", "from-env")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.BW != "from-env" || c.IdleSeconds != 12 || c.Path != path {
		t.Fatal("wrong precedence")
	}
}
func TestDefaultMissingExplicitMissingAndBadTimeout(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if _, err := Load(""); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("explicit missing accepted")
	}
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte("idle_seconds=0\n"), 0600)
	if _, err := Load(path); err == nil {
		t.Fatal("invalid timeout accepted")
	}
}
func TestRelativeXDGIsIgnored(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if !filepath.IsAbs(Dir("config")) {
		t.Fatal("relative XDG accepted")
	}
}
