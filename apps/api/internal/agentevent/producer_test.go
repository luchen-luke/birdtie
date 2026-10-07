package agentevent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

type sourceResolverStub struct {
	facts  ResolvedSource
	err    error
	calls  int
	access Access
	kind   Type
	id     string
}

func (s *sourceResolverStub) ResolveEventSource(_ context.Context, a Access, k Type, id string) (ResolvedSource, error) {
	s.calls++
	s.access = a
	s.kind = k
	s.id = id
	return s.facts, s.err
}

func testResolved(kind Type) ResolvedSource {
	e := validEnvelope(kind)
	status := "draft"
	if kind == UserQuery {
		status = "ACTIVE"
	}
	return ResolvedSource{Actor: e.Actor, Agent: agentcognitive.AgentReference{AgentID: e.AgentID, Principal: e.Subject, Role: agentruntime.PersonalAgent},
		Source: e.Source, OccurredAt: e.OccurredAt, ResolvedAt: e.ReceivedAt, NativeStatus: status, MetadataBound: true, TextPresent: true}
}

func testAccess() Access            { return Access{SessionDigest: [32]byte{1, 3, 5}} }
func testRequest(kind Type) Request { return Request{kind, sourceID, operationID, traceID, nil} }

func TestEventProducerNativeResolverPositiveAndDuplicates(t *testing.T) {
	for _, kind := range []Type{MomentCreated, UserQuery} {
		t.Run(string(kind), func(t *testing.T) {
			s := &sourceResolverStub{facts: testResolved(kind)}
			p, err := NewProducer(s)
			if err != nil {
				t.Fatal(err)
			}
			first, err := p.Produce(context.Background(), testAccess(), testRequest(kind))
			if err != nil {
				t.Fatal(err)
			}
			if first.ProcessingStatus != Unavailable || first.Actor.ID != ownID || first.Subject.ID != ownID || first.AgentID != agentID || s.access != testAccess() {
				t.Fatal("identity not derived or ingress granted analysis")
			}
			for i := 0; i < 100; i++ {
				e, err := p.Produce(context.Background(), testAccess(), testRequest(kind))
				if err != nil || e.EventID != first.EventID {
					t.Fatal("retry changed identity", err)
				}
			}
			if err := p.Revalidate(context.Background(), testAccess(), first); err != nil {
				t.Fatal(err)
			}
			r := testRequest(kind)
			r.LogicalOperationID = otherID
			second, err := p.Produce(context.Background(), testAccess(), r)
			if err != nil || second.EventID == first.EventID {
				t.Fatal("new intentional operation deduplicated", err)
			}
			// Source ownership remains a resolver fact, not editable input identity.
			if s.kind != kind || s.id != sourceID || s.calls != 103 {
				t.Fatal("unexpected resolver dispatch", s.calls)
			}
		})
	}
}

func TestEventProducerRejectsInvalidRequestsBeforeResolver(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"unknown_type", func(r *Request) { r.EventType = "UnknownFutureType" }},
		{"url_source", func(r *Request) { r.SourceID = "https://example.invalid/source" }},
		{"zero_source", func(r *Request) { r.SourceID = "00000000-0000-0000-0000-000000000000" }},
		{"blank_operation", func(r *Request) { r.LogicalOperationID = "" }},
		{"huge_operation", func(r *Request) { r.LogicalOperationID = strings.Repeat("a", 17000) }},
		{"raw_trace", func(r *Request) { r.RootTraceID = "private-body" }},
		{"causation_url", func(r *Request) { v := "file:///private"; r.CausationID = &v }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &sourceResolverStub{facts: testResolved(MomentCreated)}
			p, _ := NewProducer(s)
			r := testRequest(MomentCreated)
			tc.change(&r)
			e, err := p.Produce(context.Background(), testAccess(), r)
			if !errors.Is(err, ErrInvalid) || e != (Envelope{}) || s.calls != 0 {
				t.Fatal("invalid request reached source", err)
			}
		})
	}
	for _, tc := range []struct {
		name      string
		access    Access
		cancelled bool
	}{
		{"anonymous", Access{}, false}, {"cancelled", testAccess(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &sourceResolverStub{facts: testResolved(MomentCreated)}
			p, _ := NewProducer(s)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			e, err := p.Produce(ctx, tc.access, testRequest(MomentCreated))
			if err == nil || e != (Envelope{}) || s.calls != 0 {
				t.Fatal("invalid session/context reached source")
			}
		})
	}
}

func TestEventProducerRejectsWrongOrStaleResolvedFacts(t *testing.T) {
	cases := []struct {
		name   string
		kind   Type
		change func(*ResolvedSource)
	}{
		{"wrong_actor", MomentCreated, func(f *ResolvedSource) { f.Actor.ID = otherID }},
		{"actor_org", MomentCreated, func(f *ResolvedSource) { f.Actor.Type = actorref.Organization }},
		{"owner_other", MomentCreated, func(f *ResolvedSource) { f.Source.Owner.ID = otherID }},
		{"agent_owner_other", MomentCreated, func(f *ResolvedSource) { f.Agent.Principal.ID = otherID }},
		{"agent_org", MomentCreated, func(f *ResolvedSource) {
			f.Agent.Principal.Type = actorref.Organization
			f.Agent.Role = agentruntime.OrganizationAgent
		}},
		{"agent_business", MomentCreated, func(f *ResolvedSource) {
			f.Agent.Principal.Type = actorref.Business
			f.Agent.Role = agentruntime.BusinessAgent
		}},
		{"agent_community", MomentCreated, func(f *ResolvedSource) { f.Agent.Principal.Type = actorref.Community }},
		{"metadata_missing", MomentCreated, func(f *ResolvedSource) { f.MetadataBound = false }},
		{"no_text", MomentCreated, func(f *ResolvedSource) { f.TextPresent = false }},
		{"withdrawn", MomentCreated, func(f *ResolvedSource) { f.NativeStatus = "withdrawn" }},
		{"published", MomentCreated, func(f *ResolvedSource) { f.NativeStatus = "published" }},
		{"source_other", MomentCreated, func(f *ResolvedSource) { f.Source.ID = otherID }},
		{"source_wrong_type", MomentCreated, func(f *ResolvedSource) { f.Source.Type = QuerySource }},
		{"revision_zero", MomentCreated, func(f *ResolvedSource) { f.Source.Version.Revision = 0 }},
		{"query_failed", UserQuery, func(f *ResolvedSource) { f.NativeStatus = "FAILED" }},
		{"query_fake_counter", UserQuery, func(f *ResolvedSource) { f.Source.Version = SourceVersion{Kind: RevisionVersion, Revision: 1} }},
		{"unknown_now", MomentCreated, func(f *ResolvedSource) { f.ResolvedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &sourceResolverStub{facts: testResolved(tc.kind)}
			tc.change(&s.facts)
			p, _ := NewProducer(s)
			e, err := p.Produce(context.Background(), testAccess(), testRequest(tc.kind))
			if err == nil || e != (Envelope{}) {
				t.Fatal("wrong facts emitted event")
			}
		})
	}
	s := &sourceResolverStub{facts: testResolved(MomentCreated)}
	s.facts.OccurredAt = s.facts.ResolvedAt.Add(-MaxEventTTL)
	p, _ := NewProducer(s)
	if e, err := p.Produce(context.Background(), testAccess(), testRequest(MomentCreated)); !errors.Is(err, ErrExpired) || e != (Envelope{}) {
		t.Fatal("old source was given a fresh lifetime", err)
	}
}

func TestEventProducerReplayCurrentSourceAndAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   Type
		change func(*sourceResolverStub)
	}{
		{"revoked", MomentCreated, func(s *sourceResolverStub) { s.err = ErrDenied }},
		{"stopped_agent", MomentCreated, func(s *sourceResolverStub) { s.err = ErrDenied }},
		{"deleted_source", MomentCreated, func(s *sourceResolverStub) { s.err = ErrDenied }},
		{"moment_edited", MomentCreated, func(s *sourceResolverStub) { s.facts.Source.Version.Revision++ }},
		{"query_edited", UserQuery, func(s *sourceResolverStub) { s.facts.Source.Version.Token = strings.Repeat("a", 64) }},
		{"agent_replaced", MomentCreated, func(s *sourceResolverStub) { s.facts.Agent.AgentID = otherID }},
		{"source_expired", MomentCreated, func(s *sourceResolverStub) { s.facts.ResolvedAt = s.facts.OccurredAt.Add(MaxEventTTL) }},
		{"source_time_changed", UserQuery, func(s *sourceResolverStub) { s.facts.OccurredAt = s.facts.OccurredAt.Add(time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &sourceResolverStub{facts: testResolved(tc.kind)}
			p, _ := NewProducer(s)
			e, err := p.Produce(context.Background(), testAccess(), testRequest(tc.kind))
			if err != nil {
				t.Fatal(err)
			}
			tc.change(s)
			if err := p.Revalidate(context.Background(), testAccess(), e); err == nil {
				t.Fatal("stale event replay accepted")
			}
		})
	}
	s := &sourceResolverStub{facts: testResolved(MomentCreated)}
	p, _ := NewProducer(s)
	e := validEnvelope(MomentCreated)
	e.ProcessingStatus = "AVAILABLE"
	if err := p.Revalidate(context.Background(), testAccess(), e); !errors.Is(err, ErrInvalid) || s.calls != 0 {
		t.Fatal("wire status became authority")
	}
}

func TestEventServerAuthorityCannotBeJSONOrLeakErrors(t *testing.T) {
	access := testAccess()
	facts := testResolved(MomentCreated)
	for _, value := range []any{access, facts} {
		if _, err := json.Marshal(value); !errors.Is(err, ErrAuthorityJSON) {
			t.Fatal("authority serialized")
		}
	}
	if err := json.Unmarshal([]byte(`{"SessionDigest":[1],"Verified":true}`), &access); !errors.Is(err, ErrAuthorityJSON) || access != (Access{}) {
		t.Fatal("wire access accepted")
	}
	if err := json.Unmarshal([]byte(`{"MetadataBound":true}`), &facts); !errors.Is(err, ErrAuthorityJSON) || facts != (ResolvedSource{}) {
		t.Fatal("wire source proof accepted")
	}
	if _, err := NewProducer(nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil resolver accepted")
	}
	var typedNil *sourceResolverStub
	if _, err := NewProducer(typedNil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("typed nil accepted")
	}
	var p *Producer
	if e, err := p.Produce(context.Background(), testAccess(), testRequest(MomentCreated)); !errors.Is(err, ErrUnavailable) || e != (Envelope{}) {
		t.Fatal("nil producer unsafe")
	}
	s := &sourceResolverStub{err: errors.New("secret token-sha private body")}
	p, _ = NewProducer(s)
	if e, err := p.Produce(context.Background(), testAccess(), testRequest(MomentCreated)); !errors.Is(err, ErrUnavailable) || e != (Envelope{}) || strings.Contains(err.Error(), "secret") {
		t.Fatal("resolver detail leaked")
	}
}
