package httpapi

import (
	"errors"
	apd "github.com/birdtie/birdtie/apps/api/internal/activityparticipationdisclosure"
	"io"
	"net/http"
)

func participationDisclosureFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, apd.ErrInvalid):
		respondError(w, 400, "请重新检查报名披露与有限期限")
	case errors.Is(e, apd.ErrDenied):
		respondError(w, 403, "当前身份不允许操作此报名披露")
	case errors.Is(e, apd.ErrChanged):
		respondError(w, 409, "报名或具体来源版本已变化，请重新检查")
	default:
		respondError(w, 503, "报名披露暂不可用")
	}
}
func (s *server) getOwnParticipationDisclosures(w http.ResponseWriter, r *http.Request) {
	s.readOwnParticipationDisclosures(w, r, false)
}
func (s *server) getOwnParticipationDisclosureOptions(w http.ResponseWriter, r *http.Request) {
	s.readOwnParticipationDisclosures(w, r, true)
}
func (s *server) readOwnParticipationDisclosures(w http.ResponseWriter, r *http.Request, options bool) {
	a, ok := s.privateProfileAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) > 0 {
			participationDisclosureFailure(w, apd.ErrInvalid)
			return
		}
	}
	store, ok := s.privateProfiles.(apd.Store)
	if !ok || store == nil {
		participationDisclosureFailure(w, apd.ErrUnavailable)
		return
	}
	out, e := store.ReadOwnParticipationDisclosures(r.Context(), a, options)
	if e != nil {
		participationDisclosureFailure(w, e)
		return
	}
	if apd.ValidateView(out, a.WorkspacePrincipal.ID) != nil {
		participationDisclosureFailure(w, apd.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
func (s *server) previewOwnParticipationDisclosure(w http.ResponseWriter, r *http.Request) {
	s.mutateOwnParticipationDisclosure(w, r, false)
}
func (s *server) approveOwnParticipationDisclosure(w http.ResponseWriter, r *http.Request) {
	s.mutateOwnParticipationDisclosure(w, r, true)
}
func (s *server) mutateOwnParticipationDisclosure(w http.ResponseWriter, r *http.Request, approve bool) {
	a, ok := s.privateProfileAccess(w, r)
	if !ok {
		return
	}
	if r.Body == nil {
		participationDisclosureFailure(w, apd.ErrInvalid)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 32769))
	if e != nil {
		participationDisclosureFailure(w, apd.ErrInvalid)
		return
	}
	in, token, e := apd.DecodeBody(raw, approve)
	if e != nil {
		participationDisclosureFailure(w, e)
		return
	}
	store, ok := s.privateProfiles.(apd.Store)
	if !ok || store == nil {
		participationDisclosureFailure(w, apd.ErrUnavailable)
		return
	}
	if approve {
		out, e := store.ApproveOwnParticipationDisclosure(r.Context(), a, token)
		if e != nil {
			participationDisclosureFailure(w, e)
			return
		}
		if apd.ValidateView(out, a.WorkspacePrincipal.ID) != nil || len(out.Records) != 1 || out.Truncated {
			participationDisclosureFailure(w, apd.ErrUnavailable)
			return
		}
		respond(w, 200, map[string]any{"data": out})
		return
	}
	out, e := store.PreviewOwnParticipationDisclosure(r.Context(), a, in)
	if e != nil {
		participationDisclosureFailure(w, e)
		return
	}
	if apd.ValidatePreview(out, a.WorkspacePrincipal.ID, in) != nil {
		participationDisclosureFailure(w, apd.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out})
}
