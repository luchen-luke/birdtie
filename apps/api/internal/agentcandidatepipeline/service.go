package agentcandidatepipeline

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

type Service struct {
	executor   Executor
	controller *agentfeature.Controller
	handler    Handler
}

func NewService(e Executor, c *agentfeature.Controller) *Service { return &Service{e, c, LocalV1} }

// WithHandler is exclusively a trusted native/server construction option. A
// wire request has no handler, worker or fencing fields.
func NewServiceWithHandler(e Executor, c *agentfeature.Controller, h Handler) *Service {
	return &Service{e, c, h}
}
func (s *Service) stage(ctx context.Context, a agentprofile.PrivateAccess, id string, ref *agentcognitive.CandidateSubmission) (Receipt, error) {
	if ctx == nil || agentprofile.ValidatePrivateAccess(a) != nil || !agentenrichmentpurpose.ValidID(id) {
		return Receipt{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return Receipt{}, e
	}
	if s == nil || s.executor == nil || s.controller == nil || !ValidHandler(s.handler) {
		return Receipt{}, ErrUnavailable
	}
	ticket, e := s.controller.Capture(agentfeature.Memory)
	if e != nil {
		return Receipt{}, ErrUnavailable
	}
	out, e := s.executor.StageOwnCandidatePipeline(ctx, a, id, s.handler, s.controller, ticket, ref)
	if e != nil {
		return Receipt{}, e
	}
	if ctx.Err() != nil || !s.controller.Current(ticket) {
		return Receipt{}, ErrUnavailable
	} // unknown commit: reconcile original grant ID
	if ValidateReceipt(out) != nil || out.Owner != a.WorkspacePrincipal || out.RetentionGrantID != id || !out.Committed {
		return Receipt{}, ErrUnavailable
	}
	return out, nil
}
func (s *Service) StageOwnMomentCandidate(ctx context.Context, a agentprofile.PrivateAccess, id string) (Receipt, error) {
	return s.stage(ctx, a, id, nil)
}
func (s *Service) ReadOwnMomentCandidateReceipt(ctx context.Context, a agentprofile.PrivateAccess, id string) (Receipt, error) {
	if ctx == nil || agentprofile.ValidatePrivateAccess(a) != nil || !agentenrichmentpurpose.ValidID(id) {
		return Receipt{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return Receipt{}, e
	}
	if s == nil || s.executor == nil {
		return Receipt{}, ErrUnavailable
	}
	out, e := s.executor.ReadOwnCandidatePipeline(ctx, a, id)
	if e != nil {
		return Receipt{}, e
	}
	if ctx.Err() != nil || ValidateReceipt(out) != nil || out.Owner != a.WorkspacePrincipal || out.RetentionGrantID != id {
		return Receipt{}, ErrUnavailable
	}
	return out, nil
}

type boundSubmitter struct {
	s      *Service
	access agentprofile.PrivateAccess
	grant  string
}

// The constructor captures server-derived access and the original concrete
// retention ID; it does not grant authority or cache a native resolution.
func (s *Service) BindCandidateSubmitter(a agentprofile.PrivateAccess, id string) agentcognitive.CandidateSubmitter {
	return &boundSubmitter{s, a, id}
}
func (s *boundSubmitter) SubmitMemoryCandidate(ctx context.Context, r agentcognitive.CandidateSubmission) (agentcognitive.CandidateReceipt, error) {
	_, e := s.s.stage(ctx, s.access, s.grant, &r)
	if e != nil {
		return agentcognitive.CandidateReceipt{Status: agentcognitive.Unavailable, Reason: "candidate_pipeline_not_committed"}, e
	}
	return agentcognitive.CandidateReceipt{Status: agentcognitive.Available, Reason: "candidate_staged_native"}, nil
}

var _ Gateway = (*Service)(nil)
var _ agentcognitive.CandidateSubmitter = (*boundSubmitter)(nil)
