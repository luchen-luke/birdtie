// Package modelgateway owns normalized model contracts, not Agent identity,
// permission, Memory, tool execution or live provider authorization. The default
// Gateway cannot invoke a provider. OfflineHarness is explicitly synthetic.
package modelgateway

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
)

const RequestVersion = "air.model_request.v1"
const ResultVersion = "air.model_result.v1"
const MaxRequestBytes = 32 * 1024
const MaxResultBytes = 32 * 1024
const MaxDeadline = 2 * time.Minute

var (
	ErrInvalid     = errors.New("invalid model gateway request")
	ErrUnavailable = errors.New("model gateway unavailable")
	ErrAdapter     = errors.New("invalid model adapter response")
	ErrDeadline    = errors.New("model gateway deadline exceeded")
)

type TaskKind string
type OutputMode string
type Status string
type ExecutionMode string

const (
	ActivityQuery             TaskKind      = "ACTIVITY_QUERY"
	MemoryCandidateExtraction TaskKind      = "MEMORY_CANDIDATE_EXTRACTION"
	Text                      OutputMode    = "TEXT"
	Structured                OutputMode    = "STRUCTURED"
	ToolProposals             OutputMode    = "TOOL_PROPOSALS"
	Completed                 Status        = "COMPLETED"
	Refused                   Status        = "REFUSED"
	Truncated                 Status        = "TRUNCATED"
	Invalid                   Status        = "INVALID"
	Unavailable               Status        = "UNAVAILABLE"
	Disabled                  ExecutionMode = "DISABLED"
	OfflineContract           ExecutionMode = "OFFLINE_CONTRACT"
	Live                      ExecutionMode = "LIVE"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Budget struct {
	MaxOutputTokens int `json:"max_output_tokens"`
}

// All internal refs are non-authoritative selectors. They never become a
// provider payload, ownership proof, consent, approval or a funding receipt.
type Request struct {
	SchemaVersion        string                        `json:"schema_version"`
	RunID                string                        `json:"run_id"`
	Agent                agentcognitive.AgentReference `json:"agent_ref"`
	TaskKind             TaskKind                      `json:"task_kind"`
	PromptVersion        string                        `json:"prompt_version"`
	InputSchemaVersion   string                        `json:"input_schema_version"`
	OutputSchemaVersion  string                        `json:"output_schema_version"`
	ContextSnapshotRef   string                        `json:"context_snapshot_ref"`
	DataPolicyRef        string                        `json:"data_policy_ref"`
	BudgetRef            string                        `json:"budget_ref"`
	Budget               Budget                        `json:"budget"`
	Messages             []Message                     `json:"messages"`
	OutputMode           OutputMode                    `json:"output_mode"`
	ToolAllowlist        []string                      `json:"tool_allowlist"`
	CapabilitiesRequired []string                      `json:"capabilities_required"`
	DeadlineAt           time.Time                     `json:"deadline_at"`
}

type Usage struct {
	Status       string `json:"status"`
	InputTokens  *int64 `json:"input_tokens,omitempty"`
	OutputTokens *int64 `json:"output_tokens,omitempty"`
	// Pricing/billing are separate runtime facts; absent accounting is UNKNOWN.
	CostStatus string `json:"cost_status"`
}

type EntityProposal struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type AnswerProposal struct {
	Answer     string           `json:"answer"`
	EntityRefs []EntityProposal `json:"entity_refs"`
}
type CandidateProposal struct {
	Status             string   `json:"status"`
	Predicate          string   `json:"predicate"`
	Value              string   `json:"value"`
	SourceRefIDs       []string `json:"source_ref_ids"`
	SubjectAttribution string   `json:"subject_attribution"`
}
type ToolProposal struct {
	Tool          string            `json:"tool"`
	Arguments     map[string]string `json:"arguments"`
	ReasonSummary string            `json:"reason_summary"`
}

type Result struct {
	SchemaVersion        string                        `json:"schema_version"`
	RunID                string                        `json:"run_id"`
	Agent                agentcognitive.AgentReference `json:"agent_ref"`
	Status               Status                        `json:"status"`
	Mode                 ExecutionMode                 `json:"execution_mode"`
	Text                 string                        `json:"text,omitempty"`
	Answer               *AnswerProposal               `json:"answer_proposal,omitempty"`
	Candidate            *CandidateProposal            `json:"candidate_proposal,omitempty"`
	ToolProposals        []ToolProposal                `json:"tool_proposals,omitempty"`
	Usage                Usage                         `json:"usage"`
	ProviderID           string                        `json:"provider_id,omitempty"`
	ProviderModelVersion string                        `json:"provider_model_version,omitempty"`
	ProviderRequestID    string                        `json:"provider_request_id,omitempty"`
	FinishReason         string                        `json:"finish_reason"`
	ReasonCode           string                        `json:"reason_code"`
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

func validUUID(id string) bool {
	if id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	ref, err := actorref.Parse("PERSON", id)
	return err == nil && ref.ID == id
}

func validMessage(m Message) bool {
	return (m.Role == "system" || m.Role == "user" || m.Role == "context") &&
		strings.TrimSpace(m.Content) != "" && len(m.Content) <= 4096 && !strings.ContainsRune(m.Content, 0) && utf8.ValidString(m.Content)
}

func ValidateRequest(r Request, now time.Time) error {
	if now.IsZero() || r.SchemaVersion != RequestVersion || !validUUID(r.RunID) ||
		agentcognitive.ValidateAgentReference(r.Agent) != nil ||
		(r.Agent.Principal.Type != actorref.Person && r.Agent.Principal.Type != actorref.Organization) ||
		!validUUID(r.Agent.AgentID) || !validUUID(r.Agent.Principal.ID) ||
		!identifier.MatchString(r.PromptVersion) || r.InputSchemaVersion != "air.messages.v1" ||
		!validUUID(r.ContextSnapshotRef) || !validUUID(r.DataPolicyRef) || !validUUID(r.BudgetRef) ||
		r.Budget.MaxOutputTokens < 1 || r.Budget.MaxOutputTokens > 4096 ||
		len(r.Messages) < 1 || len(r.Messages) > 16 || len(r.ToolAllowlist) > 4 ||
		!r.DeadlineAt.After(now) || r.DeadlineAt.After(now.Add(MaxDeadline)) {
		return ErrInvalid
	}
	var size int
	for i, m := range r.Messages {
		if !validMessage(m) || (m.Role == "system" && i != 0) {
			return ErrInvalid
		}
		size += len(m.Content)
	}
	if size > 16*1024 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, tool := range r.ToolAllowlist {
		if (tool != "activity.search" && tool != "activity.detail") || seen[tool] {
			return ErrInvalid
		}
		seen[tool] = true
	}
	// These are task requirements for AIR008's later verified routing, never
	// statements that this adapter or any provider has those capabilities.
	expected := map[string]bool{"text": true}
	if r.OutputMode == Structured || r.OutputMode == ToolProposals {
		expected["structured_output_validatable"] = true
	}
	if r.OutputMode == ToolProposals {
		expected["tools"] = true
	}
	if len(r.CapabilitiesRequired) != len(expected) {
		return ErrInvalid
	}
	for _, capability := range r.CapabilitiesRequired {
		if !expected[capability] {
			return ErrInvalid
		}
		delete(expected, capability)
	}
	switch r.TaskKind {
	case ActivityQuery:
		if r.OutputMode != Text && r.OutputMode != Structured && r.OutputMode != ToolProposals {
			return ErrInvalid
		}
		if r.OutputSchemaVersion != "air.answer.v1" {
			return ErrInvalid
		}
		if r.OutputMode == ToolProposals && len(r.ToolAllowlist) == 0 {
			return ErrInvalid
		}
	case MemoryCandidateExtraction:
		if r.OutputMode != Structured || r.OutputSchemaVersion != "air.candidate_proposal.v1" || len(r.ToolAllowlist) != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	// Bound the actual encoded payload as well as raw message bytes: JSON
	// escaping can otherwise expand a small control-character input sixfold.
	if data, err := json.Marshal(r); err != nil || len(data) > MaxRequestBytes {
		return ErrInvalid
	}
	return nil
}

func emptyResult(r Request, mode ExecutionMode, status Status, reason string) Result {
	return Result{SchemaVersion: ResultVersion, RunID: r.RunID, Agent: r.Agent, Status: status, Mode: mode,
		Usage: Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}, FinishReason: "none", ReasonCode: reason}
}
