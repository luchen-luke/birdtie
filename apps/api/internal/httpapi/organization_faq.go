package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/agentorganization"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

func faqError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, organization.ErrForbidden):
		respondError(w, http.StatusForbidden, "organization_admin_required")
	case errors.Is(err, organization.ErrNotFound), errors.Is(err, organization.ErrFAQNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, organization.ErrFAQConflict):
		respondError(w, http.StatusConflict, "faq_conflict")
	default:
		serverError(w, err)
	}
}

func validFAQInput(input *organization.FAQInput) bool {
	input.Question = strings.TrimSpace(input.Question)
	input.Answer = strings.TrimSpace(input.Answer)
	return len([]rune(input.Question)) >= 2 && len([]rune(input.Question)) <= 300 &&
		len([]rune(input.Answer)) >= 1 && len([]rune(input.Answer)) <= 3000
}

func (s *server) faqAdmin(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", "", false
	}
	id := r.PathValue("organizationID")
	if actor.AccountType != "person" || s.faqs == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", "", false
	}
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return "", "", false
	}
	return actor.ID, id, true
}

func (s *server) listOrganizationFAQs(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "organizationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.faqs.(organization.FAQControlStore)
	if !ok {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadFAQControl(r.Context(), d, a, r.PathValue("organizationID"))
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v.Items}, v.ResponseProof, true)
}

func (s *server) decodeFAQ(w http.ResponseWriter, r *http.Request) (organization.FAQInput, bool) {
	var input organization.FAQInput
	if !decodeStrictJSON(w, r, &input) {
		return input, false
	}
	if !validFAQInput(&input) {
		respondError(w, http.StatusBadRequest, "invalid_faq")
		return input, false
	}
	return input, true
}

func (s *server) createOrganizationFAQ(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) updateOrganizationFAQ(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) deleteOrganizationFAQ(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

var errOrganizationSessionUnavailable = errors.New("organization session capability unavailable")

// Same canonical bearer/digest and native Session source; no caller-supplied
// account selector, role, grant or bypass for an unsupported access adapter.
func (s *server) organizationAgentActor(r *http.Request) (identity.Actor, [32]byte, error) {
	var zero [32]byte
	headers := r.Header.Values("Authorization")
	if len(headers) == 0 {
		return identity.Actor{}, zero, nil
	}
	if len(headers) != 1 {
		return identity.Actor{}, zero, identity.ErrUnauthorized
	}
	digest, e := identity.ParseBearer(headers[0])
	if e != nil {
		return identity.Actor{}, zero, e
	}
	access, ok := s.access.(interface {
		AuthenticateOrganizationAgent(context.Context, [32]byte) (identity.Actor, error)
	})
	if !ok {
		return identity.Actor{}, zero, errOrganizationSessionUnavailable
	}
	actor, e := access.AuthenticateOrganizationAgent(r.Context(), digest)
	return actor, digest, e
}

func (s *server) askOrganizationAgent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	actor, digest, err := s.organizationAgentActor(r)
	if errors.Is(err, errOrganizationSessionUnavailable) {
		respondError(w, http.StatusServiceUnavailable, "organization_agent_unavailable")
		return
	}
	if authFailed(w, err) {
		return
	}
	id := strings.ToLower(r.PathValue("organizationID"))
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return
	}
	if s.faqs == nil {
		respondError(w, http.StatusServiceUnavailable, "organization_agent_unavailable")
		return
	}
	media, _, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typeErr != nil || media != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if readErr != nil {
		respondError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	query, decodeErr := agentorganization.DecodeQuery(body)
	if decodeErr != nil {
		respondError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	answer, err := s.faqs.AnswerOrganization(r.Context(), id, actor.ID, query)
	if err != nil {
		faqError(w, err)
		return
	}
	if err = r.Context().Err(); err != nil {
		respondError(w, http.StatusServiceUnavailable, "organization_agent_unavailable")
		return
	}
	if digest != ([32]byte{}) {
		guard, ok := s.access.(interface {
			ValidateOrganizationAgentSession(context.Context, [32]byte, identity.Actor) error
		})
		if !ok {
			respondError(w, http.StatusServiceUnavailable, "organization_agent_unavailable")
			return
		}
		if authFailed(w, guard.ValidateOrganizationAgentSession(r.Context(), digest, actor)) {
			return
		}
	}
	if !agentorganization.ValidAnswer(answer, id) {
		respondError(w, http.StatusServiceUnavailable, "organization_agent_unavailable")
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": answer})
}

func (s *server) readFAQControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "organizationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.faqs.(organization.FAQControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadFAQControl(r.Context(), d, a, r.PathValue("organizationID"))
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewFAQControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "organizationID") {
		return
	}
	port, ok := s.faqs.(organization.FAQControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := organization.DecodeFAQControl(raw, r.PathValue("organizationID"), false)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	v, e := port.PreviewFAQControl(r.Context(), d, a, r.PathValue("organizationID"), in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, organization.FAQControlDigest(r.PathValue("organizationID"), in)) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitFAQControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID", "organizationID") {
		return
	}
	port, ok := s.faqs.(organization.FAQControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := organization.DecodeFAQControl(raw, r.PathValue("organizationID"), true)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_human_control")
		return
	}
	v, e := port.CommitFAQControl(r.Context(), d, a, r.PathValue("organizationID"), in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "FAQ", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readFAQControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyFAQControl(w, r, false)
}
func (s *server) stopFAQControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyFAQControl(w, r, true)
}
func (s *server) historyFAQControl(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID", "organizationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.faqs.(organization.FAQControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	var v identity.HumanControlReceipt
	var e error
	if stop {
		v, e = port.StopFAQControlOperation(r.Context(), d, a, r.PathValue("organizationID"), r.PathValue("operationID"))
	} else {
		v, e = port.ReadFAQControlOperation(r.Context(), d, a, r.PathValue("organizationID"), r.PathValue("operationID"))
	}
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "FAQ", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) registerFAQControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/faq-control", s.readFAQControl)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/faq-control/previews", s.previewFAQControl)
	mux.HandleFunc("PUT /v1/me/organizations/{organizationID}/faq-control/operations/{operationID}", s.commitFAQControl)
	mux.HandleFunc("GET /v1/me/organizations/{organizationID}/faq-control/operations/{operationID}", s.readFAQControlOperation)
	mux.HandleFunc("POST /v1/me/organizations/{organizationID}/faq-control/operations/{operationID}/stop", s.stopFAQControlOperation)
}
