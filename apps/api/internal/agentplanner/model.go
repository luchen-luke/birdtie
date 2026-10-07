// Package agentplanner describes bounded read-only proposals. Its wire data is
// neither a permission nor an approval, executable tool call, or effect receipt.
package agentplanner

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const Schema = "air.readonly_plan.v1"
const ActionSchema = "air.action_proposal.v1"
const MaxSteps = 3
const MaxModelCalls = 2
const MaxElapsed = 30 * time.Second
const (
	Proposed        = "PROPOSED"
	Clarification   = "CLARIFICATION"
	Unavailable     = "UNAVAILABLE"
	ActivitySearch  = "activity.search"
	ActivityDetail  = "activity.detail"
	CandidateReview = "memory_candidate.review"
)

var (
	ErrInvalid     = errors.New("只读规划内容无效")
	ErrUnavailable = errors.New("只读规划当前不可用")
	ErrDenied      = errors.New("当前主体或来源不允许此规划")
	ErrChanged     = errors.New("规划来源已经变化")
	ErrLimit       = errors.New("只读规划达到步骤、模型调用或时间上限")
	ErrServerOnly  = errors.New("规划当前来源只供服务端使用")
)
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidID(id string) bool {
	return idPattern.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func text(s string, max int) bool {
	return s != "" && strings.TrimSpace(s) == s && utf8.ValidString(s) && len(s) <= max && !strings.ContainsRune(s, 0)
}

// Only the active union arm is allowed. There is no code, URL, actor selector,
// approval flag, permission override or arbitrary map of tool arguments.
type Arguments struct {
	Query           string `json:"query,omitempty"`
	CityID          string `json:"city_id,omitempty"`
	ActivityID      string `json:"activity_id,omitempty"`
	CandidateID     string `json:"candidate_id,omitempty"`
	ExpectedVersion int64  `json:"expected_version,omitempty"`
}
type ActionProposal struct {
	SchemaVersion      string    `json:"schema_version"`
	ActionID           string    `json:"action_id"`
	LogicalOperationID string    `json:"logical_operation_id"`
	Tool               string    `json:"tool"`
	Arguments          Arguments `json:"arguments"`
	ResourceVersion    string    `json:"resource_version"`
	ReasonSummary      string    `json:"reason_summary"`
}

func (a ActionProposal) Valid() bool {
	if a.SchemaVersion != ActionSchema || !ValidID(a.ActionID) || !ValidID(a.LogicalOperationID) || !text(a.ResourceVersion, 240) || !text(a.ReasonSummary, 500) {
		return false
	}
	v := a.Arguments
	switch a.Tool {
	case ActivitySearch:
		return text(v.Query, 240) && text(v.CityID, 160) && !strings.Contains(v.CityID, "://") && v.ActivityID == "" && v.CandidateID == "" && v.ExpectedVersion == 0
	case ActivityDetail:
		return ValidID(v.ActivityID) && v.Query == "" && v.CityID == "" && v.CandidateID == "" && v.ExpectedVersion == 0
	case CandidateReview:
		return ValidID(v.CandidateID) && v.ExpectedVersion > 0 && v.Query == "" && v.CityID == "" && v.ActivityID == ""
	}
	return false
}

// StableID addresses one proposal in the existing logical operation. It does
// not register an effect or infer an operation from a technical ModelRun ID.
func StableID(operation string, ordinal int, tool string, args Arguments, version string) string {
	raw, _ := json.Marshal(struct {
		Operation string
		Ordinal   int
		Tool      string
		Arguments Arguments
		Version   string
	}{operation, ordinal, tool, args, version})
	h := sha256.Sum256(append([]byte("birdtie.readonly-proposal.v1\x00"), raw...))
	h[6] = (h[6] & 15) | 128
	h[8] = (h[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
func Proposal(operation string, ordinal int, tool string, args Arguments, version, reason string) ActionProposal {
	return ActionProposal{ActionSchema, StableID(operation, ordinal, tool, args, version), operation, tool, args, version, reason}
}

type View struct {
	SchemaVersion      string           `json:"schema_version"`
	Status             string           `json:"status"`
	TaskID             string           `json:"task_id,omitempty"`
	LogicalOperationID string           `json:"logical_operation_id,omitempty"`
	Actions            []ActionProposal `json:"actions"`
	Clarifications     []string         `json:"clarifications"`
	ReasonCode         string           `json:"reason_code"`
	ObservedAt         time.Time        `json:"observed_at"`
	ValidUntil         time.Time        `json:"valid_until"`
	ModelAccess        string           `json:"model_access"`
}

func NewView(status, task, operation, reason string, observed, until time.Time) View {
	return View{Schema, status, task, operation, []ActionProposal{}, []string{}, reason, observed.UTC(), until.UTC(), "UNAVAILABLE"}
}
func Clone(v View) View {
	v.Actions = append([]ActionProposal{}, v.Actions...)
	v.Clarifications = append([]string{}, v.Clarifications...)
	return v
}
func (v View) Valid(now time.Time) bool {
	if v.SchemaVersion != Schema || v.ModelAccess != "UNAVAILABLE" || v.Actions == nil || v.Clarifications == nil || len(v.Actions) > MaxSteps || len(v.Clarifications) > 2 || !text(v.ReasonCode, 100) || v.ObservedAt.IsZero() || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > MaxElapsed || !now.Before(v.ValidUntil) || v.ObservedAt.After(now) || (v.TaskID != "" && !ValidID(v.TaskID)) {
		return false
	}
	if v.Status != Unavailable && !ValidID(v.LogicalOperationID) {
		return false
	}
	switch v.Status {
	case Proposed:
		if len(v.Actions) == 0 || len(v.Clarifications) != 0 {
			return false
		}
	case Clarification:
		if len(v.Actions) != 0 || len(v.Clarifications) == 0 {
			return false
		}
	case Unavailable:
		if len(v.Actions) != 0 || len(v.Clarifications) != 0 {
			return false
		}
	default:
		return false
	}
	seen := map[string]bool{}
	for _, a := range v.Actions {
		if !a.Valid() || a.LogicalOperationID != v.LogicalOperationID || seen[a.ActionID] {
			return false
		}
		seen[a.ActionID] = true
	}
	for _, q := range v.Clarifications {
		if !text(q, 240) {
			return false
		}
	}
	return true
}

// Native callers have already checked PostgreSQL's clock in the original
// identity/source transaction. Presentation timestamps must not be compared
// to another machine's wall clock. Like the original ClockBound, this bound
// uses the native lifetime relative to a local monotonic call start; it is a
// restriction only and never establishes identity, consent or source currentness.
func (v View) ValidElapsed(started, now time.Time) bool {
	return v.Valid(v.ObservedAt) && !started.IsZero() && !now.IsZero() && !now.Before(started) && now.Sub(started) < v.ValidUntil.Sub(v.ObservedAt)
}

// Decode rejects aliases, duplicate keys, unknown fields and nested authority.
// It validates presentation shape only; native services still resolve sources.
func Decode(raw []byte) (View, error) {
	var v View
	if len(raw) == 0 || len(raw) > 32*1024 || !utf8.Valid(raw) {
		return v, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := closedTokens(d, ""); err != nil {
		return v, err
	}
	if _, e := d.Token(); e != io.EOF {
		return v, ErrInvalid
	}
	if json.Unmarshal(raw, &v) != nil || !v.Valid(v.ObservedAt) {
		return View{}, ErrInvalid
	}
	return v, nil
}
func closedTokens(d *json.Decoder, path string) error {
	tok, e := d.Token()
	if e != nil {
		return ErrInvalid
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '[' {
		for d.More() {
			if closedTokens(d, path+".*") != nil {
				return ErrInvalid
			}
		}
		tok, e = d.Token()
		if e != nil || tok != json.Delim(']') {
			return ErrInvalid
		}
		return nil
	}
	if delim != '{' {
		return ErrInvalid
	}
	allowed := map[string]bool{}
	var keys []string
	switch path {
	case "":
		keys = []string{"schema_version", "status", "task_id", "logical_operation_id", "actions", "clarifications", "reason_code", "observed_at", "valid_until", "model_access"}
	case "actions.*":
		keys = []string{"schema_version", "action_id", "logical_operation_id", "tool", "arguments", "resource_version", "reason_summary"}
	case "actions.*.arguments":
		keys = []string{"query", "city_id", "activity_id", "candidate_id", "expected_version"}
	default:
		return ErrInvalid
	}
	for _, k := range keys {
		allowed[k] = true
	}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		k, ok := tok.(string)
		if e != nil || !ok || !allowed[k] || seen[k] {
			return ErrInvalid
		}
		seen[k] = true
		p := k
		if path != "" {
			p = path + "." + k
		}
		if closedTokens(d, p) != nil {
			return ErrInvalid
		}
	}
	tok, e = d.Token()
	if e != nil || tok != json.Delim('}') {
		return ErrInvalid
	}
	return nil
}
