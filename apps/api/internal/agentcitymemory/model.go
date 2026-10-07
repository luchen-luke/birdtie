// Package agentcitymemory distinguishes private human city statements. It does
// not certify location, residence, visits, historical dates or cognitive access.
package agentcitymemory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const (
	Schema            = "agent.city_memory.v1"
	DeclarationSchema = "agent.city_declaration.v1"
	MaxSignals        = 4
	MaxReadLease      = 2 * time.Minute
	SelfDeclaration   = "SELF_DECLARATION"
	ContextSource     = "NATIVE_CONTEXT_ROW"
	MemorySource      = "EXPLICIT_CITY_MEMORY"
)

var (
	ErrInvalid       = errors.New("invalid city memory")
	ErrForbidden     = errors.New("city memory forbidden")
	ErrNotFound      = errors.New("city memory not found")
	ErrConflict      = errors.New("city memory version conflict")
	ErrUnavailable   = errors.New("city memory unavailable")
	ErrExpired       = errors.New("city memory read expired")
	ErrAuthorityJSON = errors.New("city memory is server-produced metadata")
)

type Kind string

const (
	Current    Kind = "CURRENT"
	Lived      Kind = "LIVED"
	Visited    Kind = "VISITED"
	Interested Kind = "INTERESTED"
)

func Kinds() []Kind          { return []Kind{Current, Lived, Visited, Interested} }
func historical(k Kind) bool { return k == Lived || k == Visited || k == Interested }
func ValidID(id string) bool { v, e := agentmemory.NormalizeMemoryID(id); return e == nil && v == id }

// City IDs keep the original catalogue namespace, separate from Context UUIDs.
func ValidCity(id string) bool {
	if len(id) < 1 || len(id) > 100 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
func validTime(t time.Time) bool { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func digest(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == sha256.Size && hex.EncodeToString(b) == v
}
func Explanation(k Kind) string {
	switch k {
	case Current:
		return "本人声明的当前城市（非实时定位）"
	case Lived:
		return "本人自述曾居住于此（未经核验）"
	case Visited:
		return "本人自述曾到访此城（未经核验）"
	case Interested:
		return "本人明确表示对这座城市感兴趣"
	}
	return ""
}
func DeclarationKey(city string, k Kind) string {
	h := sha256.Sum256([]byte(city))
	return "city.v1." + hex.EncodeToString(h[:]) + "." + strings.ToLower(string(k))
}
func ValidDeclarationKey(key string) bool {
	for _, k := range []Kind{Lived, Visited, Interested} {
		suffix := "." + strings.ToLower(string(k))
		if strings.HasPrefix(key, "city.v1.") && strings.HasSuffix(key, suffix) && digest(strings.TrimSuffix(strings.TrimPrefix(key, "city.v1."), suffix)) {
			return true
		}
	}
	return false
}

type PutDeclarationInput struct {
	ExpectedVersion int64                  `json:"expectedVersion"`
	CityID          string                 `json:"cityId"`
	Kind            Kind                   `json:"kind"`
	Visibility      agentmemory.Visibility `json:"visibility"`
	ValidUntil      time.Time              `json:"validUntil"`
}
type Declaration struct {
	SchemaVersion string `json:"schemaVersion"`
	CityID        string `json:"cityId"`
	Kind          Kind   `json:"kind"`
	Basis         string `json:"basis"`
}
type Source struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Token    string `json:"token"`
}
type Signal struct {
	Kind            Kind                   `json:"kind"`
	Basis           string                 `json:"basis"`
	Explanation     string                 `json:"explanation"`
	Source          Source                 `json:"source"`
	RecordCreatedAt time.Time              `json:"recordCreatedAt"`
	SourceUpdatedAt *time.Time             `json:"sourceUpdatedAt,omitempty"`
	ValidUntil      *time.Time             `json:"validUntil,omitempty"`
	Visibility      agentmemory.Visibility `json:"visibility"`
}

// Projection is a finite human read, never a source permit or fact approval.
type Projection struct {
	SchemaVersion    string                `json:"schemaVersion"`
	SnapshotID       string                `json:"snapshotId"`
	Owner            actorref.PrincipalRef `json:"owner"`
	AgentID          string                `json:"agentId"`
	CityID           string                `json:"cityId"`
	AuthorityDigest  string                `json:"authorityDigest"`
	TargetDigest     string                `json:"targetDigest"`
	Signals          []Signal              `json:"signals"`
	ObservedAt       time.Time             `json:"observedAt"`
	ExpiresAt        time.Time             `json:"expiresAt"`
	Verification     string                `json:"verification"`
	HistoricalDates  string                `json:"historicalDates"`
	ProcessingStatus string                `json:"processingStatus"`
	issuedObservedAt time.Time
	issuedExpiresAt  time.Time
	issuedSnapshotID string
}

// IssueProjection preserves the original display lease in process. It checks
// shape only: native ownership, current source and purpose are not proved here.
// This receipt is not a signature, permission or cognitive authority object.
func IssueProjection(p Projection) (Projection, error) {
	if e := Validate(p, p.ObservedAt); e != nil {
		return Projection{}, e
	}
	p.issuedObservedAt = p.ObservedAt
	p.issuedExpiresAt = p.ExpiresAt
	p.issuedSnapshotID = p.SnapshotID
	return p, nil
}
func ValidateIssued(p Projection) error {
	if p.issuedObservedAt.IsZero() || p.issuedExpiresAt.IsZero() || p.issuedSnapshotID == "" || !p.ObservedAt.Equal(p.issuedObservedAt) || !p.ExpiresAt.Equal(p.issuedExpiresAt) || p.SnapshotID != p.issuedSnapshotID {
		return ErrInvalid
	}
	return Validate(p, p.ObservedAt)
}

func (p *Projection) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = Projection{}
	}
	return ErrAuthorityJSON
}

type Store interface {
	ReadOwnCityMemory(context.Context, agentprofile.PrivateAccess, string) (Projection, error)
	RevalidateOwnCityMemory(context.Context, agentprofile.PrivateAccess, Projection) error
	PutOwnCityDeclaration(context.Context, agentprofile.PrivateAccess, string, PutDeclarationInput) (agentmemory.Record, error)
	DeleteOwnCityDeclaration(context.Context, agentprofile.PrivateAccess, string, int64) (agentmemory.Record, error)
}

func NormalizePut(in PutDeclarationInput, now time.Time) (PutDeclarationInput, error) {
	if !ValidCity(in.CityID) || !historical(in.Kind) || in.ExpectedVersion < 0 || in.ExpectedVersion == math.MaxInt64 || !validTime(now) || !validTime(in.ValidUntil) || !in.ValidUntil.After(now) || in.ValidUntil.After(now.Add(agentmemory.MaxValidity)) || (in.Visibility != agentmemory.VisibilityPrivate && in.Visibility != agentmemory.VisibilityAgentOnly) {
		return PutDeclarationInput{}, ErrInvalid
	}
	in.ValidUntil = in.ValidUntil.UTC().Truncate(time.Microsecond)
	if !in.ValidUntil.After(now) {
		return PutDeclarationInput{}, ErrInvalid
	}
	return in, nil
}
func NewMemoryInput(in PutDeclarationInput, now time.Time) (agentmemory.PutInput, error) {
	in, e := NormalizePut(in, now)
	if e != nil {
		return agentmemory.PutInput{}, e
	}
	raw, e := json.Marshal(Declaration{DeclarationSchema, in.CityID, in.Kind, SelfDeclaration})
	if e != nil {
		return agentmemory.PutInput{}, ErrInvalid
	}
	return agentmemory.PutInput{ExpectedVersion: in.ExpectedVersion, MemoryType: agentmemory.TypeCity, MemoryKey: DeclarationKey(in.CityID, in.Kind), Summary: Explanation(in.Kind), StructuredValue: raw, Visibility: in.Visibility, ValidUntil: in.ValidUntil}, nil
}
func DecodeDeclaration(r agentmemory.Record) (Declaration, error) {
	if agentmemory.ValidateRecord(r) != nil || r.MemoryType != agentmemory.TypeCity || r.SourceType != agentmemory.SourceExplicit || r.Status == agentmemory.StatusDeleted || flatObject(r.StructuredValue, []string{"schemaVersion", "cityId", "kind", "basis"}) != nil {
		return Declaration{}, ErrInvalid
	}
	var d Declaration
	if json.Unmarshal(r.StructuredValue, &d) != nil || d.SchemaVersion != DeclarationSchema || !ValidCity(d.CityID) || !historical(d.Kind) || d.Basis != SelfDeclaration || r.MemoryKey != DeclarationKey(d.CityID, d.Kind) || r.Summary != Explanation(d.Kind) {
		return Declaration{}, ErrInvalid
	}
	return d, nil
}
func SnapshotID(p Projection) string {
	p.SnapshotID = ""
	p.ObservedAt = time.Time{}
	p.ExpiresAt = time.Time{}
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func Validate(p Projection, now time.Time) error {
	if p.SchemaVersion != Schema || p.Owner.Type != actorref.Person || !ValidID(p.Owner.ID) || !ValidID(p.AgentID) || !ValidCity(p.CityID) || !digest(p.AuthorityDigest) || !digest(p.TargetDigest) || p.Verification != "UNAVAILABLE" || p.HistoricalDates != "NOT_PROVIDED" || p.ProcessingStatus != "UNAVAILABLE" || p.Signals == nil || len(p.Signals) > MaxSignals || !validTime(now) || !validTime(p.ObservedAt) || !validTime(p.ExpiresAt) || p.ObservedAt.After(now) || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.Sub(p.ObservedAt) > MaxReadLease || p.SnapshotID != SnapshotID(p) {
		return ErrInvalid
	}
	seen := map[Kind]bool{}
	ids := map[string]bool{}
	for _, s := range p.Signals {
		if seen[s.Kind] || ids[s.Source.Kind+":"+s.Source.ID] || !ValidID(s.Source.ID) || s.Basis != SelfDeclaration || s.Explanation != Explanation(s.Kind) || s.Explanation == "" || !validTime(s.RecordCreatedAt) || s.RecordCreatedAt.After(p.ObservedAt) {
			return ErrInvalid
		}
		seen[s.Kind] = true
		ids[s.Source.Kind+":"+s.Source.ID] = true
		if s.Kind == Current {
			if s.Source.Kind != ContextSource || s.Source.Revision != 0 || !digest(s.Source.Token) || s.SourceUpdatedAt != nil || s.ValidUntil != nil || s.Visibility != agentmemory.VisibilityPrivate {
				return ErrInvalid
			}
		} else if historical(s.Kind) {
			if s.Source.Kind != MemorySource || s.Source.Revision <= 0 || s.Source.Token != "" || s.SourceUpdatedAt == nil || !validTime(*s.SourceUpdatedAt) || s.SourceUpdatedAt.Before(s.RecordCreatedAt) || s.SourceUpdatedAt.After(p.ObservedAt) || s.ValidUntil == nil || !validTime(*s.ValidUntil) || s.ValidUntil.Before(p.ExpiresAt) || (s.Visibility != agentmemory.VisibilityPrivate && s.Visibility != agentmemory.VisibilityAgentOnly) {
				return ErrInvalid
			}
		} else {
			return ErrInvalid
		}
	}
	if !now.Before(p.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
