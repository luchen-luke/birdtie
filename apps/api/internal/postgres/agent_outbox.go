package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Capture stores only current native metadata. It grants neither analysis nor
// background identity; the consumer below never writes a candidate or effect.
const outboxMomentCurrentSQL = `SELECT actor.id::text,ag.id::text,m.revision,m.status,
 CASE WHEN $2='MOMENT_CREATED' THEN m.created_at ELSE m.updated_at END,clock_timestamp(),
 encode(sha256(convert_to(jsonb_build_object(
 'moment',to_jsonb(m),'moment_xmin',m.xmin::text,
 'actor',to_jsonb(actor),'actor_xmin',actor.xmin::text,
 'agent',to_jsonb(ag),'agent_xmin',ag.xmin::text,
 'metadata',to_jsonb(ap),'metadata_xmin',ap.xmin::text,
 'activity_links',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.activity_id) FROM moment_activity_links l WHERE l.moment_id=m.id),'[]'::jsonb),
 'community_links',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.community_id) FROM moment_community_links l WHERE l.moment_id=m.id),'[]'::jsonb),
 'organization_links',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.organization_id) FROM moment_organization_links l WHERE l.moment_id=m.id),'[]'::jsonb)
 )::text,'UTF8')),'hex')
 FROM moments m JOIN accounts actor ON actor.id=m.author_account_id
 JOIN agents ag ON ag.principal_account_id=actor.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
 WHERE m.id=$1 AND m.visibility='private' AND m.status IN ('draft','withdrawn')
 AND actor.account_type='person' AND actor.status='active'
 AND ag.agent_type='personal' AND ag.status='active'
 FOR SHARE OF m,actor,ag,ap`

func outboxNativeOperation(source string, revision int64, kind agentoutbox.EventType) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("birdtie.moment.operation.v1\x00%s\x00%d\x00%s", source, revision, kind)))
	hash[6] = hash[6]&0x0f | 0x80
	hash[8] = hash[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", hash[:4], hash[4:6], hash[6:8], hash[8:10], hash[10:16])
}

func resolveOutboxMomentTx(ctx context.Context, tx pgx.Tx, source string, kind agentoutbox.EventType) (agentoutbox.Envelope, time.Time, error) {
	var e agentoutbox.Envelope
	var owner string
	var now time.Time
	err := tx.QueryRow(ctx, outboxMomentCurrentSQL, source, kind).Scan(&owner, &e.AgentID, &e.Source.Revision, &e.Source.Status, &e.OccurredAt, &now, &e.Source.Fingerprint)
	if err != nil {
		return e, now, err
	}
	e.SchemaVersion, e.EventType = agentoutbox.SchemaVersion, kind
	e.Subject = actorref.PrincipalRef{Type: actorref.Person, ID: owner}
	e.Tenant, e.Source.Owner = e.Subject, e.Subject
	e.Actor = actorref.ActorRef{Type: actorref.Person, ID: owner}
	e.Source.Type, e.Source.ID = agentoutbox.MomentSource, source
	e.OccurredAt, e.ReceivedAt = e.OccurredAt.UTC(), now.UTC()
	e.ExpiresAt = e.OccurredAt.Add(agentoutbox.MaxEventTTL)
	e.LogicalOperationID = outboxNativeOperation(source, e.Source.Revision, kind)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = agentoutbox.StableEventID(e)
	return e, now.UTC(), nil
}

// Called after native source/context/audit changes, before their same commit.
// No eligible metadata means no enrichment capture; it does not create an
// Agent, consent, profile or an inferred fact on behalf of the human author.
func appendMomentOutboxTx(ctx context.Context, tx pgx.Tx, source string, kind agentoutbox.EventType) error {
	e, now, err := resolveOutboxMomentTx(ctx, tx, source, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := agentoutbox.NewPending(e, now); err != nil {
		warnOutboxPendingFailure(ctx, outboxInitialPendingFailure, e, now, err)
		return err
	}
	first := e
	var firstDenial error
	for attempt := range 3 {
		if attempt > 0 {
			timer := time.NewTimer(2 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			e, now, err = resolveOutboxMomentTx(ctx, tx, source, kind)
			// Only the initial no-metadata case is allowed to skip capture.
			// A retry cannot discard an event or renew its original source TTL.
			if err != nil {
				return err
			}
			if e.EventID != first.EventID || e.LogicalOperationID != first.LogicalOperationID ||
				!e.OccurredAt.Equal(first.OccurredAt) || !e.ExpiresAt.Equal(first.ExpiresAt) ||
				e.Source != first.Source {
				return firstDenial
			}
			if _, err = agentoutbox.NewPending(e, now); err != nil {
				warnOutboxPendingFailure(ctx, outboxRetryPendingFailure, e, now, err)
				return err
			}
		}
		if _, err = tx.Exec(ctx, `SAVEPOINT birdtie_native_outbox_capture`); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_domain_outbox(event_id,event_type,tenant_id,subject_id,actor_id,agent_id,
 source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,
 occurred_at,received_at,expires_at) VALUES($1,$2,$3,$3,$3,$4,$5,$6,$7,$8,$9,$9,$10,$11,$12)`,
			e.EventID, e.EventType, e.Subject.ID, e.AgentID, e.Source.ID, e.Source.Revision, e.Source.Fingerprint,
			e.Source.Status, e.LogicalOperationID, e.OccurredAt, e.ReceivedAt, e.ExpiresAt)
		if err == nil {
			if err = coalesceMomentRefreshTx(ctx, tx, e); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `RELEASE SAVEPOINT birdtie_native_outbox_capture`)
			return err
		}
		var native *pgconn.PgError
		if !errors.As(err, &native) || native.Code != "P0001" ||
			native.Message != "native outbox capture requires current Person binding and fresh source metadata" {
			return err
		}
		if firstDenial == nil {
			firstDenial = err
		}
		if attempt == 2 {
			return firstDenial
		}
		// Actual PG clock observations can move backwards between resolver and
		// INSERT. This is bounded recovery of a trusted native capture, not a
		// clock-only classification of the mixed denial or a time tolerance.
		// Every retry re-resolves the locked source and passes the unchanged
		// strict guard; no expiry, binding or future timestamp is accepted.
		if _, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT birdtie_native_outbox_capture`); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT birdtie_native_outbox_capture`); err != nil {
			return err
		}
	}
	return firstDenial
}

const outboxColumns = `d.schema_version,d.event_id::text,d.event_type,d.subject_id::text,d.agent_id::text,
 d.logical_operation_id::text,d.root_trace_id::text,d.causation_id::text,d.occurred_at,d.received_at,d.expires_at,
 d.source_id::text,d.source_revision,d.source_status,d.source_fingerprint,d.delivery_state,d.attempt,d.fence,
 COALESCE(d.lease_owner::text,''),d.lease_until,d.next_attempt_at,d.created_at,d.updated_at`

func scanAgentOutbox(row scanner) (agentoutbox.Record, error) {
	var r agentoutbox.Record
	var subject string
	e := &r.Event
	err := row.Scan(&e.SchemaVersion, &e.EventID, &e.EventType, &subject, &e.AgentID, &e.LogicalOperationID,
		&e.RootTraceID, &e.CausationID, &e.OccurredAt, &e.ReceivedAt, &e.ExpiresAt, &e.Source.ID,
		&e.Source.Revision, &e.Source.Status, &e.Source.Fingerprint, &r.State, &r.Attempt, &r.Fence,
		&r.LeaseOwner, &r.LeaseUntil, &r.NextAttemptAt, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return agentoutbox.Record{}, err
	}
	e.Subject = actorref.PrincipalRef{Type: actorref.Person, ID: subject}
	e.Tenant, e.Source.Owner = e.Subject, e.Subject
	e.Actor = actorref.ActorRef{Type: actorref.Person, ID: subject}
	e.Source.Type = agentoutbox.MomentSource
	if e.SchemaVersion == agentoutbox.MemorySchema {
		e.Source.Type = agentoutbox.MemorySource
	}
	if e.SchemaVersion == agentoutbox.PreferenceSchema {
		e.Source.Type = agentoutbox.PreferenceSource
	}
	e.OccurredAt, e.ReceivedAt, e.ExpiresAt = e.OccurredAt.UTC(), e.ReceivedAt.UTC(), e.ExpiresAt.UTC()
	if err := agentoutbox.ValidateRecord(r); err != nil {
		return agentoutbox.Record{}, err
	}
	return r, nil
}

func outboxCurrentTx(ctx context.Context, tx pgx.Tx, r agentoutbox.Record) (bool, error) {
	current, _, err := resolveOutboxMomentTx(ctx, tx, r.Event.Source.ID, r.Event.EventType)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current.EventID == r.Event.EventID && current.AgentID == r.Event.AgentID && current.Subject == r.Event.Subject, nil
}

// ClaimAgentOutboxControl is internal metadata/control maintenance. It is not
// registered as HTTP, a scheduler, runtime permission or a candidate consumer.
// A new handler can replay unavailable controls only; it obtains no new grant.
func (s *Store) ClaimAgentOutboxControl(ctx context.Context, worker string, handler agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
	return s.claimAgentOutboxControl(ctx, worker, handler, "")
}

// A trusted maintenance selector, not human self authorization or a wire
// authority. Filtering avoids claiming unrelated tenants' control jobs; the
// same current native resolver/fence/closed consumer runs in both entry points.
func (s *Store) ClaimAgentOutboxControlForSubject(ctx context.Context, subject actorref.PrincipalRef, worker string, handler agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
	ref, err := actorref.ParsePrincipal("PERSON", subject.ID)
	if err != nil || subject.Type != actorref.Person || ref != subject || ref.ID == "00000000-0000-0000-0000-000000000000" {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrInvalid
	}
	return s.claimAgentOutboxControl(ctx, worker, handler, subject.ID)
}

func (s *Store) claimAgentOutboxControl(ctx context.Context, worker string, handler agentoutbox.HandlerVersion, subjectID string) (agentoutbox.Record, agentoutbox.Claim, error) {
	if s == nil || s.pool == nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	if _, err := agentoutbox.NormalizeWorkerID(worker); err != nil || agentoutbox.ValidateHandlerVersion(handler) != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrInvalid
	}
	if handler == agentoutbox.MemoryHandler {
		return s.claimMemoryOutbox(ctx, worker, subjectID)
	}
	if handler == agentoutbox.PreferenceHandler {
		return s.claimPreferenceOutbox(ctx, worker, subjectID)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	defer tx.Rollback(ctx)
	r, admissionObservedAt, err := selectAgentOutboxControlTx(ctx, tx, handler, subjectID)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	valid, err := outboxCurrentTx(ctx, tx, r)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	var terminal agentoutbox.State
	if !valid {
		terminal = agentoutbox.Invalidated
	} else if !r.Event.ExpiresAt.After(now) {
		terminal = agentoutbox.Expired
	} else if r.Attempt >= agentoutbox.MaxAttempts || r.Fence == math.MaxInt64 {
		terminal = agentoutbox.DeadLetter
	}
	if terminal != "" {
		if _, err = tx.Exec(ctx, `UPDATE agent_domain_outbox SET delivery_state=$2,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp() WHERE event_id=$1`, r.Event.EventID, terminal); err != nil {
			return agentoutbox.Record{}, agentoutbox.Claim{}, err
		}
		reason := agentoutbox.ReasonSourceInvalidated
		if terminal == agentoutbox.Expired {
			reason = agentoutbox.ReasonExpired
		}
		if terminal == agentoutbox.DeadLetter {
			reason = agentoutbox.ReasonAttemptsExhausted
		}
		if _, err = tx.Exec(ctx, `UPDATE agent_consumer_inbox SET control_state=$3,reason_code=$4,updated_at=clock_timestamp()
 WHERE event_id=$1 AND fence=$2 AND control_state='LEASED'`, r.Event.EventID, r.Fence, terminal, reason); err != nil {
			return agentoutbox.Record{}, agentoutbox.Claim{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return agentoutbox.Record{}, agentoutbox.Claim{}, err
		}
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrUnavailable
	}
	r, err = scanAgentOutbox(tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() AS at)
 UPDATE agent_domain_outbox d SET delivery_state='LEASED',
 attempt=d.attempt+1,fence=d.fence+1,lease_owner=$2,updated_at=clk.at,
 lease_until=LEAST(clk.at+INTERVAL '30 seconds',d.expires_at) FROM clk
 WHERE d.event_id=$1 AND d.expires_at>clk.at RETURNING `+outboxColumns, r.Event.EventID, worker))
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_consumer_inbox(event_id,subject_id,handler_version,control_state,fence,attempt)
 VALUES($1,$2,$3,'LEASED',$4,$5) ON CONFLICT(event_id,subject_id,handler_version) DO UPDATE
 SET fence=EXCLUDED.fence,attempt=EXCLUDED.attempt,updated_at=clock_timestamp()
 WHERE agent_consumer_inbox.control_state='LEASED'`, r.Event.EventID, r.Event.Subject.ID, handler, r.Fence, r.Attempt)
	if err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	claim := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID,
		HandlerVersion: handler, WorkerID: worker, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	if err = agentoutbox.CheckFence(now, claim, claim); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	if !r.Event.ExpiresAt.After(now) {
		return agentoutbox.Record{}, agentoutbox.Claim{}, agentoutbox.ErrExpired
	}
	if err = outboxControlAdmissionClock(admissionObservedAt, now); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentoutbox.Record{}, agentoutbox.Claim{}, err
	}
	return r, claim, nil
}

// ConsumeAgentOutboxControl makes a fenced control receipt in the same native
// transaction as its outbox checkpoint. It has no caller-supplied consumer or
// source/permission facts and intentionally cannot commit any business effect.
func (s *Store) ConsumeAgentOutboxControl(ctx context.Context, claim agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	if s == nil || s.pool == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
	}
	if agentoutbox.ValidateClaim(claim) != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	if claim.HandlerVersion == agentoutbox.MemoryHandler {
		return s.consumeMemoryOutbox(ctx, claim)
	}
	if claim.HandlerVersion == agentoutbox.PreferenceHandler {
		return s.consumePreferenceOutbox(ctx, claim)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	defer tx.Rollback(ctx)
	r, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE event_id=$1 FOR UPDATE OF d`, claim.EventID))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrNotFound
	}
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if r.State != agentoutbox.Leased || r.LeaseUntil == nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	current := agentoutbox.Claim{EventID: r.Event.EventID, Subject: r.Event.Subject, AgentID: r.Event.AgentID,
		HandlerVersion: claim.HandlerVersion, WorkerID: r.LeaseOwner, Fence: r.Fence, LeaseUntil: *r.LeaseUntil}
	valid, err := outboxCurrentTx(ctx, tx, r)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if err = agentoutbox.CheckFence(now, current, claim); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	state, reason := agentoutbox.Unavailable, agentoutbox.ReasonPurposeUnavailable
	if !valid {
		state, reason = agentoutbox.Invalidated, agentoutbox.ReasonSourceInvalidated
	} else if !r.Event.ExpiresAt.After(now) {
		state, reason = agentoutbox.Expired, agentoutbox.ReasonExpired
	} else {
		outcome, e := (agentoutbox.ActualConsumer{}).Consume(ctx, r, claim)
		if !errors.Is(e, agentoutbox.ErrUnavailable) || agentoutbox.ValidateOutcome(outcome) != nil {
			return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
		}
	}
	// Recheck the actual lease clock after native source locks; a late worker
	// cannot checkpoint even a harmless unavailable result under a stale fence.
	tag, err := tx.Exec(ctx, `UPDATE agent_domain_outbox SET delivery_state=$4,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp()
 WHERE event_id=$1 AND fence=$2 AND lease_owner=$3 AND delivery_state='LEASED' AND lease_until>clock_timestamp()`, claim.EventID, claim.Fence, claim.WorkerID, state)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if tag.RowsAffected() != 1 {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	var receipt agentoutbox.ConsumerRecord
	receipt.EventID, receipt.Subject, receipt.HandlerVersion = claim.EventID, claim.Subject, claim.HandlerVersion
	err = tx.QueryRow(ctx, `UPDATE agent_consumer_inbox SET control_state=$5,reason_code=$6,updated_at=clock_timestamp()
 WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3 AND fence=$4 AND control_state='LEASED'
 RETURNING control_state,fence,attempt,reason_code,created_at,updated_at`, claim.EventID, claim.Subject.ID, claim.HandlerVersion, claim.Fence, state, reason).Scan(
		&receipt.State, &receipt.Fence, &receipt.Attempt, &receipt.Reason, &receipt.CreatedAt, &receipt.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrConflict
	}
	if err != nil || agentoutbox.ValidateConsumerRecord(receipt) != nil {
		if err == nil {
			err = agentoutbox.ErrInvalid
		}
		return agentoutbox.ConsumerRecord{}, err
	}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if err = agentoutbox.CheckFence(now, current, claim); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if !r.Event.ExpiresAt.After(now) {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrExpired
	}
	if err = tx.Commit(ctx); err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	return receipt, agentoutbox.ErrUnavailable
}
