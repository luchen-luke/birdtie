// Package agentmemory owns the single native AgentMemory record contract.
// This foundation permits authenticated human management of Person-owned
// explicit declarations. Shape validation is not source verification, model
// permission, candidate acceptance or a grant to read another principal.
package agentmemory

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const (
	SchemaV1                = "agent-memory-v1"
	MaxBodyBytes            = 16 * 1024
	MaxStructuredValueBytes = 8 * 1024
	MaxSummaryBytes         = 1200
	MaxMemoryKeyBytes       = 100
	MaxValidity             = 365 * 24 * time.Hour
	MaxStructuredDepth      = 6
	MaxStructuredNodes      = 256
	MaxObjectMembers        = 32
	MaxArrayItems           = 32
	MaxStructuredKeyBytes   = 100
)

var (
	ErrInvalid     = errors.New("invalid agent memory")
	ErrForbidden   = errors.New("agent memory forbidden")
	ErrNotFound    = errors.New("agent memory not found")
	ErrConflict    = errors.New("agent memory version conflict")
	ErrUnavailable = errors.New("agent memory unavailable")
)

type MemoryType string

const (
	TypeIdentity            MemoryType = "IDENTITY"
	TypePreference          MemoryType = "PREFERENCE"
	TypePlace               MemoryType = "PLACE"
	TypeCity                MemoryType = "CITY"
	TypeActivity            MemoryType = "ACTIVITY"
	TypeCommunity           MemoryType = "COMMUNITY"
	TypeOrganization        MemoryType = "ORGANIZATION"
	TypeRelationshipContext MemoryType = "RELATIONSHIP_CONTEXT"
	TypeHistory             MemoryType = "HISTORY"
	TypeIntent              MemoryType = "INTENT"
	TypeRoutine             MemoryType = "ROUTINE"
	TypeAvailability        MemoryType = "AVAILABILITY"
	TypeExperience          MemoryType = "EXPERIENCE"
)

type SourceType string

const (
	SourceExplicit SourceType = "EXPLICIT"
	SourceInferred SourceType = "INFERRED"
)

type Visibility string

const (
	VisibilityPrivate   Visibility = "PRIVATE"
	VisibilityAgentOnly Visibility = "AGENT_ONLY"
)

type Status string

const (
	StatusActive        Status = "ACTIVE"
	StatusExpired       Status = "EXPIRED"
	StatusDeleted       Status = "DELETED"
	StatusPendingReview Status = "PENDING_REVIEW"
)

// Record keeps stable native identities and an independent Memory version.
// SourceType distinguishes a human declaration from a future inference; it
// does not certify any external Evidence. An EXPLICIT confidence of 1 denotes
// a direct declaration, never a calibrated probability or a verified fact.
// INFERRED is reserved for non-active storage shapes. Current human writers
// and cognition ports cannot accept or activate it. AGENT_ONLY still permits
// owner human review and grants no runtime/model access.
type Record struct {
	SchemaVersion    string          `json:"schemaVersion"`
	ID               string          `json:"id"`
	AgentID          string          `json:"agentId"`
	OwnerType        actorref.Type   `json:"ownerType"`
	OwnerID          string          `json:"ownerId"`
	Version          int64           `json:"version"`
	MemoryType       MemoryType      `json:"memoryType"`
	MemoryKey        string          `json:"memoryKey"`
	Summary          string          `json:"summary"`
	StructuredValue  json.RawMessage `json:"structuredValue"`
	Confidence       float64         `json:"confidence"`
	SourceType       SourceType      `json:"sourceType"`
	Visibility       Visibility      `json:"visibility"`
	Status           Status          `json:"status"`
	ValidFrom        time.Time       `json:"validFrom"`
	ValidUntil       time.Time       `json:"validUntil"`
	LastReinforcedAt *time.Time      `json:"lastReinforcedAt"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// PutInput contains only directly editable declaration contents. IDs come
// from the path and the live session/Agent binding. ExpectedVersion zero means
// create-only; a positive version means replace that exact current row.
type PutInput struct {
	ExpectedVersion int64           `json:"expectedVersion"`
	MemoryType      MemoryType      `json:"memoryType"`
	MemoryKey       string          `json:"memoryKey"`
	Summary         string          `json:"summary"`
	StructuredValue json.RawMessage `json:"structuredValue"`
	Visibility      Visibility      `json:"visibility"`
	ValidUntil      time.Time       `json:"validUntil"`
}

type DeleteInput struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

// Access is the existing server-only native session credential shape. JSON
// cannot manufacture it. The Store must reauthenticate it for every operation.
type Access = agentprofile.PrivateAccess

type Store interface {
	ReadOwnMemories(context.Context, agentprofile.PrivateAccess) ([]Record, error)
	PutOwnMemory(context.Context, agentprofile.PrivateAccess, string, PutInput) (Record, error)
	DeleteOwnMemory(context.Context, agentprofile.PrivateAccess, string, int64) (Record, error)
}

var memoryKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,99}$`)

func MemoryTypes() []MemoryType {
	return []MemoryType{TypeIdentity, TypePreference, TypePlace, TypeCity,
		TypeActivity, TypeCommunity, TypeOrganization, TypeRelationshipContext,
		TypeHistory, TypeIntent, TypeRoutine, TypeAvailability, TypeExperience}
}

func validMemoryType(value MemoryType) bool {
	for _, allowed := range MemoryTypes() {
		if value == allowed {
			return true
		}
	}
	return false
}

func NormalizeMemoryID(id string) (string, error) {
	if id == "" || id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return "", ErrInvalid
	}
	ref, err := actorref.ParsePrincipal("PERSON", id)
	if err != nil {
		return "", ErrInvalid
	}
	return ref.ID, nil
}

func validTime(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

func normalizedSummary(value string, allowEmpty bool) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalid
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	for _, character := range value {
		if character == utf8.RuneError || (unicode.IsControl(character) && character != '\n' && character != '\t') {
			return "", ErrInvalid
		}
	}
	value = strings.TrimSpace(value)
	if len(value) > MaxSummaryBytes || (!allowEmpty && value == "") {
		return "", ErrInvalid
	}
	return value, nil
}

// NormalizePutInput validates an actual server write time. Neither an input
// deadline nor this pure normalizer proves owner/session/source authorization.
func NormalizePutInput(input PutInput, now time.Time) (PutInput, error) {
	out, err := normalizePutShape(input)
	if err != nil || !validTime(now) || !out.ValidUntil.After(now) || out.ValidUntil.After(now.Add(MaxValidity)) {
		return PutInput{}, ErrInvalid
	}
	return out, nil
}

func normalizePutShape(input PutInput) (PutInput, error) {
	if input.ExpectedVersion < 0 || !validMemoryType(input.MemoryType) || !memoryKeyPattern.MatchString(input.MemoryKey) ||
		(input.Visibility != VisibilityPrivate && input.Visibility != VisibilityAgentOnly) || !validTime(input.ValidUntil) {
		return PutInput{}, ErrInvalid
	}
	summary, err := normalizedSummary(input.Summary, false)
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	value, err := NormalizeStructuredValue(input.StructuredValue)
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	return PutInput{ExpectedVersion: input.ExpectedVersion, MemoryType: input.MemoryType,
		MemoryKey: input.MemoryKey, Summary: summary, StructuredValue: value,
		Visibility: input.Visibility, ValidUntil: input.ValidUntil.UTC()}, nil
}

func ValidateDeleteInput(input DeleteInput) error {
	if input.ExpectedVersion <= 0 {
		return ErrInvalid
	}
	return nil
}

func (record Record) OwnerRef() (actorref.PrincipalRef, error) {
	id, err := NormalizeMemoryID(record.OwnerID)
	if err != nil || record.OwnerType != actorref.Person {
		return actorref.PrincipalRef{}, ErrInvalid
	}
	return actorref.PrincipalRef{Type: actorref.Person, ID: id}, nil
}

// NewExplicit creates a bounded declaration for a binding already resolved by
// the server. It performs no persistence, grant, inference or identity creation.
func NewExplicit(id, agentID string, owner actorref.PrincipalRef, version int64, input PutInput, now, createdAt time.Time) (Record, error) {
	memoryID, err := NormalizeMemoryID(id)
	if err != nil {
		return Record{}, ErrInvalid
	}
	agentID, err = NormalizeMemoryID(agentID)
	if err != nil {
		return Record{}, ErrInvalid
	}
	ownerID, err := NormalizeMemoryID(owner.ID)
	if err != nil || owner.Type != actorref.Person || input.ExpectedVersion == math.MaxInt64 || version != input.ExpectedVersion+1 {
		return Record{}, ErrInvalid
	}
	input, err = NormalizePutInput(input, now)
	if err != nil || !validTime(createdAt) || createdAt.After(now) {
		return Record{}, ErrInvalid
	}
	record := Record{SchemaVersion: SchemaV1, ID: memoryID, AgentID: agentID,
		OwnerType: actorref.Person, OwnerID: ownerID, Version: version,
		MemoryType: input.MemoryType, MemoryKey: input.MemoryKey, Summary: input.Summary,
		StructuredValue: input.StructuredValue, Confidence: 1, SourceType: SourceExplicit,
		Visibility: input.Visibility, Status: StatusActive, ValidFrom: now.UTC(),
		ValidUntil: input.ValidUntil, CreatedAt: createdAt.UTC(), UpdatedAt: now.UTC()}
	if err := ValidateRecord(record); err != nil {
		return Record{}, ErrInvalid
	}
	return record, nil
}

// ValidateRecord checks stored shape only. It never certifies external sources
// or accepts an INFERRED proposal. The caller still needs live owner permission.
func ValidateRecord(record Record) error {
	if _, err := record.OwnerRef(); err != nil {
		return ErrInvalid
	}
	return validateRecordShape(record)
}

// The shared stored shape is private. Public Personal validation retains its
// strict PERSON owner boundary; Organization uses a separate closed wrapper.
func validateRecordShape(record Record) error {
	if record.SchemaVersion != SchemaV1 || record.Version <= 0 || !validMemoryType(record.MemoryType) ||
		!memoryKeyPattern.MatchString(record.MemoryKey) || (record.Visibility != VisibilityPrivate && record.Visibility != VisibilityAgentOnly) {
		return ErrInvalid
	}
	if _, err := NormalizeMemoryID(record.ID); err != nil {
		return ErrInvalid
	}
	if _, err := NormalizeMemoryID(record.AgentID); err != nil {
		return ErrInvalid
	}
	if !validTime(record.CreatedAt) || !validTime(record.UpdatedAt) || record.UpdatedAt.Before(record.CreatedAt) ||
		!validTime(record.ValidFrom) || !validTime(record.ValidUntil) || !record.ValidUntil.After(record.ValidFrom) ||
		record.ValidUntil.Sub(record.ValidFrom) > MaxValidity ||
		math.IsNaN(record.Confidence) || math.IsInf(record.Confidence, 0) || record.Confidence < 0 || record.Confidence > 1 {
		return ErrInvalid
	}
	switch record.SourceType {
	case SourceExplicit:
		if record.Confidence != 1 || record.LastReinforcedAt != nil ||
			(record.Status != StatusActive && record.Status != StatusExpired && record.Status != StatusDeleted) {
			return ErrInvalid
		}
	case SourceInferred:
		if record.Status != StatusPendingReview && record.Status != StatusExpired && record.Status != StatusDeleted {
			return ErrInvalid // No inferred ACTIVE storage in this foundation.
		}
	default:
		return ErrInvalid
	}
	if record.LastReinforcedAt != nil && (!validTime(*record.LastReinforcedAt) ||
		record.LastReinforcedAt.Before(record.CreatedAt) || record.LastReinforcedAt.After(record.UpdatedAt)) {
		return ErrInvalid
	}
	summary, err := normalizedSummary(record.Summary, record.Status == StatusDeleted)
	if err != nil || summary != record.Summary {
		return ErrInvalid
	}
	value, err := NormalizeStructuredValue(record.StructuredValue)
	if err != nil {
		return ErrInvalid
	}
	if record.Status == StatusDeleted && (record.Summary != "" || string(value) != "{}" || record.LastReinforcedAt != nil) {
		return ErrInvalid
	}
	return nil
}

// SortRecords returns a separate deterministic slice: explicit declarations
// precede inferred suggestions regardless of numeric score. Sorting grants no
// access and must occur only after the Store has filtered the current owner.
func SortRecords(records []Record) []Record {
	out := append([]Record{}, records...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SourceType != out[j].SourceType {
			return out[i].SourceType == SourceExplicit
		}
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
