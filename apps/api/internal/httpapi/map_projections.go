package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

func mapProjectionQuery(r *http.Request, private bool) (mp.Query, error) {
	q := mp.Query{CityID: r.PathValue("cityID"), Private: private}
	v, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(v) != 4 || r.URL.ForceQuery {
		return q, mp.ErrInvalid
	}
	targets := map[string]*float64{"west": &q.West, "south": &q.South, "east": &q.East, "north": &q.North}
	for k, target := range targets {
		values, ok := v[k]
		if !ok || len(values) != 1 || values[0] == "" {
			return q, mp.ErrInvalid
		}
		*target, e = strconv.ParseFloat(values[0], 64)
		if e != nil {
			return q, mp.ErrInvalid
		}
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			return q, mp.ErrInvalid
		}
	}
	return q, mp.ValidateQuery(q)
}
func mapProjectionFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "map_layers_unavailable", "地图图层暂不可用，请稍后重新读取。"
	switch {
	case errors.Is(e, mp.ErrInvalid):
		status, code, message = 400, "invalid_map_layers", "请检查地图范围。"
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "会话已失效，请重新登录。"
	case errors.Is(e, mp.ErrNotFound):
		status, code, message = 404, "map_context_unavailable", "该城市的公开地图来源当前不可用。"
	case errors.Is(e, mp.ErrChanged):
		status, code, message = 409, "map_source_changed", "来源已变化，请重新读取地图图层。"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *server) getPublicMapLayers(w http.ResponseWriter, r *http.Request) {
	s.getMapLayers(w, r, false)
}
func (s *server) getOwnMapOpportunityLayer(w http.ResponseWriter, r *http.Request) {
	s.getMapLayers(w, r, true)
}
func (s *server) getMapLayers(w http.ResponseWriter, r *http.Request, private bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		mapProjectionFailure(w, mp.ErrInvalid)
		return
	}
	q, e := mapProjectionQuery(r, private)
	if e != nil {
		mapProjectionFailure(w, e)
		return
	}
	var access mp.Access
	if private || len(r.Header.Values("Authorization")) != 0 {
		actor, digest, e := s.humanSocialActor(r, true)
		if e != nil {
			mapProjectionFailure(w, e)
			return
		}
		access = mp.Access{Actor: actor, Digest: digest}
		if !access.Valid() || access.Anonymous() {
			mapProjectionFailure(w, identity.ErrUnauthorized)
			return
		}
	}
	store, ok := s.catalog.(mp.Store)
	if !ok {
		mapProjectionFailure(w, mp.ErrUnavailable)
		return
	}
	receipt, e := store.ReadMapLayers(r.Context(), access, q)
	if e != nil {
		mapProjectionFailure(w, e)
		return
	}
	if receipt.Query != q || mp.ValidateView(receipt.View) != nil || receipt.View.CityID != q.CityID || (private && receipt.View.Scope != mp.SelfPrivate) || (!private && receipt.View.Scope != mp.Public) {
		mapProjectionFailure(w, mp.ErrUnavailable)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": receipt.View})
	if e != nil {
		mapProjectionFailure(w, mp.ErrUnavailable)
		return
	}
	// This final native read follows encoding/pool/table waits. No trailing actor
	// resolver/idle refresh can wait and invalidate the approved current payload.
	if e = store.RevalidateMapLayers(r.Context(), access, receipt); e != nil {
		mapProjectionFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		mapProjectionFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}
