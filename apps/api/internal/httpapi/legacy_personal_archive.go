package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"net/http"
	"net/url"
)

func personalArchiveFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, legacycollections.ErrInvalid) {
		respondError(w, 400, "invalid_personal_archive")
		return
	}
	if errors.Is(e, legacycollections.ErrNotFound) {
		respondError(w, 404, "personal_archive_unavailable")
		return
	}
	humanControlFailure(w, e)
}
func (s *server) listLegacyPersonalArchives(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	if r.URL.ForceQuery || len(r.URL.RawQuery) > 1536 {
		personalArchiveFailure(w, legacycollections.ErrInvalid)
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		personalArchiveFailure(w, legacycollections.ErrInvalid)
		return
	}
	for key, v := range q {
		if (key != "kind" && key != "cursor") || len(v) != 1 {
			personalArchiveFailure(w, legacycollections.ErrInvalid)
			return
		}
	}
	p, ok := s.catalog.(legacycollections.HumanStore)
	if !ok || nilConnectionRequestReader(p) {
		respondError(w, 503, "personal_archive_unavailable")
		return
	}
	v, e := p.ListHumanLegacyPersonalArchives(r.Context(), d, a, q.Get("kind"), q.Get("cursor"))
	if e != nil {
		personalArchiveFailure(w, e)
		return
	}
	if v.Items == nil || len(v.Items) > 20 || v.ResponseProof == nil {
		respondError(w, 503, "personal_archive_unavailable")
		return
	}
	seen := map[string]bool{}
	for _, item := range v.Items {
		if legacycollections.ValidateSummary(item) != nil || seen[item.ID] {
			respondError(w, 503, "personal_archive_unavailable")
			return
		}
		seen[item.ID] = true
	}
	if e = p.FinalizeHumanLegacyPersonalArchivePage(r.Context(), d, a, v); e != nil {
		personalArchiveFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) readLegacyPersonalArchive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		personalArchiveFailure(w, legacycollections.ErrInvalid)
		return
	}
	p, ok := s.catalog.(legacycollections.HumanStore)
	if !ok || nilConnectionRequestReader(p) {
		respondError(w, 503, "personal_archive_unavailable")
		return
	}
	v, e := p.ReadHumanLegacyPersonalArchive(r.Context(), d, a, r.PathValue("archiveID"))
	if e != nil {
		personalArchiveFailure(w, e)
		return
	}
	if legacycollections.ValidateDetail(v) != nil || v.ResponseProof == nil {
		respondError(w, 503, "personal_archive_unavailable")
		return
	}
	if e = p.FinalizeHumanLegacyPersonalArchiveDetail(r.Context(), d, a, v); e != nil {
		personalArchiveFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
