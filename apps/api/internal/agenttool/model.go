// Package agenttool binds closed tool metadata to original native authority.
// Decisions are inspectable data. Only a process-local native Call can read.
package agenttool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"strings"
	"time"
	"unicode/utf8"
)

const Schema = "air.tool_decision.v1"
const ResultSchema = "air.readonly_tool_result.v1"
const Allow = "ALLOW"
const Deny = "DENY"
const Confirm = "CONFIRM"
const SandboxWrite = "sandbox.write"

var (
	ErrDenied       = errors.New("工具读取未获当前授权")
	ErrChanged      = errors.New("工具来源或策略已经变化")
	ErrUnavailable  = errors.New("工具当前不可用")
	ErrInvalid      = errors.New("工具参数无效")
	ErrLimit        = errors.New("工具调用达到原步骤或时间上限")
	ErrServerOnly   = errors.New("工具许可句柄仅供服务端使用")
	ErrConfirmation = errors.New("该沙箱版本需要本人确认；尚未执行")
)

type Decision struct {
	SchemaVersion      string    `json:"schema_version"`
	DecisionID         string    `json:"decision_id"`
	Disposition        string    `json:"decision"`
	ReasonCodes        []string  `json:"reason_codes"`
	Tool               string    `json:"tool"`
	ToolVersion        string    `json:"tool_version"`
	ActionID           string    `json:"action_id"`
	LogicalOperationID string    `json:"logical_operation_id"`
	ActorID            string    `json:"actor_id"`
	AgentID            string    `json:"agent_id"`
	SubjectType        string    `json:"subject_type"`
	SubjectID          string    `json:"subject_id"`
	PolicyVersion      string    `json:"policy_version"`
	ResourceVersion    string    `json:"resource_version"`
	ArgumentsDigest    string    `json:"arguments_digest"`
	Purpose            string    `json:"purpose"`
	DataDestinations   []string  `json:"data_destinations"`
	ObservedAt         time.Time `json:"observed_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}
type Result struct {
	SchemaVersion string                `json:"schema_version"`
	DecisionID    string                `json:"decision_id"`
	ActionID      string                `json:"action_id"`
	Tool          string                `json:"tool"`
	Activities    []foundation.Activity `json:"activities"`
	ObservedAt    time.Time             `json:"observed_at"`
	ValidUntil    time.Time             `json:"valid_until"`
}

// Neither interface is accepted from JSON or reconstructed from a Decision.
type Call interface {
	Read(context.Context, agentevent.Access, *agentfeature.Controller) (Result, error)
}
type Plan interface {
	Check(context.Context, agentevent.Access, agentplanner.ActionProposal, *agentfeature.Controller) (Decision, Call, error)
}
type NativePlanFactory interface{ NativeToolPlan() Plan }
type SandboxProposal struct {
	ActionID           string `json:"action_id"`
	LogicalOperationID string `json:"logical_operation_id"`
	TargetID           string `json:"target_id"`
	Value              string `json:"value"`
	ResourceVersion    string `json:"resource_version"`
}
type SandboxPort interface {
	CheckOwnSandboxTool(context.Context, agentevent.Access, agentplanner.PreparedGoal, SandboxProposal, *agentfeature.Controller) (Decision, error)
}

func (p SandboxProposal) Valid() bool {
	return agentplanner.ValidID(p.ActionID) && agentplanner.ValidID(p.LogicalOperationID) && agentplanner.ValidID(p.TargetID) && bounded(p.Value, 240) && bounded(p.ResourceVersion, 240)
}
func bounded(s string, max int) bool {
	return s != "" && strings.TrimSpace(s) == s && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func Digest(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	h := sha256.Sum256(append([]byte("birdtie.native-tool-metadata.v1\x00"), b...))
	return hex.EncodeToString(h[:])
}
func Clone(d Decision) Decision {
	d.ReasonCodes = append([]string{}, d.ReasonCodes...)
	d.DataDestinations = append([]string{}, d.DataDestinations...)
	return d
}
func DecodeProposal(raw []byte) (agentplanner.ActionProposal, error) {
	var p agentplanner.ActionProposal
	if len(raw) == 0 || len(raw) > 8192 || json.Unmarshal(raw, &p) != nil {
		return p, ErrInvalid
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	v := agentplanner.NewView(agentplanner.Proposed, "11111111-1111-4111-8111-111111111111", p.LogicalOperationID, "proposal_shape_only", now, now.Add(time.Second))
	wrapper := struct {
		agentplanner.View
		Actions []json.RawMessage `json:"actions"`
	}{v, []json.RawMessage{raw}}
	encoded, e := json.Marshal(wrapper)
	if e != nil {
		return agentplanner.ActionProposal{}, ErrInvalid
	}
	view, e := agentplanner.Decode(encoded)
	if e != nil || len(view.Actions) != 1 {
		return agentplanner.ActionProposal{}, ErrInvalid
	}
	return view.Actions[0], nil
}
