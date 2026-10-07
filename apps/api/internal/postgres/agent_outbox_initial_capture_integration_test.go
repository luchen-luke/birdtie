package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var outboxInitialConditionKeys = []string{
	"schema", "tenant_person", "tenant_id", "subject_person", "subject_id", "tenant_equals_subject",
	"actor_person", "actor_equals_subject", "agent_id", "event_id", "logical_operation_id", "root_trace_id",
	"causation_id_absent_or_valid", "source_is_moment", "source_id", "source_owner_equals_subject",
	"source_revision_at_least_one", "source_fingerprint_valid", "occurred_time_valid", "received_time_valid",
	"expires_time_valid", "now_valid", "occurred_not_after_received", "received_not_after_now",
	"expiry_equals_original_occurrence_plus_15m", "stable_event_id_matches", "event_type_registered",
	"event_status_matches_type", "event_revision_matches_type", "expires_after_now", "json_marshal_ok", "encoded_bytes_within_4096",
}

func captureOutboxInitialLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		// Handler timestamp is log metadata, not a source timestamp. Omit it
		// in the test to enforce the complete domain-attribute allowlist.
		if len(groups) == 0 && a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}})))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &output
}

func requireOutboxInitialLog(t *testing.T, output *bytes.Buffer, phase, class, onlyFalse string) map[string]any {
	t.Helper()
	var row map[string]any
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	if decoder.Decode(&row) != nil || decoder.More() {
		t.Fatal("expected one complete structured warning")
	}
	allowed := map[string]bool{"level": true, "msg": true, "phase": true, "error_class": true, "conditions": true}
	if len(row) != len(allowed) {
		t.Fatal("warning contains unapproved fields")
	}
	for key := range row {
		if !allowed[key] {
			t.Fatal("warning contains unapproved field")
		}
	}
	if row["level"] != "WARN" || row["msg"] != outboxPendingFailureMessage || row["phase"] != phase || row["error_class"] != class {
		t.Fatal("warning does not use closed phase/class/message")
	}
	conditions, ok := row["conditions"].(map[string]any)
	if !ok || len(conditions) != 32 {
		t.Fatal("warning predicate set is not closed")
	}
	for _, key := range outboxInitialConditionKeys {
		value, ok := conditions[key].(bool)
		if !ok {
			t.Fatal("warning predicate is not a bool")
		}
		if onlyFalse != "" && value != (key != onlyFalse) {
			t.Fatal("unexpected controlled native predicate", key)
		}
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		t.Fatal("extra or malformed warning output")
	}
	return row
}

func outboxInitialTestEnvelope() agentoutbox.Envelope {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	p := actorref.PrincipalRef{Type: actorref.Person, ID: "84000000-0000-4000-8000-000000000001"}
	e := agentoutbox.Envelope{SchemaVersion: agentoutbox.SchemaVersion, EventType: agentoutbox.MomentCreated, Tenant: p, Subject: p, Actor: actorref.ActorRef{Type: actorref.Person, ID: p.ID}, AgentID: "84000000-0000-4000-8000-000000000002", LogicalOperationID: "84000000-0000-4000-8000-000000000003", RootTraceID: "84000000-0000-4000-8000-000000000004", OccurredAt: now, ReceivedAt: now, ExpiresAt: now.Add(agentoutbox.MaxEventTTL), Source: agentoutbox.SourceReference{Type: agentoutbox.MomentSource, ID: "84000000-0000-4000-8000-000000000005", Owner: p, Revision: 1, Status: agentoutbox.Draft, Fingerprint: strings.Repeat("a", 64)}}
	e.EventID = agentoutbox.StableEventID(e)
	return e
}

func TestAgentOutboxInitialCaptureStructuredLogAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, phase, class string
		at                 outboxPendingFailurePhase
		err                error
	}{
		{"initial", "INITIAL_NEW_PENDING", "INVALID", outboxInitialPendingFailure, agentoutbox.ErrInvalid},
		{"retry", "RETRY_NEW_PENDING", "EXPIRED", outboxRetryPendingFailure, agentoutbox.ErrExpired},
		{"wrapped", "INITIAL_NEW_PENDING", "INVALID", outboxInitialPendingFailure, fmt.Errorf("PRIVATE_ERROR_CANARY: %w", agentoutbox.ErrInvalid)},
		{"unknown_error", "RETRY_NEW_PENDING", "UNKNOWN", outboxRetryPendingFailure, errors.New("PRIVATE_ERROR_CANARY")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := captureOutboxInitialLog(t)
			e := outboxInitialTestEnvelope()
			e.SchemaVersion = "PRIVATE_SCHEMA_CANARY"
			e.AgentID = "PRIVATE_AGENT_CANARY"
			e.Source.Fingerprint = "PRIVATE_FINGERPRINT_CANARY"
			warnOutboxPendingFailure(context.Background(), tc.at, e, e.ReceivedAt, tc.err)
			requireOutboxInitialLog(t, output, tc.phase, tc.class, "")
			for _, secret := range []string{"PRIVATE_ERROR_CANARY", e.SchemaVersion, e.AgentID, e.Source.Fingerprint, e.Subject.ID, e.Source.ID, e.EventID, e.LogicalOperationID, e.RootTraceID, e.OccurredAt.Format(time.RFC3339Nano)} {
				if strings.Contains(output.String(), secret) {
					t.Fatal("raw source value leaked into warning")
				}
			}
		})
	}
	t.Run("success_and_unknown_phase_silent", func(t *testing.T) {
		output := captureOutboxInitialLog(t)
		e := outboxInitialTestEnvelope()
		warnOutboxPendingFailure(context.Background(), outboxInitialPendingFailure, e, e.ReceivedAt, nil)
		warnOutboxPendingFailure(context.Background(), outboxRetryPendingFailure, e, e.ReceivedAt, nil)
		warnOutboxPendingFailure(context.Background(), outboxPendingFailurePhase(255), e, e.ReceivedAt, agentoutbox.ErrInvalid)
		if output.Len() != 0 {
			t.Fatal("success or unregistered phase logged")
		}
	})
}

// This fake executes the actual append control flow without any PG or wall
// clock override. Only returned synthetic row clocks differ between reads.
type initialCaptureRow struct {
	pgx.Row
	e   agentoutbox.Envelope
	now time.Time
}

func (r initialCaptureRow) Scan(dest ...any) error {
	if len(dest) != 7 {
		return errors.New("unexpected initial diagnostic row shape")
	}
	*dest[0].(*string) = r.e.Subject.ID
	*dest[1].(*string) = r.e.AgentID
	*dest[2].(*int64) = r.e.Source.Revision
	*dest[3].(*agentoutbox.SourceStatus) = r.e.Source.Status
	*dest[4].(*time.Time) = r.e.OccurredAt
	*dest[5].(*time.Time) = r.now
	*dest[6].(*string) = r.e.Source.Fingerprint
	return nil
}

type initialCaptureTx struct {
	pgx.Tx
	e                         agentoutbox.Envelope
	read                      int
	commands                  []string
	initialFuture, denyInsert bool
}

func (tx *initialCaptureTx) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.read++
	now := tx.e.OccurredAt.Add(time.Second)
	if tx.initialFuture || tx.read > 1 {
		now = tx.e.OccurredAt.Add(-time.Second)
	}
	return initialCaptureRow{e: tx.e, now: now}
}
func (tx *initialCaptureTx) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(query, "INSERT INTO agent_domain_outbox") {
		tx.commands = append(tx.commands, "INSERT")
		if tx.denyInsert {
			return pgconn.CommandTag{}, &pgconn.PgError{Code: "P0001", Message: "native outbox capture requires current Person binding and fresh source metadata"}
		}
	} else {
		tx.commands = append(tx.commands, query)
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func TestAgentOutboxInitialCaptureAppendFailurePhases(t *testing.T) {
	for _, tc := range []struct {
		name, phase     string
		future, denial  bool
		reads, commands int
		want            error
	}{
		{"initial", "INITIAL_NEW_PENDING", true, false, 1, 0, agentoutbox.ErrInvalid},
		{"original_retry", "RETRY_NEW_PENDING", false, true, 2, 4, agentoutbox.ErrInvalid},
		{"success", "", false, false, 1, 3, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := captureOutboxInitialLog(t)
			e := outboxInitialTestEnvelope()
			tx := &initialCaptureTx{e: e, initialFuture: tc.future, denyInsert: tc.denial}
			err := appendMomentOutboxTx(context.Background(), tx, e.Source.ID, agentoutbox.MomentCreated)
			if err != tc.want || tx.read != tc.reads || len(tx.commands) != tc.commands {
				t.Fatal("original append resolution/retry/error flow changed")
			}
			if err == nil {
				if output.Len() != 0 {
					t.Fatal("successful capture logged")
				}
				return
			}
			requireOutboxInitialLog(t, output, tc.phase, "INVALID", "occurred_not_after_received")
			if tc.denial && !reflect.DeepEqual(tx.commands, []string{"SAVEPOINT birdtie_native_outbox_capture", "INSERT", "ROLLBACK TO SAVEPOINT birdtie_native_outbox_capture", "RELEASE SAVEPOINT birdtie_native_outbox_capture"}) {
				t.Fatal("original savepoint retry sequence changed")
			}
		})
	}
}

type initialCaptureQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func initialCaptureSnapshot(t *testing.T, q initialCaptureQueryer, ctx context.Context, label string) map[string]string {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		t.Fatal("initial diagnostic table enumeration", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]string)
	for _, name := range tables {
		var raw string
		query := `SELECT coalesce(jsonb_agg(jsonb_build_object('row',to_jsonb(r),'xmin',r.xmin::text) ORDER BY to_jsonb(r)::text,r.xmin::text),'[]'::jsonb)::text FROM ` + pgx.Identifier{"public", name}.Sanitize() + ` r`
		if err = q.QueryRow(ctx, query).Scan(&raw); err != nil {
			t.Fatal("complete diagnostic native rows", err)
		}
		result[name] = raw
	}
	if dir := os.Getenv("BIRDTIE_INITIAL_ENVELOPE_ARTIFACT_DIR"); dir != "" {
		var database string
		if err = q.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil || !strings.HasPrefix(database, "birdtie_owned_migration_") {
			t.Fatal("diagnostic artifact requires own child", err)
		}
		if !filepath.IsAbs(dir) {
			t.Fatal("diagnostic artifact output must be absolute")
		}
		if err = os.MkdirAll(filepath.Join(dir, database), 0700); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, database, label+".json"), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func TestAgentOutboxInitialCaptureNativeCurrentMutations(t *testing.T) {
	ownedMigrationDatabase(t)
	output := captureOutboxInitialLog(t)
	f := newOutboxFixture(t)
	b := f.private.base
	if _, err := b.store.PutOwnMemory(b.ctx, f.private.owner, agentMemoryID(t, f.private), agentMemoryInput("initial.diagnostic.canary")); err != nil {
		t.Fatal(err)
	}
	m := f.create(t)
	for _, kind := range []agentoutbox.EventType{agentoutbox.MomentCreated, agentoutbox.MomentUpdated, agentoutbox.MomentWithdrawn} {
		switch kind {
		case agentoutbox.MomentUpdated:
			input := outboxMomentInput()
			input.Title = "自有当前修改合同"
			var err error
			m, err = b.store.UpdateMomentDraft(b.ctx, b.person.ID, m.ID, m.Revision, input)
			if err != nil {
				t.Fatal(err)
			}
		case agentoutbox.MomentWithdrawn:
			if err := b.store.WithdrawMoment(b.ctx, b.person.ID, m.ID, m.Revision); err != nil {
				t.Fatal(err)
			}
		}
		label := strings.ToLower(string(kind))
		before := initialCaptureSnapshot(t, b.pool, b.ctx, label+"-before")
		tx, err := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		e, now, err := resolveOutboxMomentTx(b.ctx, tx, m.ID, kind)
		if err != nil {
			tx.Rollback(context.Background())
			t.Fatal(err)
		}
		c := reflect.ValueOf(agentoutbox.DiagnoseEnvelope(e, now))
		for i := 0; i < c.NumField(); i++ {
			if !c.Field(i).Bool() {
				tx.Rollback(context.Background())
				t.Fatal("current native predicate failed", c.Type().Field(i).Name)
			}
		}
		r, err := agentoutbox.NewPending(e, now)
		if err != nil || agentoutbox.ValidateEnvelope(e, now) != nil || agentoutbox.ValidateRecord(r) != nil || r.State != agentoutbox.Pending || r.Attempt != 0 || r.Fence != 0 || r.LeaseOwner != "" || r.LeaseUntil != nil || !r.CreatedAt.Equal(now.UTC()) || !r.UpdatedAt.Equal(now.UTC()) || !r.NextAttemptAt.Equal(now.UTC()) || !reflect.DeepEqual(r.Event, e) {
			tx.Rollback(context.Background())
			t.Fatal("original current pending contract", err)
		}
		if err = tx.Rollback(b.ctx); err != nil {
			t.Fatal(err)
		}
		after := initialCaptureSnapshot(t, b.pool, b.ctx, label+"-after")
		if !reflect.DeepEqual(before, after) {
			t.Fatal("current diagnostic changed complete native rows/xmin")
		}
	}
	if output.Len() != 0 {
		t.Fatal("current three native successful writers emitted warning")
	}
	t.Log("CURRENT_THREE_NATIVE_MUTATIONS_ALL32_TRUE=true SUCCESS_WARNING_COUNT=0 COMPLETE_PUBLIC_ROWS_XMIN_PRESERVED=true")
}

func TestAgentOutboxInitialCaptureNativeFutureSourceRejected(t *testing.T) {
	ownedMigrationDatabase(t)
	output := captureOutboxInitialLog(t)
	f := newOutboxFixture(t)
	b := f.private.base
	if _, err := b.store.PutOwnMemory(b.ctx, f.private.owner, agentMemoryID(t, f.private), agentMemoryInput("initial.future.canary")); err != nil {
		t.Fatal(err)
	}
	before := initialCaptureSnapshot(t, b.pool, b.ctx, "future-before")
	tx, err := b.pool.BeginTx(b.ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var id string
	var setupAt, futureAt time.Time
	err = tx.QueryRow(b.ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at), inserted AS (
	INSERT INTO moments(author_account_id,city_id,title,body,time_precision,location_precision,created_at,updated_at)
	SELECT $1,'aberdeen-gb','自有受控未来来源','LOCAL_SYNTHETIC_ONLY','unknown','city',cl.at+interval '1 day',cl.at+interval '1 day' FROM cl RETURNING id,created_at)
	SELECT inserted.id::text,cl.at,inserted.created_at FROM inserted CROSS JOIN cl`, b.person.ID).Scan(&id, &setupAt, &futureAt)
	if err != nil {
		t.Fatal(err)
	}
	e, now, err := resolveOutboxMomentTx(b.ctx, tx, id, agentoutbox.MomentCreated)
	if err != nil {
		t.Fatal(err)
	}
	if !futureAt.Equal(setupAt.Add(24*time.Hour)) || !e.OccurredAt.Equal(futureAt) || !e.ReceivedAt.Equal(now) || !e.OccurredAt.After(e.ReceivedAt) {
		t.Fatal("controlled future native condition not established")
	}
	c := reflect.ValueOf(agentoutbox.DiagnoseEnvelope(e, now))
	for i := 0; i < c.NumField(); i++ {
		if c.Field(i).Bool() != (c.Type().Field(i).Name != "OccurredNotAfterReceived") {
			t.Fatal("unexpected controlled native predicate", c.Type().Field(i).Name)
		}
	}
	if err = agentoutbox.ValidateEnvelope(e, now); err != agentoutbox.ErrInvalid {
		t.Fatal("strict future source accepted")
	}
	r, err := agentoutbox.NewPending(e, now)
	if err != agentoutbox.ErrInvalid || !reflect.DeepEqual(r, agentoutbox.Record{}) {
		t.Fatal("future source returned pending payload")
	}
	txBefore := initialCaptureSnapshot(t, tx, b.ctx, "future-in-tx-before-rejection")
	err = appendMomentOutboxTx(b.ctx, tx, id, agentoutbox.MomentCreated)
	if err != agentoutbox.ErrInvalid {
		t.Fatal("original append failed to reject")
	}
	requireOutboxInitialLog(t, output, "INITIAL_NEW_PENDING", "INVALID", "occurred_not_after_received")
	txAfter := initialCaptureSnapshot(t, tx, b.ctx, "future-in-tx-after-rejection")
	if !reflect.DeepEqual(txBefore, txAfter) {
		t.Fatal("rejected append changed complete native rows/xmin")
	}
	if err = tx.Rollback(b.ctx); err != nil {
		t.Fatal(err)
	}
	after := initialCaptureSnapshot(t, b.pool, b.ctx, "future-after-rollback")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rollback changed complete native rows/xmin")
	}
	t.Log("CONTROLLED_FUTURE_NATIVE_SOURCE_NOT_ORIGINAL_CAUSE=true ZERO_RECORD=true WARNING_COUNT=1 COMPLETE_PUBLIC_ROWS_XMIN_PRESERVED=true")
	t.Log(output.String())
}
