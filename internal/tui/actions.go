package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/daviddwlee84/lazycred/internal/model"
)

type Action struct {
	ID, Key, Label string
	Enabled        bool
}

func (m *Model) actions() []Action {
	source := m.source()
	item, has := m.selected()
	key := source.Kind == "yubikey"
	editable := key && !m.app.ReadOnly && !m.busy
	copyable := false
	for _, f := range m.detail.Fields {
		if f.Copyable {
			copyable = true
		}
	}
	return []Action{
		{"help", "?", "Help", true}, {"actions", ":", "Actions", true}, {"refresh", "r", "Refresh", !m.busy}, {"source", "s", "Choose source", true},
		{"category", "c", "Category", key || source.Kind == "bitwarden"}, {"copy", "y", "Copy field", has && copyable && !m.busy}, {"reveal", "v", "Reveal secret", has && copyable && !m.busy},
		{"lock", "L", "Lock locally", true}, {"unlock", "u", "Unlock", !m.busy && ((source.Kind == "bitwarden" && source.State != "unauthenticated" && source.State != "unavailable") || (key && (m.category == "oath" || m.category == "fido")))},
		{"add", "a", "Add TOTP", editable && m.category == "oath"}, {"delete", "d", "Delete exact credential", editable && has && (item.Kind == "oath" || item.Kind == "fido")},
		{"pin", "p", "Change application PIN/password", editable && (m.category == "oath" || m.category == "fido" || m.category == "piv" || m.category == "openpgp")},
		{"transfer", "t", "Copy TOTP to YubiKey", !m.busy && !m.app.ReadOnly && has && item.Source == "bitwarden" && item.Kind == "login"},
		{"export", "e", "Export public field", !m.busy && has && copyable && (item.Kind == "piv" || item.Kind == "ssh" || item.Kind == "openpgp")},
		{"setup", "S", "Prepare YubiKey backend", !m.busy && !m.app.ReadOnly}, {"mouse", "m", "Toggle mouse capture", true},
	}
}
func newField(id, label string, hidden bool) inputField {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 8192
	input.SetWidth(60)
	if hidden {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
	}
	return inputField{ID: id, Label: label, Secret: hidden, Input: input}
}
func (m *Model) form(action, title string, inputs []inputField) {
	item, _ := m.selected()
	m.modal = &dialog{Kind: "form", Action: action, Title: title, Inputs: inputs, Item: item, Source: m.sourceID, Category: m.category}
	if len(inputs) > 0 {
		m.modal.Inputs[0].Input.Focus()
	}
}
func (m *Model) activate(id string) tea.Cmd {
	switch id {
	case "help":
		m.modal = &dialog{Kind: "help", Title: "Help"}
		return nil
	case "actions":
		var options []Action
		for _, a := range m.actions() {
			if a.Enabled && a.ID != "actions" {
				options = append(options, a)
			}
		}
		m.modal = &dialog{Kind: "menu", Title: "Actions", Options: options}
		return nil
	case "source":
		var options []Action
		for _, s := range m.sources {
			options = append(options, Action{ID: s.ID, Label: model.Label(s), Enabled: true})
		}
		m.modal = &dialog{Kind: "source", Title: "Sources", Options: options}
		return nil
	case "category":
		cats := []string{"status", "oath", "fido", "piv", "openpgp"}
		if m.sourceID == "bitwarden" {
			cats = []string{"all", "login", "card", "note", "identity", "ssh"}
		}
		var options []Action
		for _, cat := range cats {
			options = append(options, Action{ID: cat, Label: cat, Enabled: true})
		}
		m.modal = &dialog{Kind: "category", Title: "Category", Options: options}
		return nil
	case "copy", "reveal", "export":
		var options []Action
		for _, f := range m.detail.Fields {
			if !f.Copyable {
				continue
			}
			if id == "reveal" && !f.Secret {
				continue
			}
			if id == "export" && (f.Secret || !(f.ID == "certificate" || f.ID == "public_key" || f.ID == "fingerprint" || f.ID == "fingerprint_sha256")) {
				continue
			}
			label := f.Label
			if f.Secret {
				label += " · secret"
			}
			options = append(options, Action{ID: f.ID, Label: label, Enabled: true})
		}
		if len(options) == 0 {
			m.status = "No eligible fields for this action."
			return nil
		}
		item, _ := m.selected()
		m.modal = &dialog{Kind: id, Title: strings.Title(id) + " field", Options: options, Item: item}
		return nil
	case "lock":
		m.lock()
		return m.discovery()
	case "unlock":
		label := "Application PIN/password"
		if m.sourceID == "bitwarden" {
			label = "Master password · bw unlock invalidates previous CLI sessions"
		}
		m.form("unlock", "Unlock", []inputField{newField("password", label, true)})
		return nil
	case "refresh":
		if m.sourceID == "bitwarden" {
			app := m.app
			return m.startJob("refresh", func(ctx context.Context) jobMsg {
				err := app.RefreshBW(ctx)
				return jobMsg{Message: "Bitwarden synchronized; refreshing metadata.", Err: err}
			})
		}
		return tea.Batch(m.discovery(), m.load())
	case "add":
		m.form("oath-add", "Add TOTP · source seed is not backed up by the device", []inputField{newField("name", "Account label", false), newField("secret", "Base32 TOTP seed or otpauth URI", true), newField("current", "Current OATH password (empty if unset)", true), newField("confirm", "Confirm destination: type full serial", false)})
		return nil
	case "delete":
		item, _ := m.selected()
		m.form(item.Kind+"-delete", "Delete exact credential · service-side registration is separate", []inputField{newField("current", "Application PIN/password (empty if unset)", true), newField("confirm", "Type the full target serial", false)})
		return nil
	case "pin":
		operation := m.category + "-pin"
		if m.category == "oath" {
			operation = "oath-password"
		}
		m.form(operation, "Change PIN/password · no automatic retries", []inputField{newField("current", "Current secret (empty only if unset)", true), newField("secret", "New PIN/password", true), newField("repeat", "Repeat new PIN/password", true), newField("confirm", "Type the full target serial", false)})
		return nil
	case "transfer":
		m.form("transfer", "Copy personal Bitwarden TOTP · source retained", []inputField{newField("target", "Destination source (yubikey:SERIAL)", false), newField("name", "Target label (optional)", false), newField("current", "Destination OATH password (empty if unset)", true), newField("confirm", "Type the full destination serial", false)})
		return nil
	case "setup":
		m.modal = &dialog{Kind: "setup-review", Title: "Prepare official SDK environment", Text: "Run uv sync for pinned yubikey-manager / PyOTP dependencies.\nMay download Python/dependencies. No device settings change.\n←/→ choose · Enter submit · Esc cancel", Apply: false}
		return nil
	case "mouse":
		m.mouse = !m.mouse
		m.status = fmt.Sprintf("Mouse capture: %t", m.mouse)
		return nil
	}
	return nil
}
func (m *Model) updateInputs(msg tea.Msg) tea.Cmd {
	if m.modal == nil || m.modal.Kind != "form" || m.modal.Review || len(m.modal.Inputs) == 0 {
		return nil
	}
	index := m.modal.Focus
	var cmd tea.Cmd
	m.modal.Inputs[index].Input, cmd = m.modal.Inputs[index].Input.Update(msg)
	return cmd
}
func (d *dialog) values() map[string]string {
	values := map[string]string{}
	for _, f := range d.Inputs {
		values[f.ID] = f.Input.Value()
	}
	return values
}
func (m *Model) reviewForm() tea.Cmd {
	d := m.modal
	values := d.values()
	d.Error = ""
	if values["repeat"] != "" && values["repeat"] != values["secret"] {
		d.Error = "New secrets do not match."
		return nil
	}
	if (strings.HasSuffix(d.Action, "-pin") || d.Action == "oath-password") && (values["secret"] == "" || values["repeat"] != values["secret"]) {
		d.Error = "Enter and repeat a nonempty new secret."
		return nil
	}
	if d.Action == "oath-add" && (values["name"] == "" || values["secret"] == "") {
		d.Error = "Account label and seed are required."
		return nil
	}
	if d.Action == "transfer" {
		target, ok := strings.CutPrefix(values["target"], "yubikey:")
		if !ok || target == "" || values["confirm"] != target {
			d.Error = "Enter an exact destination source and matching serial."
			return nil
		}
	} else if d.Action != "unlock" && d.Action != "export" {
		serial := strings.TrimPrefix(d.Source, "yubikey:")
		if values["confirm"] != serial {
			d.Error = "Confirmation must match the exact target serial."
			return nil
		}
	}
	if d.Action == "unlock" {
		return m.submitForm()
	}
	if d.Action == "export" && values["path"] == "" {
		d.Error = "Choose a new output path."
		return nil
	}
	d.Review = true
	d.Apply = false
	for i := range d.Inputs {
		d.Inputs[i].Input.Blur()
	}
	return nil
}
func (m *Model) submitForm() tea.Cmd {
	d := m.modal
	v := d.values()
	app := m.app
	source, category, item, action := d.Source, d.Category, d.Item, d.Action
	if action == "unlock" {
		password := v["password"]
		return m.startJob("unlock", func(ctx context.Context) jobMsg {
			if source == "bitwarden" {
				err := app.UnlockBW(ctx, password)
				return jobMsg{Message: "Bitwarden unlocked in memory.", Err: err}
			}
			_, err := app.UnlockYubi(ctx, source, category, password)
			return jobMsg{Message: "Application unlocked for this session.", Err: err}
		})
	}
	if action == "export" {
		path, field := v["path"], d.Text
		return m.startJob("export", func(ctx context.Context) jobMsg {
			err := app.Export(ctx, item, field, path)
			return jobMsg{Message: "Public field exported without replacing any existing file.", Err: err}
		})
	}
	if action == "transfer" {
		return m.startJob("transfer", func(ctx context.Context) jobMsg {
			_, err := app.Transfer(ctx, item.ID, v["target"], v["name"], v["current"], v["confirm"], true)
			if err != nil {
				return jobMsg{Err: err}
			}
			message, err := app.Transfer(ctx, item.ID, v["target"], v["name"], v["current"], v["confirm"], false)
			return jobMsg{Message: message, Err: err}
		})
	}
	return m.startJob("manage", func(ctx context.Context) jobMsg {
		message, err := app.Write(ctx, source, action, item.ID, v["name"], v["secret"], v["current"], v["confirm"])
		return jobMsg{Message: message, Err: err}
	})
}
func (m *Model) dialogKey(msg tea.KeyPressMsg) tea.Cmd {
	d := m.modal
	key := msg.String()
	if key == "esc" || key == "ctrl+c" {
		if d.Kind == "waiting" && m.cancel != nil {
			m.cancel()
			m.status = "Cancelled waiting; inspect any pending write before retrying."
		}
		m.modal = nil
		m.updateViewport()
		return nil
	}
	if d.Kind == "form" && !d.Review {
		switch key {
		case "tab", "enter":
			if d.Focus == len(d.Inputs)-1 {
				return m.reviewForm()
			}
			d.Inputs[d.Focus].Input.Blur()
			d.Focus++
			return d.Inputs[d.Focus].Input.Focus()
		case "shift+tab":
			d.Inputs[d.Focus].Input.Blur()
			d.Focus = (d.Focus + len(d.Inputs) - 1) % len(d.Inputs)
			return d.Inputs[d.Focus].Input.Focus()
		case "ctrl+s":
			return m.reviewForm()
		}
		return m.updateInputs(msg)
	}
	if d.Kind == "setup-review" || (d.Kind == "form" && d.Review) {
		switch key {
		case "left", "right", "tab", "h", "l":
			d.Apply = !d.Apply
		case "enter":
			if !d.Apply {
				m.modal = nil
				return nil
			}
			if d.Kind == "setup-review" {
				app := m.app
				return m.startJob("setup", func(ctx context.Context) jobMsg {
					err := app.Setup(ctx)
					return jobMsg{Message: "SDK helper ready; device state was not changed.", Err: err}
				})
			}
			return m.submitForm()
		case "backspace":
			if d.Kind == "form" {
				d.Review = false
				return d.Inputs[d.Focus].Input.Focus()
			}
		}
		return nil
	}
	if d.Kind == "help" || d.Kind == "result" {
		if d.Kind == "help" {
			if key == "down" || key == "j" {
				d.Selection++
			}
			if key == "up" || key == "k" {
				d.Selection = max(0, d.Selection-1)
			}
		}
		if key == "enter" || key == "q" {
			m.modal = nil
		}
		return nil
	}
	if d.Kind == "detail" {
		if key == "y" {
			return m.activate("copy")
		}
		if key == "v" {
			return m.activate("reveal")
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	}
	if d.Kind == "waiting" {
		return nil
	}
	if key == "up" || key == "k" {
		d.Selection = max(0, d.Selection-1)
		return nil
	}
	if key == "down" || key == "j" {
		d.Selection = min(len(d.Options)-1, d.Selection+1)
		return nil
	}
	if key == "enter" && d.Selection >= 0 && d.Selection < len(d.Options) {
		option := d.Options[d.Selection]
		kind, item := d.Kind, d.Item
		m.modal = nil
		switch kind {
		case "menu":
			return m.activate(option.ID)
		case "source":
			m.sourceID = option.ID
			m.category = "status"
			m.focus = 1
			return m.load()
		case "category":
			m.category = option.ID
			m.focus = 1
			m.filter.SetValue(m.pane().Query)
			return m.load()
		case "export":
			m.form("export", "Export public field · new file only", []inputField{newField("path", "Output path (parent directory must exist)", false)})
			m.modal.Text = option.ID
			return nil
		case "copy", "reveal":
			app := m.app
			field := option.ID
			if field == "otp" {
				if f, ok := m.detail.Field("oath_type"); ok && f.Value == "HOTP" {
					m.status = "HOTP advances a counter. Use explicit CLI --confirm-serial; background generation is disabled."
					return nil
				}
			}
			if kind == "copy" {
				return m.startJob("copy", func(ctx context.Context) jobMsg {
					message, err := app.Copy(ctx, item, field, "")
					return jobMsg{Message: message, Err: err}
				})
			}
			return m.startJob("reveal", func(ctx context.Context) jobMsg {
				value, err := app.Value(ctx, item, field, "")
				return jobMsg{Field: value, Err: err}
			})
		}
	}
	return nil
}
