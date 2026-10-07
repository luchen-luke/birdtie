package agentparticipationsignal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Reader struct {
	pool            *pgxpool.Pool
	devPhoneEnabled bool
}

func NewReader(pool *pgxpool.Pool, devPhoneEnabled bool) *Reader {
	return &Reader{pool, devPhoneEnabled}
}

const currentSourceSQL = `/* participation_signal_current_source_v1 */
 SELECT actor.id,ag.id,p.status,p.updated_at,clock_timestamp(),to_jsonb(p),ap.created_at,
 CASE WHEN p.status='cancelled' THEN NULL::uuid ELSE a.id END,
 CASE WHEN p.status='cancelled' THEN NULL::bigint ELSE a.revision END,
 CASE WHEN p.status='cancelled' THEN NULL::jsonb ELSE jsonb_build_object('id',a.id,'revision',a.revision,'updated_at',a.updated_at,'starts_at',a.starts_at,'ends_at',a.ends_at,'cancelled_at',a.cancelled_at,'expires_at',a.expires_at,'publication_status',a.publication_status,'visibility',a.visibility,'city_id',a.city_id,'host_account_id',a.host_account_id) END
 FROM activity_participations p
 JOIN sessions ses ON ses.token_sha256=$2
 JOIN accounts actor ON actor.id=ses.account_id AND actor.account_type='person' AND actor.status='active'
 JOIN agents ag ON ag.principal_account_id=actor.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
 LEFT JOIN activities a ON a.id=p.activity_id
 LEFT JOIN cities city ON city.id=a.city_id
 WHERE p.id=$1 AND p.participant_account_id=actor.id
 AND ses.revoked_at IS NULL AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp()
 AND ($3::boolean OR ses.authentication_method<>'dev_phone')
 AND NOT EXISTS(SELECT 1 FROM agents other WHERE other.principal_account_id=actor.id AND other.agent_type='personal' AND other.status='active' AND other.id<>ag.id)
 AND ((p.status='cancelled' AND p.cancelled_at IS NOT NULL)
  OR (p.status IN ('going','pending') AND p.cancelled_at IS NULL
   AND a.publication_status='published' AND city.publication_status='published'
   AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp() AND a.revision>0
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,actor.id)
   AND (a.host_account_id IS NULL OR EXISTS(SELECT 1 FROM accounts host WHERE host.id=a.host_account_id AND host.status='active'))
   AND EXISTS(SELECT 1 FROM activity_organizers ao
    LEFT JOIN accounts person ON person.id=ao.person_account_id AND person.account_type='person' AND person.status='active'
    LEFT JOIN communities community ON community.id=ao.community_id AND community.lifecycle_status='active' AND community.publication_status='published'
    LEFT JOIN organizations organization ON organization.id=ao.organization_id AND organization.status='active'
    LEFT JOIN accounts orgprincipal ON orgprincipal.id=organization.account_id AND orgprincipal.account_type='organization' AND orgprincipal.status='active'
    LEFT JOIN businesses business ON business.id=ao.business_id AND business.status='active' AND business.claim_status='verified'
    LEFT JOIN accounts bizprincipal ON bizprincipal.id=business.account_id AND bizprincipal.account_type='business' AND bizprincipal.status='active'
    WHERE ao.activity_id=a.id AND (person.id IS NOT NULL OR community.id IS NOT NULL OR orgprincipal.id IS NOT NULL OR bizprincipal.id IS NOT NULL))
   AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE a.host_account_id IS NOT NULL
    AND ((b.blocker_account_id=actor.id AND b.blocked_account_id=a.host_account_id)
     OR (b.blocker_account_id=a.host_account_id AND b.blocked_account_id=actor.id)))))`

func storageError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrDenied
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return ErrUnavailable
}
func readSnapshot(ctx context.Context, tx pgx.Tx, access agentevent.Access, request Request, dev bool) (Signal, error) {
	var s Signal
	var owner, agent, status string
	var raw []byte
	var at, now time.Time
	var activity *string
	var rev *int64
	var metadataAt time.Time
	var target []byte
	err := tx.QueryRow(ctx, currentSourceSQL, request.ParticipationID, access.SessionDigest[:], dev).Scan(&owner, &agent, &status, &at, &now, &raw, &metadataAt, &activity, &rev, &target)
	if err != nil {
		return Signal{}, storageError(err)
	}
	kind, err := stateKind(status)
	if err != nil {
		return Signal{}, err
	}
	version, err := agentevent.SnapshotVersion(agentevent.ParticipationSource, agentevent.UpdatedAtDigestVersion, at, raw)
	if err != nil {
		return Signal{}, ErrInvalid
	}
	s = Signal{SchemaVersion: SchemaVersion, Kind: kind, Subject: actorref.PrincipalRef{Type: actorref.Person, ID: owner}, AgentID: agent, LogicalOperationID: request.LogicalOperationID, Source: agentevent.SourceReference{Type: agentevent.ParticipationSource, ID: request.ParticipationID, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: owner}, Version: version}, SourceUpdatedAt: at.UTC(), ObservedAt: now.UTC(), ExpiresAt: at.UTC().Add(agentevent.MaxEventTTL), ReceiptSemantics: string(agentevent.CurrentRetainedState), Attendance: "UNKNOWN", StableInterest: "UNKNOWN", ProcessingStatus: "UNAVAILABLE"}
	if activity != nil {
		s.ActivityID = *activity
	}
	if rev != nil {
		s.ActivityRevision = *rev
	}
	if len(target) > 0 {
		h := sha256.Sum256(append([]byte("birdtie.participation-signal.target.v1\x00"), target...))
		s.ActivitySnapshotDigest = hex.EncodeToString(h[:])
	}
	s.MetadataCreatedAt = metadataAt.UTC()
	s.SignalID = stableID(s)
	if err = Validate(s, now); err != nil {
		return Signal{}, err
	}
	return s, nil
}

// Collect is metadata-only ingress. The final native SQL is its permission
// linearization point, never a durable processing/Memory permission.
func (r *Reader) Collect(ctx context.Context, access agentevent.Access, request Request) (Signal, error) {
	if ctx == nil || !validID(request.ParticipationID) || !validID(request.LogicalOperationID) {
		return Signal{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Signal{}, err
	}
	if r == nil || r.pool == nil {
		return Signal{}, ErrUnavailable
	}
	if access.SessionDigest == ([32]byte{}) {
		return Signal{}, ErrDenied
	}
	// Override the pool/database default: the second statement must observe
	// current commits, not a repeatable historical authorization snapshot.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Signal{}, storageError(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return Signal{}, storageError(err)
	}
	first, err := readSnapshot(ctx, tx, access, request, r.devPhoneEnabled)
	if err != nil {
		return Signal{}, err
	}
	current, err := readSnapshot(ctx, tx, access, request, r.devPhoneEnabled)
	if err != nil {
		return Signal{}, err
	}
	if first.SignalID != current.SignalID || first.Source != current.Source || first.AgentID != current.AgentID || first.Subject != current.Subject {
		return Signal{}, ErrDenied
	}
	if err = tx.Commit(ctx); err != nil {
		return Signal{}, storageError(err)
	}
	if err = ctx.Err(); err != nil {
		return Signal{}, err
	}
	return current, nil
}
func (r *Reader) Revalidate(ctx context.Context, access agentevent.Access, old Signal) error {
	if err := Validate(old, old.ObservedAt); err != nil {
		return err
	}
	current, err := r.Collect(ctx, access, Request{old.Source.ID, old.LogicalOperationID})
	if err != nil {
		return err
	}
	if err = Validate(old, current.ObservedAt); err != nil {
		return err
	}
	if old.SignalID != current.SignalID || old.Source != current.Source || old.Subject != current.Subject || old.AgentID != current.AgentID || old.ActivityID != current.ActivityID || old.ActivityRevision != current.ActivityRevision || !old.SourceUpdatedAt.Equal(current.SourceUpdatedAt) {
		return ErrDenied
	}
	return nil
}
