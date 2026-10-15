package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"reflect"
	"strconv"
	"time"

	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

func (s *server) momentMediaVideoPort(w http.ResponseWriter) (mm.VideoStore, bool) {
	p, ok := s.content.(mm.VideoStore)
	if !ok || p == nil || (reflect.ValueOf(p).Kind() == reflect.Pointer && reflect.ValueOf(p).IsNil()) {
		respondError(w, 503, "public_video_unavailable")
		return nil, false
	}
	return p, true
}
func (s *server) prepareMomentMediaVideo(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "operationID") {
		return
	}
	p, ok := s.momentMediaVideoPort(w)
	if !ok {
		return
	}
	if !p.MomentMediaVideoAvailable() {
		respondError(w, 503, "public_video_unavailable")
		return
	}
	ctx, release, e := mm.TryVideoWork(r.Context())
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	var wire struct {
		OperationID   string `json:"operationId"`
		LocalScope    string `json:"localScope"`
		Purpose       string `json:"purpose"`
		ContentType   string `json:"contentType"`
		ContentBase64 string `json:"contentBase64"`
	}
	if !mediaJSONBody(w, r, &wire, int64((mm.MaxVideoBytes+2)/3*4+2048)) {
		return
	}
	if wire.OperationID != r.PathValue("operationID") || wire.Purpose != mm.VideoPurpose || wire.ContentType != "video/mp4" {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	body, e := base64.StdEncoding.Strict().DecodeString(wire.ContentBase64)
	if e != nil || len(body) > mm.MaxVideoBytes {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	defer clear(body)
	v, e := p.PrepareMomentMediaVideo(r.Context(), mediaAccess(a, d), mm.VideoStageInput{OperationID: wire.OperationID, LocalScope: wire.LocalScope, Purpose: wire.Purpose, ContentType: wire.ContentType, Content: body})
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if v.OperationID != wire.OperationID || v.Status != "STOPPED" && (v.Status != "SUCCEEDED" || v.Material == nil || !mm.StageValid(*v.Material) || v.Material.Kind != "VIDEO") {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) getMomentVideoStageContent(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "mediaID", "mediaVersion", "variant") || !humanControlNoBody(w, r) {
		return
	}
	n, e := strconv.ParseInt(r.PathValue("mediaVersion"), 10, 64)
	if e != nil || strconv.FormatInt(n, 10) != r.PathValue("mediaVersion") {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	p, ok := s.momentMediaVideoPort(w)
	if !ok {
		return
	}
	variant := r.PathValue("variant")
	mime := "image/jpeg"
	ctx, release, e := mm.TryVideoWork(r.Context())
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	defer release()
	r = r.WithContext(ctx)
	if variant == "video" {
		mime = "video/mp4"
	}
	b, e := p.ReadMomentVideoStageContent(r.Context(), mediaAccess(a, d), r.PathValue("mediaID"), n, variant, func(meta mm.Stage, b []byte) ([]byte, error) {
		count, sha := meta.DisplayBytes, meta.DisplaySHA
		if variant == "thumbnail" {
			count, sha = meta.ThumbnailBytes, meta.ThumbnailSHA
		} else if variant == "video" {
			if meta.Video == nil {
				return nil, mm.ErrChanged
			}
			count, sha = meta.Video.Bytes, meta.Video.SHA256
		}
		if len(b) != count || mm.DigestBytes(b) != sha {
			return nil, mm.ErrChanged
		}
		return b, nil
	})
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
func (s *server) momentMediaVideoCapabilities(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r) || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.momentMediaVideoPort(w)
	if !ok {
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": map[string]any{"available": p.MomentMediaVideoAvailable(), "purpose": mm.VideoPurpose, "mimeType": "video/mp4", "maxBytes": mm.MaxVideoBytes, "maxDurationMs": mm.MaxVideoDurationMS, "validationKind": mm.VideoValidation, "antivirusScan": "NOT_RUN", "privacyRisk": "UNKNOWN"}}, nil, false)
}
func (s *server) registerMomentMediaVideoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/me/moment-media-video-materials/operations/{operationID}", s.prepareMomentMediaVideo)
	mux.HandleFunc("GET /v1/me/moment-media/video-capabilities", s.momentMediaVideoCapabilities)
	mux.HandleFunc("GET /v1/me/moment-media-materials/{mediaID}/{mediaVersion}/{variant}", s.getMomentVideoStageContent)
}
