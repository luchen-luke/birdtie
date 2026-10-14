package httpapi

import (
	"errors"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

func visualFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
	case errors.Is(e, av.ErrInvalid):
		respondError(w, 400, "invalid_visual_analysis")
	case errors.Is(e, av.ErrConflict):
		respondError(w, 409, "visual_preview_changed")
	case errors.Is(e, av.ErrDenied):
		respondError(w, 404, "visual_source_unavailable")
	case errors.Is(e, av.ErrBudget):
		respondError(w, 503, "visual_budget_unavailable")
	default:
		respondError(w, 503, "visual_analysis_unavailable")
	}
}
func (s *server) visualAccess(w http.ResponseWriter, r *http.Request) (av.Access, av.Gateway, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		respondError(w, 403, "forbidden")
		return av.Access{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		visualFailure(w, av.ErrInvalid)
		return av.Access{}, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if e != nil {
		visualFailure(w, e)
		return av.Access{}, nil, false
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "forbidden")
		return av.Access{}, nil, false
	}
	if s.agentVisual == nil || nilHumanSelfReviewPort(s.agentVisual) {
		visualFailure(w, av.ErrUnavailable)
		return av.Access{}, nil, false
	}
	return av.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}, s.agentVisual, true
}
func visualBody(w http.ResponseWriter, r *http.Request, preview bool) ([]byte, error) {
	if len(r.Header.Values("Content-Type")) != 1 || r.Body == nil {
		return nil, av.ErrInvalid
	}
	kind, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		return nil, av.ErrInvalid
	}
	for k, v := range params {
		if k != "charset" || v != "utf-8" {
			return nil, av.ErrInvalid
		}
	}
	limit := int64(8192)
	if preview {
		limit = int64(4*((av.MaxInputBytes+2)/3) + 64*1024)
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if e != nil {
		return nil, av.ErrInvalid
	}
	return raw, nil
}

func visualNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if e != nil || len(b) > 0 {
		visualFailure(w, av.ErrInvalid)
		return false
	}
	return true
}
func validVisualPreview(p av.Preview, in av.PreviewInput) bool {
	now := time.Now()
	return p.SchemaVersion == 1 && av.UUID(p.PreviewID) && p.Version == 1 && p.Status == "PREVIEW" && p.ClientDraftID == in.ClientDraftID && p.ClientDraftRevision == in.ClientDraftRevision && p.UserText == in.UserText && p.Purpose == av.Purpose && p.EgressPurpose == av.EgressPurpose && av.Hash(p.PayloadDigest) && !p.MemoryWriteAllowed && !p.PlaceFactWriteAllowed && av.UUID(p.Pixel.ID) && p.Pixel.Revision > 0 && av.Hash(p.Pixel.SHA256) && (p.Pixel.MIME == "image/png" || p.Pixel.MIME == "image/jpeg") && p.Pixel.Width > 0 && p.Pixel.Height > 0 && p.Pixel.Width <= 4096 && p.Pixel.Height <= 4096 && p.Pixel.ByteSize > 0 && p.Pixel.ByteSize <= av.MaxInputBytes && p.Pixel.PreviewContentPath == "/v1/me/agent-visual/previews/"+p.PreviewID+"/pixels" && p.Destination.Provider == "tencent_tokenhub" && p.Destination.Model == "hy-vision-2.0-instruct" && p.Destination.WireContract == "tokenhub.visual.chat-completions.v1" && p.Destination.Region == "APAC" && p.Destination.Retention == "UNKNOWN" && p.Budget.Currency == "CNY" && p.Budget.PriceVersion != "" && p.Budget.InputTokenUpper == 24576 && p.Budget.OutputTokenUpper == 768 && p.Budget.CostMicrosUpper == 210432 && !p.CreatedAt.After(now) && p.ExpiresAt.After(now) && !p.ExpiresAt.After(p.CreatedAt.Add(2*time.Minute)) && !p.ExpiresAt.After(p.Budget.PriceExpiresAt)
}
func validVisualOperation(o av.Operation) bool {
	if o.SchemaVersion != 1 || !av.UUID(o.OperationID) || !av.UUID(o.PreviewID) || o.Version != 1 {
		return false
	}
	if o.Status != "COMPLETED" {
		return (o.Status == "UNKNOWN" || o.Status == "REFUSED" || o.Status == "UNAVAILABLE") && o.Report == nil
	}
	if o.Report == nil || !av.Text(o.Report.Summary, 4096, false) || len(o.Report.Observations) > 32 || len(o.Report.Unknowns) > 16 {
		return false
	}
	for _, x := range o.Report.Observations {
		if !(x.Kind == "VISIBLE_OBJECT" || x.Kind == "VISIBLE_TEXT" || x.Kind == "VISIBLE_SCENE") || !av.Text(x.Value, 2048, false) || x.Confidence.Kind != "QUALITATIVE" || !(x.Confidence.Level == "LOW" || x.Confidence.Level == "MEDIUM" || x.Confidence.Level == "HIGH" || x.Confidence.Level == "UNKNOWN") || x.Source.Type != "APPROVED_PIXELS" || !av.UUID(x.Source.ID) || x.Source.Revision < 1 || !av.Hash(x.Source.SHA256) || x.ObservedAt.IsZero() || x.ObservedAt.After(time.Now()) {
			return false
		}
	}
	for _, x := range o.Report.Unknowns {
		if !av.Text(x, 2048, false) {
			return false
		}
	}
	return true
}
func (s *server) createAgentVisualPreview(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.visualAccess(w, r)
	if !ok {
		return
	}
	raw, e := visualBody(w, r, true)
	if e != nil {
		visualFailure(w, e)
		return
	}
	in, e := av.DecodePreview(raw)
	clear(raw)
	if e != nil {
		visualFailure(w, e)
		return
	}
	p, e := g.Preview(r.Context(), a, in)
	if e != nil {
		visualFailure(w, e)
		return
	}
	if !validVisualPreview(p, in) {
		visualFailure(w, av.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": p})
}
func (s *server) getAgentVisualPixels(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.visualAccess(w, r)
	if !ok || !visualNoBody(w, r) {
		return
	}
	id := r.PathValue("previewID")
	if !av.UUID(id) {
		visualFailure(w, av.ErrInvalid)
		return
	}
	p, b, e := g.Pixels(r.Context(), a, id)
	if e != nil {
		visualFailure(w, e)
		return
	}
	if p.PreviewContentPath != "/v1/me/agent-visual/previews/"+id+"/pixels" || len(b) != p.ByteSize || len(b) < 1 || len(b) > av.MaxInputBytes || av.Digest(b) != p.SHA256 || (p.MIME != "image/jpeg" && p.MIME != "image/png") {
		visualFailure(w, av.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", p.MIME)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	_, _ = w.Write(b)
}
func (s *server) analyzeAgentVisual(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.visualAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("previewID")
	if !av.UUID(id) {
		visualFailure(w, av.ErrInvalid)
		return
	}
	raw, e := visualBody(w, r, false)
	if e != nil {
		visualFailure(w, e)
		return
	}
	in, e := av.DecodeApproval(raw)
	if e != nil {
		visualFailure(w, e)
		return
	}
	out, e := g.Analyze(r.Context(), a, id, in)
	if e != nil {
		visualFailure(w, e)
		return
	}
	if !validVisualOperation(out) || out.OperationID != in.OperationID || out.PreviewID != id || out.Version != in.ExpectedVersion {
		visualFailure(w, av.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
func (s *server) getAgentVisualOperation(w http.ResponseWriter, r *http.Request) {
	a, g, ok := s.visualAccess(w, r)
	if !ok || !visualNoBody(w, r) {
		return
	}
	id := r.PathValue("operationID")
	if !av.UUID(id) {
		visualFailure(w, av.ErrInvalid)
		return
	}
	out, e := g.Operation(r.Context(), a, id)
	if e != nil {
		visualFailure(w, e)
		return
	}
	if !validVisualOperation(out) || out.OperationID != id {
		visualFailure(w, av.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
