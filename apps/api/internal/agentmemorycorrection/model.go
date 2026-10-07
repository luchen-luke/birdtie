// Package agentmemorycorrection is a human self-management contract. A negative
// category is an explicit choice, never an interpretation of private prose.
package agentmemorycorrection

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"math"
	"time"
)

const Schema = "agent-memory-correction-v1"
const PreviewTTL = 5 * time.Minute
const MaxBodyBytes = 16384
const Explanation = "这是你本人的具体纠正。明确选择不偏好某类活动后，会停止以任何来源再次提出该类偏好；不代表概率，也不删除原动态。本人独立声明不因关联来源到期而删除。删除纠正记忆不会恢复旧推断或旧批准。"

type Input struct {
	ID              string                `json:"id"`
	TargetKind      string                `json:"targetKind"`
	TargetID        string                `json:"targetId"`
	ExpectedVersion int64                 `json:"expectedVersion"`
	Action          string                `json:"action"`
	Category        string                `json:"category,omitempty"`
	Replacement     *agentmemory.PutInput `json:"replacement,omitempty"`
}
type ConfirmInput struct {
	PlanDigest string `json:"planDigest"`
}
type Target struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int64  `json:"version"`
}
type Preview struct {
	SchemaVersion       string                `json:"schemaVersion"`
	ID                  string                `json:"id"`
	Owner               actorref.PrincipalRef `json:"owner"`
	AgentID             string                `json:"agentId"`
	Input               Input                 `json:"input"`
	Memories            []agentmemory.Record  `json:"memories"`
	Affected            []Target              `json:"affected"`
	PlanDigest          string                `json:"planDigest"`
	ObservedAt          time.Time             `json:"observedAt"`
	ExpiresAt           time.Time             `json:"expiresAt"`
	Explanation         string                `json:"explanation"`
	NewMemoryValidUntil *time.Time            `json:"newMemoryValidUntil,omitempty"`
	ModelAccess         bool                  `json:"modelAccess"`
}
type Receipt struct {
	SchemaVersion        string                `json:"schemaVersion"`
	ID                   string                `json:"id"`
	Owner                actorref.PrincipalRef `json:"owner"`
	AgentID              string                `json:"agentId"`
	Target               Target                `json:"target"`
	Action               string                `json:"action"`
	PlanDigest           string                `json:"planDigest"`
	State                string                `json:"state"`
	ResultMemoryID       *string               `json:"resultMemoryId,omitempty"`
	ResultMemoryVersion  *int64                `json:"resultMemoryVersion,omitempty"`
	CommittedAt          *time.Time            `json:"committedAt,omitempty"`
	CurrentResultMatches bool                  `json:"currentResultMatches"`
	SuppressionActive    bool                  `json:"suppressionActive"`
	ObservedAt           time.Time             `json:"observedAt"`
	ExpiresAt            time.Time             `json:"expiresAt"`
	ModelAccess          bool                  `json:"modelAccess"`
}
type HumanStore interface {
	PreviewOwnMemoryCorrection(context.Context, agentprofile.PrivateAccess, Input) (Preview, error)
	ConfirmOwnMemoryCorrection(context.Context, agentprofile.PrivateAccess, string, ConfirmInput) (Receipt, error)
	ReadOwnMemoryCorrection(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
}

func ValidID(s string) bool { n, e := agentmemory.NormalizeMemoryID(s); return e == nil && n == s }
func ValidDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func ValidTime(t time.Time) bool  { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func ValidCategory(s string) bool { return agentmemorycandidate.Statement(s) != "" }
func NormalizeInput(v Input) (Input, error) {
	if !ValidID(v.ID) || !ValidID(v.TargetID) || v.ExpectedVersion <= 0 || v.ExpectedVersion == math.MaxInt64 {
		return Input{}, agentmemory.ErrInvalid
	}
	if v.TargetKind != "MEMORY" && v.TargetKind != "CANDIDATE" {
		return Input{}, agentmemory.ErrInvalid
	}
	switch v.Action {
	case "EDIT":
		if v.TargetKind != "MEMORY" || v.Replacement == nil || v.Replacement.ExpectedVersion != v.ExpectedVersion || v.Category != "" {
			return Input{}, agentmemory.ErrInvalid
		}
		raw, e := json.Marshal(v.Replacement)
		if e != nil {
			return Input{}, agentmemory.ErrInvalid
		}
		n, e := agentmemory.DecodePutInput(raw)
		if e != nil {
			return Input{}, e
		}
		n.ValidUntil = n.ValidUntil.UTC().Truncate(time.Microsecond)
		v.Replacement = &n
	case "DELETE":
		if v.TargetKind != "MEMORY" || v.Replacement != nil || v.Category != "" {
			return Input{}, agentmemory.ErrInvalid
		}
	case "REJECT":
		if v.Replacement != nil || v.Category != "" {
			return Input{}, agentmemory.ErrInvalid
		}
	case "NEGATE":
		if !ValidCategory(v.Category) || v.Replacement != nil {
			return Input{}, agentmemory.ErrInvalid
		}
	default:
		return Input{}, agentmemory.ErrInvalid
	}
	return v, nil
}
func NegativeStatement(c string) string {
	if !ValidCategory(c) {
		return ""
	}
	return "我不" + agentmemorycandidate.Statement(c)[3:]
}
func ValidatePreview(p Preview) error {
	if p.SchemaVersion != Schema || p.ID != p.Input.ID || p.Owner.Type != actorref.Person || !ValidID(p.Owner.ID) || !ValidID(p.AgentID) || !ValidDigest(p.PlanDigest) || !ValidTime(p.ObservedAt) || !ValidTime(p.ExpiresAt) || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.Sub(p.ObservedAt) > PreviewTTL || p.Explanation != Explanation || p.ModelAccess || p.Memories == nil || p.Affected == nil || len(p.Affected) > 100 {
		return agentmemory.ErrInvalid
	}
	if _, e := NormalizeInput(p.Input); e != nil {
		return e
	}
	if len(p.Memories) > 100 {
		return agentmemory.ErrInvalid
	}
	if p.Input.Action == "NEGATE" {
		if p.NewMemoryValidUntil == nil || !ValidTime(*p.NewMemoryValidUntil) || !p.NewMemoryValidUntil.Equal(p.ObservedAt.Add(agentmemory.MaxValidity)) {
			return agentmemory.ErrInvalid
		}
	} else if p.NewMemoryValidUntil != nil {
		return agentmemory.ErrInvalid
	}
	seen := map[string]bool{}
	for _, t := range p.Affected {
		if !ValidID(t.ID) || t.Version <= 0 || t.Version == math.MaxInt64 || (t.Kind != "MEMORY" && t.Kind != "CANDIDATE") || seen[t.Kind+t.ID] {
			return agentmemory.ErrInvalid
		}
		seen[t.Kind+t.ID] = true
	}
	targetFound := false
	for _, t := range p.Affected {
		if t.Kind == p.Input.TargetKind && t.ID == p.Input.TargetID && t.Version == p.Input.ExpectedVersion {
			targetFound = true
		}
	}
	if !targetFound {
		return agentmemory.ErrInvalid
	}
	memSeen := map[string]bool{}
	for _, m := range p.Memories {
		if agentmemory.ValidateRecord(m) != nil || m.OwnerID != p.Owner.ID || m.AgentID != p.AgentID || !seen["MEMORY"+m.ID] || memSeen[m.ID] {
			return agentmemory.ErrInvalid
		}
		matched := false
		for _, t := range p.Affected {
			if t.Kind == "MEMORY" && t.ID == m.ID && t.Version == m.Version {
				matched = true
			}
		}
		if !matched {
			return agentmemory.ErrInvalid
		}
		memSeen[m.ID] = true
	}
	for _, t := range p.Affected {
		if t.Kind == "MEMORY" && !memSeen[t.ID] {
			return agentmemory.ErrInvalid
		}
	}
	return nil
}
func ValidateReceipt(r Receipt) error {
	if r.SchemaVersion != Schema || !ValidID(r.ID) || r.Owner.Type != actorref.Person || !ValidID(r.Owner.ID) || !ValidID(r.AgentID) || !ValidID(r.Target.ID) || r.Target.Version <= 0 || r.Target.Version == math.MaxInt64 || (r.Target.Kind != "MEMORY" && r.Target.Kind != "CANDIDATE") || !ValidDigest(r.PlanDigest) || !ValidTime(r.ObservedAt) || !ValidTime(r.ExpiresAt) || r.ModelAccess {
		return agentmemory.ErrInvalid
	}
	if r.Action != "EDIT" && r.Action != "DELETE" && r.Action != "REJECT" && r.Action != "NEGATE" {
		return agentmemory.ErrInvalid
	}
	if r.Action == "EDIT" || r.Action == "DELETE" {
		if r.Target.Kind != "MEMORY" {
			return agentmemory.ErrInvalid
		}
	}
	if r.SuppressionActive && r.Action != "NEGATE" {
		return agentmemory.ErrInvalid
	}
	switch r.State {
	case "COMMITTED":
		if r.CommittedAt == nil || !ValidTime(*r.CommittedAt) || r.CommittedAt.After(r.ObservedAt) || !r.CommittedAt.Before(r.ExpiresAt) {
			return agentmemory.ErrInvalid
		}
		if (r.ResultMemoryID == nil) != (r.ResultMemoryVersion == nil) || r.ResultMemoryID != nil && (!ValidID(*r.ResultMemoryID) || *r.ResultMemoryVersion <= 0) {
			return agentmemory.ErrInvalid
		}
	case "PENDING", "EXPIRED":
		if r.CommittedAt != nil || r.ResultMemoryID != nil || r.ResultMemoryVersion != nil || r.CurrentResultMatches || r.SuppressionActive || (r.State == "PENDING") != r.ExpiresAt.After(r.ObservedAt) {
			return agentmemory.ErrInvalid
		}
	default:
		return agentmemory.ErrInvalid
	}
	if r.State == "COMMITTED" {
		if r.Target.Kind == "CANDIDATE" && r.Action == "REJECT" {
			if r.ResultMemoryID != nil || r.CurrentResultMatches {
				return agentmemory.ErrInvalid
			}
		} else {
			if r.ResultMemoryID == nil {
				return agentmemory.ErrInvalid
			}
			if r.Action != "NEGATE" && (*r.ResultMemoryID != r.Target.ID || *r.ResultMemoryVersion != r.Target.Version+1) {
				return agentmemory.ErrInvalid
			}
		}
	}
	return nil
}
