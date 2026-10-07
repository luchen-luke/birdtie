package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A SQL-aware pgx transaction spy executes the actual private native helper.
// It does not execute SQL or establish PostgreSQL isolation/clock/lock proofs.
type memoryInvalidationUnitTx struct {
	pgx.Tx
	at                                    time.Time
	record, parent                        agentoutbox.Record
	receipt, parentReceipt                agentoutbox.ConsumerRecord
	commands                              []string
	args                                  [][]any
	active, ownerActive                   int
	priorAttempts                         int64
	rowRevision                           int64
	rowEpoch                              string
	more, lockBusy, disappeared, noUpsert bool
	failQuery, failExec                   string
	finalClock                            time.Time
	lateAfterCleanup, changeAfterCleanup  bool
	cleanup, child                        int
	commitErr                             error
	commits                               int
}

func memoryInvalidationUnitFixture(child bool) (*memoryInvalidationUnitTx, agentoutbox.Claim) {
	at := time.Date(2026, 10, 7, 0, 1, 0, 0, time.UTC)
	p := actorref.PrincipalRef{Type: actorref.Person, ID: controlOwnerA}
	e := agentoutbox.Envelope{SchemaVersion: agentoutbox.MemorySchema, EventType: agentoutbox.MemoryUpdated, Tenant: p, Subject: p,
		Actor: actorref.ActorRef{Type: actorref.Person, ID: p.ID}, AgentID: "82000000-0000-4000-8000-000000000003",
		Source:     agentoutbox.SourceReference{Type: agentoutbox.MemorySource, ID: "82000000-0000-4000-8000-000000000004", Owner: p, Revision: 2, Status: agentoutbox.MemoryActive, Fingerprint: strings.Repeat("a", 64)},
		OccurredAt: at.Add(-time.Minute), ReceivedAt: at.Add(-time.Minute), ExpiresAt: at.Add(14 * time.Minute)}
	e.LogicalOperationID = agentoutbox.MemoryOperationID(e.Source.ID, e.Source.Revision, e.EventType)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = agentoutbox.StableEventID(e)
	parent, _ := agentoutbox.NewPending(e, e.ReceivedAt)
	parent.State = agentoutbox.MemoryComplete
	parent.Attempt = 1
	parent.Fence = 1
	parent.UpdatedAt = at
	pp := agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.MemoryHandler, State: agentoutbox.MemoryComplete, Reason: agentoutbox.ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: at, UpdatedAt: at}
	if child {
		cause := e.EventID
		e.EventType = agentoutbox.MemoryContextInvalidation
		e.LogicalOperationID = agentoutbox.MemoryOperationID(e.Source.ID, e.Source.Revision, e.EventType)
		e.CausationID = &cause
		e.ReceivedAt = at
		e.EventID = agentoutbox.StableEventID(e)
	}
	r, _ := agentoutbox.NewPending(e, e.ReceivedAt)
	r.State = agentoutbox.Leased
	r.Attempt = 1
	r.Fence = 1
	r.LeaseOwner = "82000000-0000-4000-8000-000000000006"
	until := at.Add(30 * time.Second)
	r.LeaseUntil = &until
	r.UpdatedAt = at
	c := agentoutbox.Claim{EventID: e.EventID, Subject: p, AgentID: e.AgentID, HandlerVersion: agentoutbox.MemoryHandler, WorkerID: r.LeaseOwner, Fence: 1, LeaseUntil: until}
	x := &memoryInvalidationUnitTx{at: at, record: r, parent: parent, parentReceipt: pp, rowRevision: 2, rowEpoch: "450", receipt: agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.MemoryHandler, State: agentoutbox.Leased, Fence: 1, Attempt: 1, CreatedAt: at, UpdatedAt: at}}
	if child {
		x.priorAttempts = 1
	}
	return x, c
}

func memoryUnitAssign(d []any, v ...any) error {
	if len(d) != len(v) {
		return errors.New("unit row shape mismatch")
	}
	for i, value := range v {
		dest := reflect.ValueOf(d[i]).Elem()
		if value == nil {
			dest.Set(reflect.Zero(dest.Type()))
		} else {
			dest.Set(reflect.ValueOf(value).Convert(dest.Type()))
		}
	}
	return nil
}
func memoryUnitRecordRow(d []any, r agentoutbox.Record) error {
	e := r.Event
	return memoryUnitAssign(d, e.SchemaVersion, e.EventID, e.EventType, e.Subject.ID, e.AgentID, e.LogicalOperationID, e.RootTraceID, e.CausationID, e.OccurredAt, e.ReceivedAt, e.ExpiresAt, e.Source.ID, e.Source.Revision, e.Source.Status, e.Source.Fingerprint, r.State, r.Attempt, r.Fence, r.LeaseOwner, r.LeaseUntil, r.NextAttemptAt, r.CreatedAt, r.UpdatedAt)
}
func memoryUnitReceiptRow(d []any, p agentoutbox.ConsumerRecord) error {
	return memoryUnitAssign(d, p.State, p.Fence, p.Attempt, p.Reason, p.CreatedAt, p.UpdatedAt)
}
func (x *memoryInvalidationUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	return dispatchUnitRow{fn: func(d []any) error {
		if x.failQuery != "" && strings.Contains(q, x.failQuery) {
			return errors.New("UNIT_PRIVATE_CANARY")
		}
		switch {
		case q == outboxControlDispatchLockSQL:
			return memoryUnitAssign(d, !x.lockBusy)
		case q == outboxControlDispatchCountSQL:
			return memoryUnitAssign(d, x.at, x.active)
		case q == memoryMaintenanceBindingSQL:
			return memoryUnitAssign(d, x.record.Event.AgentID)
		case q == memoryOutboxCurrentSQL:
			return memoryUnitAssign(d, x.rowRevision, x.record.Event.Source.Status, x.record.Event.OccurredAt, x.rowEpoch, x.record.Event.Source.Fingerprint, x.at)
		case q == memoryRootAttemptsSQL:
			return memoryUnitAssign(d, x.priorAttempts+x.record.Attempt)
		case q == `SELECT clock_timestamp()`:
			at := x.at
			if !x.finalClock.IsZero() {
				at = x.finalClock
			}
			return memoryUnitAssign(d, at)
		case q == memoryPreviewMoreSQL || q == memoryGrantMoreSQL:
			return memoryUnitAssign(d, x.more)
		case strings.HasPrefix(q, "UPDATE agent_consumer_inbox"):
			x.receipt.State = args[4].(agentoutbox.State)
			x.receipt.Reason = args[5].(agentoutbox.ReasonCode)
			x.receipt.UpdatedAt = x.at
			return memoryUnitReceiptRow(d, x.receipt)
		case strings.HasPrefix(q, "SELECT control_state"):
			if args[0] == x.parent.Event.EventID {
				return memoryUnitReceiptRow(d, x.parentReceipt)
			}
			return memoryUnitReceiptRow(d, x.receipt)
		case strings.HasPrefix(q, "WITH clk AS MATERIALIZED"):
			x.record.State = agentoutbox.Leased
			x.record.Attempt++
			x.record.Fence++
			x.record.LeaseOwner = args[1].(string)
			u := x.at.Add(30 * time.Second)
			x.record.LeaseUntil = &u
			x.record.UpdatedAt = x.at
			return memoryUnitRecordRow(d, x.record)
		case strings.HasPrefix(q, "SELECT "+outboxColumns):
			if strings.Contains(q, "LIMIT 1") && x.disappeared {
				return pgx.ErrNoRows
			}
			if args[0] == x.parent.Event.EventID && x.record.Event.EventType == agentoutbox.MemoryContextInvalidation {
				return memoryUnitRecordRow(d, x.parent)
			}
			return memoryUnitRecordRow(d, x.record)
		}
		return errors.New("unrecognized unit SQL query")
	}}
}
func (x *memoryInvalidationUnitTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	if q != outboxControlDispatchHeadsSQL {
		return nil, errors.New("unrecognized unit SQL rows")
	}
	return &dispatchUnitRows{heads: []ar.DispatchTenant{{Owner: x.record.Event.Subject.ID, Active: x.ownerActive, NextDue: x.record.Event.OccurredAt}}}, nil
}
func (x *memoryInvalidationUnitTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	if x.failExec != "" && strings.Contains(q, x.failExec) {
		return pgconn.CommandTag{}, errors.New("UNIT_PRIVATE_CANARY")
	}
	switch {
	case q == memoryPreviewCleanupSQL || q == memoryGrantCleanupSQL:
		x.cleanup++
		if x.lateAfterCleanup {
			x.finalClock = *x.record.LeaseUntil
		}
		if x.changeAfterCleanup {
			x.rowRevision++
		}
	case strings.HasPrefix(q, "INSERT INTO agent_domain_outbox"):
		x.child++
	case strings.HasPrefix(q, "INSERT INTO agent_consumer_inbox"):
		if x.noUpsert {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		x.receipt.Fence = x.record.Fence
		x.receipt.Attempt = x.record.Attempt
		x.receipt.State = agentoutbox.Leased
	case strings.HasPrefix(q, "UPDATE agent_domain_outbox"):
		state := args[1]
		if len(args) > 3 {
			state = args[3]
		}
		x.record.State = state.(agentoutbox.State)
		x.record.LeaseOwner = ""
		x.record.LeaseUntil = nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (x *memoryInvalidationUnitTx) Commit(context.Context) error { x.commits++; return x.commitErr }

func memoryUnitIndex(x *memoryInvalidationUnitTx, needle string) int {
	for i, q := range x.commands {
		if strings.Contains(q, needle) {
			return i
		}
	}
	return -1
}

func TestMemoryContextInvalidationUnitActualTwoStages(t *testing.T) {
	for _, child := range []bool{false, true} {
		t.Run(map[bool]string{false: "unbound_preview_then_fixed_child", true: "sole_consent_grant_revocation"}[child], func(t *testing.T) {
			x, c := memoryInvalidationUnitFixture(child)
			p, e := consumeMemoryOutboxTx(context.Background(), x, c)
			if e != nil || p.State != agentoutbox.MemoryComplete || p.Reason != agentoutbox.ReasonCleanupComplete || x.cleanup != 1 || x.child != map[bool]int{false: 1, true: 0}[child] {
				t.Fatal(p, e, x.cleanup, x.child)
			}
			metadata, owner, root, memory, event := memoryUnitIndex(x, "FOR UPDATE OF ap"), memoryUnitIndex(x, "76033"), memoryUnitIndex(x, "76034"), memoryUnitIndex(x, "FOR SHARE OF m"), memoryUnitIndex(x, "FOR UPDATE OF d")
			if !(metadata >= 0 && metadata < owner && owner < root && root < memory && memory < event) {
				t.Fatal("real native helper lock ordering", x.commands)
			}
			if child {
				if memoryUnitIndex(x, "UPDATE consent_grants") < event || memoryUnitIndex(x, "DELETE FROM agent_context_purpose_previews") >= 0 {
					t.Fatal("wrong stage effect")
				}
			} else {
				if memoryUnitIndex(x, "DELETE FROM agent_context_purpose_previews") < event || memoryUnitIndex(x, "UPDATE consent_grants") >= 0 {
					t.Fatal("wrong stage effect")
				}
				i := memoryUnitIndex(x, "INSERT INTO agent_domain_outbox")
				a := x.args[i]
				if a[2] != agentoutbox.MemoryContextInvalidation || a[10] != x.record.Event.RootTraceID || a[11] != c.EventID || a[14] != x.record.Event.ExpiresAt {
					t.Fatal("child fabricated root/time")
				}
			}
		})
	}
}

func TestMemoryContextInvalidationUnitProgressBudgetAndHistory(t *testing.T) {
	for _, tc := range []struct {
		name        string
		child, more bool
		prior       int64
		want        agentoutbox.State
	}{
		{"partial_root_no_child", false, true, 0, agentoutbox.Pending},
		{"six_shared_attempts_visible_unfinished", true, true, 5, agentoutbox.DeadLetter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := memoryInvalidationUnitFixture(tc.child)
			x.more = tc.more
			x.priorAttempts = tc.prior
			p, e := consumeMemoryOutboxTx(context.Background(), x, c)
			if e != nil || p.State != tc.want || x.child != 0 {
				t.Fatal(p, e, x.child)
			}
		})
	}
	x, c := memoryInvalidationUnitFixture(false)
	x.record.State = agentoutbox.MemoryComplete
	x.record.LeaseOwner = ""
	x.record.LeaseUntil = nil
	x.receipt.State = agentoutbox.MemoryComplete
	x.receipt.Reason = agentoutbox.ReasonCleanupComplete
	p, e := consumeMemoryOutboxTx(context.Background(), x, c)
	if e != nil || p != x.receipt || x.cleanup != 0 || x.child != 0 {
		t.Fatal("replay must preserve existing history, not repeat cleanup", p, e)
	}
}

func TestMemoryContextInvalidationUnitCurrentDeniedAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*memoryInvalidationUnitTx, *agentoutbox.Claim)
		want   error
	}{
		{"foreign_owner", func(x *memoryInvalidationUnitTx, c *agentoutbox.Claim) { c.Subject.ID = controlOwnerB }, agentoutbox.ErrInvalid},
		{"stale_fence", func(x *memoryInvalidationUnitTx, c *agentoutbox.Claim) { c.Fence++ }, agentoutbox.ErrConflict},
		{"late_native_clock", func(x *memoryInvalidationUnitTx, c *agentoutbox.Claim) { x.finalClock = c.LeaseUntil }, agentoutbox.ErrExpired},
		{"cleanup_unknown", func(x *memoryInvalidationUnitTx, c *agentoutbox.Claim) { x.failExec = "DELETE FROM agent_context" }, agentoutbox.ErrUnavailable},
		{"uncommitted_parent", func(x *memoryInvalidationUnitTx, c *agentoutbox.Claim) { x.parent.State = agentoutbox.Pending }, agentoutbox.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := memoryInvalidationUnitFixture(tc.name == "uncommitted_parent")
			tc.change(x, &c)
			p, e := consumeMemoryOutboxTx(context.Background(), x, c)
			if e != tc.want || p != (agentoutbox.ConsumerRecord{}) || x.child != 0 {
				t.Fatal(p, e, x.child)
			}
		})
	}
	x, c := memoryInvalidationUnitFixture(false)
	x.rowRevision++
	p, e := consumeMemoryOutboxTx(context.Background(), x, c)
	if e != nil || p.State != agentoutbox.Invalidated || x.cleanup != 0 || x.child != 0 {
		t.Fatal("source revision/ABA cannot cleanup or spawn", p, e)
	}
}

func TestMemoryContextInvalidationUnitLateSourceAndClockAfterEffect(t *testing.T) {
	for _, change := range []bool{false, true} {
		t.Run(map[bool]string{false: "lease_expired_after_cleanup", true: "source_changed_after_cleanup"}[change], func(t *testing.T) {
			x, c := memoryInvalidationUnitFixture(false)
			x.changeAfterCleanup = change
			x.lateAfterCleanup = !change
			p, e := consumeMemoryOutboxTx(context.Background(), x, c)
			want := agentoutbox.ErrExpired
			if change {
				want = agentoutbox.ErrConflict
			}
			if e != want || p != (agentoutbox.ConsumerRecord{}) || x.cleanup != 1 || x.child != 0 || memoryUnitIndex(x, "UPDATE agent_consumer_inbox") >= 0 {
				t.Fatal("late source/lease produced a checkpoint instead of rollback-required failure", p, e, x.commands)
			}
		})
	}
}
