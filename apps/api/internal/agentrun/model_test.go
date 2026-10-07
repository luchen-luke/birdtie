package agentrun

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"testing"
	"time"
)

func TestRunStateTransitionClosed(t *testing.T) {
	allowed := map[State][]State{Queued: {Running, WaitingConfirmation, Cancelled, Expired}, Running: {RetryWait, Succeeded, Failed, Cancelled, Expired}, WaitingConfirmation: {Queued, Cancelled, Expired}, RetryWait: {Running, Failed, Cancelled, Expired}}
	states := []State{Queued, Running, WaitingConfirmation, RetryWait, Succeeded, Failed, Cancelled, Expired, State("UNKNOWN")}
	for _, from := range states {
		for _, to := range states {
			t.Run(string(from)+"_"+string(to), func(t *testing.T) {
				want := false
				for _, s := range allowed[from] {
					if s == to {
						want = true
					}
				}
				if CanTransition(from, to) != want {
					t.Fatalf("%s -> %s incorrect", from, to)
				}
			})
		}
	}
}

func TestRunRecordClosedMetadataShapeAndSuccessMeaning(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: "11111111-1111-4111-8111-111111111111"}
	r := Record{SchemaVersion: Schema, ID: "22222222-2222-4222-8222-222222222222", Owner: owner, AgentID: "33333333-3333-4333-8333-333333333333", Source: agentevent.SourceReference{Type: agentevent.MomentSource, ID: "44444444-4444-4444-8444-444444444444", Owner: owner, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 1}}, EventID: "55555555-5555-4555-8555-555555555555", LogicalOperationID: "66666666-6666-4666-8666-666666666666", State: WaitingConfirmation, Checkpoint: Scheduled, Version: 1, Reason: "WAITING_APPROVAL", CreatedAt: now, UpdatedAt: now, ObservedAt: now, Deadline: now.Add(time.Minute)}
	if ValidateRecord(r) != nil {
		t.Fatal("valid waiting metadata shape")
	}
	for _, mutate := range []func(*Record){func(r *Record) { r.State = "UNKNOWN" }, func(r *Record) { r.Reason = "RAW_PRIVATE_PROVIDER_FAILURE" }, func(r *Record) { r.ModelAccess = true }, func(r *Record) { r.MemoryPromotionAllowed = true }, func(r *Record) { r.Committed = true }, func(r *Record) { r.CandidateID = "77777777-7777-4777-8777-777777777777" }, func(r *Record) { r.Source.Owner.ID = r.ID }, func(r *Record) { r.UpdatedAt = now.Add(time.Second) }, func(r *Record) { r.Deadline = now.Add(MaxLifetime + time.Second) }, func(r *Record) { r.Attempt = MaxAttempts + 1 }, func(r *Record) { r.State = Succeeded; r.Reason = "COMMITTED" }} {
		bad := r
		mutate(&bad)
		if ValidateRecord(bad) == nil {
			t.Fatal("invalid closed metadata accepted")
		}
	}
}
func TestRunLeaseShapeDoesNotAuthorizeAndCannotTravelJSON(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	c := Claim{RunID: "a9715e6d-5e54-4b39-b8fc-f7d8a0a60321", WorkerID: "b9715e6d-5e54-4b39-b8fc-f7d8a0a60321", Fence: 1, Attempt: 1, LeaseUntil: now.Add(time.Second)}
	if ValidateClaim(c) != nil || CheckLease(now, c, c) != nil {
		t.Fatal("valid shape rejected")
	}
	if _, e := json.Marshal(c); e == nil {
		t.Fatal("JSON supplied run lease authority")
	}
	var supplied Claim
	if e := json.Unmarshal([]byte(`{}`), &supplied); e == nil || supplied != (Claim{}) {
		t.Fatal("JSON decoded authority")
	}
	other := c
	other.Fence++
	if CheckLease(now, c, other) != ErrConflict {
		t.Fatal("old fence accepted")
	}
	if CheckLease(c.LeaseUntil, c, c) != ErrExpired {
		t.Fatal("deadline equality accepted")
	}
	for _, mutate := range []func(*Claim){func(c *Claim) { c.Fence = 0 }, func(c *Claim) { c.Attempt = MaxAttempts + 1 }, func(c *Claim) { c.Attempt = 2 }, func(c *Claim) { c.WorkerID = "actor" }, func(c *Claim) { c.LeaseUntil = time.Time{} }} {
		bad := c
		mutate(&bad)
		if ValidateClaim(bad) == nil {
			t.Fatal("invalid native claim shape")
		}
	}
}
func TestRunTerminalAndCheckpointSemantics(t *testing.T) {
	for _, s := range []State{Succeeded, Failed, Cancelled, Expired} {
		if !Terminal(s) {
			t.Fatal("terminal not closed")
		}
	}
	for _, s := range []State{Queued, Running, WaitingConfirmation, RetryWait, State("UNKNOWN")} {
		if Terminal(s) {
			t.Fatal("waiting/unknown became completion")
		}
	}
	for _, c := range []Checkpoint{Scheduled, Validated, DispatchPending, ReconcileEffect, EffectConfirmed} {
		if !ValidCheckpoint(c) {
			t.Fatal("known checkpoint rejected")
		}
	}
	if ValidCheckpoint(Checkpoint("MODEL_THOUGHT")) {
		t.Fatal("arbitrary context checkpoint accepted")
	}
}
