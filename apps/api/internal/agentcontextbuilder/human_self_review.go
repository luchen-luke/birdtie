package agentcontextbuilder

import (
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
)

const HumanSelfReviewSchema = "human-self-review-field-evidence-v1"
const MaxHumanSelfReviewWireBytes = 64 * 1024
const HumanSelfReviewComparison = "EXPLICIT_ACTIVITY_CATEGORY_PREFERENCE_ONLY"

type HumanSelfReviewSelection struct {
	ProfileFields  []string                     `json:"profileFields"`
	MemoryIDs      []string                     `json:"memoryIds"`
	PolicyFamilies []agentpolicysettings.Family `json:"policyFamilies"`
}
type HumanSelfReviewMemory struct {
	ID         string                      `json:"id"`
	Version    int64                       `json:"version"`
	MemoryType agentmemory.MemoryType      `json:"memoryType,omitempty"`
	Summary    string                      `json:"summary"`
	CreatedAt  *time.Time                  `json:"createdAt,omitempty"`
	ValidFrom  *time.Time                  `json:"validFrom,omitempty"`
	ValidUntil time.Time                   `json:"validUntil"`
	Confidence *agentconfidence.Assessment `json:"confidence,omitempty"`
}
type HumanSelfReviewSections struct {
	Profile  string `json:"profile"`
	Memories string `json:"memories"`
	Policies string `json:"policies"`
}

// A direct human description. It cannot be used as BuiltContext, a source
// read grant, approval, Memory write, model context or a causal receipt.
type HumanSelfReviewProjection struct {
	SchemaVersion          string                       `json:"schemaVersion"`
	Owner                  actorref.PrincipalRef        `json:"owner"`
	AgentID                string                       `json:"agentId"`
	Selection              HumanSelfReviewSelection     `json:"selection"`
	Sections               HumanSelfReviewSections      `json:"sections"`
	Profile                map[string]json.RawMessage   `json:"profile"`
	Memories               []HumanSelfReviewMemory      `json:"memories"`
	Policies               []agentpolicysettings.Record `json:"policies"`
	FieldEvidenceSet       *FieldEvidenceSet            `json:"fieldEvidenceSet"`
	ObservedAt             time.Time                    `json:"observedAt"`
	ExpiresAt              time.Time                    `json:"expiresAt"`
	ComparisonScope        string                       `json:"comparisonScope"`
	Notice                 string                       `json:"notice"`
	ModelAccess            string                       `json:"modelAccess"`
	MediaAccess            string                       `json:"mediaAccess"`
	MemoryPromotionAllowed bool                         `json:"memoryPromotionAllowed"`
	GrantsAuthority        bool                         `json:"grantsAuthority"`
}

// HumanSelfReviewView only projects this already-built selected snapshot.
// The HTTP caller must retain the original sealed object and same Service for
// final RevalidateOwn AFTER encoding and BEFORE exposing any private byte.
func HumanSelfReviewView(b BuiltContext) (HumanSelfReviewProjection, error) {
	if b.Request.Mode != HumanSelfReview || b.Request.Selection != ExactSelfReview || ValidateShape(b.Request) != nil || ValidateBuilt(b.Request, b) != nil {
		return HumanSelfReviewProjection{}, ErrUnavailable
	}
	fields, e := BuildFieldEvidenceSet(b.Bundle)
	if e != nil {
		return HumanSelfReviewProjection{}, ErrUnavailable
	}
	out := HumanSelfReviewProjection{SchemaVersion: HumanSelfReviewSchema, Owner: b.Bundle.Agent.Principal, AgentID: b.Bundle.Agent.AgentID,
		Selection: HumanSelfReviewSelection{ProfileFields: append([]string{}, b.Request.ProfileFields...), MemoryIDs: append([]string{}, b.Request.MemoryIDs...), PolicyFamilies: append([]agentpolicysettings.Family{}, b.Request.PolicyFamilies...)},
		Sections:  HumanSelfReviewSections{b.Bundle.Sections.Profile, b.Bundle.Sections.Memories, b.Bundle.Sections.Policies},
		Profile:   map[string]json.RawMessage{}, Memories: []HumanSelfReviewMemory{}, Policies: append([]agentpolicysettings.Record{}, b.Bundle.Policies...),
		FieldEvidenceSet: fields, ObservedAt: b.Bundle.ObservedAt, ExpiresAt: b.Bundle.ExpiresAt,
		ComparisonScope: HumanSelfReviewComparison, Notice: "仅核对已选择来源中的明确活动类别偏好；相反声明保留双方并等待本人确认。没有列出冲突不代表所有声明都没有冲突。更新时间和读取时间不证明发生、出席、到访或当前位置。",
		ModelAccess: "UNAVAILABLE", MediaAccess: "UNAVAILABLE"}
	for k, v := range b.Bundle.Profile {
		out.Profile[k] = append(json.RawMessage(nil), v...)
	}
	for _, m := range b.Bundle.Memories {
		out.Memories = append(out.Memories, HumanSelfReviewMemory{ID: m.ID, Version: m.Version, MemoryType: m.MemoryType, Summary: m.Summary, CreatedAt: m.CreatedAt, ValidFrom: m.ValidFrom, ValidUntil: m.ValidUntil, Confidence: m.Confidence})
	}
	// Break pointer/raw-message aliases without serializing internal authority,
	// RowToken, selectors' Access or the sealed control object itself.
	raw, e := json.Marshal(out)
	if e != nil || len(raw) > MaxHumanSelfReviewWireBytes {
		return HumanSelfReviewProjection{}, ErrUnavailable
	}
	var copy HumanSelfReviewProjection
	if json.Unmarshal(raw, &copy) != nil {
		return HumanSelfReviewProjection{}, ErrUnavailable
	}
	return copy, nil
}
