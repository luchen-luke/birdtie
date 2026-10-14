package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

type inboxPageHTTPSpy struct {
	inbox.Store
	page              inbox.Page
	request           inbox.PageRequest
	actor             identity.Actor
	digest            [32]byte
	expected          inbox.Page
	reads, finals     int
	readErr, finalErr error
}

func (p *inboxPageHTTPSpy) ListHumanInboxPage(_ context.Context, d [32]byte, a identity.Actor, q inbox.PageRequest) (inbox.Page, error) {
	p.reads++
	p.digest, p.actor, p.request = d, a, q
	return p.page, p.readErr
}
func (p *inboxPageHTTPSpy) ValidateHumanInboxPageResponse(_ context.Context, d [32]byte, a identity.Actor, q inbox.PageRequest, v inbox.Page) error {
	p.finals++
	p.expected = v
	if d != p.digest || a != p.actor || q != p.request {
		return identity.ErrUnauthorized
	}
	return p.finalErr
}
func inboxPageHTTP(t *testing.T) (http.Handler, *inboxPageHTTPSpy, string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	p := &inboxPageHTTPSpy{page: inbox.Page{Items: []inbox.Item{{ID: humanHTTPOp, Category: "updates", ResourceType: "activity_change", ResourceID: humanHTTPConv, Title: "私人当前通知", Detail: "PRIVATE_NOTICE_DETAIL", CreatedAt: time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)}}, UnreadCount: 137}}
	return New(nil, a, nil, nil, nil, nil, p, nil, nil, nil, nil, nil, false, nil, nil, nil), p, token
}
func TestInboxPageRegisteredGlobalNotificationCountAndNoWrite(t *testing.T) {
	h, p, token := inboxPageHTTP(t)
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?view=notifications&limit=3", "", token, nil)
	if w.Code != 200 || p.reads != 1 || p.finals != 1 || p.request.Limit != 3 || p.request.View != inbox.NotificationsView || p.actor.ID != privateProfileHTTPOwner || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("code=%d reads=%d final=%d", w.Code, p.reads, p.finals)
	}
	var payload struct {
		Data inbox.Page `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &payload) != nil || payload.Data.UnreadCount != 137 || len(payload.Data.Items) != 1 || payload.Data.NextCursor != nil || len(p.expected.Items) != 1 {
		t.Fatal("notification count inferred from preview or expected projection missing")
	}
}

func TestInboxPageClosedQueryAndActualIdentityBeforeStore(t *testing.T) {
	foreign, _ := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, OwnerID: privateProfileHTTPAgent, View: inbox.NotificationsView, Priority: 50, Time: time.Now().UTC(), ID: humanHTTPConv})
	for _, q := range []string{"", "view=messages", "view=notifications&view=notifications", "view=notifications&limit=0", "view=notifications&limit=51", "view=notifications&limit=abc", "view=notifications&limit=3&limit=3", "view=notifications&ownerId=x", "view=notifications&cursor=%%%", "view=notifications&cursor=" + foreign, "view=notifications&cursor=x&cursor=x"} {
		h, p, token := inboxPageHTTP(t)
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?"+q, "", token, nil)
		if w.Code != 400 || p.reads != 0 {
			t.Fatalf("closed query status=%d reads=%d", w.Code, p.reads)
		}
	}
	for _, tc := range []struct {
		name, body string
		want       int
	}{{"guest", "", 401}, {"body", "{}", 400}, {"organization", "", 403}} {
		h, p, token := inboxPageHTTP(t)
		if tc.name == "guest" {
			token = ""
		}
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?view=notifications", tc.body, token, func(r *http.Request) {
			if tc.name == "organization" {
				r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPAgent)
			}
		})
		if w.Code != tc.want || p.reads != 0 {
			t.Fatalf("identity/body status=%d reads=%d", w.Code, p.reads)
		}
	}
}

func TestInboxPageFinalGateNeverReleasesEncodedPrivateNotice(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{{"session", identity.ErrUnauthorized, 401}, {"projection", inbox.ErrSourceChanged, 409}, {"missing", inbox.ErrNotFound, 404}, {"service", socialnow.ErrUnavailable, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			h, p, token := inboxPageHTTP(t)
			p.finalErr = tc.err
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?view=notifications", "", token, nil)
			if w.Code != tc.want || p.finals != 1 || len(p.expected.Items) != 1 || strings.Contains(w.Body.String(), "私人当前通知") || strings.Contains(w.Body.String(), "PRIVATE_NOTICE_DETAIL") {
				t.Fatalf("encoded stale page status=%d final=%d", w.Code, p.finals)
			}
		})
	}
}

func TestInboxPageRejectsMalformedStoreShape(t *testing.T) {
	cursor, _ := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, OwnerID: privateProfileHTTPOwner, View: inbox.NotificationsView, Priority: 50, Time: time.Now().UTC(), ID: humanHTTPOp})
	for _, kind := range []string{"direct_message", "empty_cursor", "repeat_cursor", "negative_unread", "duplicate"} {
		h, p, token := inboxPageHTTP(t)
		path := "/v1/me/inbox/page?view=notifications"
		switch kind {
		case "direct_message":
			p.page.Items[0].ResourceType = "conversation_message"
		case "empty_cursor":
			p.page.Items = nil
			p.page.NextCursor = &cursor
		case "repeat_cursor":
			p.page.NextCursor = &cursor
			path += "&cursor=" + cursor
		case "negative_unread":
			p.page.UnreadCount = -1
		case "duplicate":
			p.page.Items = append(p.page.Items, p.page.Items[0])
		}
		w := messagePolicyUnitRequest(h, "GET", path, "", token, nil)
		if w.Code != 503 || p.finals != 0 || strings.Contains(w.Body.String(), "PRIVATE_NOTICE_DETAIL") {
			t.Fatalf("malformed %s status=%d final=%d", kind, w.Code, p.finals)
		}
	}
}

type inboxPageLegacyOnly struct{ reads int }

func (p *inboxPageLegacyOnly) ListInbox(context.Context, string) ([]inbox.Item, error) {
	p.reads++
	return []inbox.Item{{Title: "PRIVATE_LEGACY"}}, nil
}
func (*inboxPageLegacyOnly) ReadInboxItem(context.Context, string, string) (inbox.Item, error) {
	panic("paged GET must never mark read")
}

func TestInboxPageHasNoLegacyFallbackAndCancelledRequestDoesNotRead(t *testing.T) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	legacy := &inboxPageLegacyOnly{}
	h := New(nil, a, nil, nil, nil, nil, legacy, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?view=notifications", "", token, nil)
	if w.Code != 503 || legacy.reads != 0 || strings.Contains(w.Body.String(), "PRIVATE_LEGACY") {
		t.Fatal("unbounded oldStore fallback")
	}
	h, p, token := inboxPageHTTP(t)
	w = messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?view=notifications", "", token, func(r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		cancel()
		*r = *r.WithContext(ctx)
	})
	if w.Code != 503 || p.reads != 0 || p.finals != 0 {
		t.Fatal("cancelled caller read private source")
	}
}
