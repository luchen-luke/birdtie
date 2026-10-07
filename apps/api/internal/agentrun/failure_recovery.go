package agentrun

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

// This is AIR017's new human recovery limit, not the original AIR016 limit or
// a model/accounting quota. Original automatic runs still have MaxAttempts=5.
const MaxRootAttempts int64 = 6
const MaxRecoveryGenerations int64 = 1
const MaxRecoveryAttempts int64 = 1
const RecoveryReason = "RETRY_TRANSIENT_NO_EFFECT"

type FailureClass string

const (
	FailureNone      FailureClass = "NONE"
	FailurePermanent FailureClass = "PERMANENT"
	FailureTemporary FailureClass = "TEMPORARY"
	FailureExhausted FailureClass = "EXHAUSTED"
	FailureUnknown   FailureClass = "RECONCILIATION_REQUIRED"
)

func ClassifyReason(reason string) FailureClass {
	switch reason {
	case "INVALID_INPUT", "AUTHORITY_CHANGED", "CANCELLED", "EXPIRED":
		return FailurePermanent
	case "RETRY_BUSY":
		return FailureTemporary
	case "ATTEMPTS_EXHAUSTED":
		// The historical reason records a limit, not the original cause.
		return FailureExhausted
	case "RECONCILING", "RETRY_UNAVAILABLE":
		return FailureUnknown
	default:
		return FailureNone
	}
}

type RecoveryInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Reason          string `json:"reason"`
}

func ValidateRecoveryInput(in RecoveryInput) error {
	if in.ExpectedVersion < 1 || in.Reason != RecoveryReason {
		return ErrInvalid
	}
	return nil
}

type FailureView struct {
	Run              Record       `json:"run"`
	Failure          FailureClass `json:"failure"`
	DeadLetter       bool         `json:"deadLetter"`
	Recoverable      bool         `json:"recoverable"`
	RecoveryState    string       `json:"recoveryState"`
	RootAttemptLimit int64        `json:"rootAttemptLimit"`
	Explanation      string       `json:"explanation"`
}
type RecoveryGateway interface {
	ReadOwnFailure(context.Context, agentprofile.PrivateAccess, string) (FailureView, error)
	ListOwnFailures(context.Context, agentprofile.PrivateAccess) ([]FailureView, error)
	RecoverOwn(context.Context, agentprofile.PrivateAccess, string, RecoveryInput) (Record, error)
}

func ValidateFailureView(v FailureView) error {
	if ValidateRecord(v.Run) != nil || (v.Failure != ClassifyReason(v.Run.Reason) && !(v.Failure == FailureUnknown && v.RecoveryState == "RECONCILIATION_REQUIRED")) || v.DeadLetter != (v.Run.State == Failed) || v.RootAttemptLimit != MaxRootAttempts || v.Explanation == "" {
		return ErrUnavailable
	}
	switch v.RecoveryState {
	case "AVAILABLE", "NOT_ELIGIBLE", "SOURCE_UNAVAILABLE", "RECONCILIATION_REQUIRED", "ALREADY_RECOVERED":
	default:
		return ErrUnavailable
	}
	if v.Recoverable != (v.RecoveryState == "AVAILABLE") || (v.Recoverable && (v.Run.Generation != 0 || v.Run.State != Failed || v.Run.Reason != "ATTEMPTS_EXHAUSTED")) {
		return ErrUnavailable
	}
	return nil
}
