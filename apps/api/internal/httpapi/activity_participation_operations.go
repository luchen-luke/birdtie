package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

func participationOperationMissing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	return r.Kind() == reflect.Pointer && r.IsNil()
}
func participationOperationError(w http.ResponseWriter, e error) bool {
	if e == nil {
		return false
	}
	switch {
	case errors.Is(e, ap.ErrOperationConflict):
		respondError(w, 409, "participation_operation_conflict")
	case errors.Is(e, ap.ErrFull):
		respondError(w, 409, "activity_full")
	case errors.Is(e, ap.ErrUnavailable):
		respondError(w, 409, "activity_unavailable")
	case errors.Is(e, ap.ErrNotFound):
		respondError(w, 404, "not_found")
	default:
		entityActionFailure(w, e)
	}
	return true
}
func (s *server) participationOperationAccess(w http.ResponseWriter, r *http.Request, operationPath bool) (ea.Access, ap.HumanOperationStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := s.participations.(ap.HumanOperationStore)
	if !ok || participationOperationMissing(p) || participationOperationMissing(s.access) {
		entityActionFailure(w, ea.ErrUnavailable)
		return ea.Access{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || !ap.ValidOperationID(r.PathValue("activityID")) || (operationPath && !ap.ValidOperationID(r.PathValue("operationID"))) {
		entityActionFailure(w, ea.ErrInvalid)
		return ea.Access{}, nil, false
	}
	a, d, e := s.humanSocialActor(r, true)
	if e != nil {
		entityActionFailure(w, e)
		return ea.Access{}, nil, false
	}
	if a.AccountType != "person" {
		respondError(w, 403, "person_account_required")
		return ea.Access{}, nil, false
	}
	return ea.Access{Actor: a, SessionDigest: d}, p, true
}
func participationOperationBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		entityActionFailure(w, ea.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil || len(raw) > 4096 {
		entityActionFailure(w, ea.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func participationOperationNoApproval(w http.ResponseWriter, r *http.Request, noBody bool) bool {
	for _, k := range []string{"X-Birdtie-Action-Version", "X-Birdtie-Action-Until", "X-Birdtie-Action-Operation"} {
		if len(r.Header.Values(k)) != 0 {
			entityActionFailure(w, ea.ErrInvalid)
			return false
		}
	}
	if noBody && r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			entityActionFailure(w, ea.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) writeHumanParticipationOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.participationOperationAccess(w, r, false)
	if !ok {
		return
	}
	raw, ok := participationOperationBody(w, r)
	if !ok {
		return
	}
	in, e := ap.DecodeOperation(raw)
	if participationOperationError(w, e) {
		return
	}
	b, ok := entityActionCondition(w, r, ea.Ref{Type: "activity", ID: r.PathValue("activityID")}, ea.Join)
	if !ok {
		return
	}
	if b == nil || b.Operation != in.Operation {
		entityActionFailure(w, ea.ErrInvalid)
		return
	}
	d, e := ap.HumanParticipationRequestDigest(a.Actor.ID, r.PathValue("activityID"), in, *b)
	if participationOperationError(w, e) {
		return
	}
	out, fresh, e := p.WriteHumanParticipationOperation(r.Context(), a, r.PathValue("activityID"), in, *b)
	if participationOperationError(w, e) {
		return
	}
	if out.Operation != in.Operation || out.RequestDigest != d {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	if fresh && out.Status == "COMMITTED" && out.Operation == "JOIN" && out.EffectAt != nil {
		s.recordConversion(r, out.ActivityID, "rsvp", conversionSource(r, "direct"), "rsvp:"+out.EffectID+":"+out.EffectAt.UTC().Format("20060102150405.000000000"))
	}
	status := 200
	if fresh {
		status = 201
	}
	s.participationOperationResponse(w, r, a, p, out, in.OperationID, status)
}
func (s *server) readHumanParticipationOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.participationOperationAccess(w, r, true)
	if !ok || !participationOperationNoApproval(w, r, true) {
		return
	}
	out, e := p.ReadHumanParticipationOperation(r.Context(), a, r.PathValue("activityID"), r.PathValue("operationID"))
	if participationOperationError(w, e) {
		return
	}
	s.participationOperationResponse(w, r, a, p, out, r.PathValue("operationID"), 200)
}
func (s *server) closeHumanParticipationOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.participationOperationAccess(w, r, true)
	if !ok || !participationOperationNoApproval(w, r, false) {
		return
	}
	raw, ok := participationOperationBody(w, r)
	if !ok {
		return
	}
	in, e := ap.DecodeOperationClose(raw)
	if participationOperationError(w, e) {
		return
	}
	out, e := p.CloseHumanParticipationOperation(r.Context(), a, r.PathValue("activityID"), r.PathValue("operationID"), in)
	if participationOperationError(w, e) {
		return
	}
	if out.Operation != in.Operation || out.RequestDigest != in.RequestDigest {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	s.participationClosureResponse(w, r, a, p, out)
}
func (s *server) readHumanParticipationClosure(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.participationOperationAccess(w, r, true)
	if !ok || !participationOperationNoApproval(w, r, true) {
		return
	}
	out, e := p.ReadHumanParticipationClosure(r.Context(), a, r.PathValue("activityID"), r.PathValue("operationID"))
	if participationOperationError(w, e) {
		return
	}
	s.participationClosureResponse(w, r, a, p, out)
}
func (s *server) participationOperationResponse(w http.ResponseWriter, r *http.Request, a ea.Access, p ap.HumanOperationStore, out ap.OperationReceipt, op string, status int) {
	if !ap.ValidOperationReceipt(out, a.Actor.ID, r.PathValue("activityID"), op) {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw) > 4096 {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	if e = p.ValidateHumanParticipationOperationResponse(r.Context(), a, out); participationOperationError(w, e) {
		return
	}
	if r.Context().Err() != nil {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) participationClosureResponse(w http.ResponseWriter, r *http.Request, a ea.Access, p ap.HumanOperationStore, out ap.OperationClosure) {
	if !ap.ValidOperationClosure(out, a.Actor.ID, r.PathValue("activityID"), r.PathValue("operationID")) {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw) > 4096 {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	if e = p.ValidateHumanParticipationClosureResponse(r.Context(), a, out); participationOperationError(w, e) {
		return
	}
	if r.Context().Err() != nil {
		entityActionFailure(w, ea.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
