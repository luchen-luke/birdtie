// Package agentmemorycandidate describes manual hypotheses awaiting owner review.
// A candidate, score or source association never grants machine analysis.
package agentmemorycandidate

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"io"
	"sort"
	"time"
)

const Schema = "agent-memory-candidate-v1"
const MaxSources = 20
const MaxLease = 24 * time.Hour
const PreviewLease = 5 * time.Minute

var ErrServerOnly = errors.New("候选批准仅供服务端使用")

type Status string

const (
	Candidate  Status = "CANDIDATE"
	Active     Status = "ACTIVE"
	Rejected   Status = "REJECTED"
	Superseded Status = "SUPERSEDED"
	Expired    Status = "EXPIRED"
)

type Selector struct {
	Type agentevent.SourceType `json:"type"`
	ID   string                `json:"id"`
}
type Source struct {
	Selector    Selector                 `json:"selector"`
	Version     agentevent.SourceVersion `json:"version"`
	EventTime   time.Time                `json:"eventTime"`
	Fingerprint string                   `json:"fingerprint"`
	Anchors     []string                 `json:"anchors"`
}
type Draft struct {
	Predicate  string                     `json:"predicate"`
	Category   string                     `json:"category"`
	Assessment agentconfidence.Assessment `json:"assessment"`
	Sources    []Selector                 `json:"sources"`
	ValidUntil time.Time                  `json:"validUntil"`
}

func (d *Draft) UnmarshalJSON(raw []byte) error {
	*d = Draft{}
	type wire Draft
	var v wire
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if len(raw) > 8192 {
		return agentmemory.ErrInvalid
	}
	if dec.Decode(&v) != nil {
		return agentmemory.ErrInvalid
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return agentmemory.ErrInvalid
	}
	*d = Draft(v)
	return nil
}

type Record struct {
	SchemaVersion string                      `json:"schemaVersion"`
	ID            string                      `json:"id"`
	AgentID       string                      `json:"agentId"`
	Owner         actorref.PrincipalRef       `json:"owner"`
	Version       int64                       `json:"version"`
	Status        Status                      `json:"status"`
	Predicate     string                      `json:"predicate,omitempty"`
	Category      string                      `json:"category,omitempty"`
	Assessment    *agentconfidence.Assessment `json:"assessment,omitempty"`
	Sources       []Source                    `json:"sources"`
	ValidUntil    time.Time                   `json:"validUntil"`
	CreatedAt     time.Time                   `json:"createdAt"`
	UpdatedAt     time.Time                   `json:"updatedAt"`
	MemoryID      *string                     `json:"memoryId,omitempty"`
	MemoryVersion *int64                      `json:"memoryVersion,omitempty"`
	ModelAccess   string                      `json:"modelAccess"`
}
type Review struct {
	PreviousMemory        *agentmemory.Record `json:"previousMemory,omitempty"`
	Candidate             Record              `json:"candidate"`
	Statement             string              `json:"statement"`
	TargetMemoryID        string              `json:"targetMemoryId"`
	ExpectedMemoryVersion int64               `json:"expectedMemoryVersion"`
	MemoryValidUntil      time.Time           `json:"memoryValidUntil"`
	Clusters              int                 `json:"clusters"`
	PlanDigest            string              `json:"planDigest"`
	ExpiresAt             time.Time           `json:"expiresAt"`
	Purpose               string              `json:"purpose"`
}

func NormalizeSelectors(in []Selector) ([]Selector, error) {
	if len(in) == 0 || len(in) > MaxSources {
		return nil, agentmemory.ErrInvalid
	}
	out := append([]Selector(nil), in...)
	seen := map[Selector]bool{}
	for _, v := range out {
		id, e := agentmemory.NormalizeMemoryID(v.ID)
		if e != nil || id != v.ID || seen[v] || (v.Type != agentevent.MomentSource && v.Type != agentevent.ParticipationSource && v.Type != agentevent.SavedPlaceSource) {
			return nil, agentmemory.ErrInvalid
		}
		seen[v] = true
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}
func NormalizeDraft(d Draft, now time.Time) (Draft, error) {
	if agentruntime.ValidateOrdinaryCandidateAttribute(d.Predicate, d.Category) != nil || now.IsZero() || now.Year() < 1 || now.Year() > 9999 || d.ValidUntil.Year() < 1 || d.ValidUntil.Year() > 9999 || !d.ValidUntil.After(now) || d.ValidUntil.Sub(now) > MaxLease {
		return Draft{}, agentmemory.ErrInvalid
	}
	var e error
	d.Assessment, e = agentconfidence.NormalizeAssessment(d.Assessment)
	if e != nil || (d.Assessment.Semantics != agentconfidence.UncalibratedScore && d.Assessment.Semantics != agentconfidence.Ordinal) {
		return Draft{}, agentmemory.ErrInvalid
	}
	d.Sources, e = NormalizeSelectors(d.Sources)
	return d, e
}

// Sources are deduplicated by underlying native anchors, never media or score.
func ClusterCount(s []Source) (int, error) {
	entries := make([]agentreinforcement.Entry, len(s))
	seen := map[Selector]bool{}
	for i, v := range s {
		if seen[v.Selector] {
			return 0, agentmemory.ErrInvalid
		}
		seen[v.Selector] = true
		// Use a deterministic synthetic Entry address solely to reuse clustering.
		// It is never an Evidence ID, source revision or authorization token.
		digest, _ := agentreinforcement.Digest(v.Selector)
		id := digest[:8] + "-" + digest[8:12] + "-" + digest[12:16] + "-" + digest[16:20] + "-" + digest[20:32]
		entries[i] = agentreinforcement.Entry{EvidenceID: id, EvidenceVersion: 1, Fingerprint: v.Fingerprint, Anchors: v.Anchors}
	}
	return agentreinforcement.ClusterCount(entries)
}
func Statement(category string) string {
	return map[string]string{"badminton": "我偏好羽毛球活动", "basketball": "我偏好篮球活动", "football": "我偏好足球活动", "sports": "我偏好运动活动", "culture": "我偏好文化活动", "hiking": "我偏好徒步活动"}[category]
}
func Clone(r Record) Record {
	raw, _ := json.Marshal(r)
	var c Record
	_ = json.Unmarshal(raw, &c)
	return c
}
