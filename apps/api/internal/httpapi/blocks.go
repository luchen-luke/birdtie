package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) listBlocks(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	blocks, err := s.access.ListBlocks(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": blocks})
}

func (s *server) blockAccount(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	var input struct {
		AccountID string `json:"accountId"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !uuidPath.MatchString(input.AccountID) {
		respondError(w, http.StatusBadRequest, "invalid_account_id")
		return
	}
	err = s.access.BlockAccount(r.Context(), actor.ID, input.AccountID)
	switch {
	case errors.Is(err, identity.ErrInvalidGrant):
		respondError(w, http.StatusBadRequest, "cannot_block_self")
	case errors.Is(err, identity.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	case err != nil:
		serverError(w, err)
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) unblockAccount(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	targetID := r.PathValue("accountID")
	if !uuidPath.MatchString(targetID) {
		respondError(w, http.StatusBadRequest, "invalid_account_id")
		return
	}
	err = s.access.UnblockAccount(r.Context(), actor.ID, targetID)
	if errors.Is(err, identity.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
