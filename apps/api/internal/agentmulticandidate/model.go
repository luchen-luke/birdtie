// Package agentcandidateretention defines human approval of a bounded candidate
// hypothesis, independently of source analysis, model egress and Memory writes.
package agentmulticandidate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"reflect"
	"sort"
	"time"
)

const Schema = "agent-multi-candidate-v1"
const Handler = "mom-candidate-multi-v1"
const Purpose = "STAGE_MEMORY_CANDIDATE_MULTI"
const PreviewTTL = 90 * time.Second
const MaxRequestedRetention = 24 * time.Hour

var ErrBusy = errors.New("当前来源正在处理，请核实原结果")
var ErrInvalid = errors.New("候选保留输入无效")
var ErrDenied = errors.New("当前身份或来源不允许保留候选")
var ErrConflict = errors.New("候选保留的具体版本已变化")
var ErrExpired = errors.New("候选保留预览或许可已到期")
var ErrUnavailable = errors.New("当前候选保留服务不可用")
var ErrServerOnly = errors.New("候选保留解析仅供服务端使用")

type Selection struct {
	AnalysisGrantIDs []string  `json:"analysisGrantIds"`
	RetainUntil      time.Time `json:"retainUntil"`
}

func Normalize(s Selection) (Selection, error) {
	if len(s.AnalysisGrantIDs) < 2 || len(s.AnalysisGrantIDs) > 5 || !finite(s.RetainUntil) {
		return Selection{}, ErrInvalid
	}
	s.AnalysisGrantIDs = append([]string(nil), s.AnalysisGrantIDs...)
	sort.Strings(s.AnalysisGrantIDs)
	for i, id := range s.AnalysisGrantIDs {
		if !aep.ValidID(id) || (i > 0 && id == s.AnalysisGrantIDs[i-1]) {
			return Selection{}, ErrInvalid
		}
	}
	s.RetainUntil = s.RetainUntil.UTC()
	return s, nil
}
func finite(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Equal(t.Truncate(time.Microsecond))
}

type SourceSelection struct {
	AnalysisGrantID   string                      `json:"analysisGrantId"`
	AnalysisPreviewID string                      `json:"analysisPreviewId"`
	Source            agentmemorycandidate.Source `json:"source"`
	SelectedFields    []string                    `json:"selectedFields"`
}
type Review struct {
	Proposal           agentlocalcandidate.Proposal  `json:"proposal"`
	Sources            []agentmemorycandidate.Source `json:"sources"`
	SourceSelections   []SourceSelection             `json:"sourceSelections"`
	AnchorEventID      string                        `json:"anchorEventId"`
	LogicalOperationID string                        `json:"logicalOperationId"`
	AnchorSource       agentmemorycandidate.Source   `json:"anchorSource"`
	TaskID             string                        `json:"taskId"`
	RetainUntil        time.Time                     `json:"retainUntil"`
	Clusters           int                           `json:"clusters"`
}
type Preview struct {
	SchemaVersion           string                `json:"schemaVersion"`
	State                   string                `json:"state"`
	ID                      string                `json:"id"`
	Purpose                 string                `json:"purpose"`
	Owner                   actorref.PrincipalRef `json:"owner"`
	AgentID                 string                `json:"agentId"`
	Selection               Selection             `json:"selection"`
	Review                  *Review               `json:"review,omitempty"`
	ObservedAt              time.Time             `json:"observedAt"`
	ExpiresAt               time.Time             `json:"expiresAt"`
	ConsumedGrantID         string                `json:"consumedGrantId,omitempty"`
	Explanation             string                `json:"explanation"`
	ModelAccess             bool                  `json:"modelAccess"`
	MemoryPromotionAllowed  bool                  `json:"memoryPromotionAllowed"`
	CandidateWriteAvailable bool                  `json:"candidateWriteAvailable"`
}
type Grant struct {
	SchemaVersion           string                `json:"schemaVersion"`
	ID                      string                `json:"id"`
	PreviewID               string                `json:"previewId"`
	Purpose                 string                `json:"purpose"`
	Owner                   actorref.PrincipalRef `json:"owner"`
	AgentID                 string                `json:"agentId"`
	Selection               Selection             `json:"selection"`
	Revision                int64                 `json:"revision"`
	CreatedAt               time.Time             `json:"createdAt"`
	ExpiresAt               time.Time             `json:"expiresAt"`
	RevokedAt               *time.Time            `json:"revokedAt,omitempty"`
	ObservedAt              time.Time             `json:"observedAt"`
	ModelAccess             bool                  `json:"modelAccess"`
	MemoryPromotionAllowed  bool                  `json:"memoryPromotionAllowed"`
	CandidateWriteAvailable bool                  `json:"candidateWriteAvailable"`
}

// Resolution never accepts authority or a proposed category from JSON. The
// native resolver must obtain this content under both original current grants.
type Resolution struct {
	Grant          Grant
	Review         Review
	Authority      string
	AnalysisSource string
	AnalysisTask   string
}

func (Resolution) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (r *Resolution) UnmarshalJSON([]byte) error { *r = Resolution{}; return ErrServerOnly }

type RetentionStore interface {
	PreviewOwnMultiCandidate(context.Context, agentprofile.PrivateAccess, Selection) (Preview, error)
	ReadOwnMultiCandidatePreview(context.Context, agentprofile.PrivateAccess, string) (Preview, error)
	ApproveOwnMultiCandidate(context.Context, agentprofile.PrivateAccess, string) (Grant, error)
	ReadOwnMultiCandidateGrant(context.Context, agentprofile.PrivateAccess, string) (Grant, error)
	RevokeOwnMultiCandidate(context.Context, agentprofile.PrivateAccess, string, int64) (Grant, error)
}

func ValidatePreview(p Preview) error {
	s, e := Normalize(p.Selection)
	if e != nil || p.SchemaVersion != Schema || p.Purpose != Purpose || !aep.ValidID(p.ID) || p.Owner.Type != actorref.Person || !aep.ValidID(p.Owner.ID) || !aep.ValidID(p.AgentID) || !finite(p.ObservedAt) || !finite(p.ExpiresAt) || p.ExpiresAt.After(s.RetainUntil) || p.Explanation == "" || p.ModelAccess || p.MemoryPromotionAllowed || p.CandidateWriteAvailable {
		return ErrUnavailable
	}
	if p.State == "RECEIPT_ONLY" {
		if p.Review != nil || !aep.ValidID(p.ConsumedGrantID) {
			return ErrUnavailable
		}
		return nil
	}
	if p.State != "CURRENT_REVIEW" || p.ConsumedGrantID != "" || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.Sub(p.ObservedAt) > PreviewTTL || p.Review == nil {
		return ErrUnavailable
	}
	r := p.Review
	if ValidateReview(*r, p.ObservedAt) != nil || !r.RetainUntil.Equal(p.ExpiresAt) || len(r.SourceSelections) != len(s.AnalysisGrantIDs) {
		return ErrUnavailable
	}
	ids := make([]string, 0, len(r.SourceSelections))
	for _, src := range r.SourceSelections {
		ids = append(ids, src.AnalysisGrantID)
	}
	sort.Strings(ids)
	for i, id := range ids {
		if id != s.AnalysisGrantIDs[i] {
			return ErrUnavailable
		}
	}
	return nil
}
func ValidateReview(r Review, at time.Time) error {
	if len(r.Sources) < 2 || len(r.Sources) > 5 || len(r.SourceSelections) != len(r.Sources) || r.Clusters < 2 || r.Clusters > len(r.Sources) || !aep.ValidID(r.TaskID) || !aep.ValidID(r.AnchorEventID) || !aep.ValidID(r.LogicalOperationID) || !finite(r.RetainUntil) || !r.RetainUntil.After(at) || !agentlocalcandidate.ValidVersionCategory(r.Proposal.AlgorithmVersion, r.Proposal.Category) || r.Proposal.Predicate != "ACTIVITY_CATEGORY" || r.Proposal.Explanation == "" {
		return ErrUnavailable
	}
	selectors := make([]agentmemorycandidate.Selector, 0, len(r.Sources))
	seen := map[string]bool{}
	grants := map[string]bool{}
	anchor := false
	for i, src := range r.Sources {
		ss := r.SourceSelections[i]
		if src.Selector.Type != "MOMENT" || !aep.ValidID(src.Selector.ID) || seen[src.Selector.ID] || (i > 0 && r.Sources[i-1].Selector.ID >= src.Selector.ID) || src.Version.Kind != "REVISION" || src.Version.Revision < 1 || src.Version.Token != "" || !finite(src.EventTime) || src.EventTime.After(at) || !hexDigest(src.Fingerprint) || len(src.Anchors) != 1 || src.Anchors[0] != "MOMENT:"+src.Selector.ID || !reflect.DeepEqual(ss.Source, src) || !aep.ValidID(ss.AnalysisGrantID) || grants[ss.AnalysisGrantID] || !aep.ValidID(ss.AnalysisPreviewID) || len(ss.SelectedFields) < 1 || len(ss.SelectedFields) > 2 {
			return ErrUnavailable
		}
		seen[src.Selector.ID] = true
		grants[ss.AnalysisGrantID] = true
		selectors = append(selectors, src.Selector)
		fields := map[string]bool{}
		for _, field := range ss.SelectedFields {
			if (field != "title" && field != "body") || fields[field] {
				return ErrUnavailable
			}
			fields[field] = true
		}
		if reflect.DeepEqual(src, r.AnchorSource) {
			anchor = true
		}
	}
	if !anchor || !reflect.DeepEqual(r.AnchorSource, r.Sources[0]) {
		return ErrUnavailable
	}
	d, e := agentmemorycandidate.NormalizeDraft(agentmemorycandidate.Draft{Predicate: r.Proposal.Predicate, Category: r.Proposal.Category, Assessment: r.Proposal.Assessment, Sources: selectors, ValidUntil: r.RetainUntil}, at)
	if e != nil || d.Assessment.Semantics != "ORDINAL" || d.Assessment.Level != "LOW" || d.Assessment.Value != nil {
		return ErrUnavailable
	}
	return nil
}
func ValidateGrant(g Grant) error {
	s, e := Normalize(g.Selection)
	if e != nil || g.SchemaVersion != Schema || g.Purpose != Purpose || !aep.ValidID(g.ID) || !aep.ValidID(g.PreviewID) || g.Owner.Type != actorref.Person || !aep.ValidID(g.Owner.ID) || !aep.ValidID(g.AgentID) || g.Revision < 1 || !finite(g.CreatedAt) || !finite(g.ExpiresAt) || !finite(g.ObservedAt) || g.ObservedAt.Before(g.CreatedAt) || !g.ExpiresAt.After(g.CreatedAt) || g.ExpiresAt.After(s.RetainUntil) || g.ExpiresAt.Sub(g.CreatedAt) > PreviewTTL || g.ModelAccess || g.MemoryPromotionAllowed || g.CandidateWriteAvailable {
		return ErrUnavailable
	}
	if g.RevokedAt != nil && (!finite(*g.RevokedAt) || g.RevokedAt.Before(g.CreatedAt) || g.RevokedAt.After(g.ObservedAt)) {
		return ErrUnavailable
	}
	return nil
}
func DigestReview(r Review) (string, error) {
	raw, e := json.Marshal(r)
	if e != nil {
		return "", ErrInvalid
	}
	return digest(raw), nil
}
func digest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

func hexDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

type Receipt struct {
	SchemaVersion          string                       `json:"schemaVersion"`
	State                  string                       `json:"state"`
	RetentionGrantID       string                       `json:"retentionGrantId"`
	Owner                  actorref.PrincipalRef        `json:"owner"`
	AgentID                string                       `json:"agentId"`
	EventID                string                       `json:"eventId,omitempty"`
	LogicalOperationID     string                       `json:"logicalOperationId,omitempty"`
	EffectKey              string                       `json:"effectKey,omitempty"`
	HandlerVersion         string                       `json:"handlerVersion,omitempty"`
	CandidateID            string                       `json:"candidateId,omitempty"`
	Candidate              *agentmemorycandidate.Record `json:"candidate,omitempty"`
	ObservedAt             time.Time                    `json:"observedAt"`
	Committed              bool                         `json:"committed"`
	ModelAccess            bool                         `json:"modelAccess"`
	MemoryPromotionAllowed bool                         `json:"memoryPromotionAllowed"`
	Explanation            string                       `json:"explanation"`
}

func ValidateReceipt(r Receipt) error {
	if r.SchemaVersion != Schema || !aep.ValidID(r.RetentionGrantID) || r.Owner.Type != actorref.Person || !aep.ValidID(r.Owner.ID) || !aep.ValidID(r.AgentID) || r.ObservedAt.IsZero() || r.ObservedAt.Year() < 1 || r.ObservedAt.Year() > 9999 || r.ModelAccess || r.MemoryPromotionAllowed || r.Explanation == "" {
		return ErrUnavailable
	}
	if r.State == "NOT_STAGED" {
		if r.Committed || r.EventID != "" || r.LogicalOperationID != "" || r.EffectKey != "" || r.HandlerVersion != "" || r.CandidateID != "" || r.Candidate != nil {
			return ErrUnavailable
		}
		return nil
	}
	if r.State != "CANDIDATE_STAGED" || !r.Committed || !aep.ValidID(r.EventID) || !aep.ValidID(r.LogicalOperationID) || !aep.ValidID(r.CandidateID) || r.HandlerVersion != Handler || len(r.EffectKey) != 64 {
		return ErrUnavailable
	}
	b, e := hex.DecodeString(r.EffectKey)
	if e != nil || len(b) != 32 || hex.EncodeToString(b) != r.EffectKey {
		return ErrUnavailable
	}
	if r.Candidate != nil {
		c := r.Candidate
		if c.SchemaVersion != agentmemorycandidate.Schema || c.ID != r.CandidateID || c.Owner != r.Owner || c.AgentID != r.AgentID || c.Version < 1 || c.Status != agentmemorycandidate.Candidate || c.ModelAccess != "UNAVAILABLE" || !c.ValidUntil.After(r.ObservedAt) || c.Assessment == nil || c.Assessment.Semantics != agentconfidence.Ordinal || c.Assessment.Level != "LOW" || c.Assessment.Value != nil || c.MemoryID != nil || c.MemoryVersion != nil || len(c.Sources) < 2 || len(c.Sources) > 5 || c.CreatedAt.IsZero() || c.CreatedAt.After(r.ObservedAt) || c.UpdatedAt.Before(c.CreatedAt) || c.UpdatedAt.After(r.ObservedAt) {
			return ErrUnavailable
		}
		selectors := make([]agentmemorycandidate.Selector, 0, len(c.Sources))
		seen := map[string]bool{}
		for _, src := range c.Sources {
			if src.Selector.Type != agentevent.MomentSource || !aep.ValidID(src.Selector.ID) || seen[src.Selector.ID] || src.Version.Kind != agentevent.RevisionVersion || src.Version.Revision < 1 || src.Version.Token != "" || src.EventTime.IsZero() || src.EventTime.After(c.CreatedAt) || len(src.Anchors) != 1 || src.Anchors[0] != "MOMENT:"+src.Selector.ID || len(src.Fingerprint) != 64 {
				return ErrUnavailable
			}
			value, e := hex.DecodeString(src.Fingerprint)
			if e != nil || len(value) != 32 || hex.EncodeToString(value) != src.Fingerprint {
				return ErrUnavailable
			}
			seen[src.Selector.ID] = true
			selectors = append(selectors, src.Selector)
		}
		if _, e := agentmemorycandidate.NormalizeDraft(agentmemorycandidate.Draft{Predicate: c.Predicate, Category: c.Category, Assessment: *c.Assessment, Sources: selectors, ValidUntil: c.ValidUntil}, r.ObservedAt); e != nil {
			return ErrUnavailable
		}
	}
	return nil
}

type Gateway interface {
	RetentionStore
	StageOwnMultiCandidate(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
	ReadOwnMultiCandidateReceipt(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
}
type Executor interface {
	RetentionStore
	StageOwnMultiCandidatePipeline(context.Context, agentprofile.PrivateAccess, string, *agentfeature.Controller, agentfeature.Ticket, *agentcognitive.CandidateSubmission) (Receipt, error)
	ReadOwnMultiCandidatePipeline(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
}

// PreviewReceipt contains historical approval metadata only, without a review.
type PreviewReceipt = aep.PreviewReceipt
type PreviewReceiptStore interface {
	ReadOwnMultiCandidatePreviewReceipt(context.Context, agentprofile.PrivateAccess, string) (PreviewReceipt, error)
}

func ValidatePreviewReceipt(r PreviewReceipt) error {
	if aep.ValidatePreviewReceipt(r, Purpose) != nil {
		return ErrUnavailable
	}
	return nil
}
