package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func chatPageFailure(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, connection.ErrInvalidPage) {
		respondError(w, http.StatusBadRequest, "invalid_chat_page")
		return
	}
	connectionError(w, err)
}

func (s *server) chatPageAccess(w http.ResponseWriter, r *http.Request, paged bool) (ea.Access, connection.PagedStore, connection.PageRequest, bool) {
	w.Header().Set("Cache-Control", "no-store")
	in := connection.PageRequest{Limit: connection.DefaultChatPageLimit}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.URL.ForceQuery || !paged && r.URL.RawQuery != "" {
		chatPageFailure(w, connection.ErrInvalidPage)
		return ea.Access{}, nil, in, false
	}
	for k, values := range query {
		if !paged || len(values) != 1 || k != "limit" && k != "cursor" {
			chatPageFailure(w, connection.ErrInvalidPage)
			return ea.Access{}, nil, in, false
		}
		if k == "limit" {
			in.Limit, err = strconv.Atoi(values[0])
			if err != nil || in.Limit < 1 || in.Limit > connection.MaxChatPageLimit {
				chatPageFailure(w, connection.ErrInvalidPage)
				return ea.Access{}, nil, in, false
			}
		} else {
			in.Cursor = values[0]
		}
	}
	if nilConnectionRequestReader(s.access) || r.Context().Err() != nil {
		chatPageFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, in, false
	}
	// Only the closed pagination query above is removed for the common human
	// session guard. Headers, context, body and actual caller stay unchanged.
	authRequest := r.Clone(r.Context())
	authURL := *r.URL
	authURL.RawQuery, authURL.ForceQuery = "", false
	authRequest.URL = &authURL
	a, ok := s.messageWriteAccess(w, authRequest)
	if !ok || !messagePolicyNoBody(w, r) {
		return ea.Access{}, nil, in, false
	}
	port, found := s.connections.(connection.PagedStore)
	if !found || nilConnectionRequestReader(port) {
		chatPageFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, in, false
	}
	return a, port, in, true
}

func (s *server) chatPageResponse(w http.ResponseWriter, r *http.Request, a ea.Access, port connection.PagedStore, ids []string, value any) {
	raw, err := json.Marshal(map[string]any{"data": value})
	if err != nil {
		chatPageFailure(w, mp.ErrUnavailable)
		return
	}
	access, ok := s.access.(socialnow.HumanSessionStore)
	if !ok || nilConnectionRequestReader(access) {
		chatPageFailure(w, mp.ErrUnavailable)
		return
	}
	if err = access.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, a.Actor); err == nil {
		err = port.ValidateChatPageResponse(r.Context(), a, ids, value)
	}
	if err != nil {
		chatPageFailure(w, err)
		return
	}
	if r.Context().Err() != nil {
		chatPageFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}

func (s *server) listConversationsPage(w http.ResponseWriter, r *http.Request) {
	a, port, in, ok := s.chatPageAccess(w, r, true)
	if !ok {
		return
	}
	if _, _, err := connection.NormalizeChatPage(in, a.Actor.ID, ""); err != nil {
		chatPageFailure(w, err)
		return
	}
	page, err := port.ListConversationsPage(r.Context(), a, in)
	if err != nil {
		chatPageFailure(w, err)
		return
	}
	if page.Items == nil {
		page.Items = []connection.Conversation{}
	}
	ids := make([]string, 0, len(page.Items))
	if len(page.Items) > in.Limit || page.UnreadCount < 0 || !validChatNextCursor(page.NextCursor, in.Cursor, a.Actor.ID, "", len(page.Items)) {
		chatPageFailure(w, mp.ErrUnavailable)
		return
	}
	seen := make(map[string]bool, len(page.Items))
	for _, item := range page.Items {
		if !connection.ValidChatPageID(item.ID) || !connection.ValidConversationPeer(item, a.Actor.ID) || seen[item.ID] || item.UnreadCount < 0 {
			chatPageFailure(w, mp.ErrUnavailable)
			return
		}
		seen[item.ID] = true
		ids = append(ids, item.ID)
	}
	s.chatPageResponse(w, r, a, port, ids, page)
}

func validChatNextCursor(next *string, incoming, owner, conversation string, count int) bool {
	if next == nil {
		return true
	}
	if count == 0 || *next == "" || *next == incoming {
		return false
	}
	_, err := connection.DecodePageCursor(*next, owner, conversation)
	return err == nil
}

func (s *server) listMessagesPage(w http.ResponseWriter, r *http.Request) {
	a, port, in, ok := s.chatPageAccess(w, r, true)
	if !ok {
		return
	}
	id := r.PathValue("conversationID")
	if _, _, err := connection.NormalizeChatPage(in, a.Actor.ID, id); err != nil || !connection.ValidChatPageID(id) {
		chatPageFailure(w, connection.ErrInvalidPage)
		return
	}
	page, err := port.ListMessagesPage(r.Context(), a, id, in)
	if err != nil {
		chatPageFailure(w, err)
		return
	}
	if page.Items == nil {
		page.Items = []connection.Message{}
	}
	if len(page.Items) > in.Limit || !validChatNextCursor(page.NextCursor, in.Cursor, a.Actor.ID, id, len(page.Items)) {
		chatPageFailure(w, mp.ErrUnavailable)
		return
	}
	seen := make(map[string]bool, len(page.Items))
	for i, item := range page.Items {
		if !connection.ValidChatPageID(item.ID) || item.ConversationID != id || !connection.ValidChatPageID(item.SenderID) || item.CreatedAt.IsZero() || seen[item.ID] ||
			i > 0 && !connection.MessageBefore(page.Items[i-1], item) {
			chatPageFailure(w, mp.ErrUnavailable)
			return
		}
		seen[item.ID] = true
	}
	s.chatPageResponse(w, r, a, port, []string{id}, page)
}

func (s *server) getConversationCurrent(w http.ResponseWriter, r *http.Request) {
	a, port, _, ok := s.chatPageAccess(w, r, false)
	if !ok {
		return
	}
	id := r.PathValue("conversationID")
	if !connection.ValidChatPageID(id) {
		chatPageFailure(w, connection.ErrInvalidPage)
		return
	}
	item, err := port.ConversationCurrent(r.Context(), a, id)
	if err != nil {
		chatPageFailure(w, err)
		return
	}
	if item.ID != id || !connection.ValidConversationPeer(item, a.Actor.ID) || item.UnreadCount < 0 {
		chatPageFailure(w, mp.ErrUnavailable)
		return
	}
	s.chatPageResponse(w, r, a, port, []string{id}, item)
}
