package agentoutboxmaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
)

const owner = "89000000-0000-4000-8000-000000000001"
const other = "89000000-0000-4000-8000-000000000007"
const worker = "89000000-0000-4000-8000-000000000006"

type testStore struct {
	claim            func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error)
	consume          func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error)
	claims, consumes int
}

func (s *testStore) ClaimAgentOutboxControlForSubject(c context.Context, p actorref.PrincipalRef, w string, h agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
	s.claims++
	return s.claim(c, p, w, h)
}
func (s *testStore) ConsumeAgentOutboxControl(c context.Context, a agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	s.consumes++
	return s.consume(c, a)
}

func options() Options {
	return Options{Subject: actorref.PrincipalRef{Type: actorref.Person, ID: owner}, WorkerID: worker, Handler: agentoutbox.HandlerV1, Batch: 1, Timeout: time.Second}
}

type mixedRunnerStore struct {
	testStore
	mixed      func(context.Context, actorref.PrincipalRef, string) (agentoutbox.Record, agentoutbox.Claim, error)
	mixedCalls int
}

func (s *mixedRunnerStore) ClaimMixedAgentOutboxControlForSubject(c context.Context, p actorref.PrincipalRef, w string) (agentoutbox.Record, agentoutbox.Claim, error) {
	s.mixedCalls++
	return s.mixed(c, p, w)
}
func mixedRunnerRecords(handler agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, agentoutbox.ConsumerRecord) {
	r, c, p := records()
	switch handler {
	case agentoutbox.MemoryHandler:
		r.Event.SchemaVersion = agentoutbox.MemorySchema
		r.Event.EventType = agentoutbox.MemoryUpdated
		r.Event.Source.Type = agentoutbox.MemorySource
		r.Event.Source.Status = agentoutbox.MemoryActive
		r.Event.Source.Revision = 2
		r.Event.LogicalOperationID = agentoutbox.MemoryOperationID(r.Event.Source.ID, 2, r.Event.EventType)
	case agentoutbox.PreferenceHandler:
		r.Event.SchemaVersion = agentoutbox.PreferenceSchema
		r.Event.EventType = agentoutbox.PreferenceUpdated
		r.Event.Source.Type = agentoutbox.PreferenceSource
		r.Event.Source.ID = r.Event.AgentID
		r.Event.Source.Status = agentoutbox.PreferenceConfigured
		r.Event.Source.Revision = 2
		r.Event.LogicalOperationID = agentoutbox.PreferenceOperationID(r.Event.Source.ID, 2, r.Event.EventType)
	case agentoutbox.TaskRequestHandler:
		r.Event.SchemaVersion = agentoutbox.TaskRequestSchema
		r.Event.EventType = agentoutbox.TaskRequestChanged
		r.Event.Source.Type = agentoutbox.TaskRequestSource
		r.Event.Source.Status = agentoutbox.TaskRequestActive
		r.Event.Source.Revision = 0
		r.Event.LogicalOperationID = agentoutbox.TaskRequestOperationID(r.Event.Source.ID, r.Event.Source.Fingerprint, r.Event.EventType)
	case agentoutbox.ParticipationHandler:
		r.Event.SchemaVersion = agentoutbox.ParticipationSchema
		r.Event.EventType = agentoutbox.ParticipationUpdated
		r.Event.Source.Type = agentoutbox.ParticipationSource
		r.Event.Source.Status = agentoutbox.ParticipationGoing
		r.Event.Source.Revision = 0
		r.Event.LogicalOperationID = agentoutbox.ParticipationOperationID(r.Event.Source.ID, r.Event.Source.Fingerprint)
	}
	if handler != agentoutbox.HandlerV1 {
		r.Event.RootTraceID = r.Event.LogicalOperationID
		r.Event.EventID = agentoutbox.StableEventID(r.Event)
		c.HandlerVersion = handler
		c.EventID = r.Event.EventID
		p.EventID = c.EventID
		p.HandlerVersion = handler
		p.State = agentoutbox.MemoryComplete
		p.Reason = agentoutbox.ReasonCleanupComplete
	}
	return r, c, p
}
func TestMixedMaintenanceUnitRunnerActualHandlersAndRetirement(t *testing.T) {
	o := options()
	o.MixedHandlers = true
	o.Batch = 10
	sequence := []agentoutbox.HandlerVersion{agentoutbox.MemoryHandler, agentoutbox.PreferenceHandler, agentoutbox.TaskRequestHandler, agentoutbox.ParticipationHandler, agentoutbox.HandlerV1}
	fixtures := make([]struct {
		r agentoutbox.Record
		c agentoutbox.Claim
		p agentoutbox.ConsumerRecord
	}, len(sequence))
	for i, h := range sequence {
		fixtures[i].r, fixtures[i].c, fixtures[i].p = mixedRunnerRecords(h)
	}
	i := 0
	s := &mixedRunnerStore{}
	s.claim = func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
		t.Fatal("mixed dispatched through a fixed public claim")
		return agentoutbox.Record{}, agentoutbox.Claim{}, nil
	}
	s.mixed = func(_ context.Context, p actorref.PrincipalRef, w string) (agentoutbox.Record, agentoutbox.Claim, error) {
		if p != o.Subject || w != o.WorkerID {
			t.Fatal("mixed scope changed")
		}
		if s.mixedCalls == 1 {
			return agentoutbox.Record{}, agentoutbox.Claim{}, ErrNativeControlRetired
		}
		if i == len(sequence) {
			return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrNotFound
		}
		return fixtures[i].r, fixtures[i].c, nil
	}
	s.consume = func(_ context.Context, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
		expected, p := fixtures[i].c, fixtures[i].p
		if c != expected {
			t.Fatal("actual native handler/claim changed")
		}
		i++
		if c.HandlerVersion == agentoutbox.HandlerV1 {
			return p, agentoutbox.ErrUnavailable
		}
		return p, nil
	}
	result := Run(context.Background(), s, o)
	if result.Status != "FINISHED" || result.Reason != "NO_ELIGIBLE_WORK" || result.ConfirmedReceipts != 5 || result.Counts.RetiredControls != 1 || result.Counts.MaintenanceComplete != 4 || result.Counts.Unavailable != 1 || result.BusinessExecution != "UNAVAILABLE" || s.mixedCalls != 7 || s.claims != 0 || s.consumes != 5 {
		t.Fatal(result, s.mixedCalls, s.claims, s.consumes)
	}
}
func TestMixedMaintenanceUnitRunnerClosedCapabilitiesAndUnknownResults(t *testing.T) {
	o := options()
	o.MixedHandlers = true
	o.Batch = 10
	if result := Run(context.Background(), &testStore{}, o); result.Reason != "MIXED_MAINTENANCE_UNAVAILABLE" {
		t.Fatal("missing capability fell back to fixed handler", result)
	}
	for _, handler := range []agentoutbox.HandlerVersion{agentoutbox.HandlerV2, agentoutbox.MemoryHandler, agentoutbox.PreferenceHandler, agentoutbox.TaskRequestHandler, agentoutbox.ParticipationHandler} {
		copy := o
		copy.Handler = handler
		if ValidateOptions(copy) {
			t.Fatal("mixed overrode explicit handler")
		}
	}
	for _, e := range []error{fmt.Errorf("private: %w", ErrNativeControlRetired), errors.Join(ErrNativeControlRetired, errors.New("unknown commit")), agentoutbox.ErrUnavailable, errors.New("SECRET_UNKNOWN_COMMIT")} {
		s := &mixedRunnerStore{mixed: func(context.Context, actorref.PrincipalRef, string) (agentoutbox.Record, agentoutbox.Claim, error) {
			return agentoutbox.Record{}, agentoutbox.Claim{}, e
		}}
		result := Run(context.Background(), s, o)
		if result.Status != "STOPPED" || result.Counts.RetiredControls != 0 || result.ConfirmedReceipts != 0 || s.mixedCalls != 1 || s.consumes != 0 {
			t.Fatal("unknown result advanced mixed runner", result)
		}
	}
	for _, test := range []string{"nonempty-retirement", "wrong-family", "v2-replay", "foreign-owner", "unknown-consume"} {
		t.Run(test, func(t *testing.T) {
			r, c, p := mixedRunnerRecords(agentoutbox.MemoryHandler)
			e := error(nil)
			if test == "nonempty-retirement" {
				e = ErrNativeControlRetired
			}
			if test == "wrong-family" {
				c.HandlerVersion = agentoutbox.PreferenceHandler
			}
			if test == "v2-replay" {
				r, c, p = mixedRunnerRecords(agentoutbox.HandlerV1)
				c.HandlerVersion = agentoutbox.HandlerV2
			}
			if test == "foreign-owner" {
				c.Subject.ID = other
			}
			s := &mixedRunnerStore{mixed: func(context.Context, actorref.PrincipalRef, string) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, e
			}}
			s.consume = func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
				return p, errors.New("PRIVATE_COMMIT_UNKNOWN")
			}
			result := Run(context.Background(), s, o)
			if result.Status != "STOPPED" || result.ConfirmedReceipts != 0 || result.Counts.RetiredControls != 0 || s.mixedCalls != 1 || (test != "unknown-consume" && s.consumes != 0) {
				t.Fatal("unbound mixed claim/result accepted", result)
			}
		})
	}
	retire := &mixedRunnerStore{mixed: func(context.Context, actorref.PrincipalRef, string) (agentoutbox.Record, agentoutbox.Claim, error) {
		return agentoutbox.Record{}, agentoutbox.Claim{}, ErrNativeControlRetired
	}}
	o.Batch = 3
	if result := Run(context.Background(), retire, o); result.Reason != "BATCH_LIMIT_REACHED" || result.Counts.RetiredControls != 3 || result.ConfirmedReceipts != 0 || retire.mixedCalls != 3 {
		t.Fatal("retirement escaped bounded batch", result)
	}
}

func TestMixedMaintenanceUnitFixedHandlersRejectForeignNativeFamilies(t *testing.T) {
	families := []agentoutbox.HandlerVersion{agentoutbox.HandlerV1, agentoutbox.MemoryHandler, agentoutbox.PreferenceHandler, agentoutbox.TaskRequestHandler, agentoutbox.ParticipationHandler}
	for _, requested := range families {
		for _, source := range families {
			if requested == source {
				continue
			}
			t.Run(string(requested)+"/"+string(source), func(t *testing.T) {
				r, c, _ := mixedRunnerRecords(source)
				c.HandlerVersion = requested
				o := options()
				o.Handler = requested
				s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
					return r, c, nil
				}}
				if out := Run(context.Background(), s, o); out.Reason != "INVALID_CLAIM" || out.ConfirmedReceipts != 0 || s.claims != 1 || s.consumes != 0 {
					t.Fatal("fixed handler accepted a different native source family", out)
				}
			})
		}
	}
}
func records() (agentoutbox.Record, agentoutbox.Claim, agentoutbox.ConsumerRecord) {
	now := time.Now().UTC().Add(-time.Millisecond)
	p := options().Subject
	e := agentoutbox.Envelope{SchemaVersion: agentoutbox.SchemaVersion, EventType: agentoutbox.MomentCreated, Tenant: p, Subject: p, Actor: actorref.ActorRef{Type: actorref.Person, ID: owner}, AgentID: "89000000-0000-4000-8000-000000000002", Source: agentoutbox.SourceReference{Type: agentoutbox.MomentSource, ID: "89000000-0000-4000-8000-000000000003", Owner: p, Revision: 1, Status: agentoutbox.Draft, Fingerprint: strings.Repeat("a", 64)}, LogicalOperationID: "89000000-0000-4000-8000-000000000004", RootTraceID: "89000000-0000-4000-8000-000000000005", OccurredAt: now, ReceivedAt: now, ExpiresAt: now.Add(agentoutbox.MaxEventTTL)}
	e.EventID = agentoutbox.StableEventID(e)
	r, err := agentoutbox.NewPending(e, now)
	if err != nil {
		panic(err)
	}
	expiry := now.Add(agentoutbox.MaxLeaseDuration)
	r.State, r.Attempt, r.Fence, r.LeaseOwner, r.LeaseUntil = agentoutbox.Leased, 1, 1, worker, &expiry
	c := agentoutbox.Claim{EventID: e.EventID, Subject: p, AgentID: e.AgentID, WorkerID: worker, HandlerVersion: agentoutbox.HandlerV1, Fence: 1, LeaseUntil: expiry}
	receipt := agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.HandlerV1, State: agentoutbox.Unavailable, Fence: 1, Attempt: 1, Reason: agentoutbox.ReasonPurposeUnavailable, CreatedAt: now, UpdatedAt: now}
	return r, c, receipt
}

func TestAgentOutboxMaintenanceOptions(t *testing.T) {
	for name, mutate := range map[string]func(*Options){
		"foreignType": func(o *Options) { o.Subject.Type = actorref.Organization }, "zeroSubject": func(o *Options) { o.Subject.ID = "00000000-0000-0000-0000-000000000000" }, "caseAlias": func(o *Options) { o.Subject.ID = strings.ToUpper(other[:8]) + other[8:] },
		"worker": func(o *Options) { o.WorkerID = "" }, "handler": func(o *Options) { o.Handler = "candidate-approved" }, "zeroBatch": func(o *Options) { o.Batch = 0 }, "hugeBatch": func(o *Options) { o.Batch = MaxBatch + 1 }, "zeroTimeout": func(o *Options) { o.Timeout = 0 }, "hugeTimeout": func(o *Options) { o.Timeout = MaxTimeout + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			o := options()
			mutate(&o)
			if name == "caseAlias" {
				o.Subject.ID = "8900000A-0000-4000-8000-000000000007"
			}
			if ValidateOptions(o) {
				t.Fatal("invalid option accepted")
			}
		})
	}
	var nilPointer *testStore
	for _, s := range []Store{nil, nilPointer} {
		if r := Run(context.Background(), s, options()); r.Status != "STOPPED" || r.ConfirmedReceipts != 0 {
			t.Fatal(r)
		}
	}
	if r := Run(nil, &testStore{}, options()); r.Reason != "INVALID_CONFIGURATION" {
		t.Fatal(r)
	}
}

func TestAgentOutboxMaintenanceClaimErrorsStopWithoutConsume(t *testing.T) {
	for name, e := range map[string]error{"notFound": agentoutbox.ErrNotFound, "unavailableEmpty": agentoutbox.ErrUnavailable, "commitUnknown": errors.New("SECRET_DSN_PRIVATE_BODY"), "joinedNotFound": errors.Join(agentoutbox.ErrNotFound, errors.New("commit unknown")), "wrappedUnavailable": fmt.Errorf("private: %w", agentoutbox.ErrUnavailable)} {
		t.Run(name, func(t *testing.T) {
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return agentoutbox.Record{}, agentoutbox.Claim{}, e
			}}
			o := options()
			o.Batch = 100
			r := Run(context.Background(), s, o)
			if s.claims != 1 || s.consumes != 0 || r.ConfirmedReceipts != 0 {
				t.Fatal(r, s.claims, s.consumes)
			}
			if (r.Status == "FINISHED") != (e == agentoutbox.ErrNotFound) {
				t.Fatal("ambiguous error counted", r)
			}
			wire, _ := json.Marshal(r)
			if strings.Contains(string(wire), "SECRET") || strings.Contains(string(wire), "private") {
				t.Fatal("raw error leaked")
			}
		})
	}
}

func TestAgentOutboxMaintenanceClaimClosedBindings(t *testing.T) {
	for name, mutate := range map[string]func(*agentoutbox.Record, *agentoutbox.Claim){
		"event": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.EventID = other }, "agent": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.AgentID = other }, "subject": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.Subject.ID = other }, "worker": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.WorkerID = other }, "handler": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.HandlerVersion = agentoutbox.HandlerV2 }, "fence": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.Fence++ }, "lease": func(r *agentoutbox.Record, c *agentoutbox.Claim) { c.LeaseUntil = c.LeaseUntil.Add(time.Millisecond) }, "notLeased": func(r *agentoutbox.Record, c *agentoutbox.Claim) { r.State = agentoutbox.Pending }, "badRecord": func(r *agentoutbox.Record, c *agentoutbox.Claim) { r.Event.Source.Fingerprint = "invalid" }, "scope": func(r *agentoutbox.Record, c *agentoutbox.Claim) { r.Event.Subject.ID = other },
	} {
		t.Run(name, func(t *testing.T) {
			r, c, _ := records()
			mutate(&r, &c)
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}}
			out := Run(context.Background(), s, options())
			if out.Reason != "INVALID_CLAIM" || s.consumes != 0 || out.ConfirmedReceipts != 0 {
				t.Fatal(out)
			}
		})
	}
	r, c, _ := records()
	s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
		return r, c, agentoutbox.ErrNotFound
	}}
	if out := Run(context.Background(), s, options()); out.Status != "STOPPED" {
		t.Fatal("nonempty notFound accepted", out)
	}
}

func TestAgentOutboxMaintenanceReceiptNotAuthority(t *testing.T) {
	for name, mutate := range map[string]func(*agentoutbox.ConsumerRecord){
		"event": func(p *agentoutbox.ConsumerRecord) { p.EventID = other }, "subject": func(p *agentoutbox.ConsumerRecord) { p.Subject.ID = other }, "handler": func(p *agentoutbox.ConsumerRecord) { p.HandlerVersion = agentoutbox.HandlerV2 }, "fence": func(p *agentoutbox.ConsumerRecord) { p.Fence++ }, "attempt": func(p *agentoutbox.ConsumerRecord) { p.Attempt++ }, "leased": func(p *agentoutbox.ConsumerRecord) { p.State = agentoutbox.Leased; p.Reason = "" }, "fakeDone": func(p *agentoutbox.ConsumerRecord) { p.State = "COMPLETED" }, "reason": func(p *agentoutbox.ConsumerRecord) { p.Reason = agentoutbox.ReasonExpired }, "empty": func(p *agentoutbox.ConsumerRecord) { *p = agentoutbox.ConsumerRecord{} }, "clockRegressed": func(p *agentoutbox.ConsumerRecord) { p.UpdatedAt = p.UpdatedAt.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			r, c, p := records()
			mutate(&p)
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
				return p, agentoutbox.ErrUnavailable
			}}
			out := Run(context.Background(), s, options())
			if out.Status != "STOPPED" || out.ConfirmedReceipts != 0 || s.claims != 1 || s.consumes != 1 {
				t.Fatal(out)
			}
		})
	}
	for name, e := range map[string]error{"nil": nil, "unknownCommit": errors.New("SECRET_DSN"), "joined": errors.Join(agentoutbox.ErrUnavailable, errors.New("commit unknown")), "wrapped": fmt.Errorf("private: %w", agentoutbox.ErrUnavailable)} {
		t.Run(name, func(t *testing.T) {
			r, c, p := records()
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) { return p, e }}
			o := options()
			o.Batch = 100
			out := Run(context.Background(), s, o)
			if out.Status != "STOPPED" || out.ConfirmedReceipts != 0 || s.consumes != 1 || s.claims != 1 {
				t.Fatal(out)
			}
		})
	}
}

func TestAgentOutboxMaintenanceTerminalCountersAndBatch(t *testing.T) {
	for _, state := range []agentoutbox.State{agentoutbox.Unavailable, agentoutbox.Invalidated, agentoutbox.Expired, agentoutbox.DeadLetter} {
		t.Run(string(state), func(t *testing.T) {
			r, c, p := records()
			p.State = state
			switch state {
			case agentoutbox.Invalidated:
				p.Reason = agentoutbox.ReasonSourceInvalidated
			case agentoutbox.Expired:
				p.Reason = agentoutbox.ReasonExpired
			case agentoutbox.DeadLetter:
				p.Reason = agentoutbox.ReasonAttemptsExhausted
			}
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
				return p, agentoutbox.ErrUnavailable
			}}
			o := options()
			o.Batch = 100
			out := Run(context.Background(), s, o)
			if out.Status != "FINISHED" || out.Reason != "BATCH_LIMIT_REACHED" || out.ConfirmedReceipts != 100 || s.claims != 100 || s.consumes != 100 || out.Counts.Unavailable+out.Counts.Expired+out.Counts.Invalidated+out.Counts.DeadLetter != 100 || out.BusinessExecution != "UNAVAILABLE" {
				t.Fatal(out)
			}
		})
	}
}

func TestAgentOutboxMaintenanceContextBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &testStore{}
	if r := Run(ctx, s, options()); r.Reason != "CANCELLED" || s.claims != 0 {
		t.Fatal(r)
	}
	s = &testStore{claim: func(ctx context.Context, _ actorref.PrincipalRef, _ string, _ agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
		<-ctx.Done()
		return agentoutbox.Record{}, agentoutbox.Claim{}, ctx.Err()
	}}
	o := options()
	o.Timeout = 5 * time.Millisecond
	if r := Run(context.Background(), s, o); r.Reason != "DEADLINE_EXCEEDED" || s.claims != 1 || s.consumes != 0 {
		t.Fatal(r)
	}
	r, c, _ := records()
	ctx, cancel = context.WithCancel(context.Background())
	s = &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
		cancel()
		return r, c, nil
	}}
	if out := Run(ctx, s, options()); out.Stage != "BEFORE_CONSUME" || s.consumes != 0 {
		t.Fatal(out)
	}
}

func TestAgentOutboxMaintenanceReceiptCheckpointBounds(t *testing.T) {
	for _, boundary := range []string{"leaseEqual", "leaseFuture", "expiryEqual", "expiryFuture", "priorCreation"} {
		t.Run(boundary, func(t *testing.T) {
			r, c, p := records()
			switch boundary {
			case "leaseEqual":
				p.UpdatedAt = c.LeaseUntil
			case "leaseFuture":
				p.UpdatedAt = c.LeaseUntil.Add(time.Nanosecond)
			case "expiryEqual":
				p.UpdatedAt = r.Event.ExpiresAt
			case "expiryFuture":
				p.UpdatedAt = r.Event.ExpiresAt.Add(time.Nanosecond)
			case "priorCreation":
				p.CreatedAt = p.CreatedAt.Add(-time.Hour)
			}
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
				return p, agentoutbox.ErrUnavailable
			}}
			out := Run(context.Background(), s, options())
			if (out.ConfirmedReceipts == 1) != (boundary == "priorCreation") {
				t.Fatal(out)
			}
		})
	}
}

func TestAgentOutboxMaintenanceCommittedReceiptBeforeCancellation(t *testing.T) {
	r, c, p := records()
	ctx, cancel := context.WithCancel(context.Background())
	s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
		return r, c, nil
	}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
		cancel()
		return p, agentoutbox.ErrUnavailable
	}}
	o := options()
	o.Batch = 2
	out := Run(ctx, s, o)
	if out.ConfirmedReceipts != 1 || out.Reason != "CANCELLED" || s.claims != 1 || s.consumes != 1 {
		t.Fatal(out)
	}
}

func TestAgentOutboxMaintenanceDispatchUnitExactEmptyBusyStops(t *testing.T) {
	s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrDispatchBusy
	}}
	o := options()
	o.Batch = 100
	r := Run(context.Background(), s, o)
	if r.Status != "FINISHED" || r.Stage != "CLAIM" || r.Reason != "DISPATCH_CAPACITY_BUSY" || s.claims != 1 || s.consumes != 0 || r.ConfirmedReceipts != 0 || r.BusinessExecution != "UNAVAILABLE" {
		t.Fatal("known capacity must stop once without fake empty or receipt", r, s.claims, s.consumes)
	}
}

func TestAgentOutboxMaintenanceDispatchUnitAmbiguousBusyNotAuthority(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		err                                   error
		nonemptyRecord, nonemptyClaim, cancel bool
	}{
		{"wrapped", fmt.Errorf("private: %w", agentoutbox.ErrDispatchBusy), false, false, false},
		{"joined", errors.Join(agentoutbox.ErrDispatchBusy, errors.New("PRIVATE_COMMIT_CANARY")), false, false, false},
		{"nonemptyRecord", agentoutbox.ErrDispatchBusy, true, false, false},
		{"nonemptyClaim", agentoutbox.ErrDispatchBusy, false, true, false},
		{"cancelledExactBusy", agentoutbox.ErrDispatchBusy, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				var r agentoutbox.Record
				var c agentoutbox.Claim
				if tc.nonemptyRecord {
					r, _, _ = records()
				}
				if tc.nonemptyClaim {
					_, c, _ = records()
				}
				if tc.cancel {
					cancel()
				}
				return r, c, tc.err
			}}
			o := options()
			o.Batch = 100
			r := Run(ctx, s, o)
			want := "CLAIM_UNCONFIRMED"
			if tc.cancel {
				want = "CANCELLED"
			}
			if r.Status != "STOPPED" || r.Stage != "CLAIM" || r.Reason != want || s.claims != 1 || s.consumes != 0 || r.ConfirmedReceipts != 0 {
				t.Fatal(r, s.claims, s.consumes)
			}
			wire, _ := json.Marshal(r)
			if strings.Contains(string(wire), "private") || strings.Contains(string(wire), "CANARY") {
				t.Fatal("raw ambiguity escaped", string(wire))
			}
		})
	}
}
