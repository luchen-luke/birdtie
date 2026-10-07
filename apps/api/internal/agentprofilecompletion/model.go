// Package agentprofilecompletion describes a concrete human self-confirmation.
// A source declaration, preview, digest or decoded DTO is never a machine grant.
package agentprofilecompletion

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const Schema = "agent-profile-memory-completion-v1"
const Purpose = "HUMAN_PRIVATE_PROFILE_COMPLETION"
const Field = "preferredActivityTypes"
const PreviewTTL = 5 * time.Minute
const MaxBodyBytes = 2048

var ErrExpired = errors.New("profile completion preview expired")

type PreviewInput struct {
	PreviewID              string `json:"previewId"`
	MemoryID               string `json:"memoryId"`
	MemoryVersion          int64  `json:"memoryVersion"`
	ExpectedProfileVersion int64  `json:"expectedProfileVersion"`
}
type AcceptInput struct {
	PlanDigest string `json:"planDigest"`
}
type Suggestion struct {
	MemoryID         string    `json:"memoryId"`
	MemoryVersion    int64     `json:"memoryVersion"`
	Category         string    `json:"category"`
	Value            string    `json:"value"`
	MemoryValidUntil time.Time `json:"memoryValidUntil"`
}
type Suggestions struct {
	SchemaVersion  string                `json:"schemaVersion"`
	Owner          actorref.PrincipalRef `json:"owner"`
	AgentID        string                `json:"agentId"`
	ProfileVersion int64                 `json:"profileVersion"`
	TargetField    string                `json:"targetField"`
	State          string                `json:"state"`
	Sources        []Suggestion          `json:"sources"`
	ObservedAt     time.Time             `json:"observedAt"`
	ModelAccess    bool                  `json:"modelAccess"`
}
type Preview struct {
	SchemaVersion          string                `json:"schemaVersion"`
	ID                     string                `json:"id"`
	Purpose                string                `json:"purpose"`
	Owner                  actorref.PrincipalRef `json:"owner"`
	AgentID                string                `json:"agentId"`
	Source                 Suggestion            `json:"source"`
	ExpectedProfileVersion int64                 `json:"expectedProfileVersion"`
	TargetField            string                `json:"targetField"`
	Before                 []string              `json:"before"`
	After                  []string              `json:"after"`
	PlanDigest             string                `json:"planDigest"`
	ObservedAt             time.Time             `json:"observedAt"`
	ExpiresAt              time.Time             `json:"expiresAt"`
	Explanation            string                `json:"explanation"`
	ModelAccess            bool                  `json:"modelAccess"`
}

// Receipt is metadata, not a restored review or authority to execute it.
type Receipt struct {
	SchemaVersion          string                `json:"schemaVersion"`
	ID                     string                `json:"id"`
	Purpose                string                `json:"purpose"`
	Owner                  actorref.PrincipalRef `json:"owner"`
	AgentID                string                `json:"agentId"`
	MemoryID               string                `json:"memoryId"`
	MemoryVersion          int64                 `json:"memoryVersion"`
	ExpectedProfileVersion int64                 `json:"expectedProfileVersion"`
	PlanDigest             string                `json:"planDigest"`
	State                  string                `json:"state"`
	ResultProfileVersion   *int64                `json:"resultProfileVersion,omitempty"`
	CommittedAt            *time.Time            `json:"committedAt,omitempty"`
	CurrentProfileMatches  bool                  `json:"currentProfileMatches"`
	ObservedAt             time.Time             `json:"observedAt"`
	ExpiresAt              time.Time             `json:"expiresAt"`
	ModelAccess            bool                  `json:"modelAccess"`
}
type HumanStore interface {
	ReadOwnProfileCompletionSuggestions(context.Context, agentprofile.PrivateAccess) (Suggestions, error)
	PreviewOwnProfileCompletion(context.Context, agentprofile.PrivateAccess, PreviewInput) (Preview, error)
	ReadOwnProfileCompletion(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
	AcceptOwnProfileCompletion(context.Context, agentprofile.PrivateAccess, string, AcceptInput) (Receipt, error)
}

func ValidID(id string) bool {
	normalized, e := agentmemory.NormalizeMemoryID(id)
	return e == nil && normalized == id
}
func NormalizePreviewInput(v PreviewInput) (PreviewInput, error) {
	if !ValidID(v.PreviewID) || !ValidID(v.MemoryID) || v.MemoryVersion <= 0 || v.ExpectedProfileVersion <= 0 {
		return PreviewInput{}, agentprofile.ErrInvalid
	}
	if v.ExpectedProfileVersion == math.MaxInt64 {
		return PreviewInput{}, agentprofile.ErrConflict
	}
	return v, nil
}
func ValidDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func NormalizeAcceptInput(v AcceptInput) (AcceptInput, error) {
	if !ValidDigest(v.PlanDigest) {
		return AcceptInput{}, agentprofile.ErrInvalid
	}
	return v, nil
}
func finite(t time.Time) bool { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }

// EligibleSource is a closed ordinary-activity whitelist. EXPLICIT alone does
// not classify arbitrary private text as non-sensitive or prove any real fact.
func EligibleSource(r agentmemory.Record, owner, agent string, now time.Time) (Suggestion, error) {
	if !finite(now) || agentmemory.ValidateRecord(r) != nil || r.OwnerType != actorref.Person || r.OwnerID != owner || r.AgentID != agent || r.Status != agentmemory.StatusActive || r.SourceType != agentmemory.SourceExplicit || r.Visibility != agentmemory.VisibilityPrivate || r.MemoryType != agentmemory.TypePreference || r.ValidFrom.After(now) || !r.ValidUntil.After(now) || r.UpdatedAt.After(now) {
		return Suggestion{}, agentprofile.ErrForbidden
	}
	var v map[string]string
	if json.Unmarshal(r.StructuredValue, &v) != nil || len(v) != 2 || v["nature"] != "human-declaration" {
		return Suggestion{}, agentprofile.ErrForbidden
	}
	c := v["activityCategory"]
	if agentruntime.ValidateOrdinaryCandidateAttribute(agentruntime.SocialPreferenceActivityCategory, c) != nil || r.MemoryKey != "activity_category:"+c || r.Summary != agentmemorycandidate.Statement(c) {
		return Suggestion{}, agentprofile.ErrForbidden
	}
	return Suggestion{MemoryID: r.ID, MemoryVersion: r.Version, Category: c, Value: agentmemorycandidate.Statement(c), MemoryValidUntil: r.ValidUntil.UTC()}, nil
}

// Replacement fills exactly one empty field. It preserves the original other
// eight normalized fields and uses the existing full-nine-field CAS contract.
func Replacement(r agentprofile.PrivateRecord, s Suggestion) (agentprofile.ReplacePrivateInput, error) {
	if agentprofile.ValidatePrivateRecord(r) != nil || !ValidID(s.MemoryID) || s.MemoryVersion <= 0 || agentruntime.ValidateOrdinaryCandidateAttribute(agentruntime.SocialPreferenceActivityCategory, s.Category) != nil || s.Value != agentmemorycandidate.Statement(s.Category) {
		return agentprofile.ReplacePrivateInput{}, agentprofile.ErrInvalid
	}
	if r.Profile.ProfileVersion == math.MaxInt64 || len(r.Fields.PreferredActivityTypes) != 0 {
		return agentprofile.ReplacePrivateInput{}, agentprofile.ErrConflict
	}
	fields, e := agentprofile.NormalizePrivateFields(r.Fields)
	if e != nil {
		return agentprofile.ReplacePrivateInput{}, e
	}
	fields.PreferredActivityTypes = []string{s.Value}
	out, e := agentprofile.NormalizeReplacePrivateInput(agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: fields})
	if e != nil {
		return agentprofile.ReplacePrivateInput{}, e
	}
	before := r.Fields
	after := out.Fields
	before.PreferredActivityTypes = nil
	after.PreferredActivityTypes = nil
	a, _ := agentprofile.NormalizePrivateFields(before)
	b, _ := agentprofile.NormalizePrivateFields(after)
	if !reflect.DeepEqual(a, b) {
		return agentprofile.ReplacePrivateInput{}, agentprofile.ErrInvalid
	}
	return out, nil
}

func ValidatePreview(p Preview) error {
	if p.SchemaVersion != Schema || p.Purpose != Purpose || !ValidID(p.ID) || p.Owner.Type != actorref.Person || !ValidID(p.Owner.ID) || !ValidID(p.AgentID) || p.TargetField != Field || p.ExpectedProfileVersion <= 0 || p.ExpectedProfileVersion == math.MaxInt64 || !ValidID(p.Source.MemoryID) || p.Source.MemoryVersion <= 0 || !ValidDigest(p.PlanDigest) || len(p.Before) != 0 || len(p.After) != 1 || p.After[0] != p.Source.Value || p.Source.Value != agentmemorycandidate.Statement(p.Source.Category) || agentruntime.ValidateOrdinaryCandidateAttribute(agentruntime.SocialPreferenceActivityCategory, p.Source.Category) != nil || !finite(p.ObservedAt) || !finite(p.ExpiresAt) || !finite(p.Source.MemoryValidUntil) || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.Sub(p.ObservedAt) > PreviewTTL || p.ExpiresAt.After(p.Source.MemoryValidUntil) || p.Explanation == "" || p.ModelAccess {
		return agentprofile.ErrInvalid
	}
	return nil
}
func ValidateReceipt(r Receipt) error {
	if r.SchemaVersion != Schema || r.Purpose != Purpose || !ValidID(r.ID) || r.Owner.Type != actorref.Person || !ValidID(r.Owner.ID) || !ValidID(r.AgentID) || !ValidID(r.MemoryID) || r.MemoryVersion <= 0 || r.ExpectedProfileVersion <= 0 || r.ExpectedProfileVersion == math.MaxInt64 || !ValidDigest(r.PlanDigest) || !finite(r.ObservedAt) || !finite(r.ExpiresAt) || r.ModelAccess {
		return agentprofile.ErrInvalid
	}
	switch r.State {
	case "COMMITTED":
		if r.ResultProfileVersion == nil || *r.ResultProfileVersion != r.ExpectedProfileVersion+1 || r.CommittedAt == nil || !finite(*r.CommittedAt) || r.CommittedAt.After(r.ObservedAt) || !r.CommittedAt.Before(r.ExpiresAt) {
			return agentprofile.ErrInvalid
		}
	case "PENDING", "EXPIRED":
		if r.ResultProfileVersion != nil || r.CommittedAt != nil || r.CurrentProfileMatches {
			return agentprofile.ErrInvalid
		}
		if (r.State == "PENDING") != r.ExpiresAt.After(r.ObservedAt) {
			return agentprofile.ErrInvalid
		}
	default:
		return agentprofile.ErrInvalid
	}
	return nil
}
func ValidateSuggestions(s Suggestions) error {
	if s.SchemaVersion != Schema || s.Owner.Type != actorref.Person || !ValidID(s.Owner.ID) || !ValidID(s.AgentID) || s.ProfileVersion <= 0 || s.ProfileVersion == math.MaxInt64 || s.TargetField != Field || s.ModelAccess || !finite(s.ObservedAt) || len(s.Sources) > 6 || s.Sources == nil {
		return agentprofile.ErrInvalid
	}
	if s.State != "FIELD_EMPTY" && s.State != "FIELD_ALREADY_SET" {
		return agentprofile.ErrInvalid
	}
	if s.State == "FIELD_ALREADY_SET" && len(s.Sources) != 0 {
		return agentprofile.ErrInvalid
	}
	ids, cats := map[string]bool{}, map[string]bool{}
	for _, v := range s.Sources {
		if !ValidID(v.MemoryID) || v.MemoryVersion <= 0 || ids[v.MemoryID] || cats[v.Category] || agentruntime.ValidateOrdinaryCandidateAttribute(agentruntime.SocialPreferenceActivityCategory, v.Category) != nil || v.Value != agentmemorycandidate.Statement(v.Category) || !finite(v.MemoryValidUntil) || !v.MemoryValidUntil.After(s.ObservedAt) {
			return agentprofile.ErrInvalid
		}
		ids[v.MemoryID] = true
		cats[v.Category] = true
	}
	return nil
}
