package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

// Human self-management only. This transport is not a cognitive grant, a
// location history, public social proof or an attendance/check-in authority.
type humanPlaceSignal struct {
	Kind            agentplacememory.Kind       `json:"kind"`
	Basis           agentplacememory.Basis      `json:"basis"`
	SourceKind      agentplacememory.SourceKind `json:"sourceKind"`
	SourceID        string                      `json:"sourceId"`
	Revision        int64                       `json:"revision"`
	RecordCreatedAt time.Time                   `json:"recordCreatedAt"`
	SourceUpdatedAt time.Time                   `json:"sourceUpdatedAt"`
	ValidUntil      *time.Time                  `json:"validUntil,omitempty"`
	Visibility      agentmemory.Visibility      `json:"visibility"`
}
type humanPlaceMemory struct {
	SchemaVersion string             `json:"schemaVersion"`
	OwnerID       string             `json:"ownerId"`
	AgentID       string             `json:"agentId"`
	PlaceID       string             `json:"placeId"`
	CityID        string             `json:"cityId"`
	Signals       []humanPlaceSignal `json:"signals"`
	ObservedAt    time.Time          `json:"observedAt"`
	ExpiresAt     time.Time          `json:"expiresAt"`
	VerifiedVisit string             `json:"verifiedVisit"`
	Attendance    string             `json:"attendance"`
	ModelAccess   string             `json:"modelAccess"`
}
type humanPlaceReceipt struct {
	SchemaVersion string                 `json:"schemaVersion"`
	OwnerID       string                 `json:"ownerId"`
	AgentID       string                 `json:"agentId"`
	MemoryID      string                 `json:"memoryId"`
	Version       int64                  `json:"version"`
	Status        agentmemory.Status     `json:"status"`
	PlaceID       string                 `json:"placeId,omitempty"`
	Kind          agentplacememory.Kind  `json:"kind,omitempty"`
	Basis         agentplacememory.Basis `json:"basis,omitempty"`
	Visibility    agentmemory.Visibility `json:"visibility,omitempty"`
	ValidUntil    *time.Time             `json:"validUntil,omitempty"`
}

func placeMemoryFailure(w http.ResponseWriter, e error) {
	status, code, detail := 503, "place_memory_unavailable", "地点记录暂不可用，请稍后重试。"
	switch {
	case errors.Is(e, agentplacememory.ErrInvalid), errors.Is(e, agentmemory.ErrInvalid):
		status, code, detail = 400, "invalid_place_memory", "记录格式或有效期不正确，请检查后重试。"
	case errors.Is(e, agentplacememory.ErrForbidden), errors.Is(e, agentmemory.ErrForbidden):
		status, code, detail = 403, "place_memory_forbidden", "当前无权查看或修改，请使用本人账号并重新检查权限。"
	case errors.Is(e, agentplacememory.ErrNotFound), errors.Is(e, agentmemory.ErrNotFound):
		status, code, detail = 404, "place_memory_not_found", "当前地点或记录不可用；不能据此判断是否到访或已撤回。"
	case errors.Is(e, agentplacememory.ErrConflict), errors.Is(e, agentmemory.ErrConflict):
		status, code, detail = 409, "place_memory_changed", "记录或来源已变化，请读取当前版本后重新确认。"
	case errors.Is(e, agentplacememory.ErrExpired):
		status, code, detail = 409, "place_memory_expired", "查看已过期，请重新读取当前记录。"
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, detail = 401, "unauthorized", "登录已失效，请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "detail": detail}})
}
func (s *server) placeMemoryAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, agentplacememory.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		placeMemoryFailure(w, agentplacememory.ErrForbidden)
		return agentprofile.PrivateAccess{}, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		placeMemoryFailure(w, agentplacememory.ErrInvalid)
		return agentprofile.PrivateAccess{}, nil, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if e != nil {
		placeMemoryFailure(w, e)
		return agentprofile.PrivateAccess{}, nil, false
	}
	p, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if e != nil || p.Type != actorref.Person {
		placeMemoryFailure(w, agentplacememory.ErrForbidden)
		return agentprofile.PrivateAccess{}, nil, false
	}
	port, ok := s.catalog.(agentplacememory.Store)
	if !ok || port == nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return agentprofile.PrivateAccess{}, nil, false
	}
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: p}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		placeMemoryFailure(w, agentplacememory.ErrForbidden)
		return agentprofile.PrivateAccess{}, nil, false
	}
	return a, port, true
}
func placeHumanProjection(p agentplacememory.Projection) humanPlaceMemory {
	out := humanPlaceMemory{SchemaVersion: "human-place-memory-v1", OwnerID: p.Owner.ID, AgentID: p.AgentID, PlaceID: p.PlaceID, CityID: p.CityID, Signals: make([]humanPlaceSignal, 0, len(p.Signals)), ObservedAt: p.ObservedAt, ExpiresAt: p.ExpiresAt, VerifiedVisit: "UNAVAILABLE", Attendance: "UNAVAILABLE", ModelAccess: "UNAVAILABLE"}
	for _, v := range p.Signals {
		out.Signals = append(out.Signals, humanPlaceSignal{v.Kind, v.Basis, v.Source.Kind, v.Source.ID, v.Source.Version.Revision, v.RecordCreatedAt, v.SourceUpdatedAt, v.ValidUntil, v.Visibility})
	}
	return out
}

// Marshal before any final native authorization. For reads the final source
// revalidation also checks the original Session/Agent/metadata binding after
// all preceding waits; no authentication wait follows that revalidation.
func (s *server) placeMemoryRespond(w http.ResponseWriter, r *http.Request, a agentprofile.PrivateAccess, port agentplacememory.Store, p *agentplacememory.Projection, value any) {
	raw, e := json.Marshal(map[string]any{"data": value})
	if e != nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	sessions, ok := s.access.(socialnow.HumanSessionStore)
	if !ok {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	if e = sessions.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, identity.Actor{ID: a.WorkspacePrincipal.ID, AccountType: "person"}); e != nil {
		placeMemoryFailure(w, e)
		return
	}
	if p != nil {
		if e = port.RevalidateOwnPlaceMemory(r.Context(), a, *p); e != nil {
			placeMemoryFailure(w, e)
			return
		}
	}
	if r.Context().Err() != nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) getOwnPlaceMemory(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.placeMemoryAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			placeMemoryFailure(w, agentplacememory.ErrInvalid)
			return
		}
	}
	id := r.PathValue("placeID")
	if !agentplacememory.ValidID(id) {
		placeMemoryFailure(w, agentplacememory.ErrInvalid)
		return
	}
	p, e := port.ReadOwnPlaceMemory(r.Context(), a, id)
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	// Validate structure using the native observation clock. The final Store
	// revalidation checks the original lease against current PostgreSQL time
	// after all waits; the HTTP host clock must not replace that authority.
	if agentplacememory.Validate(p, p.ObservedAt) != nil || p.Owner != a.WorkspacePrincipal || p.PlaceID != id {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	s.placeMemoryRespond(w, r, a, port, &p, placeHumanProjection(p))
}

type humanPlaceDeclarationControls struct {
	SchemaVersion string                              `json:"schemaVersion"`
	OwnerID       string                              `json:"ownerId"`
	AgentID       string                              `json:"agentId"`
	PlaceID       string                              `json:"placeId"`
	Declarations  []agentplacememory.HumanDeclaration `json:"declarations"`
	ObservedAt    time.Time                           `json:"observedAt"`
	ExpiresAt     time.Time                           `json:"expiresAt"`
	ModelAccess   string                              `json:"modelAccess"`
}

// This separate private control read is not the current public Place/source
// projection. Hidden targets allow only owner declaration control metadata.
func (s *server) getOwnPlaceDeclarationControls(w http.ResponseWriter, r *http.Request) {
	a, _, ok := s.placeMemoryAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			placeMemoryFailure(w, agentplacememory.ErrInvalid)
			return
		}
	}
	place := r.PathValue("placeID")
	if !agentplacememory.ValidID(place) {
		placeMemoryFailure(w, agentplacememory.ErrInvalid)
		return
	}
	port, ok := s.catalog.(agentplacememory.HumanControlStore)
	if !ok || port == nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	c, e := port.ReadOwnPlaceDeclarationControls(r.Context(), a, place)
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	if agentplacememory.ValidateHumanControl(c, c.ObservedAt) != nil || c.Owner != a.WorkspacePrincipal || c.PlaceID != place {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	dto := humanPlaceDeclarationControls{SchemaVersion: "human-place-declaration-controls-v1", OwnerID: c.Owner.ID, AgentID: c.AgentID, PlaceID: c.PlaceID, Declarations: c.Declarations, ObservedAt: c.ObservedAt, ExpiresAt: c.ExpiresAt, ModelAccess: "UNAVAILABLE"}
	raw, e := json.Marshal(map[string]any{"data": dto})
	if e != nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	sessions, ok := s.access.(socialnow.HumanSessionStore)
	if !ok {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	if e = sessions.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, identity.Actor{ID: a.WorkspacePrincipal.ID, AccountType: "person"}); e != nil {
		placeMemoryFailure(w, e)
		return
	}
	if e = port.RevalidateOwnPlaceDeclarationControls(r.Context(), a, c); e != nil {
		placeMemoryFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func placeDeclarationBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	m, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(m, "application/json") {
		respondError(w, 415, "json_required")
		return nil, false
	}
	if r.Body == nil {
		placeMemoryFailure(w, agentplacememory.ErrInvalid)
		return nil, false
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 2049))
	if e != nil || len(b) > 2048 {
		respondError(w, 413, "place_declaration_body_too_large")
		return nil, false
	}
	return b, true
}
func (s *server) putOwnPlaceDeclaration(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.placeMemoryAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("memoryID")
	if !agentplacememory.ValidID(id) {
		placeMemoryFailure(w, agentplacememory.ErrInvalid)
		return
	}
	raw, ok := placeDeclarationBody(w, r)
	if !ok {
		return
	}
	expectedAgent, in, e := agentplacememory.DecodeHumanPut(raw, time.Now().UTC())
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	bound, ok := s.catalog.(agentplacememory.HumanBoundDeclarationStore)
	if !ok || bound == nil {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	rec, e := bound.PutOwnPlaceDeclarationBound(r.Context(), a, expectedAgent, id, in)
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	d, e := agentplacememory.DecodeDeclaration(rec)
	if e != nil || rec.OwnerID != a.WorkspacePrincipal.ID || rec.AgentID != expectedAgent || rec.ID != id || rec.Status != agentmemory.StatusActive || (rec.Version != in.ExpectedVersion+1 && rec.Version != in.ExpectedVersion) || d.PlaceID != in.PlaceID || d.Kind != in.Kind || rec.Visibility != in.Visibility || !rec.ValidUntil.Equal(in.ValidUntil) {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	p, e := port.ReadOwnPlaceMemory(r.Context(), a, in.PlaceID)
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	found := false
	for _, sig := range p.Signals {
		if sig.Source.Kind == agentplacememory.DeclarationSource && sig.Source.ID == id && sig.Source.Version.Revision == rec.Version && sig.Kind == in.Kind {
			found = true
		}
	}
	if !found || p.AgentID != rec.AgentID || p.Owner != a.WorkspacePrincipal || agentplacememory.Validate(p, p.ObservedAt) != nil {
		placeMemoryFailure(w, agentplacememory.ErrConflict)
		return
	}
	receipt := humanPlaceReceipt{SchemaVersion: "human-place-declaration-receipt-v1", OwnerID: rec.OwnerID, AgentID: rec.AgentID, MemoryID: id, Version: rec.Version, Status: rec.Status, PlaceID: d.PlaceID, Kind: d.Kind, Basis: d.Basis, Visibility: rec.Visibility, ValidUntil: &rec.ValidUntil}
	s.placeMemoryRespond(w, r, a, port, &p, receipt)
}
func (s *server) deleteOwnPlaceDeclaration(w http.ResponseWriter, r *http.Request) {
	a, port, ok := s.placeMemoryAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("memoryID")
	if !agentplacememory.ValidID(id) {
		placeMemoryFailure(w, agentplacememory.ErrInvalid)
		return
	}
	raw, ok := placeDeclarationBody(w, r)
	if !ok {
		return
	}
	in, e := agentmemory.DecodeDeleteInput(raw)
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	rec, e := port.DeleteOwnPlaceDeclaration(r.Context(), a, id, in.ExpectedVersion)
	if e != nil {
		placeMemoryFailure(w, e)
		return
	}
	if agentmemory.ValidateRecord(rec) != nil || rec.OwnerID != a.WorkspacePrincipal.ID || rec.ID != id || rec.Status != agentmemory.StatusDeleted || rec.Version != in.ExpectedVersion+1 {
		placeMemoryFailure(w, agentplacememory.ErrUnavailable)
		return
	}
	// Deleted receipt contains no erased body or stale Place facts. Hidden Places
	// must not prevent the owner from withdrawing an existing declaration.
	s.placeMemoryRespond(w, r, a, port, nil, humanPlaceReceipt{SchemaVersion: "human-place-declaration-receipt-v1", OwnerID: rec.OwnerID, AgentID: rec.AgentID, MemoryID: id, Version: rec.Version, Status: rec.Status})
}
