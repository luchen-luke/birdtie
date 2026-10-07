package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
)

func agentRunHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, ar.ErrInvalid):
		respondError(w, 400, "invalid_agent_run")
	case errors.Is(e, ar.ErrDenied):
		respondError(w, 403, "agent_run_denied")
	case errors.Is(e, ar.ErrNotFound):
		respondError(w, 404, "agent_run_not_found")
	case errors.Is(e, ar.ErrConflict), errors.Is(e, ar.ErrExpired):
		respondError(w, 409, "agent_run_changed")
	default:
		respondError(w, 503, "agent_run_unavailable")
	}
}
func (s *server) agentRunAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		agentRunHTTPError(w, ar.ErrDenied)
		return agentprofile.PrivateAccess{}, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		agentRunHTTPError(w, ar.ErrInvalid)
		return agentprofile.PrivateAccess{}, false
	}
	if s.agentRuns == nil || s.access == nil {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentprofile.PrivateAccess{}, false
	}
	if e != nil {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		agentRunHTTPError(w, ar.ErrDenied)
		return a, false
	}
	return a, true
}
func agentRunReply(w http.ResponseWriter, a agentprofile.PrivateAccess, id string, v ar.Record, e error) {
	if e != nil {
		agentRunHTTPError(w, e)
		return
	}
	if ar.ValidateRecord(v) != nil || v.Owner != a.WorkspacePrincipal || (id != "" && v.ID != id) {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	respond(w, 200, v)
}
func (s *server) scheduleOwnAgentRun(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agentRunAccess(w, r)
	if !ok {
		return
	}
	raw, ok := candidateRetentionBody(w, r, "momentId", "retentionGrantId")
	if !ok {
		return
	}
	var in ar.Input
	if json.Unmarshal(raw, &in) != nil || ar.ValidateInput(in) != nil {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	out, e := s.agentRuns.ScheduleOwn(r.Context(), a, in)
	agentRunReply(w, a, "", out, e)
}
func (s *server) readOwnAgentRun(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agentRunAccess(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	id := r.PathValue("runID")
	if !aep.ValidID(id) {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	out, e := s.agentRuns.ReadOwn(r.Context(), a, id)
	agentRunReply(w, a, id, out, e)
}
func (s *server) cancelOwnAgentRun(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agentRunAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("runID")
	raw, ok := candidateRetentionBody(w, r, "expectedVersion")
	if !ok {
		return
	}
	var in struct {
		Version int64 `json:"expectedVersion"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Version < 1 || !aep.ValidID(id) {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	out, e := s.agentRuns.CancelOwn(r.Context(), a, id, in.Version)
	agentRunReply(w, a, id, out, e)
}
func (s *server) attachOwnAgentRunGrant(w http.ResponseWriter, r *http.Request) {
	a, ok := s.agentRunAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("runID")
	raw, ok := candidateRetentionBody(w, r, "expectedVersion", "retentionGrantId")
	if !ok {
		return
	}
	var in struct {
		Version int64  `json:"expectedVersion"`
		Grant   string `json:"retentionGrantId"`
	}
	if json.Unmarshal(raw, &in) != nil || in.Version < 1 || !aep.ValidID(id) || !aep.ValidID(in.Grant) {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	out, e := s.agentRuns.AttachOwnGrant(r.Context(), a, id, in.Version, in.Grant)
	agentRunReply(w, a, id, out, e)
}

func (s *server) agentRunRecoveryPort(w http.ResponseWriter, r *http.Request) (ar.RecoveryGateway, agentprofile.PrivateAccess, bool) {
	a, ok := s.agentRunAccess(w, r)
	if !ok {
		return nil, a, false
	}
	p, ok := s.agentRuns.(ar.RecoveryGateway)
	if !ok {
		agentRunHTTPError(w, ar.ErrUnavailable)
	}
	return p, a, ok
}
func (s *server) readOwnAgentRunFailure(w http.ResponseWriter, r *http.Request) {
	p, a, ok := s.agentRunRecoveryPort(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	id := r.PathValue("runID")
	if !aep.ValidID(id) {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	v, e := p.ReadOwnFailure(r.Context(), a, id)
	if e != nil {
		agentRunHTTPError(w, e)
		return
	}
	if ar.ValidateFailureView(v) != nil || v.Run.Owner != a.WorkspacePrincipal || v.Run.ID != id {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	respond(w, 200, v)
}
func (s *server) listOwnAgentRunFailures(w http.ResponseWriter, r *http.Request) {
	p, a, ok := s.agentRunRecoveryPort(w, r)
	if !ok || !candidateRetentionEmpty(w, r) {
		return
	}
	vs, e := p.ListOwnFailures(r.Context(), a)
	if e != nil {
		agentRunHTTPError(w, e)
		return
	}
	if len(vs) > 50 {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	for _, v := range vs {
		if ar.ValidateFailureView(v) != nil || v.Run.Owner != a.WorkspacePrincipal {
			agentRunHTTPError(w, ar.ErrUnavailable)
			return
		}
	}
	respond(w, 200, vs)
}
func (s *server) recoverOwnAgentRun(w http.ResponseWriter, r *http.Request) {
	p, a, ok := s.agentRunRecoveryPort(w, r)
	if !ok {
		return
	}
	raw, ok := candidateRetentionBody(w, r, "expectedVersion", "reason")
	if !ok {
		return
	}
	id := r.PathValue("runID")
	var in ar.RecoveryInput
	if !aep.ValidID(id) || json.Unmarshal(raw, &in) != nil || ar.ValidateRecoveryInput(in) != nil {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	v, e := p.RecoverOwn(r.Context(), a, id, in)
	if e == nil && (v.Generation != 1 || v.RecoveryRootID != id || v.RecoveryPreviousVersion != in.ExpectedVersion) {
		e = ar.ErrUnavailable
	}
	agentRunReply(w, a, "", v, e)
}
