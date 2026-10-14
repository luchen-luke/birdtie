package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
)

// The raw projection is EXACTLY 076's CURRENT_TASK_REQUEST capture. Its Go
// contextPurposeHash is neither QueryVersion(to_jsonb(Task)), PublicVersion,
// nor the independent SQL outbox fingerprint. No query/body leaves this Tx.
const taskRequestOutboxCurrentSQL = `SELECT t.status,t.updated_at,t.xmin::text,
 birdtie_task_request_outbox_fingerprint(t,t.xmin::text),clock_timestamp(),
 (t.status='ACTIVE' AND t.context_type='CITY' AND EXISTS(SELECT 1 FROM contexts cx WHERE cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=t.city_context_id)),
 jsonb_build_object('id',t.id,'owner',t.owner_account_id,'city',t.city_context_id,'context',t.context_id,'status',t.status,'intent',t.intent,'queryDigest',encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex'),'updatedAt',t.updated_at,'row',t.xmin::text)
 FROM agent_tasks t WHERE t.id=$1 AND t.owner_account_id=$2 AND t.principal_type='person' AND t.acting_user_account_id=$2 FOR SHARE OF t`

type taskRequestOutboxSource struct {
	status                         agentoutbox.SourceStatus
	at, observed                   time.Time
	row, fingerprint, purposeToken string
	contextCurrent                 bool
}

func resolveTaskRequestOutboxTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (taskRequestOutboxSource, bool, error) {
	var source taskRequestOutboxSource
	var raw []byte
	if err := memoryMaintenanceTx(ctx, tx); err != nil {
		return source, false, err
	}
	if _, err := tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return source, false, agentoutbox.ErrUnavailable
	}
	err := tx.QueryRow(ctx, taskRequestOutboxCurrentSQL, e.Source.ID, e.Subject.ID).Scan(&source.status, &source.at, &source.row, &source.fingerprint, &source.observed, &source.contextCurrent, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, false, nil
	}
	if err != nil {
		return source, false, agentoutbox.ErrUnavailable
	}
	source.at, source.observed = source.at.UTC(), source.observed.UTC()
	if !json.Valid(raw) {
		return source, false, agentoutbox.ErrUnavailable
	}
	source.purposeToken = contextPurposeHash(json.RawMessage(raw))
	current := e.Source.Revision == 0 && source.status == e.Source.Status && source.fingerprint == e.Source.Fingerprint && source.at.Equal(e.OccurredAt) && source.row != "" && !source.at.After(source.observed)
	return source, current, nil
}

const taskRequestRootAttemptsSQL = `SELECT COALESCE(sum(attempt),0)::bigint FROM agent_domain_outbox
 WHERE subject_id=$1 AND agent_id=$2 AND root_trace_id=$3 AND schema_version='agent-task-request-outbox-v1'`

func taskRequestRootAttemptsTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) (int64, error) {
	var n int64
	if err := tx.QueryRow(ctx, taskRequestRootAttemptsSQL, e.Subject.ID, e.AgentID, e.RootTraceID).Scan(&n); err != nil || n < 0 || n > agentoutbox.MaxTaskRequestRootAttempts {
		return 0, agentoutbox.ErrUnavailable
	}
	return n, nil
}
func taskRequestParentCurrentTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) error {
	if e.EventType == agentoutbox.TaskRequestChanged {
		return nil
	}
	if e.CausationID == nil {
		return agentoutbox.ErrInvalid
	}
	parent, err := scanAgentOutbox(tx.QueryRow(ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.event_id=$1 AND d.subject_id=$2`, *e.CausationID, e.Subject.ID))
	if err != nil {
		return agentoutbox.ErrUnavailable
	}
	c := agentoutbox.Claim{EventID: parent.Event.EventID, Subject: e.Subject, HandlerVersion: agentoutbox.TaskRequestHandler}
	p, err := scanMemoryReceipt(tx.QueryRow(ctx, `SELECT control_state,fence,attempt,reason_code,created_at,updated_at FROM agent_consumer_inbox WHERE event_id=$1 AND subject_id=$2 AND handler_version=$3`, c.EventID, c.Subject.ID, c.HandlerVersion), c)
	if err != nil {
		return agentoutbox.ErrUnavailable
	}
	return agentoutbox.ValidateTaskRequestCausation(e, parent, p)
}
func taskRequestFinalTx(ctx context.Context, tx pgx.Tx, r agentoutbox.Record, c agentoutbox.Claim, admission time.Time, pinned *taskRequestOutboxSource) error {
	source, current, err := resolveTaskRequestOutboxTx(ctx, tx, r.Event)
	if err != nil {
		return err
	}
	if !current {
		return agentoutbox.ErrConflict
	}
	// Task SHARE keeps its row/epoch stable, while the original CITY context
	// can change independently. A cleanup receipt must prove the same source
	// projection and eligibility used by that cleanup, without pinning clocks.
	if pinned != nil && (source.status != pinned.status || !source.at.Equal(pinned.at) || source.row != pinned.row || source.fingerprint != pinned.fingerprint || source.purposeToken != pinned.purposeToken || source.contextCurrent != pinned.contextCurrent) {
		return agentoutbox.ErrConflict
	}
	if err = taskRequestParentCurrentTx(ctx, tx, r.Event); err != nil {
		return err
	}
	var at time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
		return agentoutbox.ErrUnavailable
	}
	if err = agentoutbox.CheckFence(at, c, c); err != nil {
		return err
	}
	if !r.Event.ExpiresAt.After(at) {
		return agentoutbox.ErrExpired
	}
	if err = outboxControlAdmissionClock(admission, at); err != nil {
		return err
	}
	if _, err = taskRequestRootAttemptsTx(ctx, tx, r.Event); err != nil {
		return err
	}
	return ctx.Err()
}
