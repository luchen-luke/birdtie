package agentattention

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const ownerID = "73000000-0000-4000-8000-000000000001"
const agentID = "73000000-0000-4000-8000-000000000002"
const sourceID = "73000000-0000-4000-8000-000000000003"
const operationID = "73000000-0000-4000-8000-000000000004"
const traceID = "73000000-0000-4000-8000-000000000005"
const otherID = "73000000-0000-4000-8000-000000000006"

func nowFixture() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
func agentFixture() agentcognitive.AgentReference {
	return agentcognitive.AgentReference{AgentID: agentID,
		Principal: actorref.PrincipalRef{Type: actorref.Person, ID: ownerID}, Role: agentruntime.PersonalAgent}
}
func specFixture(now time.Time) Specification {
	return Specification{DefaultRoute: Normal, ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
}
func eventFixture(now time.Time, kind agentevent.Type) agentevent.Envelope {
	d, _ := agentevent.Lookup(kind)
	owner := agentFixture().Principal
	version := agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 2}
	if kind == agentevent.UserQuery {
		version, _ = agentevent.QueryVersion(now.Add(-time.Minute), []byte(`{"query":"synthetic"}`))
	}
	e := agentevent.Envelope{SchemaVersion: agentevent.SchemaVersion, EventType: kind,
		Tenant: owner, Subject: owner, Actor: actorref.ActorRef{Type: actorref.Person, ID: ownerID},
		AgentID: agentID, LogicalOperationID: operationID, OccurredAt: now.Add(-time.Minute),
		ReceivedAt: now, ExpiresAt: now.Add(-time.Minute).Add(agentevent.MaxEventTTL),
		Source:  agentevent.SourceReference{Type: d.Source, ID: sourceID, Owner: owner, Version: version},
		Purpose: d.Purpose, RootTraceID: traceID, PayloadRef: agentevent.PayloadReference{Type: d.Source, ID: sourceID},
		ProcessingStatus: agentevent.Unavailable}
	e.EventID = agentevent.StableEventID(e)
	return e
}
func boundaryFixture(p Policy, e agentevent.Envelope, now time.Time) OfflineBoundary {
	return OfflineBoundary{State: BoundaryAllowed, CurrentSource: e.Source, PolicyRevision: p.Revision(),
		ConsentRevision: 7, CurrentConsentRevision: 7, CheckedAt: now, ExpiresAt: now.Add(time.Minute)}
}
func policyFixture(t *testing.T, spec Specification) (*Store, Policy) {
	t.Helper()
	s, err := NewStore(agentFixture(), spec)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}
func assertBlock(t *testing.T, p Policy, e agentevent.Envelope, b OfflineBoundary, now time.Time, expected error) {
	t.Helper()
	decision, err := EvaluateOffline(p, e, b, now)
	if !errors.Is(err, expected) || decision.Route != Block || decision.Mode != "OFFLINE_CONTRACT" {
		t.Fatalf("route=%s err=%v expected=%v", decision.Route, err, expected)
	}
}

func TestFiveRoutesForBothRealEventSchemas(t *testing.T) {
	now := nowFixture()
	for _, kind := range []agentevent.Type{agentevent.MomentCreated, agentevent.UserQuery} {
		for _, route := range []Route{Immediate, Normal, Digest, Silent, Block} {
			t.Run(string(kind)+"/"+string(route), func(t *testing.T) {
				spec := specFixture(now)
				spec.Rules = []Rule{{kind, route}}
				_, p := policyFixture(t, spec)
				e := eventFixture(now, kind)
				d, err := EvaluateOffline(p, e, boundaryFixture(p, e, now), now)
				if err != nil || d.Route != route || d.Reason != "event_rule" || d.PolicyRevision != 1 ||
					d.EventID != e.EventID || d.Mode != "OFFLINE_CONTRACT" {
					t.Fatal(d, err)
				}
			})
		}
	}
}

func TestDeterministicPrecedenceAndHalfOpenPause(t *testing.T) {
	now := nowFixture()
	e := eventFixture(now, agentevent.UserQuery)
	spec := specFixture(now)
	spec.Rules = []Rule{{agentevent.UserQuery, Immediate}, {agentevent.MomentCreated, Block}}
	spec.PauseUntil = now.Add(time.Minute)
	_, p := policyFixture(t, spec)
	t.Run("pause_beats_immediate", func(t *testing.T) {
		d, err := EvaluateOffline(p, e, boundaryFixture(p, e, now), now)
		if err != nil || d.Route != Silent || d.Reason != "attention_paused" {
			t.Fatal(d, err)
		}
	})
	t.Run("block_beats_pause", func(t *testing.T) {
		m := eventFixture(now, agentevent.MomentCreated)
		d, err := EvaluateOffline(p, m, boundaryFixture(p, m, now), now)
		if err != nil || d.Route != Block || d.Reason != "event_rule" {
			t.Fatal(d, err)
		}
	})
	t.Run("pause_expiry_exact", func(t *testing.T) {
		at := spec.PauseUntil
		d, err := EvaluateOffline(p, e, boundaryFixture(p, e, at), at)
		if err != nil || d.Route != Immediate {
			t.Fatal(d, err)
		}
	})
	t.Run("rule_order_no_effect", func(t *testing.T) {
		spec.PauseUntil = time.Time{}
		_, p1 := policyFixture(t, spec)
		spec.Rules[0], spec.Rules[1] = spec.Rules[1], spec.Rules[0]
		_, p2 := policyFixture(t, spec)
		d1, err1 := EvaluateOffline(p1, e, boundaryFixture(p1, e, now), now)
		d2, err2 := EvaluateOffline(p2, e, boundaryFixture(p2, e, now), now)
		if err1 != nil || err2 != nil || d1 != d2 {
			t.Fatal(d1, d2, err1, err2)
		}
	})
	t.Run("security_beats_block_or_silent", func(t *testing.T) {
		b := boundaryFixture(p, e, now)
		b.Revoked = true
		assertBlock(t, p, e, b, now, ErrDenied)
	})
	t.Run("default_and_exact_override", func(t *testing.T) {
		spec.DefaultRoute = Block
		spec.PauseUntil = time.Time{}
		spec.Rules = []Rule{{agentevent.UserQuery, Normal}}
		_, p := policyFixture(t, spec)
		d, err := EvaluateOffline(p, e, boundaryFixture(p, e, now), now)
		if err != nil || d.Route != Normal {
			t.Fatal(d, err)
		}
		m := eventFixture(now, agentevent.MomentCreated)
		d, err = EvaluateOffline(p, m, boundaryFixture(p, m, now), now)
		if err != nil || d.Route != Block || d.Reason != "default_route" {
			t.Fatal(d, err)
		}
	})
}

func TestSourcePermissionAndLifetimeFailClosed(t *testing.T) {
	now := nowFixture()
	_, p := policyFixture(t, specFixture(now))
	e := eventFixture(now, agentevent.MomentCreated)
	cases := []struct {
		name   string
		mutate func(*OfflineBoundary)
		want   error
	}{
		{"unknown", func(b *OfflineBoundary) { b.State = "CLIENT_VERIFIED" }, ErrDenied},
		{"zero_state", func(b *OfflineBoundary) { b.State = "" }, ErrDenied},
		{"denied", func(b *OfflineBoundary) { b.State = BoundaryDenied }, ErrDenied},
		{"unavailable", func(b *OfflineBoundary) { b.State = BoundaryUnavailable }, ErrUnavailable},
		{"revoked", func(b *OfflineBoundary) { b.Revoked = true }, ErrDenied},
		{"deleted", func(b *OfflineBoundary) { b.Deleted = true }, ErrDenied},
		{"source_revision", func(b *OfflineBoundary) { b.CurrentSource.Version.Revision++ }, ErrDenied},
		{"source_owner", func(b *OfflineBoundary) { b.CurrentSource.Owner.ID = otherID }, ErrDenied},
		{"source_type", func(b *OfflineBoundary) { b.CurrentSource.Type = agentevent.QuerySource }, ErrDenied},
		{"source_id", func(b *OfflineBoundary) { b.CurrentSource.ID = otherID }, ErrDenied},
		{"policy_revision", func(b *OfflineBoundary) { b.PolicyRevision++ }, ErrDenied},
		{"consent_missing", func(b *OfflineBoundary) { b.ConsentRevision = 0 }, ErrDenied},
		{"consent_changed", func(b *OfflineBoundary) { b.CurrentConsentRevision++ }, ErrDenied},
		{"consent_current_missing", func(b *OfflineBoundary) { b.CurrentConsentRevision = 0 }, ErrDenied},
		{"expiry_exact", func(b *OfflineBoundary) { b.ExpiresAt = now }, ErrExpired},
		{"expiry_zero", func(b *OfflineBoundary) { b.ExpiresAt = time.Time{} }, ErrExpired},
		{"future_check", func(b *OfflineBoundary) { b.CheckedAt = now.Add(time.Nanosecond) }, ErrExpired},
		{"stale_check", func(b *OfflineBoundary) { b.CheckedAt = now.Add(-time.Nanosecond) }, ErrExpired},
		{"zero_check", func(b *OfflineBoundary) { b.CheckedAt = time.Time{} }, ErrExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := boundaryFixture(p, e, now)
			tc.mutate(&b)
			assertBlock(t, p, e, b, now, tc.want)
		})
	}
	t.Run("query_digest_changed", func(t *testing.T) {
		q := eventFixture(now, agentevent.UserQuery)
		b := boundaryFixture(p, q, now)
		b.CurrentSource.Version, _ = agentevent.QueryVersion(now, []byte(`{"query":"changed"}`))
		assertBlock(t, p, q, b, now, ErrDenied)
	})
	t.Run("expired_event_uses_actual_now", func(t *testing.T) {
		at := e.ExpiresAt
		assertBlock(t, p, e, boundaryFixture(p, e, at), at, ErrExpired)
	})
	t.Run("before_policy", func(t *testing.T) {
		spec := specFixture(now)
		spec.ValidFrom = now.Add(time.Nanosecond)
		_, future := policyFixture(t, spec)
		assertBlock(t, future, e, boundaryFixture(future, e, now), now, ErrExpired)
	})
	t.Run("policy_expiry_exact", func(t *testing.T) {
		spec := specFixture(now)
		spec.ExpiresAt = now
		_, expired := policyFixture(t, spec)
		assertBlock(t, expired, e, boundaryFixture(expired, e, now), now, ErrExpired)
	})
	t.Run("revoked_policy", func(t *testing.T) {
		s, old := policyFixture(t, specFixture(now))
		if err := s.Revoke(old.Revision()); err != nil {
			t.Fatal(err)
		}
		current, _ := s.Snapshot()
		assertBlock(t, current, e, boundaryFixture(current, e, now), now, ErrDenied)
	})
}

func TestEnvelopeAndSubjectCannotBypassGate(t *testing.T) {
	now := nowFixture()
	_, p := policyFixture(t, specFixture(now))
	cases := []struct {
		name   string
		mutate func(*agentevent.Envelope)
	}{
		{"unknown_event", func(e *agentevent.Envelope) { e.EventType = "MESSAGE" }},
		{"claimed_actor", func(e *agentevent.Envelope) { e.Actor.ID = otherID }},
		{"claimed_tenant", func(e *agentevent.Envelope) { e.Tenant.Type = actorref.Organization }},
		{"claimed_permission", func(e *agentevent.Envelope) { e.ProcessingStatus = "AVAILABLE" }},
		{"purpose_override", func(e *agentevent.Envelope) { e.Purpose = "notification" }},
		{"future_received", func(e *agentevent.Envelope) { e.ReceivedAt = now.Add(time.Nanosecond) }},
		{"wrong_source_version_union", func(e *agentevent.Envelope) { e.Source.Version.Token = "forged" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := eventFixture(now, agentevent.MomentCreated)
			tc.mutate(&e)
			e.EventID = agentevent.StableEventID(e)
			assertBlock(t, p, e, boundaryFixture(p, e, now), now, ErrInvalid)
		})
	}
	t.Run("other_valid_agent", func(t *testing.T) {
		e := eventFixture(now, agentevent.MomentCreated)
		e.AgentID = otherID
		e.EventID = agentevent.StableEventID(e)
		assertBlock(t, p, e, boundaryFixture(p, e, now), now, ErrDenied)
	})
	t.Run("other_valid_person", func(t *testing.T) {
		e := eventFixture(now, agentevent.MomentCreated)
		e.Subject.ID = otherID
		e.Tenant.ID = otherID
		e.Actor.ID = otherID
		e.Source.Owner.ID = otherID
		e.EventID = agentevent.StableEventID(e)
		assertBlock(t, p, e, boundaryFixture(p, e, now), now, ErrDenied)
	})
	t.Run("zero_policy", func(t *testing.T) {
		e := eventFixture(now, agentevent.MomentCreated)
		assertBlock(t, Policy{}, e, OfflineBoundary{}, now, ErrInvalid)
	})
	t.Run("zero_clock", func(t *testing.T) {
		e := eventFixture(now, agentevent.MomentCreated)
		assertBlock(t, p, e, boundaryFixture(p, e, now), time.Time{}, ErrInvalid)
	})
}

func TestPolicyInvalidShapesAndNoWireAuthority(t *testing.T) {
	now := nowFixture()
	cases := []struct {
		name   string
		mutate func(*Specification)
	}{
		{"unknown_default", func(s *Specification) { s.DefaultRoute = "PUSH" }},
		{"missing_default", func(s *Specification) { s.DefaultRoute = "" }},
		{"unknown_rule", func(s *Specification) { s.Rules = []Rule{{"MESSAGE", Normal}} }},
		{"unknown_route", func(s *Specification) { s.Rules = []Rule{{agentevent.UserQuery, "PUSH"}} }},
		{"duplicate_rules", func(s *Specification) {
			s.Rules = []Rule{{agentevent.UserQuery, Block}, {agentevent.UserQuery, Immediate}}
		}},
		{"too_many", func(s *Specification) {
			s.Rules = []Rule{{agentevent.UserQuery, Normal}, {agentevent.MomentCreated, Normal}, {agentevent.UserQuery, Digest}}
		}},
		{"zero_from", func(s *Specification) { s.ValidFrom = time.Time{} }},
		{"zero_expiry", func(s *Specification) { s.ExpiresAt = time.Time{} }},
		{"backward_expiry", func(s *Specification) { s.ExpiresAt = s.ValidFrom }},
		{"invalid_time_year", func(s *Specification) { s.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"utc_normalization_out_of_range", func(s *Specification) {
			s.ExpiresAt = time.Date(9999, 12, 31, 23, 59, 59, 0, time.FixedZone("west", -14*3600))
		}},
		{"pause_before", func(s *Specification) { s.PauseUntil = s.ValidFrom.Add(-time.Nanosecond) }},
		{"pause_after", func(s *Specification) { s.PauseUntil = s.ExpiresAt.Add(time.Nanosecond) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := specFixture(now)
			tc.mutate(&spec)
			if _, err := NewStore(agentFixture(), spec); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	t.Run("wrong_role", func(t *testing.T) {
		a := agentFixture()
		a.Role = agentruntime.OrganizationAgent
		if _, err := NewStore(a, specFixture(now)); err != ErrInvalid {
			t.Fatal(err)
		}
	})
	t.Run("business_not_enabled", func(t *testing.T) {
		a := agentFixture()
		a.Principal.Type = actorref.Business
		a.Role = agentruntime.BusinessAgent
		if _, err := NewStore(a, specFixture(now)); err != ErrInvalid {
			t.Fatal(err)
		}
	})
	t.Run("zero_agent_id", func(t *testing.T) {
		a := agentFixture()
		a.AgentID = "00000000-0000-0000-0000-000000000000"
		if _, err := NewStore(a, specFixture(now)); err != ErrInvalid {
			t.Fatal(err)
		}
	})
	_, p := policyFixture(t, specFixture(now))
	for name, value := range map[string]any{"policy": p, "spec": specFixture(now), "boundary": OfflineBoundary{State: BoundaryAllowed}} {
		t.Run(name+"_cannot_encode_authority", func(t *testing.T) {
			if _, err := json.Marshal(value); !errors.Is(err, ErrServerOnly) {
				t.Fatal(err)
			}
		})
	}
	t.Run("client_verified_cannot_decode", func(t *testing.T) {
		var b OfflineBoundary
		if err := json.Unmarshal([]byte(`{"Verified":true}`), &b); !errors.Is(err, ErrServerOnly) || b.State != "" {
			t.Fatal(err, b)
		}
	})
	t.Run("wire_policy_cannot_decode", func(t *testing.T) {
		copy := p
		if err := json.Unmarshal([]byte(`{"revision":1,"defaultRoute":"IMMEDIATE"}`), &copy); !errors.Is(err, ErrServerOnly) || copy.Revision() != 0 {
			t.Fatal(err)
		}
	})
}

func TestPolicyCASIsolationAndConcurrentWriters(t *testing.T) {
	now := nowFixture()
	spec := specFixture(now)
	spec.Rules = []Rule{{agentevent.UserQuery, Immediate}}
	s, p := policyFixture(t, spec)
	spec.Rules[0].Route = Block
	if p.Specification().Rules[0].Route != Immediate {
		t.Fatal("input aliases policy")
	}
	copy := p.Specification()
	copy.Rules[0].Route = Block
	if p.Specification().Rules[0].Route != Immediate {
		t.Fatal("output aliases policy")
	}
	const workers = 32
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Replace(1, specFixture(now)) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if err != ErrConflict {
			t.Fatal(err)
		}
	}
	current, _ := s.Snapshot()
	if success != 1 || current.Revision() != 2 || s.current(p) {
		t.Fatal(success, current.Revision())
	}
	invalid := specFixture(now)
	invalid.DefaultRoute = "UNKNOWN"
	if err := s.Replace(2, invalid); err != ErrInvalid {
		t.Fatal(err)
	}
	if after, _ := s.Snapshot(); after.Revision() != 2 {
		t.Fatal("invalid update mutated revision")
	}
	if err := s.Replace(1, specFixture(now)); err != ErrConflict {
		t.Fatal(err)
	}
	if err := s.Revoke(2); err != nil {
		t.Fatal(err)
	}
	revoked, _ := s.Snapshot()
	if revoked.Revision() != 3 || !revoked.Revoked() || s.current(current) {
		t.Fatal("revocation retained ticket")
	}
	if err := s.Revoke(2); err != ErrConflict {
		t.Fatal(err)
	}
	if err := s.Replace(3, specFixture(now)); err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.Snapshot()
	if fresh.Revision() != 4 || fresh.Revoked() || s.current(p) || s.current(current) {
		t.Fatal("old policy resurrected")
	}
	t.Run("overflow_is_atomic", func(t *testing.T) {
		s, _ := policyFixture(t, specFixture(now))
		s.policy.revision = ^uint64(0)
		if s.Replace(^uint64(0), specFixture(now)) != ErrConflict || s.Revoke(^uint64(0)) != ErrConflict {
			t.Fatal("overflow accepted")
		}
		p, _ := s.Snapshot()
		if p.Revision() != ^uint64(0) {
			t.Fatal("overflow mutated state")
		}
	})
}
