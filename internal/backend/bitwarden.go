package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/daviddwlee84/lazycred/internal/model"
	"regexp"
	"strings"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type BWItem struct {
	Reprompt       int    `json:"reprompt"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           int    `json:"type"`
	OrganizationID string `json:"organizationId"`
	Notes          string `json:"notes"`
	Login          *struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		URIs     []struct {
			URI string `json:"uri"`
		} `json:"uris"`
		Passkeys []struct {
			CredentialID string `json:"credentialId"`
			RPID         string `json:"rpId"`
			UserName     string `json:"userName"`
		} `json:"fido2Credentials"`
	} `json:"login"`
	Card *struct {
		CardholderName string `json:"cardholderName"`
		Number         string `json:"number"`
		Code           string `json:"code"`
		ExpMonth       string `json:"expMonth"`
		ExpYear        string `json:"expYear"`
	} `json:"card"`
	Identity *struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
		Email     string `json:"email"`
		Username  string `json:"username"`
	} `json:"identity"`
	SSH *struct {
		PublicKey   string `json:"publicKey"`
		PrivateKey  string `json:"privateKey"`
		Fingerprint string `json:"keyFingerprint"`
	} `json:"sshKey"`
	Fields []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
		Type  int    `json:"type"`
	} `json:"fields"`
}

func BWSummary(item BWItem) model.Item {
	kind := map[int]string{1: "login", 2: "note", 3: "card", 4: "identity", 5: "ssh"}[item.Type]
	if kind == "" {
		kind = "item"
	}
	result := model.Item{ID: item.ID, Source: "bitwarden", Name: item.Name, Kind: kind, State: "ready"}
	if item.Reprompt != 0 {
		result.State = "reprompt_required"
	}
	if item.Login != nil {
		result.Subtitle = item.Login.Username
	}
	return result
}
func BWDetail(item BWItem) model.Item {
	result := BWSummary(item)
	add := func(id, label, value string, secret bool) {
		if value != "" {
			result.Fields = append(result.Fields, model.Field{ID: id, Label: label, Value: value, Secret: secret, Copyable: true})
		}
	}
	add("name", "Name", item.Name, false)
	add("organization", "Organization", item.OrganizationID, false)
	if l := item.Login; l != nil {
		add("username", "Username", l.Username, false)
		add("password", "Password", l.Password, true)
		for i, uri := range l.URIs {
			add(fmt.Sprintf("uri_%d", i), "URL", uri.URI, false)
		}
		if l.TOTP != "" {
			result.Fields = append(result.Fields, model.Field{ID: "otp", Label: "OTP (generate on demand)", Secret: true, Copyable: true})
		}
		for i, p := range l.Passkeys {
			add(fmt.Sprintf("passkey_%d", i), "Passkey metadata", p.RPID+" · "+p.UserName+" · "+p.CredentialID, false)
		}
	}
	if c := item.Card; c != nil {
		add("cardholder", "Cardholder", c.CardholderName, false)
		add("number", "Card number", c.Number, true)
		add("cvv", "CVV", c.Code, true)
		add("expiry", "Expiry", c.ExpMonth+"/"+c.ExpYear, false)
	}
	if s := item.SSH; s != nil {
		add("public_key", "Public key", s.PublicKey, false)
		add("private_key", "Private key", s.PrivateKey, true)
		add("fingerprint", "Fingerprint", s.Fingerprint, false)
	}
	if i := item.Identity; i != nil {
		add("full_name", "Full name", strings.TrimSpace(i.FirstName+" "+i.LastName), false)
		add("email", "Email", i.Email, false)
		add("username", "Username", i.Username, false)
	}
	add("notes", "Notes", item.Notes, true)
	for i, f := range item.Fields {
		add(fmt.Sprintf("custom_%d", i), f.Name, f.Value, f.Type == 1)
	}
	return result
}

func BW(ctx context.Context, r Runner, path, session string, args ...string) ([]byte, error) {
	env := map[string]string{}
	if session != "" {
		env["BW_SESSION"] = session
	}
	return r.Run(ctx, path, append([]string{"--nointeraction"}, args...), nil, env)
}
func BWStatus(ctx context.Context, r Runner, path, session string) model.Source {
	source := model.Source{ID: "bitwarden", Kind: "bitwarden", Name: "Bitwarden", State: "unavailable"}
	data, err := BW(ctx, r, path, session, "status")
	if err != nil {
		source.Summary = "Install bw and log in with the official CLI."
		return source
	}
	var status struct {
		Status    string `json:"status"`
		ServerURL string `json:"serverUrl"`
		LastSync  string `json:"lastSync"`
		UserEmail string `json:"userEmail"`
	}
	if json.Unmarshal(data, &status) != nil {
		source.Summary = "Invalid bw status response"
		return source
	}
	source.State = status.Status
	source.Summary = status.UserEmail
	if status.Status == "unauthenticated" {
		source.Summary = "Run bw login with the official CLI, then refresh."
	}
	source.Fields = []model.Field{{ID: "server", Label: "Server", Value: status.ServerURL}, {ID: "last_sync", Label: "Last sync", Value: status.LastSync}, {ID: "state", Label: "State", Value: status.Status}}
	return source
}
func BWGet(ctx context.Context, r Runner, path, session, id string) (BWItem, error) {
	var item BWItem
	if !uuid.MatchString(id) {
		return item, model.Fail("invalid", "Use a complete Bitwarden item UUID, not a name search.")
	}
	raw, err := BW(ctx, r, path, session, "get", "item", id)
	if err != nil {
		return item, err
	}
	if json.Unmarshal(raw, &item) != nil || !strings.EqualFold(item.ID, id) {
		return BWItem{}, model.Fail("protocol_error", "Invalid item response; raw output suppressed.")
	}
	return item, nil
}
