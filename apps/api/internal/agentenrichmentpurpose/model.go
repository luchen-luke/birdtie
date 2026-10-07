// Package agentenrichmentpurpose exposes explicit local source-analysis consent.
// It grants neither model egress nor candidate/Memory retention or effects.
package agentenrichmentpurpose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"io"
	"sort"
	"time"
)

const Purpose = "MOMENT_LOCAL_ANALYSIS"
const Schema = "agent-enrichment-purpose-v1"
const PreviewTTL = 5 * time.Minute
const MaxDeadline = 15 * time.Minute

var ErrInvalid = errors.New("分析许可输入无效")
var ErrDenied = errors.New("当前身份或具体来源不允许此分析")
var ErrExpired = errors.New("分析预览或许可已过期")
var ErrConflict = errors.New("分析许可的具体版本已变化")
var ErrUnavailable = errors.New("当前分析许可服务不可用")
var ErrServerOnly = errors.New("分析解析仅供服务端使用")

type Selection struct {
	TaskID         string    `json:"taskId"`
	MomentID       string    `json:"momentId"`
	MomentRevision int64     `json:"momentRevision"`
	Fields         []string  `json:"fields"`
	DeadlineAt     time.Time `json:"deadlineAt"`
}

func ValidID(s string) bool {
	p, e := actorref.ParsePrincipal("person", s)
	return e == nil && p.ID == s && s != "00000000-0000-0000-0000-000000000000"
}
func Normalize(s Selection) (Selection, error) {
	if !ValidID(s.TaskID) || !ValidID(s.MomentID) || s.MomentRevision < 1 || s.DeadlineAt.IsZero() || s.DeadlineAt.Year() < 1 || s.DeadlineAt.Year() > 9999 || !s.DeadlineAt.Equal(s.DeadlineAt.Truncate(time.Microsecond)) || len(s.Fields) < 1 || len(s.Fields) > 2 {
		return Selection{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, k := range s.Fields {
		if (k != "title" && k != "body") || seen[k] {
			return Selection{}, ErrInvalid
		}
		seen[k] = true
	}
	s.Fields = append([]string{}, s.Fields...)
	sort.Strings(s.Fields)
	s.DeadlineAt = s.DeadlineAt.UTC()
	return s, nil
}

type Review struct {
	TaskQuery     string            `json:"taskQuery"`
	TaskUpdatedAt time.Time         `json:"taskUpdatedAt"`
	Content       map[string]string `json:"content"`
}
type Preview struct {
	State                     string                `json:"state"`
	SchemaVersion             string                `json:"schemaVersion"`
	ID                        string                `json:"id"`
	Purpose                   string                `json:"purpose"`
	Owner                     actorref.PrincipalRef `json:"owner"`
	AgentID                   string                `json:"agentId"`
	Selection                 Selection             `json:"selection"`
	Review                    Review                `json:"review"`
	ObservedAt                time.Time             `json:"observedAt"`
	ExpiresAt                 time.Time             `json:"expiresAt"`
	ConsumedGrantID           string                `json:"consumedGrantId,omitempty"`
	Explanation               string                `json:"explanation"`
	ModelAccess               bool                  `json:"modelAccess"`
	CandidateRetentionAllowed bool                  `json:"candidateRetentionAllowed"`
}
type Grant struct {
	SchemaVersion             string                `json:"schemaVersion"`
	ID                        string                `json:"id"`
	PreviewID                 string                `json:"previewId"`
	Purpose                   string                `json:"purpose"`
	Owner                     actorref.PrincipalRef `json:"owner"`
	AgentID                   string                `json:"agentId"`
	Selection                 Selection             `json:"selection"`
	Revision                  int64                 `json:"revision"`
	CreatedAt                 time.Time             `json:"createdAt"`
	ExpiresAt                 time.Time             `json:"expiresAt"`
	RevokedAt                 *time.Time            `json:"revokedAt,omitempty"`
	ObservedAt                time.Time             `json:"observedAt"`
	ModelAccess               bool                  `json:"modelAccess"`
	CandidateRetentionAllowed bool                  `json:"candidateRetentionAllowed"`
}
type Resolution struct {
	GrantID               string
	GrantRevision         int64
	Owner                 actorref.PrincipalRef
	AgentID               string
	Selection             Selection
	Content               map[string]string
	ObservedAt, ExpiresAt time.Time
}

func (Resolution) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (v *Resolution) UnmarshalJSON([]byte) error { *v = Resolution{}; return ErrServerOnly }

type Store interface {
	PreviewOwnEnrichmentPurpose(context.Context, agentprofile.PrivateAccess, Selection) (Preview, error)
	ReadOwnEnrichmentPurposePreview(context.Context, agentprofile.PrivateAccess, string) (Preview, error)
	ApproveOwnEnrichmentPurpose(context.Context, agentprofile.PrivateAccess, string) (Grant, error)
	ReadOwnEnrichmentPurpose(context.Context, agentprofile.PrivateAccess, string) (Grant, error)
	RevokeOwnEnrichmentPurpose(context.Context, agentprofile.PrivateAccess, string, int64) (Grant, error)
	ResolveOwnEnrichmentPurpose(context.Context, agentprofile.PrivateAccess, string) (Resolution, error)
}

// StrictObject is a closed wire decoder, not an authorization constructor.
func StrictObject(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	if len(raw) > 8192 {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok {
			return nil, ErrInvalid
		}
		valid := false
		for _, want := range keys {
			valid = valid || k == want
		}
		if _, dup := out[k]; dup || !valid {
			return nil, ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, ErrInvalid
		}
		out[k] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(out) != len(keys) {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return out, nil
}
func ValidatePreview(p Preview) error {
	s, e := Normalize(p.Selection)
	if e != nil || !ValidID(p.ID) || p.SchemaVersion != Schema || p.Purpose != Purpose || p.Owner.Type != actorref.Person || !ValidID(p.Owner.ID) || !ValidID(p.AgentID) || p.ModelAccess || p.CandidateRetentionAllowed || p.Explanation == "" || p.ObservedAt.IsZero() || p.ExpiresAt.IsZero() || p.ExpiresAt.After(s.DeadlineAt) || (p.ConsumedGrantID != "" && !ValidID(p.ConsumedGrantID)) {
		return ErrUnavailable
	}
	if p.State == "RECEIPT_ONLY" {
		if p.ConsumedGrantID == "" || len(p.Review.Content) != 0 || p.Review.TaskQuery != "" || !p.Review.TaskUpdatedAt.IsZero() {
			return ErrUnavailable
		}
		return nil
	}
	if p.State != "CURRENT_REVIEW" || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.After(p.ObservedAt.Add(PreviewTTL)) || p.Review.TaskUpdatedAt.IsZero() || p.Review.TaskQuery == "" || len(p.Review.Content) != len(s.Fields) {
		return ErrUnavailable
	}
	for _, k := range s.Fields {
		if _, ok := p.Review.Content[k]; !ok {
			return ErrUnavailable
		}
	}
	return nil
}
func ValidateGrant(g Grant) error {
	_, e := Normalize(g.Selection)
	if e != nil || g.SchemaVersion != Schema || g.Purpose != Purpose || !ValidID(g.ID) || !ValidID(g.PreviewID) || g.Owner.Type != actorref.Person || !ValidID(g.Owner.ID) || !ValidID(g.AgentID) || g.Revision < 1 || g.CreatedAt.IsZero() || g.ObservedAt.Before(g.CreatedAt) || !g.ExpiresAt.After(g.CreatedAt) || g.ExpiresAt.After(g.Selection.DeadlineAt) || g.ModelAccess || g.CandidateRetentionAllowed || (g.RevokedAt != nil && (g.RevokedAt.Before(g.CreatedAt) || g.RevokedAt.After(g.ObservedAt))) {
		return ErrUnavailable
	}
	return nil
}

// PreviewReceipt is an original approval-record observation, never a source
// review, grant or execution authority. Its read lease does not renew approval.
type PreviewReceipt struct {
	SchemaVersion           string                `json:"schemaVersion"`
	PreviewID               string                `json:"previewId"`
	Purpose                 string                `json:"purpose"`
	Owner                   actorref.PrincipalRef `json:"owner"`
	AgentID                 string                `json:"agentId"`
	State                   string                `json:"state"`
	ConsumedGrantID         string                `json:"consumedGrantId,omitempty"`
	PreviewObservedAt       time.Time             `json:"previewObservedAt"`
	PreviewExpiresAt        time.Time             `json:"previewExpiresAt"`
	ObservedAt              time.Time             `json:"observedAt"`
	ValidUntil              time.Time             `json:"validUntil"`
	ModelAccess             bool                  `json:"modelAccess"`
	CandidateWriteAvailable bool                  `json:"candidateWriteAvailable"`
	MemoryPromotionAllowed  bool                  `json:"memoryPromotionAllowed"`
}
type PreviewReceiptStore interface {
	ReadOwnEnrichmentPurposePreviewReceipt(context.Context, agentprofile.PrivateAccess, string) (PreviewReceipt, error)
}

func ValidatePreviewReceipt(r PreviewReceipt, purpose string) error {
	schema := "agent-enrichment-purpose-preview-receipt-v1"
	if purpose == "STAGE_MEMORY_CANDIDATE_MULTI" {
		schema = "agent-multi-candidate-preview-receipt-v1"
	} else if purpose != Purpose {
		return ErrUnavailable
	}
	if r.SchemaVersion != schema || r.Purpose != purpose || !ValidID(r.PreviewID) || r.Owner.Type != actorref.Person || !ValidID(r.Owner.ID) || !ValidID(r.AgentID) ||
		r.ModelAccess || r.CandidateWriteAvailable || r.MemoryPromotionAllowed ||
		!receiptFinite(r.PreviewObservedAt) || !receiptFinite(r.PreviewExpiresAt) || !receiptFinite(r.ObservedAt) || !receiptFinite(r.ValidUntil) ||
		!r.PreviewExpiresAt.After(r.PreviewObservedAt) || r.PreviewExpiresAt.Sub(r.PreviewObservedAt) > PreviewTTL || r.ObservedAt.Before(r.PreviewObservedAt) ||
		!r.ValidUntil.After(r.ObservedAt) || r.ValidUntil.Sub(r.ObservedAt) > 30*time.Second {
		return ErrUnavailable
	}
	switch r.State {
	case "APPROVAL_RECORDED":
		if !ValidID(r.ConsumedGrantID) {
			return ErrUnavailable
		}
	case "OPEN_UNCONSUMED":
		if r.ConsumedGrantID != "" || !r.PreviewExpiresAt.After(r.ObservedAt) {
			return ErrUnavailable
		}
	case "CLOSED_UNCONSUMED":
		if r.ConsumedGrantID != "" || r.PreviewExpiresAt.After(r.ObservedAt) {
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	return nil
}

func receiptFinite(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
