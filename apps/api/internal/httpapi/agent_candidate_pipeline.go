package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acp "github.com/birdtie/birdtie/apps/api/internal/agentcandidatepipeline"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
)

func candidatePipelineHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, acp.ErrInvalid):
		respondError(w, 400, "invalid_candidate_pipeline")
	case errors.Is(e, acp.ErrDenied):
		respondError(w, 403, "candidate_pipeline_denied")
	case errors.Is(e, acp.ErrExpired), errors.Is(e, acp.ErrConflict), errors.Is(e, acp.ErrBusy):
		respondError(w, 409, "candidate_pipeline_changed")
	default:
		respondError(w, 503, "candidate_pipeline_unavailable")
	}
}
func (s *server) candidatePipelineAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, acp.Gateway, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		candidatePipelineHTTPError(w, acp.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		candidatePipelineHTTPError(w, acp.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if s.candidatePipeline == nil || s.access == nil {
		candidatePipelineHTTPError(w, acp.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentprofile.PrivateAccess{}, nil, false
	}
	if e != nil {
		candidatePipelineHTTPError(w, acp.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		candidatePipelineHTTPError(w, acp.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return a, s.candidatePipeline, true
}
func (s *server) stageOwnMomentCandidate(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.candidatePipelineAccess(w, r)
	if !ok {
		return
	}
	raw, ok := candidateRetentionBody(w, r, "retentionGrantId")
	if !ok {
		return
	}
	var in struct {
		ID string `json:"retentionGrantId"`
	}
	if json.Unmarshal(raw, &in) != nil || !aep.ValidID(in.ID) {
		candidatePipelineHTTPError(w, acp.ErrInvalid)
		return
	}
	out, e := p.StageOwnMomentCandidate(r.Context(), a, in.ID)
	if e != nil {
		candidatePipelineHTTPError(w, e)
		return
	}
	if acp.ValidateReceipt(out) != nil || out.Owner != a.WorkspacePrincipal || out.RetentionGrantID != in.ID || !out.Committed {
		candidatePipelineHTTPError(w, acp.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
func (s *server) getOwnMomentCandidateReceipt(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.candidatePipelineAccess(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	id := r.PathValue("grantID")
	if !aep.ValidID(id) {
		candidatePipelineHTTPError(w, acp.ErrInvalid)
		return
	}
	out, e := p.ReadOwnMomentCandidateReceipt(r.Context(), a, id)
	if e != nil {
		candidatePipelineHTTPError(w, e)
		return
	}
	if acp.ValidateReceipt(out) != nil || out.Owner != a.WorkspacePrincipal || out.RetentionGrantID != id {
		candidatePipelineHTTPError(w, acp.ErrUnavailable)
		return
	}
	respond(w, 200, out)
}
