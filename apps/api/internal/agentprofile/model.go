// Package agentprofile owns the versioned Agent Profile foundation. It is
// separate from Account/Agent identity and the existing identity.Profile.
// This base record has no public/private contents, Memory, provider or runtime
// permission. Only a trusted store may bind it to a current native agents row.
package agentprofile

import (
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

var (
	ErrInvalid     = errors.New("invalid agent profile")
	ErrNotFound    = errors.New("agent profile not found")
	ErrForbidden   = errors.New("agent profile forbidden")
	ErrConflict    = errors.New("agent profile version conflict")
	ErrUnavailable = errors.New("agent profile unavailable")
)

// Record is one shared profile foundation for Person, Organization and
// Business account principals. OwnerID is an account principal ID, never the
// public organizations.id/businesses.id, City, Community, Place or model ID.
// AgentID always refers to the independent stable agents.id. Merely decoding a
// record or constructing New does not create an Agent, prove ownership, grant
// access, or enable the Business runtime.
type Record struct {
	AgentID        string        `json:"agentId"`
	OwnerType      actorref.Type `json:"ownerType"`
	OwnerID        string        `json:"ownerId"`
	ProfileVersion int64         `json:"profileVersion"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

func validID(id string) bool {
	if id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	_, err := actorref.ParsePrincipal("PERSON", id)
	return err == nil
}

func normalizeOwner(owner actorref.PrincipalRef) (actorref.PrincipalRef, error) {
	switch owner.Type {
	case actorref.Person, actorref.Organization, actorref.Business:
	default:
		return actorref.PrincipalRef{}, ErrInvalid
	}
	if !validID(owner.ID) {
		return actorref.PrincipalRef{}, ErrInvalid
	}
	ref, err := actorref.ParsePrincipal(string(owner.Type), owner.ID)
	if err != nil {
		return actorref.PrincipalRef{}, ErrInvalid
	}
	return ref, nil
}

func validTime(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}

// New builds metadata for an already-existing Agent binding. The store must
// resolve and check that actual Agent first; this pure function performs no
// identity creation, migration/backfill, authorization or persistence.
func New(agentID string, owner actorref.PrincipalRef, now time.Time) (Record, error) {
	principal, err := normalizeOwner(owner)
	if err != nil || !validID(agentID) || !validTime(now) {
		return Record{}, ErrInvalid
	}
	return Record{AgentID: strings.ToLower(agentID), OwnerType: principal.Type, OwnerID: principal.ID,
		ProfileVersion: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}, nil
}

func (r Record) OwnerRef() (actorref.PrincipalRef, error) {
	return normalizeOwner(actorref.PrincipalRef{Type: r.OwnerType, ID: r.OwnerID})
}

// Validate checks metadata shape, not the current owner or permission. Stable
// binding, current Account/Agent status and actor/membership rights must be
// enforced by the server/store on every operation. A positive version supplied
// by a model/client cannot replace optimistic concurrency or current sources.
func Validate(r Record) error {
	if !validID(r.AgentID) || r.ProfileVersion <= 0 || !validTime(r.CreatedAt) || !validTime(r.UpdatedAt) || r.UpdatedAt.Before(r.CreatedAt) {
		return ErrInvalid
	}
	_, err := r.OwnerRef()
	return err
}
