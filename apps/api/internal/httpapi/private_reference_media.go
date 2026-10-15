package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func (s *server) registerReferenceImageRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /v1/me/reference-image-targets", s.readReferenceImageTargets)
	m.HandleFunc("GET /v1/me/reference-images", s.readReferenceImages)
	m.HandleFunc("GET /v1/me/reference-images/{imageID}/content", s.readReferenceImageContent)
	m.HandleFunc("DELETE /v1/me/reference-images/{imageID}", s.deleteReferenceImage)
	m.HandleFunc("GET /v1/me/reference-image-operations/{operationID}", s.readReferenceImageOperation)
}

func (s *server) readReferenceImageTargets(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	values, e := referenceQuery(r)
	q := content.ReferenceImageTargetsQuery{TargetType: values.Get("targetType"), Cursor: values.Get("cursor")}
	if e != nil || !q.Valid() || len(values) < 1 || len(values) > 2 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	for key := range values {
		if key != "targetType" && key != "cursor" {
			momentControlFailure(w, content.ErrInvalid)
			return
		}
	}
	r, actor, digest, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.content.(content.HumanReferenceImageTargetsStore)
	if !ok || nilHumanSelfReviewPort(port) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	page, e := port.ReadHumanReferenceImageTargets(r.Context(), digest, actor, q)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if page.TargetType != q.TargetType || !page.Valid(time.Now().UTC()) || page.ResponseProof == nil {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, digest, actor, map[string]any{"data": page}, page.ResponseProof, true)
}
func (s *server) referenceImagePort(w http.ResponseWriter) (content.HumanReferenceImageStore, bool) {
	p, ok := s.content.(content.HumanReferenceImageStore)
	if !ok || nilHumanSelfReviewPort(p) {
		momentControlFailure(w, content.ErrUnavailable)
		return nil, false
	}
	return p, true
}
func referenceQuery(r *http.Request) (url.Values, error) {
	v, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		return nil, content.ErrInvalid
	}
	for _, a := range v {
		if len(a) != 1 || a[0] == "" {
			return nil, content.ErrInvalid
		}
	}
	return v, nil
}
func referenceActorRequest(r *http.Request) *http.Request {
	v := r.Clone(r.Context())
	u := *r.URL
	u.RawQuery = ""
	u.ForceQuery = false
	v.URL = &u
	return v
}
func (s *server) readReferenceImages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	values, e := referenceQuery(r)
	q := content.ReferenceImageQuery{TargetType: values.Get("targetType"), TargetID: values.Get("targetId"), Cursor: values.Get("cursor")}
	if e != nil || !q.Valid() || len(values) < 2 || len(values) > 3 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	for k := range values {
		if k != "targetType" && k != "targetId" && k != "cursor" {
			momentControlFailure(w, content.ErrInvalid)
			return
		}
	}
	r, a, d, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.referenceImagePort(w)
	if !ok {
		return
	}
	v, e := p.ReadHumanReferenceImages(r.Context(), d, a, q)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !v.Valid(time.Now().UTC()) || v.TargetType != q.TargetType || v.TargetID != q.TargetID {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) readReferenceImageContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	values, e := referenceQuery(r)
	rev, pe := strconv.ParseInt(values.Get("revision"), 10, 64)
	id, source := r.PathValue("imageID"), values.Get("sourceVersion")
	if e != nil || pe != nil || rev < 1 || strconv.FormatInt(rev, 10) != values.Get("revision") || len(values) != 2 || !identity.HumanControlUUID(id) || !content.MomentControlHex(source) || r.Header.Get("Range") != "" {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, referenceActorRequest(r))
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.referenceImagePort(w)
	if !ok {
		return
	}
	v, e := p.ReadHumanReferenceImageContent(r.Context(), d, a, id, rev, source)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	defer clear(v.Bytes)
	if v.Image.ID != id || v.Image.Revision != rev || v.Image.SourceVersion != source || v.Image.ByteCount != len(v.Bytes) || legacymedia.Hash(v.Bytes) != v.Image.SHA256 || v.ResponseProof == nil || !v.ValidUntil.After(time.Now().UTC()) || (v.Image.MIMEType != "image/jpeg" && v.Image.MIMEType != "image/png") {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	frame, valid, _ := identity.OriginalHumanControlFrame(r.Context(), d, a)
	sessions, ok := s.access.(identity.HumanControlSessionStore)
	if !valid || !ok {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	if e = sessions.ValidateHumanControlSession(r.Context(), d, a, frame); e != nil {
		humanControlFailure(w, e)
		return
	}
	if e = v.ResponseProof.RevalidateHumanControl(r.Context()); e != nil {
		momentControlFailure(w, e)
		return
	}
	if r.Context().Err() != nil || !v.ValidUntil.After(time.Now().UTC()) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", v.Image.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(v.Bytes)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(v.Bytes)
}
func validReferenceReceipt(v content.ReferenceImageReceipt, op string) bool {
	return v.SchemaVersion == "owned-reference-image-operation-v1" && v.OperationID == op && identity.HumanControlUUID(v.ImageID) && v.Revision > 1 && v.Status == "COMMITTED" && v.Effect == "DELETED" && !v.CommittedAt.IsZero()
}
func (s *server) deleteReferenceImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("imageID")
	if !identity.HumanControlUUID(id) {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeReferenceImageDelete(raw)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	p, ok := s.referenceImagePort(w)
	if !ok {
		return
	}
	v, e := p.DeleteHumanReferenceImage(r.Context(), d, a, id, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !validReferenceReceipt(v, in.OperationID) || v.ImageID != id || v.Revision != in.ExpectedRevision+1 {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) readReferenceImageOperation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || !identity.HumanControlUUID(r.PathValue("operationID")) {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	p, ok := s.referenceImagePort(w)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	v, e := p.ReadHumanReferenceImageOperation(r.Context(), d, a, op)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !validReferenceReceipt(v, op) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
