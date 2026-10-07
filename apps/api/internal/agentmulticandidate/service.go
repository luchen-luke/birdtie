package agentmulticandidate

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
}

func NewService(e Executor, c *agentfeature.Controller) *Service { return &Service{e, c} }

func (s *Service) stage(ctx context.Context, a agentprofile.PrivateAccess, id string, ref *agentcognitive.CandidateSubmission) (Receipt, error) {
	if ctx == nil || agentprofile.ValidatePrivateAccess(a) != nil || !agentenrichmentpurpose.ValidID(id) {
		return Receipt{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return Receipt{}, e
	}
	if s == nil || s.executor == nil || s.controller == nil {
		return Receipt{}, ErrUnavailable
	}
	ticket, e := s.controller.Capture(agentfeature.Memory)
	if e != nil {
		return Receipt{}, ErrUnavailable
	}
	out, e := s.executor.StageOwnMultiCandidatePipeline(ctx, a, id, s.controller, ticket, ref)
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
func (s *Service) StageOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, id string) (Receipt, error) {
	return s.stage(ctx, a, id, nil)
}
func (s *Service) ReadOwnMultiCandidateReceipt(ctx context.Context, a agentprofile.PrivateAccess, id string) (Receipt, error) {
	if ctx == nil || agentprofile.ValidatePrivateAccess(a) != nil || !agentenrichmentpurpose.ValidID(id) {
		return Receipt{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return Receipt{}, e
	}
	if s == nil || s.executor == nil {
		return Receipt{}, ErrUnavailable
	}
	out, e := s.executor.ReadOwnMultiCandidatePipeline(ctx, a, id)
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

func (s *Service) permit(ctx context.Context, a agentprofile.PrivateAccess) (agentfeature.Ticket, error) {
	if ctx == nil || agentprofile.ValidatePrivateAccess(a) != nil {
		return agentfeature.Ticket{}, ErrInvalid
	}
	if ctx.Err() != nil {
		return agentfeature.Ticket{}, ctx.Err()
	}
	if s == nil || s.executor == nil || s.controller == nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	t, e := s.controller.Capture(agentfeature.Memory)
	if e != nil {
		return agentfeature.Ticket{}, ErrUnavailable
	}
	return t, nil
}
func (s *Service) PreviewOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, in Selection) (Preview, error) {
	t, e := s.permit(ctx, a)
	if e != nil {
		return Preview{}, e
	}
	in, e = Normalize(in)
	if e != nil {
		return Preview{}, e
	}
	v, e := s.executor.PreviewOwnMultiCandidate(ctx, a, in)
	if e != nil {
		return Preview{}, e
	}
	if !s.controller.Current(t) || ctx.Err() != nil || ValidatePreview(v) != nil || v.Owner != a.WorkspacePrincipal {
		return Preview{}, ErrUnavailable
	}
	return v, nil
}
func (s *Service) ReadOwnMultiCandidatePreview(ctx context.Context, a agentprofile.PrivateAccess, id string) (Preview, error) {
	if !agentenrichmentpurpose.ValidID(id) {
		return Preview{}, ErrInvalid
	}
	if s == nil || s.executor == nil {
		return Preview{}, ErrUnavailable
	}
	v, e := s.executor.ReadOwnMultiCandidatePreview(ctx, a, id)
	if e == nil && (ValidatePreview(v) != nil || v.Owner != a.WorkspacePrincipal || v.ID != id) {
		return Preview{}, ErrUnavailable
	}
	return v, e
}
func (s *Service) ApproveOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, id string) (Grant, error) {
	t, e := s.permit(ctx, a)
	if e != nil {
		return Grant{}, e
	}
	if !agentenrichmentpurpose.ValidID(id) {
		return Grant{}, ErrInvalid
	}
	v, e := s.executor.ApproveOwnMultiCandidate(ctx, a, id)
	if e == nil && (!s.controller.Current(t) || ctx.Err() != nil || ValidateGrant(v) != nil || v.Owner != a.WorkspacePrincipal || v.PreviewID != id) {
		return Grant{}, ErrUnavailable
	}
	return v, e
}
func (s *Service) ReadOwnMultiCandidateGrant(ctx context.Context, a agentprofile.PrivateAccess, id string) (Grant, error) {
	if !agentenrichmentpurpose.ValidID(id) {
		return Grant{}, ErrInvalid
	}
	if s == nil || s.executor == nil {
		return Grant{}, ErrUnavailable
	}
	v, e := s.executor.ReadOwnMultiCandidateGrant(ctx, a, id)
	if e == nil && (ValidateGrant(v) != nil || v.Owner != a.WorkspacePrincipal || v.ID != id) {
		return Grant{}, ErrUnavailable
	}
	return v, e
}
func (s *Service) RevokeOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, id string, revision int64) (Grant, error) {
	if !agentenrichmentpurpose.ValidID(id) || revision < 1 || revision == int64(^uint64(0)>>1) {
		return Grant{}, ErrInvalid
	}
	if s == nil || s.executor == nil {
		return Grant{}, ErrUnavailable
	}
	v, e := s.executor.RevokeOwnMultiCandidate(ctx, a, id, revision)
	if e == nil && (ValidateGrant(v) != nil || v.Owner != a.WorkspacePrincipal || v.ID != id || v.RevokedAt == nil || v.Revision != revision+1) {
		return Grant{}, ErrUnavailable
	}
	return v, e
}

func (s *Service) ReadOwnMultiCandidatePreviewReceipt(ctx context.Context, a agentprofile.PrivateAccess, id string) (PreviewReceipt, error) {
	if ctx == nil || agentprofile.ValidatePrivateAccess(a) != nil || !agentenrichmentpurpose.ValidID(id) {
		return PreviewReceipt{}, ErrInvalid
	}
	if ctx.Err() != nil || s == nil || s.executor == nil {
		return PreviewReceipt{}, ErrUnavailable
	}
	port, ok := s.executor.(PreviewReceiptStore)
	if !ok || port == nil {
		return PreviewReceipt{}, ErrUnavailable
	}
	out, e := port.ReadOwnMultiCandidatePreviewReceipt(ctx, a, id)
	if e != nil {
		return PreviewReceipt{}, e
	}
	if ctx.Err() != nil || ValidatePreviewReceipt(out) != nil || out.PreviewID != id || out.Owner != a.WorkspacePrincipal {
		return PreviewReceipt{}, ErrUnavailable
	}
	return out, nil
}
