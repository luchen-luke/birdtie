package agentpurpose

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func TestPurposeLimitationOfflineContractNeverAuthorizes(t *testing.T) {
	for _, recipient := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business} {
		for _, origin := range []Origin{OriginUnknown, OriginTemporaryActivity} {
			for _, operation := range []Operation{Read, StageMemory, PersistMemory, ModelEgress, Forward} {
				for _, retention := range []Retention{Transient, Persistent} {
					t.Run(string(recipient)+"/"+string(origin)+"/"+string(operation)+"/"+string(retention), func(t *testing.T) {
						err := Check(context.Background(), Request{origin, operation, retention, recipient})
						want := ErrUnavailable
						if retention == Persistent && (operation == StageMemory || operation == PersistMemory) && (origin == OriginTemporaryActivity || recipient != actorref.Person) {
							want = ErrProhibited
						}
						if !errors.Is(err, want) {
							t.Fatalf("guard: %v, want %v", err, want)
						}
					})
				}
			}
		}
	}
	t.Log("OfflineContract: temporary origin is a hypothetical prohibition probe, not a native grant; no branch permits reading, retaining or forwarding")
}

func TestPurposeLimitationMalformedCancelAndWire(t *testing.T) {
	base := Request{OriginTemporaryActivity, PersistMemory, Persistent, actorref.Organization}
	for _, mutate := range []func(*Request){func(r *Request) { r.Origin = "OWNER_CONFIRMED" }, func(r *Request) { r.Operation = "ALLOW" }, func(r *Request) { r.Retention = "FOREVER_APPROVED" }, func(r *Request) { r.Recipient = actorref.Community }} {
		r := base
		mutate(&r)
		if !errors.Is(Check(context.Background(), r), ErrUnavailable) {
			t.Fatal("unknown shape manufactured permission")
		}
	}
	if !errors.Is(Check(nil, base), ErrUnavailable) {
		t.Fatal("nil context did not fail closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(Check(ctx, base), context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if _, err := json.Marshal(base); !errors.Is(err, ErrWire) {
		t.Fatal("guard serialized as authority")
	}
	if err := json.Unmarshal([]byte(`{"Origin":"TEMPORARY_ACTIVITY","Verified":true}`), &base); !errors.Is(err, ErrWire) || base != (Request{}) {
		t.Fatal("wire injected purpose authority")
	}
}
