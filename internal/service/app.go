package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daviddwlee84/lazycred/internal/backend"
	"github.com/daviddwlee84/lazycred/internal/config"
	"github.com/daviddwlee84/lazycred/internal/model"
)

type App struct {
	Config     config.Config
	Demo       bool
	ReadOnly   bool
	Runner     backend.Runner
	mu         sync.Mutex
	session    string
	auth       map[string]string
	epoch      uint64
	locks      sync.Map
	hardwareMu sync.Mutex
	demo       *demoStore
}

func New(c config.Config, demo, readOnly bool) *App {
	a := &App{Config: c, Demo: demo, ReadOnly: readOnly, Runner: backend.NativeRunner{}, auth: map[string]string{}}
	if demo {
		a.demo = newDemo()
	} else {
		a.session = os.Getenv("BW_SESSION")
	}
	return a
}
func (a *App) Lock() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = ""
	clear(a.auth)
	a.epoch++
	if a.Demo {
		a.demo.locked = true
	}
}
func (a *App) Session() string { a.mu.Lock(); defer a.mu.Unlock(); return a.session }
func (a *App) Epoch() uint64   { a.mu.Lock(); defer a.mu.Unlock(); return a.epoch }
func (a *App) Auth(source, category string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.auth[source+"/"+category]
}
func (a *App) forgetAuth(source, category string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.auth, source+"/"+category)
}
func (a *App) UnlockBW(ctx context.Context, password string) error {
	epoch := a.Epoch()
	if a.Demo {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.demo.locked = false
		return nil
	}
	raw, err := a.Runner.Run(ctx, a.Config.BW, []string{"--nointeraction", "--raw", "unlock", "--passwordenv", "LAZYCRED_SECRET_PASSWORD"}, nil, map[string]string{"LAZYCRED_SECRET_PASSWORD": password})
	if err != nil {
		return model.Fail("locked", "Bitwarden unlock failed; no automatic retry.")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || strings.ContainsAny(token, "\r\n ") {
		return model.Fail("protocol_error", "Unexpected unlock response; raw output suppressed.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.epoch != epoch || ctx.Err() != nil {
		return model.Fail("cancelled", "Unlock was superseded by Lock.")
	}
	a.session = token
	return nil
}
func serial(source string) (string, error) {
	s, ok := strings.CutPrefix(source, "yubikey:")
	if !ok || s == "" {
		return "", model.Fail("invalid", "Select an exact YubiKey source.")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", model.Fail("invalid", "Invalid serial.")
		}
	}
	return s, nil
}
func (a *App) yubi(ctx context.Context, source string, request backend.YubiRequest, target any) error {
	if source != "" {
		s, err := serial(source)
		if err != nil {
			return err
		}
		request.Serial = s
	}
	key := source
	if key == "" {
		key = "discovery"
	}
	lock, _ := a.locks.LoadOrStore(key, &sync.Mutex{})
	m := lock.(*sync.Mutex)
	m.Lock()
	defer m.Unlock()
	a.hardwareMu.Lock()
	defer a.hardwareMu.Unlock()
	if ctx.Err() != nil {
		return model.Fail("cancelled", "Cancelled before backend invocation.")
	}
	err := backend.Yubi(ctx, a.Runner, a.Config.UV, request, target)
	if err != nil {
		a.forgetAuth(source, request.Category)
	}
	return err
}
func (a *App) UnlockYubi(ctx context.Context, source, category, password string) ([]model.Item, error) {
	epoch := a.Epoch()
	var rows []model.Item
	if a.Demo {
		return a.Items(ctx, source, category)
	}
	err := a.yubi(ctx, source, backend.YubiRequest{Op: "items", Category: category, Auth: password}, &rows)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if epoch != a.epoch || ctx.Err() != nil {
		return nil, model.Fail("cancelled", "Unlock superseded by Lock.")
	}
	a.auth[source+"/"+category] = password
	for i := range rows {
		rows[i].Source = source
	}
	return rows, nil
}
func (a *App) Sources(ctx context.Context) ([]model.Source, error) {
	if a.Demo {
		return a.demoSources(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var keys []model.Source
	var keyErr error
	var bw model.Source
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); keyErr = a.yubi(ctx, "", backend.YubiRequest{Op: "sources"}, &keys) }()
	go func() { defer wait.Done(); bw = backend.BWStatus(ctx, a.Runner, a.Config.BW, a.Session()) }()
	wait.Wait()
	if keyErr != nil {
		keys = []model.Source{{ID: "yubikey", Kind: "setup", Name: "YubiKey backend", State: model.Code(keyErr), Summary: keyErr.Error()}}
	}
	for i := range keys {
		if alias := a.Config.Aliases[keys[i].ID]; alias != "" {
			keys[i].Name = alias
		}
	}
	sort.SliceStable(keys, func(i, j int) bool { return keys[i].ID < keys[j].ID })
	return append([]model.Source{{ID: "overview", Kind: "overview", Name: "Overview", State: "ready", Summary: "Sources, capabilities and freshness"}, {ID: "all", Kind: "all", Name: "All items", State: "ready", Summary: "Readable metadata; locked categories are labeled"}}, append(keys, bw)...), nil
}
func (a *App) Items(ctx context.Context, source, category string) ([]model.Item, error) {
	if a.Demo {
		return a.demoItems(source, category)
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if source == "overview" {
		sources, err := a.Sources(ctx)
		if err != nil {
			return nil, err
		}
		var rows []model.Item
		for _, s := range sources {
			if s.ID != "overview" && s.ID != "all" {
				rows = append(rows, model.Item{ID: s.ID, Source: "overview", Name: s.Name, Kind: s.Kind, State: s.State, Subtitle: s.Summary, Fields: s.Fields})
			}
		}
		return rows, nil
	}
	if source == "all" {
		sources, err := a.Sources(ctx)
		if err != nil {
			return nil, err
		}
		var rows []model.Item
		for _, s := range sources {
			cats := []string{}
			if s.Kind == "bitwarden" {
				cats = []string{"all"}
			}
			if s.Kind == "yubikey" {
				cats = []string{"oath", "fido", "piv", "openpgp"}
			}
			for _, cat := range cats {
				items, readErr := a.Items(ctx, s.ID, cat)
				if readErr != nil {
					rows = append(rows, model.Item{ID: "coverage:" + s.ID + ":" + cat, Source: s.ID, Kind: "coverage", Name: s.Name + " / " + cat, State: model.Code(readErr), Subtitle: readErr.Error()})
				} else {
					rows = append(rows, items...)
				}
			}
		}
		return rows, nil
	}
	if source == "bitwarden" {
		session := a.Session()
		if session == "" {
			return nil, model.Fail("locked", "Unlock Bitwarden or supply an existing BW_SESSION.")
		}
		raw, err := backend.BW(ctx, a.Runner, a.Config.BW, session, "list", "items")
		if err != nil {
			return nil, err
		}
		var items []backend.BWItem
		if json.Unmarshal(raw, &items) != nil {
			return nil, model.Fail("protocol_error", "Invalid vault list; raw output suppressed.")
		}
		rows := make([]model.Item, 0, len(items))
		for _, i := range items {
			row := backend.BWSummary(i)
			if category == "all" || category == "status" || category == row.Kind {
				rows = append(rows, row)
			}
		}
		return rows, nil
	}
	if strings.HasPrefix(source, "reader:") {
		return nil, model.Fail("unavailable", "Reader is visible but card cannot be opened. Touch once if configured for touch-eject, then refresh.")
	}
	if category == "all" {
		category = "status"
	}
	var rows []model.Item
	err := a.yubi(ctx, source, backend.YubiRequest{Op: "items", Category: category, Auth: a.Auth(source, category)}, &rows)
	for i := range rows {
		rows[i].Source = source
	}
	return rows, err
}
func (a *App) Detail(ctx context.Context, item model.Item) (model.Item, error) {
	if a.Demo {
		return a.demoDetail(item)
	}
	if item.Source != "bitwarden" {
		return item, nil
	}
	session := a.Session()
	if session == "" {
		return model.Item{}, model.Fail("locked", "Bitwarden is locked.")
	}
	native, err := backend.BWGet(ctx, a.Runner, a.Config.BW, session, item.ID)
	if err != nil {
		return model.Item{}, err
	}
	return backend.BWDetail(native), nil
}
func (a *App) RefreshBW(ctx context.Context) error {
	if a.Demo {
		return nil
	}
	if a.Session() == "" {
		return model.Fail("locked", "Unlock Bitwarden before syncing.")
	}
	_, err := backend.BW(ctx, a.Runner, a.Config.BW, a.Session(), "sync")
	return err
}
func (a *App) Value(ctx context.Context, item model.Item, field string, confirm string) (model.Field, error) {
	detail, err := a.Detail(ctx, item)
	if err != nil {
		return model.Field{}, err
	}
	f, ok := detail.Field(field)
	if !ok || !f.Copyable {
		return model.Field{}, model.Fail("invalid", "This field cannot be copied or revealed.")
	}
	if detail.State == "reprompt_required" && f.Secret {
		return model.Field{}, model.Fail("reprompt_required", "This item requires a master-password re-prompt. Use the official Bitwarden app for secret fields in v0.1.")
	}
	if field != "otp" {
		return f, nil
	}
	if a.Demo {
		f.Value = "123456"
		return f, nil
	}
	if item.Source == "bitwarden" {
		raw, err := backend.BW(ctx, a.Runner, a.Config.BW, a.Session(), "--raw", "get", "totp", item.ID)
		if err != nil {
			return f, err
		}
		f.Value = strings.TrimSpace(string(raw))
		return f, nil
	}
	hotp := false
	for _, v := range detail.Fields {
		if v.ID == "oath_type" && v.Value == "HOTP" {
			hotp = true
		}
	}
	if hotp && a.ReadOnly {
		return f, model.Fail("read_only", "HOTP counter advancement is disabled in read-only mode.")
	}
	var result struct {
		Value string `json:"value"`
	}
	err = a.yubi(ctx, item.Source, backend.YubiRequest{Op: "code", Category: "oath", ID: item.ID, Auth: a.Auth(item.Source, "oath"), Confirm: confirm}, &result)
	f.Value = result.Value
	return f, err
}

var writeCategories = map[string]string{"oath-add": "oath", "oath-delete": "oath", "oath-password": "oath", "fido-delete": "fido", "fido-pin": "fido", "piv-pin": "piv", "openpgp-pin": "openpgp"}

func (a *App) Write(ctx context.Context, source, operation, id, name, secret, current, confirm string) (string, error) {
	if a.ReadOnly {
		return "", model.Fail("read_only", "Management actions are disabled in read-only mode.")
	}
	category, ok := writeCategories[operation]
	if !ok {
		return "", model.Fail("invalid", "Unsupported management action.")
	}
	s, err := serial(source)
	if err != nil {
		return "", err
	}
	if s != confirm {
		return "", model.Fail("confirm_required", "Type the exact target serial to confirm.")
	}
	if a.Demo {
		return a.demoWrite(source, operation, id, name)
	}
	auth := current
	if auth == "" {
		auth = a.Auth(source, category)
	}
	request := backend.YubiRequest{Op: operation, Category: category, ID: id, Name: name, Auth: auth, Confirm: confirm, Touch: true}
	if operation == "oath-add" {
		request.Seed = secret
	} else {
		request.NewSecret = secret
	}
	var response struct {
		Message string `json:"message"`
	}
	err = a.yubi(ctx, source, request, &response)
	// PIN changes and uncertain writes invalidate any remembered application secret.
	if strings.HasSuffix(operation, "pin") || operation == "oath-password" || err != nil {
		a.forgetAuth(source, category)
	}
	return response.Message, err
}
func (a *App) Transfer(ctx context.Context, id, target, name, password, confirm string, preview bool) (string, error) {
	if a.ReadOnly && !preview {
		return "", model.Fail("read_only", "Transfers are disabled in read-only mode.")
	}
	s, err := serial(target)
	if err != nil {
		return "", err
	}
	if !preview && confirm != s {
		return "", model.Fail("confirm_required", "Type the exact destination serial to confirm.")
	}
	if a.Demo {
		if preview {
			return "Demo preview: source retained; destination available.", nil
		}
		return a.demoWrite(target, "oath-add", "", name)
	}
	item, err := backend.BWGet(ctx, a.Runner, a.Config.BW, a.Session(), id)
	if err != nil {
		return "", err
	}
	if item.Reprompt != 0 {
		return "", model.Fail("reprompt_required", "Use the official Bitwarden app for items requiring a master-password re-prompt.")
	}
	if item.OrganizationID != "" || item.Login == nil || item.Login.TOTP == "" {
		return "", model.Fail("invalid", "Select a personal login with a standard TOTP seed.")
	}
	if name == "" {
		name = item.Login.Username
		if name == "" {
			name = item.Name
		}
	}
	seed := item.Login.TOTP
	if !strings.Contains(seed, "://") {
		seed = "otpauth://totp/" + url.PathEscape(item.Name+":"+name) + "?" + url.Values{"secret": {seed}, "issuer": {item.Name}}.Encode()
	}
	operation := "oath-add"
	if preview {
		operation = "oath-preview"
	}
	auth := password
	if auth == "" {
		auth = a.Auth(target, "oath")
	}
	var result struct {
		Message string `json:"message"`
	}
	err = a.yubi(ctx, target, backend.YubiRequest{Op: operation, Category: "oath", Name: name, Seed: seed, Auth: auth, Confirm: s, Touch: true}, &result)
	return result.Message, err
}
func (a *App) Setup(ctx context.Context) error {
	if a.ReadOnly {
		return model.Fail("read_only", "Setup is disabled in read-only mode.")
	}
	if a.Demo {
		return nil
	}
	return backend.Prepare(ctx, a.Runner, a.Config.UV)
}
func (a *App) Diagnostic() map[string]any {
	return map[string]any{"demo": a.Demo, "read_only": a.ReadOnly, "config": a.Config, "secrets_persisted": false, "update": "go install github.com/daviddwlee84/lazycred/cmd/lazycred@latest"}
}
func (a *App) ValidateSource(source string) error {
	if source == "overview" || source == "bitwarden" {
		return nil
	}
	_, err := serial(source)
	return err
}
func (a *App) String() string {
	return fmt.Sprintf("lazycred (demo=%t, read-only=%t)", a.Demo, a.ReadOnly)
}
