package httpapi

import (
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// These routes are human self-management. Memory contents do not certify
// identity, membership or consent, and are not a model ContextBundle.
func (s *server) memoryAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "personal_memory_required")
		return agentprofile.PrivateAccess{}, false
	}
	if s.access == nil || s.memories == nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, err := s.actor(r, true)
	if errors.Is(err, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return agentprofile.PrivateAccess{}, false
	}
	if err != nil {
		memoryFailure(w, err)
		return agentprofile.PrivateAccess{}, false
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || principal.Type != actorref.Person {
		respondError(w, http.StatusForbidden, "personal_memory_required")
		return agentprofile.PrivateAccess{}, false
	}
	if r.URL.RawQuery != "" {
		respondError(w, http.StatusBadRequest, "memory_query_not_supported")
		return agentprofile.PrivateAccess{}, false
	}
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		memoryFailure(w, agentmemory.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	return access, true
}

func memoryFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentmemory.ErrInvalid):
		respondError(w, http.StatusBadRequest, "invalid_agent_memory")
	case errors.Is(err, agentmemory.ErrForbidden):
		respondError(w, http.StatusForbidden, "agent_memory_forbidden")
	case errors.Is(err, agentmemory.ErrNotFound):
		respondError(w, http.StatusNotFound, "agent_memory_not_found")
	case errors.Is(err, agentmemory.ErrConflict):
		respondError(w, http.StatusConflict, "agent_memory_version_conflict")
	default:
		log.Printf("request_id=%s agent_memory_error category=unavailable", w.Header().Get("X-Request-ID"))
		respondError(w, http.StatusServiceUnavailable, "agent_memory_unavailable")
	}
}

func validOwnMemory(access agentprofile.PrivateAccess, record agentmemory.Record) bool {
	return agentmemory.ValidateRecord(record) == nil && record.OwnerType == actorref.Person && record.OwnerID == access.WorkspacePrincipal.ID
}

func memoryResponse(w http.ResponseWriter, access agentprofile.PrivateAccess, record agentmemory.Record) {
	if !validOwnMemory(access, record) {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": record})
}

func (s *server) listOwnAgentMemories(w http.ResponseWriter, r *http.Request) {
	access, ok := s.memoryAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			respondError(w, http.StatusBadRequest, "memory_get_body_not_supported")
			return
		}
	}
	records, err := s.memories.ReadOwnMemories(r.Context(), access)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	var agentID string
	for _, record := range records {
		if !validOwnMemory(access, record) || record.Status == agentmemory.StatusDeleted ||
			(agentID != "" && record.AgentID != agentID) {
			memoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		agentID = record.AgentID
	}
	respond(w, http.StatusOK, map[string]any{"data": agentmemory.SortRecords(records)})
}

func memoryBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return nil, false
	}
	if r.Body == nil {
		memoryFailure(w, agentmemory.ErrInvalid)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, agentmemory.MaxBodyBytes+1))
	if err != nil || len(body) > agentmemory.MaxBodyBytes {
		respondError(w, http.StatusRequestEntityTooLarge, "memory_body_too_large")
		return nil, false
	}
	return body, true
}

func (s *server) putOwnAgentMemory(w http.ResponseWriter, r *http.Request) {
	access, ok := s.memoryAccess(w, r)
	if !ok {
		return
	}
	id, err := agentmemory.NormalizeMemoryID(r.PathValue("memoryID"))
	if err != nil {
		memoryFailure(w, err)
		return
	}
	body, ok := memoryBody(w, r)
	if !ok {
		return
	}
	input, err := agentmemory.DecodePutInput(body)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	record, err := s.memories.PutOwnMemory(r.Context(), access, id, input)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	if record.ID != id || record.SourceType != agentmemory.SourceExplicit || record.Status != agentmemory.StatusActive {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	memoryResponse(w, access, record)
}

func (s *server) deleteOwnAgentMemory(w http.ResponseWriter, r *http.Request) {
	access, ok := s.memoryAccess(w, r)
	if !ok {
		return
	}
	id, err := agentmemory.NormalizeMemoryID(r.PathValue("memoryID"))
	if err != nil {
		memoryFailure(w, err)
		return
	}
	body, ok := memoryBody(w, r)
	if !ok {
		return
	}
	input, err := agentmemory.DecodeDeleteInput(body)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	record, err := s.memories.DeleteOwnMemory(r.Context(), access, id, input.ExpectedVersion)
	if err != nil {
		memoryFailure(w, err)
		return
	}
	if record.ID != id || record.Status != agentmemory.StatusDeleted {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	memoryResponse(w, access, record)
}
