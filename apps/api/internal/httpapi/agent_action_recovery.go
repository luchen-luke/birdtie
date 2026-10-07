package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func sandboxRecoveryMissing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func sandboxRecoveryError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
	case errors.Is(e, aa.ErrInvalid):
		respondError(w, 400, "invalid_sandbox_recovery")
	case errors.Is(e, aa.ErrDenied):
		respondError(w, 403, "sandbox_recovery_denied")
	case errors.Is(e, aa.ErrChanged), errors.Is(e, aa.ErrExpired):
		respondError(w, 409, "sandbox_recovery_changed")
	case errors.Is(e, aa.ErrUnknown):
		respondError(w, 503, "sandbox_recovery_unknown")
	default:
		respondError(w, 503, "sandbox_recovery_unavailable")
	}
}

func (s *server) recoverOwnSandboxApproval(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Context().Err() != nil {
		sandboxRecoveryError(w, aa.ErrUnavailable)
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		sandboxRecoveryError(w, aa.ErrDenied)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		sandboxRecoveryError(w, aa.ErrInvalid)
		return
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			sandboxRecoveryError(w, aa.ErrInvalid)
			return
		}
	}
	port := s.sandboxRecovery
	sessions, ok := s.access.(socialnow.HumanSessionStore)
	if sandboxRecoveryMissing(port) || sandboxRecoveryMissing(s.access) || !ok || sandboxRecoveryMissing(sessions) {
		sandboxRecoveryError(w, aa.ErrUnavailable)
		return
	}
	actor, digest, e := s.actor(r, true)
	if e != nil {
		sandboxRecoveryError(w, e)
		return
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if e != nil || p.Type != actorref.Person || agentprofile.ValidatePrivateAccess(a) != nil {
		sandboxRecoveryError(w, aa.ErrDenied)
		return
	}
	id := r.PathValue("approvalID")
	if !agentplanner.ValidID(id) {
		sandboxRecoveryError(w, aa.ErrInvalid)
		return
	}
	out, e := port.RecoverOwn(r.Context(), a, id)
	if e != nil {
		sandboxRecoveryError(w, e)
		return
	}
	if aa.ValidateHumanRecoveryReceipt(out, p, id) != nil {
		sandboxRecoveryError(w, aa.ErrUnavailable)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(encoded)+1 > aa.HumanRecoveryMaxBytes {
		sandboxRecoveryError(w, aa.ErrUnavailable)
		return
	}
	// The native service has already committed its original fence/receipt Tx.
	// Recheck the captured Person session AFTER encoding, before any body bytes.
	// A failed reply check does not undo reconciliation or justify a resend.
	if e = sessions.ValidateHumanSocialResponse(r.Context(), digest, actor); e != nil {
		sandboxRecoveryError(w, e)
		return
	}
	if r.Context().Err() != nil {
		sandboxRecoveryError(w, aa.ErrUnknown)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
