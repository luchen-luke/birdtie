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
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type contactPageHTTPSpy struct {
	connection.Store
	ties          connection.ContactPage[connection.Tie]
	follows       connection.ContactPage[follow.Record]
	requests      connection.ContactPage[connection.Request]
	access        ea.Access
	query         connection.PageRequest
	kind, id      string
	reads, final  int
	err, finalErr error
}

func (p *contactPageHTTPSpy) ListContactTiesPage(_ context.Context, a ea.Access, q connection.PageRequest) (connection.ContactPage[connection.Tie], error) {
	p.access, p.query, p.kind = a, q, connection.ContactTies
	p.reads++
	return p.ties, p.err
}
func (p *contactPageHTTPSpy) ListContactFollowsPage(_ context.Context, a ea.Access, q connection.PageRequest) (connection.ContactPage[follow.Record], error) {
	p.access, p.query, p.kind = a, q, connection.ContactFollows
	p.reads++
	return p.follows, p.err
}
func (p *contactPageHTTPSpy) ListContactRequestsPage(_ context.Context, a ea.Access, q connection.PageRequest) (connection.ContactPage[connection.Request], error) {
	p.access, p.query, p.kind = a, q, connection.ContactRequests
	p.reads++
	return p.requests, p.err
}
func (p *contactPageHTTPSpy) ValidateContactPageResponse(_ context.Context, a ea.Access, kind string, q connection.PageRequest, v any) error {
	p.final++
	if a != p.access || kind != p.kind || q != p.query || v == nil {
		return identity.ErrUnauthorized
	}
	return p.finalErr
}
func (p *contactPageHTTPSpy) ReadContactRequestCurrent(_ context.Context, a ea.Access, id string) (connection.Request, error) {
	p.reads++
	p.access, p.id = a, id
	return p.requests.Items[0], p.err
}
func (p *contactPageHTTPSpy) ValidateContactRequestResponse(_ context.Context, a ea.Access, r connection.Request) error {
	p.final++
	if a != p.access || r.ID != p.id {
		return identity.ErrUnauthorized
	}
	return p.finalErr
}
func contactPageHTTP(t *testing.T) (http.Handler, *contactPageHTTPSpy, string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	stamp := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	p := &contactPageHTTPSpy{
		ties:     connection.ContactPage[connection.Tie]{Items: []connection.Tie{{ID: humanHTTPOp, OtherAccountID: privateProfileHTTPAgent, OtherName: "合成联系人", CreatedAt: stamp}}},
		follows:  connection.ContactPage[follow.Record]{Items: []follow.Record{{ID: humanHTTPOp, Target: follow.Target{Type: follow.Person, ID: privateProfileHTTPAgent}, Label: "合成关注", CreatedAt: stamp}}},
		requests: connection.ContactPage[connection.Request]{Items: []connection.Request{{ID: humanHTTPOp, OtherAccountID: privateProfileHTTPAgent, OtherName: "合成申请", Direction: "incoming", Scope: "friend", State: "pending", Note: "合成申请文字", CreatedAt: stamp, ExpiresAt: stamp.Add(time.Hour)}}},
	}
	return New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil), p, token
}
func TestContactPageRegisteredClosedWireAndFinalGate(t *testing.T) {
	for _, kind := range []string{connection.ContactTies, connection.ContactFollows, connection.ContactRequests} {
		h, p, token := contactPageHTTP(t)
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/"+kind+"/page", "", token, nil)
		if w.Code != 200 || p.reads != 1 || p.final != 1 || p.query.Limit != 20 || p.kind != kind || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("kind %s status %d reads %d final %d", kind, w.Code, p.reads, p.final)
		}
		var v struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Data) != 2 || v.Data["items"] == nil || string(v.Data["nextCursor"]) != "null" {
			t.Fatal("wire items/nextCursor only")
		}
	}
}
func TestContactPageRejectsForeignCursorAndClosedQueryBeforeRead(t *testing.T) {
	stamp := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	foreign, _ := connection.EncodeContactCursor(connection.ContactCursor{Version: 1, OwnerID: privateProfileHTTPAgent, Kind: connection.ContactTies, Snapshot: stamp, Time: stamp, ID: humanHTTPOp})
	wrong, _ := connection.EncodeContactCursor(connection.ContactCursor{Version: 1, OwnerID: privateProfileHTTPOwner, Kind: connection.ContactFollows, Snapshot: stamp, Time: stamp, ID: humanHTTPOp})
	for _, q := range []string{"limit=0", "limit=51", "limit=-1", "limit=a", "limit=1&limit=2", "owner=x", "cursor=", "cursor=%%%", "cursor=" + foreign, "cursor=" + wrong, "cursor=x&cursor=x", "&&"} {
		h, p, token := contactPageHTTP(t)
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/ties/page?"+q, "", token, nil)
		if w.Code != 400 || p.reads != 0 {
			t.Fatalf("query %s status %d reads%d", q, w.Code, p.reads)
		}
	}
}
func TestContactPageFinalDenialAndMalformedPageNeverLeaks(t *testing.T) {
	for _, kind := range []string{connection.ContactTies, connection.ContactFollows, connection.ContactRequests} {
		for _, err := range []error{identity.ErrUnauthorized, connection.ErrConflict, connection.ErrNotFound} {
			h, p, token := contactPageHTTP(t)
			p.finalErr = err
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/"+kind+"/page", "", token, nil)
			if w.Code == 200 || p.final != 1 || strings.Contains(w.Body.String(), "合成") {
				t.Fatal("stale response leaked")
			}
		}
	}
	h, p, token := contactPageHTTP(t)
	p.ties.Items = append(p.ties.Items, p.ties.Items[0])
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/ties/page", "", token, nil)
	if w.Code != 503 || p.final != 0 {
		t.Fatal("duplicate page")
	}
	for _, tc := range []struct {
		body           string
		anonymous, org bool
		want           int
	}{{"", true, false, 401}, {"{}", false, false, 400}, {"", false, true, 403}} {
		h, p, token = contactPageHTTP(t)
		if tc.anonymous {
			token = ""
		}
		w = messagePolicyUnitRequest(h, "GET", "/v1/me/ties/page", tc.body, token, func(r *http.Request) {
			if tc.org {
				r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPAgent)
			}
		})
		if w.Code != tc.want || p.reads != 0 {
			t.Fatalf("transport status%d", w.Code)
		}
	}
}
func TestContactPageDirectRequestCurrentExactIDAndDenial(t *testing.T) {
	for _, e := range []error{nil, connection.ErrNotFound, identity.ErrUnauthorized, connection.ErrConflict} {
		h, p, token := contactPageHTTP(t)
		p.finalErr = e
		w := messagePolicyUnitRequest(h, "GET", "/v1/me/connection-requests/"+humanHTTPOp, "", token, nil)
		if e == nil {
			if w.Code != 200 || p.id != humanHTTPOp || p.reads != 1 || p.final != 1 {
				t.Fatal("direct original request")
			}
			var out struct {
				Data connection.Request `json:"data"`
			}
			if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.ID != humanHTTPOp {
				t.Fatal("wrong destination")
			}
		} else if w.Code == 200 || strings.Contains(w.Body.String(), "合成") {
			t.Fatal("direct late denial leaked")
		}
	}
	h, p, token := contactPageHTTP(t)
	p.requests.Items[0].ID = privateProfileHTTPAgent
	w := messagePolicyUnitRequest(h, "GET", "/v1/me/connection-requests/"+humanHTTPOp, "", token, nil)
	if w.Code != 503 || p.final != 0 {
		t.Fatal("foreign request reply")
	}
}
