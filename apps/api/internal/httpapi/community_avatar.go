package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

func communityAvatarFailure(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		respondError(w, 401, "unauthorized")
	case errors.Is(err, ca.ErrInvalid):
		respondError(w, 400, "invalid_community_avatar")
	case errors.Is(err, ca.ErrForbidden):
		respondError(w, 403, "forbidden")
	case errors.Is(err, ca.ErrChanged), errors.Is(err, identity.ErrConflict):
		respondError(w, 409, "community_avatar_changed")
	case errors.Is(err, ca.ErrNotFound):
		respondError(w, 404, "not_found")
	default:
		respondError(w, 503, "community_avatar_unavailable")
	}
}

func (s *server) communityAvatarAccess(w http.ResponseWriter, r *http.Request) (ca.Access, ca.Store, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		communityAvatarFailure(w, ca.ErrForbidden)
		return ca.Access{}, nil, "", false
	}
	id := r.PathValue("communityID")
	if !av.ValidID(id) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		communityAvatarFailure(w, ca.ErrInvalid)
		return ca.Access{}, nil, "", false
	}
	port, ok := s.access.(ca.Store)
	if !ok || nilHumanSelfReviewPort(port) {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return ca.Access{}, nil, "", false
	}
	if _, ok := s.access.(interface {
		ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
	}); !ok {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return ca.Access{}, nil, "", false
	}
	actor, digest, err := s.actor(r, true)
	if err != nil {
		communityAvatarFailure(w, err)
		return ca.Access{}, nil, "", false
	}
	if actor.AccountType != "person" || !av.ValidID(actor.ID) {
		communityAvatarFailure(w, ca.ErrForbidden)
		return ca.Access{}, nil, "", false
	}
	return ca.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}, port, id, true
}

func communityAvatarNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if err != nil || len(raw) != 0 {
		communityAvatarFailure(w, ca.ErrInvalid)
		return false
	}
	return true
}

func validCommunityAvatarState(state ca.State, id string) bool {
	return state.Version >= 0 && state.Version <= ca.MaxVersion &&
		(state.Avatar == nil || state.Avatar.Valid() && state.Avatar.CommunityID == id && state.Avatar.Version == state.Version)
}
func validCommunityAvatarResult(result ca.Result, id, op string) bool {
	return result.CommunityID == id && result.OperationID == op && av.ValidID(op) &&
		(result.Action == "SAVE" || result.Action == "DELETE") && result.AppliedVersion > 0 && result.AppliedVersion <= ca.MaxVersion &&
		result.Current.Version >= result.AppliedVersion && validCommunityAvatarState(result.Current, id)
}

func decodeCommunityAvatar(w http.ResponseWriter, r *http.Request, commit bool) (ca.Input, error) {
	if len(r.Header.Values("Content-Type")) != 1 || r.Body == nil {
		return ca.Input{}, ca.ErrInvalid
	}
	kind, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		return ca.Input{}, ca.ErrInvalid
	}
	for k, v := range params {
		if k != "charset" || v != "utf-8" {
			return ca.Input{}, ca.ErrInvalid
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(4*((av.MaxInputBytes+2)/3)+8192)))
	if err != nil {
		return ca.Input{}, ca.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ca.Input{}, ca.ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !(name == "operationId" || name == "expectedVersion" || name == "action" || name == "contentType" || name == "contentBase64" || commit && name == "approval") {
			return ca.Input{}, ca.ErrInvalid
		}
		if _, duplicate := fields[name]; duplicate {
			return ca.Input{}, ca.ErrInvalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return ca.Input{}, ca.ErrInvalid
		}
		fields[name] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return ca.Input{}, ca.ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return ca.Input{}, ca.ErrInvalid
	}
	var in ca.Input
	var action string
	if json.Unmarshal(fields["operationId"], &in.OperationID) != nil || !av.ValidID(in.OperationID) ||
		bytes.Equal(bytes.TrimSpace(fields["expectedVersion"]), []byte("null")) || json.Unmarshal(fields["expectedVersion"], &in.ExpectedVersion) != nil || in.ExpectedVersion < 0 || in.ExpectedVersion >= ca.MaxVersion ||
		json.Unmarshal(fields["action"], &action) != nil || (action != "SAVE" && action != "DELETE") {
		return ca.Input{}, ca.ErrInvalid
	}
	in.Delete = action == "DELETE"
	count := 3
	if !in.Delete {
		count += 2
		var encoded string
		if json.Unmarshal(fields["contentType"], &in.ContentType) != nil || json.Unmarshal(fields["contentBase64"], &encoded) != nil {
			return ca.Input{}, ca.ErrInvalid
		}
		in.Content, err = base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(in.Content) > av.MaxInputBytes || base64.StdEncoding.EncodeToString(in.Content) != encoded {
			return ca.Input{}, ca.ErrInvalid
		}
	}
	if commit {
		count++
		if json.Unmarshal(fields["approval"], &in.Approval) != nil || len(in.Approval) < 8 || len(in.Approval) > 1024 || !strings.HasPrefix(in.Approval, "ca1.") {
			return ca.Input{}, ca.ErrInvalid
		}
	}
	if len(fields) != count {
		return ca.Input{}, ca.ErrInvalid
	}
	if _, _, _, _, err = ca.Normalize(in); err != nil {
		return ca.Input{}, ca.ErrInvalid
	}
	return in, nil
}

func (s *server) getCommunityAvatar(w http.ResponseWriter, r *http.Request) {
	a, port, id, ok := s.communityAvatarAccess(w, r)
	if !ok || !communityAvatarNoBody(w, r) {
		return
	}
	state, err := port.ReadCommunityAvatar(r.Context(), a, id)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	if !validCommunityAvatarState(state, id) {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return
	}
	s.respondHumanSocialNow(w, r, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType}, map[string]any{"data": state})
}

func (s *server) previewCommunityAvatar(w http.ResponseWriter, r *http.Request) {
	a, port, id, ok := s.communityAvatarAccess(w, r)
	if !ok {
		return
	}
	in, err := decodeCommunityAvatar(w, r, false)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	approval, err := port.PreviewCommunityAvatar(r.Context(), a, id, in)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	_, _, action, sum, _ := ca.Normalize(in)
	remaining := time.Until(approval.ExpiresAt)
	if approval.CommunityID != id || approval.OperationID != in.OperationID || approval.ExpectedVersion != in.ExpectedVersion || approval.Action != action || approval.InputSHA256 != sum ||
		!strings.HasPrefix(approval.Token, "ca1.") || len(approval.Token) > 1024 || remaining <= 0 || remaining > 35*time.Second {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return
	}
	s.respondHumanSocialNow(w, r, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType}, map[string]any{"data": approval})
}

func (s *server) putCommunityAvatar(w http.ResponseWriter, r *http.Request) {
	a, port, id, ok := s.communityAvatarAccess(w, r)
	if !ok {
		return
	}
	in, err := decodeCommunityAvatar(w, r, true)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	result, err := port.ChangeCommunityAvatar(r.Context(), a, id, in)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	if !validCommunityAvatarResult(result, id, in.OperationID) || result.AppliedVersion != in.ExpectedVersion+1 || result.Action != map[bool]string{true: "DELETE", false: "SAVE"}[in.Delete] {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return
	}
	s.respondHumanSocialNow(w, r, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType}, map[string]any{"data": result})
}

func (s *server) getCommunityAvatarOperation(w http.ResponseWriter, r *http.Request) {
	a, port, id, ok := s.communityAvatarAccess(w, r)
	if !ok || !communityAvatarNoBody(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !av.ValidID(op) {
		communityAvatarFailure(w, ca.ErrInvalid)
		return
	}
	result, err := port.ReadCommunityAvatarOperation(r.Context(), a, id, op)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	if !validCommunityAvatarResult(result, id, op) {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return
	}
	s.respondHumanSocialNow(w, r, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType}, map[string]any{"data": result})
}

func (s *server) getCommunityAvatarContent(w http.ResponseWriter, r *http.Request) {
	a, port, id, ok := s.communityAvatarAccess(w, r)
	if !ok || !communityAvatarNoBody(w, r) {
		return
	}
	rawVersion := r.PathValue("version")
	version, err := strconv.ParseInt(rawVersion, 10, 64)
	if err != nil || version < 1 || version > ca.MaxVersion || strconv.FormatInt(version, 10) != rawVersion {
		communityAvatarFailure(w, ca.ErrInvalid)
		return
	}
	metadata, raw, err := port.ReadCommunityAvatarContent(r.Context(), a, id, version)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	sum := sha256.Sum256(raw)
	if !metadata.Valid() || metadata.CommunityID != id || metadata.Version != version || len(raw) != metadata.ByteCount || hex.EncodeToString(sum[:]) != metadata.SHA256 {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return
	}
	state, err := port.ReadCommunityAvatar(r.Context(), a, id)
	if err != nil {
		communityAvatarFailure(w, err)
		return
	}
	if !validCommunityAvatarState(state, id) || state.Avatar == nil || *state.Avatar != metadata {
		communityAvatarFailure(w, ca.ErrNotFound)
		return
	}
	sessions := s.access.(interface {
		ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
	})
	if err = sessions.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType}); err != nil {
		communityAvatarFailure(w, err)
		return
	}
	if r.Context().Err() != nil {
		communityAvatarFailure(w, ca.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}

func (s *server) registerCommunityAvatarRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/communities/{communityID}/avatar", s.getCommunityAvatar)
	mux.HandleFunc("POST /v1/communities/{communityID}/avatar/preview", s.previewCommunityAvatar)
	mux.HandleFunc("PUT /v1/communities/{communityID}/avatar", s.putCommunityAvatar)
	mux.HandleFunc("GET /v1/communities/{communityID}/avatar/operations/{operationID}", s.getCommunityAvatarOperation)
	// The operation and versioned-content shapes overlap at operations/content.
	// A general tail keeps the operation route strictly more specific in ServeMux.
	mux.HandleFunc("GET /v1/communities/{communityID}/avatar/{contentPath...}", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.PathValue("contentPath"), "/")
		if len(parts) != 2 || parts[1] != "content" {
			communityAvatarFailure(w, ca.ErrInvalid)
			return
		}
		r.SetPathValue("version", parts[0])
		s.getCommunityAvatarContent(w, r)
	})
}
