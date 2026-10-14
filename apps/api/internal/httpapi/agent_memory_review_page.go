package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	review "github.com/birdtie/birdtie/apps/api/internal/agentmemoryreview"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (s *server) memoryPageAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, string, bool) {
	fail := func() (agentprofile.PrivateAccess, string, bool) {
		memoryFailure(w, agentmemory.ErrInvalid)
		return agentprofile.PrivateAccess{}, "", false
	}
	if r.URL.ForceQuery && r.URL.RawQuery == "" || len(r.URL.RawQuery) > review.MaxCursor+16 {
		return fail()
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) > 1 {
		return fail()
	}
	cursor := ""
	for k, v := range q {
		if k != "cursor" || len(v) != 1 || !review.CursorShape(v[0]) {
			return fail()
		}
		cursor = v[0]
	}
	if r.Body != nil {
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
		if e != nil || len(body) > 0 {
			return fail()
		}
	}
	// Query has been validated as the single opaque selector. Reuse the original
	// auth/workspace checks on a clone; never mutate a caller request or headers.
	copy := r.Clone(r.Context())
	u := *r.URL
	u.RawQuery = ""
	u.ForceQuery = false
	copy.URL = &u
	a, ok := s.memoryAccess(w, copy)
	return a, cursor, ok
}
func (s *server) listOwnAgentMemoryPage(w http.ResponseWriter, r *http.Request) {
	a, cursor, ok := s.memoryPageAccess(w, r)
	if !ok {
		return
	}
	port, ok := s.memories.(review.MemoryPager)
	if !ok || nilHumanSelfReviewPort(port) {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	out, e := port.ReadOwnMemoryPage(r.Context(), a, cursor)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if review.Validate(out, a.WorkspacePrincipal.ID, review.Memories, time.Now()) != nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	seen := map[string]bool{}
	for _, item := range out.Items {
		if !validOwnMemory(a, item) || item.AgentID != out.AgentID || item.Status == agentmemory.StatusDeleted || seen[item.ID] {
			memoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		seen[item.ID] = true
	}
	respond(w, 200, map[string]any{"data": out})
}
func (s *server) listOwnAgentMemoryCandidatePage(w http.ResponseWriter, r *http.Request) {
	a, cursor, ok := s.memoryPageAccess(w, r)
	if !ok {
		return
	}
	port, ok := s.memoryCandidates.(review.CandidatePager)
	if !ok || nilHumanSelfReviewPort(port) {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	out, e := port.ReadOwnCandidatePage(r.Context(), a, cursor)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if review.Validate(out, a.WorkspacePrincipal.ID, review.Candidates, time.Now()) != nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	seen := map[string]bool{}
	for _, item := range out.Items {
		if !candidateRecordValid(a, item) || item.AgentID != out.AgentID || seen[item.ID] {
			memoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		seen[item.ID] = true
	}
	respond(w, 200, map[string]any{"data": out})
}
