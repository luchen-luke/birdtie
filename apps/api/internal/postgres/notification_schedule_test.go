package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"os"
	"strings"
	"testing"
	"time"
)

// SQL-return spies only: none of these opens PostgreSQL or proves persistence,
// locking, migration validity or that supplied current=true is an authority.
const notificationScheduleUnitAgent = "82000000-0000-4000-8000-000000000001"
const notificationScheduleUnitOwner = "82000000-0000-4000-8000-000000000002"

func notificationScheduleUnitSettings() ns.Settings {
	return ns.Settings{Enabled: true, TimeZone: "UTC", LocalMinute: 600, GapPolicy: ns.GapSkip, FoldPolicy: ns.FoldEarlierOnce, MaxContactsPerDay: 3, Categories: []agentnotification.Category{agentnotification.CategorySocial, agentnotification.CategoryActivity}}
}
func notificationScheduleUnitPolicyRows(now time.Time, session bool, edit func(*ns.Settings)) messagePolicyUnitRow {
	s := notificationScheduleUnitSettings()
	if edit != nil {
		edit(&s)
	}
	raw, _ := json.Marshal(s)
	from := now.Add(-12 * time.Hour)
	until := now.Add(24 * time.Hour)
	return messagePolicyUnitRow{values: []any{uint64(1), notificationScheduleUnitAgent, raw, &from, &until, &from, session, now}}
}
func TestNotificationScheduleUnitNativeScanAndOwnErrors(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		session bool
		edit    func(*ns.Settings)
		want    string
	}{{"active", true, nil, "ACTIVE"}, {"off", true, func(s *ns.Settings) { s.Enabled = false }, "DISABLED"}, {"revoked_origin", false, nil, "PAUSED_SESSION"}} {
		t.Run(tc.name, func(t *testing.T) {
			p, e := scanNotificationSchedule(notificationScheduleUnitPolicyRows(now, tc.session, tc.edit))
			if e != nil || p.Status != tc.want {
				t.Fatal(p.Status, e)
			}
		})
	}
	row := notificationScheduleUnitPolicyRows(now, true, nil)
	row.values[2] = []byte(`{"unknown":true}`)
	if _, e := scanNotificationSchedule(row); e != ns.ErrUnavailable {
		t.Fatal("corrupt settings")
	}
	for _, tc := range []struct{ e, want error }{{agentprofile.ErrForbidden, ns.ErrDenied}, {agentprofile.ErrNotFound, ns.ErrDenied}, {ns.ErrChanged, ns.ErrChanged}, {ns.ErrInvalid, ns.ErrInvalid}, {errors.New("PRIVATE_ERROR_CANARY"), ns.ErrUnavailable}, {&pgconn.PgError{Code: "40P01"}, ns.ErrChanged}} {
		if e := notificationScheduleError(tc.e); e != tc.want {
			t.Fatal(e)
		}
	}
	var store *Store
	if _, e := store.GetOwnNotificationSchedule(context.Background(), agentprofile.PrivateAccess{}); e != ns.ErrUnavailable {
		t.Fatal("nil store")
	}
}
func TestNotificationScheduleUnitEditFinalFence(t *testing.T) {
	for _, tc := range []struct {
		name             string
		session, current bool
		want             error
	}{{"current", true, true, nil}, {"session_expiry_or_revoke", false, true, ns.ErrDenied}, {"schedule_CAS_or_clock_expiry", true, false, ns.ErrChanged}, {"unknown", false, false, ns.ErrDenied}} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &messagePolicyUnitTx{rows: []messagePolicyUnitRow{{values: []any{tc.session, tc.current}}}}
			e := finishNotificationScheduleEdit(context.Background(), tx, agentPrivateBinding{"session", notificationScheduleUnitOwner, notificationScheduleUnitAgent}, 2, false)
			if !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
			sql := tx.queries[0]
			if !strings.Contains(sql, "clock_timestamp()") || !strings.Contains(sql, "session_id=$3") || !strings.Contains(sql, "version=$4") || len(tx.execs) != 0 {
				t.Fatal("lost last native clock/version/session point")
			}
		})
	}
}
func TestNotificationScheduleUnitMigrationContractOnly(t *testing.T) {
	up, e := os.ReadFile("../../migrations/099_native_notification_schedules.sql")
	if e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile("../../migrations/099_native_notification_schedules.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, text := range []string{"UNIQUE(owner_id,local_date)", "interval '24 hours'", "ON DELETE SET NULL", "slot.schedule_token", "p.xmin::text", "birdtie_native_notification_visible_v098", "birdtie_native_notification_source", "min(candidate)", "ss.idle_expires_at>stamp.n"} {
		if !strings.Contains(string(up), text) {
			t.Fatal("missing source contract", text)
		}
	}
	if strings.Contains(string(up), "INSERT INTO agent_tasks") || strings.Contains(string(up), "consent_grants") || strings.Contains(string(up), "UserQuery") {
		t.Fatal("no default inference or authority")
	}
	if !strings.Contains(string(down), "down refuses to erase") || !strings.Contains(string(down), "RENAME TO birdtie_native_notification_visible") {
		t.Fatal("down must preserve used history and old visibility")
	}
}

type notificationScheduleUnitRows struct {
	pgx.Rows
	data []messagePolicyUnitRow
	pos  int
	err  error
}

func (r *notificationScheduleUnitRows) Next() bool {
	if r.pos < len(r.data) {
		r.pos++
		return true
	}
	return false
}
func (r *notificationScheduleUnitRows) Scan(v ...any) error { return r.data[r.pos-1].Scan(v...) }
func (r *notificationScheduleUnitRows) Err() error          { return r.err }
func (r *notificationScheduleUnitRows) Close()              {}

type notificationScheduleUnitTx struct {
	messagePolicyUnitTx
	candidateRows *notificationScheduleUnitRows
	commits       int
	commitErr     error
	queryArgs     [][]any
	rowSQL        []string
	operations    []string
}

func (tx *notificationScheduleUnitTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	tx.operations = append(tx.operations, "ROW:"+q)
	tx.queryArgs = append(tx.queryArgs, args)
	tx.rowSQL = append(tx.rowSQL, q)
	return tx.messagePolicyUnitTx.QueryRow(ctx, q, args...)
}
func (tx *notificationScheduleUnitTx) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	tx.operations = append(tx.operations, "EXEC:"+q)
	return tx.messagePolicyUnitTx.Exec(ctx, q, args...)
}

func (tx *notificationScheduleUnitTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	tx.queries = append(tx.queries, q)
	tx.args = append(tx.args, args)
	if tx.candidateRows == nil {
		return nil, errors.New("unexpected candidates")
	}
	return tx.candidateRows, nil
}
func (tx *notificationScheduleUnitTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}
