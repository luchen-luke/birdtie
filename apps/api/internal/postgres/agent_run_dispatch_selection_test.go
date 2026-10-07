package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentworker"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type dispatchUnitTx struct {
	pgx.Tx
	at                        time.Time
	active                    int
	execErr, queryErr, rowErr error
	heads                     []ar.DispatchTenant
	rows                      *dispatchUnitRows
	commands                  []string
	args                      [][]any
}

func TestAgentRunDispatchUnitMapperPreservesConsumerSignals(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"busy", ar.ErrDispatchBusy, ar.ErrDispatchBusy},
		{"wrapped busy", fmt.Errorf("internal dispatch: %w", ar.ErrDispatchBusy), ar.ErrDispatchBusy},
		{"empty", ar.ErrNotFound, ar.ErrNotFound},
		{"wrapped empty", fmt.Errorf("internal dispatch: %w", ar.ErrNotFound), ar.ErrNotFound},
		{"invalid precedence", errors.Join(ar.ErrDispatchBusy, ar.ErrInvalid), ar.ErrInvalid},
		{"denied precedence", errors.Join(ar.ErrNotFound, ar.ErrDenied), ar.ErrDenied},
		{"no rows remains denied", pgx.ErrNoRows, ar.ErrDenied},
		{"no rows precedence", errors.Join(ar.ErrNotFound, pgx.ErrNoRows), ar.ErrDenied},
		{"expired precedence", errors.Join(ar.ErrDispatchBusy, ar.ErrExpired), ar.ErrExpired},
		{"conflict precedence", errors.Join(ar.ErrNotFound, ar.ErrConflict), ar.ErrConflict},
		{"lock conflict precedence", errors.Join(ar.ErrDispatchBusy, &pgconn.PgError{Code: "55P03"}), ar.ErrConflict},
		{"unique conflict precedence", errors.Join(ar.ErrNotFound, &pgconn.PgError{Code: "23505"}), ar.ErrConflict},
		{"deadlock conflict precedence", errors.Join(ar.ErrDispatchBusy, &pgconn.PgError{Code: "40P01"}), ar.ErrConflict},
		{"unknown unavailable", errors.New("PRIVATE_DISPATCH_CANARY"), ar.ErrUnavailable},
		{"unknown postgres unavailable", &pgconn.PgError{Code: "XX000", Message: "PRIVATE_DISPATCH_CANARY"}, ar.ErrUnavailable},
		{"nil unchanged", nil, ar.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runError(tc.err)
			if got != tc.want || strings.Contains(got.Error(), "CANARY") {
				t.Fatalf("consumer signal must be sanitized and preserve old precedence: got %v, want %v", got, tc.want)
			}
		})
	}
}

// Compose the production selector, its claim error mapper and the original
// finite worker. The pgx transaction below is a unit spy, not a native database.
type dispatchConsumerUnitBackend struct {
	tx                        *dispatchUnitTx
	claims, executes, expires int
}

func (b *dispatchConsumerUnitBackend) ExpireAgentRuns(context.Context, int) (int64, error) {
	b.expires++
	return 0, nil
}
func (b *dispatchConsumerUnitBackend) ClaimAgentRun(ctx context.Context, _ string) (ar.Claim, error) {
	b.claims++
	_, e := selectAgentRunDispatch(ctx, b.tx)
	if e != nil {
		return ar.Claim{}, runError(e)
	}
	return ar.Claim{}, nil
}
func (b *dispatchConsumerUnitBackend) ExecuteAgentRun(context.Context, ar.Claim) (ar.Record, error) {
	b.executes++
	return ar.Record{}, ar.ErrUnavailable
}

func TestAgentRunDispatchUnitSelectorMapperFeedsOriginalWorker(t *testing.T) {
	owner := "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name                   string
		edit                   func(*dispatchUnitTx)
		throttled, unavailable bool
		commands               int
	}{
		{"global cap", func(x *dispatchUnitTx) { x.active = ar.MaxConcurrentRuns }, true, false, 2},
		{"owner cap", func(x *dispatchUnitTx) {
			x.active = 1
			x.heads = []ar.DispatchTenant{{Owner: owner, Active: 1, NextDue: x.at}}
		}, true, false, 3},
		{"chosen row disappeared", func(x *dispatchUnitTx) {
			x.heads = []ar.DispatchTenant{{Owner: owner, NextDue: x.at}}
		}, true, false, 4},
		{"genuine empty", func(*dispatchUnitTx) {}, false, false, 3},
		{"lock fault", func(x *dispatchUnitTx) { x.execErr = errors.New("PRIVATE_LOCK_CANARY") }, false, true, 1},
		{"clock fault", func(x *dispatchUnitTx) { x.rowErr = errors.New("PRIVATE_CLOCK_CANARY") }, false, true, 2},
		{"query fault", func(x *dispatchUnitTx) { x.queryErr = errors.New("PRIVATE_QUERY_CANARY") }, false, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := &dispatchUnitTx{at: time.Now().UTC()}
			tc.edit(x)
			b := &dispatchConsumerUnitBackend{tx: x}
			r, e := agentenrichmentworker.RunOnce(context.Background(), b, "consumer-unit", 25)
			if e != nil || r.Throttled != tc.throttled || r.Unavailable != tc.unavailable ||
				b.expires != 1 || b.claims != 1 || b.executes != 0 || len(x.commands) != tc.commands ||
				r.Claimed != 0 || r.Succeeded != 0 || r.Unknown != 0 || r.Failed != 0 {
				t.Fatalf("original worker must stop once without executing or masking control result: result=%+v error=%v backend=%+v calls=%d", r, e, b, len(x.commands))
			}
		})
	}
}

func (x *dispatchUnitTx) Exec(_ context.Context, q string, a ...any) (pgconn.CommandTag, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, a)
	return pgconn.CommandTag{}, x.execErr
}
func (x *dispatchUnitTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	x.commands = append(x.commands, q)
	x.args = append(x.args, a)
	if q == agentRunDispatchCountSQL {
		return dispatchUnitRow{fn: func(d []any) error {
			if x.rowErr != nil {
				return x.rowErr
			}
			*d[0].(*time.Time) = x.at
			*d[1].(*int) = x.active
			return nil
		}}
	}
	return dispatchUnitRow{fn: func([]any) error { return pgx.ErrNoRows }}
}
func (x *dispatchUnitTx) Query(_ context.Context, q string, a ...any) (pgx.Rows, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, a)
	x.rows = &dispatchUnitRows{heads: x.heads}
	return x.rows, x.queryErr
}

type dispatchUnitRow struct{ fn func([]any) error }

func (r dispatchUnitRow) Scan(d ...any) error { return r.fn(d) }

type dispatchUnitRows struct {
	pgx.Rows
	heads  []ar.DispatchTenant
	index  int
	closed bool
}

func (r *dispatchUnitRows) Next() bool { return r.index < len(r.heads) }
func (r *dispatchUnitRows) Scan(d ...any) error {
	h := r.heads[r.index]
	r.index++
	*d[0].(*string) = h.Owner
	*d[1].(*int) = h.Active
	*d[2].(**time.Time) = h.LastServed
	*d[3].(*time.Time) = h.NextDue
	return nil
}
func (r *dispatchUnitRows) Close()     { r.closed = true }
func (r *dispatchUnitRows) Err() error { return nil }

func TestAgentRunDispatchUnitActualSelectionLocksBeforeMetadata(t *testing.T) {
	now := time.Now().UTC()
	last := now.Add(-time.Second)
	a := "11111111-1111-4111-8111-111111111111"
	b := "22222222-2222-4222-8222-222222222222"
	x := &dispatchUnitTx{at: now, heads: []ar.DispatchTenant{{Owner: a, LastServed: &last, NextDue: now.Add(-time.Minute)}, {Owner: b, NextDue: now}}}
	_, e := selectAgentRunDispatch(context.Background(), x)
	if !errors.Is(e, ar.ErrDispatchBusy) {
		t.Fatal("chosen row disappeared, not global empty", e)
	}
	if len(x.commands) != 4 || x.commands[0] != agentRunDispatchLockSQL || x.commands[1] != agentRunDispatchCountSQL || x.commands[2] != agentRunDispatchHeadsSQL {
		t.Fatal(x.commands)
	}
	if len(x.args[2]) != 2 || x.args[2][0] != now || x.args[2][1] != ar.MaxDispatchTenantHeads || len(x.args[3]) != 1 || x.args[3][0] != b {
		t.Fatal("least served native owner bound to original claim", x.args)
	}
	if !strings.Contains(x.commands[3], "r.owner_id=$1") || !strings.Contains(x.commands[3], "FOR UPDATE SKIP LOCKED") || !strings.Contains(x.commands[3], "r.deadline>clock_timestamp()") || !x.rows.closed {
		t.Fatal(x.commands)
	}
}
func TestAgentRunDispatchUnitActualSelectionBoundedAndFaultClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*dispatchUnitTx)
		want  error
		calls int
	}{
		{"global capacity", func(x *dispatchUnitTx) { x.active = ar.MaxConcurrentRuns }, ar.ErrDispatchBusy, 2},
		{"empty", func(*dispatchUnitTx) {}, ar.ErrNotFound, 3},
		{"lock unavailable", func(x *dispatchUnitTx) { x.execErr = errors.New("PRIVATE_PROVIDER_CANARY") }, ar.ErrUnavailable, 1},
		{"clock unavailable", func(x *dispatchUnitTx) { x.rowErr = errors.New("PRIVATE_SESSION_CANARY") }, ar.ErrUnavailable, 2},
		{"query unavailable", func(x *dispatchUnitTx) { x.queryErr = errors.New("PRIVATE_SOURCE_CANARY") }, ar.ErrUnavailable, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := &dispatchUnitTx{at: time.Now().UTC()}
			tc.edit(x)
			_, e := selectAgentRunDispatch(context.Background(), x)
			if !errors.Is(e, tc.want) || len(x.commands) != tc.calls {
				t.Fatal(e, x.commands)
			}
			if strings.Contains(e.Error(), "CANARY") {
				t.Fatal("raw error leaked", e)
			}
		})
	}
}
