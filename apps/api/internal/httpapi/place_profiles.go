package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"

	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
)

func (s *server) placeProfileStore(w http.ResponseWriter) (pp.Store, bool) {
	store, ok := s.catalog.(pp.Store)
	if !ok || store == nil {
		respondError(w, 503, "地点语义资料暂不可用")
		return nil, false
	}
	return store, true
}
func placeProfileError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, pp.ErrInvalid):
		respondError(w, 400, "地点语义资料格式无效")
	case errors.Is(e, pp.ErrDenied):
		respondError(w, 403, "当前会话或城市编辑权限无效")
	case errors.Is(e, pp.ErrConflict):
		respondError(w, 409, "资料版本已变化，请重新读取")
	case errors.Is(e, pp.ErrNotFound):
		respondError(w, 404, "没有仍公开有效的地点语义资料")
	default:
		respondError(w, 503, "地点语义资料暂不可用")
	}
}
func placeProfileBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	m, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || m != "application/json" {
		respondError(w, 400, "请使用JSON格式提交地点资料")
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, pp.MaxInputBytes))
	if e != nil {
		respondError(w, 400, "地点资料过大或无法读取")
		return nil, false
	}
	return raw, true
}
func (s *server) placeProfileActor(w http.ResponseWriter, r *http.Request) (pp.Access, bool) {
	if r.URL.RawQuery != "" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		placeProfileError(w, pp.ErrInvalid)
		return pp.Access{}, false
	}
	actor, digest, e := s.actor(r, true)
	if authFailed(w, e) {
		return pp.Access{}, false
	}
	a := pp.Access{ActorID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}
	if pp.ValidateAccess(a) != nil {
		placeProfileError(w, pp.ErrDenied)
		return pp.Access{}, false
	}
	return a, true
}

func (s *server) getPublicPlaceProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	id := r.PathValue("placeID")
	if !pp.ValidID(id) {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	store, ok := s.placeProfileStore(w)
	if !ok {
		return
	}
	p, e := store.GetPublicPlaceSemanticProfile(r.Context(), id)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	if p.PlaceID != id || p.CheckedAt.IsZero() || pp.ValidatePublic(p, p.CheckedAt) != nil {
		placeProfileError(w, pp.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": p, "claims": p.Claims(), "source": "PUBLIC_REVIEWED_DECLARATION", "modelAuthorization": "UNAVAILABLE"})
}
func (s *server) submitPlaceProfileCandidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := s.placeProfileActor(w, r)
	if !ok {
		return
	}
	store, ok := s.placeProfileStore(w)
	if !ok {
		return
	}
	city, id := r.PathValue("cityID"), r.PathValue("placeID")
	if len(city) < 1 || len(city) > 80 || !pp.ValidID(id) {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	raw, ok := placeProfileBody(w, r)
	if !ok {
		return
	}
	in, e := pp.DecodeSubmit(raw)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	c, created, e := store.SubmitPlaceSemanticCandidate(r.Context(), a, city, id, in)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	if c.PlaceID != id || c.CityID != city || !pp.ValidID(c.ID) || c.Version != 1 || (c.Status != "pending" && c.Status != "approved" && c.Status != "rejected") {
		placeProfileError(w, pp.ErrUnavailable)
		return
	}
	status := 200
	if created {
		status = 201
	}
	publication := "REQUIRES_INDEPENDENT_REVIEW"
	if c.Status == "approved" {
		publication = "APPROVED_CANDIDATE_RECEIPT"
	}
	if c.Status == "rejected" {
		publication = "REJECTED_CANDIDATE_RECEIPT"
	}
	respond(w, status, map[string]any{"data": c, "created": created, "publication": publication})
}
func (s *server) listPlaceProfileCandidates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := s.placeProfileActor(w, r)
	if !ok {
		return
	}
	store, ok := s.placeProfileStore(w)
	if !ok {
		return
	}
	city := r.PathValue("cityID")
	if len(city) < 1 || len(city) > 80 {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	cs, e := store.ListPlaceSemanticCandidates(r.Context(), a, city)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	if cs == nil {
		cs = []pp.Candidate{}
	}
	if len(cs) > 100 {
		placeProfileError(w, pp.ErrUnavailable)
		return
	}
	for _, c := range cs {
		if c.CityID != city || c.Status != "pending" || !pp.ValidID(c.PlaceID) || !pp.ValidID(c.ID) {
			placeProfileError(w, pp.ErrUnavailable)
			return
		}
	}
	respond(w, 200, map[string]any{"data": cs})
}
func (s *server) reviewPlaceProfileCandidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := s.placeProfileActor(w, r)
	if !ok {
		return
	}
	store, ok := s.placeProfileStore(w)
	if !ok {
		return
	}
	id := r.PathValue("candidateID")
	if !pp.ValidID(id) {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	raw, ok := placeProfileBody(w, r)
	if !ok {
		return
	}
	in, e := pp.DecodeReview(raw)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	c, e := store.ReviewPlaceSemanticCandidate(r.Context(), a, id, in)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	expected := "approved"
	if in.Decision == "reject" {
		expected = "rejected"
	}
	if c.ID != id || c.Status != expected || c.Version != in.CandidateVersion || c.ExpectedVersion != in.ExpectedVersion {
		placeProfileError(w, pp.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": c, "review": "EXPLICIT_VERSION_DECISION"})
}
func (s *server) withdrawPlaceProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a, ok := s.placeProfileActor(w, r)
	if !ok {
		return
	}
	store, ok := s.placeProfileStore(w)
	if !ok {
		return
	}
	id := r.PathValue("placeID")
	if !pp.ValidID(id) {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	raw, ok := placeProfileBody(w, r)
	if !ok {
		return
	}
	in, e := pp.DecodeWithdraw(raw)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	out, e := store.WithdrawPlaceSemanticProfile(r.Context(), a, id, in)
	if e != nil {
		placeProfileError(w, e)
		return
	}
	if out.PlaceID != id || out.State != "withdrawn" || out.Version != in.ExpectedVersion+1 {
		placeProfileError(w, pp.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
