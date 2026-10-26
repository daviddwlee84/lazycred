package service

import (
	"github.com/daviddwlee84/lazycred/internal/model"
	"strings"
)

type demoStore struct {
	locked  bool
	extra   map[string][]model.Item
	deleted map[string]bool
}

func newDemo() *demoStore {
	return &demoStore{extra: map[string][]model.Item{}, deleted: map[string]bool{}}
}
func (a *App) demoSources() []model.Source {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := "unlocked"
	if a.demo.locked {
		state = "locked"
	}
	return []model.Source{
		{ID: "overview", Kind: "overview", Name: "Overview", State: "ready", Summary: "Fictional data · no hardware, vault or clipboard access"},
		{ID: "all", Kind: "all", Name: "All items", State: "ready", Summary: "Search readable metadata across sources"},
		{ID: "yubikey:11111111", Kind: "yubikey", Name: "Travel key · USB-C", State: "ready", Summary: "Firmware 5.7.4 · FIDO + CCID"},
		{ID: "yubikey:22222222", Kind: "yubikey", Name: "Backup key · USB-A", State: "ready", Summary: "Firmware 4.4.5 · CCID"},
		{ID: "bitwarden", Kind: "bitwarden", Name: "Bitwarden · Demo", State: state, Summary: "demo@example.invalid"},
	}
}
func (a *App) demoItems(source, category string) ([]model.Item, error) {
	if source == "overview" {
		var rows []model.Item
		for _, s := range a.demoSources() {
			if s.ID != "overview" && s.ID != "all" {
				rows = append(rows, model.Item{ID: s.ID, Source: "overview", Name: s.Name, Kind: s.Kind, State: s.State, Subtitle: s.Summary, Fields: []model.Field{{ID: "source", Label: "Source", Value: s.ID, Copyable: true}, {ID: "state", Label: "State", Value: s.State}}})
			}
		}
		return rows, nil
	}
	if source == "all" {
		var rows []model.Item
		for _, s := range a.demoSources() {
			cats := []string{}
			if s.Kind == "bitwarden" {
				cats = []string{"all"}
			}
			if s.Kind == "yubikey" {
				cats = []string{"oath", "fido", "piv", "openpgp"}
			}
			for _, cat := range cats {
				items, err := a.demoItems(s.ID, cat)
				if err == nil {
					rows = append(rows, items...)
				}
			}
		}
		return rows, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if source == "bitwarden" {
		if a.demo.locked {
			return nil, model.Fail("locked", "Demo vault is locked. Use Unlock; any demo password is accepted.")
		}
		return []model.Item{
			{ID: "11111111-1111-4111-8111-111111111111", Source: source, Name: "Example Cloud", Kind: "login", Subtitle: "alice@example.invalid", State: "ready"},
			{ID: "22222222-2222-4222-8222-222222222222", Source: source, Name: "Development SSH", Kind: "ssh", State: "ready"},
			{ID: "33333333-3333-4333-8333-333333333333", Source: source, Name: "專案 é 👩🏽‍💻", Kind: "note", State: "ready"},
		}, nil
	}
	var rows []model.Item
	switch category {
	case "oath":
		rows = []model.Item{{ID: "demo-oath", Source: source, Name: "Example:alice@example.invalid", Kind: "oath", Subtitle: "TOTP · touch required", State: "ready", Fields: []model.Field{{ID: "username", Label: "Username", Value: "alice@example.invalid", Copyable: true}, {ID: "oath_type", Label: "Type", Value: "TOTP"}, {ID: "otp", Label: "OTP (generate on demand)", Secret: true, Copyable: true}}}}
	case "fido":
		if strings.HasSuffix(source, "22222222") {
			return nil, model.Fail("not_supported", "This fictional v4 has no FIDO2 application.")
		}
		rows = []model.Item{{ID: "demo-fido", Source: source, Name: "Example passkey", Kind: "fido", State: "ready", Fields: []model.Field{{ID: "rp_id", Label: "Relying party", Value: "example.invalid", Copyable: true}, {ID: "username", Label: "Username", Value: "alice", Copyable: true}, {ID: "credential_id", Label: "Credential ID", Value: "demo-fido", Copyable: true}}}}
	case "piv":
		rows = []model.Item{{ID: "9a", Source: source, Name: "Slot 9A · AUTHENTICATION", Kind: "piv", State: "certificate_present", Fields: []model.Field{{ID: "subject", Label: "Subject", Value: "CN=Demo only", Copyable: true}, {ID: "expires", Label: "Expires", Value: "2027-12-31", Copyable: true}, {ID: "private_key", Label: "Private key", Value: "Non-exportable"}}}}
	case "openpgp":
		rows = []model.Item{{ID: "1", Source: source, Name: "Signing key", Kind: "openpgp", State: "ready", Fields: []model.Field{{ID: "fingerprint", Label: "Fingerprint", Value: "0000 1111 2222 3333 (fictional)", Copyable: true}}}}
	default:
		rows = []model.Item{{ID: "device", Source: source, Name: "Device information", Kind: "status", State: "ready", Fields: []model.Field{{ID: "serial", Label: "Serial", Value: strings.TrimPrefix(source, "yubikey:"), Copyable: true}, {ID: "firmware", Label: "Firmware", Value: "Demo fixture"}, {ID: "pin_state", Label: "PIN", Value: "Configured"}, {ID: "credentials", Label: "FIDO capacity", Value: "100 maximum (fictional)"}, {ID: "fips", Label: "FIPS", Value: "Capability and approved state are distinct"}}}}
	}
	rows = append(rows, a.demo.extra[source+"/"+category]...)
	var visible []model.Item
	for _, r := range rows {
		if !a.demo.deleted[source+"/"+r.ID] {
			visible = append(visible, r)
		}
	}
	return visible, nil
}
func (a *App) demoDetail(item model.Item) (model.Item, error) {
	if item.Source != "bitwarden" {
		return item, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.demo.locked {
		return model.Item{}, model.Fail("locked", "Demo vault is locked.")
	}
	if item.Kind == "login" {
		item.Fields = []model.Field{{ID: "username", Label: "Username", Value: "alice@example.invalid", Copyable: true}, {ID: "uri_0", Label: "URL", Value: "https://example.invalid", Copyable: true}, {ID: "password", Label: "Password", Value: "demo-only-value", Secret: true, Copyable: true}, {ID: "otp", Label: "OTP (generate on demand)", Secret: true, Copyable: true}}
	} else if item.Kind == "ssh" {
		item.Fields = []model.Field{{ID: "fingerprint", Label: "Fingerprint", Value: "SHA256:fictional-demo", Copyable: true}, {ID: "private_key", Label: "Private key", Value: "FICTIONAL DEMO ONLY", Secret: true, Copyable: true}}
	} else {
		item.Fields = []model.Field{{ID: "notes", Label: "Notes", Value: "This note is fictional. 中文與 emoji remain readable.", Secret: true, Copyable: true}}
	}
	return item, nil
}
func (a *App) demoWrite(source, operation, id, name string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.HasSuffix(operation, "delete") {
		a.demo.deleted[source+"/"+id] = true
	}
	if operation == "oath-add" {
		if name == "" {
			return "", model.Fail("invalid", "Choose a name.")
		}
		for _, row := range a.demo.extra[source+"/oath"] {
			if row.Name == name {
				return "", model.Fail("conflict", "Demo account already exists.")
			}
		}
		a.demo.extra[source+"/oath"] = append(a.demo.extra[source+"/oath"], model.Item{ID: "demo-" + name, Source: source, Name: name, Kind: "oath", State: "ready", Fields: []model.Field{{ID: "otp", Label: "OTP", Secret: true, Copyable: true}}})
	}
	return "Demo operation completed in memory only. No real resources changed.", nil
}
