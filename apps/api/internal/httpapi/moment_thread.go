package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

func (s *server) momentThreadPort(w http.ResponseWriter) (content.MomentThreadStore, bool) {
	p, ok := s.content.(content.MomentThreadStore)
	if !ok || p == nil {
		respondError(w, 503, "moment_thread_unavailable")
		return nil, false
	}
	return p, true
}

func threadBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Header.Get("Content-Type") != "application/json" {
		respondError(w, 400, "invalid_moment_thread")
		return nil, false
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
	if e != nil {
		respondError(w, 400, "invalid_moment_thread")
		return nil, false
	}
	return raw, true
}

func (s *server) previewMomentThread(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	p, ok := s.momentThreadPort(w)
	if !ok {
		return
	}
	raw, ok := threadBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeMomentThread(raw, false)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	v, e := p.PreviewMomentThread(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, content.MomentThreadDigest(in)) || content.MomentThreadDigest(v.Input) != content.MomentThreadDigest(in) || !content.MomentControlHex(v.SourceVersion) || v.Members == nil || v.Materials == nil || (in.Action == "CREATE_PUBLISH" || in.Action == "PUBLISH") && (len(v.Members) != 0 || len(v.Materials) != len(in.Items)) || in.Action == "WITHDRAW" && (len(v.Members) < 2 || len(v.Materials) != 0) {
		respondError(w, 503, "moment_thread_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}

func (s *server) commitMomentThread(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	p, ok := s.momentThreadPort(w)
	if !ok {
		return
	}
	raw, ok := threadBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeMomentThread(raw, true)
	if e != nil || in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_moment_thread")
		return
	}
	v, e := p.CommitMomentThread(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !content.ValidMomentThreadReceipt(v, in.OperationID) || v.Status == "SUCCEEDED" && (v.Action != in.Action || v.PayloadDigest != content.MomentThreadDigest(in) || v.ExpectedVersion != in.ExpectedVersion || (in.Action == "WITHDRAW" || in.Action == "PUBLISH") && v.ThreadID != in.ThreadID) {
		respondError(w, 503, "moment_thread_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}

func (s *server) historyMomentThread(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.momentThreadPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentThreadOperation(r.Context(), d, a, r.PathValue("operationID"), stop)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !content.ValidMomentThreadReceipt(v, r.PathValue("operationID")) {
		respondError(w, 503, "moment_thread_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}

func (s *server) readMomentThread(w http.ResponseWriter, r *http.Request, byMoment bool) {
	key := "threadID"
	if byMoment {
		key = "momentID"
	}
	if !humanControlPath(w, r, key) || !humanControlNoBody(w, r) {
		return
	}
	var access content.PublicMomentAccess
	if len(r.Header.Values("Authorization")) > 0 {
		var ok bool
		r, access.Actor, access.SessionDigest, ok = s.humanControlActor(w, r)
		if !ok {
			return
		}
	}
	p, ok := s.momentThreadPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentThread(r.Context(), access, r.PathValue(key), byMoment)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if v.SchemaVersion != "moment-thread-view-v1" || !content.MomentControlHex(v.SourceVersion) || v.ResponseProof == nil || len(v.Members) < 2 || len(v.Members) > content.MomentThreadMaxEntries {
		respondError(w, 503, "moment_thread_unavailable")
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		respondError(w, 503, "moment_thread_unavailable")
		return
	}
	if e = v.ResponseProof.RevalidateHumanControl(r.Context()); e != nil {
		momentControlFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}

func (s *server) registerMomentThreadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/me/moment-threads/previews", s.previewMomentThread)
	mux.HandleFunc("PUT /v1/me/moment-threads/operations/{operationID}", s.commitMomentThread)
	mux.HandleFunc("GET /v1/me/moment-threads/operations/{operationID}", func(w http.ResponseWriter, r *http.Request) { s.historyMomentThread(w, r, false) })
	mux.HandleFunc("POST /v1/me/moment-threads/operations/{operationID}/stop", func(w http.ResponseWriter, r *http.Request) { s.historyMomentThread(w, r, true) })
	mux.HandleFunc("GET /v1/moment-threads/{threadID}", func(w http.ResponseWriter, r *http.Request) { s.readMomentThread(w, r, false) })
	mux.HandleFunc("GET /v1/moments/{momentID}/thread", func(w http.ResponseWriter, r *http.Request) { s.readMomentThread(w, r, true) })
}

func (s *server) registerOwnedMomentThreadRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/moment-threads/{threadID}", func(w http.ResponseWriter, r *http.Request) { s.readOwnedMomentThread(w, r, false) })
	mux.HandleFunc("GET /v1/me/moments/{momentID}/thread", func(w http.ResponseWriter, r *http.Request) { s.readOwnedMomentThread(w, r, true) })
}
func (s *server) readOwnedMomentThread(w http.ResponseWriter, r *http.Request, byMoment bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	key := "threadID"
	if byMoment {
		key = "momentID"
	}
	if !ok || !humanControlPath(w, r, key) || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.content.(content.OwnedMomentThreadStore)
	if !ok || p == nil {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	v, e := p.ReadOwnedMomentThread(r.Context(), d, a, r.PathValue(key), byMoment)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if v.SchemaVersion != "owned-moment-thread-v1" || v.Members == nil || v.EntryContexts == nil || v.Legs == nil || !content.MomentControlHex(v.SourceVersion) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
