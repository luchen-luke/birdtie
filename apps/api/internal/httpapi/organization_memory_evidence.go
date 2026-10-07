package httpapi

import (
	"io"
	"math"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationevidence"
)

func organizationEvidencePath(r *http.Request, name string) (string, error) {
	id := r.PathValue(name)
	normalized, e := agentmemory.NormalizeMemoryID(id)
	if e != nil || normalized != id {
		return "", agentmemory.ErrInvalid
	}
	return id, nil
}
func (s *server) putOrganizationMemoryEvidence(w http.ResponseWriter, r *http.Request) {
	a, authority, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return
	}
	store, ok := s.catalog.(agentorganizationevidence.Store)
	if !ok {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	mid, e := organizationEvidencePath(r, "memoryID")
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	eid, e := organizationEvidencePath(r, "evidenceID")
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	in, e := agentorganizationevidence.DecodeReference(raw)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	rec, e := store.PutOrganizationMemoryEvidence(r.Context(), a, mid, eid, in)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationevidence.ValidateEvidence(rec) != nil || rec.ID != eid || rec.MemoryID != mid || rec.MemoryVersion != in.ExpectedMemoryVersion || rec.Status != agentmemory.EvidenceCurrent || rec.Source.Type != in.SourceType || rec.Source.ID != in.SourceID {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	if _, e = authority.ValidateOrganizationMemoryAccess(r.Context(), a); e != nil {
		orgMemoryFailure(w, e)
		return
	}
	// Re-read the actual current provenance before releasing a source address.
	// A successful prior write is not permission to release a now withdrawn URL/ID.
	p, e := store.ReadOrganizationMemoryProvenance(r.Context(), a, mid)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationevidence.ValidateProvenance(p, a.OrganizationID, mid) != nil || p.MemoryVersion != in.ExpectedMemoryVersion {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	found := false
	for _, current := range p.Evidence {
		if current.ID == eid && current.AgentID == rec.AgentID && current.OwnerID == rec.OwnerID && current.Version == rec.Version && current.Source != nil && *current.Source == *rec.Source {
			found = true
			rec = current
			break
		}
	}
	if !found || r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrConflict)
		return
	}
	respond(w, 200, map[string]any{"data": rec})
}
func (s *server) removeOrganizationMemoryEvidence(w http.ResponseWriter, r *http.Request) {
	a, authority, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return
	}
	store, ok := s.catalog.(agentorganizationevidence.Store)
	if !ok {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	mid, e := organizationEvidencePath(r, "memoryID")
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	eid, e := organizationEvidencePath(r, "evidenceID")
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	in, e := agentmemory.DecodeDeleteInput(raw)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	rec, e := store.RemoveOrganizationMemoryEvidence(r.Context(), a, mid, eid, in.ExpectedVersion)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if _, e = authority.ValidateOrganizationMemoryAccess(r.Context(), a); e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationevidence.ValidateEvidence(rec) != nil || rec.ID != eid || rec.MemoryID != mid || rec.Status != agentmemory.EvidenceRemoved ||
		(rec.Version != in.ExpectedVersion && (in.ExpectedVersion == math.MaxInt64 || rec.Version != in.ExpectedVersion+1)) || r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": rec})
}
func (s *server) readOrganizationMemoryProvenance(w http.ResponseWriter, r *http.Request) {
	a, authority, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return
	}
	store, ok := s.catalog.(agentorganizationevidence.Store)
	if !ok {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	mid, e := organizationEvidencePath(r, "memoryID")
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) > 0 {
			orgMemoryFailure(w, agentmemory.ErrInvalid)
			return
		}
	}
	p, e := store.ReadOrganizationMemoryProvenance(r.Context(), a, mid)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationevidence.ValidateProvenance(p, a.OrganizationID, mid) != nil || r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	if _, e = authority.ValidateOrganizationMemoryAccess(r.Context(), a); e != nil {
		orgMemoryFailure(w, e)
		return
	}
	// Final native read resolves source versions and session at the same snapshot.
	final, e := store.ReadOrganizationMemoryProvenance(r.Context(), a, mid)
	if e != nil {
		orgMemoryFailure(w, e)
		return
	}
	if agentorganizationevidence.ValidateProvenance(final, a.OrganizationID, mid) != nil || final.AgentID != p.AgentID || final.OrganizationAccountID != p.OrganizationAccountID || final.MemoryVersion != p.MemoryVersion || r.Context().Err() != nil {
		orgMemoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	p = final
	respond(w, 200, map[string]any{"data": p})
}
