package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"time"
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

func momentMediaFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, mm.ErrVideoBusy):
		respondError(w, 503, "video_processing_busy")
	case errors.Is(e, mm.ErrForbidden):
		respondError(w, 403, "moment_media_owner_required")
	case errors.Is(e, mm.ErrNotFound):
		respondError(w, 404, "moment_media_not_found")
	default:
		humanControlFailure(w, e)
	}
}
func (s *server) momentMediaPort(w http.ResponseWriter) (mm.Store, bool) {
	p, ok := s.content.(mm.Store)
	if !ok || p == nil || (reflect.ValueOf(p).Kind() == reflect.Pointer && reflect.ValueOf(p).IsNil()) {
		respondError(w, 503, "moment_media_unavailable")
		return nil, false
	}
	return p, true
}
func mediaJSONBody(w http.ResponseWriter, r *http.Request, out any, maximum int64) bool {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Type") != "application/json" {
		respondError(w, 415, "json_required")
		return false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, maximum))
	if e != nil || mm.StrictJSON(raw, out) != nil {
		momentMediaFailure(w, mm.ErrInvalid)
		return false
	}
	return true
}
func mediaShape(w http.ResponseWriter, r *http.Request, keys ...string) bool {
	if len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 {
		respondError(w, 403, "person_account_required")
		return false
	}
	return humanControlPath(w, r, keys...)
}
func mediaAccess(a identity.Actor, d [32]byte) mm.Access {
	return mm.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}
}
func (s *server) prepareMomentMedia(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "operationID") {
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	var wire struct {
		OperationID   string `json:"operationId"`
		LocalScope    string `json:"localScope"`
		ContentType   string `json:"contentType"`
		ContentBase64 string `json:"contentBase64"`
	}
	if !mediaJSONBody(w, r, &wire, int64((mm.MaxInputBytes+2)/3*4+2048)) {
		return
	}
	if wire.OperationID != r.PathValue("operationID") {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	content, e := base64.StdEncoding.Strict().DecodeString(wire.ContentBase64)
	if e != nil {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	v, e := p.PrepareMomentMedia(r.Context(), mediaAccess(a, d), mm.StageInput{OperationID: wire.OperationID, LocalScope: wire.LocalScope, ContentType: wire.ContentType, Content: content})
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if v.OperationID != wire.OperationID || (v.Status != "STOPPED" && (v.Status != "SUCCEEDED" || v.Material == nil || !mm.StageValid(*v.Material))) {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) momentMediaPreparationHistory(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentMediaPreparation(r.Context(), mediaAccess(a, d), r.PathValue("operationID"), stop)
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if v.OperationID != r.PathValue("operationID") || v.CommittedAt.IsZero() || (v.Status != "SUCCEEDED" && v.Status != "STOPPED") {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) getOwnMomentMedia(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "momentID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	v, e := p.ReadOwnMomentMedia(r.Context(), mediaAccess(a, d), r.PathValue("momentID"))
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if !v.Valid(r.PathValue("momentID"), time.Now().UTC()) {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewMomentMedia(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "momentID") {
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	var in mm.Input
	if !mediaJSONBody(w, r, &in, 32768) || mm.ValidateInput(in, false, time.Now().UTC()) != nil {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	v, e := p.PreviewMomentMedia(r.Context(), mediaAccess(a, d), r.PathValue("momentID"), in)
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if len(v.Images) != len(in.Attachments) {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	valid := humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, mm.InputDigest(r.PathValue("momentID"), in)) && v.MomentID == r.PathValue("momentID") && v.ExpectedMomentRevision == in.ExpectedMomentRevision && v.ExpectedSetVersion == in.ExpectedSetVersion && mm.Hash(v.SourceVersion) && len(v.Images) == len(in.Attachments)
	for i, image := range v.Images {
		b, e := base64.StdEncoding.Strict().DecodeString(image.ThumbnailBase64)
		valid = valid && e == nil && mm.StageValid(image.Stage) && image.MediaID == in.Attachments[i].MediaID && image.MediaVersion == in.Attachments[i].MediaVersion && image.Rights.Digest() == in.Attachments[i].Rights.Digest() && len(b) == image.Stage.ThumbnailBytes && mm.DigestBytes(b) == image.Stage.ThumbnailSHA
	}
	if !valid {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitMomentMedia(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "momentID", "operationID") {
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	var in mm.Input
	if !mediaJSONBody(w, r, &in, 49152) || in.OperationID != r.PathValue("operationID") || mm.ValidateInput(in, true, time.Now().UTC()) != nil {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	v, e := p.CommitMomentMedia(r.Context(), mediaAccess(a, d), r.PathValue("momentID"), in)
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if !v.Valid(r.PathValue("momentID"), in.OperationID) || v.Status == "SUCCEEDED" && (v.PayloadDigest != mm.InputDigest(r.PathValue("momentID"), in) || v.Action != in.Action || v.ExpectedMomentRevision != in.ExpectedMomentRevision || v.ExpectedSetVersion != in.ExpectedSetVersion) {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) momentMediaHistory(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !mediaShape(w, r, "momentID", "operationID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentMediaOperation(r.Context(), mediaAccess(a, d), r.PathValue("momentID"), r.PathValue("operationID"), stop)
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if !v.Valid(r.PathValue("momentID"), r.PathValue("operationID")) {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) publicMomentMediaAccess(w http.ResponseWriter, r *http.Request) (*http.Request, mm.Access, bool) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 || !humanControlNoBody(w, r) {
		momentMediaFailure(w, mm.ErrInvalid)
		return r, mm.Access{}, false
	}
	if len(r.Header.Values("Authorization")) != 0 {
		r, a, d, ok := s.humanControlActor(w, r)
		return r, mediaAccess(a, d), ok
	}
	return r, mm.Access{}, true
}
func (s *server) getPublicMomentMedia(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r, a, ok := s.publicMomentMediaAccess(w, r)
	if !ok {
		return
	}
	if !mm.ID(r.PathValue("momentID")) {
		momentMediaFailure(w, mm.ErrInvalid)
		return
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	v, e := p.ReadPublicMomentMedia(r.Context(), a, r.PathValue("momentID"))
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if !v.Valid(r.PathValue("momentID"), time.Now().UTC()) || v.CanReplace || v.CanDelete {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	b, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		momentMediaFailure(w, mm.ErrUnavailable)
		return
	}
	if _, e = p.FinalizePublicMomentMedia(r.Context(), a, v); e != nil {
		momentMediaFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	w.Write(b)
}
func (s *server) getPublicMomentMediaContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r, a, ok := s.publicMomentMediaAccess(w, r)
	if !ok {
		return
	}
	selector := mm.ContentSelector{MomentID: r.PathValue("momentID"), MediaID: r.PathValue("mediaID"), Variant: r.PathValue("variant")}
	if selector.Variant == "video" {
		ctx, release, e := mm.TryVideoWork(r.Context())
		if e != nil {
			momentMediaFailure(w, e)
			return
		}
		defer release()
		r = r.WithContext(ctx)
	}
	values := []*int64{&selector.MomentRevision, &selector.SetVersion, &selector.MediaVersion}
	for i, key := range []string{"momentRevision", "setVersion", "mediaVersion"} {
		n, e := strconv.ParseInt(r.PathValue(key), 10, 64)
		if e != nil || strconv.FormatInt(n, 10) != r.PathValue(key) {
			momentMediaFailure(w, mm.ErrInvalid)
			return
		}
		*values[i] = n
	}
	p, ok := s.momentMediaPort(w)
	if !ok {
		return
	}
	b, e := p.ReadPublicMomentMediaContent(r.Context(), a, selector, func(meta mm.Attachment, b []byte) ([]byte, error) {
		count, hash := meta.DisplayBytes, meta.DisplaySHA
		if selector.Variant == "thumbnail" {
			count, hash = meta.ThumbnailBytes, meta.ThumbnailSHA
		}
		if selector.Variant == "video" {
			if meta.Kind != "VIDEO" || meta.Video == nil {
				return nil, mm.ErrNotFound
			}
			count, hash = meta.Video.Bytes, meta.Video.SHA256
		}
		if len(b) != count || mm.DigestBytes(b) != hash {
			return nil, mm.ErrChanged
		}
		return b, nil
	})
	if e != nil {
		momentMediaFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	mime := "image/jpeg"
	if selector.Variant == "video" {
		mime = "video/mp4"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(200)
	w.Write(b)
}

// Host calls this registration once after its current source/build checkpoint.
func (s *server) registerMomentMediaRoutes(mux *http.ServeMux) {
	stage := "/v1/me/moment-media-materials/operations/{operationID}"
	mux.HandleFunc("PUT "+stage, s.prepareMomentMedia)
	mux.HandleFunc("GET "+stage, func(w http.ResponseWriter, r *http.Request) { s.momentMediaPreparationHistory(w, r, false) })
	mux.HandleFunc("POST "+stage+"/stop", func(w http.ResponseWriter, r *http.Request) { s.momentMediaPreparationHistory(w, r, true) })
	set := "/v1/me/moments/{momentID}/public-media"
	mux.HandleFunc("GET "+set, s.getOwnMomentMedia)
	mux.HandleFunc("POST "+set+"/previews", s.previewMomentMedia)
	mux.HandleFunc("PUT "+set+"/operations/{operationID}", s.commitMomentMedia)
	mux.HandleFunc("GET "+set+"/operations/{operationID}", func(w http.ResponseWriter, r *http.Request) { s.momentMediaHistory(w, r, false) })
	mux.HandleFunc("POST "+set+"/operations/{operationID}/stop", func(w http.ResponseWriter, r *http.Request) { s.momentMediaHistory(w, r, true) })
	mux.HandleFunc("GET /v1/moments/{momentID}/public-media", s.getPublicMomentMedia)
	mux.HandleFunc("GET /v1/moments/{momentID}/public-media/{momentRevision}/{setVersion}/{mediaID}/{mediaVersion}/{variant}", s.getPublicMomentMediaContent)
}
