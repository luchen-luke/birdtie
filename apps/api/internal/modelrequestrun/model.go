// Package modelrequestrun describes minimal execution-control metadata.
// Shape validation and state observations do not authorize dispatch, budget,
// private-answer release or continuation. Native ports must establish those
// independently from the original approval, source and process-local ticket.
package modelrequestrun

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
)

const SchemaVersion = "model-request-run-v1"
const LiveSourceSchemaVersion = "model-request-run-v2"
const LiveSourceAnswer = "LIVE_SOURCE_ANSWER"
const SourceRetrieval = "SOURCE_RETRIEVAL"
const ModelInference = "MODEL_INFERENCE"
const Bound = "BOUND"
const WaitingSource = "WAITING_SOURCE"
const MaxWindow = 2 * time.Minute

var (
	ErrInvalid    = errors.New("模型请求运行记录无效")
	ErrChanged    = errors.New("模型请求运行记录或原截止时间已变化")
	ErrExpired    = errors.New("模型请求运行原期限已过")
	ErrServerOnly = errors.New("模型运行时钟仅供服务端使用")
	digest        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	identifier    = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)
)

type RunState string

const (
	RunPlanned   RunState = "PLANNED"
	RunRunning   RunState = "RUNNING"
	RunFinished  RunState = "FINISHED"
	RunStopped   RunState = "STOPPED"
	RunCancelled RunState = "CANCELLED"
	RunExpired   RunState = "EXPIRED"
)

type StepState string

const (
	StepPlanned   StepState = "PLANNED"
	StepReserved  StepState = "RESERVED"
	StepInFlight  StepState = "IN_FLIGHT"
	StepSettled   StepState = "SETTLED"
	StepUnknown   StepState = "UNKNOWN"
	StepCancelled StepState = "CANCELLED_BEFORE_SEND"
)

type Step struct {
	Ordinal              int       `json:"ordinal"`
	OperationID          string    `json:"operationId"`
	PreviewID            string    `json:"previewId,omitempty"`
	PriceVersion         string    `json:"priceVersion"`
	RequestDigest        string    `json:"requestDigest,omitempty"`
	State                StepState `json:"state"`
	ReservationID        string    `json:"reservationId,omitempty"`
	Kind                 string    `json:"stepKind,omitempty"`
	BindingState         string    `json:"bindingState,omitempty"`
	SourceEvidenceDigest string    `json:"sourceEvidenceDigest,omitempty"`
	MaxOutputTokens      int       `json:"maxOutputTokens,omitempty"`
}

// Control can be owner-readable JSON metadata. Its IDs/revision/fence never
// form a claim, grant, continuation receipt or reconstructible gate ticket.
type Control struct {
	SchemaVersion string                `json:"schemaVersion"`
	Kind          string                `json:"runKind,omitempty"`
	ModelRunID    string                `json:"modelRunId"`
	Owner         actorref.PrincipalRef `json:"owner"`
	TaskID        string                `json:"taskId"`
	RootTraceID   string                `json:"rootTraceId"`
	BindingID     string                `json:"bindingId"`
	State         RunState              `json:"state"`
	Revision      int64                 `json:"revision"`
	Fence         int64                 `json:"fence"`
	CreatedAt     time.Time             `json:"createdAt"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	DeadlineAt    time.Time             `json:"deadlineAt"`
	LeaseUntil    time.Time             `json:"leaseUntil"`
	Steps         []Step                `json:"steps"`
}

func canonicalID(id string) bool {
	r, e := actorref.Parse("PERSON", id)
	return e == nil && r.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func runState(s RunState) bool {
	switch s {
	case RunPlanned, RunRunning, RunFinished, RunStopped, RunCancelled, RunExpired:
		return true
	}
	return false
}
func stepState(s StepState) bool {
	switch s {
	case StepPlanned, StepReserved, StepInFlight, StepSettled, StepUnknown, StepCancelled:
		return true
	}
	return false
}

// ValidateControl admits historical metadata even after its original deadline.
// It does not renew that deadline or interpret accounting as permission.
func ValidateControl(v Control, now time.Time) error {
	live := v.SchemaVersion == LiveSourceSchemaVersion && v.Kind == LiveSourceAnswer
	if now.IsZero() || (!live && (v.SchemaVersion != SchemaVersion || v.Kind != "")) || v.Owner.Type != actorref.Person || !canonicalID(v.Owner.ID) || !canonicalID(v.ModelRunID) || !canonicalID(v.TaskID) || !canonicalID(v.RootTraceID) || !canonicalID(v.BindingID) || v.ModelRunID == v.BindingID || !runState(v.State) || v.Revision < 1 || v.Fence < 1 {
		return ErrInvalid
	}
	if v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() || v.DeadlineAt.IsZero() || v.LeaseUntil.IsZero() || v.CreatedAt.After(v.UpdatedAt) || v.UpdatedAt.After(now) || !v.DeadlineAt.After(v.CreatedAt) || v.DeadlineAt.Sub(v.CreatedAt) > MaxWindow || !v.LeaseUntil.After(v.CreatedAt) || v.LeaseUntil.After(v.DeadlineAt) {
		return ErrInvalid
	}
	if len(v.Steps) < 1 || len(v.Steps) > modelresilience.MaxAttempts {
		return ErrInvalid
	}
	if live && len(v.Steps) != 2 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	hasFinished := false
	for i, s := range v.Steps {
		waiting := live && i == 1 && s.BindingState == WaitingSource
		if s.Ordinal != i+1 || !canonicalID(s.OperationID) || (!waiting && !canonicalID(s.PreviewID)) || seen[s.OperationID] || !identifier.MatchString(s.PriceVersion) || (!waiting && !digest.MatchString(s.RequestDigest)) || !stepState(s.State) {
			return ErrInvalid
		}
		if !live && (s.Kind != "" || s.BindingState != "" || s.SourceEvidenceDigest != "" || s.MaxOutputTokens != 0) {
			return ErrInvalid
		}
		if live {
			if i == 0 && (s.Kind != SourceRetrieval || s.BindingState != Bound || s.SourceEvidenceDigest != "" || s.MaxOutputTokens != 0) {
				return ErrInvalid
			}
			if i == 1 {
				if s.Kind != ModelInference || s.MaxOutputTokens < 1 || s.MaxOutputTokens > 768 {
					return ErrInvalid
				}
				if waiting {
					if s.PreviewID != "" || s.RequestDigest != "" || s.SourceEvidenceDigest != "" || s.State != StepPlanned || s.ReservationID != "" {
						return ErrInvalid
					}
				} else if s.BindingState != Bound || !digest.MatchString(s.SourceEvidenceDigest) || s.SourceEvidenceDigest == strings.Repeat("0", 64) || v.Steps[0].State != StepUnknown {
					return ErrInvalid
				}
			}
			if s.State == StepSettled || s.State == StepCancelled {
				return ErrInvalid
			}
		}
		seen[s.OperationID] = true
		if v.State == RunPlanned && s.State != StepPlanned {
			return ErrInvalid
		}
		if s.State == StepPlanned {
			if s.ReservationID != "" {
				return ErrInvalid
			}
		} else if s.ReservationID != s.OperationID {
			return ErrInvalid
		}
		if s.State == StepSettled || s.State == StepUnknown {
			hasFinished = true
		}
		if v.State == RunFinished && (s.State == StepReserved || s.State == StepInFlight) {
			return ErrInvalid
		}
	}
	if v.State == RunFinished && (!hasFinished || (live && (v.Steps[1].State != StepUnknown || v.Steps[1].BindingState != Bound))) {
		return ErrInvalid
	}
	return nil
}

// ValidatePlan only validates a fresh metadata proposal. Actual native Run
// creation still needs the original exact approved A/B/source closure.
func ValidatePlan(v Control, now time.Time) error {
	if e := ValidateControl(v, now); e != nil {
		return e
	}
	if v.State != RunPlanned {
		return ErrChanged
	}
	for _, s := range v.Steps {
		if s.State != StepPlanned {
			return ErrChanged
		}
	}
	if !now.Before(v.LeaseUntil) || !now.Before(v.DeadlineAt) {
		return ErrExpired
	}
	return nil
}
func Clone(v Control) Control { v.Steps = append([]Step(nil), v.Steps...); return v }

func observedStepTransition(a, b StepState) bool {
	if a == b {
		return true
	}
	switch a {
	case StepPlanned:
		return b == StepReserved
	case StepReserved:
		return b == StepInFlight || b == StepCancelled
	case StepInFlight:
		return b == StepSettled || b == StepUnknown
	case StepUnknown:
		return b == StepSettled
	}
	return false
}
func observedRunTransition(a, b RunState) bool {
	if a == b {
		return true
	}
	switch a {
	case RunPlanned:
		return b == RunRunning || b == RunStopped || b == RunCancelled || b == RunExpired
	case RunRunning:
		return b == RunFinished || b == RunStopped || b == RunCancelled || b == RunExpired
	}
	return false
}

// ValidateObservation compares adjacent native metadata snapshots only. A
// terminal Run may receive settled accounting, but never become RUNNING again.
// This function does not perform the transition or prove its original ledger.
func ValidateObservation(before, after Control, now time.Time) error {
	if ValidateControl(before, now) != nil || ValidateControl(after, now) != nil {
		return ErrInvalid
	}
	if before.SchemaVersion != after.SchemaVersion || before.Kind != after.Kind || before.ModelRunID != after.ModelRunID || before.Owner != after.Owner || before.TaskID != after.TaskID || before.RootTraceID != after.RootTraceID || before.BindingID != after.BindingID || !before.CreatedAt.Equal(after.CreatedAt) || len(before.Steps) != len(after.Steps) || after.DeadlineAt.After(before.DeadlineAt) || after.LeaseUntil.After(before.LeaseUntil) || after.Fence < before.Fence || after.Revision < before.Revision || after.UpdatedAt.Before(before.UpdatedAt) || !observedRunTransition(before.State, after.State) {
		return ErrChanged
	}
	if after.Revision == before.Revision && !reflect.DeepEqual(before, after) {
		return ErrChanged
	}
	for i, a := range before.Steps {
		b := after.Steps[i]
		binding := before.Kind == LiveSourceAnswer && i == 1 && a.BindingState == WaitingSource && b.BindingState == Bound && before.State == RunRunning && after.State == RunRunning && a.State == StepPlanned && b.State == StepPlanned && before.Steps[0].State == StepUnknown && after.Revision > before.Revision && after.Fence == before.Fence
		if a.Ordinal != b.Ordinal || a.OperationID != b.OperationID || a.PriceVersion != b.PriceVersion || a.Kind != b.Kind || a.MaxOutputTokens != b.MaxOutputTokens || (!binding && (a.PreviewID != b.PreviewID || a.RequestDigest != b.RequestDigest || a.BindingState != b.BindingState || a.SourceEvidenceDigest != b.SourceEvidenceDigest)) || !observedStepTransition(a.State, b.State) {
			return ErrChanged
		}
		if a.ReservationID != "" && a.ReservationID != b.ReservationID {
			return ErrChanged
		}
		if before.State != RunPlanned && before.State != RunRunning && a.State != b.State && (b.State == StepReserved || b.State == StepInFlight) {
			return ErrChanged
		}
	}
	return nil
}

// ClockBound is a relative clock constraint, not a native authority receipt.
// It is intentionally not persistable; restart metadata cannot rebuild it.
type ClockBound struct{ until time.Time }

func NewClockBound(observed, expires, queryStarted time.Time) (ClockBound, error) {
	if observed.IsZero() || expires.IsZero() || queryStarted.IsZero() || !expires.After(observed) || expires.Sub(observed) > MaxWindow {
		return ClockBound{}, ErrInvalid
	}
	return ClockBound{until: queryStarted.Add(expires.Sub(observed))}, nil
}
func (b ClockBound) Remaining(now time.Time) time.Duration {
	if now.IsZero() || b.until.IsZero() || !now.Before(b.until) {
		return 0
	}
	return b.until.Sub(now)
}
func (b ClockBound) Tighten(until time.Time) (ClockBound, error) {
	if b.until.IsZero() || until.IsZero() {
		return ClockBound{}, ErrInvalid
	}
	if until.After(b.until) {
		return b, ErrChanged
	}
	b.until = until
	return b, nil
}
func (ClockBound) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (b *ClockBound) UnmarshalJSON([]byte) error { *b = ClockBound{}; return ErrServerOnly }
