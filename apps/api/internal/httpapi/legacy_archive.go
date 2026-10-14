package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
)

func legacyArchiveFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, legacyarchive.ErrInvalid):
		respondError(w, 400, "invalid_legacy_archive")
	case errors.Is(e, legacyarchive.ErrNotFound):
		respondError(w, 404, "legacy_archive_unavailable")
	default:
		humanControlFailure(w, e)
	}
}

func (s *server) legacyArchivePort(w http.ResponseWriter) (legacyarchive.HumanStore, bool) {
	if s.legacyArchives == nil {
		respondError(w, 503, "legacy_archive_unavailable")
		return nil, false
	}
	return s.legacyArchives, true
}

func (s *server) listLegacyArchives(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	if len(r.URL.RawQuery) > 1536 || r.URL.ForceQuery {
		respondError(w, 400, "invalid_legacy_archive")
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		respondError(w, 400, "invalid_legacy_archive")
		return
	}
	for key, values := range q {
		if (key != "cursor" && key != "kind") || len(values) != 1 {
			respondError(w, 400, "invalid_legacy_archive")
			return
		}
	}
	if values, exists := q["kind"]; exists && values[0] != "note" {
		respondError(w, 400, "invalid_legacy_archive")
		return
	}
	p, ok := s.legacyArchivePort(w)
	if !ok {
		return
	}
	v, e := p.ListHumanLegacyArchives(r.Context(), d, a, q.Get("cursor"))
	if e != nil {
		legacyArchiveFailure(w, e)
		return
	}
	if v.Items == nil || len(v.Items) > legacyarchive.PageSize || len(v.NextCursor) > 1024 || v.ResponseProof == nil {
		respondError(w, 503, "legacy_archive_unavailable")
		return
	}
	seen := map[string]bool{}
	for _, item := range v.Items {
		if legacyarchive.ValidateSummary(item) != nil || seen[item.ID] {
			respondError(w, 503, "legacy_archive_unavailable")
			return
		}
		seen[item.ID] = true
	}
	if len(v.Items) == 0 && v.NextCursor != "" {
		respondError(w, 503, "legacy_archive_unavailable")
		return
	}
	if e = p.FinalizeHumanLegacyArchivePage(r.Context(), d, a, v); e != nil {
		legacyArchiveFailure(w, e)
		return
	}
	// The same original Session and source are checked again after actual JSON
	// encoding. This never returns the importer's raw source or credentials.
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}

func (s *server) readLegacyArchive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlNoBody(w, r) || !humanControlPath(w, r, "archiveID") {
		return
	}
	p, ok := s.legacyArchivePort(w)
	if !ok {
		return
	}
	v, e := p.ReadHumanLegacyArchive(r.Context(), d, a, r.PathValue("archiveID"))
	if e != nil {
		legacyArchiveFailure(w, e)
		return
	}
	if v.ID != r.PathValue("archiveID") || legacyarchive.ValidateDetail(v) != nil || v.ResponseProof == nil {
		respondError(w, 503, "legacy_archive_unavailable")
		return
	}
	if e = p.FinalizeHumanLegacyArchiveRead(r.Context(), d, a, v); e != nil {
		legacyArchiveFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
