package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

func memoryMaintenanceTx(ctx context.Context, tx pgx.Tx) error {
	if ctx == nil || tx == nil {
		return agentoutbox.ErrInvalid
	}
	v := reflect.ValueOf(tx)
	if v.Kind() == reflect.Pointer && v.IsNil() {
		return agentoutbox.ErrInvalid
	}
	return ctx.Err()
}

// This trusted maintenance binding narrows derived state only. It does not
// manufacture a human session or permit any content/candidate/Memory write.
const memoryMaintenanceBindingSQL = `SELECT ap.agent_id::text FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ag.id=$2 AND ag.agent_type='personal' AND ag.status='active'
 AND ap.profile_version>0 FOR SHARE OF a,ag FOR UPDATE OF ap`

func lockMemoryMaintenanceBindingTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) error {
	var agent string
	if err := tx.QueryRow(ctx, memoryMaintenanceBindingSQL, e.Subject.ID, e.AgentID).Scan(&agent); err != nil || agent != e.AgentID {
		return agentoutbox.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,76033))`, e.Subject.ID); err != nil {
		return agentoutbox.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,76034))`, e.Subject.ID+":"+e.AgentID+":"+e.RootTraceID); err != nil {
		return agentoutbox.ErrUnavailable
	}
	return nil
}

const memoryOutboxCurrentSQL = `SELECT m.version,m.status,m.updated_at,m.xmin::text,
 birdtie_memory_outbox_fingerprint(m,m.xmin::text),clock_timestamp()
 FROM agent_memories m WHERE m.id=$1 AND m.agent_id=$2 AND m.owner_id=$3 AND m.owner_type='PERSON'
 AND m.status IN('ACTIVE','DELETED') AND (m.source_type='EXPLICIT' OR m.status='DELETED') FOR SHARE OF m`

type memoryOutboxSource struct {
	revision         int64
	status           agentoutbox.SourceStatus
	at               time.Time
	row, fingerprint string
	observed         time.Time
}

func resolveMemoryOutboxTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (memoryOutboxSource, bool, error) {
	var s memoryOutboxSource
	err := tx.QueryRow(ctx, memoryOutboxCurrentSQL, e.Source.ID, e.AgentID, e.Subject.ID).Scan(&s.revision, &s.status, &s.at, &s.row, &s.fingerprint, &s.observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	if err != nil {
		return s, false, agentoutbox.ErrUnavailable
	}
	current := s.revision == e.Source.Revision && s.status == e.Source.Status && s.fingerprint == e.Source.Fingerprint && s.at.Equal(e.OccurredAt) && s.row != "" && !s.at.After(s.observed)
	return s, current, nil
}

const memoryRootAttemptsSQL = `SELECT COALESCE(sum(attempt),0)::bigint FROM agent_domain_outbox
 WHERE subject_id=$1 AND agent_id=$2 AND root_trace_id=$3 AND schema_version='agent-memory-outbox-v1'`

func memoryRootAttemptsTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (int64, error) {
	var n int64
	if err := tx.QueryRow(ctx, memoryRootAttemptsSQL, e.Subject.ID, e.AgentID, e.RootTraceID).Scan(&n); err != nil || n < 0 || n > agentoutbox.MaxMemoryRootAttempts {
		return 0, agentoutbox.ErrUnavailable
	}
	return n, nil
}

func scanMemoryReceipt(row scanner, c agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	p := agentoutbox.ConsumerRecord{EventID: c.EventID, Subject: c.Subject, HandlerVersion: c.HandlerVersion}
	err := row.Scan(&p.State, &p.Fence, &p.Attempt, &p.Reason, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return agentoutbox.ConsumerRecord{}, err
	}
	if agentoutbox.ValidateConsumerRecord(p) != nil {
		return agentoutbox.ConsumerRecord{}, agentoutbox.ErrInvalid
	}
	return p, nil
}

func memoryParentCurrentTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) error {
	if e.EventType == agentoutbox.MemoryUpdated {
		return nil
	}
	if e.CausationID == nil {
		return agentoutbox.ErrInvalid
	}
	parent, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2`, *e.CausationID, e.Subject.ID))
	if err != nil {
		return agentoutbox.ErrUnavailable
	}
	c := agentoutbox.Claim{EventID: parent.Event.EventID, Subject: e.Subject, HandlerVersion: agentoutbox.MemoryHandler}
	p, err := scanMemoryReceipt(tx.QueryRow(ctx, `SELECT control_state,fence,attempt,reason_code,created_at,updated_at FROM agent_consumer_inbox WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3`, c.EventID, c.Subject.ID, c.HandlerVersion), c)
	if err != nil {
		return agentoutbox.ErrUnavailable
	}
	return agentoutbox.ValidateMemoryCausation(e, parent, p)
}

func memoryFinalTx(ctx context.Context, tx pgx.Tx, r agentoutbox.Record, claim agentoutbox.Claim, admission time.Time) error {
	_, current, err := resolveMemoryOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return err
	}
	if !current {
		return agentoutbox.ErrConflict
	}
	if err = memoryParentCurrentTx(ctx, tx, r.Event); err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentoutbox.ErrUnavailable
	}
	if err = agentoutbox.CheckFence(now, claim, claim); err != nil {
		return err
	}
	if !r.Event.ExpiresAt.After(now) {
		return agentoutbox.ErrExpired
	}
	if err = outboxControlAdmissionClock(admission, now); err != nil {
		return err
	}
	_, err = memoryRootAttemptsTx(ctx, tx, r.Event)
	if err != nil {
		return err
	}
	return ctx.Err()
}

func memoryPerson(id string) actorref.PrincipalRef {
	return actorref.PrincipalRef{Type: actorref.Person, ID: id}
}
