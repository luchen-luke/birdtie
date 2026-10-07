package httpapi

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organizationannouncement"
)

func announcementFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "organization_announcement_unavailable", "暂时无法处理组织公告，请重新读取检查结果。"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(e, agentmemory.ErrForbidden):
		status, code, message = 403, "organization_announcement_forbidden", "当前身份无权管理该组织公告。"
	case errors.Is(e, agentmemory.ErrNotFound):
		status, code, message = 404, "organization_announcement_not_found", "未找到当前可查看的组织公告。"
	case errors.Is(e, agentmemory.ErrInvalid):
		status, code, message = 400, "invalid_organization_announcement", "请检查公告内容、具体版本和有效期限。"
	case errors.Is(e, agentmemory.ErrConflict):
		status, code, message = 409, "organization_announcement_version_conflict", "公告或权限已变化，请重新读取、预览并确认。"
	case errors.Is(e, agentorganizationmemory.ErrLimit):
		status, code, message = 409, "organization_announcement_limit", "公告或未确认预览已达上限；请勿重复创建。"
	}
	respond(w, status, map[string]any{"error": code, "message": message})
}
func announcementEmptyBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 1))
	if e != nil || len(raw) != 0 {
		announcementFailure(w, agentmemory.ErrInvalid)
		return false
	}
	return true
}
func (s *server) announcementAccess(w http.ResponseWriter, r *http.Request) (agentorganizationmemory.Access, agentorganizationmemory.Store, organizationannouncement.Store, bool) {
	a, auth, ok := s.organizationMemoryAccess(w, r)
	if !ok {
		return a, nil, nil, false
	}
	store, ok := s.catalog.(organizationannouncement.Store)
	if !ok {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return a, nil, nil, false
	}
	return a, auth, store, true
}
func validAnnouncementBinding(rec organizationannouncement.Record, a agentorganizationmemory.Access, id string) bool {
	return organizationannouncement.ValidateRecord(rec) == nil && rec.OrganizationID == a.OrganizationID && rec.ID == id
}
func (s *server) listOrganizationAnnouncements(w http.ResponseWriter, r *http.Request) {
	a, auth, store, ok := s.announcementAccess(w, r)
	if !ok || !announcementEmptyBody(w, r) {
		return
	}
	all, e := store.ListOrganizationAnnouncements(r.Context(), a)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if _, e = auth.ValidateOrganizationMemoryAccess(r.Context(), a); e != nil {
		announcementFailure(w, e)
		return
	}
	final, e := store.ListOrganizationAnnouncements(r.Context(), a)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if len(all) > organizationannouncement.MaxResources || len(final) > organizationannouncement.MaxResources || r.Context().Err() != nil {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	seen := map[string]bool{}
	for _, rec := range final {
		if !validAnnouncementBinding(rec, a, rec.ID) || seen[rec.ID] {
			announcementFailure(w, agentmemory.ErrUnavailable)
			return
		}
		seen[rec.ID] = true
	}
	if final == nil {
		final = []organizationannouncement.Record{}
	}
	respond(w, 200, map[string]any{"data": final})
}
func (s *server) readOrganizationAnnouncement(w http.ResponseWriter, r *http.Request) {
	a, auth, store, ok := s.announcementAccess(w, r)
	if !ok || !announcementEmptyBody(w, r) {
		return
	}
	id, e := organizationEvidencePath(r, "announcementID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	rec, e := store.ReadOrganizationAnnouncement(r.Context(), a, id)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	s.announcementResponse(w, r, a, auth, store, id, rec)
}
func (s *server) announcementResponse(w http.ResponseWriter, r *http.Request, a agentorganizationmemory.Access, auth agentorganizationmemory.Store, store organizationannouncement.Store, id string, rec organizationannouncement.Record) {
	if !validAnnouncementBinding(rec, a, id) {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	if _, e := auth.ValidateOrganizationMemoryAccess(r.Context(), a); e != nil {
		announcementFailure(w, e)
		return
	}
	final, e := store.ReadOrganizationAnnouncement(r.Context(), a, id)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if !validAnnouncementBinding(final, a, id) || final.OrganizationAccountID != rec.OrganizationAccountID || final.Revision != rec.Revision || final.State != rec.State || r.Context().Err() != nil {
		announcementFailure(w, agentmemory.ErrConflict)
		return
	}
	respond(w, 200, map[string]any{"data": final})
}
func (s *server) putOrganizationAnnouncement(w http.ResponseWriter, r *http.Request) {
	a, auth, store, ok := s.announcementAccess(w, r)
	if !ok {
		return
	}
	id, e := organizationEvidencePath(r, "announcementID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	in, e := organizationannouncement.DecodeDraft(raw)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	rec, e := store.PutOrganizationAnnouncement(r.Context(), a, id, in)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if rec.State != organizationannouncement.Draft || rec.Revision != in.ExpectedRevision+1 {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	s.announcementResponse(w, r, a, auth, store, id, rec)
}
func (s *server) previewOrganizationAnnouncement(w http.ResponseWriter, r *http.Request) {
	a, auth, store, ok := s.announcementAccess(w, r)
	if !ok {
		return
	}
	id, e := organizationEvidencePath(r, "announcementID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	in, e := organizationannouncement.DecodeRevision(raw)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	preview, e := store.PreviewOrganizationAnnouncement(r.Context(), a, id, in.ExpectedRevision)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	now, e := auth.ValidateOrganizationMemoryAccess(r.Context(), a)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	final, e := store.ReadOrganizationAnnouncement(r.Context(), a, id)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if !organizationannouncement.Canonical(preview.ID) || !validAnnouncementBinding(preview.Announcement, a, id) || !validAnnouncementBinding(final, a, id) || preview.ActingPersonID != a.ActingPersonID || preview.Announcement.State != organizationannouncement.Draft || preview.Announcement.Revision != in.ExpectedRevision || final.Revision != in.ExpectedRevision || final.State != organizationannouncement.Draft || final.OrganizationAccountID != preview.Announcement.OrganizationAccountID || !preview.ExpiresAt.After(now) || preview.ExpiresAt.After(preview.CreatedAt.Add(organizationannouncement.PreviewTTL)) || preview.ExpiresAt.After(final.ValidUntil) || preview.Consequence != organizationannouncement.PublicationConsequence || r.Context().Err() != nil {
		announcementFailure(w, agentmemory.ErrConflict)
		return
	}
	respond(w, 200, map[string]any{"data": preview})
}
func (s *server) publishOrganizationAnnouncement(w http.ResponseWriter, r *http.Request) {
	a, auth, store, ok := s.announcementAccess(w, r)
	if !ok {
		return
	}
	id, e := organizationEvidencePath(r, "announcementID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	in, e := organizationannouncement.DecodePublish(raw)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	rec, e := store.PublishOrganizationAnnouncement(r.Context(), a, id, in)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if rec.State != organizationannouncement.Published || rec.Revision != in.ExpectedRevision+1 {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	s.announcementResponse(w, r, a, auth, store, id, rec)
}
func (s *server) withdrawOrganizationAnnouncement(w http.ResponseWriter, r *http.Request) {
	a, auth, store, ok := s.announcementAccess(w, r)
	if !ok {
		return
	}
	id, e := organizationEvidencePath(r, "announcementID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	raw, ok := orgMemoryBody(w, r)
	if !ok {
		return
	}
	in, e := organizationannouncement.DecodeRevision(raw)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	rec, e := store.WithdrawOrganizationAnnouncement(r.Context(), a, id, in.ExpectedRevision)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if rec.State != organizationannouncement.Withdrawn || rec.Revision != in.ExpectedRevision+1 {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	s.announcementResponse(w, r, a, auth, store, id, rec)
}
func (s *server) readPublicOrganizationAnnouncement(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 || !announcementEmptyBody(w, r) {
		announcementFailure(w, agentmemory.ErrInvalid)
		return
	}
	org, e := organizationEvidencePath(r, "organizationID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	id, e := organizationEvidencePath(r, "announcementID")
	if e != nil {
		announcementFailure(w, e)
		return
	}
	store, ok := s.catalog.(organizationannouncement.Store)
	if !ok {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	actor, digest, e := s.organizationAgentActor(r)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	viewer := actor.ID
	access := organizationannouncement.PublicAccess{ViewerID: viewer, SessionDigest: digest}
	first, e := store.ReadPublicOrganizationAnnouncement(r.Context(), org, id, access)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	finalActor, finalDigest, e := s.organizationAgentActor(r)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if finalActor.ID != viewer || finalDigest != digest {
		announcementFailure(w, agentmemory.ErrForbidden)
		return
	}
	access.ExpectedSessionSnapshot = first.SessionSnapshot
	if viewer != "" && access.ExpectedSessionSnapshot == "" {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	final, e := store.ReadPublicOrganizationAnnouncement(r.Context(), org, id, access)
	if e != nil {
		announcementFailure(w, e)
		return
	}
	if organizationannouncement.ValidatePublic(final, org, id, time.Now().UTC()) != nil || final.Revision != first.Revision || r.Context().Err() != nil {
		announcementFailure(w, agentmemory.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": final})
}
