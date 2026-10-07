// Package agentplacememory projects current own Place metadata for human review.
// It does not certify a visit, attendance, location, interest strength or model access.
package agentplacememory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const (
	Schema            = "agent.place_memory.v1"
	DeclarationSchema = "agent.place_declaration.v1"
	MaxSignals        = 100
	MaxReadLease      = 2 * time.Minute
)

var (
	ErrInvalid       = errors.New("invalid place memory")
	ErrForbidden     = errors.New("place memory forbidden")
	ErrNotFound      = errors.New("place memory not found")
	ErrConflict      = errors.New("place memory version conflict")
	ErrUnavailable   = errors.New("place memory unavailable")
	ErrExpired       = errors.New("place memory read expired")
	ErrAuthorityJSON = errors.New("place memory is server-produced metadata")
)

type Kind string

const (
	Saved              Kind = "SAVED"
	CreatedMomentAt    Kind = "CREATED_MOMENT_AT"
	Liked              Kind = "LIKED"
	Visited            Kind = "VISITED"
	AttendedActivityAt Kind = "ATTENDED_ACTIVITY_AT"
)

type Basis string

const (
	CurrentBookmark   Basis = "CURRENT_NATIVE_BOOKMARK"
	CurrentMomentLink Basis = "CURRENT_NATIVE_MOMENT_LINK"
	SelfDeclaration   Basis = "SELF_DECLARATION"
)

type SourceKind string

const (
	BookmarkSource    SourceKind = "SAVED_PLACE"
	MomentSource      SourceKind = "MOMENT"
	DeclarationSource SourceKind = "EXPLICIT_PLACE_MEMORY"
)

type Source struct {
	Kind    SourceKind               `json:"kind"`
	ID      string                   `json:"id"`
	Version agentevent.SourceVersion `json:"version"`
}
type Signal struct {
	Kind            Kind                   `json:"kind"`
	Basis           Basis                  `json:"basis"`
	Source          Source                 `json:"source"`
	RecordCreatedAt time.Time              `json:"recordCreatedAt"`
	SourceUpdatedAt time.Time              `json:"sourceUpdatedAt"`
	ValidUntil      *time.Time             `json:"validUntil,omitempty"`
	Visibility      agentmemory.Visibility `json:"visibility"`
}

// Projection contains no title/body, coordinates, photos, timeline inference or
// activity IDs. The short lease belongs to this human read, not the native row.
type Projection struct {
	SchemaVersion    string                `json:"schemaVersion"`
	SnapshotID       string                `json:"snapshotId"`
	Owner            actorref.PrincipalRef `json:"owner"`
	AgentID          string                `json:"agentId"`
	PlaceID          string                `json:"placeId"`
	CityID           string                `json:"cityId"`
	AuthorityDigest  string                `json:"authorityDigest"`
	TargetDigest     string                `json:"targetDigest"`
	Signals          []Signal              `json:"signals"`
	ObservedAt       time.Time             `json:"observedAt"`
	ExpiresAt        time.Time             `json:"expiresAt"`
	VerifiedVisit    string                `json:"verifiedVisit"`
	Attendance       string                `json:"attendance"`
	ProcessingStatus string                `json:"processingStatus"`
}

func (p *Projection) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = Projection{}
	}
	return ErrAuthorityJSON
}

type PutDeclarationInput struct {
	ExpectedVersion int64                  `json:"expectedVersion"`
	PlaceID         string                 `json:"placeId"`
	Kind            Kind                   `json:"kind"`
	Visibility      agentmemory.Visibility `json:"visibility"`
	ValidUntil      time.Time              `json:"validUntil"`
}

// Store is an authenticated human-management boundary. Implementing it does
// not implement the cognitive MemoryReader or an automatic enrichment sink.
type Store interface {
	ReadOwnPlaceMemory(context.Context, agentprofile.PrivateAccess, string) (Projection, error)
	RevalidateOwnPlaceMemory(context.Context, agentprofile.PrivateAccess, Projection) error
	PutOwnPlaceDeclaration(context.Context, agentprofile.PrivateAccess, string, PutDeclarationInput) (agentmemory.Record, error)
	DeleteOwnPlaceDeclaration(context.Context, agentprofile.PrivateAccess, string, int64) (agentmemory.Record, error)
}

// Declaration is a typed, direct human statement in the existing Memory ledger.
// No client-confirmed flag or likelihood score can turn it into objective proof.
type Declaration struct {
	SchemaVersion string `json:"schemaVersion"`
	PlaceID       string `json:"placeId"`
	CityID        string `json:"cityId"`
	Kind          Kind   `json:"kind"`
	Basis         Basis  `json:"basis"`
}

func ValidID(id string) bool {
	n, e := agentmemory.NormalizeMemoryID(id)
	return e == nil && n == id
}
func validCity(city string) bool {
	if len(city) < 1 || len(city) > 100 {
		return false
	}
	for _, ch := range city {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
			return false
		}
	}
	return true
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func digestValid(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == sha256.Size && hex.EncodeToString(b) == v
}
func declarationKind(k Kind) bool { return k == Liked || k == Visited }
func DeclarationKey(place string, kind Kind) string {
	return "place.v1." + place + "." + strings.ToLower(string(kind))
}
func DeclarationSummary(kind Kind) string {
	switch kind {
	case Liked:
		return "本人明确表示喜欢这个地点"
	case Visited:
		return "本人自述到访过这个地点（未经核验）"
	default:
		return ""
	}
}
func NormalizePut(input PutDeclarationInput, now time.Time) (PutDeclarationInput, error) {
	if !ValidID(input.PlaceID) || !declarationKind(input.Kind) || input.ExpectedVersion < 0 || input.ExpectedVersion == math.MaxInt64 ||
		(input.Visibility != agentmemory.VisibilityPrivate && input.Visibility != agentmemory.VisibilityAgentOnly) || !validTime(now) ||
		!validTime(input.ValidUntil) || !input.ValidUntil.After(now) || input.ValidUntil.Sub(now) > agentmemory.MaxValidity {
		return PutDeclarationInput{}, ErrInvalid
	}
	input.ValidUntil = input.ValidUntil.UTC().Truncate(time.Microsecond)
	if !input.ValidUntil.After(now) {
		return PutDeclarationInput{}, ErrInvalid
	}
	return input, nil
}

// flatObject rejects duplicates, unknown/case-altered names, nulls, trailing
// objects and nested values before ordinary decoding. Storage JSONB still must
// be treated as a declaration, never a credential or fact approval.
func flatObject(raw []byte, names []string) error {
	if len(raw) == 0 || len(raw) > 2048 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return ErrInvalid
	}
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	seen := map[string]bool{}
	for d.More() {
		v, e := d.Token()
		n, ok := v.(string)
		if e != nil || !ok || !allowed[n] || seen[n] {
			return ErrInvalid
		}
		seen[n] = true
		v, e = d.Token()
		if e != nil || v == nil {
			return ErrInvalid
		}
		if _, nested := v.(json.Delim); nested {
			return ErrInvalid
		}
	}
	last, e := d.Token()
	if e != nil || last != json.Delim('}') || len(seen) != len(names) {
		return ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrInvalid
	}
	return nil
}
func DecodePut(raw []byte, now time.Time) (PutDeclarationInput, error) {
	if flatObject(raw, []string{"expectedVersion", "placeId", "kind", "visibility", "validUntil"}) != nil {
		return PutDeclarationInput{}, ErrInvalid
	}
	var in PutDeclarationInput
	if json.Unmarshal(raw, &in) != nil {
		return PutDeclarationInput{}, ErrInvalid
	}
	return NormalizePut(in, now)
}
func NewMemoryInput(in PutDeclarationInput, city string, now time.Time) (agentmemory.PutInput, error) {
	in, e := NormalizePut(in, now)
	if e != nil || !validCity(city) {
		return agentmemory.PutInput{}, ErrInvalid
	}
	v := Declaration{DeclarationSchema, in.PlaceID, city, in.Kind, SelfDeclaration}
	raw, e := json.Marshal(v)
	if e != nil {
		return agentmemory.PutInput{}, ErrInvalid
	}
	return agentmemory.PutInput{ExpectedVersion: in.ExpectedVersion, MemoryType: agentmemory.TypePlace, MemoryKey: DeclarationKey(in.PlaceID, in.Kind), Summary: DeclarationSummary(in.Kind), StructuredValue: raw, Visibility: in.Visibility, ValidUntil: in.ValidUntil}, nil
}
func DecodeDeclaration(r agentmemory.Record) (Declaration, error) {
	if agentmemory.ValidateRecord(r) != nil || r.MemoryType != agentmemory.TypePlace || r.SourceType != agentmemory.SourceExplicit || r.Status == agentmemory.StatusDeleted ||
		flatObject(r.StructuredValue, []string{"schemaVersion", "placeId", "cityId", "kind", "basis"}) != nil {
		return Declaration{}, ErrInvalid
	}
	var v Declaration
	if json.Unmarshal(r.StructuredValue, &v) != nil || v.SchemaVersion != DeclarationSchema || !ValidID(v.PlaceID) || !validCity(v.CityID) || !declarationKind(v.Kind) || v.Basis != SelfDeclaration ||
		r.MemoryKey != DeclarationKey(v.PlaceID, v.Kind) || r.Summary != DeclarationSummary(v.Kind) {
		return Declaration{}, ErrInvalid
	}
	return v, nil
}

// SnapshotID excludes read observation/lease times so revalidation cannot renew
// an old lease. All source/current authority/target metadata is included.
func SnapshotID(p Projection) string {
	p.SnapshotID = ""
	p.ObservedAt = time.Time{}
	p.ExpiresAt = time.Time{}
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func Validate(p Projection, now time.Time) error {
	if p.SchemaVersion != Schema || p.Owner.Type != actorref.Person || !ValidID(p.Owner.ID) || !ValidID(p.AgentID) || !ValidID(p.PlaceID) || !validCity(p.CityID) ||
		!digestValid(p.AuthorityDigest) || !digestValid(p.TargetDigest) || p.VerifiedVisit != "UNAVAILABLE" || p.Attendance != "UNAVAILABLE" || p.ProcessingStatus != "UNAVAILABLE" ||
		p.Signals == nil || len(p.Signals) > MaxSignals || !validTime(p.ObservedAt) || !validTime(p.ExpiresAt) || !validTime(now) || p.ObservedAt.After(now) ||
		!p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.Sub(p.ObservedAt) > MaxReadLease || p.SnapshotID != SnapshotID(p) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, s := range p.Signals {
		key := string(s.Source.Kind) + ":" + s.Source.ID
		if seen[key] || !ValidID(s.Source.ID) || !validTime(s.RecordCreatedAt) || !validTime(s.SourceUpdatedAt) || s.SourceUpdatedAt.Before(s.RecordCreatedAt) || s.SourceUpdatedAt.After(p.ObservedAt) {
			return ErrInvalid
		}
		seen[key] = true
		v := s.Source.Version
		switch s.Kind {
		case Saved:
			if s.Basis != CurrentBookmark || s.Source.Kind != BookmarkSource || v.Kind != agentevent.CreatedAtDigestVersion || v.Revision != 0 || !digestValid(v.Token) || !s.SourceUpdatedAt.Equal(s.RecordCreatedAt) || s.ValidUntil != nil || s.Visibility != agentmemory.VisibilityPrivate {
				return ErrInvalid
			}
		case CreatedMomentAt:
			if s.Basis != CurrentMomentLink || s.Source.Kind != MomentSource || v.Kind != agentevent.RevisionVersion || v.Revision <= 0 || v.Token != "" || s.ValidUntil != nil || s.Visibility != agentmemory.VisibilityPrivate {
				return ErrInvalid
			}
		case Liked, Visited:
			if s.Basis != SelfDeclaration || s.Source.Kind != DeclarationSource || v.Kind != agentevent.RevisionVersion || v.Revision <= 0 || v.Token != "" || s.ValidUntil == nil || !validTime(*s.ValidUntil) || s.ValidUntil.Before(p.ExpiresAt) ||
				(s.Visibility != agentmemory.VisibilityPrivate && s.Visibility != agentmemory.VisibilityAgentOnly) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	if !now.Before(p.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
