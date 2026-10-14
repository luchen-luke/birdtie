package httpapi

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
)

func momentPhotoFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, mp.ErrForbidden):
		respondError(w, 403, "moment_photo_owner_required")
	case errors.Is(e, mp.ErrNotFound):
		respondError(w, 404, "moment_photo_not_found")
	default:
		humanControlFailure(w, e)
	}
}
func (s *server) momentPhotoPort(w http.ResponseWriter) (mp.Store, bool) {
	p, ok := s.content.(mp.Store)
	if !ok || p == nil {
		respondError(w, 503, "moment_photo_unavailable")
		return nil, false
	}
	return p, true
}
func momentPhotoRequestShape(w http.ResponseWriter, r *http.Request, op bool) bool {
	if len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 {
		respondError(w, 403, "person_account_required")
		return false
	}
	if op {
		return humanControlPath(w, r, "momentID", "operationID")
	}
	return humanControlPath(w, r, "momentID")
}
func momentPhotoNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	return humanControlNoBody(w, r)
}
func momentPhotoBody(w http.ResponseWriter, r *http.Request, commit bool) (mp.Input, bool) {
	if len(r.Header.Values("Content-Type")) != 1 || r.Header.Get("Content-Type") != "application/json" {
		respondError(w, 415, "json_required")
		return mp.Input{}, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, int64((mp.MaxInputBytes+2)/3*4+16384)))
	if e != nil {
		momentPhotoFailure(w, mp.ErrInvalid)
		return mp.Input{}, false
	}
	in, e := mp.Decode(raw, commit)
	if e != nil {
		momentPhotoFailure(w, e)
		return in, false
	}
	return in, true
}
func (s *server) getMomentPhotoState(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !momentPhotoRequestShape(w, r, false) || !momentPhotoNoBody(w, r) {
		return
	}
	p, ok := s.momentPhotoPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentPhotoState(r.Context(), mp.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}, r.PathValue("momentID"))
	if e != nil {
		momentPhotoFailure(w, e)
		return
	}
	if !v.Valid(r.PathValue("momentID"), time.Now().UTC()) {
		momentPhotoFailure(w, mp.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewMomentPhoto(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !momentPhotoRequestShape(w, r, false) {
		return
	}
	p, ok := s.momentPhotoPort(w)
	if !ok {
		return
	}
	in, ok := momentPhotoBody(w, r, false)
	if !ok {
		return
	}
	v, e := p.PreviewMomentPhoto(r.Context(), mp.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}, r.PathValue("momentID"), in)
	if e != nil {
		momentPhotoFailure(w, e)
		return
	}
	derived, sum, rights, e := mp.Normalize(in, false, time.Now().UTC())
	if e != nil || !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, mp.Digest(r.PathValue("momentID"), in, sum, rights)) || v.MomentID != r.PathValue("momentID") || v.PhotoID != in.PhotoID || v.ExpectedMomentRevision != in.ExpectedMomentRevision || v.ExpectedPhotoVersion != in.ExpectedPhotoVersion || v.InputSHA256 != sum || v.RightsSHA256 != rights || !mp.Hex(v.SourceVersion) {
		momentPhotoFailure(w, mp.ErrUnavailable)
		return
	}
	if in.Action == "SAVE" {
		if v.PreviewDerivative == nil || v.PreviewDerivative.ContentType != "image/jpeg" || v.PreviewDerivative.Width != mp.Width || v.PreviewDerivative.Height != mp.Height || v.PreviewDerivative.ByteCount != len(derived.Bytes) || v.PreviewDerivative.SHA256 != sum || v.PreviewDerivative.ContentBase64 != base64.StdEncoding.EncodeToString(derived.Bytes) {
			momentPhotoFailure(w, mp.ErrUnavailable)
			return
		}
	} else if v.PreviewDerivative != nil {
		momentPhotoFailure(w, mp.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitMomentPhoto(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !momentPhotoRequestShape(w, r, true) {
		return
	}
	p, ok := s.momentPhotoPort(w)
	if !ok {
		return
	}
	in, ok := momentPhotoBody(w, r, true)
	if !ok {
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		momentPhotoFailure(w, mp.ErrInvalid)
		return
	}
	v, e := p.CommitMomentPhoto(r.Context(), mp.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}, r.PathValue("momentID"), in)
	if e != nil {
		momentPhotoFailure(w, e)
		return
	}
	_, sum, rights, e := mp.Normalize(in, true, time.Now().UTC())
	if e != nil || !v.Valid(r.PathValue("momentID"), in.OperationID) || v.Status == "SUCCEEDED" && (v.Action != in.Action || v.PhotoID != in.PhotoID || *v.ExpectedMomentRevision != in.ExpectedMomentRevision || *v.ExpectedPhotoVersion != in.ExpectedPhotoVersion || v.PayloadDigest != mp.Digest(r.PathValue("momentID"), in, sum, rights)) {
		momentPhotoFailure(w, mp.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readMomentPhotoOperation(w http.ResponseWriter, r *http.Request) {
	s.momentPhotoHistory(w, r, false)
}
func (s *server) stopMomentPhotoOperation(w http.ResponseWriter, r *http.Request) {
	s.momentPhotoHistory(w, r, true)
}
func (s *server) momentPhotoHistory(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !momentPhotoRequestShape(w, r, true) || !momentPhotoNoBody(w, r) {
		return
	}
	p, ok := s.momentPhotoPort(w)
	if !ok {
		return
	}
	var v mp.Receipt
	var e error
	access := mp.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}
	moment, op := r.PathValue("momentID"), r.PathValue("operationID")
	if stop {
		v, e = p.StopMomentPhotoOperation(r.Context(), access, moment, op)
	} else {
		v, e = p.ReadMomentPhotoOperation(r.Context(), access, moment, op)
	}
	if e != nil {
		momentPhotoFailure(w, e)
		return
	}
	if !v.Valid(moment, op) {
		momentPhotoFailure(w, mp.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) getMomentPhotoContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) != 0 || !momentPhotoNoBody(w, r) {
		momentPhotoFailure(w, mp.ErrInvalid)
		return
	}
	revision, e := strconv.ParseInt(r.PathValue("momentRevision"), 10, 64)
	version, e2 := strconv.ParseInt(r.PathValue("photoVersion"), 10, 64)
	selector := mp.Selector{MomentID: r.PathValue("momentID"), MomentRevision: revision, PhotoID: r.PathValue("photoID"), Version: version}
	if e != nil || e2 != nil || !selector.Valid() || strconv.FormatInt(revision, 10) != r.PathValue("momentRevision") || strconv.FormatInt(version, 10) != r.PathValue("photoVersion") {
		momentPhotoFailure(w, mp.ErrInvalid)
		return
	}
	var access mp.Access
	if len(r.Header.Values("Authorization")) != 0 {
		a, d, e := s.humanSocialActor(r, true)
		if e != nil {
			momentPhotoFailure(w, e)
			return
		}
		if a.AccountType != "person" {
			momentPhotoFailure(w, identity.ErrUnauthorized)
			return
		}
		port, ok := s.access.(identity.HumanControlSessionStore)
		if !ok {
			momentPhotoFailure(w, mp.ErrUnavailable)
			return
		}
		frame, e := port.CaptureHumanControlSession(r.Context(), d, a)
		if e != nil {
			momentPhotoFailure(w, e)
			return
		}
		r = r.WithContext(identity.WithHumanControlFrame(r.Context(), d, a, frame))
		access = mp.Access{AccountID: a.ID, AccountType: a.AccountType, SessionDigest: d}
	}
	p, ok := s.momentPhotoPort(w)
	if !ok {
		return
	}
	data, e := p.ReadMomentPhotoContent(r.Context(), access, selector, func(meta mp.Metadata, raw []byte) ([]byte, error) {
		if !meta.Valid(time.Now().UTC()) || meta.MomentID != selector.MomentID || meta.MomentRevision != selector.MomentRevision || meta.PhotoID != selector.PhotoID || meta.Version != selector.Version {
			return nil, mp.ErrChanged
		}
		return raw, nil
	})
	if e != nil {
		momentPhotoFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func (s *server) registerMomentPhotoRoutes(mux *http.ServeMux) {
	prefix := "/v1/me/moments/{momentID}/public-photo"
	mux.HandleFunc("GET "+prefix, s.getMomentPhotoState)
	mux.HandleFunc("POST "+prefix+"/previews", s.previewMomentPhoto)
	mux.HandleFunc("PUT "+prefix+"/operations/{operationID}", s.commitMomentPhoto)
	mux.HandleFunc("GET "+prefix+"/operations/{operationID}", s.readMomentPhotoOperation)
	mux.HandleFunc("POST "+prefix+"/operations/{operationID}/stop", s.stopMomentPhotoOperation)
	mux.HandleFunc("GET /v1/moments/{momentID}/public-photo/{momentRevision}/{photoID}/{photoVersion}/content", s.getMomentPhotoContent)
}
