package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

func WithVideoMaterials(service *media.VideoMaterialService) Option {
	return func(s *server) { s.videoMaterials = service }
}
func videoFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		authFailed(w, e)
	case errors.Is(e, content.ErrNotFound):
		respondError(w, 404, "video_not_found")
	case errors.Is(e, content.ErrConflict):
		respondError(w, 409, "video_changed")
	case errors.Is(e, content.ErrInvalid), errors.Is(e, media.ErrVideoInvalid):
		respondError(w, 400, "invalid_video")
	default:
		respondError(w, 503, "video_unavailable")
	}
}
func (s *server) videoActor(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, bool) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return a, d, false
	}
	if s.videoMaterials == nil || s.videoMaterials.Repo == nil {
		respondError(w, 503, "video_unavailable")
		return a, d, false
	}
	return a, d, true
}
func videoRespond(w http.ResponseWriter, r *http.Request, code int, data any, e error) {
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		videoFailure(w, e)
		return
	}
	respond(w, code, map[string]any{"data": data})
}
func (s *server) videoCapabilities(w http.ResponseWriter, r *http.Request) {
	if !momentNoQuery(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	caps := (*media.VideoMaterialService)(nil).Capabilities()
	if s.videoMaterials != nil {
		caps = s.videoMaterials.Capabilities()
	}
	respond(w, 200, map[string]any{"data": caps})
}
func (s *server) createVideoMaterial(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.videoActor(w, r)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	var in media.VideoMaterialInput
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	p, e := s.videoMaterials.Create(r.Context(), d, a, in)
	videoRespond(w, r, 201, p, e)
}
func videoPath(w http.ResponseWriter, r *http.Request, key string) bool {
	if !media.ValidPrivateImageID(r.PathValue(key)) {
		respondError(w, 400, "invalid_video_id")
		return false
	}
	return true
}
func (s *server) completeVideoMaterial(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.videoActor(w, r)
	if !ok || !momentNoQuery(w, r) || !videoPath(w, r, "videoID") {
		return
	}
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	if in.Revision < 1 {
		respondError(w, 400, "invalid_video_revision")
		return
	}
	v, e := s.videoMaterials.Complete(r.Context(), d, a, r.PathValue("videoID"), in.Revision)
	videoRespond(w, r, 202, v, e)
}
func (s *server) listVideoMaterials(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.videoActor(w, r)
	if !ok {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) > 1 || (len(q) == 1 && (len(q["cursor"]) != 1 || !media.ValidPrivateImageID(q.Get("cursor")))) {
		respondError(w, 400, "invalid_video_query")
		return
	}
	p, e := s.videoMaterials.List(r.Context(), d, a, q.Get("cursor"))
	videoRespond(w, r, 200, p, e)
}
func (s *server) getVideoMaterial(w http.ResponseWriter, r *http.Request) {
	s.readVideoMaterial(w, r, false)
}
func (s *server) getVideoMaterialOperation(w http.ResponseWriter, r *http.Request) {
	s.readVideoMaterial(w, r, true)
}
func (s *server) readVideoMaterial(w http.ResponseWriter, r *http.Request, operation bool) {
	a, d, ok := s.videoActor(w, r)
	key := "videoID"
	if operation {
		key = "operationID"
	}
	if !ok || !momentNoQuery(w, r) || !videoPath(w, r, key) {
		return
	}
	b, e := s.videoMaterials.Read(r.Context(), d, a, r.PathValue(key), operation, func(v media.VideoMaterialReceipt) ([]byte, error) { return json.Marshal(map[string]any{"data": v}) })
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		videoFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
func (s *server) readVideoMaterialContent(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.videoActor(w, r)
	if !ok || !momentNoQuery(w, r) || !videoPath(w, r, "videoID") {
		return
	}
	if r.Header.Get("Range") != "" {
		respondError(w, 400, "video_range_unavailable")
		return
	}
	b, e := s.videoMaterials.Content(r.Context(), d, a, r.PathValue("videoID"))
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		videoFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline; filename=private-video.mp4")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
func (s *server) deleteVideoMaterial(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.videoActor(w, r)
	if !ok || !videoPath(w, r, "videoID") {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	rev, parse := strconv.ParseInt(q.Get("revision"), 10, 64)
	if e != nil || parse != nil || rev < 1 || len(q) != 1 || len(q["revision"]) != 1 {
		respondError(w, 400, "invalid_video_revision")
		return
	}
	v, e := s.videoMaterials.Delete(r.Context(), d, a, r.PathValue("videoID"), rev)
	videoRespond(w, r, 202, v, e)
}
func (s *server) registerVideoMaterialRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/video-material-capabilities", s.videoCapabilities)
	mux.HandleFunc("POST /v1/me/video-materials", s.createVideoMaterial)
	mux.HandleFunc("GET /v1/me/video-materials", s.listVideoMaterials)
	mux.HandleFunc("GET /v1/me/video-materials/operations/{operationID}/receipt", s.getVideoMaterialOperation)
	mux.HandleFunc("GET /v1/me/video-materials/{videoID}", s.getVideoMaterial)
	mux.HandleFunc("GET /v1/me/video-materials/{videoID}/content", s.readVideoMaterialContent)
	mux.HandleFunc("POST /v1/me/video-materials/{videoID}/complete", s.completeVideoMaterial)
	mux.HandleFunc("DELETE /v1/me/video-materials/{videoID}", s.deleteVideoMaterial)
}
