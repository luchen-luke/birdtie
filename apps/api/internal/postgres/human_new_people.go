package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/jackc/pgx/v5"
)

var humanNewPeopleUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type humanNewPeopleBinding struct {
	Digest   [32]byte
	DevPhone bool
	Policy   *nativeToolPolicy
	Observed *time.Time
}

var _ newpeople.HumanStore = (*Store)(nil)
var humanNewPeopleKey struct {
	sync.Once
	key [32]byte
	err error
}

func humanNewPeopleDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func humanNewPeopleSeal(actor identity.Actor, digest [32]byte, r newpeople.HumanReceipt) (string, error) {
	humanNewPeopleKey.Do(func() { _, humanNewPeopleKey.err = rand.Read(humanNewPeopleKey.key[:]) })
	if humanNewPeopleKey.err != nil {
		return "", newpeople.ErrForbidden
	}
	raw, e := json.Marshal(struct {
		Actor     identity.Actor
		Digest    [32]byte
		Response  newpeople.Response
		Proof     string
		ExpiresAt time.Time
	}{actor, digest, r.Response, r.Proof, r.ExpiresAt})
	if e != nil {
		return "", e
	}
	h := hmac.New(sha256.New, humanNewPeopleKey.key[:])
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Hash only IDs/xmin and current eligibility. Source text and private Profile
// values never enter a receipt. Native audit IDs close legal Block/unblock ABA.
// Pair scope also covers policy audience dependencies; no read permission is
// inferred from these tokens. The original visibility predicates remain decisive.
const humanNewPeopleVersionSQL = `encode(sha256(convert_to(jsonb_build_array(
 i.id,i.xmin::text,a.xmin::text,p.xmin::text,opt.xmin::text,ic.xmin::text,target.xmin::text,city.xmin::text,place.xmin::text,
 (SELECT jsonb_agg(jsonb_build_array(x.id,x.xmin::text) ORDER BY x.id) FROM accounts x WHERE x.id IN($1,i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.id,x.xmin::text) ORDER BY x.id) FROM agents x WHERE x.principal_account_id IN($1,i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.agent_id,x.xmin::text) ORDER BY x.agent_id) FROM agent_profiles x WHERE x.owner_id IN($1,i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.agent_id,x.xmin::text) ORDER BY x.agent_id) FROM agent_profile_field_visibility x WHERE x.owner_id IN($1,i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.blocker_account_id,x.blocked_account_id,x.xmin::text) ORDER BY x.blocker_account_id,x.blocked_account_id) FROM account_blocks x WHERE (x.blocker_account_id=$1 AND x.blocked_account_id=i.creator_account_id) OR (x.blocked_account_id=$1 AND x.blocker_account_id=i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.id,x.xmin::text) ORDER BY x.id) FROM person_ties x WHERE x.person_a_account_id=LEAST($1::uuid,i.creator_account_id) AND x.person_b_account_id=GREATEST($1::uuid,i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.id,x.xmin::text) ORDER BY x.id) FROM connection_requests x WHERE (x.sender_account_id=$1 AND x.recipient_account_id=i.creator_account_id) OR (x.recipient_account_id=$1 AND x.sender_account_id=i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.intent_id,x.invitee_account_id,x.xmin::text) ORDER BY x.intent_id,x.invitee_account_id) FROM social_intent_invitations x WHERE x.intent_id IN(i.id,$2::uuid)),
 (SELECT jsonb_agg(jsonb_build_array(x.community_id,x.user_account_id,x.xmin::text) ORDER BY x.community_id,x.user_account_id) FROM community_memberships x WHERE x.user_account_id IN($1,i.creator_account_id)),
 (SELECT jsonb_agg(jsonb_build_array(x.id,x.xmin::text,(x.expires_at IS NULL OR x.expires_at>statement_timestamp())) ORDER BY x.id) FROM communities x WHERE x.id IN(SELECT community_id FROM community_memberships WHERE user_account_id IN($1,i.creator_account_id))),
 (SELECT jsonb_agg(jsonb_build_array(x.id,x.xmin::text) ORDER BY x.id) FROM audit_events x WHERE x.actor_account_id IN($1,i.creator_account_id))
 )::text,'UTF8')),'hex')`

const humanNewPeopleRelations = `LOCK TABLE accounts,sessions,user_profiles,person_new_people_consent,
 social_intents,social_intent_audience_targets,social_intent_invitations,contexts,cities,places,
 agents,agent_profiles,agent_profile_field_visibility,account_blocks,person_ties,
 connection_requests,community_memberships,communities,audit_events IN ACCESS SHARE MODE`

func (s *Store) humanNewPeopleRead(ctx context.Context, actor identity.Actor, digest [32]byte, source string) (newpeople.HumanReceipt, error) {
	var out newpeople.HumanReceipt
	if ctx == nil || ctx.Err() != nil || actor.AccountType != "person" || !humanNewPeopleUUID.MatchString(actor.ID) || !humanNewPeopleUUID.MatchString(source) || digest == ([32]byte{}) {
		return out, identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	out, e = s.humanNewPeopleReadTx(ctx, tx, actor, digest, source, nil, nil)
	if e != nil {
		return newpeople.HumanReceipt{}, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return newpeople.HumanReceipt{}, newpeople.ErrForbidden
	}
	return out, nil
}

// Keeps the original same-current-source resolver in one transaction so the
// registered read adapter can add a policy fence without copying the matcher.
func (s *Store) humanNewPeopleReadTx(ctx context.Context, tx pgx.Tx, actor identity.Actor, digest [32]byte, source string, policy *nativeToolPolicy, observed *time.Time) (newpeople.HumanReceipt, error) {
	var out newpeople.HumanReceipt
	var e error
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, e
	}
	// Acquire all relation waits before Account then Session, matching existing
	// native human readers. No idle refresh, follow-on Authenticate or domain write.
	if _, e = tx.Exec(ctx, humanNewPeopleRelations); e != nil {
		return out, e
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, actor.ID).Scan(&id); errors.Is(e, pgx.ErrNoRows) {
		return out, identity.ErrUnauthorized
	} else if e != nil {
		return out, e
	}
	if e = s.lockHumanMomentSession(ctx, tx, digest, actor.ID); e != nil {
		return out, e
	}
	var sessionFrame string
	e = tx.QueryRow(ctx, `SELECT encode(sha256(convert_to(jsonb_build_array(a.id,a.xmin::text,se.id,se.xmin::text,se.created_at,se.authentication_method,se.expires_at,se.idle_expires_at,encode(se.token_sha256,'hex'))::text,'UTF8')),'hex') FROM accounts a JOIN sessions se ON se.account_id=a.id WHERE a.id=$1 AND a.status='active' AND a.account_type='person' AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>clock_timestamp() AND se.idle_expires_at>clock_timestamp() AND ($3::boolean OR se.authentication_method<>'dev_phone')`, actor.ID, digest[:], s.devPhoneEnabled).Scan(&sessionFrame)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, identity.ErrUnauthorized
	}
	if e != nil {
		return out, e
	}
	out.Response, out.Proof, out.ExpiresAt, e = s.findNewPeopleTx(ctx, tx, actor.ID, source, humanNewPeopleBinding{Digest: digest, DevPhone: s.devPhoneEnabled, Policy: policy, Observed: observed})
	if e != nil {
		return newpeople.HumanReceipt{}, e
	}
	out.Proof = humanNewPeopleDigest([]byte(sessionFrame + ":" + out.Proof))
	// findNewPeopleTx is the final current statement: it materializes current
	// owner/Session with PG clock, all source ACL/version inputs and deadlines.
	// No later Authenticate or Session-only query can introduce another wait and
	// retain a prior source snapshot. Commit is the only remaining database call.

	return out, nil
}
func (s *Store) ReadHumanNewPeople(ctx context.Context, a identity.Actor, d [32]byte, source string) (newpeople.HumanReceipt, error) {
	r, e := s.humanNewPeopleRead(ctx, a, d, source)
	if e != nil {
		return newpeople.HumanReceipt{}, e
	}
	r.Seal, e = humanNewPeopleSeal(a, d, r)
	return r, e
}
func (s *Store) RevalidateHumanNewPeople(ctx context.Context, a identity.Actor, d [32]byte, r newpeople.HumanReceipt) error {
	seal, e := humanNewPeopleSeal(a, d, r)
	if e != nil || r.Seal == "" || !hmac.Equal([]byte(seal), []byte(r.Seal)) {
		return newpeople.ErrForbidden
	}
	current, e := s.humanNewPeopleRead(ctx, a, d, r.Response.SourceIntentID)
	if e != nil {
		return e
	}
	if current.Proof != r.Proof || !current.ExpiresAt.Equal(r.ExpiresAt) || !reflect.DeepEqual(current.Response, r.Response) {
		return newpeople.ErrChanged
	}
	return nil
}
