package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

func (s *server) candidateAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	// Reuse actual native authenticated Person workspace boundary, not a body actor.
	a, ok := s.memoryAccess(w, r)
	if !ok {
		return a, false
	}
	if s.memoryCandidates == nil {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return a, false
	}
	return a, true
}
func candidateJSON(raw []byte, out any) bool {
	raw = bytes.TrimSpace(raw)
	// Check duplicate keys at every nesting level before strict typed decoding.
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var visit func() bool
	visit = func() bool {
		t, e := d.Token()
		if e != nil {
			return false
		}
		v, ok := t.(json.Delim)
		if !ok {
			return true
		}
		switch v {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				s, ok := k.(string)
				if e != nil || !ok || seen[s] {
					return false
				}
				seen[s] = true
				if !visit() {
					return false
				}
			}
			t, e = d.Token()
			return e == nil && t == json.Delim('}')
		case '[':
			for d.More() {
				if !visit() {
					return false
				}
			}
			t, e = d.Token()
			return e == nil && t == json.Delim(']')
		default:
			return false
		}
	}
	if len(raw) == 0 || raw[0] != '{' || !visit() {
		return false
	}
	if _, e := d.Token(); e != io.EOF {
		return false
	}
	if !candidateExactFields(raw, reflect.TypeOf(out).Elem()) {
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return false
	}
	return true
}
func candidateExactFields(raw []byte, t reflect.Type) bool {
	if t.PkgPath() == "time" {
		return true
	}
	if t.Kind() == reflect.Slice {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil || bytes.Equal(raw, []byte("null")) {
			return false
		}
		for _, item := range items {
			if !candidateExactFields(item, t.Elem()) {
				return false
			}
		}
		return true
	}
	if t.Kind() != reflect.Struct {
		return true
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || len(m) != t.NumField() {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		key := strings.Split(f.Tag.Get("json"), ",")[0]
		v, ok := m[key]
		if !ok || !candidateExactFields(v, f.Type) {
			return false
		}
	}
	return true
}
func candidateDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	raw, ok := memoryBody(w, r)
	if !ok {
		return false
	}
	if !candidateJSON(raw, v) {
		memoryFailure(w, agentmemory.ErrInvalid)
		return false
	}
	return true
}
func candidateNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			memoryFailure(w, agentmemory.ErrInvalid)
			return false
		}
	}
	return true
}
func candidateID(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := r.PathValue("candidateID")
	id, e := agentmemory.NormalizeMemoryID(raw)
	if e != nil || id != raw {
		memoryFailure(w, agentmemory.ErrInvalid)
		return "", false
	}
	return id, true
}
func candidateRecordValid(a agentprofile.PrivateAccess, v agentmemorycandidate.Record) bool {
	id, e := agentmemory.NormalizeMemoryID(v.ID)
	ag, err := agentmemory.NormalizeMemoryID(v.AgentID)
	return e == nil && err == nil && id == v.ID && ag == v.AgentID && v.SchemaVersion == agentmemorycandidate.Schema && v.Owner == a.WorkspacePrincipal && v.Version > 0 && v.ModelAccess == "UNAVAILABLE" && (v.Status == agentmemorycandidate.Candidate || v.Status == agentmemorycandidate.Active || v.Status == agentmemorycandidate.Rejected || v.Status == agentmemorycandidate.Superseded || v.Status == agentmemorycandidate.Expired)
}
func candidateResponse(w http.ResponseWriter, a agentprofile.PrivateAccess, id string, v agentmemorycandidate.Record, e error) {
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if !candidateRecordValid(a, v) || (id != "" && id != v.ID) {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) listOwnMemoryCandidates(w http.ResponseWriter, r *http.Request) {
	a, ok := s.candidateAccess(w, r)
	if !ok || !candidateNoBody(w, r) {
		return
	}
	v, e := s.memoryCandidates.List(r.Context(), a)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if len(v) > 50 {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	seen := map[string]bool{}
	agent := ""
	for _, c := range v {
		if !candidateRecordValid(a, c) || seen[c.ID] || (agent != "" && agent != c.AgentID) {
			memoryFailure(w, agentmemory.ErrUnavailable)
			return
		}
		seen[c.ID] = true
		agent = c.AgentID
	}
	if v == nil {
		v = []agentmemorycandidate.Record{}
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) getOwnMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.candidateAccess(w, r)
	if !ok || !candidateNoBody(w, r) {
		return
	}
	id, ok := candidateID(w, r)
	if !ok {
		return
	}
	v, e := s.memoryCandidates.Read(r.Context(), a, id)
	candidateResponse(w, a, id, v, e)
}
func (s *server) saveOwnMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.candidateAccess(w, r)
	if !ok {
		return
	}
	var d agentmemorycandidate.HumanDraft
	if !candidateDecode(w, r, &d) {
		return
	}
	v, e := s.memoryCandidates.Save(r.Context(), a, d)
	candidateResponse(w, a, "", v, e)
}
func (s *server) previewOwnMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.candidateAccess(w, r)
	if !ok {
		return
	}
	id, ok := candidateID(w, r)
	if !ok {
		return
	}
	var in agentmemorycandidate.HumanPreviewInput
	if !candidateDecode(w, r, &in) {
		return
	}
	v, e := s.memoryCandidates.Preview(r.Context(), a, id, in)
	if e != nil {
		memoryFailure(w, e)
		return
	}
	if v.PreviewID != in.PreviewID || !candidateRecordValid(a, v.Review.Candidate) || v.Review.Candidate.ID != id || v.Review.Candidate.Version != in.ExpectedVersion || v.Review.Purpose != "HUMAN_EXPLICIT_DECLARATION" || v.Review.ExpectedMemoryVersion != 0 || v.Review.PreviousMemory != nil || v.Review.Statement != agentmemorycandidate.Statement(v.Review.Candidate.Category) || v.Review.Clusters < 2 {
		memoryFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) acceptOwnMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.candidateAccess(w, r)
	if !ok {
		return
	}
	id, ok := candidateID(w, r)
	if !ok {
		return
	}
	var in struct {
		PreviewID string `json:"previewId"`
	}
	if !candidateDecode(w, r, &in) {
		return
	}
	v, e := s.memoryCandidates.Accept(r.Context(), a, id, in.PreviewID)
	if e == nil && v.Status != agentmemorycandidate.Active {
		e = agentmemory.ErrUnavailable
	}
	candidateResponse(w, a, id, v, e)
}
func (s *server) rejectOwnMemoryCandidate(w http.ResponseWriter, r *http.Request) {
	a, ok := s.candidateAccess(w, r)
	if !ok {
		return
	}
	id, ok := candidateID(w, r)
	if !ok {
		return
	}
	var in struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	if !candidateDecode(w, r, &in) {
		return
	}
	v, e := s.memoryCandidates.Reject(r.Context(), a, id, in.ExpectedVersion)
	if e == nil && v.Status != agentmemorycandidate.Rejected {
		e = agentmemory.ErrUnavailable
	}
	candidateResponse(w, a, id, v, e)
}
