package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
)

type followTargetHTTPSpy struct {
	inbox.Store
	calls     int
	denyFinal bool
	missing   bool
}

type personFollowHTTPAccess struct{ *privateProfileHTTPAccess }

func (p *personFollowHTTPAccess) CaptureHumanControlSession(_ context.Context, d [32]byte, a identity.Actor) (identity.HumanControlFrame, error) {
	if d != p.digest || a != p.actor {
		return identity.HumanControlFrame{}, identity.ErrUnauthorized
	}
	return identity.HumanControlFrame{ID: privateProfileHTTPOwner, Generation: 1}, nil
}

func (p *personFollowHTTPAccess) ValidateHumanControlSession(ctx context.Context, d [32]byte, a identity.Actor, frame identity.HumanControlFrame) error {
	expected, err := p.CaptureHumanControlSession(ctx, d, a)
	if err != nil {
		return err
	}
	if frame != expected {
		return identity.ErrControlChanged
	}
	return nil
}

func (p *followTargetHTTPSpy) GetInboxPersonTarget(_ context.Context, d [32]byte, a identity.Actor, id string) (map[string]any, error) {
	p.calls++
	if p.missing {
		return nil, inbox.ErrNotFound
	}
	if d == ([32]byte{}) || a.ID != privateProfileHTTPOwner || id != humanHTTPOp {
		return nil, inbox.ErrNotFound
	}
	if p.denyFinal && p.calls > 1 {
		return nil, inbox.ErrNotFound
	}
	return map[string]any{"accountId": humanHTTPConv}, nil
}
func TestPersonFollowNotificationHTTPCurrentTargetAndCapability(t *testing.T) {
	for _, final := range []bool{false, true} {
		_, _, access, token := messagePolicyUnitHTTP(t)
		store := &followTargetHTTPSpy{denyFinal: final}
		h := New(nil, &personFollowHTTPAccess{access}, nil, nil, nil, nil, store, nil, nil, nil, nil, nil, false, nil, nil, nil)
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/"+humanHTTPOp+"/person-target", "", token, nil)
		if !final && (w.Code != 200 || !strings.Contains(w.Body.String(), humanHTTPConv) || store.calls < 2) {
			t.Fatal("target final gate", w.Code, store.calls)
		}
		if final && (w.Code != 409 || strings.Contains(w.Body.String(), humanHTTPConv)) {
			t.Fatal("late target leaked", w.Code)
		}
		before := store.calls
		w = messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/"+humanHTTPOp+"/person-target?accountId="+humanHTTPConv, "", token, nil)
		if w.Code != 400 || store.calls != before {
			t.Fatal("caller target accepted", w.Code)
		}
		w = messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/"+humanHTTPOp+"/person-target", "", "", nil)
		if w.Code != 401 || store.calls != before {
			t.Fatal("anonymous target", w.Code)
		}
		store.missing = true
		w = messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/"+humanHTTPOp+"/person-target", "", token, nil)
		if w.Code != 404 || strings.Contains(w.Body.String(), humanHTTPConv) {
			t.Fatal("current target missing", w.Code)
		}
	}
	for _, capable := range []bool{false, true} {
		h, p, token := inboxPageHTTP(t)
		p.page.Items[0].ResourceType = "person_follow"
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/inbox/page?view=notifications", "", token, func(r *http.Request) {
			if capable {
				r.Header.Set("X-Birdtie-Inbox-Features", "person-follow-v1")
			}
		})
		if p.request.SupportsPersonFollow != capable {
			t.Fatal("capability not transported")
		}
		if capable && w.Code != http.StatusOK || !capable && w.Code == http.StatusOK {
			t.Fatal("old client admitted new kind", w.Code)
		}
	}
	stamp := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	cursor, err := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, OwnerID: privateProfileHTTPOwner, View: inbox.NotificationsView, Priority: 50, Time: stamp, ID: humanHTTPOp, SupportsPersonFollow: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = inbox.NormalizePage(inbox.PageRequest{View: inbox.NotificationsView, Cursor: cursor}, privateProfileHTTPOwner); err == nil {
		t.Fatal("new projection cursor accepted by old projection")
	}
}
