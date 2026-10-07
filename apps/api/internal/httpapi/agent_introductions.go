package httpapi

import (
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/agentintroduction"
	"io"
	"net/http"
	"net/url"
)

func (s *server) ownAgentIntroductionSuggestions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, e := url.ParseQuery(r.URL.RawQuery)
	ids := q["sourceIntentId"]
	if e != nil || len(q) != 1 || len(ids) != 1 || !uuidPath.MatchString(ids[0]) {
		respondError(w, 400, "请选择一个本人公开的找搭子意图")
		return
	}
	// The shared self-only access gate intentionally rejects queries. Strip only
	// the already-validated selector in a request copy; never strip headers/workspace.
	rr := r.Clone(r.Context())
	u := *r.URL
	u.RawQuery = ""
	u.ForceQuery = false
	rr.URL = &u
	access, ok := s.privateProfileAccess(w, rr)
	if !ok {
		return
	}
	if r.Body != nil {
		body, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(body) > 0 {
			respondError(w, 400, "此查看入口不接收提交内容")
			return
		}
	}
	store, ok := s.privateProfiles.(ai.Store)
	if !ok || store == nil {
		respondError(w, 503, "引荐建议暂不可用")
		return
	}
	out, e := store.ReadOwnIntroductionSuggestions(r.Context(), access, ids[0])
	switch {
	case errors.Is(e, ai.ErrInvalid):
		respondError(w, 400, "找搭子意图无效")
	case errors.Is(e, ai.ErrDenied):
		respondError(w, 403, "当前公开来源或社交设置不允许查看建议")
	case errors.Is(e, ai.ErrChanged):
		respondError(w, 409, "公开来源已变化，请重新查看")
	case e != nil:
		respondError(w, 503, "引荐建议暂不可用")
	default:
		if ai.ValidateResponse(out, ids[0]) != nil {
			respondError(w, 503, "引荐建议暂不可用")
			return
		}
		respond(w, 200, map[string]any{"data": out})
	}
}
