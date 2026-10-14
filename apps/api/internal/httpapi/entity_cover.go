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
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

type entityCoverResponseValidator interface {
	ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
}

func (s *server) respondOrganizationCoverResult(w http.ResponseWriter, r *http.Request, actor identity.Actor, d [32]byte, status int, item organization.Organization) {
	a := ec.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	if item.Cover == nil && item.CoverSourceProof == "" {
		s.respondCoverWithoutMedia(w, r, a, status, item)
		return
	}
	p, ok := s.access.(ec.Store)
	if !ok {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	s.respondEntityCoverStatus(w, r, a, status, item, func() error {
		return p.ValidateEntityCoverProjection(r.Context(), a, ec.Ref{Type: "organization", ID: item.ID}, item.Cover, item.CoverSourceProof)
	})
}

type coverRequestFrameKey struct{}
type coverRequestFrame struct {
	access  ec.Access
	frame   ec.SessionFrame
	guarded bool
}

func (s *server) captureEntityCoverHTTP(w http.ResponseWriter, r *http.Request, actor identity.Actor, d [32]byte) bool {
	a := ec.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	if old, ok := r.Context().Value(coverRequestFrameKey{}).(coverRequestFrame); ok {
		if old.access != a {
			entityCoverFailure(w, ec.ErrChanged)
			return false
		}
		return true
	}
	frame := ec.SessionFrame{}
	guarded := actor.ID == ""
	if ec.HasOriginalSession(r.Context()) {
		bound, ok := ec.OriginalSession(r.Context(), a)
		if !ok {
			entityCoverFailure(w, ec.ErrChanged)
			return false
		}
		frame = bound
		guarded = true
		if a.AccountID != "" {
			p, ok := s.access.(ec.SessionStore)
			if !ok {
				entityCoverFailure(w, ec.ErrUnavailable)
				return false
			}
			if e := p.ValidateEntityCoverSession(r.Context(), a, frame); e != nil {
				entityCoverFailure(w, e)
				return false
			}
		}
		ctx := context.WithValue(r.Context(), coverRequestFrameKey{}, coverRequestFrame{a, frame, guarded})
		*r = *r.WithContext(ctx)
		return true
	}
	if actor.ID != "" {
		p, ok := s.access.(ec.SessionStore)
		if !ok {
			if _, media := s.access.(ec.Store); media {
				entityCoverFailure(w, ec.ErrUnavailable)
				return false
			}
		} else {
			var e error
			frame, e = p.CaptureEntityCoverSession(r.Context(), a)
			if e != nil {
				entityCoverFailure(w, e)
				return false
			}
			guarded = true
		}
	}
	ctx := context.WithValue(r.Context(), coverRequestFrameKey{}, coverRequestFrame{a, frame, guarded})
	if guarded {
		ctx = ec.WithOriginalSession(ctx, a, frame)
	}
	*r = *r.WithContext(ctx)
	return true
}
func (s *server) validateEntityCoverHTTP(r *http.Request, a ec.Access) error {
	f, ok := r.Context().Value(coverRequestFrameKey{}).(coverRequestFrame)
	if !ok || f.access != a {
		return ec.ErrChanged
	}
	if a.AccountID == "" {
		if f.frame != (ec.SessionFrame{}) {
			return ec.ErrChanged
		}
		return nil
	}
	if !f.guarded {
		if _, media := s.access.(ec.Store); media {
			return ec.ErrUnavailable
		}
		return nil
	}
	p, ok := s.access.(ec.SessionStore)
	if !ok {
		return ec.ErrUnavailable
	}
	return p.ValidateEntityCoverSession(r.Context(), a, f.frame)
}
func (s *server) respondCoverWithoutMedia(w http.ResponseWriter, r *http.Request, a ec.Access, status int, payload any) {
	b, e := json.Marshal(map[string]any{"data": payload})
	if e != nil {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	if e = s.validateEntityCoverHTTP(r, a); e != nil {
		entityCoverFailure(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func (s *server) respondManagedActivityCover(w http.ResponseWriter, r *http.Request, actor identity.Actor, status int, payload any, items []activitypublish.Activity) {
	has := false
	for _, i := range items {
		if i.Cover != nil || i.CoverSourceProof != "" {
			has = true
		}
	}
	if !has {
		f, ok := r.Context().Value(coverRequestFrameKey{}).(coverRequestFrame)
		if !ok {
			entityCoverFailure(w, ec.ErrUnavailable)
			return
		}
		s.respondCoverWithoutMedia(w, r, f.access, status, payload)
		return
	}
	current, d, e := s.actor(r, true)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	if current != actor {
		entityCoverFailure(w, ec.ErrChanged)
		return
	}
	p, ok := s.access.(ec.Store)
	if !ok {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	a := ec.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	w.Header().Set("Cache-Control", "no-store")
	s.respondEntityCoverStatus(w, r, a, status, payload, func() error {
		for _, i := range items {
			if i.Cover != nil || i.CoverSourceProof != "" {
				if e := p.ValidateEntityCoverProjection(r.Context(), a, ec.Ref{Type: "activity", ID: i.ID}, i.Cover, i.CoverSourceProof); e != nil {
					return e
				}
			}
		}
		return nil
	})
}

func (s *server) respondActivityCoverList(w http.ResponseWriter, r *http.Request, actor identity.Actor, d [32]byte, items []foundation.Activity) {
	s.respondActivityCoverPayload(w, r, actor, d, items, items)
}
func (s *server) respondActivityCoverPayload(w http.ResponseWriter, r *http.Request, actor identity.Actor, d [32]byte, payload any, items []foundation.Activity) {
	has := false
	for _, i := range items {
		if i.Cover != nil || i.CoverSourceProof != "" {
			has = true
		}
	}
	if !has {
		s.respondCoverWithoutMedia(w, r, ec.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}, 200, payload)
		return
	}
	p, ok := s.access.(ec.Store)
	if !ok {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	a := ec.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}
	w.Header().Set("Cache-Control", "no-store")
	s.respondEntityCover(w, r, a, payload, func() error {
		for _, i := range items {
			if i.Cover != nil || i.CoverSourceProof != "" {
				if e := p.ValidateEntityCoverProjection(r.Context(), a, ec.Ref{Type: "activity", ID: i.ID}, i.Cover, i.CoverSourceProof); e != nil {
					return e
				}
			}
		}
		return nil
	})
}

func entityCoverFailure(w http.ResponseWriter, e error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(e, ec.ErrInvalid):
		respondError(w, 400, "invalid_entity_cover")
	case errors.Is(e, ec.ErrForbidden):
		respondError(w, 403, "forbidden")
	case errors.Is(e, ec.ErrChanged), errors.Is(e, identity.ErrConflict):
		respondError(w, 409, "entity_cover_changed")
	case errors.Is(e, ec.ErrNotFound):
		respondError(w, 404, "not_found")
	default:
		respondError(w, 503, "entity_cover_unavailable")
	}
}
func (s *server) entityCoverAccess(w http.ResponseWriter, r *http.Request, write bool) (ec.Access, ec.Store, ec.Ref, bool) {
	w.Header().Set("Cache-Control", "no-store")
	ref := ec.Ref{Type: r.PathValue("entityType"), ID: r.PathValue("entityID")}
	if !ref.Valid() || r.URL.RawQuery != "" || r.URL.ForceQuery {
		entityCoverFailure(w, ec.ErrInvalid)
		return ec.Access{}, nil, ref, false
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		entityCoverFailure(w, ec.ErrForbidden)
		return ec.Access{}, nil, ref, false
	}
	port, ok := s.access.(ec.Store)
	if !ok || nilHumanSelfReviewPort(port) {
		entityCoverFailure(w, ec.ErrUnavailable)
		return ec.Access{}, nil, ref, false
	}
	actor, d, e := s.actor(r, write)
	if e != nil {
		entityCoverFailure(w, e)
		return ec.Access{}, nil, ref, false
	}
	if actor.ID != "" && actor.AccountType != "person" {
		entityCoverFailure(w, ec.ErrForbidden)
		return ec.Access{}, nil, ref, false
	}
	if write && actor.AccountType != "person" {
		entityCoverFailure(w, ec.ErrForbidden)
		return ec.Access{}, nil, ref, false
	}
	if !s.captureEntityCoverHTTP(w, r, actor, d) {
		return ec.Access{}, nil, ref, false
	}
	return ec.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: d}, port, ref, true
}
func entityCoverNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1))
	if e != nil || len(raw) != 0 {
		entityCoverFailure(w, ec.ErrInvalid)
		return false
	}
	return true
}
func entityCoverObject(raw []byte, keys map[string]bool) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, ec.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return nil, ec.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		tok, e = d.Token()
		k, ok := tok.(string)
		if e != nil || !ok || !keys[k] || out[k] != nil {
			return nil, ec.ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, ec.ErrInvalid
		}
		out[k] = v
	}
	tok, e = d.Token()
	if e != nil || tok != json.Delim('}') {
		return nil, ec.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ec.ErrInvalid
	}
	return out, nil
}
func entityCoverBody(w http.ResponseWriter, r *http.Request, max int64) ([]byte, error) {
	if len(r.Header.Values("Content-Type")) != 1 || r.Body == nil {
		return nil, ec.ErrInvalid
	}
	kind, p, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		return nil, ec.ErrInvalid
	}
	for k, v := range p {
		if k != "charset" || strings.ToLower(v) != "utf-8" {
			return nil, ec.ErrInvalid
		}
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if e != nil {
		return nil, ec.ErrInvalid
	}
	return raw, nil
}
func decodeEntityCover(w http.ResponseWriter, r *http.Request, commit bool) (ec.Input, error) {
	raw, e := entityCoverBody(w, r, int64(4*((ec.MaxInputBytes+2)/3)+8192))
	if e != nil {
		return ec.Input{}, e
	}
	m, e := entityCoverObject(raw, map[string]bool{"operationId": true, "expectedVersion": true, "action": true, "contentType": true, "contentBase64": true, "rights": true, "approval": commit})
	if e != nil {
		return ec.Input{}, e
	}
	var in ec.Input
	if json.Unmarshal(m["operationId"], &in.OperationID) != nil || json.Unmarshal(m["expectedVersion"], &in.ExpectedVersion) != nil || json.Unmarshal(m["action"], &in.Action) != nil {
		return in, ec.ErrInvalid
	}
	n := 3
	if in.Action == "SAVE" {
		n += 3
		var encoded string
		if json.Unmarshal(m["contentType"], &in.ContentType) != nil || json.Unmarshal(m["contentBase64"], &encoded) != nil {
			return in, ec.ErrInvalid
		}
		in.Content, e = base64.StdEncoding.Strict().DecodeString(encoded)
		if e != nil || base64.StdEncoding.EncodeToString(in.Content) != encoded {
			return in, ec.ErrInvalid
		}
		rights, e := entityCoverObject(m["rights"], map[string]bool{"basis": true, "attribution": true, "licenseReference": true, "retentionUntil": true, "canRetain": true, "canPublish": true})
		if e != nil || len(rights) != 6 {
			return in, ec.ErrInvalid
		}
		in.Rights = &ec.Rights{}
		if json.Unmarshal(m["rights"], in.Rights) != nil {
			return in, ec.ErrInvalid
		}
		in.Rights.RetentionUntil = in.Rights.RetentionUntil.UTC()
	}
	if commit {
		n++
		if json.Unmarshal(m["approval"], &in.Approval) != nil || !strings.HasPrefix(in.Approval, "ec1.") || len(in.Approval) > 1024 {
			return in, ec.ErrInvalid
		}
	}
	if len(m) != n {
		return in, ec.ErrInvalid
	}
	if _, _, _, e = ec.Normalize(in, time.Now().UTC()); e != nil {
		return in, e
	}
	return in, nil
}
func (s *server) respondEntityCover(w http.ResponseWriter, r *http.Request, a ec.Access, payload any, validate func() error) {
	s.respondEntityCoverStatus(w, r, a, 200, payload, validate)
}
func (s *server) respondEntityCoverStatus(w http.ResponseWriter, r *http.Request, a ec.Access, status int, payload any, validate func() error) {
	encoded, e := json.Marshal(map[string]any{"data": payload})
	if e != nil || len(encoded) > 512*1024 {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	actor, d, e := s.actor(r, a.AccountID != "")
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	if actor.ID != a.AccountID || actor.AccountType != a.AccountType || d != a.SessionDigest || r.Context().Err() != nil {
		entityCoverFailure(w, ec.ErrChanged)
		return
	}
	if a.AccountID != "" {
		v, ok := s.access.(entityCoverResponseValidator)
		if !ok {
			entityCoverFailure(w, ec.ErrUnavailable)
			return
		}
		if e = v.ValidateHumanSocialResponse(r.Context(), d, actor); e != nil {
			entityCoverFailure(w, e)
			return
		}
	}
	if e = s.validateEntityCoverHTTP(r, a); e != nil {
		entityCoverFailure(w, e)
		return
	}
	// Projection/history ports receive the original frame in context and perform
	// their source check after their own final Session wait, without recapture.
	if e = validate(); e != nil {
		entityCoverFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(encoded, '\n'))
}
func (s *server) getEntityCover(w http.ResponseWriter, r *http.Request) {
	a, p, ref, ok := s.entityCoverAccess(w, r, false)
	if !ok || !entityCoverNoBody(w, r) {
		return
	}
	out, e := p.ReadEntityCover(r.Context(), a, ref)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	if out.Version < 0 || out.Version > ec.MaxVersion || (out.Cover != nil && (out.Cover.Ref != ref || !out.Cover.Valid(time.Now().UTC()))) {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	s.respondEntityCover(w, r, a, out, func() error { return p.ValidateEntityCoverProjection(r.Context(), a, ref, out.Cover, out.SourceProof) })
}
func (s *server) previewEntityCover(w http.ResponseWriter, r *http.Request) {
	a, p, ref, ok := s.entityCoverAccess(w, r, true)
	if !ok {
		return
	}
	in, e := decodeEntityCover(w, r, false)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	out, e := p.PreviewEntityCover(r.Context(), a, ref, in)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	_, sum, rights, _ := ec.Normalize(in, time.Now().UTC())
	if out.Ref != ref || out.OperationID != in.OperationID || out.ExpectedVersion != in.ExpectedVersion || out.Action != in.Action || out.InputSHA256 != sum || out.RightsSHA256 != rights || !out.ExpiresAt.After(time.Now()) || time.Until(out.ExpiresAt) > 31*time.Second || !strings.HasPrefix(out.Token, "ec1.") {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	if in.Action == "SAVE" {
		v := out.PreviewDerivative
		if v == nil {
			entityCoverFailure(w, ec.ErrUnavailable)
			return
		}
		raw, e := base64.StdEncoding.Strict().DecodeString(v.ContentBase64)
		hash := sha256.Sum256(raw)
		if e != nil || base64.StdEncoding.EncodeToString(raw) != v.ContentBase64 || v.ContentType != "image/jpeg" || v.Width != ec.Width || v.Height != ec.Height || len(raw) != v.ByteCount || len(raw) > ec.MaxThumbnailBytes || v.SHA256 != sum || hex.EncodeToString(hash[:]) != sum {
			entityCoverFailure(w, ec.ErrUnavailable)
			return
		}
	} else if out.PreviewDerivative != nil {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	s.respondEntityCover(w, r, a, out, func() error { return p.ValidateEntityCoverPreview(r.Context(), a, ref, in, out) })
}
func (s *server) entityCoverReceipt(w http.ResponseWriter, r *http.Request, a ec.Access, p ec.Store, ref ec.Ref, op string, out ec.Receipt) {
	if !out.Valid(ref, op) {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	s.respondEntityCover(w, r, a, out, func() error {
		current, e := p.ReadEntityCoverOperation(r.Context(), a, ref, op)
		if e != nil {
			return e
		}
		if current != out {
			return ec.ErrChanged
		}
		return nil
	})
}
func (s *server) putEntityCover(w http.ResponseWriter, r *http.Request) {
	a, p, ref, ok := s.entityCoverAccess(w, r, true)
	if !ok {
		return
	}
	in, e := decodeEntityCover(w, r, true)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	out, e := p.ChangeEntityCover(r.Context(), a, ref, in)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	if out.Status == "SUCCEEDED" && (out.AppliedVersion != in.ExpectedVersion+1 || out.Action != in.Action) {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	s.entityCoverReceipt(w, r, a, p, ref, in.OperationID, out)
}
func (s *server) getEntityCoverOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ref, ok := s.entityCoverAccess(w, r, true)
	if !ok || !entityCoverNoBody(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !av.ValidID(op) {
		entityCoverFailure(w, ec.ErrInvalid)
		return
	}
	out, e := p.ReadEntityCoverOperation(r.Context(), a, ref, op)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	s.entityCoverReceipt(w, r, a, p, ref, op, out)
}
func (s *server) stopEntityCoverOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ref, ok := s.entityCoverAccess(w, r, true)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	raw, e := entityCoverBody(w, r, 1024)
	if e != nil || !av.ValidID(op) {
		entityCoverFailure(w, ec.ErrInvalid)
		return
	}
	m, e := entityCoverObject(raw, map[string]bool{"expectedVersion": true})
	var expected int64
	if e != nil || len(m) != 1 || json.Unmarshal(m["expectedVersion"], &expected) != nil || expected < 0 || expected >= ec.MaxVersion {
		entityCoverFailure(w, ec.ErrInvalid)
		return
	}
	out, e := p.StopEntityCoverOperation(r.Context(), a, ref, op, expected)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	s.entityCoverReceipt(w, r, a, p, ref, op, out)
}
func (s *server) getEntityCoverContent(w http.ResponseWriter, r *http.Request) {
	a, p, ref, ok := s.entityCoverAccess(w, r, false)
	if !ok || !entityCoverNoBody(w, r) {
		return
	}
	v, e := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if e != nil || strconv.FormatInt(v, 10) != r.PathValue("version") || v < 1 || v > ec.MaxVersion {
		entityCoverFailure(w, ec.ErrInvalid)
		return
	}
	content, ok := p.(ec.OriginalContentStore)
	if !ok {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	first, e := content.ReadEntityCoverOriginalContent(r.Context(), a, ref, v)
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	m, raw := first.Metadata, first.Bytes
	sum := sha256.Sum256(raw)
	if first.SourceProof == "" || !m.Valid(time.Now().UTC()) || m.Ref != ref || m.Version != v || len(raw) != m.ByteCount || hex.EncodeToString(sum[:]) != m.SHA256 {
		entityCoverFailure(w, ec.ErrUnavailable)
		return
	}
	actor, d, e := s.actor(r, a.AccountID != "")
	if e != nil {
		entityCoverFailure(w, e)
		return
	}
	if actor.ID != a.AccountID || actor.AccountType != a.AccountType || d != a.SessionDigest || r.Context().Err() != nil {
		entityCoverFailure(w, ec.ErrChanged)
		return
	}
	if a.AccountID != "" {
		gate, ok := s.access.(entityCoverResponseValidator)
		if !ok {
			entityCoverFailure(w, ec.ErrUnavailable)
			return
		}
		if e = gate.ValidateHumanSocialResponse(r.Context(), d, actor); e != nil {
			entityCoverFailure(w, e)
			return
		}
	}
	if e = s.validateEntityCoverHTTP(r, a); e != nil {
		entityCoverFailure(w, e)
		return
	}
	if e = p.ValidateEntityCoverProjection(r.Context(), a, ref, &m, first.SourceProof); e != nil {
		entityCoverFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
func (s *server) registerEntityCoverRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"activity", "organization"} {
		base := "/v1/" + map[string]string{"activity": "activities", "organization": "organizations"}[kind] + "/{entityID}/cover"
		wrap := func(fn http.HandlerFunc) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("entityType", kind); fn(w, r) }
		}
		mux.HandleFunc("GET "+base, wrap(s.getEntityCover))
		mux.HandleFunc("POST "+base+"/preview", wrap(s.previewEntityCover))
		mux.HandleFunc("PUT "+base, wrap(s.putEntityCover))
		mux.HandleFunc("GET "+base+"/operations/{operationID}", wrap(s.getEntityCoverOperation))
		mux.HandleFunc("POST "+base+"/operations/{operationID}/stop", wrap(s.stopEntityCoverOperation))
		mux.HandleFunc("GET "+base+"/{contentPath...}", wrap(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Split(r.PathValue("contentPath"), "/")
			if len(parts) != 2 || parts[1] != "content" {
				entityCoverFailure(w, ec.ErrInvalid)
				return
			}
			r.SetPathValue("version", parts[0])
			s.getEntityCoverContent(w, r)
		}))
	}
}
