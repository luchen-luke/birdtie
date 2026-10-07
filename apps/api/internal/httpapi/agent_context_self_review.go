package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// This guard is deliberately local. Missing native read capability is not
// replaced by a Task purpose grant, owner-only lookup or model permission.
func nilHumanSelfReviewPort(p any) bool {
	if p == nil {
		return true
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Chan, reflect.Slice:
		return v.IsNil()
	}
	return false
}
func humanSelfReviewFailure(w http.ResponseWriter, e error) {
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return
	}
	contextBuilderHTTPFailure(w, e)
}
func (s *server) humanSelfReviewContext(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Fixed at request entry: slow authentication/resolve/build cannot restart it.
	deadline := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond)
	if r.Context().Err() != nil {
		humanSelfReviewFailure(w, acb.ErrUnavailable)
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		humanSelfReviewFailure(w, acb.ErrDenied)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		humanSelfReviewFailure(w, acb.ErrInvalid)
		return
	}
	port, ok := s.catalog.(acb.Store)
	if !ok || nilHumanSelfReviewPort(port) || nilHumanSelfReviewPort(s.access) {
		humanSelfReviewFailure(w, acb.ErrUnavailable)
		return
	}
	actor, digest, e := s.actor(r, true)
	if e != nil {
		humanSelfReviewFailure(w, e)
		return
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if e != nil || principal.Type != actorref.Person || agentprofile.ValidatePrivateAccess(access) != nil {
		humanSelfReviewFailure(w, acb.ErrDenied)
		return
	}
	raw, ok := contextPurposeBody(w, r, "profileFields", "memoryIds", "policyFamilies")
	if !ok {
		return
	}
	var selection acb.HumanSelfReviewSelection
	if json.Unmarshal(raw, &selection) != nil || selection.ProfileFields == nil || selection.MemoryIDs == nil || selection.PolicyFamilies == nil {
		humanSelfReviewFailure(w, acb.ErrInvalid)
		return
	}
	service, e := acb.NewService(port)
	if e != nil {
		humanSelfReviewFailure(w, e)
		return
	}
	agent, e := service.Resolve(r.Context(), access)
	if e != nil {
		humanSelfReviewFailure(w, e)
		return
	}
	requestID := w.Header().Get("X-Request-ID")
	if requestID == "" {
		requestID = "human-self-review"
	}
	request := acb.Request{Access: access, Agent: agent, RequestID: requestID, Mode: acb.HumanSelfReview, Selection: acb.ExactSelfReview,
		ProfileFields: selection.ProfileFields, MemoryIDs: selection.MemoryIDs, PolicyFamilies: selection.PolicyFamilies, DeadlineAt: deadline}
	built, e := service.Build(r.Context(), request)
	if e != nil {
		humanSelfReviewFailure(w, e)
		return
	}
	view, e := acb.HumanSelfReviewView(built)
	if e != nil {
		humanSelfReviewFailure(w, e)
		return
	}
	// Encode the complete reviewed response BEFORE the original final native
	// current-source/session/clock check. Never stream partial private content.
	encoded, e := json.Marshal(map[string]any{"data": view})
	if e != nil || len(encoded)+1 > acb.MaxHumanSelfReviewWireBytes {
		humanSelfReviewFailure(w, acb.ErrUnavailable)
		return
	}
	if _, e = service.RevalidateOwn(r.Context(), access, built); e != nil {
		humanSelfReviewFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		humanSelfReviewFailure(w, acb.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(encoded, '\n'))
}
