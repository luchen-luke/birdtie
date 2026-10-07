// Package agentrun describes bounded native runtime metadata. Shapes, states,
// references and leases never grant access to a source, a model or a writer.
package agentrun

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const Schema = "agent-enrichment-run-v1"
const MaxAttempts int64 = 5
const MaxLease = 30 * time.Second
const MaxLifetime = 15 * time.Minute

type State string

const (
	Queued              State = "QUEUED"
	Running             State = "RUNNING"
	WaitingConfirmation State = "WAITING_CONFIRMATION"
	RetryWait           State = "RETRY_WAIT"
	Succeeded           State = "SUCCEEDED"
	Failed              State = "FAILED"
	Cancelled           State = "CANCELLED"
	Expired             State = "EXPIRED"
)

type Checkpoint string

const (
	Scheduled       Checkpoint = "SCHEDULED"
	Validated       Checkpoint = "VALIDATED"
	DispatchPending Checkpoint = "DISPATCH_PENDING"
	ReconcileEffect Checkpoint = "RECONCILE_EFFECT"
	EffectConfirmed Checkpoint = "EFFECT_CONFIRMED"
)

var (
	ErrInvalid  = errors.New("运行输入无效")
	ErrDenied   = errors.New("当前身份或来源不允许此操作")
	ErrExpired  = errors.New("运行、租约或具体许可已到期")
	ErrConflict = errors.New("运行版本或处理租约已变化，请核实当前结果")
	ErrNotFound = errors.New("当前运行不可用")
	// Native bounded worker control result: a failed head was durably retired;
	// this is not an empty queue and must not be returned as a human receipt.
	ErrClaimExhausted = errors.New("耗尽的运行已记录失败，请继续下一项")
	ErrUnavailable    = errors.New("异步处理当前不可用")
	ErrAuthorityJSON  = errors.New("运行租约仅供原生服务使用")
)

func Terminal(s State) bool { return s == Succeeded || s == Failed || s == Cancelled || s == Expired }
func ValidCheckpoint(c Checkpoint) bool {
	return c == Scheduled || c == Validated || c == DispatchPending || c == ReconcileEffect || c == EffectConfirmed
}
func CanTransition(from, to State) bool {
	switch from {
	case Queued:
		return to == Running || to == WaitingConfirmation || to == Cancelled || to == Expired
	case Running:
		return to == RetryWait || to == Succeeded || to == Failed || to == Cancelled || to == Expired
	case WaitingConfirmation:
		return to == Queued || to == Cancelled || to == Expired
	case RetryWait:
		return to == Running || to == Failed || to == Cancelled || to == Expired
	default:
		return false
	}
}
func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }

type Claim struct {
	RunID, WorkerID string
	Fence, Attempt  int64
	LeaseUntil      time.Time
}

func (Claim) MarshalJSON() ([]byte, error) { return nil, ErrAuthorityJSON }
func (c *Claim) UnmarshalJSON([]byte) error {
	if c != nil {
		*c = Claim{}
	}
	return ErrAuthorityJSON
}
func ValidateClaim(c Claim) error {
	if !agentenrichmentpurpose.ValidID(c.RunID) || !agentenrichmentpurpose.ValidID(c.WorkerID) || c.Fence <= 0 || c.Fence == math.MaxInt64 || c.Attempt < 1 || c.Attempt > MaxAttempts || c.Attempt > c.Fence || !validTime(c.LeaseUntil) {
		return ErrInvalid
	}
	return nil
}
func CheckLease(now time.Time, current, supplied Claim) error {
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

type Record struct {
	SchemaVersion           string                     `json:"schemaVersion"`
	ID                      string                     `json:"id"`
	Owner                   actorref.PrincipalRef      `json:"owner"`
	AgentID                 string                     `json:"agentId"`
	Source                  agentevent.SourceReference `json:"source"`
	EventID                 string                     `json:"eventId"`
	LogicalOperationID      string                     `json:"logicalOperationId"`
	State                   State                      `json:"state"`
	Checkpoint              Checkpoint                 `json:"checkpoint"`
	Version                 int64                      `json:"version"`
	Attempt                 int64                      `json:"attempt"`
	Reason                  string                     `json:"reason"`
	CreatedAt               time.Time                  `json:"createdAt"`
	UpdatedAt               time.Time                  `json:"updatedAt"`
	Deadline                time.Time                  `json:"deadline"`
	ObservedAt              time.Time                  `json:"observedAt"`
	CandidateID             string                     `json:"candidateId,omitempty"`
	RetentionGrantID        string                     `json:"retentionGrantId,omitempty"`
	Committed               bool                       `json:"committed"`
	ModelAccess             bool                       `json:"modelAccess"`
	MemoryPromotionAllowed  bool                       `json:"memoryPromotionAllowed"`
	Generation              int64                      `json:"generation,omitempty"`
	RecoveryRootID          string                     `json:"recoveryRootId,omitempty"`
	RecoveryReason          string                     `json:"recoveryReason,omitempty"`
	RecoveryPreviousVersion int64                      `json:"recoveryPreviousVersion,omitempty"`
}

// ValidateRecord checks only the closed public shape, not native permission.
func ValidateRecord(r Record) error {
	if r.Generation < 0 || r.Generation > MaxRecoveryGenerations ||
		(r.Generation == 0 && (r.RecoveryRootID != "" || r.RecoveryReason != "" || r.RecoveryPreviousVersion != 0)) ||
		(r.Generation == 1 && (!agentenrichmentpurpose.ValidID(r.RecoveryRootID) || r.RecoveryRootID == r.ID || r.RecoveryReason != RecoveryReason || r.RecoveryPreviousVersion < 1 || r.Attempt > MaxRecoveryAttempts)) {
		return ErrUnavailable
	}
	if r.SchemaVersion != Schema || r.Owner.Type != actorref.Person || !agentenrichmentpurpose.ValidID(r.Owner.ID) || !agentenrichmentpurpose.ValidID(r.ID) || !agentenrichmentpurpose.ValidID(r.AgentID) || !agentenrichmentpurpose.ValidID(r.EventID) || !agentenrichmentpurpose.ValidID(r.LogicalOperationID) || r.Version < 1 || r.Attempt < 0 || r.Attempt > MaxAttempts || !ValidCheckpoint(r.Checkpoint) || r.Source.Type != agentevent.MomentSource || r.Source.Owner != r.Owner || !agentenrichmentpurpose.ValidID(r.Source.ID) || r.Source.Version.Kind != agentevent.RevisionVersion || r.Source.Version.Revision < 1 || r.Source.Version.Token != "" || r.ModelAccess || r.MemoryPromotionAllowed {
		return ErrUnavailable
	}
	switch r.State {
	case Queued, Running, WaitingConfirmation, RetryWait, Succeeded, Failed, Cancelled, Expired:
	default:
		return ErrUnavailable
	}
	switch r.Reason {
	case "WAITING_APPROVAL", "QUEUED", "DISPATCHING", "RECONCILING", "COMMITTED", "CANCELLED", "EXPIRED", "RETRY_BUSY", "RETRY_UNAVAILABLE", "AUTHORITY_CHANGED", "ATTEMPTS_EXHAUSTED", "INVALID_INPUT":
	default:
		return ErrUnavailable
	}
	if !validTime(r.CreatedAt) || !validTime(r.UpdatedAt) || !validTime(r.Deadline) || !validTime(r.ObservedAt) || r.UpdatedAt.Before(r.CreatedAt) || r.CreatedAt.After(r.ObservedAt) || r.UpdatedAt.After(r.ObservedAt) || !r.Deadline.After(r.CreatedAt) || r.Deadline.Sub(r.CreatedAt) > MaxLifetime {
		return ErrUnavailable
	}
	if r.RetentionGrantID != "" && !agentenrichmentpurpose.ValidID(r.RetentionGrantID) {
		return ErrUnavailable
	}
	if r.Committed != (r.State == Succeeded) || (r.State == Succeeded) != agentenrichmentpurpose.ValidID(r.CandidateID) || (r.CandidateID != "" && r.State != Succeeded) {
		return ErrUnavailable
	}
	if (r.State == Queued || r.State == Running || r.State == RetryWait || r.State == Succeeded) && r.RetentionGrantID == "" {
		return ErrUnavailable
	}
	validReason := false
	switch r.State {
	case WaitingConfirmation:
		validReason = r.Reason == "WAITING_APPROVAL" && r.Checkpoint == Scheduled
	case Queued:
		validReason = r.Reason == "QUEUED"
	case Running:
		validReason = r.Reason == "DISPATCHING" || r.Reason == "RECONCILING"
	case RetryWait:
		validReason = r.Reason == "RETRY_BUSY" || r.Reason == "RETRY_UNAVAILABLE" || r.Reason == "RECONCILING"
	case Succeeded:
		validReason = r.Reason == "COMMITTED" && r.Checkpoint == EffectConfirmed
	case Failed:
		validReason = r.Reason == "AUTHORITY_CHANGED" || r.Reason == "ATTEMPTS_EXHAUSTED" || r.Reason == "INVALID_INPUT"
	case Cancelled:
		validReason = r.Reason == "CANCELLED"
	case Expired:
		validReason = r.Reason == "EXPIRED"
	}
	if !validReason {
		return ErrUnavailable
	}
	return nil
}

// No Session/token/digest, source body, query, coordinates, raw provider error
// or arbitrary reasoning fields can be represented in a runtime record.
type Input struct {
	MomentID         string `json:"momentId"`
	RetentionGrantID string `json:"retentionGrantId,omitempty"`
}

func ValidateInput(in Input) error {
	if !agentenrichmentpurpose.ValidID(in.MomentID) || (in.RetentionGrantID != "" && !agentenrichmentpurpose.ValidID(in.RetentionGrantID)) {
		return ErrInvalid
	}
	return nil
}

type Gateway interface {
	ScheduleOwn(context.Context, agentprofile.PrivateAccess, Input) (Record, error)
	ReadOwn(context.Context, agentprofile.PrivateAccess, string) (Record, error)
	CancelOwn(context.Context, agentprofile.PrivateAccess, string, int64) (Record, error)
	AttachOwnGrant(context.Context, agentprofile.PrivateAccess, string, int64, string) (Record, error)
}
