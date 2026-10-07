package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/jackc/pgx/v5"
)

var _ agentevent.SourceResolver = (*Store)(nil)

// Event identity is resolved in the same final SQL statement as the native
// source. This is metadata-only ingress, not an analysis/egress grant. v1 does
// not register anonymous, Organization, Business or Community ingress.
const agentEventCurrentPrincipalSQL = `
	JOIN sessions ses ON ses.token_sha256=$2
	JOIN accounts actor ON actor.id=ses.account_id AND actor.account_type='person' AND actor.status='active'
	JOIN agents ag ON ag.principal_account_id=actor.id AND ag.agent_type='personal' AND ag.status='active'
	JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
`

const agentEventCurrentSessionSQL = `
	AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp()
	AND ses.idle_expires_at>clock_timestamp()
	AND ($3::boolean OR ses.authentication_method<>'dev_phone')
`

// ResolveEventSource never accepts actor/subject/Agent claims from a request.
// A current session derives the Person account and its exact active Agent with
// native metadata; source ownership/status is part of that same SQL snapshot.
// The snapshot is not a permanent grant: dispatch/consumption must check again.
func (s *Store) ResolveEventSource(ctx context.Context, access agentevent.Access, kind agentevent.Type, sourceID string) (agentevent.ResolvedSource, error) {
	if s == nil || s.pool == nil {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return agentevent.ResolvedSource{}, err
	}
	ref, err := actorref.Parse("PERSON", sourceID)
	if err != nil || sourceID != strings.TrimSpace(sourceID) || ref.ID == "00000000-0000-0000-0000-000000000000" {
		return agentevent.ResolvedSource{}, agentevent.ErrInvalid
	}
	if access.SessionDigest == ([32]byte{}) {
		return agentevent.ResolvedSource{}, agentevent.ErrDenied
	}
	descriptor, registered := agentevent.Lookup(kind)
	if !registered {
		return agentevent.ResolvedSource{}, agentevent.ErrInvalid
	}
	var facts agentevent.ResolvedSource
	var ownerID, agentID string
	switch kind {
	case agentevent.MomentCreated:
		err = s.pool.QueryRow(ctx, `SELECT actor.id,ag.id,m.revision,m.created_at,clock_timestamp(),m.status,
			length(btrim(m.title||' '||m.body))>0
			FROM moments m `+agentEventCurrentPrincipalSQL+`
			WHERE m.id=$1 AND m.author_account_id=actor.id AND m.status='draft' AND m.visibility='private'
			`+agentEventCurrentSessionSQL, ref.ID, access.SessionDigest[:], s.devPhoneEnabled).Scan(
			&ownerID, &agentID, &facts.Source.Version.Revision, &facts.OccurredAt, &facts.ResolvedAt,
			&facts.NativeStatus, &facts.TextPresent)
		facts.Source.Version.Kind = agentevent.RevisionVersion
	case agentevent.UserQuery:
		var canonicalContent []byte
		// to_jsonb(t) is the current native source's canonical representation.
		// The private query/conversation is hashed locally and is never emitted,
		// logged, persisted in a new event table or sent to a model. updated_at
		// is a Task snapshot modification time, not a per-message input time.
		err = s.pool.QueryRow(ctx, `SELECT actor.id,ag.id,t.updated_at,clock_timestamp(),t.status,
			length(btrim(t.query)) BETWEEN 1 AND 240,to_jsonb(t)
			FROM agent_tasks t `+agentEventCurrentPrincipalSQL+`
			WHERE t.id=$1 AND t.owner_account_id=actor.id AND t.principal_type='person'
			AND t.acting_user_account_id=actor.id AND t.status IN ('ACTIVE','COMPLETED')
			`+agentEventCurrentSessionSQL, ref.ID, access.SessionDigest[:], s.devPhoneEnabled).Scan(
			&ownerID, &agentID, &facts.OccurredAt, &facts.ResolvedAt, &facts.NativeStatus,
			&facts.TextPresent, &canonicalContent)
		if err == nil {
			facts.Source.Version, err = agentevent.QueryVersion(facts.OccurredAt, canonicalContent)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentevent.ResolvedSource{}, agentevent.ErrDenied
	}
	if err != nil {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	principal, err := actorref.ParsePrincipal("PERSON", ownerID)
	if err != nil {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	facts.Actor = actorref.ActorRef{Type: actorref.Person, ID: principal.ID}
	facts.Agent = agentcognitive.AgentReference{AgentID: agentID, Principal: principal, Role: agentruntime.PersonalAgent}
	facts.Source.Type, facts.Source.ID, facts.Source.Owner = descriptor.Source, ref.ID, principal
	facts.MetadataBound = true
	return facts, nil
}

// ProduceAgentEvent is a real callable internal native producer. It is not
// automatically hooked to domain writes, reliable outbox/inbox, a HTTP route,
// a provider or a running event bus. Those boundaries remain later tasks.
func (s *Store) ProduceAgentEvent(ctx context.Context, access agentevent.Access, request agentevent.Request) (agentevent.Envelope, error) {
	producer, err := agentevent.NewProducer(s)
	if err != nil {
		return agentevent.Envelope{}, err
	}
	return producer.Produce(ctx, access, request)
}

func (s *Store) RevalidateAgentEvent(ctx context.Context, access agentevent.Access, event agentevent.Envelope) error {
	producer, err := agentevent.NewProducer(s)
	if err != nil {
		return err
	}
	return producer.Revalidate(ctx, access, event)
}
