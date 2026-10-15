package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func inboxPageFailure(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, inbox.ErrInvalidPage):
		respondError(w, 400, "invalid_inbox_page")
	case errors.Is(err, inbox.ErrSourceChanged):
		respondError(w, 409, "inbox_source_changed")
	case errors.Is(err, inbox.ErrNotFound):
		respondError(w, 404, "not_found")
	default:
		socialNowReadError(w, err)
	}
}

func (s *server) listInboxPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.URL.ForceQuery || len(q["view"]) != 1 || q.Get("view") != inbox.NotificationsView {
		inboxPageFailure(w, inbox.ErrInvalidPage)
		return
	}
	in := inbox.PageRequest{View: inbox.NotificationsView, Limit: inbox.DefaultPageLimit}
	for k, v := range q {
		if len(v) != 1 || k != "view" && k != "limit" && k != "cursor" && k != "category" && k != "unreadOnly" && k != "origin" {
			inboxPageFailure(w, inbox.ErrInvalidPage)
			return
		}
		if k == "limit" {
			in.Limit, err = strconv.Atoi(v[0])
			if err != nil || in.Limit < 1 || in.Limit > inbox.MaxPageLimit {
				inboxPageFailure(w, inbox.ErrInvalidPage)
				return
			}
		}
		if k == "origin" {
			in.Origin = v[0]
			if in.Origin != "NATIVE" && in.Origin != "LEGACY_HISTORY" {
				inboxPageFailure(w, inbox.ErrInvalidPage)
				return
			}
		}
		if k == "cursor" {
			in.Cursor = v[0]
		}
		if k == "category" {
			in.Category = v[0]
			if !inbox.ValidCategory(in.Category) {
				inboxPageFailure(w, inbox.ErrInvalidPage)
				return
			}
		}
		if k == "unreadOnly" {
			if v[0] != "true" && v[0] != "false" {
				inboxPageFailure(w, inbox.ErrInvalidPage)
				return
			}
			in.UnreadOnly = v[0] == "true"
		}
	}
	authRequest := r.Clone(r.Context())
	authURL := *r.URL
	authURL.RawQuery = ""
	authURL.ForceQuery = false
	authRequest.URL = &authURL
	if !socialInboxWire(w, authRequest) || !messagePolicyNoBody(w, r) {
		return
	}
	if r.Context().Err() != nil || nilConnectionRequestReader(s.access) {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "personal_inbox_required")
		return
	}
	in, _, err = inbox.NormalizePage(in, actor.ID)
	if err != nil {
		inboxPageFailure(w, err)
		return
	}
	store, ok := s.inbox.(inbox.PagedStore)
	if !ok || nilConnectionRequestReader(store) {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	page, err := store.ListHumanInboxPage(r.Context(), digest, actor, in)
	if err != nil {
		inboxPageFailure(w, err)
		return
	}
	if page.Items == nil {
		page.Items = []inbox.Item{}
	}
	if !validInboxPage(page, in, actor.ID) {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	raw, err := json.Marshal(map[string]any{"data": page})
	if err != nil {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	access, ok := s.access.(socialnow.HumanSessionStore)
	if !ok || nilConnectionRequestReader(access) {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	if err = access.ValidateHumanSocialResponse(r.Context(), digest, actor); err == nil {
		err = store.ValidateHumanInboxPageResponse(r.Context(), digest, actor, in, page)
	}
	if err != nil {
		inboxPageFailure(w, err)
		return
	}
	if r.Context().Err() != nil {
		inboxPageFailure(w, socialnow.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}

func validInboxPage(page inbox.Page, in inbox.PageRequest, owner string) bool {
	if len(page.Items) > in.Limit || page.UnreadCount < 0 || page.HistoricalUnreadCount < 0 {
		return false
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if !inbox.ValidPageID(item.ID) || !inbox.ValidPageID(item.ResourceID) || item.ResourceType == "conversation_message" || item.CreatedAt.IsZero() || seen[item.ID] || item.Title == "" {
			return false
		}
		if (in.Origin == "LEGACY_HISTORY") != (item.Origin == "LEGACY_HISTORY") || in.Category != "" && item.Category != in.Category || in.UnreadOnly && item.ReadAt != nil {
			return false
		}
		for _, id := range []*string{item.TargetActivityID, item.TargetConversationID, item.TargetCommunityID, item.TargetTaskID, item.TargetBusinessID} {
			if id != nil && !inbox.ValidPageID(*id) {
				return false
			}
		}
		seen[item.ID] = true
	}
	if page.NextCursor != nil {
		if len(page.Items) == 0 || *page.NextCursor == "" || *page.NextCursor == in.Cursor {
			return false
		}
		c, err := inbox.DecodePageCursor(*page.NextCursor, owner, in.View)
		last := page.Items[len(page.Items)-1]
		if err != nil || c == nil || c.ID != last.ID || !c.Time.Equal(last.CreatedAt) || c.Category != in.Category || c.UnreadOnly != in.UnreadOnly || c.Origin != in.Origin {
			return false
		}
	}
	return true
}
