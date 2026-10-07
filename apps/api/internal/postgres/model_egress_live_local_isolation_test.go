package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These unit seams execute no SQL or transport. Root's independent native
// integration verifies the same denial with the real original ledger rows.
type liveLocalIsolationRow func(...any) error

func (r liveLocalIsolationRow) Scan(dest ...any) error { return r(dest...) }

type liveLocalIsolationTx struct {
	pgx.Tx
	kind, state string
	queries     []string
	writes      int
}

func (tx *liveLocalIsolationTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	if strings.Contains(query, "SELECT billing_kind") {
		return liveLocalIsolationRow(func(dest ...any) error { *dest[0].(*string) = tx.kind; return nil })
	}
	if strings.Contains(query, "SELECT operation_id,preview_id") {
		return liveLocalIsolationRow(func(d ...any) error {
			for i, v := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444", "price.test.v1", strings.Repeat("a", 64), tx.state, "CNY"} {
				*d[i].(*string) = v
			}
			*d[8].(*int64) = 196608
			*d[9].(*int64) = 768
			*d[10].(*int64) = 199680
			*d[11].(**int64) = nil
			*d[12].(**int64) = nil
			phase := "LIVE_ATTEMPTED"
			if tx.kind == "LOCAL" {
				phase = "UNAVAILABLE"
			}
			*d[13].(*string) = phase
			*d[14].(*time.Time) = time.Now().Add(-time.Second)
			return nil
		})
	}
	if strings.Contains(query, "FROM sessions CROSS JOIN c") {
		return liveLocalIsolationRow(func(d ...any) error { *d[0].(*bool) = true; *d[1].(*time.Time) = time.Now(); return nil })
	}
	return liveLocalIsolationRow(func(...any) error { return errors.New("unexpected unit query") })
}
func (tx *liveLocalIsolationTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestLiveLocalIsolationUnknownFinishRejectsLiveBeforeAccounting(t *testing.T) {
	for _, kind := range []string{"TOKEN", "CALL"} {
		t.Run(kind, func(t *testing.T) {
			tx := &liveLocalIsolationTx{kind: kind, state: "UNKNOWN"}
			var s *Store
			r, e := s.finishModelAttemptTx(context.Background(), tx, "11111111-1111-4111-8111-111111111111", modelegressbudget.LocalUsage{}, false, "owner", "session")
			if !errors.Is(e, modelegressbudget.ErrDenied) || r.OperationID != "" || tx.writes != 0 {
				t.Fatalf("old LOCAL finish accepted %s UNKNOWN live reservation: error=%v, writes=%d", kind, e, tx.writes)
			}
			for _, query := range tx.queries {
				if strings.Contains(query, "FROM sessions") || strings.Contains(query, "model_budget_accounts") {
					t.Fatal("LOCAL accounting advanced past typed reservation denial")
				}
			}
		})
	}
}

func TestLiveLocalIsolationOriginalLocalUnknownFinishStillAvailable(t *testing.T) {
	tx := &liveLocalIsolationTx{kind: "LOCAL", state: "UNKNOWN"}
	var s *Store
	r, e := s.finishModelAttemptTx(context.Background(), tx, "11111111-1111-4111-8111-111111111111", modelegressbudget.LocalUsage{}, false, "owner", "session")
	if e != nil || r.State != "UNKNOWN" || r.ExecutionStatus != "UNAVAILABLE" || tx.writes != 0 {
		t.Fatalf("original LOCAL unknown accounting changed: error=%v", e)
	}
}

func TestLiveLocalIsolationLocalDispatchBeginAndReleaseDenyEveryLiveKind(t *testing.T) {
	for _, kind := range []string{"TOKEN", "CALL"} {
		for _, phase := range []string{"RESERVED", "IN_FLIGHT", "UNKNOWN", "SETTLED"} {
			t.Run(kind+"_"+phase, func(t *testing.T) {
				var s *Store
				makeTx := func() *liveLocalIsolationTx { return &liveLocalIsolationTx{kind: kind, state: phase} }
				tx := makeTx()
				if r, e := readLocalEgressReservation(context.Background(), tx, "operation", "owner"); !errors.Is(e, modelegressbudget.ErrDenied) || r.OperationID != "" || len(tx.queries) != 1 {
					t.Fatal("LOCAL read retained live metadata")
				}
				tx = makeTx()
				if _, e := s.beginModelAttemptTx(context.Background(), tx, agentevent.Access{}, "operation", nil, agentfeature.Ticket{}, "owner", "session"); !errors.Is(e, modelegressbudget.ErrDenied) || len(tx.queries) != 1 || tx.writes != 0 {
					t.Fatal("LOCAL begin advanced beyond kind check")
				}
				tx = makeTx()
				if _, e := s.checkLocalModelDispatchTx(context.Background(), tx, agentevent.Access{}, "operation", nil, agentfeature.Ticket{}, modelgateway.Request{}, modelegressbudget.LocalAttemptDestination{}, "owner", "session"); !errors.Is(e, modelegressbudget.ErrDenied) || len(tx.queries) != 1 || tx.writes != 0 {
					t.Fatal("LOCAL dispatch advanced beyond kind check")
				}
				tx = makeTx()
				if _, e := s.releaseLocalModelResultTx(context.Background(), tx, agentevent.Access{}, "operation", nil, agentfeature.Ticket{}, modelgateway.Request{}, modelgateway.Result{}, "owner", "session"); !errors.Is(e, modelegressbudget.ErrDenied) || len(tx.queries) != 1 || tx.writes != 0 {
					t.Fatal("LOCAL result release advanced beyond kind check")
				}
				// The shared reader used by Live itself remains unchanged.
				tx = makeTx()
				if r, e := readEgressReservation(context.Background(), tx, "operation", "owner"); e != nil || r.OperationID == "" || r.State != phase {
					t.Fatal("shared native metadata reader incorrectly filtered live")
				}
			})
		}
	}
}

func TestLiveLocalIsolationUnknownBillingKindFailsClosed(t *testing.T) {
	for _, kind := range []string{"", "local", "UNKNOWN", "LOCAL_SYNTHETIC", "TOKEN", "CALL"} {
		tx := &liveLocalIsolationTx{kind: kind}
		if e := requireLocalReservation(context.Background(), tx, "operation", "owner"); !errors.Is(e, modelegressbudget.ErrDenied) {
			t.Fatalf("unrecognized kind %q accepted", kind)
		}
	}
	tx := &liveLocalIsolationTx{kind: "LOCAL", state: "RESERVED"}
	if r, e := readLocalEgressReservation(context.Background(), tx, "operation", "owner"); e != nil || r.State != "RESERVED" || len(tx.queries) != 2 {
		t.Fatal("original LOCAL reservation read changed")
	}
}
