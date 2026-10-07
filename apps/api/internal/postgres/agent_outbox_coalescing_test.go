package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This spy calls the original native append, not an alternate producer. It
// does not execute PostgreSQL or establish row-lock/concurrency behavior.
type refreshRow struct {
	pgx.Row
	e     agentoutbox.Envelope
	ready bool
	err   error
}

func (r refreshRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) == 1 {
		*dest[0].(*bool) = r.ready
		return nil
	}
	if len(dest) != 7 {
		return errors.New("unexpected refresh row shape")
	}
	*dest[0].(*string) = r.e.Subject.ID
	*dest[1].(*string) = r.e.AgentID
	*dest[2].(*int64) = r.e.Source.Revision
	*dest[3].(*agentoutbox.SourceStatus) = r.e.Source.Status
	*dest[4].(*time.Time) = r.e.OccurredAt
	*dest[5].(*time.Time) = r.e.ReceivedAt
	*dest[6].(*string) = r.e.Source.Fingerprint
	return nil
}

type refreshTx struct {
	pgx.Tx
	e                                                           agentoutbox.Envelope
	ready                                                       bool
	queries, commands                                           []string
	args                                                        [][]any
	catalogErr, coalesceErr, insertErr, rollbackErr, releaseErr error
	cancel                                                      context.CancelFunc
}

func (tx *refreshTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, sql)
	if sql != outboxMomentCurrentSQL {
		return refreshRow{ready: tx.ready, err: tx.catalogErr}
	}
	return refreshRow{e: tx.e, ready: tx.ready}
}
func (tx *refreshTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.commands = append(tx.commands, sql)
	tx.args = append(tx.args, args)
	if strings.HasPrefix(sql, "INSERT INTO agent_domain_outbox") && tx.insertErr != nil {
		return pgconn.CommandTag{}, tx.insertErr
	}
	if sql == outboxRefreshCoalesceSQL {
		if tx.cancel != nil {
			tx.cancel()
		}
		if tx.coalesceErr != nil {
			return pgconn.CommandTag{}, tx.coalesceErr
		}
	}
	if strings.HasPrefix(sql, "ROLLBACK TO SAVEPOINT birdtie_outbox_refresh_coalescing") && tx.rollbackErr != nil {
		return pgconn.CommandTag{}, tx.rollbackErr
	}
	if sql == "RELEASE SAVEPOINT birdtie_outbox_refresh_coalescing" && tx.releaseErr != nil {
		return pgconn.CommandTag{}, tx.releaseErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func refreshEnvelope() agentoutbox.Envelope {
	e := outboxInitialTestEnvelope()
	e.EventType = agentoutbox.MomentUpdated
	e.Source.Revision = 2
	e.ReceivedAt = e.OccurredAt.Add(time.Second)
	return e
}

func TestAgentOutboxRefreshAppendConsumesBeforeCaptureRelease(t *testing.T) {
	tx := &refreshTx{e: refreshEnvelope(), ready: true}
	if err := appendMomentOutboxTx(context.Background(), tx, tx.e.Source.ID, agentoutbox.MomentUpdated); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(tx.commands, "\n")
	if !strings.Contains(joined, "SET delivery_state='INVALIDATED'") {
		t.Fatalf("successful original append did not consume short-window refresh controls; commands=%d", len(tx.commands))
	}
	if !strings.HasPrefix(tx.commands[0], "SAVEPOINT birdtie_native_outbox_capture") ||
		!strings.HasPrefix(tx.commands[1], "INSERT INTO agent_domain_outbox") ||
		tx.commands[len(tx.commands)-1] != "RELEASE SAVEPOINT birdtie_native_outbox_capture" {
		t.Fatal("coalescing must follow original insert and precede its release")
	}
	if len(tx.commands) != 6 || tx.commands[2] != "SAVEPOINT birdtie_outbox_refresh_coalescing" ||
		tx.commands[3] != outboxRefreshCoalesceSQL || tx.commands[4] != "RELEASE SAVEPOINT birdtie_outbox_refresh_coalescing" {
		t.Fatal("optimization is not isolated after the original insert")
	}
	insert, update := tx.args[1], tx.args[3]
	want := []any{insert[0], insert[2], insert[3], insert[4], insert[5], insert[6], insert[9], insert[10], insert[11]}
	if !reflect.DeepEqual(update, want) {
		t.Fatal("coalescing did not bind exact newly captured native metadata/time/IDs")
	}
}

func TestAgentOutboxRefreshOriginalPathsRetained(t *testing.T) {
	for _, kind := range []agentoutbox.EventType{agentoutbox.MomentCreated, agentoutbox.MomentWithdrawn} {
		t.Run(string(kind), func(t *testing.T) {
			e := refreshEnvelope()
			e.EventType = kind
			if kind == agentoutbox.MomentCreated {
				e.Source.Revision = 1
			} else {
				e.Source.Status = agentoutbox.Withdrawn
			}
			tx := &refreshTx{e: e, ready: true}
			if err := appendMomentOutboxTx(context.Background(), tx, e.Source.ID, kind); err != nil {
				t.Fatal(err)
			}
			if len(tx.queries) != 1 || len(tx.commands) != 3 {
				t.Fatal("original creation/withdrawal path touched optimization")
			}
		})
	}
	t.Run("insert_failure_is_original_failure", func(t *testing.T) {
		failure := errors.New("insert unavailable")
		tx := &refreshTx{e: refreshEnvelope(), ready: true, insertErr: failure}
		if err := appendMomentOutboxTx(context.Background(), tx, tx.e.Source.ID, agentoutbox.MomentUpdated); err != failure || len(tx.commands) != 2 || len(tx.queries) != 1 {
			t.Fatal("failed insert was swallowed or optimized")
		}
	})
}

func TestAgentOutboxRefreshOptionalSchemaAndFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		ready                              bool
		catalogErr, coalesceErr, errorWant error
		rollback                           bool
	}{
		{name: "schema064_or079_incomplete", ready: false},
		{name: "missing_relation_race", ready: true, coalesceErr: &pgconn.PgError{Code: "42P01"}, rollback: true},
		{name: "missing_column_race", ready: true, coalesceErr: &pgconn.PgError{Code: "42703"}, rollback: true},
		{name: "probe_missing_column_race", catalogErr: &pgconn.PgError{Code: "42703"}, rollback: true},
		{name: "permission_not_schema_skip", ready: true, coalesceErr: &pgconn.PgError{Code: "42501"}},
		{name: "deadline_guard_failure", ready: true, coalesceErr: &pgconn.PgError{Code: "23514"}},
		{name: "deadlock_not_suppressed", ready: true, coalesceErr: &pgconn.PgError{Code: "40P01"}},
		{name: "unknown_database_error", ready: true, coalesceErr: errors.New("private database detail")},
		{name: "catalog_error", ready: true, catalogErr: errors.New("catalog unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &refreshTx{e: refreshEnvelope(), ready: tc.ready, catalogErr: tc.catalogErr, coalesceErr: tc.coalesceErr}
			err := appendMomentOutboxTx(context.Background(), tx, tx.e.Source.ID, agentoutbox.MomentUpdated)
			recoverable := tc.rollback || (tc.catalogErr == nil && tc.coalesceErr == nil)
			if recoverable {
				if err != nil || tx.commands[len(tx.commands)-1] != "RELEASE SAVEPOINT birdtie_native_outbox_capture" {
					t.Fatal("schema-only skip did not preserve original insertion")
				}
			} else {
				want := tc.coalesceErr
				if tc.catalogErr != nil {
					want = tc.catalogErr
				}
				if err != want {
					t.Fatal("non-schema failure was swallowed")
				}
				if strings.Contains(strings.Join(tx.commands, "\n"), "RELEASE SAVEPOINT birdtie_native_outbox_capture") {
					t.Fatal("failed optimization released original capture")
				}
			}
			rollback := strings.Contains(strings.Join(tx.commands, "\n"), "ROLLBACK TO SAVEPOINT birdtie_outbox_refresh_coalescing")
			if rollback != tc.rollback {
				t.Fatal("optional rollback does not match missing-schema classification")
			}
			if !tc.ready && tc.catalogErr == nil && strings.Contains(strings.Join(tx.commands, "\n"), "SET delivery_state='INVALIDATED'") {
				t.Fatal("missing protective schema was treated as absence of history")
			}
		})
	}
	for _, part := range []string{"rollback", "release", "cancel"} {
		t.Run(part, func(t *testing.T) {
			tx := &refreshTx{e: refreshEnvelope(), ready: true}
			ctx := context.Background()
			want := errors.New(part)
			switch part {
			case "rollback":
				tx.coalesceErr = &pgconn.PgError{Code: "42P01"}
				tx.rollbackErr = want
			case "release":
				tx.releaseErr = want
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				tx.cancel = cancel
				want = context.Canceled
			}
			if err := appendMomentOutboxTx(ctx, tx, tx.e.Source.ID, agentoutbox.MomentUpdated); err != want {
				t.Fatal("optimization completion failure was swallowed")
			}
			if tx.commands[len(tx.commands)-1] == "RELEASE SAVEPOINT birdtie_native_outbox_capture" {
				t.Fatal("completion failure cannot release native capture")
			}
		})
	}
}

// SQL contract checks are deliberately labelled static. They prove that the
// actual append sends these predicates, not that PostgreSQL executed them or
// that a mocked SQL engine reproduces locks, rows, fairness or throughput.
func TestAgentOutboxRefreshNativeSQLClosedContract(t *testing.T) {
	guards := []string{
		"d.tenant_id=$2 AND d.subject_id=$2 AND d.actor_id=$2", "d.agent_id=$3 AND d.source_id=$4",
		"d.event_id<>$1 AND d.source_revision<$5", "d.source_status='draft'", "d.event_type IN('MOMENT_CREATED','MOMENT_UPDATED')",
		"d.delivery_state='PENDING' AND d.attempt=0 AND d.fence=0 AND d.lease_owner IS NULL AND d.lease_until IS NULL",
		"d.causation_id IS NULL AND d.root_trace_id=d.logical_operation_id", "INTERVAL '5 seconds'",
		"LIMIT 64 FOR UPDATE OF d SKIP LOCKED", "clk AS MATERIALIZED(SELECT clock_timestamp() at,count(*) locked_count FROM locked)",
		"newest.source_revision=$5 AND newest.source_fingerprint=$6", "newest.occurred_at=$7 AND newest.received_at=$8 AND newest.expires_at=$9",
		"newest.received_at<=clk.at AND newest.updated_at<=clk.at AND newest.expires_at>clk.at", "newest.expires_at=newest.occurred_at+INTERVAL '15 minutes'",
		"m.visibility='private' AND m.status='draft' AND m.revision=newest.source_revision AND m.updated_at=newest.occurred_at",
		"actor.account_type='person' AND actor.status='active' AND ag.agent_type='personal' AND ag.status='active'",
		"d.updated_at<=clk.at AND d.received_at<=clk.at AND d.expires_at>clk.at", "d.received_at>=clk.at-INTERVAL '5 seconds'",
		"newest.source_fingerprint=encode(sha256(convert_to(jsonb_build_object(",
	}
	for _, guard := range guards {
		if !strings.Contains(outboxRefreshCoalesceSQL, guard) {
			t.Errorf("missing actual SQL guard: %s", guard)
		}
	}
	for _, table := range []string{"agent_consumer_inbox", "agent_effect_ledger", "agent_enrichment_purpose_previews", "agent_candidate_retention_previews", "agent_multi_candidate_previews", "agent_enrichment_runs"} {
		if strings.Count(outboxRefreshCoalesceSQL, "FROM public."+table+" ") != 2 {
			t.Errorf("%s history must be protected both before and after candidate locks", table)
		}
		if !strings.Contains(outboxRefreshCatalogSQL, "('"+table+"',") {
			t.Errorf("%s missing from schema gate", table)
		}
	}
	if !strings.Contains(outboxRefreshCatalogSQL, "NOT a.attisdropped") || !strings.Contains(outboxRefreshCatalogSQL, "format_type(a.atttypid,a.atttypmod)<>r.typ") {
		t.Fatal("catalog gate does not verify real non-dropped types")
	}
	if strings.Contains(outboxRefreshCatalogSQL, "('agent_effect_ledger','event_id'") {
		t.Fatal("064 effects must not require the later 082 column")
	}
	set := strings.Split(strings.Split(outboxRefreshCoalesceSQL, " SET ")[1], "\n FROM ")[0]
	if set != "delivery_state='INVALIDATED',updated_at=clk.at" {
		t.Fatal("immutable event/source/op/root/timestamps/TTL or fence mutated")
	}
	if strings.Count(outboxRefreshCoalesceSQL, "clock_timestamp()") != 1 {
		t.Fatal("coalescing must use one native clock after candidate locks")
	}
	// Compare the complete original source fingerprint, including links and
	// actual row xmin. Qualification is the only deliberate SQL difference.
	start := strings.Index(outboxMomentCurrentSQL, "encode(sha256(")
	end := strings.Index(outboxMomentCurrentSQL, "\n FROM moments")
	fingerprint := outboxMomentCurrentSQL[start:end]
	qualified := strings.ReplaceAll(fingerprint, "FROM moment_", "FROM public.moment_")
	if !strings.Contains(outboxRefreshCoalesceSQL, qualified) {
		t.Fatal("current source fingerprint diverged from original native capture")
	}
}
