// Package agentaction describes the closed, private sandbox action protocol.
// JSON views never confer permission. consent_grants is the only grant lifecycle.
package agentaction

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"time"
)

const Schema = "air.sandbox_action.v1"
const Purpose = "OWN_SANDBOX_ACTION"
const EffectKind = "OWN_SANDBOX_WRITE"
const ToolVersion = "sandbox.write.v1"
const Pending = "AWAITING_APPROVAL"
const Approved = "APPROVED_NOT_EXECUTED"
const Consumed = "DISPATCH_COMMITTED"
const Committed = "DISPATCH_COMMITTED"
const InFlight = "IN_FLIGHT"
const Unknown = "UNKNOWN_OUTCOME"
const Succeeded = "SUCCEEDED"
const NoEffect = "NO_EFFECT"
const Cancelled = "CANCELLED"

var (
	ErrInvalid     = errors.New("动作参数无效")
	ErrDenied      = errors.New("动作未获当前本人授权")
	ErrChanged     = errors.New("动作版本已改变，请重新确认")
	ErrExpired     = errors.New("动作批准已过期")
	ErrApproval    = errors.New("此版本需要本人明确批准")
	ErrUnavailable = errors.New("动作当前不可用")
	ErrUnknown     = errors.New("动作结果未知，请核查结果；不会自动重发")
	ErrConsumed    = errors.New("批准已消费，不代表执行成功")
	ErrServerOnly  = errors.New("执行句柄仅供服务端使用")
)

// The full immutable review binding. NONE_PERSON_ONLY is an explicit scope
// restriction, not an invented organization membership or permission epoch.
type Binding struct {
	Schema             string    `json:"schema_version"`
	ApprovalID         string    `json:"approval_id"`
	TenantID           string    `json:"tenant_id"`
	ActorID            string    `json:"actor_id"`
	SubjectType        string    `json:"subject_type"`
	SubjectID          string    `json:"subject_id"`
	AgentID            string    `json:"agent_id"`
	SessionID          string    `json:"session_id"`
	TaskID             string    `json:"task_id"`
	LogicalOperationID string    `json:"logical_operation_id"`
	ActionID           string    `json:"action_id"`
	Tool               string    `json:"tool"`
	ToolVersion        string    `json:"tool_version"`
	TargetID           string    `json:"target_id"`
	PayloadDigest      string    `json:"payload_digest"`
	SourceVersion      string    `json:"source_version"`
	SourceGeneration   string    `json:"source_generation"`
	AuthorityVersion   string    `json:"authority_version"`
	PolicyVersion      string    `json:"policy_version"`
	ConsentPurpose     string    `json:"consent_purpose"`
	GrantID            string    `json:"grant_id"`
	GrantRevision      int64     `json:"grant_revision"`
	MembershipVersion  string    `json:"membership_version"`
	ObservedAt         time.Time `json:"observed_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}
type Preview struct {
	Binding       Binding                   `json:"binding"`
	BindingDigest string                    `json:"binding_digest"`
	Proposal      agenttool.SandboxProposal `json:"proposal"`
	State         string                    `json:"state"`
}
type Dispatch struct {
	Schema      string     `json:"schema_version"`
	ID          string     `json:"dispatch_id"`
	ApprovalID  string     `json:"approval_id"`
	TenantID    string     `json:"tenant_id"`
	EffectKey   string     `json:"effect_key"`
	State       string     `json:"state"`
	CommittedAt time.Time  `json:"committed_at"`
	LeaseUntil  *time.Time `json:"lease_until,omitempty"`
	EffectID    *string    `json:"effect_id,omitempty"`
	AppliedAt   *time.Time `json:"applied_at,omitempty"`
}

// Opaque process-local capabilities are returned only by a confirmed native
// transaction. Restart recovery can reconcile persisted views, never recreate
// either capability or automatically dispatch from a persisted JSON view.
type Commitment interface {
	Begin(context.Context, agentevent.Access, *agentfeature.Controller) (Dispatch, Claim, error)
}
type Claim interface {
	Execute(context.Context, agentevent.Access, *agentfeature.Controller) (Dispatch, error)
}
type Port interface {
	PreviewOwnSandboxAction(context.Context, agentevent.Access, agentplanner.PreparedGoal, agenttool.SandboxProposal, *agentfeature.Controller) (Preview, error)
	ApproveOwnSandboxAction(context.Context, agentevent.Access, string, string, *agentfeature.Controller) (Preview, error)
	CommitOwnSandboxAction(context.Context, agentevent.Access, string, agenttool.SandboxProposal, *agentfeature.Controller) (Dispatch, Commitment, error)
	CancelOwnSandboxAction(context.Context, agentevent.Access, string, *agentfeature.Controller) (Preview, error)
	ReadOwnSandboxDispatch(context.Context, agentevent.Access, string, *agentfeature.Controller) (Dispatch, error)
	ReconcileOwnSandboxDispatch(context.Context, agentevent.Access, string, *agentfeature.Controller) (Dispatch, error)
	MarkOwnSandboxUnknown(context.Context, agentevent.Access, string, *agentfeature.Controller) (Dispatch, error)
}
