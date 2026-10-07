package agentoutbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

// Claim is process-local lease evidence. The real Store must validate its
// current row/fence/expiry within the commit transaction. JSON cannot grant it.
type Claim struct {
	EventID        string
	Subject        actorref.PrincipalRef
	AgentID        string
	HandlerVersion HandlerVersion
	WorkerID       string
	Fence          int64
	LeaseUntil     time.Time
}

func (Claim) MarshalJSON() ([]byte, error) { return nil, ErrAuthorityJSON }
func (claim *Claim) UnmarshalJSON([]byte) error {
	if claim != nil {
		*claim = Claim{}
	}
	return ErrAuthorityJSON
}

func ValidateClaim(claim Claim) error {
	if !validID(claim.EventID) || !validPerson(claim.Subject) || !validID(claim.AgentID) ||
		ValidateHandlerVersion(claim.HandlerVersion) != nil || !validID(claim.WorkerID) || claim.Fence <= 0 || claim.Fence == math.MaxInt64 || !validTime(claim.LeaseUntil) {
		return ErrInvalid
	}
	return nil
}

// CheckFence is a shape/comparison primitive, never a current DB resolver.
// Passing synthetic claims to it does not authorize consumption or an effect.
func CheckFence(now time.Time, current, supplied Claim) error {
	if !validTime(now) || ValidateClaim(current) != nil || ValidateClaim(supplied) != nil {
		return ErrInvalid
	}
	if current != supplied {
		return ErrConflict
	}
	if !current.LeaseUntil.After(now) {
		return ErrExpired
	}
	return nil
}

type ConsumerRecord struct {
	EventID        string                `json:"event_id"`
	Subject        actorref.PrincipalRef `json:"subject"`
	HandlerVersion HandlerVersion        `json:"handler_version"`
	State          State                 `json:"state"`
	Fence          int64                 `json:"fence"`
	Attempt        int64                 `json:"attempt"`
	Reason         ReasonCode            `json:"reason"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

func ValidateConsumerRecord(r ConsumerRecord) error {
	metadataHandler := ValidateHandlerVersion(r.HandlerVersion) == nil || (r.State == CandidateStaged && (r.HandlerVersion == "mom-candidate-local-v1" || r.HandlerVersion == "mom-candidate-local-v2"))
	if !validID(r.EventID) || !validPerson(r.Subject) || !metadataHandler ||
		r.Fence <= 0 || r.Attempt < 1 || r.Attempt > MaxAttempts || r.Attempt > r.Fence ||
		!validTime(r.CreatedAt) || !validTime(r.UpdatedAt) || r.UpdatedAt.Before(r.CreatedAt) {
		return ErrInvalid
	}
	if r.HandlerVersion == MemoryHandler || r.HandlerVersion == PreferenceHandler {
		return validateMemoryConsumer(r)
	}
	switch r.State {
	case CandidateStaged:
		if r.Reason != "CANDIDATE_STAGED" {
			return ErrInvalid
		}
	case Leased:
		if r.Reason != "" {
			return ErrInvalid
		}
	case Unavailable:
		if r.Reason != ReasonPurposeUnavailable {
			return ErrInvalid
		}
	case Invalidated:
		if r.Reason != ReasonSourceInvalidated {
			return ErrInvalid
		}
	case Expired:
		if r.Reason != ReasonExpired {
			return ErrInvalid
		}
	case DeadLetter:
		if r.Reason != ReasonAttemptsExhausted {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type EffectKind string

const MemoryCandidateEffect EffectKind = "MEMORY_CANDIDATE"

// EffectAddress deliberately has no handlerVersion or eventID. Handler upgrades
// may create a new consumer receipt but cannot create a new effect identity.
// This address, its JSON form or its hash never authorizes a business writer.
type EffectAddress struct {
	Tenant             actorref.PrincipalRef
	Subject            actorref.PrincipalRef
	AgentID            string
	LogicalOperationID string
	ActionID           string
	Kind               EffectKind
}

func EffectKey(address EffectAddress) (string, error) {
	if !validPerson(address.Tenant) || address.Subject != address.Tenant || !validID(address.AgentID) ||
		!validID(address.LogicalOperationID) || !validID(address.ActionID) || address.Kind != MemoryCandidateEffect {
		return "", ErrInvalid
	}
	raw, _ := json.Marshal(address)
	hash := sha256.Sum256(append([]byte("birdtie.outbox.effect-address.v1\x00"), raw...))
	return hex.EncodeToString(hash[:]), nil
}

type Outcome struct {
	State   State      `json:"state"`
	Reason  ReasonCode `json:"reason"`
	Effects int64      `json:"effects"`
}

func ValidateOutcome(outcome Outcome) error {
	if outcome.State != Unavailable || outcome.Reason != ReasonPurposeUnavailable || outcome.Effects != 0 {
		return ErrInvalid
	}
	return nil
}

// ActualConsumer has no injected provider, authority facts or candidate writer.
// There is currently no analysis-purpose resolver; well-shaped metadata and
// valid leases still return unavailable and zero committed business effects.
type ActualConsumer struct{}

func (ActualConsumer) Consume(ctx context.Context, record Record, claim Claim) (Outcome, error) {
	if ctx == nil {
		return Outcome{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Outcome{}, err
	}
	if ValidateRecord(record) != nil || ValidateClaim(claim) != nil ||
		claim.EventID != record.Event.EventID || claim.Subject != record.Event.Subject || claim.AgentID != record.Event.AgentID {
		return Outcome{}, ErrInvalid
	}
	return Outcome{Unavailable, ReasonPurposeUnavailable, 0}, ErrUnavailable
}
