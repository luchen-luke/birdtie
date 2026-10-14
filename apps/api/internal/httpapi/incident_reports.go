package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/safety"
)

func (s *server) createIncidentReport(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) listOwnIncidentReports(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) || !humanControlNoBody(w, r) {
		return
	}
	if s.safety == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	items, e := s.safety.ListOwnReports(r.Context(), a.ID)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	original, _ := json.Marshal(items)
	proof := humanControlFunctionProof(func(ctx context.Context) error {
		current, e := s.safety.ListOwnReports(ctx, a.ID)
		if e != nil {
			return identity.ErrControlUnavailable
		}
		raw, e := json.Marshal(current)
		if e != nil || string(raw) != string(original) {
			return identity.ErrControlChanged
		}
		return nil
	})
	s.respondHumanControl(w, r, d, a, map[string]any{"data": items}, proof, true)
}

func (s *server) readReportControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.safety.(safety.ReportControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadReportControl(r.Context(), d, a)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewReportControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	port, ok := s.safety.(safety.ReportControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := safety.DecodeReportControl(raw, false)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	v, e := port.PreviewReportControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, safety.ReportControlDigest(in)) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitReportControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	port, ok := s.safety.(safety.ReportControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := safety.DecodeReportControl(raw, true)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_human_control")
		return
	}
	v, e := port.CommitReportControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "REPORT", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readReportControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyReportControl(w, r, false)
}
func (s *server) stopReportControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyReportControl(w, r, true)
}
func (s *server) historyReportControl(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.safety.(safety.ReportControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	var v identity.HumanControlReceipt
	var e error
	if stop {
		v, e = port.StopReportControlOperation(r.Context(), d, a, r.PathValue("operationID"))
	} else {
		v, e = port.ReadReportControlOperation(r.Context(), d, a, r.PathValue("operationID"))
	}
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "REPORT", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) registerReportControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/report-control", s.readReportControl)
	mux.HandleFunc("POST /v1/me/report-control/previews", s.previewReportControl)
	mux.HandleFunc("PUT /v1/me/report-control/operations/{operationID}", s.commitReportControl)
	mux.HandleFunc("GET /v1/me/report-control/operations/{operationID}", s.readReportControlOperation)
	mux.HandleFunc("POST /v1/me/report-control/operations/{operationID}/stop", s.stopReportControlOperation)
}
