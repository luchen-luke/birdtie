package agentevent

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

// Access is process-local session evidence, not a wire permission or a claimed
// actor/workspace. The current native resolver derives all identity fields.
type Access struct{ SessionDigest [32]byte }

func (Access) MarshalJSON() ([]byte, error)  { return nil, ErrAuthorityJSON }
func (v *Access) UnmarshalJSON([]byte) error { *v = Access{}; return ErrAuthorityJSON }

type Request struct {
	EventType          Type
	SourceID           string
	LogicalOperationID string
	RootTraceID        string
	CausationID        *string
}

// ResolvedSource is a server-only result of a current native session/source
// statement, not authorizable JSON. It contains no source text or model consent.
type ResolvedSource struct {
	Actor         actorref.ActorRef
	Agent         agentcognitive.AgentReference
	Source        SourceReference
	OccurredAt    time.Time
	ResolvedAt    time.Time
	NativeStatus  string
	MetadataBound bool
	TextPresent   bool
}

func (ResolvedSource) MarshalJSON() ([]byte, error)  { return nil, ErrAuthorityJSON }
func (v *ResolvedSource) UnmarshalJSON([]byte) error { *v = ResolvedSource{}; return ErrAuthorityJSON }

type SourceResolver interface {
	ResolveEventSource(context.Context, Access, Type, string) (ResolvedSource, error)
}

type Producer struct{ resolver SourceResolver }

func resolverError(err error) error {
	for _, allowed := range []error{ErrInvalid, ErrDenied, ErrExpired, ErrUnavailable, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, allowed) {
			return allowed
		}
	}
	return ErrUnavailable
}

func NewProducer(resolver SourceResolver) (*Producer, error) {
	if resolver == nil {
		return nil, ErrUnavailable
	}
	v := reflect.ValueOf(resolver)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return nil, ErrUnavailable
		}
	}
	return &Producer{resolver}, nil
}

func normalizeRequest(r Request) (Request, error) {
	if d, ok := Lookup(r.EventType); !ok {
		return Request{}, ErrInvalid
	} else if d.Support != CurrentNativeSource {
		return Request{}, ErrUnavailable
	}
	var err error
	if r.SourceID, err = canonicalID(r.SourceID); err != nil {
		return Request{}, err
	}
	if r.LogicalOperationID, err = canonicalID(r.LogicalOperationID); err != nil {
		return Request{}, err
	}
	if r.RootTraceID, err = canonicalID(r.RootTraceID); err != nil {
		return Request{}, err
	}
	if r.CausationID != nil {
		v, err := canonicalID(*r.CausationID)
		if err != nil {
			return Request{}, err
		}
		r.CausationID = &v
	}
	return r, nil
}

func fromResolved(r Request, facts ResolvedSource) (Envelope, error) {
	d, ok := Lookup(r.EventType)
	if !ok || agentcognitive.ValidateAgentReference(facts.Agent) != nil ||
		facts.Agent.Role != agentruntime.PersonalAgent || !validPerson(facts.Agent.Principal) ||
		facts.Actor.Type != actorref.Person || facts.Actor.ID != facts.Agent.Principal.ID ||
		!facts.Source.Owner.Equal(facts.Agent.Principal) || !facts.MetadataBound || (d.RequiresText && !facts.TextPresent) ||
		facts.Source.ID != r.SourceID || facts.Source.Type != d.Source ||
		!validVersion(facts.Source.Version, d.VersionKind) || facts.ResolvedAt.IsZero() ||
		d.Support != CurrentNativeSource ||
		(d.NativeStatus != "" && facts.NativeStatus != d.NativeStatus) ||
		(d.VersionKind == RevisionVersion && facts.Source.Version.Revision < d.MinRevision) ||
		(r.EventType == UserQuery && facts.NativeStatus != "ACTIVE" && facts.NativeStatus != "COMPLETED") {
		return Envelope{}, ErrDenied
	}
	e := Envelope{SchemaVersion: SchemaVersion, EventType: r.EventType,
		Tenant: facts.Agent.Principal, Subject: facts.Agent.Principal, Actor: facts.Actor,
		AgentID: facts.Agent.AgentID, LogicalOperationID: r.LogicalOperationID,
		OccurredAt: facts.OccurredAt.UTC(), ReceivedAt: facts.ResolvedAt.UTC(),
		ExpiresAt: facts.OccurredAt.UTC().Add(MaxEventTTL), Source: facts.Source,
		Purpose: d.Purpose, RootTraceID: r.RootTraceID, CausationID: r.CausationID,
		PayloadRef: PayloadReference{Type: d.Source, ID: facts.Source.ID}, ProcessingStatus: Unavailable}
	e.EventID = StableEventID(e)
	if err := Validate(e, facts.ResolvedAt); err != nil {
		return Envelope{}, err
	}
	return e, nil
}

func (p *Producer) Produce(ctx context.Context, access Access, request Request) (Envelope, error) {
	if p == nil || p.resolver == nil {
		return Envelope{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Envelope{}, err
	}
	if access.SessionDigest == ([32]byte{}) {
		return Envelope{}, ErrDenied
	}
	r, err := normalizeRequest(request)
	if err != nil {
		return Envelope{}, err
	}
	facts, err := p.resolver.ResolveEventSource(ctx, access, r.EventType, r.SourceID)
	if err != nil {
		return Envelope{}, resolverError(err)
	}
	return fromResolved(r, facts)
}

// Revalidate checks the current source and lifetime of an old envelope. It does
// not dispatch or approve processing. Its linearization point is the resolver's
// final native statement; later consumers must check again under their own
// transaction/consent boundary (AIR015/018), not cache a permanent grant.
func (p *Producer) Revalidate(ctx context.Context, access Access, event Envelope) error {
	if p == nil || p.resolver == nil {
		return ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if access.SessionDigest == ([32]byte{}) {
		return ErrDenied
	}
	if err := Validate(event, event.ReceivedAt); err != nil {
		return err
	}
	facts, err := p.resolver.ResolveEventSource(ctx, access, event.EventType, event.Source.ID)
	if err != nil {
		return resolverError(err)
	}
	if err = Validate(event, facts.ResolvedAt); err != nil {
		return err
	}
	current, err := fromResolved(Request{event.EventType, event.Source.ID, event.LogicalOperationID, event.RootTraceID, event.CausationID}, facts)
	if err != nil {
		return err
	}
	if current.EventID != event.EventID || current.AgentID != event.AgentID ||
		!current.Subject.Equal(event.Subject) || !current.Tenant.Equal(event.Tenant) ||
		current.Actor != event.Actor || current.Source != event.Source ||
		!current.OccurredAt.Equal(event.OccurredAt) || !current.ExpiresAt.Equal(event.ExpiresAt) {
		return ErrDenied
	}
	return nil
}
