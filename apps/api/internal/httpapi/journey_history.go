package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/url"
)

func (s *server) registerJourneyHistoryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /v1/me/journey-history", s.getJourneyHistory)
	m.HandleFunc("GET /v1/me/journey-history/{historyID}", s.getJourneyHistory)
	m.HandleFunc("GET /v1/me/notification-preferences/original", s.getOriginalNotificationPreferences)
}
func (s *server) getOriginalNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	s.serveJourneyHistory(w, r, true)
}
func (s *server) getJourneyHistory(w http.ResponseWriter, r *http.Request) {
	s.serveJourneyHistory(w, r, false)
}
func (s *server) serveJourneyHistory(w http.ResponseWriter, r *http.Request, prefs bool) {
	w.Header().Set("Cache-Control", "no-store")
	values, e := url.ParseQuery(r.URL.RawQuery)
	q := content.JourneyHistoryQuery{Preferences: prefs, ThreadID: r.PathValue("historyID")}
	if e != nil || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	for k, v := range values {
		if prefs || q.ThreadID != "" || len(v) != 1 || v[0] == "" || (k != "journeyId" && k != "cursor") {
			momentControlFailure(w, content.ErrInvalid)
			return
		}
		if k == "journeyId" {
			q.JourneyID = v[0]
		} else {
			q.Cursor = v[0]
		}
	}
	if q.JourneyID != "" && !identity.HumanControlUUID(q.JourneyID) || q.ThreadID != "" && !identity.HumanControlUUID(q.ThreadID) || len(q.Cursor) > 4096 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	auth := r.Clone(r.Context())
	u := *r.URL
	u.RawQuery = ""
	u.ForceQuery = false
	auth.URL = &u
	r, a, d, ok := s.humanControlActor(w, auth)
	if !ok || !humanControlNoBody(w, r) {
		return
	}
	store, ok := s.content.(content.JourneyHistoryStore)
	if !ok || nilHumanSelfReviewPort(store) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	v, e := store.ReadJourneyHistory(r.Context(), d, a, q)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if v.SchemaVersion != content.JourneyHistorySchema || v.Threads == nil || v.Messages == nil || !content.MomentControlHex(v.SourceVersion) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
