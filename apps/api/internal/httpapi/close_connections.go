package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

func closeConnectionsFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		messagePolicyError(w, 401, "unauthorized", "登录已失效，请重新登录。")
	case errors.Is(e, connection.ErrCloseInvalid):
		messagePolicyError(w, 400, "invalid_close_connection", "请检查这次好友选择，并明确确认。")
	case errors.Is(e, connection.ErrForbidden):
		messagePolicyError(w, 403, "close_connection_forbidden", "请在本人账号中管理密友。")
	case errors.Is(e, connection.ErrNotFound):
		messagePolicyError(w, 404, "close_connection_not_found", "当前好友关系不可用，请重新查看。")
	case errors.Is(e, connection.ErrCloseChanged):
		messagePolicyError(w, 409, "close_connection_changed", "好友或资料条件已变化，请重新查看后确认。")
	default:
		messagePolicyError(w, 503, "close_connection_unavailable", "密友设置暂不可用，请稍后重试。")
	}
}
func nilCloseConnectionPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Chan, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func (s *server) closeConnectionsAccess(w http.ResponseWriter, r *http.Request) (ea.Access, connection.CloseCurrentStore, connection.CloseResponseValidator, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return ea.Access{}, nil, nil, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		closeConnectionsFailure(w, connection.ErrForbidden)
		return ea.Access{}, nil, nil, false
	}
	if nilCloseConnectionPort(s.access) {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return ea.Access{}, nil, nil, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if e != nil {
		closeConnectionsFailure(w, e)
		return ea.Access{}, nil, nil, false
	}
	a := ea.Access{Actor: actor, SessionDigest: digest}
	if !connection.ValidCloseAccess(a) {
		closeConnectionsFailure(w, connection.ErrForbidden)
		return a, nil, nil, false
	}
	port, ok := s.connections.(connection.CloseCurrentStore)
	validator, typed := s.connections.(connection.CloseResponseValidator)
	session, current := s.access.(socialnow.HumanSessionStore)
	if !ok || !typed || !current || nilCloseConnectionPort(port) || nilCloseConnectionPort(validator) || nilCloseConnectionPort(session) {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return a, nil, nil, false
	}
	return a, port, validator, true
}
func closeConnectionsBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	ct, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || ct != "application/json" || len(r.Header.Values("Content-Type")) != 1 || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return nil, false
	}
	for k := range params {
		if k != "charset" {
			closeConnectionsFailure(w, connection.ErrCloseInvalid)
			return nil, false
		}
	}
	if r.Body == nil {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, connection.CloseMaxBody+1))
	if e != nil || len(raw) > connection.CloseMaxBody {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return nil, false
	}
	return raw, true
}

// Encode before both final checks. The native typed proof is bound to these
// captured bytes and source facts, rather than reconstructed from a wire DTO.
func (s *server) closeConnectionsResponse(w http.ResponseWriter, r *http.Request, a ea.Access, validator connection.CloseResponseValidator, value any) {
	var raw []byte
	var e error
	if _, deleted := value.(connection.CloseDeleteReceipt); !deleted {
		raw, e = json.Marshal(map[string]any{"data": value})
		if e != nil {
			closeConnectionsFailure(w, connection.ErrCloseUnavailable)
			return
		}
	}
	session, ok := s.access.(socialnow.HumanSessionStore)
	if !ok || nilCloseConnectionPort(session) {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return
	}
	if e = session.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, a.Actor); e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	switch v := value.(type) {
	case connection.CloseView:
		e = validator.ValidateCloseConnectionsResponse(r.Context(), a, v)
	case connection.CloseResult:
		e = validator.ValidateCloseConnectionResponse(r.Context(), a, v.Connection.PersonID, v)
	case connection.CloseDeleteReceipt:
		e = validator.ValidateCloseDeleteResponse(r.Context(), a, v)
	default:
		e = connection.ErrCloseUnavailable
	}
	if e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return
	}
	if raw == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) listOwnCloseConnections(w http.ResponseWriter, r *http.Request) {
	a, port, validator, ok := s.closeConnectionsAccess(w, r)
	if !ok {
		return
	}
	if r.ContentLength > 0 || len(r.TransferEncoding) > 0 {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return
	}
	if r.Body != nil {
		one := make([]byte, 1)
		n, e := r.Body.Read(one)
		if n != 0 || e != io.EOF {
			closeConnectionsFailure(w, connection.ErrCloseInvalid)
			return
		}
	}
	v, e := port.ListOwnCloseConnectionsCurrent(r.Context(), a)
	if e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	if !connection.ValidCloseView(v, a.Actor.ID) {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return
	}
	s.closeConnectionsResponse(w, r, a, validator, v)
}
func (s *server) putOwnCloseConnection(w http.ResponseWriter, r *http.Request) {
	a, port, validator, ok := s.closeConnectionsAccess(w, r)
	if !ok {
		return
	}
	peer := r.PathValue("personID")
	if !connection.ValidCloseID(peer) || peer == a.Actor.ID {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return
	}
	raw, ok := closeConnectionsBody(w, r)
	if !ok {
		return
	}
	in, e := connection.DecodeClosePut(raw)
	if e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	v, e := port.PutOwnCloseConnectionCurrent(r.Context(), a, peer, in)
	if e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	if !connection.ValidCloseResult(v, a.Actor.ID, peer) || v.Connection.TieID != in.TieID || v.Connection.RequestID != in.RequestID || !v.ValidUntil.Equal(in.ValidUntil) {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return
	}
	s.closeConnectionsResponse(w, r, a, validator, v)
}
func (s *server) deleteOwnCloseConnection(w http.ResponseWriter, r *http.Request) {
	a, port, validator, ok := s.closeConnectionsAccess(w, r)
	if !ok {
		return
	}
	peer := r.PathValue("personID")
	if !connection.ValidCloseID(peer) || peer == a.Actor.ID {
		closeConnectionsFailure(w, connection.ErrCloseInvalid)
		return
	}
	raw, ok := closeConnectionsBody(w, r)
	if !ok {
		return
	}
	in, e := connection.DecodeCloseDelete(raw)
	if e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	v, e := port.DeleteOwnCloseConnectionCurrent(r.Context(), a, peer, in)
	if e != nil {
		closeConnectionsFailure(w, e)
		return
	}
	if v.PersonID != peer {
		closeConnectionsFailure(w, connection.ErrCloseUnavailable)
		return
	}
	s.closeConnectionsResponse(w, r, a, validator, v)
}
