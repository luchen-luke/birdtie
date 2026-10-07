package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// Human review only. This port cannot reserve, begin, settle or dispatch a
// request, register prices, change quotas or manufacture an AgentRun.
type humanModelEgressStore interface {
	PreviewOwnModelEgress(context.Context, agentevent.Access, modelegressbudget.PreviewInput) (modelegressbudget.Preview, error)
	ApproveOwnModelEgress(context.Context, agentevent.Access, string, string) error
	RevokeOwnModelEgress(context.Context, agentevent.Access, string) error
	ReadOwnModelBudget(context.Context, agentevent.Access, string, string) ([]modelegressbudget.BudgetView, error)
}

type humanModelEgressPreview struct {
	SchemaVersion        string                 `json:"schemaVersion"`
	Status               string                 `json:"status"`
	ID                   string                 `json:"id"`
	RequestDigest        string                 `json:"requestDigest"`
	Scope                string                 `json:"scope"`
	Purpose              string                 `json:"purpose"`
	PriceVersion         string                 `json:"priceVersion"`
	Evidence             string                 `json:"evidence"`
	Currency             string                 `json:"currency"`
	Destination          modelcapability.Key    `json:"destination"`
	Region               modelcapability.Region `json:"region"`
	Retention            string                 `json:"retention"`
	Request              modelgateway.Request   `json:"request"`
	Upper                humanModelBudgetAmount `json:"upper"`
	ExpiresAt            time.Time              `json:"expiresAt"`
	PriceExpiresAt       time.Time              `json:"priceExpiresAt"`
	InputMicrosPerToken  int64                  `json:"inputMicrosPerToken"`
	OutputMicrosPerToken int64                  `json:"outputMicrosPerToken"`
	ModelAccess          string                 `json:"modelAccess"`
}
type humanModelBudgetAmount struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	CostMicros   int64 `json:"costMicros"`
}
type humanModelBudgetLimits struct {
	Requests     int64 `json:"requests"`
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	CostMicros   int64 `json:"costMicros"`
}
type humanModelBudgetView struct {
	Scope     string                 `json:"scope"`
	Limits    humanModelBudgetLimits `json:"limits"`
	Allocated humanModelBudgetLimits `json:"allocated"`
	Currency  string                 `json:"currency"`
}

func humanBudgetLimits(v modelegressbudget.Limits) humanModelBudgetLimits {
	return humanModelBudgetLimits{v.Requests, v.InputTokens, v.OutputTokens, v.CostMicros}
}
func humanEgressDisplay(p modelegressbudget.Preview) (humanModelEgressPreview, error) {
	d, e := p.Display()
	if e != nil {
		return humanModelEgressPreview{}, e
	}
	return humanModelEgressPreview{SchemaVersion: modelegressbudget.SchemaVersion, Status: p.Status, ID: d.ID, RequestDigest: d.RequestDigest, Scope: d.Scope, Purpose: d.Purpose, PriceVersion: d.PriceVersion, Evidence: d.Evidence, Currency: d.Currency, Destination: d.Destination, Region: d.Region, Retention: d.Retention, Request: d.Request, Upper: humanModelBudgetAmount{d.Upper.InputTokens, d.Upper.OutputTokens, d.Upper.CostMicros}, ExpiresAt: d.ExpiresAt, PriceExpiresAt: d.PriceExpiresAt, InputMicrosPerToken: d.InputMicrosPerToken, OutputMicrosPerToken: d.OutputMicrosPerToken, ModelAccess: "UNAVAILABLE"}, nil
}
func modelEgressHTTPFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, modelegressbudget.ErrInvalid):
		respondError(w, 400, "invalid_model_egress_request")
	case errors.Is(e, modelegressbudget.ErrDenied):
		respondError(w, 403, "model_egress_denied")
	case errors.Is(e, modelegressbudget.ErrConflict):
		respondError(w, 409, "model_egress_changed")
	case errors.Is(e, modelegressbudget.ErrBudget):
		respondError(w, 409, "model_budget_insufficient")
	default:
		respondError(w, 503, "model_egress_unavailable")
	}
}
func (s *server) humanModelEgressAccess(w http.ResponseWriter, r *http.Request) (agentevent.Access, humanModelEgressStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		modelEgressHTTPFailure(w, modelegressbudget.ErrDenied)
		return agentevent.Access{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		modelEgressHTTPFailure(w, modelegressbudget.ErrInvalid)
		return agentevent.Access{}, nil, false
	}
	if s.access == nil {
		modelEgressHTTPFailure(w, modelegressbudget.ErrUnavailable)
		return agentevent.Access{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if errors.Is(e, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
		return agentevent.Access{}, nil, false
	}
	if e != nil {
		modelEgressHTTPFailure(w, modelegressbudget.ErrUnavailable)
		return agentevent.Access{}, nil, false
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if e != nil || principal.Type != actorref.Person {
		modelEgressHTTPFailure(w, modelegressbudget.ErrDenied)
		return agentevent.Access{}, nil, false
	}
	port, ok := s.catalog.(humanModelEgressStore)
	if !ok || port == nil {
		modelEgressHTTPFailure(w, modelegressbudget.ErrUnavailable)
		return agentevent.Access{}, nil, false
	}
	return agentevent.Access{SessionDigest: digest}, port, true
}
func (s *server) previewModelEgress(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.humanModelEgressAccess(w, r)
	if !ok {
		return
	}
	raw, ok := contextPurposeBody(w, r, "rootTraceId", "taskId", "priceVersion", "maxOutputTokens", "deadlineAt")
	if !ok {
		return
	}
	var in struct {
		RootTraceID     string    `json:"rootTraceId"`
		TaskID          string    `json:"taskId"`
		PriceVersion    string    `json:"priceVersion"`
		MaxOutputTokens int       `json:"maxOutputTokens"`
		DeadlineAt      time.Time `json:"deadlineAt"`
	}
	if json.Unmarshal(raw, &in) != nil || !uuidPath.MatchString(in.RootTraceID) || !uuidPath.MatchString(in.TaskID) || !modelconfiguration.ValidVersion(in.PriceVersion) || in.MaxOutputTokens < 1 || in.MaxOutputTokens > 4096 || in.DeadlineAt.IsZero() {
		modelEgressHTTPFailure(w, modelegressbudget.ErrInvalid)
		return
	}
	p, e := port.PreviewOwnModelEgress(r.Context(), a, modelegressbudget.PreviewInput{RootTraceID: in.RootTraceID, TaskID: in.TaskID, PriceVersion: in.PriceVersion, MaxOutputTokens: in.MaxOutputTokens, DeadlineAt: in.DeadlineAt})
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	d, e := humanEgressDisplay(p)
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": d})
}
func (s *server) approveModelEgress(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.humanModelEgressAccess(w, r)
	if !ok {
		return
	}
	raw, ok := contextPurposeBody(w, r, "previewId", "requestDigest")
	if !ok {
		return
	}
	var in struct {
		PreviewID string `json:"previewId"`
		Digest    string `json:"requestDigest"`
	}
	if json.Unmarshal(raw, &in) != nil || !uuidPath.MatchString(in.PreviewID) || !modelconfiguration.ValidDigest(in.Digest) {
		modelEgressHTTPFailure(w, modelegressbudget.ErrInvalid)
		return
	}
	if e := port.ApproveOwnModelEgress(r.Context(), a, in.PreviewID, in.Digest); e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": map[string]string{"previewId": in.PreviewID, "status": "APPROVED", "evidence": modelegressbudget.LocalPrice, "modelAccess": "UNAVAILABLE"}})
}
func (s *server) revokeModelEgress(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.humanModelEgressAccess(w, r)
	if !ok || !contextPurposeNoBody(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !uuidPath.MatchString(id) {
		modelEgressHTTPFailure(w, modelegressbudget.ErrInvalid)
		return
	}
	if e := port.RevokeOwnModelEgress(r.Context(), a, id); e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": map[string]string{"previewId": id, "status": "REVOKED", "modelAccess": "UNAVAILABLE"}})
}
func (s *server) readModelEgressBudget(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.humanModelEgressAccess(w, r)
	if !ok || !contextPurposeNoBody(w, r) {
		return
	}
	root, task := r.PathValue("rootID"), r.PathValue("taskID")
	if !uuidPath.MatchString(root) || !uuidPath.MatchString(task) {
		modelEgressHTTPFailure(w, modelegressbudget.ErrInvalid)
		return
	}
	views, e := port.ReadOwnModelBudget(r.Context(), a, root, task)
	if e != nil {
		modelEgressHTTPFailure(w, e)
		return
	}
	out := make([]humanModelBudgetView, 0, len(views))
	for _, v := range views {
		out = append(out, humanModelBudgetView{v.Scope, humanBudgetLimits(v.Limits), humanBudgetLimits(v.Allocated), v.Currency})
	}
	respond(w, 200, map[string]any{"data": out, "modelAccess": "UNAVAILABLE"})
}
