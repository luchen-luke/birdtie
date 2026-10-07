package httpapi

import (
	"errors"
	cg "github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"io"
	"net/http"
)

func communityInterestFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, cg.ErrInterestInvalid):
		respondError(w, 400, "请重新检查社群兴趣声明")
	case errors.Is(e, cg.ErrInterestDenied):
		respondError(w, 403, "当前身份不允许操作此声明")
	case errors.Is(e, cg.ErrInterestChanged):
		respondError(w, 409, "具体版本或预览已变化，请重新检查")
	default:
		respondError(w, 503, "社群兴趣声明暂不可用")
	}
}
func (s *server) getOwnCommunityInterests(w http.ResponseWriter, r *http.Request) {
	s.readOwnCommunityInterests(w, r, false)
}
func (s *server) getOwnCommunityInterestOptions(w http.ResponseWriter, r *http.Request) {
	s.readOwnCommunityInterests(w, r, true)
}
func (s *server) readOwnCommunityInterests(w http.ResponseWriter, r *http.Request, options bool) {
	a, ok := s.privateProfileAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) > 0 {
			communityInterestFailure(w, cg.ErrInterestInvalid)
			return
		}
	}
	store, ok := s.privateProfiles.(cg.CommunityInterestStore)
	if !ok || store == nil {
		communityInterestFailure(w, cg.ErrInterestUnavailable)
		return
	}
	out, e := store.ReadOwnCommunityInterests(r.Context(), a, options)
	if e != nil {
		communityInterestFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
func (s *server) previewOwnCommunityInterest(w http.ResponseWriter, r *http.Request) {
	s.mutateOwnCommunityInterest(w, r, false)
}
func (s *server) approveOwnCommunityInterest(w http.ResponseWriter, r *http.Request) {
	s.mutateOwnCommunityInterest(w, r, true)
}
func (s *server) mutateOwnCommunityInterest(w http.ResponseWriter, r *http.Request, approve bool) {
	a, ok := s.privateProfileAccess(w, r)
	if !ok {
		return
	}
	if r.Body == nil {
		communityInterestFailure(w, cg.ErrInterestInvalid)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 32769))
	if e != nil {
		communityInterestFailure(w, cg.ErrInterestInvalid)
		return
	}
	in, token, e := cg.DecodeCommunityInterestBody(raw, approve)
	if e != nil {
		communityInterestFailure(w, e)
		return
	}
	store, ok := s.privateProfiles.(cg.CommunityInterestStore)
	if !ok || store == nil {
		communityInterestFailure(w, cg.ErrInterestUnavailable)
		return
	}
	if approve {
		out, e := store.ApproveOwnCommunityInterest(r.Context(), a, token)
		if e != nil {
			communityInterestFailure(w, e)
			return
		}
		respond(w, 200, map[string]any{"data": out})
		return
	}
	out, e := store.PreviewOwnCommunityInterest(r.Context(), a, in)
	if e != nil {
		communityInterestFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
