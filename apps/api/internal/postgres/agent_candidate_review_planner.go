package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"reflect"
	"time"
)

var _ agentplanner.CandidatePort = (*MemoryCandidateService)(nil)

// A strictly read-only projection of the original current human candidate.
// Unlike List/Read lifecycle refresh, this method never transitions EXPIRED,
// stages a candidate or accepts/rejects Memory. No model receives this source.
func (s *MemoryCandidateService) PlanOwnCandidateReview(ctx context.Context, a agentprofile.PrivateAccess, input agentplanner.CandidateSelection) (agentplanner.View, error) {
	if ctx == nil || !agentplanner.ValidID(input.LogicalOperationID) || (input.ID == "" && input.ExpectedVersion != 0) || (input.ID != "" && (!agentplanner.ValidID(input.ID) || input.ExpectedVersion < 1)) {
		return agentplanner.View{}, agentplanner.ErrInvalid
	}
	started := time.Now()
	bounded, cancel := context.WithTimeout(ctx, agentplanner.MaxElapsed)
	defer cancel()
	ctx = bounded
	tx, b, meta, ticket, e := s.begin(ctx, a)
	if e != nil {
		return agentplanner.View{}, e
	}
	defer tx.Rollback(ctx)
	var candidate candidateState
	if input.ID != "" {
		candidate, e = candidateLoad(ctx, tx, b, input.ID)
		if e != nil {
			return agentplanner.View{}, e
		}
		if candidate.record.Status != agentmemorycandidate.Candidate || candidate.record.Version != input.ExpectedVersion || candidate.metadata != meta {
			return agentplanner.View{}, agentplanner.ErrChanged
		}
		allowed, e := candidateCorrectionAllowed(ctx, tx, b.agentID, b.accountID, candidate.record.Predicate, candidate.record.Category)
		if e != nil || !allowed {
			return agentplanner.View{}, agentplanner.ErrDenied
		}
		support, e := candidateMultiSupportCurrent(ctx, tx, b, candidate.record.ID)
		if e != nil || !support {
			return agentplanner.View{}, agentplanner.ErrDenied
		}
		sources, now, e := candidateSources(ctx, tx, b, meta, candidateSelectors(candidate.record.Sources))
		if e != nil || !reflect.DeepEqual(sources, candidate.record.Sources) || !candidate.record.ValidUntil.After(now) {
			return agentplanner.View{}, agentplanner.ErrChanged
		}
	}
	var now, until time.Time
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() instant) SELECT n.instant,LEAST(expires_at,idle_expires_at,n.instant+interval '30 seconds') FROM sessions CROSS JOIN n WHERE id=$1 AND revoked_at IS NULL AND expires_at>n.instant AND idle_expires_at>n.instant`, b.sessionID).Scan(&now, &until)
	if e != nil {
		return agentplanner.View{}, agentplanner.ErrDenied
	}
	if input.ID != "" && candidate.record.ValidUntil.Before(until) {
		until = candidate.record.ValidUntil
	}
	if d, ok := ctx.Deadline(); ok && d.Before(until) {
		until = d
	}
	status, reason := agentplanner.Proposed, "human_candidate_review_only"
	if input.ID == "" {
		status, reason = agentplanner.Clarification, "candidate_selection_required"
	}
	v := agentplanner.NewView(status, "", input.LogicalOperationID, reason, now, until)
	if input.ID == "" {
		v.Clarifications = []string{"你想审阅哪一条已有候选记忆？请先选择具体候选。"}
	} else {
		// Candidate revision plus retained xmin binds ABA as well as source version.
		var generation string
		if tx.QueryRow(ctx, `SELECT xmin::text FROM agent_memory_candidates WHERE id=$1 AND owner_id=$2`, input.ID, b.accountID).Scan(&generation) != nil {
			return agentplanner.View{}, agentmemory.ErrUnavailable
		}
		version, e := agentreinforcement.Digest(struct {
			Domain, CandidateID, OwnerID, Generation string
			Version, Metadata                        int64
			Sources                                  []agentmemorycandidate.Source
		}{"birdtie.candidate-review.source.v1", input.ID, b.accountID, generation, input.ExpectedVersion, meta, candidate.record.Sources})
		if e != nil {
			return agentplanner.View{}, agentmemory.ErrUnavailable
		}
		v.Actions = []agentplanner.ActionProposal{agentplanner.Proposal(input.LogicalOperationID, 1, agentplanner.CandidateReview, agentplanner.Arguments{CandidateID: input.ID, ExpectedVersion: input.ExpectedVersion}, version, "打开原候选详情与来源，由你审阅；不会接受、拒绝或改写记忆。")}
	}
	if !v.Valid(now) {
		return agentplanner.View{}, agentplanner.ErrChanged
	}
	var selected []string
	if input.ID != "" {
		selected = []string{input.ID}
	}
	if e = s.finish(ctx, tx, b, ticket, &until, selected...); e != nil {
		return agentplanner.View{}, e
	}
	if ctx.Err() != nil || !s.flags.Current(ticket) || !v.ValidElapsed(started, time.Now()) {
		return agentplanner.View{}, agentplanner.ErrChanged
	}
	return v, nil
}
