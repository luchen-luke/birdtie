package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/jackc/pgx/v5"
)

// A wrapper reuses the original two AIR014 resolvers and adds current retained
// native source snapshots. It is neither another bus nor automatic domain hooks.
type enrichmentEventResolver struct{ store *Store }

var _ agentevent.SourceResolver = enrichmentEventResolver{}

func (r enrichmentEventResolver) ResolveEventSource(ctx context.Context, access agentevent.Access, kind agentevent.Type, sourceID string) (agentevent.ResolvedSource, error) {
	if kind == agentevent.MomentCreated || kind == agentevent.UserQuery {
		return r.store.ResolveEventSource(ctx, access, kind, sourceID)
	}
	return r.resolveRetainedSource(ctx, access, kind, sourceID)
}

func (r enrichmentEventResolver) resolveRetainedSource(ctx context.Context, access agentevent.Access, kind agentevent.Type, sourceID string) (agentevent.ResolvedSource, error) {
	if r.store == nil || r.store.pool == nil {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return agentevent.ResolvedSource{}, err
	}
	d, registered := agentevent.Lookup(kind)
	if !registered {
		return agentevent.ResolvedSource{}, agentevent.ErrInvalid
	}
	if d.Support != agentevent.CurrentNativeSource {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	ref, err := actorref.Parse("PERSON", sourceID)
	if err != nil || strings.TrimSpace(sourceID) != sourceID || ref.ID == "00000000-0000-0000-0000-000000000000" {
		return agentevent.ResolvedSource{}, agentevent.ErrInvalid
	}
	if access.SessionDigest == ([32]byte{}) {
		return agentevent.ResolvedSource{}, agentevent.ErrDenied
	}
	var query string
	switch kind {
	case agentevent.MomentUpdated:
		query = `SELECT actor.id,ag.id,m.updated_at,clock_timestamp(),m.status,
		 length(btrim(m.title||' '||m.body))>0,m.revision,NULL::jsonb
		 FROM moments m ` + agentEventCurrentPrincipalSQL + `
		 WHERE m.id=$1 AND m.author_account_id=actor.id AND m.visibility='private' AND m.status='draft' AND m.revision>1`
	case agentevent.MomentDeleted:
		// WithdrawMoment retains author/version metadata. Never select the old
		// title/body, and do not call a physical purge a retained deletion fact.
		query = `SELECT actor.id,ag.id,m.updated_at,clock_timestamp(),m.status,
		 false,m.revision,NULL::jsonb FROM moments m ` + agentEventCurrentPrincipalSQL + `
		 WHERE m.id=$1 AND m.author_account_id=actor.id AND m.visibility='private' AND m.status='withdrawn' AND m.revision>1`
	case agentevent.ActivityJoined:
		query = `SELECT actor.id,ag.id,p.updated_at,clock_timestamp(),p.status,
		 false,0::bigint,to_jsonb(p) FROM activity_participations p ` + agentEventCurrentPrincipalSQL + `
		 JOIN activities a ON a.id=p.activity_id
		 JOIN cities c ON c.id=a.city_id AND c.publication_status='published'
		 WHERE p.id=$1 AND p.participant_account_id=actor.id AND p.status='going' AND p.cancelled_at IS NULL
		 AND a.publication_status='published' AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp()
		 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,actor.id)
		 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE a.host_account_id IS NOT NULL
		 AND ((b.blocker_account_id=actor.id AND b.blocked_account_id=a.host_account_id)
		 OR (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=actor.id)))`
	case agentevent.ActivityLeft:
		// The owner can invalidate a retained cancellation without receiving an
		// unavailable/private Activity. Cancelled may mean a declined request;
		// it does not prove earlier attendance or who cancelled the request.
		query = `SELECT actor.id,ag.id,p.updated_at,clock_timestamp(),p.status,
		 false,0::bigint,to_jsonb(p) FROM activity_participations p ` + agentEventCurrentPrincipalSQL + `
		 WHERE p.id=$1 AND p.participant_account_id=actor.id AND p.status='cancelled' AND p.cancelled_at IS NOT NULL`
	case agentevent.PlaceSaved:
		query = `SELECT actor.id,ag.id,sv.created_at,clock_timestamp(),'saved',false,0::bigint,to_jsonb(sv)
		 FROM saved_items sv ` + agentEventCurrentPrincipalSQL + `
		 JOIN places p ON p.id=sv.place_id JOIN cities c ON c.id=p.city_id AND c.publication_status='published'
		 WHERE sv.id=$1 AND sv.owner_account_id=actor.id AND sv.place_id IS NOT NULL
		 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())`
	case agentevent.CommunityJoined:
		query = `SELECT actor.id,ag.id,cm.updated_at,clock_timestamp(),cm.status,
		 false,0::bigint,to_jsonb(cm) FROM community_memberships cm ` + agentEventCurrentPrincipalSQL + `
		 JOIN communities c ON c.id=cm.community_id JOIN accounts co ON co.id=c.owner_account_id AND co.status='active'
		 WHERE cm.id=$1 AND cm.user_account_id=actor.id AND cm.status='active'
		 AND c.publication_status='published' AND c.lifecycle_status='active'
		 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
		 AND (c.city_id IS NULL OR EXISTS(SELECT 1 FROM cities city WHERE city.id=c.city_id AND city.publication_status='published'))
		 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
		 (b.blocker_account_id=actor.id AND b.blocked_account_id=c.owner_account_id)
		 OR (b.blocker_account_id=c.owner_account_id AND b.blocked_account_id=actor.id))`
	case agentevent.CommunityLeft:
		query = `SELECT actor.id,ag.id,cm.updated_at,clock_timestamp(),cm.status,
		 false,0::bigint,to_jsonb(cm) FROM community_memberships cm ` + agentEventCurrentPrincipalSQL + `
		 WHERE cm.id=$1 AND cm.user_account_id=actor.id AND cm.status='left'`
	case agentevent.ProfileUpdated:
		query = `SELECT actor.id,ag.id,p.updated_at,clock_timestamp(),'profile_current',false,0::bigint,to_jsonb(p)
		 FROM user_profiles p ` + agentEventCurrentPrincipalSQL + ` WHERE p.account_id=$1 AND p.account_id=actor.id`
	case agentevent.PreferenceUpdated:
		query = `SELECT actor.id,ag.id,pp.updated_at,clock_timestamp(),'explicit_preferences',false,
		 pp.written_profile_version,pp.fields FROM agent_private_profiles pp ` + agentEventCurrentPrincipalSQL + `
		 WHERE pp.agent_id=$1 AND pp.agent_id=ag.id AND pp.owner_id=actor.id AND pp.owner_type='PERSON'
		 AND pp.written_profile_version>1 AND pp.written_profile_version<=ap.profile_version`
	default:
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	// All checks and source fields use one current native SQL snapshot. Its actor
	// is the producer's current Person, not an invented historical action author.
	// This is not a persistent grant; any future consumer must recheck again.
	query += agentEventCurrentSessionSQL + ` AND NOT EXISTS(SELECT 1 FROM agents other
	 WHERE other.principal_account_id=actor.id AND other.agent_type='personal'
	 AND other.status='active' AND other.id<>ag.id)`
	var facts agentevent.ResolvedSource
	var ownerID, agentID string
	var canonicalSource []byte
	err = r.store.pool.QueryRow(ctx, query, ref.ID, access.SessionDigest[:], r.store.devPhoneEnabled).Scan(
		&ownerID, &agentID, &facts.OccurredAt, &facts.ResolvedAt, &facts.NativeStatus, &facts.TextPresent, &facts.Source.Version.Revision, &canonicalSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentevent.ResolvedSource{}, agentevent.ErrDenied
	}
	if err != nil {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	if d.VersionKind == agentevent.RevisionVersion {
		facts.Source.Version.Kind = agentevent.RevisionVersion
		if kind == agentevent.PreferenceUpdated {
			fields, decodeErr := agentprofile.DecodePrivateFields(canonicalSource)
			if decodeErr != nil || !enrichmentHasExplicitPreference(fields) {
				return agentevent.ResolvedSource{}, agentevent.ErrDenied
			}
		}
	} else {
		facts.Source.Version, err = agentevent.SnapshotVersion(d.Source, d.VersionKind, facts.OccurredAt, canonicalSource)
		if err != nil {
			return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
		}
	}
	principal, err := actorref.ParsePrincipal("PERSON", ownerID)
	if err != nil {
		return agentevent.ResolvedSource{}, agentevent.ErrUnavailable
	}
	facts.Actor = actorref.ActorRef{Type: actorref.Person, ID: principal.ID}
	facts.Agent = agentcognitive.AgentReference{AgentID: agentID, Principal: principal, Role: agentruntime.PersonalAgent}
	facts.Source.Type, facts.Source.ID, facts.Source.Owner = d.Source, ref.ID, principal
	facts.MetadataBound = true
	return facts, nil
}

func enrichmentHasExplicitPreference(f agentprofile.PrivateFields) bool {
	return len(f.PersonalPreferences)+len(f.SocialPreferences)+len(f.PreferredActivityTypes)+len(f.TravelPreferences)+len(f.InteractionPreferences)+len(f.LanguagePreferences) > 0
}

// ProduceAgentEnrichmentEvent is locally callable native metadata ingress.
// No business hook, outbox, consumer, provider or implicit Memory write exists.
func (s *Store) ProduceAgentEnrichmentEvent(ctx context.Context, access agentevent.Access, request agentevent.Request) (agentevent.Envelope, error) {
	p, err := agentevent.NewProducer(enrichmentEventResolver{s})
	if err != nil {
		return agentevent.Envelope{}, err
	}
	return p.Produce(ctx, access, request)
}

func (s *Store) RevalidateAgentEnrichmentEvent(ctx context.Context, access agentevent.Access, event agentevent.Envelope) error {
	p, err := agentevent.NewProducer(enrichmentEventResolver{s})
	if err != nil {
		return err
	}
	return p.Revalidate(ctx, access, event)
}
