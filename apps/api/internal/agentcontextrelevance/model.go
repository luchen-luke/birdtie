// Package agentcontextrelevance selects from the exact current approved
// context. A text match is a projection, never a permission or inferred fact.
package agentcontextrelevance

import (
	"encoding/json"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
)

const Method = "bounded-lexical-zh-en-v1"

type Match struct {
	Kind     string   `json:"kind"`
	SourceID string   `json:"sourceId"`
	Field    string   `json:"field,omitempty"`
	Terms    []string `json:"terms"`
	Score    int      `json:"score"` // Count of literal matches, not a probability.
	Reason   string   `json:"reason"`
}

type View struct {
	Context  acb.Bundle `json:"context"`
	Matches  []Match    `json:"matches"`
	Excluded int        `json:"excluded"`
	Method   string     `json:"method"`
}

// Result retains the original complete sealed approval privately. Its DTO
// projection cannot be used to manufacture or narrow the underlying approval.
type Result struct {
	service  *Service
	original acb.BuiltContext
	view     View
}

func (Result) MarshalJSON() ([]byte, error)  { return nil, acb.ErrServerOnly }
func (r *Result) UnmarshalJSON([]byte) error { *r = Result{}; return acb.ErrServerOnly }
func cloneBundle(b acb.Bundle) acb.Bundle {
	raw, _ := json.Marshal(b)
	var out acb.Bundle
	_ = json.Unmarshal(raw, &out)
	return out
}
func (r Result) ProjectionBundle() acb.Bundle { return cloneBundle(r.view.Context) }

// ExcludedCounts contains only counts from the original exact approved view.
// It never identifies or carries text from a filtered source.
func (r Result) ExcludedCounts() map[string]int {
	b, v := r.original.Bundle, r.view.Context
	return map[string]int{"profile": len(b.Profile) - len(v.Profile), "memories": len(b.Memories) - len(v.Memories), "places": len(b.Places) - len(v.Places), "activities": len(b.Activities) - len(v.Activities), "relationships": len(b.Relationships) - len(v.Relationships), "policies": len(b.Policies) - len(v.Policies)}
}
func (r Result) View() View {
	raw, _ := json.Marshal(r.view)
	var out View
	_ = json.Unmarshal(raw, &out)
	return out
}
