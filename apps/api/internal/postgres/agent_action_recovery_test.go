package postgres

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/jackc/pgx/v5"
)

const approvalAddressTestID = "11111111-1111-4111-8111-111111111111"
const approvalAddressTestOwner = "22222222-2222-4222-8222-222222222222"
const approvalAddressTestDispatch = "33333333-3333-4333-8333-333333333333"

type approvalAddressTestRow func(...any) error

func (r approvalAddressTestRow) Scan(v ...any) error { return r(v...) }

type approvalAddressTestTx struct {
	pgx.Tx
	calls int
	query string
	args  []any
	row   pgx.Row
}

func (s *approvalAddressTestTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	s.calls++
	s.query = q
	s.args = append([]any(nil), a...)
	return s.row
}
func approvalAddressRow(id string, e error) pgx.Row {
	return approvalAddressTestRow(func(v ...any) error {
		if e != nil {
			return e
		}
		*(v[0].(*string)) = id
		return nil
	})
}

func TestActionRecoveryNativeApprovalAddressUsesActualOwnerPredicate(t *testing.T) {
	tx := &approvalAddressTestTx{row: approvalAddressRow(approvalAddressTestDispatch, nil)}
	id, e := actionApprovalDispatchAddress(context.Background(), tx, approvalAddressTestID, approvalAddressTestOwner)
	if e != nil || id != approvalAddressTestDispatch || tx.calls != 1 {
		t.Fatal(e, id, tx.calls)
	}
	if tx.query != `SELECT id FROM agent_action_dispatches WHERE approval_id=$1 AND owner_id=$2` || len(tx.args) != 2 || tx.args[0] != approvalAddressTestID || tx.args[1] != approvalAddressTestOwner {
		t.Fatal("owner-scoped native address changed", tx.query, tx.args)
	}
}

func TestActionRecoveryNativeMissingOrFailedLookupRemainsUnknown(t *testing.T) {
	for _, x := range []struct {
		name, id string
		e        error
	}{{"no committed dispatch", "", pgx.ErrNoRows}, {"transport failure", "", errors.New("lost native reply")}, {"corrupt address", "unknown", nil}} {
		t.Run(x.name, func(t *testing.T) {
			tx := &approvalAddressTestTx{row: approvalAddressRow(x.id, x.e)}
			id, e := actionApprovalDispatchAddress(context.Background(), tx, approvalAddressTestID, approvalAddressTestOwner)
			if !errors.Is(e, aa.ErrUnknown) || id != "" || tx.calls != 1 {
				t.Fatal("absence must not be NO_EFFECT/retry", e, id, tx.calls)
			}
		})
	}
}

func TestActionRecoveryNativeInvalidOrCancelledAddressDoesNotRead(t *testing.T) {
	for _, x := range []struct{ name, approval, owner string }{{"approval", "confirmed", approvalAddressTestOwner}, {"owner", approvalAddressTestID, "organization"}} {
		t.Run(x.name, func(t *testing.T) {
			tx := &approvalAddressTestTx{}
			id, e := actionApprovalDispatchAddress(context.Background(), tx, x.approval, x.owner)
			if !errors.Is(e, aa.ErrInvalid) || id != "" || tx.calls != 0 {
				t.Fatal(e, id, tx.calls)
			}
		})
	}
	tx := &approvalAddressTestTx{}
	id, e := actionApprovalDispatchAddress(nil, tx, approvalAddressTestID, approvalAddressTestOwner)
	if !errors.Is(e, aa.ErrInvalid) || id != "" || tx.calls != 0 {
		t.Fatal("nil context", e)
	}
	c, cancel := context.WithCancel(context.Background())
	cancel()
	id, e = actionApprovalDispatchAddress(c, tx, approvalAddressTestID, approvalAddressTestOwner)
	if !errors.Is(e, aa.ErrUnknown) || id != "" || tx.calls != 0 {
		t.Fatal("cancelled context", e)
	}
}

func TestActionRecoveryNativeCancelledResponseDoesNotPublishAddress(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx := &approvalAddressTestTx{row: approvalAddressTestRow(func(v ...any) error { *(v[0].(*string)) = approvalAddressTestDispatch; cancel(); return nil })}
	id, e := actionApprovalDispatchAddress(c, tx, approvalAddressTestID, approvalAddressTestOwner)
	if !errors.Is(e, aa.ErrUnknown) || id != "" || tx.calls != 1 {
		t.Fatal("late result published", e, id)
	}
}

func TestActionRecoveryNativeApprovalUsesOriginalReconciliationFence(t *testing.T) {
	// Static wiring unit, not a native PostgreSQL or concurrency proof.
	raw, e := os.ReadFile("agent_action_reconciliation.go")
	if e != nil {
		t.Fatal(e)
	}
	text := string(raw)
	start := strings.Index(text, "func (s *Store) actionReadDispatchAddress(")
	if start < 0 {
		t.Fatal("original address wrapper absent")
	}
	body := text[start:]
	previous := -1
	for _, call := range []string{"s.egressOwner(", "actionApprovalDispatchAddress(", "actionDispatchTx(", "egressFinish(", "actionCommit("} {
		pos := strings.Index(body, call)
		if pos <= previous {
			t.Fatal("owner/source/final session order changed", call)
		}
		previous = pos
	}
	for _, original := range []string{"mode == 1 && d.State != aa.Succeeded && d.State != aa.NoEffect", "WHERE dispatch_id=$1 AND owner_id=$2 AND effect_key=$3", "state='NO_EFFECT',fence=fence+1", "state='SUCCEEDED',effect_id=$2,applied_at=$3,fence=fence+1"} {
		if !strings.Contains(body, original) {
			t.Fatal("original exclusive sandbox receipt/fence changed", original)
		}
	}
	f, e := parser.ParseFile(token.NewFileSet(), "agent_action_recovery.go", nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	matched := false
	ast.Inspect(f, func(n ast.Node) bool {
		decl, ok := n.(*ast.FuncDecl)
		if !ok || decl.Name.Name != "ReconcileOwnSandboxApproval" {
			return true
		}
		if len(decl.Body.List) != 1 {
			t.Fatal("recovery gained action steps")
		}
		ret, ok := decl.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			t.Fatal("not original native reader")
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 6 {
			t.Fatal("not exact original reader")
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "actionReadDispatchAddress" {
			t.Fatal("alternate writer/reader")
		}
		mode, ok := call.Args[4].(*ast.BasicLit)
		if !ok || mode.Value != "1" {
			t.Fatal("not reconciliation")
		}
		by, ok := call.Args[5].(*ast.Ident)
		if !ok || by.Name != "true" {
			t.Fatal("not approval address")
		}
		matched = true
		return false
	})
	if !matched {
		t.Fatal("native consumer missing")
	}
}
