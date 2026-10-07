// Package agentpolicysettings owns persisted human preference DTOs. Settings
// are not consent, source authority, processing flags or concrete approvals.
package agentpolicysettings

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentattention"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentsocialpolicy"
)

const SchemaVersion = "agent-policy-settings-v1"
const MaxBodyBytes = 8 * 1024
const MaxValidity = 30 * 24 * time.Hour
const MaxRevision = int64(math.MaxInt64)

type Family string

const (
	Attention Family = "ATTENTION"
	Social    Family = "SOCIAL"
	Autonomy  Family = "AUTONOMY"
)

var (
	ErrInvalid     = errors.New("策略设置无效")
	ErrForbidden   = errors.New("策略设置访问被拒绝")
	ErrNotFound    = errors.New("策略身份不可用")
	ErrConflict    = errors.New("策略设置已更新")
	ErrUnavailable = errors.New("策略设置暂不可用")
)

func Families() []Family        { return []Family{Attention, Social, Autonomy} }
func ValidFamily(f Family) bool { return f == Attention || f == Social || f == Autonomy }

type AttentionRule struct {
	EventType agentevent.Type      `json:"eventType"`
	Route     agentattention.Route `json:"route"`
}
type AttentionSettings struct {
	DefaultRoute agentattention.Route `json:"defaultRoute"`
	Rules        []AttentionRule      `json:"rules"`
	PauseUntil   *time.Time           `json:"pauseUntil,omitempty"`
}
type SocialRule struct {
	Category   agentsocialpolicy.Category   `json:"category"`
	Preference agentsocialpolicy.Preference `json:"preference"`
}
type SocialSettings struct {
	Rules []SocialRule `json:"rules"`
}
type AutonomySettings struct {
	Level agentautonomy.Level `json:"level"`
}
type PutInput struct {
	ExpectedVersion int64           `json:"expectedVersion"`
	Settings        json.RawMessage `json:"settings"`
	ExpiresAt       time.Time       `json:"expiresAt"`
}
type Record struct {
	Family         Family          `json:"family"`
	Configured     bool            `json:"configured"`
	NativeRevision int64           `json:"nativeRevision"`
	Status         string          `json:"status"`
	Settings       json.RawMessage `json:"settings"`
	ValidFrom      *time.Time      `json:"validFrom,omitempty"`
	ExpiresAt      *time.Time      `json:"expiresAt,omitempty"`
	UpdatedAt      *time.Time      `json:"updatedAt,omitempty"`
}
type Bundle struct {
	SchemaVersion string        `json:"schemaVersion"`
	OwnerType     actorref.Type `json:"ownerType"`
	OwnerID       string        `json:"ownerId"`
	AgentID       string        `json:"agentId"`
	ObservedAt    time.Time     `json:"observedAt"`
	Attention     Record        `json:"attention"`
	Social        Record        `json:"social"`
	Autonomy      Record        `json:"autonomy"`
}
type Store interface {
	GetOwnPolicies(context.Context, agentprofile.PrivateAccess) (Bundle, error)
	PutOwnPolicy(context.Context, agentprofile.PrivateAccess, Family, PutInput) (Bundle, error)
}

func ValidTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 && t.Equal(t.Truncate(time.Microsecond))
}
func NormalizeInput(f Family, in PutInput, now time.Time) (PutInput, error) {
	if !ValidFamily(f) || in.ExpectedVersion < 0 || in.ExpectedVersion == MaxRevision || !ValidTime(now) || !ValidTime(in.ExpiresAt) || !in.ExpiresAt.After(now) || in.ExpiresAt.Sub(now) > MaxValidity {
		return PutInput{}, ErrInvalid
	}
	in.ExpiresAt = in.ExpiresAt.UTC()
	normalized, err := NormalizeSettings(f, in.Settings, now.UTC(), in.ExpiresAt)
	if err != nil {
		return PutInput{}, err
	}
	in.Settings = normalized
	return in, nil
}
func NormalizeSettings(f Family, raw json.RawMessage, from, expires time.Time) (json.RawMessage, error) {
	if !ValidFamily(f) || !ValidTime(from) || !ValidTime(expires) || !expires.After(from) || expires.Sub(from) > MaxValidity {
		return nil, ErrInvalid
	}
	object, err := strictObject(raw)
	if err != nil {
		return nil, err
	}
	switch f {
	case Attention:
		if !hasKeys(object, []string{"defaultRoute", "rules"}, []string{"pauseUntil"}) {
			return nil, ErrInvalid
		}
		var input AttentionSettings
		if json.Unmarshal(raw, &input) != nil {
			return nil, ErrInvalid
		}
		ruleObjects, err := strictArray(object["rules"])
		if err != nil {
			return nil, err
		}
		if len(ruleObjects) != len(input.Rules) {
			return nil, ErrInvalid
		}
		rules := make([]agentattention.Rule, 0, len(input.Rules))
		for i, r := range input.Rules {
			if !hasKeys(ruleObjects[i], []string{"eventType", "route"}, nil) {
				return nil, ErrInvalid
			}
			rules = append(rules, agentattention.Rule{EventType: r.EventType, Route: r.Route})
		}
		pause := time.Time{}
		if input.PauseUntil != nil {
			if !ValidTime(*input.PauseUntil) {
				return nil, ErrInvalid
			}
			pause = input.PauseUntil.UTC()
			input.PauseUntil = &pause
		}
		if agentattention.ValidateSettingsSpecification(agentattention.Specification{DefaultRoute: input.DefaultRoute, Rules: rules, ValidFrom: from, ExpiresAt: expires, PauseUntil: pause}) != nil {
			return nil, ErrInvalid
		}
		if input.Rules == nil {
			input.Rules = []AttentionRule{}
		}
		sort.Slice(input.Rules, func(i, j int) bool { return input.Rules[i].EventType < input.Rules[j].EventType })
		return json.Marshal(input)
	case Social:
		if !hasKeys(object, []string{"rules"}, nil) {
			return nil, ErrInvalid
		}
		var input SocialSettings
		if json.Unmarshal(raw, &input) != nil {
			return nil, ErrInvalid
		}
		objects, err := strictArray(object["rules"])
		if err != nil || len(objects) != len(input.Rules) {
			return nil, ErrInvalid
		}
		specRules := make([]agentsocialpolicy.Rule, 0, len(input.Rules))
		byCategory := map[agentsocialpolicy.Category]agentsocialpolicy.Preference{}
		for i, r := range input.Rules {
			if !hasKeys(objects[i], []string{"category", "preference"}, nil) {
				return nil, ErrInvalid
			}
			specRules = append(specRules, agentsocialpolicy.Rule{Category: r.Category, Preference: r.Preference})
			byCategory[r.Category] = r.Preference
		}
		if agentsocialpolicy.ValidateSettingsSpecification(agentsocialpolicy.Specification{Rules: specRules, ValidFrom: from, ExpiresAt: expires}) != nil {
			return nil, ErrInvalid
		}
		input.Rules = []SocialRule{}
		for _, c := range agentsocialpolicy.Categories() {
			p := byCategory[c]
			if p == "" {
				p = agentsocialpolicy.Disabled
			}
			input.Rules = append(input.Rules, SocialRule{Category: c, Preference: p})
		}
		return json.Marshal(input)
	case Autonomy:
		if !hasKeys(object, []string{"level"}, nil) {
			return nil, ErrInvalid
		}
		var input AutonomySettings
		if json.Unmarshal(raw, &input) != nil {
			return nil, ErrInvalid
		}
		if agentautonomy.ValidateSettingsSpecification(agentautonomy.Specification{Level: input.Level, ValidFrom: from, ExpiresAt: expires}, from) != nil {
			return nil, ErrInvalid
		}
		return json.Marshal(input)
	}
	return nil, ErrInvalid
}
func DefaultRecord(f Family) Record {
	var settings []byte
	switch f {
	case Attention:
		settings, _ = json.Marshal(AttentionSettings{DefaultRoute: agentattention.Block, Rules: []AttentionRule{}})
	case Social:
		rules := []SocialRule{}
		for _, c := range agentsocialpolicy.Categories() {
			rules = append(rules, SocialRule{c, agentsocialpolicy.Disabled})
		}
		settings, _ = json.Marshal(SocialSettings{rules})
	case Autonomy:
		settings, _ = json.Marshal(AutonomySettings{agentautonomy.LevelObserve})
	}
	return Record{Family: f, Status: "UNCONFIGURED", Settings: settings}
}
func ValidateRecord(r Record, observed time.Time) error {
	if !ValidTime(observed) || !ValidFamily(r.Family) {
		return ErrInvalid
	}
	if !r.Configured {
		d := DefaultRecord(r.Family)
		if r.NativeRevision != 0 || r.Status != "UNCONFIGURED" || r.ValidFrom != nil || r.ExpiresAt != nil || r.UpdatedAt != nil || string(r.Settings) != string(d.Settings) {
			return ErrInvalid
		}
		return nil
	}
	if r.NativeRevision < 1 || r.ValidFrom == nil || r.ExpiresAt == nil || r.UpdatedAt == nil || !ValidTime(*r.UpdatedAt) || !r.UpdatedAt.Equal(*r.ValidFrom) || r.ValidFrom.After(observed) {
		return ErrInvalid
	}
	normalized, err := NormalizeSettings(r.Family, r.Settings, *r.ValidFrom, *r.ExpiresAt)
	if err != nil || !jsonSemanticEqual(normalized, r.Settings) {
		return ErrInvalid
	}
	want := "ACTIVE"
	if !observed.Before(*r.ExpiresAt) {
		want = "EXPIRED"
	}
	if r.Status != want {
		return ErrInvalid
	}
	return nil
}
func ValidateBundle(b Bundle) error {
	owner, e := actorref.ParsePrincipal("PERSON", b.OwnerID)
	agent, a := actorref.ParsePrincipal("PERSON", b.AgentID)
	if e != nil || a != nil || owner.ID != b.OwnerID || agent.ID != b.AgentID || b.OwnerID == "00000000-0000-0000-0000-000000000000" || b.AgentID == "00000000-0000-0000-0000-000000000000" || b.OwnerType != actorref.Person || b.SchemaVersion != SchemaVersion || !ValidTime(b.ObservedAt) || b.Attention.Family != Attention || b.Social.Family != Social || b.Autonomy.Family != Autonomy {
		return ErrInvalid
	}
	for _, r := range []Record{b.Attention, b.Social, b.Autonomy} {
		if ValidateRecord(r, b.ObservedAt) != nil {
			return ErrInvalid
		}
	}
	return nil
}
func jsonSemanticEqual(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflectJSONEqual(x, y)
}
func reflectJSONEqual(x, y any) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}
