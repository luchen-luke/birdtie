// Package agentcandidatepipeline owns an actual, purpose-bound local consumer.
// A receipt/reference/digest is never a replacement for native permission.
package agentcandidatepipeline

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"time"
)

const Schema = "agent-candidate-pipeline-v1"

type Handler string

const LocalV1 Handler = "mom-candidate-local-v1"
const LocalV2 Handler = "mom-candidate-local-v2"

// This stable action identifies candidate staging, never an individual payload,
// run/task, model, grant, source event or handler implementation version.
const StageActionID = "519fde3b-cc12-4abc-8c4f-f3227290a815"

var ErrInvalid = errors.New("候选提交输入无效")
var ErrDenied = errors.New("当前本人许可或来源不允许提交候选")
var ErrExpired = errors.New("候选许可、来源或事件租约已到期")
var ErrConflict = errors.New("具体候选版本已变化或已处理，请先核实原结果")
var ErrBusy = errors.New("当前事件正在处理，请核实原结果后重试")
var ErrUnavailable = errors.New("候选提交功能当前不可用")

func ValidHandler(h Handler) bool { return h == LocalV1 || h == LocalV2 }

type Receipt struct {
	SchemaVersion          string                       `json:"schemaVersion"`
	State                  string                       `json:"state"`
	RetentionGrantID       string                       `json:"retentionGrantId"`
	Owner                  actorref.PrincipalRef        `json:"owner"`
	AgentID                string                       `json:"agentId"`
	EventID                string                       `json:"eventId,omitempty"`
	LogicalOperationID     string                       `json:"logicalOperationId,omitempty"`
	EffectKey              string                       `json:"effectKey,omitempty"`
	HandlerVersion         Handler                      `json:"handlerVersion,omitempty"`
	CandidateID            string                       `json:"candidateId,omitempty"`
	Candidate              *agentmemorycandidate.Record `json:"candidate,omitempty"`
	ObservedAt             time.Time                    `json:"observedAt"`
	Committed              bool                         `json:"committed"`
	ModelAccess            bool                         `json:"modelAccess"`
	MemoryPromotionAllowed bool                         `json:"memoryPromotionAllowed"`
	Explanation            string                       `json:"explanation"`
}

func ValidateReceipt(r Receipt) error {
	if r.SchemaVersion != Schema || !agentenrichmentpurpose.ValidID(r.RetentionGrantID) || r.Owner.Type != actorref.Person || !agentenrichmentpurpose.ValidID(r.Owner.ID) || !agentenrichmentpurpose.ValidID(r.AgentID) || r.ObservedAt.IsZero() || r.ObservedAt.Year() < 1 || r.ObservedAt.Year() > 9999 || r.ModelAccess || r.MemoryPromotionAllowed || r.Explanation == "" {
		return ErrUnavailable
	}
	if r.State == "NOT_STAGED" {
		if r.Committed || r.EventID != "" || r.LogicalOperationID != "" || r.EffectKey != "" || r.HandlerVersion != "" || r.CandidateID != "" || r.Candidate != nil {
			return ErrUnavailable
		}
		return nil
	}
	if r.State != "CANDIDATE_STAGED" || !r.Committed || !agentenrichmentpurpose.ValidID(r.EventID) || !agentenrichmentpurpose.ValidID(r.LogicalOperationID) || !agentenrichmentpurpose.ValidID(r.CandidateID) || !ValidHandler(r.HandlerVersion) || len(r.EffectKey) != 64 {
		return ErrUnavailable
	}
	b, e := hex.DecodeString(r.EffectKey)
	if e != nil || len(b) != 32 || hex.EncodeToString(b) != r.EffectKey {
		return ErrUnavailable
	}
	if r.Candidate != nil {
		c := r.Candidate
		if c.SchemaVersion != agentmemorycandidate.Schema || c.ID != r.CandidateID || c.Owner != r.Owner || c.AgentID != r.AgentID || c.Version < 1 || c.Status != agentmemorycandidate.Candidate || c.ModelAccess != "UNAVAILABLE" || !c.ValidUntil.After(r.ObservedAt) || c.Assessment == nil || c.Assessment.Semantics != agentconfidence.Ordinal || c.Assessment.Level != "LOW" || c.Assessment.Value != nil || c.MemoryID != nil || c.MemoryVersion != nil || len(c.Sources) != 1 || c.CreatedAt.IsZero() || c.CreatedAt.After(r.ObservedAt) || c.UpdatedAt.Before(c.CreatedAt) || c.UpdatedAt.After(r.ObservedAt) {
			return ErrUnavailable
		}
		src := c.Sources[0]
		if _, e := agentmemorycandidate.NormalizeDraft(agentmemorycandidate.Draft{Predicate: c.Predicate, Category: c.Category, Assessment: *c.Assessment, Sources: []agentmemorycandidate.Selector{src.Selector}, ValidUntil: c.ValidUntil}, r.ObservedAt); e != nil {
			return ErrUnavailable
		}
		if src.Selector.Type != agentevent.MomentSource || src.Version.Kind != agentevent.RevisionVersion || src.Version.Revision < 1 || src.Version.Token != "" || src.EventTime.IsZero() || src.EventTime.After(c.CreatedAt) || len(src.Anchors) != 1 || src.Anchors[0] != "MOMENT:"+src.Selector.ID || len(src.Fingerprint) != 64 {
			return ErrUnavailable
		}
		if value, e := hex.DecodeString(src.Fingerprint); e != nil || len(value) != 32 || hex.EncodeToString(value) != src.Fingerprint {
			return ErrUnavailable
		}
	}
	return nil
}

type Gateway interface {
	StageOwnMomentCandidate(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
	ReadOwnMomentCandidateReceipt(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
}

// Executor is injected by trusted server construction. The current ticket and
// same controller must be checked in the native transaction before Commit.
// A supplied legacy submission is merely a consistency constraint, not grant.
type Executor interface {
	StageOwnCandidatePipeline(context.Context, agentprofile.PrivateAccess, string, Handler, *agentfeature.Controller, agentfeature.Ticket, *agentcognitive.CandidateSubmission) (Receipt, error)
	ReadOwnCandidatePipeline(context.Context, agentprofile.PrivateAccess, string) (Receipt, error)
}
