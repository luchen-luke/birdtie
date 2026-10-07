package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

// Local interface keeps the original094 receipt contract without a circular
// dependency from agentmemory back to agentmemorycorrection.
type memoryHumanRejectStore interface {
	RejectOwnMemory(context.Context, agentprofile.PrivateAccess, string, agentmemory.RejectInput) (mc.Receipt, error)
}

func (s *server) memoryAPIPath(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.ForceQuery {
		memoryFailure(w, agentmemory.ErrInvalid)
		return agentprofile.PrivateAccess{}, "", false
	}
	a, ok := s.memoryAccess(w, r)
	if !ok {
		return a, "", false
	}
	id, e := agentmemory.NormalizeMemoryID(r.PathValue("memoryID"))
	if e != nil || id != r.PathValue("memoryID") {
		memoryFailure(w, agentmemory.ErrInvalid)
		return a, "", false
	}
	return a, id, true
}
func (s *server) getOwnAgentMemoryDetail(w http.ResponseWriter, r *http.Request) {
	a, id, ok := s.memoryAPIPath(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			memoryFailure(w, agentmemory.ErrInvalid)
			return
		}
	}
	store, ok := s.memories.(agentmemory.CurrentHumanStore)
	if !ok || store == nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	p, e := store.ReadOwnMemoryDetail(r.Context(), a, id)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if agentmemory.ValidateDetail(p, p.ObservedAt) != nil || p.Target.ID != id || !p.Owner.Equal(a.WorkspacePrincipal) {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	if _, e = agentmemory.DetailNativeProof(p, a); e != nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	// Encode exactly the reviewed snapshot before the final native read. Do not
	// stream private contents before current owner/record/control/TTL is checked.
	fields, e := acb.MemoryDetailFieldEvidence(p)
	if e != nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	// Add only metadata about this same reviewed record. The original proof and
	// final native Revalidate still bind the actual Memory, actor and session.
	raw, e := json.Marshal(map[string]any{"data": struct {
		agentmemory.DetailProjection
		FieldEvidenceSet *acb.FieldEvidenceSet `json:"fieldEvidenceSet"`
	}{p, fields}})
	if e != nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	if e = store.RevalidateOwnMemoryDetail(r.Context(), a, p); e != nil {
		memoryFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) rejectOwnAgentMemory(w http.ResponseWriter, r *http.Request) {
	a, id, ok := s.memoryAPIPath(w, r)
	if !ok {
		return
	}
	store, ok := s.memories.(memoryHumanRejectStore)
	if !ok || store == nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	raw, ok := memoryBody(w, r)
	if !ok {
		return
	}
	in, e := agentmemory.DecodeRejectInput(raw)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	receipt, e := store.RejectOwnMemory(r.Context(), a, id, in)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if mc.ValidateReceipt(receipt) != nil || !receipt.Owner.Equal(a.WorkspacePrincipal) || receipt.ID != in.OperationID || receipt.Target.Kind != "MEMORY" || receipt.Target.ID != id || receipt.Action != "REJECT" || receipt.PlanDigest != in.PlanDigest || receipt.State != "COMMITTED" {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	// Existing human operation receipt; no duplicate Memory state or permission.
	respond(w, http.StatusOK, receipt)
}
