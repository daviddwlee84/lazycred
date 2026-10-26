package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNestedJSONAlwaysRedacts(t *testing.T) {
	item := Item{ID: "test", Fields: []Field{{ID: "password", Value: "fixture-only-private-value", Secret: true, Copyable: true}, {ID: "username", Value: "demo", Copyable: true}}}
	raw, err := json.Marshal(map[string]any{"item": item})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-only-private-value") || !strings.Contains(string(raw), "[redacted]") || !strings.Contains(string(raw), "demo") {
		t.Fatal("nested field redaction failed")
	}
}
func TestHelperFieldDecodeKeepsMemoryValueButRedactsOutput(t *testing.T) {
	var f Field
	if err := json.Unmarshal([]byte(`{"id":"password","label":"Password","value":"fixture-value","secret":true,"copyable":true}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.Value != "fixture-value" || !f.Secret || !f.Copyable {
		t.Fatal("invalid bridge field")
	}
	raw, _ := json.Marshal(f)
	if strings.Contains(string(raw), "fixture-value") {
		t.Fatal("secret leaked")
	}
}
func TestTerminalControlSanitization(t *testing.T) {
	if strings.ContainsAny(Safe("name\x1b]52;c;payload\a\n"), "\x1b\a\n") {
		t.Fatal("terminal control survived")
	}
}
