package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func contactPageFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(e, connection.ErrInvalidContactPage) {
		respondError(w, 400, "invalid_contact_page")
		return
	}
	connectionError(w, e)
}
func (s *server) contactPageAccess(w http.ResponseWriter, r *http.Request, kind string, paged bool) (ea.Access, connection.ContactPagedStore, connection.PageRequest, bool) {
	w.Header().Set("Cache-Control", "no-store")
	in := connection.PageRequest{Limit: connection.DefaultChatPageLimit}
	fail := func() (ea.Access, connection.ContactPagedStore, connection.PageRequest, bool) {
		contactPageFailure(w, connection.ErrInvalidContactPage)
		return ea.Access{}, nil, in, false
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || r.URL.ForceQuery || !paged && r.URL.RawQuery != "" || r.URL.RawQuery != "" && len(values) == 0 {
		return fail()
	}
	for key, v := range values {
		if !paged || len(v) != 1 {
			return fail()
		}
		switch key {
		case "limit":
			in.Limit, e = strconv.Atoi(v[0])
			if e != nil || in.Limit < 1 || in.Limit > connection.MaxChatPageLimit {
				return fail()
			}
		case "cursor":
			if v[0] == "" || len(v[0]) > 1024 {
				return fail()
			}
			in.Cursor = v[0]
		default:
			return fail()
		}
	}
	if nilConnectionRequestReader(s.access) || r.Context().Err() != nil {
		contactPageFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, in, false
	}
	auth := r.Clone(r.Context())
	u := *r.URL
	u.RawQuery = ""
	u.ForceQuery = false
	auth.URL = &u
	a, ok := s.messageWriteAccess(w, auth)
	if !ok || !messagePolicyNoBody(w, r) {
		return ea.Access{}, nil, in, false
	}
	if _, _, e = connection.NormalizeContactPage(in, a.Actor.ID, kind); e != nil {
		return fail()
	}
	p, ok := s.connections.(connection.ContactPagedStore)
	if !ok || nilConnectionRequestReader(p) {
		contactPageFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, in, false
	}
	return a, p, in, true
}
func contactPageResponse[T any](s *server, w http.ResponseWriter, r *http.Request, a ea.Access, p connection.ContactPagedStore, kind string, in connection.PageRequest, out connection.ContactPage[T], row func(T) (string, time.Time, bool)) {
	if out.Items == nil {
		out.Items = []T{}
	}
	if connection.ValidateContactPage(out, in, a.Actor.ID, kind, row) != nil {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	gate, ok := s.access.(socialnow.HumanSessionStore)
	if !ok || nilConnectionRequestReader(gate) {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	if e = gate.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, a.Actor); e == nil {
		e = p.ValidateContactPageResponse(r.Context(), a, kind, in, out)
	}
	if e != nil {
		contactPageFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) listContactTiesPage(w http.ResponseWriter, r *http.Request) {
	a, p, in, ok := s.contactPageAccess(w, r, connection.ContactTies, true)
	if !ok {
		return
	}
	out, e := p.ListContactTiesPage(r.Context(), a, in)
	if e != nil {
		contactPageFailure(w, e)
		return
	}
	contactPageResponse(s, w, r, a, p, connection.ContactTies, in, out, func(t connection.Tie) (string, time.Time, bool) { return connection.ContactTieRow(t, a.Actor.ID) })
}
func (s *server) listContactFollowsPage(w http.ResponseWriter, r *http.Request) {
	a, p, in, ok := s.contactPageAccess(w, r, connection.ContactFollows, true)
	if !ok {
		return
	}
	out, e := p.ListContactFollowsPage(r.Context(), a, in)
	if e != nil {
		contactPageFailure(w, e)
		return
	}
	contactPageResponse(s, w, r, a, p, connection.ContactFollows, in, out, connection.ContactFollowRow)
}
func (s *server) listContactRequestsPage(w http.ResponseWriter, r *http.Request) {
	a, p, in, ok := s.contactPageAccess(w, r, connection.ContactRequests, true)
	if !ok {
		return
	}
	out, e := p.ListContactRequestsPage(r.Context(), a, in)
	if e != nil {
		contactPageFailure(w, e)
		return
	}
	contactPageResponse(s, w, r, a, p, connection.ContactRequests, in, out, func(t connection.Request) (string, time.Time, bool) {
		return connection.ContactRequestRow(t, a.Actor.ID)
	})
}
func (s *server) getContactRequestCurrent(w http.ResponseWriter, r *http.Request) {
	a, p, _, ok := s.contactPageAccess(w, r, connection.ContactRequests, false)
	if !ok {
		return
	}
	id := r.PathValue("requestID")
	if !connection.ValidChatPageID(id) {
		contactPageFailure(w, connection.ErrInvalidContactPage)
		return
	}
	out, e := p.ReadContactRequestCurrent(r.Context(), a, id)
	if e != nil {
		contactPageFailure(w, e)
		return
	}
	_, at, valid := connection.ContactRequestRow(out, a.Actor.ID)
	if !valid || out.ID != id || at.IsZero() {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	gate, ok := s.access.(socialnow.HumanSessionStore)
	if !ok || nilConnectionRequestReader(gate) {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	if e = gate.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, a.Actor); e == nil {
		e = p.ValidateContactRequestResponse(r.Context(), a, out)
	}
	if e != nil {
		contactPageFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		contactPageFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
