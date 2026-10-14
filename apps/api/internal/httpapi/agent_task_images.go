package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"

	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

func (s *server) taskImageGateway(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, media.HumanTaskImageStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	a, d, e := s.actor(r, true)
	if authFailed(w, e) {
		return a, d, nil, false
	}
	if a.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "person_account_required")
		return a, d, nil, false
	}
	if !media.ValidPrivateImageID(r.PathValue("taskID")) || (r.PathValue("imageID") != "" && !media.ValidPrivateImageID(r.PathValue("imageID"))) {
		respondError(w, 400, "invalid_task_image_id")
		return a, d, nil, false
	}
	_, policy := workspaceAgentPolicy(false)
	if !policy.Available || !policy.Allows(agentruntime.CityContextRead) || !policy.DecideContext(agentruntime.AccessRequest{Scope: agentruntime.WorkspacePrivate, OwnerID: a.ID, ViewerID: a.ID, PrincipalID: a.ID, AuthorityVerified: true}).Allowed {
		respondError(w, 403, "agent_context_forbidden")
		return a, d, nil, false
	}
	if !s.requireActiveWorkspaceAgent(w, r, "person", a.ID) {
		return a, d, nil, false
	}
	g, ok := s.agent.(media.HumanTaskImageStore)
	if !ok || g == nil || (reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil()) {
		respondError(w, 503, "task_image_unavailable")
		return a, d, nil, false
	}
	return a, d, g, true
}
func taskImageFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		authFailed(w, e)
	case errors.Is(e, content.ErrNotFound):
		respondError(w, 404, "task_image_not_found")
	case errors.Is(e, content.ErrConflict):
		respondError(w, 409, "task_image_changed")
	case errors.Is(e, media.ErrImageLimit):
		respondError(w, 413, "task_image_limit")
	case errors.Is(e, content.ErrInvalid), errors.Is(e, media.ErrImageType), errors.Is(e, media.ErrImageInvalid):
		respondError(w, 400, "invalid_task_image")
	default:
		respondError(w, 503, "task_image_unavailable")
	}
}
func taskImageNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "invalid_task_image_query")
		return false
	}
	return true
}
func (s *server) previewHumanTaskImage(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.taskImageGateway(w, r)
	if !ok || !taskImageNoQuery(w, r) {
		return
	}
	var p media.TaskImageInput
	if !decodeStrictJSON(w, r, &p) {
		return
	}
	if e := media.ValidateTaskImageInput(p); e != nil {
		taskImageFailure(w, e)
		return
	}
	v, e := g.PreviewHumanTaskImage(r.Context(), d, a, r.PathValue("taskID"), p)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		taskImageFailure(w, e)
		return
	}
	respond(w, 201, map[string]any{"data": v})
}
func (s *server) saveHumanTaskImage(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.taskImageGateway(w, r)
	if !ok || !taskImageNoQuery(w, r) {
		return
	}
	mime := r.Header.Get("Content-Type")
	if len(r.Header.Values("Content-Type")) != 1 || (mime != "image/png" && mime != "image/jpeg") || len(r.Header.Values("X-Birdtie-Task-Image-Confirmation")) != 1 || r.Header.Get("X-Birdtie-Task-Image-Confirmation") != r.PathValue("imageID") {
		respondError(w, 400, "task_image_confirmation_required")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, media.MaxImageInputBytes+1))
	if e != nil {
		taskImageFailure(w, content.ErrInvalid)
		return
	}
	if len(raw) > media.MaxImageInputBytes {
		taskImageFailure(w, media.ErrImageLimit)
		return
	}
	v, e := g.SaveHumanTaskImage(r.Context(), d, a, r.PathValue("taskID"), r.PathValue("imageID"), mime, raw)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		taskImageFailure(w, e)
		return
	}
	respond(w, 200, map[string]any{"data": v})
}
func (s *server) readHumanTaskImages(w http.ResponseWriter, r *http.Request) {
	s.readHumanTaskImage(w, r, false, false)
}
func (s *server) readHumanTaskImageOperation(w http.ResponseWriter, r *http.Request) {
	s.readHumanTaskImage(w, r, false, true)
}
func (s *server) readHumanTaskImageContent(w http.ResponseWriter, r *http.Request) {
	s.readHumanTaskImage(w, r, true, false)
}
func (s *server) readHumanTaskImage(w http.ResponseWriter, r *http.Request, binary, operation bool) {
	a, d, g, ok := s.taskImageGateway(w, r)
	if !ok || !taskImageNoQuery(w, r) {
		return
	}
	var mime string
	encode := func(rs []media.TaskImageReceipt, b []byte) ([]byte, error) {
		if binary {
			if len(rs) != 1 {
				return nil, content.ErrNotFound
			}
			mime = rs[0].MIME
			return append([]byte(nil), b...), nil
		}
		if operation {
			if len(rs) != 1 {
				return nil, content.ErrNotFound
			}
			return json.Marshal(map[string]any{"data": rs[0]})
		}
		return json.Marshal(map[string]any{"data": rs})
	}
	raw, e := g.ReadHumanTaskImages(r.Context(), d, a, r.PathValue("taskID"), r.PathValue("imageID"), binary, operation, encode)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		taskImageFailure(w, e)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if binary {
		w.Header().Set("Content-Type", mime)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (s *server) deleteHumanTaskImage(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.taskImageGateway(w, r)
	if !ok {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	rev, parse := strconv.ParseInt(q.Get("revision"), 10, 64)
	if e != nil || parse != nil || rev < 1 || len(q) != 1 || len(q["revision"]) != 1 {
		respondError(w, 400, "invalid_task_image_revision")
		return
	}
	e = g.DeleteHumanTaskImage(r.Context(), d, a, r.PathValue("taskID"), r.PathValue("imageID"), rev)
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e != nil {
		taskImageFailure(w, e)
		return
	}
	w.WriteHeader(204)
}
