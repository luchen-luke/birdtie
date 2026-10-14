package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (s *server) listOwnAgentRunPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.URL.RawQuery) > ar.MaxPageCursor+32 || r.URL.ForceQuery && r.URL.RawQuery == "" {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q) > 1 {
		agentRunHTTPError(w, ar.ErrInvalid)
		return
	}
	cursor := ""
	for k, v := range q {
		if k != "cursor" || len(v) != 1 || !ar.PageCursorShape(v[0]) {
			agentRunHTTPError(w, ar.ErrInvalid)
			return
		}
		cursor = v[0]
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			agentRunHTTPError(w, ar.ErrInvalid)
			return
		}
	}
	g, ok := s.agentRuns.(ar.PageGateway)
	if !ok || nilHumanSelfReviewPort(g) {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		agentRunHTTPError(w, ar.ErrDenied)
		return
	}
	headers := r.Header.Values("Authorization")
	if len(headers) != 1 {
		respondError(w, 401, "unauthorized")
		return
	}
	digest, e := identity.ParseBearer(headers[0])
	if e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			respondError(w, 401, "unauthorized")
		} else {
			agentRunHTTPError(w, e)
		}
		return
	}
	actor, e := g.AuthenticateRunPage(r.Context(), digest)
	if e != nil {
		if errors.Is(e, identity.ErrUnauthorized) {
			respondError(w, 401, "unauthorized")
		} else {
			agentRunHTTPError(w, e)
		}
		return
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if e != nil || principal.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		agentRunHTTPError(w, ar.ErrDenied)
		return
	}
	p, e := g.ReadOwnRunPage(r.Context(), a, cursor)
	if e != nil {
		agentRunHTTPError(w, e)
		return
	}
	if ar.ValidatePage(p, a.WorkspacePrincipal.ID, time.Now()) != nil {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	encoded, e := json.Marshal(p)
	if e != nil {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	var frozen ar.Page
	if json.Unmarshal(encoded, &frozen) != nil {
		agentRunHTTPError(w, ar.ErrUnavailable)
		return
	}
	frozen.Proof = p.Proof
	if e = g.RevalidateOwnRunPage(r.Context(), a, frozen); e != nil {
		agentRunHTTPError(w, e)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(encoded)
}
