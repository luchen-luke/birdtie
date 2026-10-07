package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

func (s *server) privateImageGateway(w http.ResponseWriter) (media.HumanPrivateImageStore, bool) {
	g, ok := s.content.(media.HumanPrivateImageStore)
	if !ok || g == nil || nilPrivateImageGateway(g) {
		respondError(w, 503, "private_image_unavailable")
		return nil, false
	}
	return g, true
}
func nilPrivateImageGateway(g media.HumanPrivateImageStore) bool {
	v := reflect.ValueOf(g)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Chan, reflect.Slice:
		return v.IsNil()
	}
	return false
}
func privateImageResponseCurrent(w http.ResponseWriter, r *http.Request) bool {
	if r.Context().Err() != nil {
		w.WriteHeader(http.StatusRequestTimeout)
		return false
	}
	return true
}
func privateImageFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, media.ErrImageLimit) {
		respondError(w, 413, "private_image_limit")
		return
	}
	if errors.Is(e, media.ErrImageType) || errors.Is(e, media.ErrImageInvalid) {
		respondError(w, 400, "invalid_private_image")
		return
	}
	momentFailure(w, e)
}
func privateImagePath(w http.ResponseWriter, r *http.Request, withID bool) bool {
	if !media.ValidPrivateImageID(r.PathValue("momentID")) || (withID && !media.ValidPrivateImageID(r.PathValue("imageID"))) {
		respondError(w, 400, "invalid_private_image_id")
		return false
	}
	return true
}
func (s *server) previewPrivateMomentImage(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	g, ok := s.privateImageGateway(w)
	if !ok || !momentNoQuery(w, r) || !privateImagePath(w, r, false) {
		return
	}
	var p media.PrivateImageInput
	if !decodeStrictJSON(w, r, &p) {
		return
	}
	if e := media.ValidatePrivateImageInput(p); e != nil {
		privateImageFailure(w, e)
		return
	}
	v, e := g.PreviewHumanPrivateImage(r.Context(), d, a, r.PathValue("momentID"), p)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		privateImageFailure(w, e)
		return
	}
	respond(w, 201, map[string]any{"data": v})
}
func (s *server) savePrivateMomentImage(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	g, ok := s.privateImageGateway(w)
	if !ok || !momentNoQuery(w, r) || !privateImagePath(w, r, true) {
		return
	}
	mime := r.Header.Get("Content-Type")
	if (mime != "image/png" && mime != "image/jpeg") || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("X-Birdtie-Private-Image-Confirmation")) != 1 || r.Header.Get("X-Birdtie-Private-Image-Confirmation") != r.PathValue("imageID") {
		respondError(w, 400, "private_image_confirmation_required")
		return
	}
	// Confirmation selects the reviewed bytes; it is never ownership proof.
	raw, e := io.ReadAll(io.LimitReader(r.Body, media.MaxImageInputBytes+1))
	if e != nil {
		privateImageFailure(w, content.ErrInvalid)
		return
	}
	if len(raw) > media.MaxImageInputBytes {
		privateImageFailure(w, media.ErrImageLimit)
		return
	}
	v, e := g.SaveHumanPrivateImage(r.Context(), d, a, r.PathValue("momentID"), r.PathValue("imageID"), mime, raw)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		privateImageFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) readPrivateMomentImages(w http.ResponseWriter, r *http.Request) {
	s.readPrivateMomentImage(w, r, false, false)
}
func (s *server) readPrivateMomentImageReceipt(w http.ResponseWriter, r *http.Request) {
	s.readPrivateMomentImage(w, r, false, false)
}
func (s *server) readPrivateMomentImageBytes(w http.ResponseWriter, r *http.Request) {
	s.readPrivateMomentImage(w, r, true, false)
}
func (s *server) readPrivateMomentImageOperation(w http.ResponseWriter, r *http.Request) {
	s.readPrivateMomentImage(w, r, false, true)
}
func (s *server) readPrivateMomentImage(w http.ResponseWriter, r *http.Request, binary, operation bool) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	g, ok := s.privateImageGateway(w)
	if !ok || !momentNoQuery(w, r) || !privateImagePath(w, r, r.PathValue("imageID") != "") {
		return
	}
	var mime string
	encode := func(rows []media.PrivateImageReceipt, b []byte) ([]byte, error) {
		if binary {
			if len(rows) != 1 {
				return nil, content.ErrNotFound
			}
			mime = rows[0].MIME
			return append([]byte(nil), b...), nil
		}
		if r.PathValue("imageID") != "" {
			if len(rows) != 1 {
				return nil, content.ErrNotFound
			}
			return json.Marshal(map[string]any{"data": rows[0]})
		}
		return json.Marshal(map[string]any{"data": rows})
	}
	var raw []byte
	var e error
	if operation {
		raw, e = g.ReadHumanPrivateImageOperation(r.Context(), d, a, r.PathValue("momentID"), r.PathValue("imageID"), encode)
	} else {
		raw, e = g.ReadHumanPrivateImages(r.Context(), d, a, r.PathValue("momentID"), r.PathValue("imageID"), binary, encode)
	}
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		privateImageFailure(w, e)
		return
	}
	// No bytes have reached ResponseWriter during native encode/final recheck.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if binary {
		w.Header().Set("Content-Type", mime)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (s *server) deletePrivateMomentImage(w http.ResponseWriter, r *http.Request) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	g, ok := s.privateImageGateway(w)
	if !ok || !privateImagePath(w, r, true) {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	rev, parse := strconv.ParseInt(q.Get("revision"), 10, 64)
	if e != nil || parse != nil || rev < 1 || len(q) != 1 || len(q["revision"]) != 1 {
		respondError(w, 400, "invalid_private_image_revision")
		return
	}
	e = g.DeleteHumanPrivateImage(r.Context(), d, a, r.PathValue("momentID"), r.PathValue("imageID"), rev)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		privateImageFailure(w, e)
		return
	}
	w.WriteHeader(204)
}
