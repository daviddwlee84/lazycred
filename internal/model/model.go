package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Field always redacts secret values when marshalled, including nested JSON.
type Field struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Value    string `json:"-"`
	Secret   bool   `json:"secret,omitempty"`
	Copyable bool   `json:"copyable,omitempty"`
}

func (f Field) MarshalJSON() ([]byte, error) {
	value := f.Value
	if f.Secret {
		value = "[redacted]"
	}
	return json.Marshal(struct {
		ID       string `json:"id"`
		Label    string `json:"label"`
		Value    string `json:"value"`
		Secret   bool   `json:"secret,omitempty"`
		Copyable bool   `json:"copyable,omitempty"`
	}{f.ID, f.Label, value, f.Secret, f.Copyable})
}
func (f *Field) UnmarshalJSON(data []byte) error {
	var v struct {
		ID, Label, Value string
		Secret, Copyable bool
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*f = Field{v.ID, v.Label, v.Value, v.Secret, v.Copyable}
	return nil
}

type Source struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Name    string  `json:"name"`
	State   string  `json:"state"`
	Summary string  `json:"summary,omitempty"`
	Fields  []Field `json:"fields,omitempty"`
}
type Item struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Subtitle string  `json:"subtitle,omitempty"`
	State    string  `json:"state,omitempty"`
	Fields   []Field `json:"fields,omitempty"`
}

func (i Item) Field(id string) (Field, bool) {
	for _, f := range i.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return Field{}, false
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string        { return e.Message }
func Fail(code, message string) error { return &Error{code, message} }
func Code(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return "failed"
}
func Safe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\x1b' {
			return ' '
		}
		return r
	}, s)
}
func Label(s Source) string { return fmt.Sprintf("%s · %s", Safe(s.Name), s.State) }
