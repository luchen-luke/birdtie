package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/url"
)

func (s *server) interactionPort(w http.ResponseWriter) (content.MomentInteractionStore, bool) {
	p, ok := s.content.(content.MomentInteractionStore)
	if !ok || p == nil {
		respondError(w, 503, "moment_interaction_unavailable")
		return nil, false
	}
	return p, true
}
func (s *server) previewMomentInteraction(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	p, ok := s.interactionPort(w)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeMomentInteraction(raw, false)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	v, e := p.PreviewMomentInteraction(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, content.MomentInteractionDigest(in)) || content.MomentInteractionDigest(v.Input) != content.MomentInteractionDigest(in) || !content.MomentControlHex(v.SourceVersion) {
		respondError(w, 503, "moment_interaction_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitMomentInteraction(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	p, ok := s.interactionPort(w)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeMomentInteraction(raw, true)
	if e != nil || in.OperationID != r.PathValue("operationID") {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	v, e := p.CommitMomentInteraction(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !content.ValidMomentInteractionReceipt(v, in.OperationID) || v.Status == "SUCCEEDED" && (v.PayloadDigest != content.MomentInteractionDigest(in) || v.MomentID != in.MomentID || v.Action != in.Action) {
		respondError(w, 503, "moment_interaction_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) historyMomentInteraction(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.interactionPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentInteractionOperation(r.Context(), d, a, r.PathValue("operationID"), stop)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !content.ValidMomentInteractionReceipt(v, r.PathValue("operationID")) {
		respondError(w, 503, "moment_interaction_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readMomentInteractions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("momentID")
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || r.URL.ForceQuery || len(q) > 1 || len(q["cursor"]) > 1 || len(q["commentId"]) > 1 || !uuidPath.MatchString(id) || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	for key := range q {
		if key != "cursor" && key != "commentId" {
			momentControlFailure(w, content.ErrInvalid)
			return
		}
	}
	if !humanControlNoBody(w, r) {
		return
	}
	var a content.PublicMomentAccess
	if len(r.Header.Values("Authorization")) > 0 {
		actor, d, e := s.actor(r, true)
		if e != nil || actor.AccountType != "person" {
			momentControlFailure(w, identity.ErrUnauthorized)
			return
		}
		a = content.PublicMomentAccess{Actor: actor, SessionDigest: d}
	}
	p, ok := s.interactionPort(w)
	if !ok {
		return
	}
	v, e := p.ReadMomentInteractions(r.Context(), a, id, q.Get("cursor"), q.Get("commentId"))
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil || v.ResponseProof == nil {
		momentControlFailure(w, content.ErrUnavailable)
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
func (s *server) registerMomentInteractionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/moments/{momentID}/interactions", s.readMomentInteractions)
	mux.HandleFunc("POST /v1/me/moment-interactions/previews", s.previewMomentInteraction)
	mux.HandleFunc("PUT /v1/me/moment-interactions/operations/{operationID}", s.commitMomentInteraction)
	mux.HandleFunc("GET /v1/me/moment-interactions/operations/{operationID}", func(w http.ResponseWriter, r *http.Request) { s.historyMomentInteraction(w, r, false) })
	mux.HandleFunc("POST /v1/me/moment-interactions/operations/{operationID}/stop", func(w http.ResponseWriter, r *http.Request) { s.historyMomentInteraction(w, r, true) })
}
func (s *server) inboxManagementPort(w http.ResponseWriter) (content.InboxManagementStore, bool) {
	p, ok := s.inbox.(content.InboxManagementStore)
	if !ok || p == nil {
		respondError(w, 503, "inbox_management_unavailable")
		return nil, false
	}
	return p, true
}
func (s *server) previewInboxManagement(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	p, ok := s.inboxManagementPort(w)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeInboxManagement(raw, false)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	v, e := p.PreviewInboxManagement(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, content.InboxManagementDigest(v.Input)) || v.Input.OperationID != in.OperationID || v.Input.Action != in.Action || v.Input.ItemID != in.ItemID || v.Input.Category != in.Category || v.Input.UnreadOnly != in.UnreadOnly || v.Input.Cutoff == nil || !v.Input.Cutoff.Equal(v.Cutoff) || v.Input.SourceVersion != v.SourceVersion || v.Input.Approval != "" || v.Count < 0 || v.Cutoff.IsZero() || !content.MomentControlHex(v.SourceVersion) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitInboxManagement(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	p, ok := s.inboxManagementPort(w)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeInboxManagement(raw, true)
	if e != nil || in.OperationID != r.PathValue("operationID") {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	v, e := p.CommitInboxManagement(r.Context(), d, a, in)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	if v.OperationID != in.OperationID || v.Status != "SUCCEEDED" && v.Status != "STOPPED" || v.Status == "SUCCEEDED" && (v.Action != in.Action || v.PayloadDigest != content.InboxManagementDigest(in) || v.AffectedCount < 0) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) historyInboxManagement(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.inboxManagementPort(w)
	if !ok {
		return
	}
	v, e := p.ReadInboxManagementOperation(r.Context(), d, a, r.PathValue("operationID"), stop)
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readInboxMomentTarget(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "itemID") || !humanControlNoBody(w, r) {
		return
	}
	p, ok := s.inbox.(interface {
		GetInboxMomentTarget(context.Context, [32]byte, identity.Actor, string) (map[string]any, error)
	})
	if !ok {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	first, e := p.GetInboxMomentTarget(r.Context(), d, a, r.PathValue("itemID"))
	if e != nil {
		momentControlFailure(w, e)
		return
	}
	proof := inboxMomentTargetProof{p, d, a, r.PathValue("itemID"), first}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": first}, proof, true)
}

type inboxMomentTargetProof struct {
	store interface {
		GetInboxMomentTarget(context.Context, [32]byte, identity.Actor, string) (map[string]any, error)
	}
	d        [32]byte
	a        identity.Actor
	id       string
	expected map[string]any
}

func (p inboxMomentTargetProof) RevalidateHumanControl(ctx context.Context) error {
	value, e := p.store.GetInboxMomentTarget(ctx, p.d, p.a, p.id)
	if e != nil {
		return e
	}
	before, _ := json.Marshal(p.expected)
	after, _ := json.Marshal(value)
	if string(before) != string(after) {
		return identity.ErrControlChanged
	}
	return nil
}
func (s *server) registerInboxManagementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/me/inbox-management/previews", s.previewInboxManagement)
	mux.HandleFunc("PUT /v1/me/inbox-management/operations/{operationID}", s.commitInboxManagement)
	mux.HandleFunc("GET /v1/me/inbox-management/operations/{operationID}", func(w http.ResponseWriter, r *http.Request) { s.historyInboxManagement(w, r, false) })
	mux.HandleFunc("POST /v1/me/inbox-management/operations/{operationID}/stop", func(w http.ResponseWriter, r *http.Request) { s.historyInboxManagement(w, r, true) })
	mux.HandleFunc("GET /v1/me/inbox/{itemID}/moment-target", s.readInboxMomentTarget)
	mux.HandleFunc("GET /v1/me/inbox/{itemID}/person-target", s.readInboxPersonTarget)
}
