package postgres

import (
	"context"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
)

func TestMessagePolicyUnitNoJSONGrantOrInvalidCurrentWriter(t *testing.T) {
	s := &Store{}
	for _, a := range []ea.Access{{}, {Public: true}, {Actor: identity.Actor{ID: "80000000-0000-4000-8000-000000000001", AccountType: "organization"}, SessionDigest: [32]byte{1}}, {Actor: identity.Actor{ID: "80000000-0000-4000-8000-000000000001", AccountType: "person"}}} {
		if _, e := s.CreateFriendRequestCurrent(context.Background(), a, "peer", "note", "", nil); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("invalid writer reached store")
		}
		if _, e := s.CreateRequestCurrent(context.Background(), a, "peer", "city", "note", "", nil); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("invalid conversation request")
		}
		if _, e := s.DecideRequestCurrent(context.Background(), a, "id", "accept"); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("invalid accept")
		}
		if _, e := s.StartFriendConversationCurrent(context.Background(), a, "id", nil); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("invalid start")
		}
		if _, e := s.SendMessageCurrent(context.Background(), a, "id", "body", "", ""); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("invalid send")
		}
	}
	a := ea.Access{Actor: identity.Actor{ID: "80000000-0000-4000-8000-000000000001", AccountType: "person"}, SessionDigest: [32]byte{1}}
	if _, e := s.messageCurrentAccess(context.Background(), a, "confirmed"); e != mp.ErrInvalid {
		t.Fatal("source condition cannot be model approval")
	}
	ctx, e := s.messageCurrentAccess(context.Background(), a, strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	a.Actor.ID = "foreign"
	a.SessionDigest = [32]byte{2}
	captured := messageCurrentContext(ctx)
	if captured.access.Actor.ID == a.Actor.ID || captured.access.SessionDigest == a.SessionDigest {
		t.Fatal("identity must be captured by value")
	}
	if _, e := s.messageBindAccess(ctx, a); e != identity.ErrUnauthorized {
		t.Fatal("changed caller cannot reuse captured context")
	}
	tx := &messagePolicyUnitTx{}
	if messageAuthenticateCurrent(ctx, tx, "foreign") != identity.ErrUnauthorized || len(tx.queries) != 0 {
		t.Fatal("foreign actor must not reach private SQL")
	}
}
func TestMessagePolicyUnitSameRequestAnnotationAndNoScreenDelivery(t *testing.T) {
	for _, mode := range []mp.Disposition{mp.Request, mp.Screen, mp.Block, mp.Allow} {
		for _, configured := range []bool{false, true} {
			tx := &messagePolicyUnitTx{}
			r := connection.Request{ID: "same-original-request"}
			f := messageRouteFence{peer: "same-original-recipient", version: strings.Repeat("a", 64), configured: configured, route: mode}
			e := messageRecordRequest(context.Background(), tx, &r, f)
			if !configured {
				if e != nil || len(tx.execs) != 0 || r.PolicyDisposition != "" {
					t.Fatal("legacy default has no added side effect")
				}
				continue
			}
			if mode == mp.Allow || mode == mp.Block {
				if e != connection.ErrNotFound || len(tx.execs) != 0 {
					t.Fatal("invalid routing cannot be persisted as a pending request")
				}
				continue
			}
			if e != nil || len(tx.execs) != 1 || tx.args[0][0] != r.ID || tx.args[0][1] != f.peer || r.PolicyDisposition != string(mode) {
				t.Fatal("annotation must refer to same original request/recipient")
			}
			if mode == mp.Screen && (r.ScreeningStatus != "PENDING_REVIEW" || messageOrdinaryDelivery(f)) {
				t.Fatal("screen must not ordinary-deliver or auto-accept")
			}
			if !messageRequestAllowed(f) {
				t.Fatal("pending request is the only permitted effect")
			}
		}
	}
}
