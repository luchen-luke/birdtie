package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"net/http"
)

func momentControlFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, content.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, content.ErrConflict):
		respondError(w, 409, "moment_changed")
	case errors.Is(e, content.ErrInvalid):
		respondError(w, 400, "invalid_moment_control")
	case errors.Is(e, content.ErrUnavailable):
		respondError(w, 503, "moment_unavailable")
	default:
		humanControlFailure(w, e)
	}
}
func (s *server) momentControlPort(w http.ResponseWriter) (content.MomentControlStore, bool) {
	p, ok := s.content.(content.MomentControlStore)
	if !ok || p == nil {
		respondError(w, 503, "moment_unavailable")
		return nil, false
	}
	return p, true
}
func (s *server) previewMomentControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	p, ok := s.momentControlPort(w)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeMomentControl(raw, false)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	v, e := p.PreviewMomentControl(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, content.MomentControlDigest(in)) || v.MomentID != in.MomentID || v.ExpectedRevision != in.ExpectedRevision || v.InputSHA256 != content.MomentControlInputHash(in.Input) || content.MomentControlInputHash(v.Input) != v.InputSHA256 || !content.MomentControlHex(v.SourceVersion) || v.PreservedPlaceUnavailable && (in.Action != "UPDATE" || in.Input == nil || in.Input.LocationPrecision == "place") {
		respondError(w, 503, "moment_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitMomentControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	p, ok := s.momentControlPort(w)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeMomentControl(raw, true)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_moment_control")
		return
	}
	v, e := p.CommitMomentControl(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !content.ValidMomentControlReceipt(v, in.OperationID) || v.Status == "SUCCEEDED" && (v.Action != in.Action || v.PayloadDigest != content.MomentControlDigest(in) || *v.ExpectedRevision != in.ExpectedRevision || in.Action != "CREATE" && v.MomentID != in.MomentID) {
		respondError(w, 503, "moment_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readMomentControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyMomentControl(w, r, false)
}
func (s *server) stopMomentControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyMomentControl(w, r, true)
}
func (s *server) historyMomentControl(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.momentControlPort(w)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	var v content.MomentControlReceipt
	var e error
	if stop {
		v, e = p.StopMomentControlOperation(r.Context(), d, a, op)
	} else {
		v, e = p.ReadMomentControlOperation(r.Context(), d, a, op)
	}
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !content.ValidMomentControlReceipt(v, op) {
		respondError(w, 503, "moment_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) registerMomentControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/me/moment-controls/previews", s.previewMomentControl)
	mux.HandleFunc("PUT /v1/me/moment-controls/operations/{operationID}", s.commitMomentControl)
	mux.HandleFunc("GET /v1/me/moment-controls/operations/{operationID}", s.readMomentControlOperation)
	mux.HandleFunc("POST /v1/me/moment-controls/operations/{operationID}/stop", s.stopMomentControlOperation)
}
