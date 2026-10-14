package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
)

type photoCatalogFrame struct {
	access pp.Access
	proof  string
}
type photoCatalogFrameKey struct{}

func (s *server) beginPhotoCatalog(w http.ResponseWriter, r *http.Request) bool {
	actor, d, e := s.actor(r, false)
	if authFailed(w, e) {
		return false
	}
	if actor.AccountType != "person" {
		return true
	}
	p, ok := s.access.(pp.SessionStore)
	if !ok {
		return true
	}
	a := pp.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	proof, e := p.CapturePlacePhotoSession(r.Context(), a)
	if e != nil {
		placePhotoFailure(w, e)
		return false
	}
	*r = *r.WithContext(context.WithValue(r.Context(), photoCatalogFrameKey{}, photoCatalogFrame{a, proof}))
	return true
}

func placePhotoFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, pp.ErrInvalid):
		respondError(w, 400, "invalid_place_photo")
	case errors.Is(e, pp.ErrForbidden):
		respondError(w, 403, "forbidden")
	case errors.Is(e, pp.ErrChanged):
		respondError(w, 409, "place_photo_changed")
	case errors.Is(e, pp.ErrNotFound):
		respondError(w, 404, "not_found")
	default:
		respondError(w, 503, "place_photo_unavailable")
	}
}
func (s *server) photoAccess(w http.ResponseWriter, r *http.Request, private bool) (pp.Access, pp.Store, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("placeID")
	if !pp.ValidID(id) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		placePhotoFailure(w, pp.ErrInvalid)
		return pp.Access{}, nil, id, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		placePhotoFailure(w, pp.ErrForbidden)
		return pp.Access{}, nil, id, false
	}
	p, ok := s.access.(pp.Store)
	if !ok || nilHumanSelfReviewPort(p) {
		placePhotoFailure(w, pp.ErrUnavailable)
		return pp.Access{}, nil, id, false
	}
	actor, d, e := s.actor(r, private)
	if e != nil {
		placePhotoFailure(w, e)
		return pp.Access{}, nil, id, false
	}
	if actor.ID != "" && actor.AccountType != "person" || private && actor.AccountType != "person" {
		placePhotoFailure(w, pp.ErrForbidden)
		return pp.Access{}, nil, id, false
	}
	return pp.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}, p, id, true
}
func photoNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if e != nil || len(raw) != 0 {
		placePhotoFailure(w, pp.ErrInvalid)
		return false
	}
	return true
}
func photoObject(raw []byte, keys map[string]bool) (map[string]json.RawMessage, error) {
	m, e := entityCoverObject(raw, keys)
	if e != nil {
		return nil, pp.ErrInvalid
	}
	return m, nil
}
func decodePlacePhoto(w http.ResponseWriter, r *http.Request, commit bool) (pp.Input, error) {
	raw, e := entityCoverBody(w, r, int64(4*((ec.MaxInputBytes+2)/3)+8192))
	if e != nil {
		return pp.Input{}, pp.ErrInvalid
	}
	m, e := photoObject(raw, map[string]bool{"operationId": true, "expectedVersion": true, "action": true, "contentType": true, "contentBase64": true, "rights": true, "candidateId": true, "candidateVersion": true, "rightsConfirmed": true, "photoConfirmed": true, "approval": commit})
	if e != nil {
		return pp.Input{}, e
	}
	in := pp.Input{}
	if json.Unmarshal(m["operationId"], &in.OperationID) != nil || json.Unmarshal(m["expectedVersion"], &in.ExpectedVersion) != nil || json.Unmarshal(m["action"], &in.Action) != nil {
		return in, pp.ErrInvalid
	}
	n := 3
	if in.Action == "SUBMIT" {
		n += 3
		var encoded string
		if json.Unmarshal(m["contentType"], &in.ContentType) != nil || json.Unmarshal(m["contentBase64"], &encoded) != nil {
			return in, pp.ErrInvalid
		}
		in.Content, e = base64.StdEncoding.Strict().DecodeString(encoded)
		if e != nil || base64.StdEncoding.EncodeToString(in.Content) != encoded {
			return in, pp.ErrInvalid
		}
		rights, e := photoObject(m["rights"], map[string]bool{"basis": true, "attribution": true, "licenseReference": true, "retentionUntil": true, "canRetain": true, "canPublish": true})
		if e != nil || len(rights) != 6 {
			return in, pp.ErrInvalid
		}
		in.Rights = &pp.Rights{}
		if json.Unmarshal(m["rights"], in.Rights) != nil {
			return in, pp.ErrInvalid
		}
		in.Rights.RetentionUntil = in.Rights.RetentionUntil.UTC()
	} else {
		n += 2
		if json.Unmarshal(m["candidateId"], &in.CandidateID) != nil || json.Unmarshal(m["candidateVersion"], &in.CandidateVersion) != nil {
			return in, pp.ErrInvalid
		}
		if in.Action == "APPROVE" {
			n += 2
			if json.Unmarshal(m["rightsConfirmed"], &in.RightsConfirmed) != nil || json.Unmarshal(m["photoConfirmed"], &in.PhotoConfirmed) != nil {
				return in, pp.ErrInvalid
			}
		}
	}
	if commit {
		n++
		if json.Unmarshal(m["approval"], &in.Approval) != nil || !strings.HasPrefix(in.Approval, "pp1.") || len(in.Approval) > 1024 {
			return in, pp.ErrInvalid
		}
	}
	if len(m) != n {
		return in, pp.ErrInvalid
	}
	if _, e = pp.Normalize(in, time.Now().UTC()); e != nil {
		return in, e
	}
	return in, nil
}
func (s *server) finalPhotoActor(r *http.Request, a pp.Access) error {
	actor, d, e := s.actor(r, a.AccountID != "")
	if e != nil {
		return e
	}
	if actor.ID != a.AccountID || actor.AccountType != a.AccountType || d != a.SessionDigest || r.Context().Err() != nil {
		return pp.ErrChanged
	}
	if actor.ID != "" {
		v, ok := s.access.(entityCoverResponseValidator)
		if !ok {
			return pp.ErrUnavailable
		}
		return v.ValidateHumanSocialResponse(r.Context(), d, actor)
	}
	return nil
}
func (s *server) respondPlacePhoto(w http.ResponseWriter, r *http.Request, a pp.Access, payload any, validate func() error) {
	encoded, e := json.Marshal(map[string]any{"data": payload})
	if e != nil || len(encoded) > 512*1024 {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	if e = validate(); e != nil {
		placePhotoFailure(w, e)
		return
	}
	if e = s.finalPhotoActor(r, a); e != nil {
		placePhotoFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
func (s *server) getPlacePhoto(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, false)
	if !ok || !photoNoBody(w, r) {
		return
	}
	out, e := p.ReadPlacePhoto(r.Context(), a, id)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	if out.Version < 0 || out.Version > pp.MaxVersion || out.PlacePhoto != nil && (!out.PlacePhoto.Valid(time.Now().UTC()) || out.PlacePhoto.EntityID != id) {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	s.respondPlacePhoto(w, r, a, out, func() error {
		return p.ValidatePlacePhotoProjection(r.Context(), a, id, out.PlacePhoto, out.SourceProof)
	})
}
func (s *server) getPlacePhotoEditorial(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, true)
	if !ok || !photoNoBody(w, r) {
		return
	}
	out, e := p.ReadPlacePhotoEditorial(r.Context(), a, id)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	if out.PlaceID != id || len(out.Candidates) > 20 || out.Version < 0 || out.Version > pp.MaxVersion || !out.ExpiresAt.After(time.Now()) || out.ExpiresAt.Sub(out.ObservedAt) > 30*time.Second {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	s.respondPlacePhoto(w, r, a, out, func() error { return p.ValidatePlacePhotoEditorial(r.Context(), a, id, out) })
}
func (s *server) previewPlacePhoto(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, true)
	if !ok {
		return
	}
	in, e := decodePlacePhoto(w, r, false)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	out, e := p.PreviewPlacePhoto(r.Context(), a, id, in)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	if out.PlaceID != id || out.OperationID != in.OperationID || out.ExpectedVersion != in.ExpectedVersion || out.Action != in.Action || out.CandidateVersion != in.CandidateVersion || !out.ExpiresAt.After(time.Now()) || time.Until(out.ExpiresAt) > 31*time.Second || !strings.HasPrefix(out.Token, "pp1.") {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	if in.Action == "SUBMIT" {
		d, _ := pp.Normalize(in, time.Now().UTC())
		if out.CandidateID != in.OperationID || out.InputSHA256 != d.SHA256 || out.RightsSHA256 != in.Rights.Digest() {
			placePhotoFailure(w, pp.ErrUnavailable)
			return
		}
	} else if out.CandidateID != in.CandidateID {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	if in.Action != "WITHDRAW" {
		v := out.PreviewDerivative
		if v == nil {
			placePhotoFailure(w, pp.ErrUnavailable)
			return
		}
		raw, e := base64.StdEncoding.Strict().DecodeString(v.ContentBase64)
		sum := sha256.Sum256(raw)
		conf, format, e2 := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || e2 != nil || format != "jpeg" || conf.Width != 320 || conf.Height != 180 || v.ContentType != "image/jpeg" || v.Width != 320 || v.Height != 180 || v.ByteCount != len(raw) || len(raw) > ec.MaxThumbnailBytes || v.SHA256 != out.InputSHA256 || hex.EncodeToString(sum[:]) != out.InputSHA256 || base64.StdEncoding.EncodeToString(raw) != v.ContentBase64 {
			placePhotoFailure(w, pp.ErrUnavailable)
			return
		}
	} else if out.PreviewDerivative != nil {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	s.respondPlacePhoto(w, r, a, out, func() error { return p.ValidatePlacePhotoPreview(r.Context(), a, id, in, out) })
}
func (s *server) photoReceipt(w http.ResponseWriter, r *http.Request, a pp.Access, p pp.Store, id, op string, out pp.Receipt) {
	if !out.Valid(id, op) {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	s.respondPlacePhoto(w, r, a, out, func() error {
		fresh, e := p.ReadPlacePhotoOperation(r.Context(), a, id, op)
		if e != nil {
			return e
		}
		if fresh != out {
			return pp.ErrChanged
		}
		if out.ResponseProof != "" {
			gate, ok := p.(pp.SessionStore)
			if !ok {
				return pp.ErrUnavailable
			}
			return gate.ValidatePlacePhotoSession(r.Context(), a, out.ResponseProof)
		}
		return nil
	})
}
func (s *server) putPlacePhoto(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, true)
	if !ok {
		return
	}
	in, e := decodePlacePhoto(w, r, true)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	out, e := p.ChangePlacePhoto(r.Context(), a, id, in)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	if out.Status == "SUCCEEDED" && (out.Action != in.Action || in.Action == "SUBMIT" && (out.CandidateID != in.OperationID || out.CandidateVersion != 1 || out.AppliedVersion != 0) || in.Action != "SUBMIT" && (out.CandidateID != in.CandidateID || out.CandidateVersion != min(in.CandidateVersion+1, pp.MaxVersion))) {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	s.photoReceipt(w, r, a, p, id, in.OperationID, out)
}
func (s *server) getPlacePhotoOperation(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, true)
	if !ok || !photoNoBody(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !pp.ValidID(op) {
		placePhotoFailure(w, pp.ErrInvalid)
		return
	}
	out, e := p.ReadPlacePhotoOperation(r.Context(), a, id, op)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	s.photoReceipt(w, r, a, p, id, op, out)
}
func (s *server) stopPlacePhotoOperation(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, true)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	raw, e := entityCoverBody(w, r, 1024)
	if e != nil || !pp.ValidID(op) {
		placePhotoFailure(w, pp.ErrInvalid)
		return
	}
	m, e := photoObject(raw, map[string]bool{"expectedVersion": true})
	var v int64
	if e != nil || len(m) != 1 || json.Unmarshal(m["expectedVersion"], &v) != nil || v < 0 || v > pp.MaxVersion {
		placePhotoFailure(w, pp.ErrInvalid)
		return
	}
	out, e := p.StopPlacePhotoOperation(r.Context(), a, id, op, v)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	s.photoReceipt(w, r, a, p, id, op, out)
}
func (s *server) getPlacePhotoContent(w http.ResponseWriter, r *http.Request) {
	a, p, id, ok := s.photoAccess(w, r, false)
	if !ok || !photoNoBody(w, r) {
		return
	}
	v, e := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if e != nil || strconv.FormatInt(v, 10) != r.PathValue("version") || v < 1 || v > pp.MaxVersion {
		placePhotoFailure(w, pp.ErrInvalid)
		return
	}
	m, raw, e := p.ReadPlacePhotoContent(r.Context(), a, id, v)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	sum := sha256.Sum256(raw)
	conf, format, e2 := image.DecodeConfig(bytes.NewReader(raw))
	if !m.Valid(time.Now().UTC()) || m.EntityID != id || m.Version != v || len(raw) != m.ByteCount || hex.EncodeToString(sum[:]) != m.SHA256 || e2 != nil || format != "jpeg" || conf.Width != 320 || conf.Height != 180 {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	current, e := p.ReadPlacePhoto(r.Context(), a, id)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	if !pp.PreserveLease(&m, current.PlacePhoto, time.Now().UTC()) {
		placePhotoFailure(w, pp.ErrNotFound)
		return
	}
	proof := m.ResponseProof
	if proof == "" {
		proof = current.SourceProof
	}
	if e = p.ValidatePlacePhotoProjection(r.Context(), a, id, &m, proof); e != nil {
		placePhotoFailure(w, e)
		return
	}
	if e = s.finalPhotoActor(r, a); e != nil {
		placePhotoFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (s *server) respondPublicPlacePhotos(w http.ResponseWriter, r *http.Request, payload any, places []foundation.Place) {
	has := false
	for _, v := range places {
		has = has || v.PlacePhoto != nil
	}
	if !has {
		respond(w, 200, map[string]any{"data": payload})
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		placePhotoFailure(w, pp.ErrForbidden)
		return
	}
	actor, d, e := s.actor(r, false)
	if e != nil {
		placePhotoFailure(w, e)
		return
	}
	if actor.ID != "" && actor.AccountType != "person" {
		placePhotoFailure(w, pp.ErrForbidden)
		return
	}
	p, ok := s.access.(pp.Store)
	if !ok {
		placePhotoFailure(w, pp.ErrUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	a := pp.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	s.respondPlacePhoto(w, r, a, payload, func() error {
		for _, v := range places {
			if v.PlacePhoto != nil {
				if e := p.ValidatePlacePhotoProjection(r.Context(), pp.Access{}, v.ID, v.PlacePhoto, v.PhotoSourceProof); e != nil {
					return e
				}
			}
		}
		if a.AccountID != "" {
			frame, ok := r.Context().Value(photoCatalogFrameKey{}).(photoCatalogFrame)
			gate, exists := s.access.(pp.SessionStore)
			if !ok || !exists || frame.access != a {
				return pp.ErrChanged
			}
			return gate.ValidatePlacePhotoSession(r.Context(), a, frame.proof)
		}
		return nil
	})
}
func (s *server) registerPlacePhotoRoutes(mux *http.ServeMux) {
	base := "/v1/places/{placeID}/photos"
	mux.HandleFunc("GET "+base, s.getPlacePhoto)
	mux.HandleFunc("GET "+base+"/editorial", s.getPlacePhotoEditorial)
	mux.HandleFunc("POST "+base+"/preview", s.previewPlacePhoto)
	mux.HandleFunc("PUT "+base, s.putPlacePhoto)
	mux.HandleFunc("GET "+base+"/operations/{operationID}", s.getPlacePhotoOperation)
	mux.HandleFunc("POST "+base+"/operations/{operationID}/stop", s.stopPlacePhotoOperation)
	mux.HandleFunc("GET "+base+"/{contentPath...}", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.PathValue("contentPath"), "/")
		if len(parts) != 2 || parts[1] != "content" {
			placePhotoFailure(w, pp.ErrInvalid)
			return
		}
		r.SetPathValue("version", parts[0])
		s.getPlacePhotoContent(w, r)
	})
}
