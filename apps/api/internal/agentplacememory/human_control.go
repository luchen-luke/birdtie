package agentplacememory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

// HumanDeclaration is management metadata in the existing Memory ledger.
// EXPIRED is not a current place signal, visit evidence or machine permission.
type HumanDeclaration struct {
	MemoryID   string                 `json:"memoryId"`
	Version    int64                  `json:"version"`
	Kind       Kind                   `json:"kind"`
	Basis      Basis                  `json:"basis"`
	Visibility agentmemory.Visibility `json:"visibility"`
	Status     agentmemory.Status     `json:"status"`
	ValidUntil time.Time              `json:"validUntil"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}
type HumanControl struct {
	Owner                                   actorref.PrincipalRef
	AgentID, PlaceID                        string
	Declarations                            []HumanDeclaration
	ObservedAt, ExpiresAt                   time.Time
	AuthorityStamp, SourceStamp, SnapshotID string
}

func (c *HumanControl) UnmarshalJSON([]byte) error {
	if c != nil {
		*c = HumanControl{}
	}
	return ErrAuthorityJSON
}

type HumanControlStore interface {
	ReadOwnPlaceDeclarationControls(context.Context, agentprofile.PrivateAccess, string) (HumanControl, error)
	RevalidateOwnPlaceDeclarationControls(context.Context, agentprofile.PrivateAccess, HumanControl) error
}

func HumanControlSnapshot(c HumanControl) string {
	c.SnapshotID = ""
	c.ObservedAt = time.Time{}
	c.ExpiresAt = time.Time{}
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ValidateHumanControl(c HumanControl, now time.Time) error {
	if c.Owner.Type != actorref.Person || !ValidID(c.Owner.ID) || !ValidID(c.AgentID) || !ValidID(c.PlaceID) || !digestValid(c.AuthorityStamp) || !digestValid(c.SourceStamp) || c.SnapshotID != HumanControlSnapshot(c) || c.Declarations == nil || len(c.Declarations) > MaxSignals || !validTime(now) || !validTime(c.ObservedAt) || !validTime(c.ExpiresAt) || c.ObservedAt.After(now) || !c.ExpiresAt.After(c.ObservedAt) || c.ExpiresAt.Sub(c.ObservedAt) > MaxReadLease {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, d := range c.Declarations {
		if !ValidID(d.MemoryID) || seen[d.MemoryID] || d.Version < 1 || !declarationKind(d.Kind) || d.Basis != SelfDeclaration || (d.Visibility != agentmemory.VisibilityPrivate && d.Visibility != agentmemory.VisibilityAgentOnly) || !validTime(d.ValidUntil) || !validTime(d.UpdatedAt) || d.UpdatedAt.After(c.ObservedAt) {
			return ErrInvalid
		}
		seen[d.MemoryID] = true
		switch d.Status {
		case agentmemory.StatusActive:
			if !d.ValidUntil.After(c.ObservedAt) || d.ValidUntil.Before(c.ExpiresAt) {
				return ErrInvalid
			}
		case agentmemory.StatusExpired:
			if d.ValidUntil.After(c.ObservedAt) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	if !now.Before(c.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

// HumanBoundDeclarationStore binds a direct human mutation to its concrete
// displayed Agent target. The ID is not authority or a machine permission.
type HumanBoundDeclarationStore interface {
	PutOwnPlaceDeclarationBound(context.Context, agentprofile.PrivateAccess, string, string, PutDeclarationInput) (agentmemory.Record, error)
}

func DecodeHumanPut(raw []byte, now time.Time) (string, PutDeclarationInput, error) {
	if flatObject(raw, []string{"agentId", "expectedVersion", "placeId", "kind", "visibility", "validUntil"}) != nil {
		return "", PutDeclarationInput{}, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", PutDeclarationInput{}, ErrInvalid
	}
	var agent string
	if json.Unmarshal(fields["agentId"], &agent) != nil || !ValidID(agent) {
		return "", PutDeclarationInput{}, ErrInvalid
	}
	delete(fields, "agentId")
	native, e := json.Marshal(fields)
	if e != nil {
		return "", PutDeclarationInput{}, ErrInvalid
	}
	in, e := DecodePut(native, now)
	return agent, in, e
}
