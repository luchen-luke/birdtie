// Package agentcontextbuilder assembles bounded native views for current rules
// queries or explicit human review. Neither mode grants cognitive/model access.
package agentcontextbuilder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

const SchemaVersion = "agent-context-builder-v1"
const MaxLease = 5 * time.Minute
const MaxDeadline = 15 * time.Minute

type Mode string

const RulesPublicQuery Mode = "RULES_PUBLIC_QUERY"
const HumanSelfReview Mode = "HUMAN_SELF_REVIEW"

// This local native read has its own concrete Task/field/source approval. It
// does not authorize a model provider, a Memory write or any external action.
const MachineTaskContext Mode = "MACHINE_TASK_CONTEXT"

type Selection string

const ActivitySearch Selection = "ACTIVITY_SEARCH"
const PlaceSearch Selection = "PLACE_SEARCH"
const ExactSelfReview Selection = "EXACT_SELF_REVIEW"
const ExactTaskContext Selection = "EXACT_TASK_CONTEXT"

var (
	ErrInvalid     = errors.New("上下文请求无效")
	ErrDenied      = errors.New("当前上下文不可读取或已失效")
	ErrExpired     = errors.New("上下文已过期，请重新读取")
	ErrUnavailable = errors.New("该用途的上下文尚不可用")
	ErrServerOnly  = errors.New("上下文控制对象仅供服务端使用")
)

type Request struct {
	Access                                          agentprofile.PrivateAccess
	Agent                                           agentcognitive.AgentReference
	TaskID, RequestID, CityID, CurrentQuery         string
	TaskUpdatedAt                                   time.Time
	Selection                                       Selection
	Mode                                            Mode
	ActivityIDs, PlaceIDs, ProfileFields, MemoryIDs []string
	PolicyFamilies                                  []agentpolicysettings.Family
	Relationships                                   bool
	PurposeGrantID                                  string
	PurposeDeadlineAt                               time.Time
	RelationshipTieIDs                              []string
	DeadlineAt                                      time.Time
}

func (Request) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Request) UnmarshalJSON([]byte) error { *r = Request{}; return ErrServerOnly }
func validID(id string) bool {
	ref, e := actorref.ParsePrincipal("PERSON", id)
	return e == nil && ref.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func validText(s string, max int, empty bool) bool {
	return (empty || s != "") && len(s) <= max && utf8.ValidString(s) && s == strings.TrimSpace(s) && !strings.ContainsAny(s, "\x00\r\n")
}
func ValidTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func PrivateFieldKeys() []string {
	return []string{"personalPreferences", "socialPreferences", "availability", "preferredActivityTypes", "travelPreferences", "interactionPreferences", "privateCityHistory", "languagePreferences", "agentNotes"}
}
func hasKey(keys []string, k string) bool {
	for _, v := range keys {
		if v == k {
			return true
		}
	}
	return false
}
func uniqueIDs(ids []string, max int) bool {
	if len(ids) > max {
		return false
	}
	seen := map[string]bool{}
	for _, v := range ids {
		if !validID(v) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

// ValidateShape never treats a valid request or a human mode as a permission.
func ValidateShape(r Request) error {
	if agentprofile.ValidatePrivateAccess(r.Access) != nil || agentcognitive.ValidateAgentReference(r.Agent) != nil || r.Agent.Principal != r.Access.WorkspacePrincipal || r.Agent.Principal.Type != actorref.Person || r.Agent.Role != agentruntime.PersonalAgent {
		return ErrDenied
	}
	if !validText(r.RequestID, 100, false) || !ValidTime(r.DeadlineAt) {
		return ErrInvalid
	}
	if r.Mode != RulesPublicQuery && r.Mode != HumanSelfReview && r.Mode != MachineTaskContext {
		return ErrUnavailable
	}
	if r.Relationships {
		return ErrUnavailable
	}
	if r.Mode == MachineTaskContext {
		return validateMachineShape(r)
	}
	if r.PurposeGrantID != "" || !r.PurposeDeadlineAt.IsZero() || len(r.RelationshipTieIDs) != 0 {
		return ErrInvalid
	}
	if r.Mode == RulesPublicQuery {
		if len(r.ProfileFields)+len(r.MemoryIDs)+len(r.PolicyFamilies) > 0 {
			return ErrUnavailable
		}
		if !validID(r.TaskID) || !validText(r.CityID, 100, false) || !validText(r.CurrentQuery, 240, false) || !ValidTime(r.TaskUpdatedAt) {
			return ErrInvalid
		}
		switch r.Selection {
		case ActivitySearch:
			if len(r.PlaceIDs) > 0 || !uniqueIDs(r.ActivityIDs, 30) {
				return ErrInvalid
			}
		case PlaceSearch:
			if len(r.ActivityIDs) > 0 || !uniqueIDs(r.PlaceIDs, 100) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	} else {
		if r.Selection != ExactSelfReview || r.TaskID != "" || !r.TaskUpdatedAt.IsZero() || r.CityID != "" || r.CurrentQuery != "" || len(r.ActivityIDs)+len(r.PlaceIDs) > 0 {
			return ErrInvalid
		}
		if len(r.ProfileFields) > 3 || !uniqueIDs(r.MemoryIDs, 3) || len(r.PolicyFamilies) > 1 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, k := range r.ProfileFields {
			if !hasKey(PrivateFieldKeys(), k) || seen[k] {
				return ErrInvalid
			}
			seen[k] = true
		}
		for _, f := range r.PolicyFamilies {
			if !agentpolicysettings.ValidFamily(f) {
				return ErrInvalid
			}
		}
		if len(r.ProfileFields)+len(r.MemoryIDs)+len(r.PolicyFamilies) == 0 {
			return ErrInvalid
		}
	}
	return nil
}
func ValidateAt(r Request, now time.Time) error {
	if e := ValidateShape(r); e != nil {
		return e
	}
	if !ValidTime(now) || !r.DeadlineAt.After(now) || r.DeadlineAt.Sub(now) > MaxDeadline {
		return ErrExpired
	}
	return nil
}

type Source struct {
	Kind       string                   `json:"kind"`
	ID         string                   `json:"id"`
	Version    agentevent.SourceVersion `json:"version"`
	NativeTime time.Time                `json:"nativeTime"`
	RowToken   string                   `json:"rowToken,omitempty"` // Actual retained xmin, never an access or CAS grant.
}

// PublicVersion is a Context namespace fingerprint, not a new event catalog,
// monotonic counter, source permission or a CAS assertion.
func PublicVersion(kind string, at time.Time, currentProjection []byte) (agentevent.SourceVersion, error) {
	if !hasKey([]string{"PUBLIC_CITY", "PUBLIC_ACTIVITY", "PUBLIC_PLACE", "CURRENT_TASK_REQUEST"}, kind) || !ValidTime(at) || len(currentProjection) == 0 || len(currentProjection) > 64*1024 {
		return agentevent.SourceVersion{}, ErrInvalid
	}
	h := sha256.New()
	for _, p := range []string{"birdtie.context.current.v1", kind, at.UTC().Format(time.RFC3339Nano)} {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	h.Write(currentProjection)
	return agentevent.SourceVersion{Kind: agentevent.UpdatedAtDigestVersion, Token: hex.EncodeToString(h.Sum(nil))}, nil
}

type PublicActivity struct {
	ID                         string `json:"id"`
	Title                      string `json:"title"`
	Category                   string `json:"category"`
	StartsAt, EndsAt           time.Time
	OrganizerType, OrganizerID string
}
type PublicPlace struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}
type ReviewMemory struct {
	ID              string                      `json:"id"`
	Version         int64                       `json:"version"`
	MemoryKey       string                      `json:"memoryKey"`
	MemoryType      agentmemory.MemoryType      `json:"memoryType,omitempty"`
	Summary         string                      `json:"summary"`
	StructuredValue json.RawMessage             `json:"structuredValue"`
	ValidUntil      time.Time                   `json:"validUntil"`
	ValidFrom       *time.Time                  `json:"validFrom,omitempty"`
	CreatedAt       *time.Time                  `json:"createdAt,omitempty"`
	Confidence      *agentconfidence.Assessment `json:"confidence,omitempty"`
}

// Missing metadata remains unknown for older in-process fixtures. The native
// reader supplies the actual explicit-memory value; no inferred score is legal
// in this explicit-only Context namespace.
func validMemoryConfidence(a *agentconfidence.Assessment) bool {
	if a == nil {
		return true
	}
	_, e := agentconfidence.NormalizeAssessment(*a)
	return e == nil && a.Semantics == agentconfidence.DirectDeclaration
}

type Sections struct{ Profile, Memories, Places, Activities, Relationships, Policies string }
type ContextTie struct {
	ID            string `json:"id"`
	PeerAccountID string `json:"peerAccountId"`
	State         string `json:"state"`
}
type ContextCity struct {
	ID       string `json:"id"`
	TimeZone string `json:"timeZone"`
}
type ContextTask struct {
	ID        string    `json:"id"`
	Query     string    `json:"query"`
	UpdatedAt time.Time `json:"updatedAt"`
}
type Bundle struct {
	SchemaVersion                           string                        `json:"schemaVersion"`
	Agent                                   agentcognitive.AgentReference `json:"agent"`
	Mode                                    Mode                          `json:"mode"`
	RequestID, TaskID, CityID, CurrentQuery string
	ObservedAt, ExpiresAt                   time.Time
	Sources                                 []Source                     `json:"sources"`
	Sections                                Sections                     `json:"sections"`
	Places                                  []PublicPlace                `json:"places"`
	Activities                              []PublicActivity             `json:"activities"`
	Profile                                 map[string]json.RawMessage   `json:"profile,omitempty"`
	Memories                                []ReviewMemory               `json:"memories,omitempty"`
	Policies                                []agentpolicysettings.Record `json:"policies,omitempty"`
	Relationships                           []ContextTie                 `json:"relationships,omitempty"`
	City                                    *ContextCity                 `json:"city,omitempty"`
	Task                                    *ContextTask                 `json:"task,omitempty"`
	ModelAccess                             string                       `json:"modelAccess"`
	MemoryPromotionAllowed                  bool                         `json:"memoryPromotionAllowed"`
	FieldEvidenceSet                        *FieldEvidenceSet            `json:"fieldEvidenceSet,omitempty"`
}

// BuiltContext is an internal control object. Domain DTOs are only for the
// existing human rules response, never a second model-input envelope.
type BuiltContext struct {
	Bundle     Bundle
	Activities []foundation.Activity
	Places     []foundation.Place
	Authority  string
	Request    Request
	seal       []byte
}

func (BuiltContext) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (b *BuiltContext) UnmarshalJSON([]byte) error { *b = BuiltContext{}; return ErrServerOnly }

type Store interface {
	ResolveOwnContextAgent(context.Context, agentprofile.PrivateAccess) (agentcognitive.AgentReference, error)
	BuildOwnAgentContext(context.Context, Request) (BuiltContext, error)
}

// ValidateBuilt is a defense against an incorrect port result, not authority.
// The real native Store still verifies the current sources and session.
func ValidateBuilt(r Request, out BuiltContext) error {
	b := out.Bundle
	if ValidateExactFieldEvidence(b) != nil {
		return ErrUnavailable
	}
	if b.SchemaVersion != SchemaVersion || b.Agent != r.Agent || b.Mode != r.Mode || b.RequestID != r.RequestID || b.TaskID != r.TaskID || b.CityID != r.CityID || b.CurrentQuery != r.CurrentQuery || b.ModelAccess != "UNAVAILABLE" || b.MemoryPromotionAllowed || len(out.Authority) != 64 || !ValidTime(b.ObservedAt) || !b.ExpiresAt.After(b.ObservedAt) || b.ExpiresAt.Sub(b.ObservedAt) > MaxLease || b.ExpiresAt.After(r.DeadlineAt) {
		return ErrUnavailable
	}
	raw, e := json.Marshal(b)
	if e != nil || len(raw) > 64*1024 {
		return ErrUnavailable
	}
	if _, e = json.Marshal(struct {
		Activities []foundation.Activity
		Places     []foundation.Place
	}{out.Activities, out.Places}); e != nil {
		return ErrUnavailable
	}
	if _, e = hex.DecodeString(out.Authority); e != nil {
		return ErrUnavailable
	}
	if r.Mode == MachineTaskContext {
		return validateMachineBuilt(r, out)
	}
	if len(b.Relationships) != 0 || b.City != nil || b.Task != nil {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	sources := map[string]Source{}
	for _, source := range b.Sources {
		key := source.Kind + "\x00" + source.ID
		if source.ID == "" || seen[key] || !ValidTime(source.NativeTime) || source.NativeTime.After(b.ObservedAt) {
			return ErrUnavailable
		}
		seen[key] = true
		sources[key] = source
		if r.Mode == RulesPublicQuery {
			allowed := source.Kind == "PUBLIC_CITY" && source.ID == r.CityID || source.Kind == "CURRENT_TASK_REQUEST" && source.ID == r.TaskID || source.Kind == "PUBLIC_ACTIVITY" && hasKey(r.ActivityIDs, source.ID) || source.Kind == "PUBLIC_PLACE" && hasKey(r.PlaceIDs, source.ID)
			if !allowed || source.Version.Kind != agentevent.UpdatedAtDigestVersion || source.Version.Revision != 0 || len(source.Version.Token) != 64 {
				return ErrUnavailable
			}
			if _, e := hex.DecodeString(source.Version.Token); e != nil {
				return ErrUnavailable
			}
		} else {
			allowed := source.Kind == "HUMAN_PRIVATE_PROFILE" && source.ID == r.Agent.AgentID && len(r.ProfileFields) > 0 || source.Kind == "HUMAN_EXPLICIT_MEMORY" && hasKey(r.MemoryIDs, source.ID) || source.Kind == "HUMAN_POLICY_SETTINGS" && len(r.PolicyFamilies) == 1 && source.ID == r.Agent.AgentID+":"+string(r.PolicyFamilies[0])
			if !allowed || source.Version.Kind != agentevent.RevisionVersion || source.Version.Revision <= 0 || source.Version.Token != "" || source.RowToken == "" {
				return ErrUnavailable
			}
		}
	}
	if r.Mode == RulesPublicQuery {
		if len(b.Profile)+len(b.Memories)+len(b.Policies) > 0 || b.Sections.Profile != "NOT_REQUESTED" || b.Sections.Memories != "NOT_REQUESTED" || b.Sections.Policies != "NOT_REQUESTED" || b.Sections.Relationships != "UNAVAILABLE" || len(b.Sources) != 2+len(r.ActivityIDs)+len(r.PlaceIDs) {
			return ErrUnavailable
		}
		if len(b.Activities) != len(r.ActivityIDs) || len(out.Activities) != len(r.ActivityIDs) || len(b.Places) != len(r.PlaceIDs) || len(out.Places) != len(r.PlaceIDs) {
			return ErrUnavailable
		}
		for i, v := range b.Activities {
			if v.ID != r.ActivityIDs[i] || out.Activities[i].ID != v.ID || out.Activities[i].CityID != r.CityID || out.Activities[i].Visibility != "public" || v.Title != out.Activities[i].Title {
				return ErrUnavailable
			}
		}
		for i, v := range b.Places {
			if v.ID != r.PlaceIDs[i] || out.Places[i].ID != v.ID || out.Places[i].CityID != r.CityID || v.Name != out.Places[i].Name {
				return ErrUnavailable
			}
		}
	} else {
		if b.Sections.Activities != "NOT_REQUESTED" || b.Sections.Places != "NOT_REQUESTED" || b.Sections.Relationships != "UNAVAILABLE" {
			return ErrUnavailable
		}
		expectedSources := len(r.MemoryIDs)
		if len(r.ProfileFields) == 0 {
			if b.Sections.Profile != "NOT_REQUESTED" {
				return ErrUnavailable
			}
		} else if len(b.Profile) == 0 {
			if b.Sections.Profile != "UNCONFIGURED" {
				return ErrUnavailable
			}
		} else {
			if b.Sections.Profile != "AVAILABLE" || len(b.Profile) != len(r.ProfileFields) {
				return ErrUnavailable
			}
			expectedSources++
			if _, ok := sources["HUMAN_PRIVATE_PROFILE\x00"+r.Agent.AgentID]; !ok {
				return ErrUnavailable
			}
		}
		if len(r.MemoryIDs) == 0 {
			if b.Sections.Memories != "NOT_REQUESTED" {
				return ErrUnavailable
			}
		} else if b.Sections.Memories != "AVAILABLE" {
			return ErrUnavailable
		}
		if len(r.PolicyFamilies) == 0 && b.Sections.Policies != "NOT_REQUESTED" {
			return ErrUnavailable
		}
		if len(b.Activities)+len(b.Places)+len(out.Activities)+len(out.Places) > 0 || len(b.Profile) > len(r.ProfileFields) || len(b.Memories) != len(r.MemoryIDs) || len(b.Policies) != len(r.PolicyFamilies) {
			return ErrUnavailable
		}
		for key, value := range b.Profile {
			if !hasKey(r.ProfileFields, key) || !json.Valid(value) {
				return ErrUnavailable
			}
		}
		for i, v := range b.Memories {
			if v.ID != r.MemoryIDs[i] || v.Version <= 0 || !json.Valid(v.StructuredValue) || !v.ValidUntil.After(b.ObservedAt) || !validMemoryConfidence(v.Confidence) {
				return ErrUnavailable
			}
			if source, ok := sources["HUMAN_EXPLICIT_MEMORY\x00"+v.ID]; !ok || source.Version.Revision != v.Version {
				return ErrUnavailable
			}
		}
		for i, v := range b.Policies {
			if v.Family != r.PolicyFamilies[i] || agentpolicysettings.ValidateRecord(v, b.ObservedAt) != nil {
				return ErrUnavailable
			}
			if v.Configured {
				expectedSources++
				source, ok := sources["HUMAN_POLICY_SETTINGS\x00"+r.Agent.AgentID+":"+string(v.Family)]
				if !ok || source.Version.Revision != v.NativeRevision || b.Sections.Policies != "AVAILABLE" {
					return ErrUnavailable
				}
			} else if b.Sections.Policies != "UNCONFIGURED" {
				return ErrUnavailable
			}
		}
		if len(b.Sources) != expectedSources {
			return ErrUnavailable
		}
	}
	return nil
}

func cloneRequest(r Request) Request {
	r.ActivityIDs = append([]string(nil), r.ActivityIDs...)
	r.PlaceIDs = append([]string(nil), r.PlaceIDs...)
	r.ProfileFields = append([]string(nil), r.ProfileFields...)
	r.MemoryIDs = append([]string(nil), r.MemoryIDs...)
	r.PolicyFamilies = append([]agentpolicysettings.Family(nil), r.PolicyFamilies...)
	r.RelationshipTieIDs = append([]string(nil), r.RelationshipTieIDs...)
	return r
}
func cloneBuilt(in BuiltContext) BuiltContext {
	// Requests have intentionally denied JSON marshaling. Clone visible payloads
	// separately; no serialization can recreate a seal or native authority.
	raw, _ := json.Marshal(struct {
		Bundle     Bundle
		Activities []foundation.Activity
		Places     []foundation.Place
	}{in.Bundle, in.Activities, in.Places})
	var copied struct {
		Bundle     Bundle
		Activities []foundation.Activity
		Places     []foundation.Place
	}
	_ = json.Unmarshal(raw, &copied)
	return BuiltContext{copied.Bundle, copied.Activities, copied.Places, in.Authority, cloneRequest(in.Request), append([]byte(nil), in.seal...)}
}

func PurposeSelectionFromRequest(r Request) PurposeSelection {
	return PurposeSelection{AgentID: r.Agent.AgentID, TaskID: r.TaskID, CityID: r.CityID,
		CurrentQuery: r.CurrentQuery, TaskUpdatedAt: r.TaskUpdatedAt,
		ProfileFields: append([]string(nil), r.ProfileFields...), MemoryIDs: append([]string(nil), r.MemoryIDs...),
		PlaceIDs: append([]string(nil), r.PlaceIDs...), ActivityIDs: append([]string(nil), r.ActivityIDs...),
		RelationshipTieIDs: append([]string(nil), r.RelationshipTieIDs...),
		PolicyFamilies:     append([]agentpolicysettings.Family(nil), r.PolicyFamilies...), DeadlineAt: r.PurposeDeadlineAt}
}
func validateMachineShape(r Request) error {
	if r.Selection != ExactTaskContext || !validID(r.PurposeGrantID) || !ValidTime(r.PurposeDeadlineAt) || r.DeadlineAt.After(r.PurposeDeadlineAt) ||
		!validID(r.TaskID) || !ValidTime(r.TaskUpdatedAt) || !validText(r.CityID, 100, false) || !validText(r.CurrentQuery, 240, false) ||
		len(r.ProfileFields) > 3 || !uniqueIDs(r.MemoryIDs, 3) || !uniqueIDs(r.PlaceIDs, 5) || !uniqueIDs(r.ActivityIDs, 5) ||
		!uniqueIDs(r.RelationshipTieIDs, 3) || len(r.PolicyFamilies) > 1 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, key := range r.ProfileFields {
		if !hasKey(PrivateFieldKeys(), key) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
	}
	for _, family := range r.PolicyFamilies {
		if !agentpolicysettings.ValidFamily(family) {
			return ErrInvalid
		}
	}
	return nil
}
func validateMachineBuilt(r Request, out BuiltContext) error {
	b := out.Bundle
	if b.ExpiresAt.After(r.PurposeDeadlineAt) || b.City == nil || b.City.ID != r.CityID || b.City.TimeZone == "" ||
		b.Task == nil || b.Task.ID != r.TaskID || b.Task.Query != r.CurrentQuery || !b.Task.UpdatedAt.Equal(r.TaskUpdatedAt) ||
		len(b.Profile) != len(r.ProfileFields) || len(b.Memories) != len(r.MemoryIDs) || len(b.Policies) != len(r.PolicyFamilies) ||
		len(b.Places) != len(r.PlaceIDs) || len(out.Places) != len(r.PlaceIDs) || len(b.Activities) != len(r.ActivityIDs) || len(out.Activities) != len(r.ActivityIDs) ||
		len(b.Relationships) != len(r.RelationshipTieIDs) {
		return ErrUnavailable
	}
	sections := []struct {
		n     int
		state string
	}{{len(r.ProfileFields), b.Sections.Profile}, {len(r.MemoryIDs), b.Sections.Memories},
		{len(r.PolicyFamilies), b.Sections.Policies}, {len(r.PlaceIDs), b.Sections.Places}, {len(r.ActivityIDs), b.Sections.Activities},
		{len(r.RelationshipTieIDs), b.Sections.Relationships}}
	for _, s := range sections {
		if s.n == 0 && s.state != "NOT_REQUESTED" || s.n > 0 && s.state != "AVAILABLE" {
			return ErrUnavailable
		}
	}
	sources := map[string]Source{}
	for _, source := range b.Sources {
		key := source.Kind + "\x00" + source.ID
		if source.ID == "" || !ValidTime(source.NativeTime) || source.NativeTime.After(b.ObservedAt) {
			return ErrUnavailable
		}
		if _, exists := sources[key]; exists {
			return ErrUnavailable
		}
		sources[key] = source
		digestSource := source.Kind == "PUBLIC_CITY" && source.ID == r.CityID || source.Kind == "CURRENT_TASK_REQUEST" && source.ID == r.TaskID ||
			source.Kind == "PUBLIC_PLACE" && hasKey(r.PlaceIDs, source.ID) || source.Kind == "PUBLIC_ACTIVITY" && hasKey(r.ActivityIDs, source.ID) ||
			source.Kind == "PURPOSE_RELATIONSHIP_TIE" && hasKey(r.RelationshipTieIDs, source.ID)
		revisionSource := source.Kind == "PURPOSE_PRIVATE_PROFILE" && source.ID == r.Agent.AgentID && len(r.ProfileFields) > 0 ||
			source.Kind == "PURPOSE_EXPLICIT_MEMORY" && hasKey(r.MemoryIDs, source.ID) ||
			source.Kind == "PURPOSE_POLICY_SETTINGS" && len(r.PolicyFamilies) == 1 && source.ID == r.Agent.AgentID+":"+string(r.PolicyFamilies[0])
		if digestSource {
			if source.Version.Kind != agentevent.UpdatedAtDigestVersion || source.Version.Revision != 0 || len(source.Version.Token) != 64 {
				return ErrUnavailable
			}
			if _, err := hex.DecodeString(source.Version.Token); err != nil {
				return ErrUnavailable
			}
		} else if revisionSource {
			if source.Version.Kind != agentevent.RevisionVersion || source.Version.Revision <= 0 || source.Version.Token != "" || source.RowToken == "" {
				return ErrUnavailable
			}
		} else {
			return ErrUnavailable
		}
	}
	expected := 2 + len(r.MemoryIDs) + len(r.PolicyFamilies) + len(r.PlaceIDs) + len(r.ActivityIDs) + len(r.RelationshipTieIDs)
	if len(r.ProfileFields) > 0 {
		expected++
	}
	if len(sources) != expected {
		return ErrUnavailable
	}
	for key, value := range b.Profile {
		if !hasKey(r.ProfileFields, key) || len(value) == 0 || !json.Valid(value) || string(value) == "null" {
			return ErrUnavailable
		}
	}
	for i, m := range b.Memories {
		if m.ID != r.MemoryIDs[i] || m.Version <= 0 || !json.Valid(m.StructuredValue) || !m.ValidUntil.After(b.ObservedAt) || b.ExpiresAt.After(m.ValidUntil) || !validMemoryConfidence(m.Confidence) {
			return ErrUnavailable
		}
		if source, ok := sources["PURPOSE_EXPLICIT_MEMORY\x00"+m.ID]; !ok || source.Version.Revision != m.Version {
			return ErrUnavailable
		}
	}
	for i, p := range b.Policies {
		if !p.Configured || p.Family != r.PolicyFamilies[i] || agentpolicysettings.ValidateRecord(p, b.ObservedAt) != nil || p.ExpiresAt == nil || b.ExpiresAt.After(*p.ExpiresAt) {
			return ErrUnavailable
		}
		if source, ok := sources["PURPOSE_POLICY_SETTINGS\x00"+r.Agent.AgentID+":"+string(p.Family)]; !ok || source.Version.Revision != p.NativeRevision {
			return ErrUnavailable
		}
	}
	for i, p := range b.Places {
		if p.ID != r.PlaceIDs[i] || out.Places[i].ID != p.ID || out.Places[i].Name != p.Name || out.Places[i].CityID != r.CityID {
			return ErrUnavailable
		}
	}
	for i, a := range b.Activities {
		if a.ID != r.ActivityIDs[i] || out.Activities[i].ID != a.ID || out.Activities[i].Title != a.Title || out.Activities[i].Visibility != "public" || out.Activities[i].CityID != r.CityID || b.ExpiresAt.After(a.EndsAt) {
			return ErrUnavailable
		}
	}
	for i, tie := range b.Relationships {
		if tie.ID != r.RelationshipTieIDs[i] || !validID(tie.PeerAccountID) || tie.PeerAccountID == r.Agent.Principal.ID || tie.State != "ACCEPTED" {
			return ErrUnavailable
		}
	}
	return nil
}
