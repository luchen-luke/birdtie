// Package agentoutbox owns metadata and delivery controls for native Moment
// transactions. Its records, keys and leases never authorize analysis, a model
// call or a MemoryCandidate. The actual consumer remains unavailable.
package agentoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const SchemaVersion = "agent-outbox-v1"
const MaxEventTTL = 15 * time.Minute
const MaxLeaseDuration = 30 * time.Second
const MaxAttempts int64 = 20
const MaxEnvelopeBytes = 4096
const MaxRecordBytes = 8192

var (
	ErrInvalid       = errors.New("事件控制记录无效")
	ErrNotFound      = errors.New("事件控制记录不存在")
	ErrConflict      = errors.New("事件控制版本已变化")
	ErrExpired       = errors.New("事件或租约已过期")
	ErrUnavailable   = errors.New("当前分析目的与候选消费者尚不可用")
	ErrAuthorityJSON = errors.New("事件租约仅限服务端使用")
)

type EventType string
type SourceType string
type SourceStatus string
type State string
type HandlerVersion string
type ReasonCode string

const (
	MomentCreated            EventType      = "MOMENT_CREATED"
	MomentUpdated            EventType      = "MOMENT_UPDATED"
	MomentWithdrawn          EventType      = "MOMENT_WITHDRAWN"
	MomentSource             SourceType     = "MOMENT"
	Draft                    SourceStatus   = "draft"
	Withdrawn                SourceStatus   = "withdrawn"
	Pending                  State          = "PENDING"
	Leased                   State          = "LEASED"
	Unavailable              State          = "UNAVAILABLE"
	Invalidated              State          = "INVALIDATED"
	Expired                  State          = "EXPIRED"
	DeadLetter               State          = "DEAD_LETTER"
	CandidateStaged          State          = "CANDIDATE_STAGED"
	HandlerV1                HandlerVersion = "mom-control-v1"
	HandlerV2                HandlerVersion = "mom-control-v2"
	ReasonPurposeUnavailable ReasonCode     = "PURPOSE_UNAVAILABLE"
	ReasonSourceInvalidated  ReasonCode     = "SOURCE_INVALIDATED"
	ReasonExpired            ReasonCode     = "EXPIRED"
	ReasonAttemptsExhausted  ReasonCode     = "ATTEMPTS_EXHAUSTED"
)

type SourceReference struct {
	Type        SourceType            `json:"type"`
	ID          string                `json:"id"`
	Owner       actorref.PrincipalRef `json:"owner"`
	Revision    int64                 `json:"revision"`
	Status      SourceStatus          `json:"status"`
	Fingerprint string                `json:"fingerprint"`
}

// Envelope is distinct from AIR014's air.event.v1. The three event types record
// current native Moment mutations, not physical deletion history or permission.
type Envelope struct {
	SchemaVersion      string                `json:"schema_version"`
	EventID            string                `json:"event_id"`
	EventType          EventType             `json:"event_type"`
	Tenant             actorref.PrincipalRef `json:"tenant"`
	Subject            actorref.PrincipalRef `json:"subject"`
	Actor              actorref.ActorRef     `json:"actor"`
	AgentID            string                `json:"agent_id"`
	LogicalOperationID string                `json:"logical_operation_id"`
	OccurredAt         time.Time             `json:"occurred_at"`
	ReceivedAt         time.Time             `json:"received_at"`
	ExpiresAt          time.Time             `json:"expires_at"`
	Source             SourceReference       `json:"source"`
	RootTraceID        string                `json:"root_trace_id"`
	CausationID        *string               `json:"causation_id"`
}

type Record struct {
	Event         Envelope   `json:"event"`
	State         State      `json:"state"`
	Attempt       int64      `json:"attempt"`
	Fence         int64      `json:"fence"`
	LeaseOwner    string     `json:"lease_owner,omitempty"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func validID(value string) bool {
	if value != strings.TrimSpace(value) || value != strings.ToLower(value) || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	ref, err := actorref.ParsePrincipal("PERSON", value)
	return err == nil && ref.ID == value
}

func validPerson(value actorref.PrincipalRef) bool {
	return value.Type == actorref.Person && validID(value.ID)
}
func validTime(value time.Time) bool {
	return !value.IsZero() && value.UTC().Year() >= 1 && value.UTC().Year() <= 9999
}
func digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func ValidateHandlerVersion(value HandlerVersion) error {
	if value != HandlerV1 && value != HandlerV2 && value != MemoryHandler && value != PreferenceHandler {
		return ErrInvalid
	}
	return nil
}

func NormalizeWorkerID(value string) (string, error) {
	if !validID(value) {
		return "", ErrInvalid
	}
	return value, nil
}

// StableEventID binds metadata identity, not an access grant or business effect.
// Time/trace changes cannot renew a source or make a second logical mutation.
func StableEventID(e Envelope) string {
	raw, _ := json.Marshal(struct {
		Schema    string
		Type      EventType
		Tenant    actorref.PrincipalRef
		Subject   actorref.PrincipalRef
		Agent     string
		Operation string
		Source    SourceReference
	}{e.SchemaVersion, e.EventType, e.Tenant, e.Subject, e.AgentID, e.LogicalOperationID, e.Source})
	hash := sha256.Sum256(append([]byte("birdtie.outbox.identity.v1\x00"), raw...))
	hash[6] = hash[6]&0x0f | 0x80
	hash[8] = hash[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", hash[:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}

func ValidateEnvelope(e Envelope, now time.Time) error {
	if e.SchemaVersion == PreferenceSchema {
		return validatePreferenceEnvelope(e, now)
	}
	if e.SchemaVersion == MemorySchema {
		return validateMemoryEnvelope(e, now)
	}
	if e.SchemaVersion != SchemaVersion || !validPerson(e.Tenant) || !validPerson(e.Subject) || e.Tenant != e.Subject ||
		e.Actor.Type != actorref.Person || e.Actor.ID != e.Subject.ID || !validID(e.AgentID) || !validID(e.EventID) ||
		!validID(e.LogicalOperationID) || !validID(e.RootTraceID) || (e.CausationID != nil && !validID(*e.CausationID)) ||
		e.Source.Type != MomentSource || !validID(e.Source.ID) || e.Source.Owner != e.Subject || e.Source.Revision < 1 ||
		!digest(e.Source.Fingerprint) || !validTime(e.OccurredAt) || !validTime(e.ReceivedAt) || !validTime(e.ExpiresAt) ||
		!validTime(now) || e.OccurredAt.After(e.ReceivedAt) || e.ReceivedAt.After(now) ||
		!e.ExpiresAt.Equal(e.OccurredAt.Add(MaxEventTTL)) || e.EventID != StableEventID(e) {
		return ErrInvalid
	}
	switch e.EventType {
	case MomentCreated:
		if e.Source.Status != Draft || e.Source.Revision != 1 {
			return ErrInvalid
		}
	case MomentUpdated:
		if e.Source.Status != Draft || e.Source.Revision <= 1 {
			return ErrInvalid
		}
	case MomentWithdrawn:
		if e.Source.Status != Withdrawn || e.Source.Revision <= 1 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if !e.ExpiresAt.After(now) {
		return ErrExpired
	}
	raw, err := json.Marshal(struct{ Envelope }{e})
	if err != nil || len(raw) > MaxEnvelopeBytes {
		return ErrInvalid
	}
	return nil
}

func cloneEnvelope(e Envelope) Envelope {
	if e.CausationID != nil {
		copied := *e.CausationID
		e.CausationID = &copied
	}
	return e
}

func NewPending(e Envelope, now time.Time) (Record, error) {
	if err := ValidateEnvelope(e, now); err != nil {
		return Record{}, err
	}
	now = now.UTC()
	return Record{Event: cloneEnvelope(e), State: Pending, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}, nil
}

// ValidateRecord checks historical storage shape only. In particular validating
// at ReceivedAt does not prove current source, identity, permission or expiry.
func ValidateRecord(r Record) error {
	if err := ValidateEnvelope(r.Event, r.Event.ReceivedAt); err != nil {
		return err
	}
	if !validTime(r.CreatedAt) || !validTime(r.UpdatedAt) || !validTime(r.NextAttemptAt) ||
		r.CreatedAt.Before(r.Event.ReceivedAt) || r.UpdatedAt.Before(r.CreatedAt) ||
		r.NextAttemptAt.Before(r.CreatedAt) || r.Attempt < 0 || r.Attempt > MaxAttempts || r.Fence < 0 {
		return ErrInvalid
	}
	if r.Attempt > r.Fence {
		return ErrInvalid
	}
	if (r.Event.SchemaVersion == MemorySchema || r.Event.SchemaVersion == PreferenceSchema) && r.State != Pending && r.State != Leased && r.State != Invalidated && r.State != Expired && r.State != DeadLetter && r.State != MemoryComplete {
		return ErrInvalid
	}
	switch r.State {
	case Pending:
		if r.LeaseOwner != "" || r.LeaseUntil != nil {
			return ErrInvalid
		}
	case Leased:
		if r.Attempt < 1 || r.Fence < 1 || !validID(r.LeaseOwner) || r.LeaseUntil == nil || !validTime(*r.LeaseUntil) ||
			!r.LeaseUntil.After(r.UpdatedAt) || r.LeaseUntil.After(r.UpdatedAt.Add(MaxLeaseDuration)) || r.LeaseUntil.After(r.Event.ExpiresAt) {
			return ErrInvalid
		}
	case Unavailable, Invalidated, Expired, DeadLetter, CandidateStaged, MemoryComplete:
		if r.State == MemoryComplete && r.Event.SchemaVersion != MemorySchema && r.Event.SchemaVersion != PreferenceSchema {
			return ErrInvalid
		}
		if r.LeaseOwner != "" || r.LeaseUntil != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if r.Fence == math.MaxInt64 && r.State == Leased {
		return ErrInvalid
	}
	return nil
}
