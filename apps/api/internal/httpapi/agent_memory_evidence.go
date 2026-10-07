package httpapi

import (
	"io"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

func (s *server) memoryEvidenceAccess(w http.ResponseWriter, r *http.Request) (agentmemory.Access, string, bool) {
	a, ok := s.memoryAccess(w, r)
	if !ok {
		return agentmemory.Access{}, "", false
	}
	if s.memoryEvidence == nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return agentmemory.Access{}, "", false
	}
	id, err := agentmemory.NormalizeMemoryID(r.PathValue("memoryID"))
	if err != nil {
		memoryFailure(w, err)
		return agentmemory.Access{}, "", false
	}
	return a, id, true
}
func validOwnEvidence(a agentmemory.Access, memoryID, id string, e agentmemory.Evidence) bool {
	return agentmemory.ValidateEvidence(e) == nil && e.OwnerType == actorref.Person && e.OwnerID == a.WorkspacePrincipal.ID && e.MemoryID == memoryID && e.ID == id
}
func (s *server) putOwnMemoryEvidence(w http.ResponseWriter, r *http.Request) {
	a, mid, ok := s.memoryEvidenceAccess(w, r)
	if !ok {
		return
	}
	id, err := agentmemory.NormalizeEvidenceID(r.PathValue("evidenceID"))
	if err != nil {
		memoryFailure(w, err)
		return
	}
	body, ok := memoryBody(w, r)
	if !ok {
		return
	}
	input, err := agentmemory.DecodeEvidenceReferenceInput(body)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	e, err := s.memoryEvidence.PutOwnMemoryEvidence(r.Context(), a, mid, id, input)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	if !validOwnEvidence(a, mid, id, e) || e.Status != agentmemory.EvidenceCurrent || e.MemoryVersion != input.ExpectedMemoryVersion ||
		e.Source == nil || e.Source.Type != input.SourceType || e.Source.ID != input.SourceID {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": e})
}
func (s *server) removeOwnMemoryEvidence(w http.ResponseWriter, r *http.Request) {
	a, mid, ok := s.memoryEvidenceAccess(w, r)
	if !ok {
		return
	}
	id, err := agentmemory.NormalizeEvidenceID(r.PathValue("evidenceID"))
	if err != nil {
		memoryFailure(w, err)
		return
	}
	body, ok := memoryBody(w, r)
	if !ok {
		return
	}
	input, err := agentmemory.DecodeEvidenceDetachInput(body)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	e, err := s.memoryEvidence.RemoveOwnMemoryEvidence(r.Context(), a, mid, id, input.ExpectedVersion)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	if !validOwnEvidence(a, mid, id, e) || e.Status != agentmemory.EvidenceRemoved || (e.Version != input.ExpectedVersion && e.Version != input.ExpectedVersion+1) {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": e})
}
func (s *server) readOwnMemoryProvenance(w http.ResponseWriter, r *http.Request) {
	a, mid, ok := s.memoryEvidenceAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			memoryFailure(w, agentmemory.ErrInvalid)
			return
		}
	}
	p, err := s.memoryEvidence.ReadOwnMemoryProvenance(r.Context(), a, mid)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	if agentmemory.ValidateProvenance(p) != nil || p.MemoryID != mid {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	var agentID string
	for _, e := range p.Evidence {
		if !validOwnEvidence(a, mid, e.ID, e) || e.Status != agentmemory.EvidenceCurrent || e.MemoryVersion != p.MemoryVersion || (agentID != "" && agentID != e.AgentID) {
			memoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		agentID = e.AgentID
	}
	respond(w, http.StatusOK, map[string]any{"data": p})
}
