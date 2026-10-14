package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func activityJoinHTTPError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, aa.ErrInvalid):
		respondError(w, 400, "invalid_agent_activity_join")
	case errors.Is(e, aa.ErrDenied):
		respondError(w, 403, "agent_activity_join_denied")
	case errors.Is(e, aa.ErrChanged), errors.Is(e, aa.ErrExpired), errors.Is(e, aa.ErrConsumed), errors.Is(e, aa.ErrApproval):
		respondError(w, 409, "agent_activity_join_changed")
	case errors.Is(e, ap.ErrFull):
		respondError(w, 409, "activity_full")
	case errors.Is(e, ap.ErrUnavailable):
		respondError(w, 409, "activity_unavailable")
	case errors.Is(e, aa.ErrUnknown):
		respondError(w, 503, "agent_activity_join_unknown")
	default:
		respondError(w, 503, "agent_activity_join_unavailable")
	}
}
func (s *server) activityJoinAccess(w http.ResponseWriter, r *http.Request, approval bool) (agentprofile.PrivateAccess, aa.ActivityJoinHumanGateway, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		activityJoinHTTPError(w, aa.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		activityJoinHTTPError(w, aa.ErrDenied)
		return agentprofile.PrivateAccess{}, nil, false
	}
	g := s.activityJoin
	if sandboxRecoveryMissing(g) {
		activityJoinHTTPError(w, aa.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if approval && !agentplanner.ValidID(r.PathValue("approvalID")) {
		activityJoinHTTPError(w, aa.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if e != nil {
		activityJoinHTTPError(w, e)
		return agentprofile.PrivateAccess{}, nil, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		activityJoinHTTPError(w, aa.ErrDenied)
		return a, nil, false
	}
	return a, g, true
}
func activityJoinHTTPBody(w http.ResponseWriter, r *http.Request, empty bool) ([]byte, bool) {
	limit := int64(4097)
	if empty {
		limit = 1
	}
	if r.Body == nil {
		if empty {
			return nil, true
		}
		activityJoinHTTPError(w, aa.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, limit))
	if e != nil || len(raw) > 4096 || empty && len(raw) != 0 {
		activityJoinHTTPError(w, aa.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func (s *server) activityJoinPreviewResponse(w http.ResponseWriter, r *http.Request, a agentprofile.PrivateAccess, g aa.ActivityJoinHumanGateway, v aa.ActivityJoinPreview, status int) {
	if e := aa.ValidateActivityJoinPreview(v, a.WorkspacePrincipal); e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": v})
	if e != nil || len(encoded)+1 > aa.ActivityJoinMaxBytes {
		activityJoinHTTPError(w, aa.ErrUnavailable)
		return
	}
	var out struct {
		Data aa.ActivityJoinPreview `json:"data"`
	}
	if json.Unmarshal(encoded, &out) != nil {
		activityJoinHTTPError(w, aa.ErrUnavailable)
		return
	}
	out.Data.Proof = v.Proof
	if e = g.ValidatePreviewOwn(r.Context(), a, out.Data); e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	if r.Context().Err() != nil {
		activityJoinHTTPError(w, aa.ErrUnknown)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(append(encoded, '\n'))
}
func (s *server) activityJoinReceiptResponse(w http.ResponseWriter, r *http.Request, a agentprofile.PrivateAccess, g aa.ActivityJoinHumanGateway, v aa.ActivityJoinReceipt) {
	if e := aa.ValidateActivityJoinReceipt(v, a.WorkspacePrincipal, r.PathValue("approvalID")); e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": v})
	if e != nil || len(encoded)+1 > aa.ActivityJoinMaxBytes {
		activityJoinHTTPError(w, aa.ErrUnavailable)
		return
	}
	var out struct {
		Data aa.ActivityJoinReceipt `json:"data"`
	}
	if json.Unmarshal(encoded, &out) != nil {
		activityJoinHTTPError(w, aa.ErrUnavailable)
		return
	}
	out.Data.Proof = v.Proof
	if e = g.ValidateReceiptOwn(r.Context(), a, out.Data); e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	if r.Context().Err() != nil {
		activityJoinHTTPError(w, aa.ErrUnknown)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	w.Write(append(encoded, '\n'))
}
func (s *server) previewOwnAgentActivityJoin(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.activityJoinAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := activityJoinHTTPBody(w, r, false)
	if !ok {
		return
	}
	p, e := aa.DecodeActivityJoinInput(raw)
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	v, e := g.PreviewOwn(r.Context(), a, p)
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	s.activityJoinPreviewResponse(w, r, a, g, v, 201)
}
func (s *server) confirmOwnAgentActivityJoin(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.activityJoinAccess(w, r, true)
	if !ok {
		return
	}
	raw, ok := activityJoinHTTPBody(w, r, false)
	if !ok {
		return
	}
	d, e := aa.DecodeActivityJoinDigest(raw)
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	v, e := g.ConfirmOwn(r.Context(), a, r.PathValue("approvalID"), d)
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	s.activityJoinPreviewResponse(w, r, a, g, v, 200)
}
func (s *server) executeOwnAgentActivityJoin(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.activityJoinAccess(w, r, true)
	if !ok {
		return
	}
	raw, ok := activityJoinHTTPBody(w, r, false)
	if !ok {
		return
	}
	d, e := aa.DecodeActivityJoinDigest(raw)
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	v, e := g.ExecuteOwn(r.Context(), a, r.PathValue("approvalID"), d)
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	s.activityJoinReceiptResponse(w, r, a, g, v)
}
func (s *server) reconcileOwnAgentActivityJoin(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.activityJoinAccess(w, r, true)
	if !ok {
		return
	}
	if _, ok = activityJoinHTTPBody(w, r, true); !ok {
		return
	}
	v, e := g.ReconcileOwn(r.Context(), a, r.PathValue("approvalID"))
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	s.activityJoinReceiptResponse(w, r, a, g, v)
}
func (s *server) revokeOwnAgentActivityJoin(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.activityJoinAccess(w, r, true)
	if !ok {
		return
	}
	if _, ok = activityJoinHTTPBody(w, r, true); !ok {
		return
	}
	v, e := g.RevokeOwn(r.Context(), a, r.PathValue("approvalID"))
	if e != nil {
		activityJoinHTTPError(w, e)
		return
	}
	s.activityJoinPreviewResponse(w, r, a, g, v, 200)
}
