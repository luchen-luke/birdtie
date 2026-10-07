// Package nowcontextselection is an explicit human view selector. Neither a
// descriptive Context nor a sealed selection is a model or business permit.
package nowcontextselection

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"time"
)

const Schema = "now-context-selection-v1"
const Limit = 100
const TTL = 90 * time.Second

var ErrInvalid = errors.New("invalid context selection")
var ErrDenied = errors.New("context selection denied")
var ErrConflict = errors.New("context selection frame changed")
var ErrUnavailable = errors.New("context selection unavailable")

type Access struct {
	Actor  identity.Actor
	Digest [32]byte
}

func (a Access) Valid() bool {
	return a.Actor.AccountType == "person" && businessconsole.ValidID(a.Actor.ID) && a.Digest != [32]byte{}
}
func (Access) MarshalJSON() ([]byte, error) { return nil, ErrDenied }

type Option struct {
	OptionID    string `json:"optionId"`
	ContextID   string `json:"contextId"`
	ContextType string `json:"contextType"`
	CityID      string `json:"cityId"`
	Label       string `json:"label"`
	Relation    string `json:"relation"`
	ViewMode    string `json:"viewMode"`
	Declared    bool   `json:"declared"`
	QueryRoute  string `json:"queryRoute"`
}
type Envelope struct {
	SchemaVersion string                `json:"schemaVersion"`
	Owner         actorref.PrincipalRef `json:"owner"`
	AgentID       string                `json:"agentId"`
	ObservedAt    time.Time             `json:"observedAt"`
	ExpiresAt     time.Time             `json:"expiresAt"`
	ViewOnly      bool                  `json:"viewOnly"`
	ModelAccess   bool                  `json:"modelAccess"`
	SendAllowed   bool                  `json:"sendAllowed"`
}
type Options struct {
	Envelope
	OptionsToken string   `json:"optionsToken"`
	Items        []Option `json:"items"`
	Limit        int      `json:"limit"`
	Truncated    bool     `json:"truncated"`
}
type Selection struct {
	Envelope
	Choice Option `json:"choice"`
}
type Input struct {
	OptionsToken string `json:"optionsToken"`
	OptionID     string `json:"optionId"`
}
type OptionsReceipt struct {
	Response Options
	Proof    string
}
type SelectionReceipt struct {
	Response Selection
	Proof    string
}

func (OptionsReceipt) MarshalJSON() ([]byte, error)   { return nil, ErrDenied }
func (SelectionReceipt) MarshalJSON() ([]byte, error) { return nil, ErrDenied }

type Gateway interface {
	ReadOptions(context.Context, Access) (OptionsReceipt, error)
	Resolve(context.Context, Access, Input) (SelectionReceipt, error)
	RevalidateOptions(context.Context, Access, OptionsReceipt) error
	RevalidateSelection(context.Context, Access, SelectionReceipt) error
}

func Decode(raw []byte) (Input, error) {
	var v Input
	b, e := businessconsole.StrictObject(raw, "optionsToken", "optionId")
	if e != nil || json.Unmarshal(b, &v) != nil || ValidateInput(v) != nil {
		return v, ErrInvalid
	}
	return v, nil
}
func hexID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, v := range s {
		if !(v >= 'a' && v <= 'f' || v >= '0' && v <= '9') {
			return false
		}
	}
	return true
}
func Token(s string) bool {
	return len(s) >= 80 && len(s) <= 12000 && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\r\n\x00 ")
}
func ValidateInput(v Input) error {
	if !Token(v.OptionsToken) || !hexID(v.OptionID) {
		return ErrInvalid
	}
	return nil
}
func ValidateOption(v Option) error {
	if !hexID(v.OptionID) || !businessconsole.ValidID(v.ContextID) || strings.TrimSpace(v.Label) != v.Label || len([]rune(v.Label)) < 1 || len([]rune(v.Label)) > 160 || strings.ContainsAny(v.Label, "\r\n\x00") {
		return ErrInvalid
	}
	if v.ContextType == "CITY" {
		if v.CityID == "" || len(v.CityID) > 160 || v.QueryRoute != "CITY" {
			return ErrInvalid
		}
	} else if v.CityID != "" {
		return ErrInvalid
	}
	if !v.Declared {
		if v.ContextType != "CITY" || v.Relation != "" || v.ViewMode != "DESTINATION" {
			return ErrInvalid
		}
		return nil
	}
	switch v.Relation {
	case "current", "destination", "past", "home", "affiliation", "interest":
	default:
		return ErrInvalid
	}
	switch v.ContextType {
	case "CITY":
		if v.ViewMode != strings.ToUpper(v.Relation) || !(v.Relation == "current" || v.Relation == "destination" || v.Relation == "past" || v.Relation == "home") {
			return ErrInvalid
		}
	case "ONLINE":
		if v.ViewMode != "ONLINE" {
			return ErrInvalid
		}
		route := "UNAVAILABLE"
		if v.Relation == "interest" || v.Relation == "current" || v.Relation == "affiliation" {
			route = "ONLINE"
		}
		if v.QueryRoute != route && v.QueryRoute != "UNAVAILABLE" {
			return ErrInvalid
		}
	case "INSTITUTION", "COUNTRY", "COMMUNITY":
		if v.ViewMode != strings.ToUpper(v.Relation) || v.QueryRoute != "UNAVAILABLE" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func ValidateEnvelope(v Envelope, owner string) error {
	if v.SchemaVersion != Schema || v.Owner.Type != actorref.Person || v.Owner.ID != owner || !businessconsole.ValidID(owner) || !businessconsole.ValidID(v.AgentID) || v.ObservedAt.IsZero() || !v.ExpiresAt.After(v.ObservedAt) || v.ExpiresAt.Sub(v.ObservedAt) > TTL || !v.ViewOnly || v.ModelAccess || v.SendAllowed {
		return ErrInvalid
	}
	return nil
}
func ValidateOptions(v Options, owner string) error {
	if ValidateEnvelope(v.Envelope, owner) != nil || !Token(v.OptionsToken) || v.Items == nil || len(v.Items) > Limit || v.Limit != Limit {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, x := range v.Items {
		if ValidateOption(x) != nil || seen[x.OptionID] {
			return ErrInvalid
		}
		seen[x.OptionID] = true
	}
	return nil
}
