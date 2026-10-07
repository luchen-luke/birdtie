package postgres

import (
	"context"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
	"time"
)

const decisionUnitOwner = "11111111-1111-4111-8111-111111111111"
const decisionUnitPeer = "22222222-2222-4222-8222-222222222222"
const decisionUnitRequest = "33333333-3333-4333-8333-333333333333"
const decisionUnitOp = "44444444-4444-4444-8444-444444444444"

// Executes the actual native Tx helper against scripted SQL rows, not PG.
type decisionOperationUnitTx struct {
	pgx.Tx
	commands          []string
	state, scope      string
	now               time.Time
	prior             *connection.DecisionOperationReceipt
	finalSession      bool
	finalToken        string
	effects, receipts int
	receiptZero       bool
}
type decisionOperationAccounts struct {
	pgx.Rows
	n int
}

func (a *decisionOperationAccounts) Next() bool { a.n++; return a.n <= 2 }
func (a *decisionOperationAccounts) Close()     {}
func (a *decisionOperationAccounts) Err() error { return nil }
func (x *decisionOperationUnitTx) Query(_ context.Context, q string, a ...any) (pgx.Rows, error) {
	x.commands = append(x.commands, q)
	return &decisionOperationAccounts{}, nil
}
func (x *decisionOperationUnitTx) Exec(_ context.Context, q string, a ...any) (pgconn.CommandTag, error) {
	x.commands = append(x.commands, q)
	if strings.Contains(q, "UPDATE connection_requests") || strings.Contains(q, "INSERT INTO person_ties") || strings.Contains(q, "INSERT INTO audit_events") {
		x.effects++
	}
	if strings.Contains(q, "INSERT INTO connection_request_decision_receipts") {
		x.receipts++
		if x.receiptZero {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (x *decisionOperationUnitTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	x.commands = append(x.commands, q)
	var v []any
	switch {
	case q == humanMomentCurrentSession:
		v = []any{true}
	case strings.HasPrefix(q, "SELECT id FROM sessions"):
		v = []any{decisionUnitOwner}
	case strings.HasPrefix(q, "SELECT sender_account_id,recipient_account_id"):
		v = []any{decisionUnitPeer, decisionUnitOwner}
	case q == messagePolicyStaticSQL:
		v = []any{"static"}
	case strings.Contains(q, "FROM n LEFT JOIN agent_message_request_policies"):
		v = []any{x.now, false, false, true, (*string)(nil), (*time.Time)(nil), (*time.Time)(nil), true}
	case strings.HasPrefix(q, "SELECT encode(sha256"):
		v = []any{strings.Repeat("a", 64)}
	case strings.Contains(q, "COALESCE(city_id,'')"):
		v = []any{decisionUnitPeer, decisionUnitOwner, "", "SYNTHETIC_NOTE", x.scope, x.state, x.now.Add(time.Hour), x.now.Add(-time.Hour)}
	case strings.Contains(q, "FROM connection_request_decision_receipts"):
		if x.prior == nil {
			return messagePolicyUnitRow{err: pgx.ErrNoRows}
		}
		r := x.prior
		v = []any{r.OwnerID, r.RequestID, r.OperationID, r.RequestDigest, r.Action, r.Scope, r.Status, r.State, r.Reason, r.RecordedAt}
	case q == "SELECT clock_timestamp()":
		v = []any{x.now}
	case strings.HasPrefix(q, "SELECT EXISTS ("):
		v = []any{true}
	case q == "SHOW transaction_isolation":
		v = []any{"read committed"}
	case strings.Contains(q, "INSERT INTO native_notification_decisions"):
		return messagePolicyUnitRow{err: pgx.ErrNoRows}
	case strings.Contains(q, "INSERT INTO conversations"):
		x.effects++
		v = []any{decisionUnitOp}
	case strings.Contains(q, "$6 AND a.account_type='person'"):
		v = []any{x.now, x.finalToken, x.finalSession}
	default:
		return messagePolicyUnitRow{err: errors.New("unexpected SQL")}
	}
	return messagePolicyUnitRow{values: v}
}
func decisionUnitFixture() (*decisionOperationUnitTx, context.Context, *connectionDecisionOperation) {
	now := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	d, _ := connection.DecisionDigest(decisionUnitOwner, decisionUnitRequest, "accept")
	return &decisionOperationUnitTx{now: now, state: "pending", scope: "friend", finalSession: true, finalToken: "static"}, messageWithCurrent(context.Background(), identity.Actor{ID: decisionUnitOwner, AccountType: "person"}, [32]byte{1}, ""), &connectionDecisionOperation{id: decisionUnitOp, digest: d}
}
func TestConnectionDecisionOperationOriginalWriterCausalBranches(t *testing.T) {
	for _, tc := range []string{"friend", "conversation", "replay", "no_effect", "digest_changed", "wrong_actor", "late_session", "source_ABA", "receipt_insert_zero"} {
		t.Run(tc, func(t *testing.T) {
			tx, ctx, op := decisionUnitFixture()
			want := error(nil)
			switch tc {
			case "conversation":
				tx.scope = "conversation"
			case "replay", "digest_changed":
				tx.state = "accepted"
				tx.prior = &connection.DecisionOperationReceipt{SchemaVersion: connection.DecisionOperationSchema, OwnerID: decisionUnitOwner, RequestID: decisionUnitRequest, OperationID: decisionUnitOp, RequestDigest: op.digest, Action: "accept", Scope: "friend", Status: "COMMITTED", State: "accepted", RecordedAt: tx.now.Add(-time.Hour)}
				if tc == "digest_changed" {
					tx.prior.RequestDigest = strings.Repeat("b", 64)
					want = connection.ErrOperationChanged
				}
			case "no_effect":
				tx.state = "declined"
			case "wrong_actor":
				ctx = messageWithCurrent(context.Background(), identity.Actor{ID: decisionUnitPeer, AccountType: "person"}, [32]byte{1}, "")
				want = identity.ErrUnauthorized
			case "late_session":
				tx.finalSession = false
				want = identity.ErrUnauthorized
			case "source_ABA":
				tx.finalToken = "new-xmin"
				want = mp.ErrChanged
			case "receipt_insert_zero":
				tx.receiptZero = true
				want = connection.ErrOperationUnavailable
			}
			_, e := decideRequestInTx(ctx, tx, decisionUnitOwner, decisionUnitRequest, "accept", op)
			if !errors.Is(e, want) {
				t.Fatalf("%s: %v want %v\n%v", tc, e, want, tx.commands)
			}
			if tc == "replay" && (tx.effects != 0 || tx.receipts != 0 || op.receipt.Status != "COMMITTED") {
				t.Fatal("replay repeated effects")
			}
			if tc == "no_effect" && (tx.effects != 0 || tx.receipts != 1 || op.receipt.Reason != "ALREADY_DECIDED") {
				t.Fatal("no-effect requires actual terminal source")
			}
			if tc == "wrong_actor" && len(tx.commands) != 0 {
				t.Fatal("private digest read before identity")
			}
			if e == nil && tc == "friend" && (tx.effects != 3 || tx.receipts != 1) {
				t.Fatal("original request+tie+audit and one receipt", tx.effects)
			}
		})
	}
}
func TestConnectionDecisionOperationPriorReadIsOwnerScopedAndMissingUnknown(t *testing.T) {
	tx, ctx, _ := decisionUnitFixture()
	_, e := readConnectionDecisionReceipt(ctx, tx, decisionUnitOwner, decisionUnitOp)
	if !errors.Is(e, connection.ErrNotFound) || !strings.Contains(tx.commands[0], "WHERE owner_account_id=$1 AND operation_id=$2") {
		t.Fatal("missing receipt is not no effect")
	}
}

func TestConnectionDecisionOperationOwnHistoryReadFinalSession(t *testing.T) {
	for _, name := range []string{"current_history", "missing_unknown", "late_session"} {
		t.Run(name, func(t *testing.T) {
			tx, ctx, op := decisionUnitFixture()
			tx.state = "accepted"
			tx.prior = &connection.DecisionOperationReceipt{SchemaVersion: connection.DecisionOperationSchema, OwnerID: decisionUnitOwner, RequestID: decisionUnitRequest, OperationID: decisionUnitOp, RequestDigest: op.digest, Action: "accept", Scope: "friend", Status: "COMMITTED", State: "accepted", RecordedAt: tx.now.Add(-time.Hour)}
			want := error(nil)
			if name == "missing_unknown" {
				tx.prior = nil
				want = connection.ErrNotFound
			}
			if name == "late_session" {
				tx.finalSession = false
				want = identity.ErrUnauthorized
			}
			r, e := readRequestDecisionOperationInTx(ctx, tx, decisionUnitOwner, decisionUnitRequest, decisionUnitOp)
			if !errors.Is(e, want) || e == nil && r.RecordedAt != tx.now.Add(-time.Hour) {
				t.Fatal("current-session historical result", e, r)
			}
			if tx.effects != 0 || tx.receipts != 0 {
				t.Fatal("read/reconciliation repeated original effect")
			}
		})
	}
}
