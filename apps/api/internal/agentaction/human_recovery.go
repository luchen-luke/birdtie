package agentaction

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const HumanRecoverySchema = "air.sandbox_recovery_receipt.v1"
const HumanRecoveryMaxBytes = 8192

// HumanRecoveryReceipt is a historical outcome view, never an execution
// capability. Do not marshal Binding, Dispatch or a process-local Claim here.
type HumanRecoveryReceipt struct {
	SchemaVersion string                `json:"schemaVersion"`
	Owner         actorref.PrincipalRef `json:"owner"`
	ApprovalID    string                `json:"approvalId"`
	DispatchID    string                `json:"dispatchId"`
	Status        string                `json:"status"`
	CommittedAt   time.Time             `json:"committedAt"`
	EffectID      *string               `json:"effectId,omitempty"`
	AppliedAt     *time.Time            `json:"appliedAt,omitempty"`
}

type HumanRecoveryGateway interface {
	RecoverOwn(context.Context, agentprofile.PrivateAccess, string) (HumanRecoveryReceipt, error)
}

func recoveryTime(v time.Time) bool {
	return !v.IsZero() && v.Year() >= 1 && v.Year() <= 9999 && v.Equal(v.Truncate(time.Microsecond))
}

func ValidateHumanRecoveryReceipt(v HumanRecoveryReceipt, owner actorref.PrincipalRef, approvalID string) error {
	if v.SchemaVersion != HumanRecoverySchema || owner.Type != actorref.Person || !agentplanner.ValidID(owner.ID) ||
		v.Owner != owner || !agentplanner.ValidID(approvalID) || v.ApprovalID != approvalID || !agentplanner.ValidID(v.DispatchID) || !recoveryTime(v.CommittedAt) {
		return ErrUnavailable
	}
	switch v.Status {
	case Committed, InFlight, Unknown, NoEffect:
		if v.EffectID != nil || v.AppliedAt != nil {
			return ErrUnavailable
		}
	case Succeeded:
		if v.EffectID == nil || !agentplanner.ValidID(*v.EffectID) || v.AppliedAt == nil || !recoveryTime(*v.AppliedAt) || v.AppliedAt.Before(v.CommittedAt) {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	return nil
}

func NewHumanRecoveryReceipt(d Dispatch, owner actorref.PrincipalRef, approvalID string) (HumanRecoveryReceipt, error) {
	if d.Schema != Schema || d.TenantID != owner.ID || !hex64(d.EffectKey) ||
		(d.LeaseUntil != nil && (!recoveryTime(*d.LeaseUntil) || !d.LeaseUntil.After(d.CommittedAt))) ||
		(d.State == InFlight && d.LeaseUntil == nil) {
		return HumanRecoveryReceipt{}, ErrUnavailable
	}
	v := HumanRecoveryReceipt{SchemaVersion: HumanRecoverySchema, Owner: owner, ApprovalID: d.ApprovalID,
		DispatchID: d.ID, Status: d.State, CommittedAt: d.CommittedAt}
	if d.EffectID != nil {
		id := *d.EffectID
		v.EffectID = &id
	}
	if d.AppliedAt != nil {
		at := *d.AppliedAt
		v.AppliedAt = &at
	}
	if e := ValidateHumanRecoveryReceipt(v, owner, approvalID); e != nil {
		return HumanRecoveryReceipt{}, e
	}
	return v, nil
}
