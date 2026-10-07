package agentoutbox

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

func TestAgentOutboxLeaseFenceCannotGrantAuthority(t *testing.T) {
	current := outboxClaim()
	if CheckFence(outboxTime(), current, current) != nil {
		t.Fatal("valid comparison")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Claim)
	}{
		{"other_event", func(c *Claim) { c.EventID = testOther }},
		{"other_subject", func(c *Claim) { c.Subject.ID = testOther }},
		{"other_agent", func(c *Claim) { c.AgentID = testOther }},
		{"handler_upgrade", func(c *Claim) { c.HandlerVersion = HandlerV2 }},
		{"other_worker", func(c *Claim) { c.WorkerID = testOther }},
		{"old_fence", func(c *Claim) { c.Fence = 2 }},
		{"old_lease", func(c *Claim) { c.LeaseUntil = c.LeaseUntil.Add(time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			supplied := current
			tc.mutate(&supplied)
			if CheckFence(outboxTime(), current, supplied) != ErrConflict {
				t.Fatal("mismatched lease passed")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Claim)
	}{
		{"no_event", func(c *Claim) { c.EventID = "" }},
		{"zero_subject", func(c *Claim) { c.Subject.ID = "00000000-0000-0000-0000-000000000000" }},
		{"organization", func(c *Claim) { c.Subject.Type = actorref.Organization }},
		{"business", func(c *Claim) { c.Subject.Type = actorref.Business }},
		{"no_agent", func(c *Claim) { c.AgentID = "" }},
		{"model_handler", func(c *Claim) { c.HandlerVersion = "model-v3" }},
		{"zero_worker", func(c *Claim) { c.WorkerID = "00000000-0000-0000-0000-000000000000" }},
		{"zero_fence", func(c *Claim) { c.Fence = 0 }},
		{"negative_fence", func(c *Claim) { c.Fence = -1 }},
		{"overflow_fence", func(c *Claim) { c.Fence = math.MaxInt64 }},
		{"zero_lease", func(c *Claim) { c.LeaseUntil = time.Time{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := current
			tc.mutate(&invalid)
			if ValidateClaim(invalid) != ErrInvalid || CheckFence(outboxTime(), current, invalid) != ErrInvalid {
				t.Fatal("invalid lease passed")
			}
		})
	}
	t.Run("actual_clock_boundary", func(t *testing.T) {
		if CheckFence(current.LeaseUntil, current, current) != ErrExpired {
			t.Fatal("lease at expiry passed")
		}
	})
	t.Run("missing_clock", func(t *testing.T) {
		if CheckFence(time.Time{}, current, current) != ErrInvalid {
			t.Fatal("clock optional")
		}
	})
	t.Run("json_marshal", func(t *testing.T) {
		if _, err := json.Marshal(current); !errors.Is(err, ErrAuthorityJSON) {
			t.Fatal("lease serializable")
		}
	})
	t.Run("json_unmarshal_clears", func(t *testing.T) {
		claim := current
		if err := json.Unmarshal([]byte(`{"Fence":7,"verified":true}`), &claim); err != ErrAuthorityJSON || claim != (Claim{}) {
			t.Fatal("JSON supplied lease authority")
		}
	})
	t.Run("nil_receiver", func(t *testing.T) {
		if (*Claim)(nil).UnmarshalJSON(nil) != ErrAuthorityJSON {
			t.Fatal("nil receiver")
		}
	})
}

func TestAgentOutboxHandlerAndWorkerClosed(t *testing.T) {
	for _, handler := range []HandlerVersion{HandlerV1, HandlerV2} {
		t.Run(string(handler), func(t *testing.T) {
			if ValidateHandlerVersion(handler) != nil {
				t.Fatal("registered control version")
			}
		})
	}
	for _, handler := range []HandlerVersion{"", "mom-control-v3", "MOM-control-v1", " mom-control-v1", "model/confirmed", "business-v1"} {
		t.Run("handler_"+string(handler), func(t *testing.T) {
			if ValidateHandlerVersion(handler) != ErrInvalid {
				t.Fatal("unknown handler accepted")
			}
		})
	}
	for _, worker := range []string{"", "worker-root", "00000000-0000-0000-0000-000000000000", " " + testWorker} {
		t.Run("worker_"+worker, func(t *testing.T) {
			value, err := NormalizeWorkerID(worker)
			if err != ErrInvalid || value != "" {
				t.Fatal("invalid worker accepted")
			}
		})
	}
	if value, err := NormalizeWorkerID(testWorker); err != nil || value != testWorker {
		t.Fatal("valid worker rejected")
	}
}

func TestAgentOutboxStableEffectKeyIgnoresHandlerUpgrade(t *testing.T) {
	p := actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}
	a := EffectAddress{p, p, testAgent, testOperation, testSource, MemoryCandidateEffect}
	first, err := EffectKey(a)
	if err != nil || len(first) != 64 {
		t.Fatal("key shape")
	}
	for _, handler := range []HandlerVersion{HandlerV1, HandlerV2} {
		t.Run(string(handler), func(t *testing.T) {
			for range 100 {
				key, err := EffectKey(a)
				if err != nil || key != first {
					t.Fatal("handler upgrade/retry changed logical effect key")
				}
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*EffectAddress)
	}{
		{"tenant_subject", func(a *EffectAddress) { a.Tenant.ID = testOther; a.Subject.ID = testOther }},
		{"agent", func(a *EffectAddress) { a.AgentID = testOther }},
		{"logical_operation", func(a *EffectAddress) { a.LogicalOperationID = testOther }},
		{"action", func(a *EffectAddress) { a.ActionID = testOther }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := a
			tc.mutate(&copy)
			key, err := EffectKey(copy)
			if err != nil || key == first {
				t.Fatal("effect address collision")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*EffectAddress)
	}{
		{"wrong_subject", func(a *EffectAddress) { a.Subject.ID = testOther }},
		{"organization", func(a *EffectAddress) { a.Tenant.Type = actorref.Organization; a.Subject.Type = actorref.Organization }},
		{"business", func(a *EffectAddress) { a.Tenant.Type = actorref.Business; a.Subject.Type = actorref.Business }},
		{"unknown_kind", func(a *EffectAddress) { a.Kind = "RSVP" }},
		{"invalid_action", func(a *EffectAddress) { a.ActionID = "confirmed" }},
		{"invalid_operation", func(a *EffectAddress) { a.LogicalOperationID = "request-id" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := a
			tc.mutate(&copy)
			key, err := EffectKey(copy)
			if err != ErrInvalid || key != "" {
				t.Fatal("invalid effect key accepted")
			}
		})
	}
}

func TestAgentOutboxConsumerReceiptsAreControlOnly(t *testing.T) {
	e := outboxEvent(MomentCreated)
	for _, tc := range []struct {
		state  State
		reason ReasonCode
	}{{Leased, ""}, {Unavailable, ReasonPurposeUnavailable}, {Invalidated, ReasonSourceInvalidated}, {Expired, ReasonExpired}, {DeadLetter, ReasonAttemptsExhausted}} {
		t.Run(string(tc.state), func(t *testing.T) {
			r := ConsumerRecord{e.EventID, e.Subject, HandlerV1, tc.state, 1, 1, tc.reason, outboxTime(), outboxTime()}
			if ValidateConsumerRecord(r) != nil {
				t.Fatal("control shape")
			}
			r.Reason = "model_confirmed"
			if ValidateConsumerRecord(r) != ErrInvalid {
				t.Fatal("arbitrary reason accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ConsumerRecord)
	}{
		{"succeeded", func(r *ConsumerRecord) { r.State = "SUCCEEDED" }},
		{"pending", func(r *ConsumerRecord) { r.State = Pending }},
		{"cross_subject_type", func(r *ConsumerRecord) { r.Subject.Type = actorref.Business }},
		{"unknown_handler", func(r *ConsumerRecord) { r.HandlerVersion = "approved-v1" }},
		{"missing_event", func(r *ConsumerRecord) { r.EventID = "" }},
		{"zero_fence", func(r *ConsumerRecord) { r.Fence = 0 }},
		{"zero_attempt", func(r *ConsumerRecord) { r.Attempt = 0 }},
		{"too_many_attempts", func(r *ConsumerRecord) { r.Attempt = MaxAttempts + 1 }},
		{"attempt_exceeds_fence", func(r *ConsumerRecord) { r.Attempt = 2 }},
		{"time_regression", func(r *ConsumerRecord) { r.UpdatedAt = r.CreatedAt.Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ConsumerRecord{e.EventID, e.Subject, HandlerV1, Unavailable, 1, 1, ReasonPurposeUnavailable, outboxTime(), outboxTime()}
			tc.mutate(&r)
			if ValidateConsumerRecord(r) != ErrInvalid {
				t.Fatal("invalid receipt accepted")
			}
		})
	}
}

func TestAgentOutboxActualConsumerUnavailableInEveryControlVersion(t *testing.T) {
	for _, kind := range []EventType{MomentCreated, MomentUpdated, MomentWithdrawn} {
		for _, handler := range []HandlerVersion{HandlerV1, HandlerV2} {
			t.Run(string(kind)+"/"+string(handler), func(t *testing.T) {
				r := outboxLease()
				r.Event = outboxEvent(kind)
				claim := outboxClaim()
				claim.EventID = r.Event.EventID
				claim.HandlerVersion = handler
				outcome, err := (ActualConsumer{}).Consume(context.Background(), r, claim)
				if err != ErrUnavailable || ValidateOutcome(outcome) != nil || outcome.Effects != 0 {
					t.Fatal("metadata supplied actual consumer authority")
				}
			})
		}
	}
	t.Run("invalid_record", func(t *testing.T) {
		r := outboxLease()
		r.Event.Subject.ID = testOther
		o, err := (ActualConsumer{}).Consume(context.Background(), r, outboxClaim())
		if err != ErrInvalid || o != (Outcome{}) {
			t.Fatal("invalid metadata accepted")
		}
	})
	t.Run("wrong_event_claim", func(t *testing.T) {
		c := outboxClaim()
		c.EventID = testOther
		o, err := (ActualConsumer{}).Consume(context.Background(), outboxLease(), c)
		if err != ErrInvalid || o != (Outcome{}) {
			t.Fatal("cross event accepted")
		}
	})
	t.Run("nil_context", func(t *testing.T) {
		o, err := (ActualConsumer{}).Consume(nil, outboxLease(), outboxClaim())
		if err != ErrInvalid || o != (Outcome{}) {
			t.Fatal("nil context")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		o, err := (ActualConsumer{}).Consume(ctx, outboxLease(), outboxClaim())
		if err != context.Canceled || o != (Outcome{}) {
			t.Fatal("cancel ignored")
		}
	})
	for _, o := range []Outcome{{Unavailable, ReasonPurposeUnavailable, 1}, {"SUCCEEDED", ReasonPurposeUnavailable, 0}, {Unavailable, "approved", 0}} {
		if ValidateOutcome(o) != ErrInvalid {
			t.Fatal("fake effect outcome accepted")
		}
	}
}

func TestAgentOutboxCandidateReceiptReadOnlyClosed(t *testing.T) {
	r := ConsumerRecord{EventID: testSource, Subject: actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, HandlerVersion: "mom-candidate-local-v1", State: CandidateStaged, Fence: 1, Attempt: 1, Reason: "CANDIDATE_STAGED", CreatedAt: outboxTime(), UpdatedAt: outboxTime()}
	if ValidateConsumerRecord(r) != nil {
		t.Fatal("native terminal metadata")
	}
	r.State = Leased
	r.Reason = ""
	if ValidateConsumerRecord(r) == nil {
		t.Fatal("candidate metadata enabled old control lease")
	}
}
