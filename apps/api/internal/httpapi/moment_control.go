package httpapi

import (
	"encoding/base64"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"net/http"
	"time"
)

func momentControlFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, content.ErrMomentLocationsClientRequired):
		respond(w, 409, map[string]any{"error": map[string]string{"code": "moment_locations_client_required", "message": "请使用支持完整位置集合的编辑器修改这些位置。"}})
	case errors.Is(e, content.ErrMomentLegacyPhotoReselection):
		respond(w, 409, map[string]any{"error": map[string]string{"code": "legacy_moment_photo_reselection_required", "message": "这张旧版配图需要重新选择后才能修改公开动态；也可以添加正文并明确移除配图。"}})
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
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, content.MomentControlDigest(in)) || v.MomentID != in.MomentID || v.ExpectedRevision != in.ExpectedRevision || v.InputSHA256 != content.MomentControlInputHash(in.Input) || content.MomentControlInputHash(v.Input) != v.InputSHA256 || !content.MomentControlHex(v.SourceVersion) || v.Locations == nil || len(v.Locations) > content.MomentLocationMaxLinks || !content.MomentControlHex(v.LocationsSourceVersion) {
		respondError(w, 503, "moment_unavailable")
		return
	}
	count := 0
	if in.Media != nil {
		count = len(*in.Media)
	}
	if len(v.Materials) != count || v.PreservedMedia != nil && (in.Action != "UPDATE_PUBLIC" || in.Media != nil || !v.PreservedMedia.Valid(in.MomentID, time.Now().UTC()) || !v.PreservedMedia.CanReplace || v.PreservedMedia.Status != "READY") {
		respondError(w, 503, "moment_unavailable")
		return
	}
	for i, preview := range v.Materials {
		material := preview.Material
		if !mm.StageValid(material) || material.MediaID != (*in.Media)[i].MediaID || material.Version != (*in.Media)[i].MediaVersion || material.LocalScope != in.LocalScope || !material.RetainUntil.After(time.Now().UTC()) || len(preview.ThumbnailBase64) > base64.StdEncoding.EncodedLen(mm.MaxThumbnailBytes) {
			respondError(w, 503, "moment_unavailable")
			return
		}
		thumbnail, e := base64.StdEncoding.Strict().DecodeString(preview.ThumbnailBase64)
		if e != nil || len(thumbnail) != material.ThumbnailBytes || mm.DigestBytes(thumbnail) != material.ThumbnailSHA {
			respondError(w, 503, "moment_unavailable")
			return
		}
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
