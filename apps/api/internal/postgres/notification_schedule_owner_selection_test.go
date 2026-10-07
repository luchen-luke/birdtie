package postgres

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/jackc/pgx/v5"
)

// Actual production transaction core with a pgx-return spy, not PostgreSQL.
// A precise early no-row retirement has made no slot, delivery, audit or commit.
func TestNotificationScheduleOwnerSelectionEarlyRetirement(t *testing.T) {
	tx := &notificationScheduleUnitTx{messagePolicyUnitTx: messagePolicyUnitTx{
		rows: []messagePolicyUnitRow{{err: pgx.ErrNoRows}},
	}}
	out, err := processNotificationScheduleOwnerTx(context.Background(), tx, notificationScheduleUnitOwner, false)
	if err != nil || out != (ns.RunResult{}) {
		t.Fatalf("retired owner must be a zero-effect skip, not a batch-stopping error: out=%+v err=%v", out, err)
	}
	if len(tx.rowSQL) != 1 || len(tx.execs) != 0 || len(tx.queries) != 1 || tx.commits != 0 {
		t.Fatalf("early retirement must precede any effects: rows=%d execs=%d queries=%d commits=%d", len(tx.rowSQL), len(tx.execs), len(tx.queries), tx.commits)
	}
}

type notificationScheduleOwnerSelectionQuerySpy struct {
	pgxRows   *notificationScheduleUnitRows
	query     string
	args      []any
	queryErr  error
	applyGate bool
}

func (q *notificationScheduleOwnerSelectionQuerySpy) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	q.query, q.args = sql, append([]any{}, args...)
	if q.queryErr != nil {
		return nil, q.queryErr
	}
	if q.applyGate {
		// Query-aware synthetic fixture, not SQL execution: the older owner
		// has no current metadata. Without the actual pre-LIMIT ap predicate
		// its old plan occupies the sole nomination, as the old query allowed.
		owner := notificationScheduleUnitOwner
		if strings.Contains(sql, "JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'") {
			owner = notificationScheduleOwnerSelectionNextOwner
		}
		return &notificationScheduleUnitRows{data: []messagePolicyUnitRow{{values: []any{owner}}}}, nil
	}
	return q.pgxRows, nil
}

const notificationScheduleOwnerSelectionNextOwner = "82000000-0000-4000-8000-000000000003"

func TestNotificationScheduleOwnerSelectionCurrentOwnerGetsBoundedTurn(t *testing.T) {
	q := &notificationScheduleOwnerSelectionQuerySpy{applyGate: true}
	var processed []string
	out, err := processNotificationScheduleOwners(context.Background(), q, func(_ context.Context, owner string) (ns.RunResult, error) {
		processed = append(processed, owner)
		return ns.RunResult{Slots: 1, Delivered: 2}, nil
	}, 1, false)
	if err != nil || out != (ns.RunResult{Owners: 1, Slots: 1, Delivered: 2}) || len(processed) != 1 || processed[0] != notificationScheduleOwnerSelectionNextOwner {
		t.Fatalf("current owner must receive the bounded turn: processed=%v out=%+v err=%v", processed, out, err)
	}
	if len(q.args) != 2 || q.args[0] != 1 || q.args[1] != false {
		t.Fatal("original owner bound/dev gate changed", q.args)
	}
	for _, old := range []string{"p.expires_at>stamp.n", "ss.revoked_at IS NULL", "ss.expires_at>stamp.n", "ss.idle_expires_at>stamp.n", "ss.authentication_method<>'dev_phone'", "NOT EXISTS(SELECT 1 FROM native_notification_schedule_slots", "due.due_at>=p.valid_from", "due.due_at<=stamp.n", "birdtie_notification_schedule_quiet(p.settings,stamp.n)", "birdtie_notification_schedule_quiet(p.settings,due.due_at)", "ORDER BY p.updated_at,p.owner_id LIMIT $1"} {
		if !strings.Contains(q.query, old) {
			t.Error("lost old due/slot predicate", old)
		}
	}
}

func TestNotificationScheduleOwnerSelectionRetirementRaceContinues(t *testing.T) {
	tx := &notificationScheduleUnitTx{messagePolicyUnitTx: messagePolicyUnitTx{rows: []messagePolicyUnitRow{{err: pgx.ErrNoRows}}}}
	q := &notificationScheduleOwnerSelectionQuerySpy{pgxRows: &notificationScheduleUnitRows{data: []messagePolicyUnitRow{{values: []any{notificationScheduleUnitOwner}}, {values: []any{notificationScheduleOwnerSelectionNextOwner}}}}}
	var processed []string
	out, err := processNotificationScheduleOwners(context.Background(), q, func(ctx context.Context, owner string) (ns.RunResult, error) {
		processed = append(processed, owner)
		if owner == notificationScheduleUnitOwner {
			return processNotificationScheduleOwnerTx(ctx, tx, owner, false)
		}
		return ns.RunResult{Slots: 1, Delivered: 1}, nil
	}, 2, false)
	if err != nil || out != (ns.RunResult{Owners: 2, Slots: 1, Delivered: 1}) || len(processed) != 2 || processed[1] != notificationScheduleOwnerSelectionNextOwner {
		t.Fatalf("early zero-effect retirement must not abort the next owner: processed=%v out=%+v err=%v", processed, out, err)
	}
	if len(tx.rowSQL) != 1 || len(tx.execs) != 0 || tx.commits != 0 {
		t.Fatal("retirement made effects", tx.operations, tx.commits)
	}
}

func TestNotificationScheduleOwnerSelectionUnknownStillStops(t *testing.T) {
	for _, tc := range []struct {
		name   string
		makeTx func() *notificationScheduleUnitTx
		want   error
	}{
		{"scan_account_mismatch", func() *notificationScheduleUnitTx {
			return &notificationScheduleUnitTx{messagePolicyUnitTx: messagePolicyUnitTx{rows: []messagePolicyUnitRow{{values: []any{notificationScheduleOwnerSelectionNextOwner}}}}}
		}, ns.ErrDenied},
		{"account_read_infrastructure_error", func() *notificationScheduleUnitTx {
			return &notificationScheduleUnitTx{messagePolicyUnitTx: messagePolicyUnitTx{rows: []messagePolicyUnitRow{{err: errors.New("synthetic read failure")}}}}
		}, ns.ErrUnavailable},
		{"commit_outcome_unknown", func() *notificationScheduleUnitTx {
			now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
			return &notificationScheduleUnitTx{
				messagePolicyUnitTx: messagePolicyUnitTx{rows: []messagePolicyUnitRow{
					{values: []any{notificationScheduleUnitOwner}}, notificationScheduleUnitPolicyRows(now, true, nil),
					{values: []any{"source-xmin", "session", notificationScheduleUnitAgent, now}},
					{values: []any{"slot"}}, {values: []any{0}}, {values: []any{true}},
				}},
				candidateRows: &notificationScheduleUnitRows{}, commitErr: errors.New("synthetic commit outcome unknown"),
			}
		}, ns.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := tc.makeTx()
			q := &notificationScheduleOwnerSelectionQuerySpy{pgxRows: &notificationScheduleUnitRows{data: []messagePolicyUnitRow{{values: []any{notificationScheduleUnitOwner}}, {values: []any{notificationScheduleOwnerSelectionNextOwner}}}}}
			calls := 0
			out, err := processNotificationScheduleOwners(context.Background(), q, func(ctx context.Context, owner string) (ns.RunResult, error) {
				calls++
				return processNotificationScheduleOwnerTx(ctx, tx, owner, false)
			}, 2, false)
			if !errors.Is(err, tc.want) || out != (ns.RunResult{}) || calls != 1 {
				t.Fatalf("error must stop; zero struct with error is UNKNOWN, not proven zero effects: out=%+v calls=%d err=%v", out, calls, err)
			}
			if tc.name == "commit_outcome_unknown" && tx.commits != 1 {
				t.Fatal("fixture did not reach original Commit", tx.commits)
			}
		})
	}
}

// UNIT_STATIC: inspect the actual nomination SQL, not PostgreSQL execution.
// This protects the predicate placement before the bounded ORDER/LIMIT.
func TestNotificationScheduleOwnerSelectionEligibilityBeforeLimit(t *testing.T) {
	raw, err := os.ReadFile("notification_schedule_delivery.go")
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "notification_schedule_delivery.go", raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sql string
	ast.Inspect(f, func(n ast.Node) bool {
		v, ok := n.(*ast.BasicLit)
		if !ok || v.Kind != token.STRING {
			return true
		}
		s, e := strconv.Unquote(v.Value)
		if e == nil && strings.Contains(s, "SELECT p.owner_id FROM native_notification_schedules p") {
			if sql != "" {
				t.Fatal("nomination SQL must have one actual production definition")
			}
			sql = strings.Join(strings.Fields(s), " ")
		}
		return true
	})
	cut := strings.Index(sql, "ORDER BY p.updated_at,p.owner_id LIMIT $1")
	if cut < 0 {
		t.Fatal("original stable order and bound missing")
	}
	prefix := sql[:cut]
	for _, required := range []string{
		"JOIN accounts a ON a.id=p.owner_id AND a.account_type='person' AND a.status='active'",
		"JOIN agents ag ON ag.id=p.agent_id AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'",
		"JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'",
		"JOIN sessions ss ON ss.id=p.session_id AND ss.account_id=a.id",
	} {
		if !strings.Contains(prefix, required) {
			t.Errorf("ineligible owner can occupy LIMIT: missing %s", required)
		}
	}
}
