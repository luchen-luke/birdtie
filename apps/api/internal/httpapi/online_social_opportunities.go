package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	oso "github.com/birdtie/birdtie/apps/api/internal/onlinesocialopportunity"
	"io"
	"net/http"
)

func onlineSocialWire(r *http.Request) error {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		return oso.ErrInvalid
	}
	if r.Body != nil {
		raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(raw) != 0 {
			return oso.ErrInvalid
		}
	}
	return nil
}
func onlineSocialFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "online_discovery_unavailable", "线上发现暂不可用，请稍后重试。"
	switch {
	case errors.Is(e, oso.ErrInvalid):
		status, code, message = 400, "invalid_online_discovery", "请重新选择当前线上意图。"
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "请重新登录后打开线上发现。"
	case errors.Is(e, oso.ErrDenied):
		status, code, message = 403, "online_discovery_denied", "当前身份或线上意图不可用，请重新读取。"
	case errors.Is(e, oso.ErrChanged):
		status, code, message = 409, "online_discovery_changed", "意图或来源已变化，请重新读取。"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *server) getOwnOnlineSocialOpportunityOptions(w http.ResponseWriter, r *http.Request) {
	s.getOnlineSocialDiscovery(w, r, true)
}
func (s *server) getOwnOnlineSocialOpportunities(w http.ResponseWriter, r *http.Request) {
	s.getOnlineSocialDiscovery(w, r, false)
}
func (s *server) getOnlineSocialDiscovery(w http.ResponseWriter, r *http.Request, options bool) {
	w.Header().Set("Cache-Control", "no-store")
	if e := onlineSocialWire(r); e != nil {
		onlineSocialFailure(w, e)
		return
	}
	id := r.PathValue("intentID")
	if !options && !businessconsole.ValidID(id) {
		onlineSocialFailure(w, oso.ErrInvalid)
		return
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if e != nil {
		onlineSocialFailure(w, e)
		return
	}
	access := oso.Access{Actor: actor, Digest: digest}
	if !access.Valid() {
		onlineSocialFailure(w, oso.ErrDenied)
		return
	}
	store, ok := s.catalog.(oso.Store)
	if !ok {
		onlineSocialFailure(w, oso.ErrUnavailable)
		return
	}
	var receipt oso.Receipt
	if options {
		receipt, e = store.ReadOnlineSocialOpportunityOptions(r.Context(), access)
	} else {
		receipt, e = store.ReadOnlineSocialOpportunities(r.Context(), access, id)
	}
	if e != nil {
		onlineSocialFailure(w, e)
		return
	}
	if receipt.View.OwnerID != actor.ID || receipt.View.IntentID != id || oso.Validate(receipt.View) != nil {
		onlineSocialFailure(w, oso.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": receipt.View})
	if e != nil {
		onlineSocialFailure(w, oso.ErrUnavailable)
		return
	}
	if e = store.RevalidateOnlineSocialOpportunities(r.Context(), access, receipt); e != nil {
		onlineSocialFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		onlineSocialFailure(w, oso.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
