package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"io"
	"mime"
	"net/http"
	"strconv"
)

func personAvatarFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
	case errors.Is(e, av.ErrInvalid):
		respondError(w, 400, "invalid_avatar")
	case errors.Is(e, av.ErrConflict), errors.Is(e, identity.ErrConflict):
		respondError(w, 409, "avatar_changed")
	case errors.Is(e, av.ErrNotFound), errors.Is(e, identity.ErrNotFound):
		respondError(w, 404, "not_found")
	default:
		respondError(w, 503, "avatar_unavailable")
	}
}
func (s *server) personAvatarAccess(w http.ResponseWriter, r *http.Request, own bool) (av.Access, av.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "forbidden")
		return av.Access{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		personAvatarFailure(w, av.ErrInvalid)
		return av.Access{}, nil, false
	}
	port, ok := s.access.(av.Store)
	if !ok || nilHumanSelfReviewPort(port) {
		personAvatarFailure(w, av.ErrUnavailable)
		return av.Access{}, nil, false
	}
	actor, digest, e := s.actor(r, own)
	if e != nil {
		personAvatarFailure(w, e)
		return av.Access{}, nil, false
	}
	if own && actor.AccountType != "person" {
		respondError(w, 403, "forbidden")
		return av.Access{}, nil, false
	}
	return av.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}, port, true
}
func avatarNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if e != nil || len(b) != 0 {
		personAvatarFailure(w, av.ErrInvalid)
		return false
	}
	return true
}
func validPersonAvatarState(state av.State, owner string) bool {
	return state.Version >= 0 && (state.Avatar == nil || state.Avatar.Valid() && state.Avatar.AccountID == owner && state.Avatar.Version == state.Version)
}
func (s *server) getOwnPersonAvatar(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.personAvatarAccess(w, r, true)
	if !ok || !avatarNoBody(w, r) {
		return
	}
	state, e := p.ReadOwnAvatar(r.Context(), a)
	if e != nil {
		personAvatarFailure(w, e)
		return
	}
	if !validPersonAvatarState(state, a.AccountID) {
		personAvatarFailure(w, av.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": state})
}
func decodePersonAvatar(w http.ResponseWriter, r *http.Request, deleting bool) (av.Input, error) {
	if len(r.Header.Values("Content-Type")) != 1 || r.Body == nil {
		return av.Input{}, av.ErrInvalid
	}
	kind, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		return av.Input{}, av.ErrInvalid
	}
	for k, v := range params {
		if k != "charset" || v != "utf-8" {
			return av.Input{}, av.ErrInvalid
		}
	}
	limit := int64(4*((av.MaxInputBytes+2)/3) + 4096)
	if deleting {
		limit = 4096
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if e != nil {
		return av.Input{}, av.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	t, e := decoder.Token()
	if e != nil || t != json.Delim('{') {
		return av.Input{}, av.ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		t, e = decoder.Token()
		key, ok := t.(string)
		if e != nil || !ok || (key != "operationId" && key != "expectedVersion" && (deleting || (key != "contentType" && key != "contentBase64"))) {
			return av.Input{}, av.ErrInvalid
		}
		if _, exists := fields[key]; exists {
			return av.Input{}, av.ErrInvalid
		}
		var v json.RawMessage
		if decoder.Decode(&v) != nil {
			return av.Input{}, av.ErrInvalid
		}
		fields[key] = v
	}
	t, e = decoder.Token()
	if e != nil || t != json.Delim('}') {
		return av.Input{}, av.ErrInvalid
	}
	if _, e = decoder.Token(); e != io.EOF {
		return av.Input{}, av.ErrInvalid
	}
	count := 4
	if deleting {
		count = 2
	}
	if len(fields) != count {
		return av.Input{}, av.ErrInvalid
	}
	in := av.Input{Delete: deleting}
	if json.Unmarshal(fields["operationId"], &in.OperationID) != nil || !av.ValidID(in.OperationID) || bytes.Equal(bytes.TrimSpace(fields["expectedVersion"]), []byte("null")) || json.Unmarshal(fields["expectedVersion"], &in.ExpectedVersion) != nil || in.ExpectedVersion < 0 {
		return av.Input{}, av.ErrInvalid
	}
	if !deleting {
		var content string
		if json.Unmarshal(fields["contentType"], &in.ContentType) != nil || json.Unmarshal(fields["contentBase64"], &content) != nil {
			return av.Input{}, av.ErrInvalid
		}
		in.Content, e = base64.StdEncoding.Strict().DecodeString(content)
		if e != nil || len(in.Content) > av.MaxInputBytes || base64.StdEncoding.EncodeToString(in.Content) != content {
			return av.Input{}, av.ErrInvalid
		}
	}
	return in, nil
}
func (s *server) putOwnPersonAvatar(w http.ResponseWriter, r *http.Request) {
	s.changeOwnPersonAvatar(w, r, false)
}
func (s *server) deleteOwnPersonAvatar(w http.ResponseWriter, r *http.Request) {
	s.changeOwnPersonAvatar(w, r, true)
}
func (s *server) changeOwnPersonAvatar(w http.ResponseWriter, r *http.Request, deleting bool) {
	a, p, ok := s.personAvatarAccess(w, r, true)
	if !ok {
		return
	}
	in, e := decodePersonAvatar(w, r, deleting)
	if e != nil {
		personAvatarFailure(w, e)
		return
	}
	result, e := p.ChangeOwnAvatar(r.Context(), a, in)
	if e != nil {
		personAvatarFailure(w, e)
		return
	}
	if result.OperationID != in.OperationID || result.Action != map[bool]string{true: "DELETE", false: "SAVE"}[deleting] || result.AppliedVersion != in.ExpectedVersion+1 || result.Current.Version < result.AppliedVersion || !validPersonAvatarState(result.Current, a.AccountID) {
		personAvatarFailure(w, av.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": result})
}
func (s *server) getOwnPersonAvatarOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.personAvatarAccess(w, r, true)
	if !ok || !avatarNoBody(w, r) {
		return
	}
	id := r.PathValue("operationID")
	if !av.ValidID(id) {
		personAvatarFailure(w, av.ErrInvalid)
		return
	}
	result, e := p.ReadOwnAvatarOperation(r.Context(), a, id)
	if e != nil {
		personAvatarFailure(w, e)
		return
	}
	if result.OperationID != id || result.AppliedVersion < 1 || result.Current.Version < result.AppliedVersion || (result.Action != "SAVE" && result.Action != "DELETE") || !validPersonAvatarState(result.Current, a.AccountID) {
		personAvatarFailure(w, av.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": result})
}
func (s *server) getPersonAvatarContent(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.personAvatarAccess(w, r, false)
	if !ok || !avatarNoBody(w, r) {
		return
	}
	id, rawVersion := r.PathValue("accountID"), r.PathValue("version")
	version, e := strconv.ParseInt(rawVersion, 10, 64)
	if !av.ValidID(id) || e != nil || version < 1 || strconv.FormatInt(version, 10) != rawVersion {
		personAvatarFailure(w, av.ErrInvalid)
		return
	}
	metadata, raw, e := p.ReadAvatarContent(r.Context(), a, id, version)
	if e != nil {
		personAvatarFailure(w, e)
		return
	}
	if !metadata.Valid() || metadata.AccountID != id || metadata.Version != version || len(raw) != metadata.ByteCount || r.Context().Err() != nil {
		personAvatarFailure(w, av.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
