package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
)

func (s *server) privateMomentVideoGateway(w http.ResponseWriter) (media.HumanPrivateVideoStore, bool) {
	g, ok := s.content.(media.HumanPrivateVideoStore)
	if !ok || g == nil || (reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil()) {
		respondError(w, 503, "private_video_unavailable")
		return nil, false
	}
	return g, true
}
func (s *server) readPrivateMomentVideos(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	g, ok := s.privateMomentVideoGateway(w)
	id, mid := r.PathValue("videoID"), r.PathValue("momentID")
	if !ok || !momentNoQuery(w, r) {
		return
	}
	if !media.ValidPrivateImageID(mid) || (id != "" && !media.ValidPrivateImageID(id)) {
		respondError(w, 400, "invalid_private_video_id")
		return
	}
	part := ""
	if r.Pattern == "GET /v1/me/moments/{momentID}/videos/{videoID}/content" {
		part = "content"
	}
	if r.Pattern == "GET /v1/me/moments/{momentID}/videos/{videoID}/poster" {
		part = "poster"
	}
	if r.Header.Get("Range") != "" {
		respondError(w, 400, "private_video_range_unavailable")
		return
	}
	raw, e := g.ReadHumanPrivateVideos(r.Context(), d, a, mid, id, part, func(v []media.PrivateVideoReceipt, b []byte) ([]byte, error) {
		if part != "" {
			return b, nil
		}
		if id != "" {
			if len(v) != 1 {
				return nil, http.ErrMissingFile
			}
			return json.Marshal(map[string]any{"data": v[0]})
		}
		return json.Marshal(map[string]any{"data": map[string]any{"items": v}})
	})
	if e != nil {
		privateImageFailure(w, e)
		return
	}
	if part != "" {
		defer clear(raw)
	}
	if !privateImageResponseCurrent(w, r) {
		return
	}
	mime := "application/json; charset=utf-8"
	if part == "content" {
		mime = "video/mp4"
		w.Header().Set("Content-Disposition", "inline; filename=private-moment-video.mp4")
	}
	if part == "poster" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (s *server) deletePrivateMomentVideo(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	g, ok := s.privateMomentVideoGateway(w)
	if !ok {
		return
	}
	mid, id := r.PathValue("momentID"), r.PathValue("videoID")
	q, e := url.ParseQuery(r.URL.RawQuery)
	rev, pe := strconv.ParseInt(q.Get("revision"), 10, 64)
	if !media.ValidPrivateImageID(mid) || !media.ValidPrivateImageID(id) || e != nil || pe != nil || rev < 1 || len(q) != 1 || len(q["revision"]) != 1 {
		respondError(w, 400, "invalid_private_video_revision")
		return
	}
	v, e := g.DeleteHumanPrivateVideo(r.Context(), d, a, mid, id, rev)
	if e != nil {
		privateImageFailure(w, e)
		return
	}
	if !privateImageResponseCurrent(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) registerPrivateMomentVideoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/moments/{momentID}/videos", s.readPrivateMomentVideos)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/videos/{videoID}", s.readPrivateMomentVideos)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/videos/{videoID}/content", s.readPrivateMomentVideos)
	mux.HandleFunc("GET /v1/me/moments/{momentID}/videos/{videoID}/poster", s.readPrivateMomentVideos)
	mux.HandleFunc("DELETE /v1/me/moments/{momentID}/videos/{videoID}", s.deletePrivateMomentVideo)
}
