// Package agentcandidateretention defines human approval of a bounded candidate
// hypothesis, independently of source analysis, model egress and Memory writes.
package agentcandidateretention

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

const Schema = "agent-candidate-retention-v1"
const Purpose = "STAGE_MEMORY_CANDIDATE"
const PreviewTTL = 90 * time.Second
const MaxRequestedRetention = 24 * time.Hour

var ErrInvalid = errors.New("候选保留输入无效")
var ErrDenied = errors.New("当前身份或来源不允许保留候选")
var ErrConflict = errors.New("候选保留的具体版本已变化")
var ErrExpired = errors.New("候选保留预览或许可已到期")
var ErrUnavailable = errors.New("当前候选保留服务不可用")
var ErrServerOnly = errors.New("候选保留解析仅供服务端使用")

type Selection struct {
	AnalysisGrantID string    `json:"analysisGrantId"`
	RetainUntil     time.Time `json:"retainUntil"`
}

func Normalize(s Selection) (Selection, error) {
	if !aep.ValidID(s.AnalysisGrantID) || !finite(s.RetainUntil) {
		return Selection{}, ErrInvalid
	}
	s.RetainUntil = s.RetainUntil.UTC()
	return s, nil
}
func finite(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Equal(t.Truncate(time.Microsecond))
}

type Review struct {
	Proposal       agentlocalcandidate.Proposal `json:"proposal"`
	Source         agentmemorycandidate.Source  `json:"source"`
	TaskID         string                       `json:"taskId"`
	SelectedFields []string                     `json:"selectedFields"`
	RetainUntil    time.Time                    `json:"retainUntil"`
	Clusters       int                          `json:"clusters"`
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

type Store interface {
	PreviewOwnCandidateRetention(context.Context, agentprofile.PrivateAccess, Selection) (Preview, error)
	ReadOwnCandidateRetentionPreview(context.Context, agentprofile.PrivateAccess, string) (Preview, error)
	ApproveOwnCandidateRetention(context.Context, agentprofile.PrivateAccess, string) (Grant, error)
	ReadOwnCandidateRetention(context.Context, agentprofile.PrivateAccess, string) (Grant, error)
	RevokeOwnCandidateRetention(context.Context, agentprofile.PrivateAccess, string, int64) (Grant, error)
	ResolveOwnCandidateRetention(context.Context, agentprofile.PrivateAccess, string) (Resolution, error)
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
	if r.Clusters != 1 || !aep.ValidID(r.TaskID) || !r.RetainUntil.Equal(p.ExpiresAt) || !agentlocalcandidate.ValidVersionCategory(r.Proposal.AlgorithmVersion, r.Proposal.Category) || r.Proposal.Predicate != "ACTIVITY_CATEGORY" || r.Proposal.Explanation == "" || len(r.SelectedFields) < 1 || len(r.SelectedFields) > 2 {
		return ErrUnavailable
	}
	normalized, e := agentmemorycandidate.NormalizeDraft(agentmemorycandidate.Draft{Predicate: r.Proposal.Predicate, Category: r.Proposal.Category, Assessment: r.Proposal.Assessment, Sources: []agentmemorycandidate.Selector{r.Source.Selector}, ValidUntil: r.RetainUntil}, p.ObservedAt)
	if e != nil || normalized.Assessment.Level != "LOW" || normalized.Assessment.Value != nil {
		return ErrUnavailable
	}
	if r.Source.Selector.Type != "MOMENT" || r.Source.Version.Kind != "REVISION" || r.Source.Version.Revision < 1 || r.Source.Version.Token != "" || !finite(r.Source.EventTime) || r.Source.EventTime.After(p.ObservedAt) || !hexDigest(r.Source.Fingerprint) || len(r.Source.Anchors) != 1 || r.Source.Anchors[0] != "MOMENT:"+r.Source.Selector.ID {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, v := range r.SelectedFields {
		if (v != "title" && v != "body") || seen[v] {
			return ErrUnavailable
		}
		seen[v] = true
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
