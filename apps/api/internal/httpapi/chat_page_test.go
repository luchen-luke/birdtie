package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type chatPageHTTPSpy struct {
	connection.Store
	conversations connection.ConversationPage
	messages      connection.MessagePage
	access        ea.Access
	request       connection.PageRequest
	ids           []string
	expected      any
	reads, final  int
	err, finalErr error
}

func (p *chatPageHTTPSpy) ListConversationsPage(_ context.Context, a ea.Access, q connection.PageRequest) (connection.ConversationPage, error) {
	p.access, p.request = a, q
	p.reads++
	return p.conversations, p.err
}
func (p *chatPageHTTPSpy) ListMessagesPage(_ context.Context, a ea.Access, id string, q connection.PageRequest) (connection.MessagePage, error) {
	p.access, p.request = a, q
	p.reads++
	p.ids = []string{id}
	return p.messages, p.err
}
func (p *chatPageHTTPSpy) ConversationCurrent(_ context.Context, a ea.Access, id string) (connection.Conversation, error) {
	p.access = a
	p.reads++
	p.ids = []string{id}
	return p.conversations.Items[0], p.err
}
func (p *chatPageHTTPSpy) ValidateChatPageResponse(_ context.Context, a ea.Access, ids []string, expected any) error {
	p.final++
	p.ids = ids
	p.expected = expected
	if a != p.access {
		return identity.ErrUnauthorized
	}
	return p.finalErr
}
func chatPageHTTP(t *testing.T) (http.Handler, *chatPageHTTPSpy, string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	p := &chatPageHTTPSpy{conversations: connection.ConversationPage{Items: []connection.Conversation{{ID: humanHTTPConv, OtherAccountID: privateProfileHTTPAgent, OtherName: "合成联系人"}}, UnreadCount: 207}, messages: connection.MessagePage{Items: []connection.Message{{ID: humanHTTPOp, ConversationID: humanHTTPConv, SenderID: privateProfileHTTPAgent, SpeakerKind: "human", Body: "合成消息", CreatedAt: time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)}}}}
	return New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil), p, token
}
func TestChatPageRegisteredContractsAndNoWrite(t *testing.T) {
	for _, kind := range []string{"conversations", "messages"} {
		t.Run(kind, func(t *testing.T) {
			h, p, token := chatPageHTTP(t)
			path := "/v1/me/conversations/page?limit=20"
			if kind == "messages" {
				path = "/v1/me/conversations/" + humanHTTPConv + "/messages/page?limit=20"
			}
			w := messagePolicyUnitRequest(h, "GET", path, "", token, nil)
			if w.Code != 200 || p.reads != 1 || p.final != 1 || p.request.Limit != 20 || p.access.Actor.ID != privateProfileHTTPOwner || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("code=%d read=%d final=%d body=%s", w.Code, p.reads, p.final, w.Body.String())
			}
			var root struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &root) != nil || root.Data["items"] == nil || root.Data["nextCursor"] == nil {
				t.Fatal("page DTO missing")
			}
			if kind == "conversations" && string(root.Data["unreadCount"]) != "207" {
				t.Fatal("total unread must not be inferred from first page")
			}
		})
	}
}
func TestChatPageRejectsInvalidAndForeignHintsBeforeStore(t *testing.T) {
	foreign, _ := connection.EncodePageCursor(connection.PageCursor{Version: 1, OwnerID: privateProfileHTTPAgent, Time: time.Now().UTC(), ID: humanHTTPConv})
	other, _ := connection.EncodePageCursor(connection.PageCursor{Version: 1, OwnerID: privateProfileHTTPOwner, ConversationID: humanHTTPOp, Time: time.Now().UTC(), ID: humanHTTPConv})
	for _, query := range []string{"limit=0", "limit=51", "limit=-1", "limit=abc", "limit=20&limit=20", "ownerId=x", "cursor=%%%", "cursor=" + foreign, "cursor=" + other, "cursor=x&cursor=x"} {
		t.Run(query, func(t *testing.T) {
			h, p, token := chatPageHTTP(t)
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/conversations/page?"+query, "", token, nil)
			if w.Code != 400 || p.reads != 0 {
				t.Fatalf("status=%d reads=%d", w.Code, p.reads)
			}
		})
	}
}
func TestChatPageDeniedAndLateAuthorizationNeverReturnsPrivatePage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		initial, final error
		want           int
	}{
		{"anonymous", nil, nil, 401}, {"blocked", connection.ErrNotFound, nil, 404}, {"forbidden", connection.ErrForbidden, nil, 403}, {"expired_reply", nil, identity.ErrUnauthorized, 401}, {"blocked_reply", nil, connection.ErrNotFound, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, p, token := chatPageHTTP(t)
			p.err, p.finalErr = tc.initial, tc.final
			if tc.name == "anonymous" {
				token = ""
			}
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/conversations/"+humanHTTPConv+"/messages/page", "", token, nil)
			if w.Code != tc.want || strings.Contains(w.Body.String(), "合成消息") {
				t.Fatalf("status=%d leaked body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestChatPageDirectConversationUsesExactOriginalDestination(t *testing.T) {
	h, p, token := chatPageHTTP(t)
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/conversations/"+humanHTTPConv, "", token, nil)
	if w.Code != 200 || p.reads != 1 || p.final != 1 || len(p.ids) != 1 || p.ids[0] != humanHTTPConv {
		t.Fatalf("status=%d reads=%d final=%d", w.Code, p.reads, p.final)
	}
	var root struct {
		Data connection.Conversation `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &root) != nil || root.Data.ID != humanHTTPConv {
		t.Fatal("must return original conversation, not a new copy or page")
	}
	if original, ok := p.expected.(connection.Conversation); !ok || original.OtherName != "合成联系人" {
		t.Fatal("final gate missing encoded projection")
	}
}

func TestChatPageDirectConversationClosedQueryAndNoBody(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		headers          map[string]string
		want             int
	}{
		{"foreign_actor", "/v1/me/conversations/" + humanHTTPConv + "?ownerId=" + privateProfileHTTPAgent, "", nil, 400},
		{"body", "/v1/me/conversations/" + humanHTTPConv, "{}", nil, 400},
		{"invalid_id", "/v1/me/conversations/not-a-uuid", "", nil, 400},
		{"empty_query", "/v1/me/conversations/" + humanHTTPConv + "?", "", nil, 400},
		{"query_separators", "/v1/me/conversations/" + humanHTTPConv + "?&&", "", nil, 400},
		{"organization", "/v1/me/conversations/" + humanHTTPConv, "", map[string]string{"X-Birdtie-Organization-Workspace": privateProfileHTTPAgent}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, p, token := chatPageHTTP(t)
			w := messagePolicyUnitRequest(h, "GET", tc.path, tc.body, token, func(r *http.Request) {
				for k, v := range tc.headers {
					r.Header.Set(k, v)
				}
			})
			if w.Code != tc.want || p.reads != 0 || strings.Contains(w.Body.String(), "合成联系人") {
				t.Fatalf("status=%d read=%d", w.Code, p.reads)
			}
		})
	}
}

func TestChatPageFinalRevokedProjectionAndSessionAreNotReturned(t *testing.T) {
	for _, path := range []string{"/v1/me/conversations/page", "/v1/me/conversations/" + humanHTTPConv, "/v1/me/conversations/" + humanHTTPConv + "/messages/page"} {
		for _, err := range []error{connection.ErrConflict, identity.ErrUnauthorized, connection.ErrNotFound} {
			h, p, token := chatPageHTTP(t)
			p.finalErr = err
			w := messagePolicyUnitRequest(h, "GET", path, "", token, nil)
			if w.Code == 200 || p.final != 1 || p.expected == nil || strings.Contains(w.Body.String(), "合成") {
				t.Fatalf("status=%d final=%d private projection escaped", w.Code, p.final)
			}
		}
	}
}

func TestChatPageRejectsMalformedStoreCursorAndHistoryOrder(t *testing.T) {
	for _, kind := range []string{"empty_with_cursor", "repeated_cursor", "wrong_conversation", "reverse_order"} {
		t.Run(kind, func(t *testing.T) {
			h, p, token := chatPageHTTP(t)
			path := "/v1/me/conversations/" + humanHTTPConv + "/messages/page"
			cursor, _ := connection.EncodePageCursor(connection.PageCursor{Version: 1, OwnerID: privateProfileHTTPOwner, ConversationID: humanHTTPConv, Time: time.Now().UTC(), ID: humanHTTPOp})
			switch kind {
			case "empty_with_cursor":
				p.messages.Items = nil
				p.messages.NextCursor = &cursor
			case "repeated_cursor":
				p.messages.NextCursor = &cursor
				path += "?cursor=" + cursor
			case "wrong_conversation":
				p.messages.Items[0].ConversationID = humanHTTPOp
			case "reverse_order":
				older := p.messages.Items[0]
				older.ID = privateProfileHTTPOwner
				older.CreatedAt = older.CreatedAt.Add(-time.Second)
				p.messages.Items = append(p.messages.Items, older)
			}
			w := messagePolicyUnitRequest(h, "GET", path, "", token, nil)
			if w.Code != 503 || p.final != 0 || strings.Contains(w.Body.String(), "合成消息") {
				t.Fatalf("status=%d final=%d", w.Code, p.final)
			}
		})
	}
}
