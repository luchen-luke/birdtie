package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/jackc/pgx/v5"
)

var _ acb.Store = (*Store)(nil)

type contextBuilderBinding struct {
	session, owner, agent, authority string
	limit                            time.Time
}

func contextBuilderError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, foundation.ErrNotFound) {
		return acb.ErrDenied
	}
	return acb.ErrUnavailable
}
func (s *Store) beginContextBuilder(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, contextBuilderBinding, error) {
	var b contextBuilderBinding
	if ctx == nil || s == nil || s.pool == nil || ctx.Err() != nil {
		return nil, b, acb.ErrUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, b, acb.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, acb.ErrUnavailable
	}
	fail := func(err error) (pgx.Tx, contextBuilderBinding, error) {
		tx.Rollback(context.Background())
		return nil, contextBuilderBinding{}, err
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(acb.ErrUnavailable)
	}
	// Current Account precedes Session/Agent locks. Pool default RR is ignored.
	var generation string
	if e = tx.QueryRow(ctx, `SELECT id,xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.WorkspacePrincipal.ID).Scan(&b.owner, &generation); e != nil {
		return fail(contextBuilderError(e))
	}
	var sessionLimit time.Time
	if e = tx.QueryRow(ctx, `SELECT id,LEAST(expires_at,idle_expires_at) FROM sessions WHERE account_id=$1 AND token_sha256=$2
 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp()
 AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, b.owner, a.SessionDigest[:], s.devPhoneEnabled).Scan(&b.session, &sessionLimit); e != nil {
		return fail(contextBuilderError(e))
	}
	var agentGeneration, metadataGeneration string
	var metadataVersion int64
	if e = tx.QueryRow(ctx, `SELECT ag.id,ag.xmin::text,ap.xmin::text,ap.profile_version FROM agents ag
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=$1 AND ap.owner_type='PERSON'
 WHERE ag.principal_account_id=$1 AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0
 AND ag.created_at<=clock_timestamp() AND ap.created_at<=clock_timestamp() AND ap.updated_at<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=$1 AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id)
 FOR SHARE OF ag,ap`, b.owner).Scan(&b.agent, &agentGeneration, &metadataGeneration, &metadataVersion); e != nil {
		return fail(contextBuilderError(e))
	}
	raw, _ := json.Marshal([]any{b.session, b.owner, generation, b.agent, agentGeneration, metadataGeneration, metadataVersion})
	h := sha256.Sum256(raw)
	b.authority = hex.EncodeToString(h[:])
	b.limit = sessionLimit.UTC()
	return tx, b, nil
}
func (s *Store) finishContextBuilder(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r *acb.Request) (time.Time, error) {
	var at time.Time
	var alive bool
	e := tx.QueryRow(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT clock.at,EXISTS(SELECT 1 FROM sessions se JOIN accounts actor ON actor.id=se.account_id
 JOIN agents ag ON ag.principal_account_id=actor.id JOIN agent_profiles ap ON ap.agent_id=ag.id
 WHERE se.id=$1 AND actor.id=$2 AND ag.id=$3 AND actor.account_type='person' AND actor.status='active'
 AND ag.agent_type='personal' AND ag.status='active' AND ap.owner_id=actor.id AND ap.owner_type='PERSON' AND ap.profile_version>0
 AND se.revoked_at IS NULL AND se.expires_at>clock.at AND se.idle_expires_at>clock.at
 AND ($4::boolean OR se.authentication_method<>'dev_phone')) FROM clock`, b.session, b.owner, b.agent, s.devPhoneEnabled).Scan(&at, &alive)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, acb.ErrUnavailable
	}
	if !alive {
		return time.Time{}, acb.ErrDenied
	}
	at = at.UTC()
	if r != nil {
		if e = acb.ValidateAt(*r, at); e != nil {
			return time.Time{}, e
		}
	}
	return at, nil
}
func (s *Store) ResolveOwnContextAgent(ctx context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	tx, b, e := s.beginContextBuilder(ctx, a)
	if e != nil {
		return agentcognitive.AgentReference{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = s.finishContextBuilder(ctx, tx, b, nil); e != nil {
		return agentcognitive.AgentReference{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return agentcognitive.AgentReference{}, acb.ErrUnavailable
	}
	return agentcognitive.AgentReference{AgentID: b.agent, Principal: actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}, Role: agentruntime.PersonalAgent}, nil
}
func contextBuilderSource(kind, id string, at time.Time, raw []byte) (acb.Source, error) {
	v, e := acb.PublicVersion(kind, at, raw)
	return acb.Source{Kind: kind, ID: id, NativeTime: at.UTC(), Version: v}, e
}
func contextBuilderLimit(bundle *acb.Bundle, at time.Time) {
	if !at.IsZero() && at.Before(bundle.ExpiresAt) {
		bundle.ExpiresAt = at.UTC()
	}
}

// BuildOwnAgentContext uses existing native sources and never stores a context,
// consent, inferred fact or Memory. PUBLIC and human review are separate modes.
func (s *Store) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	if e := acb.ValidateShape(r); e != nil {
		return acb.BuiltContext{}, e
	}
	tx, b, e := s.beginContextBuilder(ctx, r.Access)
	if e != nil {
		return acb.BuiltContext{}, e
	}
	defer tx.Rollback(context.Background())
	if r.Agent.AgentID != b.agent {
		return acb.BuiltContext{}, acb.ErrDenied
	}
	observed, e := s.finishContextBuilder(ctx, tx, b, &r)
	if e != nil {
		return acb.BuiltContext{}, e
	}
	out := acb.BuiltContext{Authority: b.authority, Request: r, Bundle: acb.Bundle{SchemaVersion: acb.SchemaVersion, Agent: r.Agent, Mode: r.Mode, RequestID: r.RequestID, TaskID: r.TaskID, CityID: r.CityID, CurrentQuery: r.CurrentQuery, ObservedAt: observed, ExpiresAt: observed.Add(acb.MaxLease), ModelAccess: "UNAVAILABLE", Sections: acb.Sections{Profile: "NOT_REQUESTED", Memories: "NOT_REQUESTED", Places: "NOT_REQUESTED", Activities: "NOT_REQUESTED", Relationships: "UNAVAILABLE", Policies: "NOT_REQUESTED"}}}
	contextBuilderLimit(&out.Bundle, r.DeadlineAt)
	contextBuilderLimit(&out.Bundle, b.limit)
	if r.Mode == acb.RulesPublicQuery {
		e = s.contextBuilderPublic(ctx, tx, b, r, &out)
	} else if r.Mode == acb.MachineTaskContext {
		e = s.contextBuilderMachine(ctx, tx, b, r, &out)
	} else {
		e = s.contextBuilderHumanReview(ctx, tx, b, r, &out)
	}
	if e != nil {
		return acb.BuiltContext{}, e
	}
	final, e := s.finishContextBuilder(ctx, tx, b, &r)
	if e != nil {
		return acb.BuiltContext{}, e
	}
	// Current wall clock after every potentially blocking source lock/read. All
	// source limits stay fixed; a slow read does not create or renew a lease.
	if !out.Bundle.ExpiresAt.After(final) {
		return acb.BuiltContext{}, acb.ErrExpired
	}
	for _, source := range out.Bundle.Sources {
		if source.NativeTime.After(final) {
			return acb.BuiltContext{}, acb.ErrDenied
		}
	}
	if r.Mode == acb.RulesPublicQuery {
		if final, e = contextBuilderFinalPublic(ctx, tx, b, r, out.Bundle.ExpiresAt); e != nil {
			return acb.BuiltContext{}, e
		}
	} else if r.Mode == acb.MachineTaskContext {
		// The last native read rechecks the grant, current source projections,
		// phantom ACLs and database clock in this same transaction. No human
		// review seal or old field visibility is an authorization fallback.
		resolved, err := contextPurposeResolveTx(ctx, tx, b, r.PurposeGrantID, acb.PurposeSelectionFromRequest(r))
		if err != nil {
			return acb.BuiltContext{}, err
		}
		before, _ := json.Marshal(out.Bundle.Sources)
		after, _ := json.Marshal(resolved.Sources)
		if out.Authority != resolved.Authority || !bytes.Equal(before, after) || !out.Bundle.ExpiresAt.After(resolved.ObservedAt) {
			return acb.BuiltContext{}, acb.ErrDenied
		}
		final = resolved.ObservedAt
	}
	out.Bundle.ObservedAt = final
	// Derive descriptive field evidence only after the original final native
	// session/grant/source/ACL/clock boundary. It grants no additional purpose.
	if e = contextBuilderFieldEvidence(&out.Bundle); e != nil {
		return acb.BuiltContext{}, e
	}
	if ctx.Err() != nil {
		return acb.BuiltContext{}, acb.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return acb.BuiltContext{}, acb.ErrUnavailable
	}
	return out, nil
}

func contextBuilderFieldEvidence(b *acb.Bundle) error {
	if b == nil {
		return acb.ErrUnavailable
	}
	b.FieldEvidenceSet = nil
	// The existing public rules projection permits 100 places / 30 activities
	// in its original 64KiB envelope. Do not break it by appending repeated field
	// metadata. The purpose-approved machine reader retains its original smaller
	// caps and handles public fields together with private selected declarations.
	if b.Mode == acb.RulesPublicQuery {
		return nil
	}
	var e error
	b.FieldEvidenceSet, e = acb.BuildFieldEvidenceSet(*b)
	return e
}

func (s *Store) contextBuilderMachine(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r acb.Request, out *acb.BuiltContext) error {
	resolved, e := contextPurposeResolveTx(ctx, tx, b, r.PurposeGrantID, acb.PurposeSelectionFromRequest(r))
	if e != nil {
		return e
	}
	contextBuilderLimit(&out.Bundle, resolved.ExpiresAt)
	if e = s.contextBuilderSelectedTaskPayload(ctx, tx, b, r, out); e != nil {
		return e
	}
	out.Bundle.Sources = append([]acb.Source(nil), resolved.Sources...)
	out.Authority = resolved.Authority
	return nil
}

// Used only inside the exact authenticated native transaction. This helper
// assembles payload for a human preview or an already-resolved machine read;
// calling it supplies neither a grant nor a reusable control object.
func (s *Store) contextBuilderSelectedTaskPayload(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r acb.Request, out *acb.BuiltContext) error {
	var e error
	// Existing exact human/public domain readers supply only the selected
	// payload. The separate native purpose resolver supplies permission; these
	// reader helpers alone cannot create it.
	for _, selection := range []acb.Selection{acb.ActivitySearch, acb.PlaceSearch} {
		public := r
		public.Selection = selection
		if selection == acb.ActivitySearch {
			public.PlaceIDs = nil
		} else {
			public.ActivityIDs = nil
		}
		if e = s.contextBuilderPublic(ctx, tx, b, public, out); e != nil {
			return e
		}
	}
	if len(r.ActivityIDs) == 0 {
		out.Bundle.Sections.Activities = "NOT_REQUESTED"
	}
	if len(r.PlaceIDs) == 0 {
		out.Bundle.Sections.Places = "NOT_REQUESTED"
	}
	if e = s.contextBuilderHumanReview(ctx, tx, b, r, out); e != nil {
		return e
	}
	if len(r.RelationshipTieIDs) == 0 {
		out.Bundle.Sections.Relationships = "NOT_REQUESTED"
	} else {
		out.Bundle.Sections.Relationships = "AVAILABLE"
		for _, id := range r.RelationshipTieIDs {
			var peer string
			e = tx.QueryRow(ctx, `SELECT CASE WHEN t.person_a_account_id=$2 THEN t.person_b_account_id ELSE t.person_a_account_id END
 FROM person_ties t JOIN connection_requests fr ON fr.id=t.request_id AND fr.scope='friend' AND fr.state='accepted'
 AND LEAST(fr.sender_account_id,fr.recipient_account_id)=t.person_a_account_id AND GREATEST(fr.sender_account_id,fr.recipient_account_id)=t.person_b_account_id
 JOIN person_agent_relationship_consent consent ON consent.account_id=$2 AND consent.enabled
 JOIN accounts peer ON peer.id=CASE WHEN t.person_a_account_id=$2 THEN t.person_b_account_id ELSE t.person_a_account_id END
 WHERE t.id=$1 AND t.status='active' AND $2 IN(t.person_a_account_id,t.person_b_account_id) AND peer.account_type='person' AND peer.status='active'
 AND NOT EXISTS(SELECT 1 FROM account_blocks blocked WHERE (blocked.blocker_account_id=$2 AND blocked.blocked_account_id=peer.id) OR (blocked.blocked_account_id=$2 AND blocked.blocker_account_id=peer.id))`, id, b.owner).Scan(&peer)
			if e != nil {
				return contextBuilderError(e)
			}
			out.Bundle.Relationships = append(out.Bundle.Relationships, acb.ContextTie{ID: id, PeerAccountID: peer, State: "ACCEPTED"})
		}
	}
	city := acb.ContextCity{ID: r.CityID}
	if e = tx.QueryRow(ctx, `SELECT time_zone FROM cities WHERE id=$1 AND publication_status='published' AND (expires_at IS NULL OR expires_at>clock_timestamp())`, r.CityID).Scan(&city.TimeZone); e != nil {
		return contextBuilderError(e)
	}
	out.Bundle.City = &city
	out.Bundle.Task = &acb.ContextTask{ID: r.TaskID, Query: r.CurrentQuery, UpdatedAt: r.TaskUpdatedAt}
	// The authoritative source list was captured from the exact same current
	// source selections. Public helpers' duplicate anchors and human-only names
	// are not carried into the machine context or treated as grants.
	return nil
}

func (s *Store) contextPurposeReviewTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, c acb.PurposeCapture) (acb.PurposeReview, error) {
	sel := c.BoundSelection
	r := acb.Request{TaskID: sel.TaskID, TaskUpdatedAt: sel.TaskUpdatedAt, CurrentQuery: sel.CurrentQuery, CityID: sel.CityID,
		ProfileFields: sel.ProfileFields, MemoryIDs: sel.MemoryIDs, PolicyFamilies: sel.PolicyFamilies, PlaceIDs: sel.PlaceIDs, ActivityIDs: sel.ActivityIDs, RelationshipTieIDs: sel.RelationshipTieIDs}
	out := acb.BuiltContext{Bundle: acb.Bundle{ObservedAt: c.ObservedAt, ExpiresAt: c.ExpiresAt, Sections: acb.Sections{Profile: "NOT_REQUESTED", Memories: "NOT_REQUESTED", Policies: "NOT_REQUESTED"}}}
	if e := s.contextBuilderSelectedTaskPayload(ctx, tx, b, r, &out); e != nil {
		return acb.PurposeReview{}, e
	}
	review := acb.PurposeReview{Profile: out.Bundle.Profile, Memories: out.Bundle.Memories, Policies: out.Bundle.Policies, Places: out.Bundle.Places, Activities: out.Bundle.Activities, City: out.Bundle.City, Task: out.Bundle.Task, Relationships: out.Bundle.Relationships}
	raw, e := json.Marshal(review)
	if e != nil || len(raw) > 64*1024 {
		return acb.PurposeReview{}, acb.ErrUnavailable
	}
	return review, nil
}
func (s *Store) contextBuilderPublic(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r acb.Request, out *acb.BuiltContext) error {
	var cityRaw, taskRaw []byte
	var cityAt, taskAt time.Time
	var cityExpiry *time.Time
	e := tx.QueryRow(ctx, `SELECT c.updated_at,c.expires_at,jsonb_build_object('id',c.id,'timeZone',c.time_zone,'updatedAt',c.updated_at,'expiry',c.expires_at,'cityRow',c.xmin::text,'context',cx.id,'contextRow',cx.xmin::text,'stateRow',cc.xmin::text)
 FROM cities c JOIN contexts cx ON cx.city_id=c.id AND cx.context_type='CITY' JOIN city_contexts cc ON cc.city_id=c.id
 WHERE c.id=$1 AND c.publication_status='published' AND cc.status='active' AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND c.updated_at<=clock_timestamp() AND cx.created_at<=clock_timestamp() AND cc.updated_at<=clock_timestamp()
 FOR SHARE OF c,cx,cc`, r.CityID).Scan(&cityAt, &cityExpiry, &cityRaw)
	if e != nil {
		return contextBuilderError(e)
	}
	var currentQuery string
	e = tx.QueryRow(ctx, `SELECT t.updated_at,COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),jsonb_build_object('id',t.id,'owner',t.owner_account_id,'city',t.city_context_id,'context',t.context_id,'status',t.status,'intent',t.intent,'currentQuery',COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'updatedAt',t.updated_at,'row',t.xmin::text)
 FROM agent_tasks t JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=$3
 WHERE t.id=$1 AND t.owner_account_id=$2 AND t.principal_type='person' AND t.acting_user_account_id=$2
 AND t.city_context_id=$3 AND t.context_type='CITY' AND t.status='ACTIVE' AND t.updated_at<=clock_timestamp()
 FOR SHARE OF t`, r.TaskID, b.owner, r.CityID).Scan(&taskAt, &currentQuery, &taskRaw)
	if e != nil {
		return contextBuilderError(e)
	}
	if currentQuery != r.CurrentQuery || !taskAt.Equal(r.TaskUpdatedAt) {
		return acb.ErrDenied
	}
	for _, f := range []struct {
		kind, id string
		at       time.Time
		raw      []byte
	}{{"PUBLIC_CITY", r.CityID, cityAt, cityRaw}, {"CURRENT_TASK_REQUEST", r.TaskID, taskAt, taskRaw}} {
		source, e := contextBuilderSource(f.kind, f.id, f.at, f.raw)
		if e != nil {
			return e
		}
		out.Bundle.Sources = append(out.Bundle.Sources, source)
	}
	if cityExpiry != nil {
		contextBuilderLimit(&out.Bundle, *cityExpiry)
	}
	if r.Selection == acb.ActivitySearch {
		out.Bundle.Sections.Activities = "AVAILABLE"
		for _, id := range r.ActivityIDs {
			var raw []byte
			var at time.Time
			e = tx.QueryRow(ctx, `SELECT a.updated_at,jsonb_build_object('id',a.id,'title',a.title,'category',a.category_code,'starts',a.starts_at,'ends',a.ends_at,'updatedAt',a.updated_at,'expiry',a.expires_at,'activityRow',a.xmin::text,'organizerRow',ao.xmin::text)
 FROM activities a JOIN activity_organizers ao ON ao.activity_id=a.id WHERE a.id=$1 AND a.city_id=$2
 AND a.publication_status='published' AND a.visibility='public' AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp()
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND a.updated_at<=clock_timestamp() FOR SHARE OF a,ao`, id, r.CityID).Scan(&at, &raw)
			if e != nil {
				return contextBuilderError(e)
			}
			source, e := contextBuilderSource("PUBLIC_ACTIVITY", id, at, raw)
			if e != nil {
				return e
			}
			out.Bundle.Sources = append(out.Bundle.Sources, source)
			activity, e := scanActivity(tx.QueryRow(ctx, `SELECT `+activityColumns+publishedActivityFrom+` AND a.id=$1 AND a.city_id=$3 AND a.visibility='public'
 AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp() AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())`, id, b.owner, r.CityID))
			if e != nil {
				return contextBuilderError(e)
			}
			out.Activities = append(out.Activities, activity)
			out.Bundle.Activities = append(out.Bundle.Activities, acb.PublicActivity{ID: activity.ID, Title: activity.Title, Category: activity.CategoryCode, StartsAt: activity.StartsAt, EndsAt: activity.EndsAt, OrganizerType: activity.Organizer.Type, OrganizerID: activity.Organizer.ID})
			contextBuilderLimit(&out.Bundle, activity.EndsAt)
			if activity.Source.ExpiresAt != nil {
				contextBuilderLimit(&out.Bundle, *activity.Source.ExpiresAt)
			}
		}
	} else {
		out.Bundle.Sections.Places = "AVAILABLE"
		for _, id := range r.PlaceIDs {
			var at time.Time
			var raw []byte
			e = tx.QueryRow(ctx, `SELECT p.updated_at,jsonb_build_object('id',p.id,'name',p.name,'category',p.category_code,'updatedAt',p.updated_at,'expiry',p.expires_at,'row',p.xmin::text)
 FROM places p WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND p.updated_at<=clock_timestamp() FOR SHARE OF p`, id, r.CityID).Scan(&at, &raw)
			if e != nil {
				return contextBuilderError(e)
			}
			source, e := contextBuilderSource("PUBLIC_PLACE", id, at, raw)
			if e != nil {
				return e
			}
			out.Bundle.Sources = append(out.Bundle.Sources, source)
			place, e := scanPlace(tx.QueryRow(ctx, `SELECT `+placeColumns+` FROM places p WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp())`, id, r.CityID))
			if e != nil {
				return contextBuilderError(e)
			}
			out.Places = append(out.Places, place)
			out.Bundle.Places = append(out.Bundle.Places, acb.PublicPlace{ID: place.ID, Name: place.Name, Category: place.CategoryCode})
			if place.Source.ExpiresAt != nil {
				contextBuilderLimit(&out.Bundle, *place.Source.ExpiresAt)
			}
		}
	}
	return nil
}
func contextBuilderFinalPublic(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r acb.Request, leaseEnd time.Time) (time.Time, error) {
	// Phantom Block/ACL revocations are evaluated in this final current statement,
	// not presumed impossible just because retained entity rows were locked.
	var valid bool
	var at time.Time
	e := tx.QueryRow(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS at)
 SELECT clock.at,clock.at<$7::timestamptz AND EXISTS(SELECT 1 FROM sessions se JOIN accounts actor ON actor.id=se.account_id
 JOIN agents ag ON ag.principal_account_id=actor.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
 WHERE se.id=$8 AND actor.id=$2 AND actor.account_type='person' AND actor.status='active' AND ag.id=$9 AND ag.status='active' AND ag.agent_type='personal'
 AND se.revoked_at IS NULL AND se.expires_at>clock.at AND se.idle_expires_at>clock.at)
 AND EXISTS(SELECT 1 FROM agent_tasks t JOIN cities c ON c.id=t.city_context_id JOIN city_contexts cc ON cc.city_id=c.id
 WHERE t.id=$1 AND t.owner_account_id=$2 AND t.status='ACTIVE' AND t.updated_at=$3 AND t.city_context_id=$4
 AND c.publication_status='published' AND cc.status='active' AND (c.expires_at IS NULL OR c.expires_at>clock.at))
 AND NOT EXISTS(SELECT unnest($5::uuid[]) EXCEPT SELECT a.id FROM activities a
 WHERE a.city_id=$4 AND a.visibility='public' AND a.publication_status='published' AND birdtie_activity_visible_to(a.id,$2::uuid)
 AND a.cancelled_at IS NULL AND a.ends_at>clock.at AND (a.expires_at IS NULL OR a.expires_at>clock.at)
 AND NOT EXISTS(SELECT 1 FROM account_blocks blocked WHERE (blocked.blocker_account_id=$2 AND blocked.blocked_account_id=a.host_account_id) OR (blocked.blocked_account_id=$2 AND blocked.blocker_account_id=a.host_account_id)))
 AND NOT EXISTS(SELECT unnest($6::uuid[]) EXCEPT SELECT p.id FROM places p WHERE p.city_id=$4 AND p.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock.at)) FROM clock`, r.TaskID, b.owner, r.TaskUpdatedAt, r.CityID, r.ActivityIDs, r.PlaceIDs, leaseEnd, b.session, b.agent).Scan(&at, &valid)
	if e != nil || ctx.Err() != nil {
		return time.Time{}, acb.ErrUnavailable
	}
	if !valid {
		return time.Time{}, acb.ErrDenied
	}
	return at.UTC(), nil
}
func (s *Store) contextBuilderHumanReview(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, r acb.Request, out *acb.BuiltContext) error {
	if len(r.ProfileFields) > 0 {
		out.Bundle.Sections.Profile = "UNCONFIGURED"
		out.Bundle.Profile = map[string]json.RawMessage{}
		var version int64
		var rowToken string
		var at time.Time
		e := tx.QueryRow(ctx, `SELECT written_profile_version,updated_at,xmin::text FROM agent_private_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' FOR SHARE`, b.agent, b.owner).Scan(&version, &at, &rowToken)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return acb.ErrUnavailable
		}
		if e == nil {
			for _, key := range r.ProfileFields {
				var raw []byte
				if e = tx.QueryRow(ctx, `SELECT fields->$3 FROM agent_private_profiles WHERE agent_id=$1 AND owner_id=$2`, b.agent, b.owner, key).Scan(&raw); e != nil {
					return acb.ErrUnavailable
				}
				out.Bundle.Profile[key] = append(json.RawMessage(nil), raw...)
			}
			out.Bundle.Sections.Profile = "AVAILABLE"
			out.Bundle.Sources = append(out.Bundle.Sources, acb.Source{Kind: "HUMAN_PRIVATE_PROFILE", ID: b.agent, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: version}, NativeTime: at.UTC(), RowToken: rowToken})
		}
	}
	if len(r.MemoryIDs) > 0 {
		out.Bundle.Sections.Memories = "AVAILABLE"
		for _, id := range r.MemoryIDs {
			memory, e := scanAgentMemory(tx.QueryRow(ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3 AND owner_type='PERSON' AND status='ACTIVE' AND source_type='EXPLICIT' AND visibility='PRIVATE' AND valid_from<=clock_timestamp() AND valid_until>clock_timestamp() FOR SHARE`, id, b.agent, b.owner))
			if e != nil {
				return contextBuilderError(e)
			}
			var rowToken string
			if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_memories WHERE id=$1 AND agent_id=$2 AND owner_id=$3`, id, b.agent, b.owner).Scan(&rowToken); e != nil {
				return acb.ErrUnavailable
			}
			projection, e := nativeContextReviewMemory(memory)
			if e != nil {
				return acb.ErrUnavailable
			}
			out.Bundle.Memories = append(out.Bundle.Memories, projection)
			out.Bundle.Sources = append(out.Bundle.Sources, acb.Source{Kind: "HUMAN_EXPLICIT_MEMORY", ID: memory.ID, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: memory.Version}, NativeTime: memory.UpdatedAt.UTC(), RowToken: rowToken})
			contextBuilderLimit(&out.Bundle, memory.ValidUntil)
		}
	}
	for _, family := range r.PolicyFamilies {
		out.Bundle.Sections.Policies = "UNCONFIGURED"
		var raw []byte
		var version int64
		var rowToken string
		var from, expires, updated time.Time
		e := tx.QueryRow(ctx, `SELECT native_revision,settings,valid_from,expires_at,updated_at,xmin::text FROM agent_policy_settings WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND family=$3 FOR SHARE`, b.agent, b.owner, family).Scan(&version, &raw, &from, &expires, &updated, &rowToken)
		if errors.Is(e, pgx.ErrNoRows) {
			out.Bundle.Policies = append(out.Bundle.Policies, agentpolicysettings.DefaultRecord(family))
			continue
		}
		if e != nil {
			return acb.ErrUnavailable
		}
		settings, e := agentpolicysettings.NormalizeSettings(family, raw, from, expires)
		if e != nil {
			return acb.ErrUnavailable
		}
		record := agentpolicysettings.Record{Family: family, Configured: true, NativeRevision: version, Status: "ACTIVE", Settings: settings, ValidFrom: &from, ExpiresAt: &expires, UpdatedAt: &updated}
		if agentpolicysettings.ValidateRecord(record, out.Bundle.ObservedAt) != nil {
			return acb.ErrDenied
		}
		out.Bundle.Policies = append(out.Bundle.Policies, record)
		out.Bundle.Sections.Policies = "AVAILABLE"
		out.Bundle.Sources = append(out.Bundle.Sources, acb.Source{Kind: "HUMAN_POLICY_SETTINGS", ID: b.agent + ":" + string(family), Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: version}, NativeTime: updated.UTC(), RowToken: rowToken})
		contextBuilderLimit(&out.Bundle, expires)
	}
	return nil
}

// The retained native record supplies real declaration/creation/validity times.
// Missing older port metadata must remain unknown, never inferred from a lease.
func nativeContextReviewMemory(memory agentmemory.Record) (acb.ReviewMemory, error) {
	if agentmemory.ValidateRecord(memory) != nil || memory.SourceType != agentmemory.SourceExplicit || memory.Status != agentmemory.StatusActive {
		return acb.ReviewMemory{}, acb.ErrUnavailable
	}
	confidence, e := agentconfidence.NormalizeAssessment(agentconfidence.Assessment{Semantics: agentconfidence.DirectDeclaration, Value: &memory.Confidence})
	if e != nil {
		return acb.ReviewMemory{}, acb.ErrUnavailable
	}
	from, created := memory.ValidFrom.UTC(), memory.CreatedAt.UTC()
	return acb.ReviewMemory{ID: memory.ID, Version: memory.Version, MemoryType: memory.MemoryType, MemoryKey: memory.MemoryKey, Summary: memory.Summary,
		StructuredValue: append(json.RawMessage(nil), memory.StructuredValue...), ValidFrom: &from, CreatedAt: &created, ValidUntil: memory.ValidUntil.UTC(), Confidence: &confidence}, nil
}
