// Package agentpurpose provides prohibitions, never an authorization resolver.
// Caller supplied purpose, origin, membership or TTL cannot grant a read/write.
package agentpurpose

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

type Origin string
type Operation string
type Retention string

const (
	OriginUnknown           Origin    = "UNKNOWN"
	OriginTemporaryActivity Origin    = "TEMPORARY_ACTIVITY"
	Read                    Operation = "READ"
	StageMemory             Operation = "STAGE_MEMORY_CANDIDATE"
	PersistMemory           Operation = "PERSIST_MEMORY"
	ModelEgress             Operation = "MODEL_EGRESS"
	Forward                 Operation = "FORWARD"
	Transient               Retention = "TRANSIENT"
	Persistent              Retention = "PERSISTENT"
)

var (
	ErrUnavailable = errors.New("current source purpose provenance unavailable")
	ErrProhibited  = errors.New("source purpose prohibits persistent memory")
	ErrWire        = errors.New("purpose guard is not a wire authorization")
)

// Request contains only a proposed operation, not verified authority. There is
// deliberately no Allow/Verified/grant constructor or content body. Current
// cognitive references do not carry native temporary-Activity provenance, so
// their production adapters must use OriginUnknown instead of guessing it.
type Request struct {
	Origin    Origin
	Operation Operation
	Retention Retention
	Recipient actorref.Type
}

func (Request) MarshalJSON() ([]byte, error)  { return nil, ErrWire }
func (r *Request) UnmarshalJSON([]byte) error { *r = Request{}; return ErrWire }

// Check only refuses or reports missing capability. It can never authorize a
// consumer. The temporary-purpose prohibition is independent of expiry: a
// longer TTL, another Task, an RSVP, a profile_view grant or public visibility
// cannot turn temporary activity information into durable Agent knowledge.
func Check(ctx context.Context, r Request) error {
	if ctx == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.Recipient != actorref.Person && r.Recipient != actorref.Organization && r.Recipient != actorref.Business {
		return ErrUnavailable
	}
	if r.Retention != Transient && r.Retention != Persistent {
		return ErrUnavailable
	}
	switch r.Operation {
	case Read, StageMemory, PersistMemory, ModelEgress, Forward:
	default:
		return ErrUnavailable
	}
	switch r.Origin {
	case OriginUnknown, OriginTemporaryActivity:
	default:
		return ErrUnavailable
	}
	if r.Retention == Persistent && (r.Operation == StageMemory || r.Operation == PersistMemory) {
		if r.Origin == OriginTemporaryActivity || r.Recipient == actorref.Organization || r.Recipient == actorref.Business {
			return ErrProhibited
		}
	}
	return ErrUnavailable
}
