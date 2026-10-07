package postgres

import (
	"context"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Scripted SQL-return unit companion, NOT a native PostgreSQL fixture.
// Filename keeps it in the assigned lease; no database is opened by these tests.
type messagePolicyUnitRow struct {
	values []any
	err    error
}

func (r messagePolicyUnitRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected scripted SQL row shape")
	}
	for i, v := range r.values {
		out := reflect.ValueOf(dest[i]).Elem()
		value := reflect.ValueOf(v)
		if !value.IsValid() {
			out.SetZero()
		} else if value.Type().AssignableTo(out.Type()) {
			out.Set(value)
		} else if value.Type().ConvertibleTo(out.Type()) {
			out.Set(value.Convert(out.Type()))
		} else {
			return errors.New("scripted row type mismatch")
		}
	}
	return nil
}

type messagePolicyUnitTx struct {
	pgx.Tx
	rows    []messagePolicyUnitRow
	queries []string
	execs   []string
	args    [][]any
	execErr error
}

func (tx *messagePolicyUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	tx.queries = append(tx.queries, q)
	if len(tx.rows) == 0 {
		return messagePolicyUnitRow{err: errors.New("unexpected SQL read")}
	}
	r := tx.rows[0]
	tx.rows = tx.rows[1:]
	return r
}
func (tx *messagePolicyUnitTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	tx.execs = append(tx.execs, q)
	tx.args = append(tx.args, args)
	return pgconn.NewCommandTag("INSERT 0 1"), tx.execErr
}
func TestMessagePolicyUnitFinalFenceExpirySourceAndSession(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	actor := identity.Actor{ID: "80000000-0000-4000-8000-000000000001", AccountType: "person"}
	for _, tc := range []struct {
		name    string
		at      time.Time
		token   string
		session bool
		want    error
	}{{"current", now, "original", true, nil}, {"expiry_exact", now.Add(time.Second), "original", true, mp.ErrChanged}, {"expiry_after_wait", now.Add(2 * time.Second), "original", true, mp.ErrChanged}, {"source_changed", now, "new-xmin", true, mp.ErrChanged}, {"source_ABA", now, "same-values-new-xmin", true, mp.ErrChanged}, {"session_revoked", now, "original", false, identity.ErrUnauthorized}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := messageWithCurrent(context.Background(), actor, [32]byte{1}, "")
			tx := &messagePolicyUnitTx{rows: []messagePolicyUnitRow{{values: []any{tc.at, tc.token, tc.session}}}}
			e := finishMessageRoute(ctx, tx, messageRouteFence{actor: actor.ID, peer: "80000000-0000-4000-8000-000000000003", static: "original", until: now.Add(time.Second)})
			if !errors.Is(e, tc.want) {
				t.Fatalf("fence=%v want=%v", e, tc.want)
			}
			if !strings.Contains(tx.queries[0], "clock_timestamp()") || !strings.Contains(tx.queries[0], "xmin") || len(tx.execs) != 0 {
				t.Fatal("final check must be current clock/source and perform no effect")
			}
		})
	}
	tx := &messagePolicyUnitTx{rows: []messagePolicyUnitRow{{err: errors.New("PRIVATE_DATABASE_CANARY")}}}
	if finishMessageRoute(context.Background(), tx, messageRouteFence{}) != mp.ErrUnavailable {
		t.Fatal("DB failure must not leak or report success")
	}
}
func TestMessagePolicyUnitRecipientDecisionStillBindsActualActor(t *testing.T) {
	recipient := identity.Actor{ID: "80000000-0000-4000-8000-000000000003", AccountType: "person"}
	ctx := messageWithCurrent(context.Background(), recipient, [32]byte{1}, "")
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	f := messageRouteFence{actor: "80000000-0000-4000-8000-000000000001", peer: recipient.ID, static: "source", until: now.Add(time.Second)}
	tx := &messagePolicyUnitTx{rows: []messagePolicyUnitRow{{values: []any{now, "source", true}}}}
	if finishMessageRouteForActor(ctx, tx, f, recipient.ID) != nil {
		t.Fatal("recipient is decision actor, not original requester")
	}
	tx = &messagePolicyUnitTx{}
	if finishMessageRoute(ctx, tx, f) != identity.ErrUnauthorized || len(tx.queries) != 0 {
		t.Fatal("cannot relabel recipient as sender to bypass current session")
	}
}
func TestMessagePolicyUnitOwnRecordDefaultExpiredAndBindingChange(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	b := agentPrivateBinding{agentID: "agent", accountID: "owner"}
	for _, tc := range []struct {
		name, agent string
		until       time.Time
		none        bool
		wantStatus  string
		want        error
	}{{"unconfigured", "", now, true, "UNCONFIGURED", nil}, {"active", "agent", now.Add(time.Hour), false, "ACTIVE", nil}, {"expired_exact", "agent", now, false, "EXPIRED", nil}, {"agent_ABA", "old-agent", now.Add(time.Hour), false, "", mp.ErrChanged}} {
		t.Run(tc.name, func(t *testing.T) {
			row := messagePolicyUnitRow{values: []any{tc.agent, int64(2), mp.Screen, now.Add(-time.Hour), tc.until}}
			if tc.none {
				row = messagePolicyUnitRow{err: pgx.ErrNoRows}
			}
			tx := &messagePolicyUnitTx{rows: []messagePolicyUnitRow{row, {values: []any{now}}}}
			r, e := messagePolicyRecord(context.Background(), tx, b)
			if !errors.Is(e, tc.want) || e == nil && r.Status != tc.wantStatus {
				t.Fatal("record must reflect own exact agent and native supplied clock")
			}
			if e == nil && !mp.ValidRecord(r, b.accountID) {
				t.Fatal("returned record malformed")
			}
		})
	}
	if messageOwnError(agentprofile.ErrForbidden) != mp.ErrDenied || messageOwnError(errors.New("secret")) != mp.ErrUnavailable {
		t.Fatal("sanitized private binding failures")
	}
}
