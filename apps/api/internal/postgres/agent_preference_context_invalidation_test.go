package postgres

import (
	"context"
	"errors"
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
type preferenceInvalidationUnitTx struct {
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

func preferenceInvalidationUnitFixture(child bool) (*preferenceInvalidationUnitTx, agentoutbox.Claim) {
	at := time.Date(2026, 10, 7, 0, 1, 0, 0, time.UTC)
	p := actorref.PrincipalRef{Type: actorref.Person, ID: controlOwnerA}
	e := agentoutbox.Envelope{SchemaVersion: agentoutbox.PreferenceSchema, EventType: agentoutbox.PreferenceUpdated, Tenant: p, Subject: p,
		Actor: actorref.ActorRef{Type: actorref.Person, ID: p.ID}, AgentID: "82000000-0000-4000-8000-000000000003",
		Source:     agentoutbox.SourceReference{Type: agentoutbox.PreferenceSource, ID: "82000000-0000-4000-8000-000000000003", Owner: p, Revision: 2, Status: agentoutbox.PreferenceConfigured, Fingerprint: strings.Repeat("a", 64)},
		OccurredAt: at.Add(-time.Minute), ReceivedAt: at.Add(-time.Minute), ExpiresAt: at.Add(14 * time.Minute)}
	e.LogicalOperationID = agentoutbox.PreferenceOperationID(e.Source.ID, e.Source.Revision, e.EventType)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = agentoutbox.StableEventID(e)
	parent, _ := agentoutbox.NewPending(e, e.ReceivedAt)
	parent.State = agentoutbox.MemoryComplete
	parent.Attempt = 1
	parent.Fence = 1
	parent.UpdatedAt = at
	pp := agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.PreferenceHandler, State: agentoutbox.MemoryComplete, Reason: agentoutbox.ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: at, UpdatedAt: at}
	if child {
		cause := e.EventID
		e.EventType = agentoutbox.PreferenceContextInvalidation
		e.LogicalOperationID = agentoutbox.PreferenceOperationID(e.Source.ID, e.Source.Revision, e.EventType)
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
	c := agentoutbox.Claim{EventID: e.EventID, Subject: p, AgentID: e.AgentID, HandlerVersion: agentoutbox.PreferenceHandler, WorkerID: r.LeaseOwner, Fence: 1, LeaseUntil: until}
	x := &preferenceInvalidationUnitTx{at: at, record: r, parent: parent, parentReceipt: pp, rowRevision: 2, rowEpoch: "450", receipt: agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.PreferenceHandler, State: agentoutbox.Leased, Fence: 1, Attempt: 1, CreatedAt: at, UpdatedAt: at}}
	if child {
		x.priorAttempts = 1
	}
	return x, c
}

func (x *preferenceInvalidationUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
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
		case q == preferenceOutboxConfiguredSQL || q == preferenceOutboxClearedSQL:
			return memoryUnitAssign(d, x.rowRevision, x.record.Event.Source.Status, x.record.Event.OccurredAt, x.rowEpoch, x.record.Event.Source.Fingerprint, x.at)
		case q == preferenceRootAttemptsSQL:
			return memoryUnitAssign(d, x.priorAttempts+x.record.Attempt)
		case q == `SELECT clock_timestamp()`:
			at := x.at
			if !x.finalClock.IsZero() {
				at = x.finalClock
			}
			return memoryUnitAssign(d, at)
		case q == preferencePreviewMoreSQL || q == preferenceGrantMoreSQL:
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
			if args[0] == x.parent.Event.EventID && x.record.Event.EventType == agentoutbox.PreferenceContextInvalidation {
				return memoryUnitRecordRow(d, x.parent)
			}
			return memoryUnitRecordRow(d, x.record)
		}
		return errors.New("unrecognized unit SQL query")
	}}
}
func (x *preferenceInvalidationUnitTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	if q != outboxControlDispatchHeadsSQL {
		return nil, errors.New("unrecognized unit SQL rows")
	}
	return &dispatchUnitRows{heads: []ar.DispatchTenant{{Owner: x.record.Event.Subject.ID, Active: x.ownerActive, NextDue: x.record.Event.OccurredAt}}}, nil
}
func (x *preferenceInvalidationUnitTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	if x.failExec != "" && strings.Contains(q, x.failExec) {
		return pgconn.CommandTag{}, errors.New("UNIT_PRIVATE_CANARY")
	}
	switch {
	case q == preferencePreviewCleanupSQL || q == preferenceGrantCleanupSQL:
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
func (x *preferenceInvalidationUnitTx) Commit(context.Context) error { x.commits++; return x.commitErr }

func preferenceUnitIndex(x *preferenceInvalidationUnitTx, needle string) int {
	for i, q := range x.commands {
		if strings.Contains(q, needle) {
			return i
		}
	}
	return -1
}

func TestPreferenceInvalidationActualConsumerStages(t *testing.T) {
	for _, tc := range []struct {
		name         string
		child, clear bool
	}{{"configured_preview", false, false}, {"configured_grant", true, false}, {"clear_preview", false, true}, {"clear_grant", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := preferenceInvalidationUnitFixture(tc.child)
			if tc.clear {
				x.record.Event.Source.Status = agentoutbox.PreferenceCleared
				x.record.Event.EventID = agentoutbox.StableEventID(x.record.Event)
				x.parent.Event.Source.Status = agentoutbox.PreferenceCleared
				x.parent.Event.EventID = agentoutbox.StableEventID(x.parent.Event)
				if tc.child {
					cause := x.parent.Event.EventID
					x.record.Event.CausationID = &cause
				}
				c.EventID = x.record.Event.EventID
				x.receipt.EventID = c.EventID
				x.parentReceipt.EventID = x.parent.Event.EventID
			}
			receipt, e := consumePreferenceOutboxTx(context.Background(), x, c)
			if e != nil || receipt.State != agentoutbox.MemoryComplete || x.cleanup != 1 || x.child != map[bool]int{false: 1, true: 0}[tc.child] {
				t.Fatal(receipt, e, x.commands)
			}
			meta, owner, root, event := preferenceUnitIndex(x, "FOR UPDATE OF ap"), preferenceUnitIndex(x, "76033"), preferenceUnitIndex(x, "76034"), preferenceUnitIndex(x, "FOR UPDATE OF d")
			source := preferenceUnitIndex(x, "FOR SHARE OF pp")
			if tc.clear {
				source = preferenceUnitIndex(x, "NOT EXISTS(SELECT 1 FROM agent_private_profiles")
			}
			if !(meta >= 0 && meta < owner && owner < root && root < source && source < event) {
				t.Fatal("native helper call lock order", x.commands)
			}
			needle := "DELETE FROM agent_context_purpose_previews"
			if tc.child {
				needle = "UPDATE consent_grants"
			}
			i := preferenceUnitIndex(x, needle)
			if i < event || x.args[i][5] != !tc.clear || x.args[i][2] != c.AgentID {
				t.Fatal("wrong real private source stage", x.args)
			}
		})
	}
}

func TestPreferenceInvalidationConsumerCurrentBudgetHistory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*preferenceInvalidationUnitTx, *agentoutbox.Claim)
		want   error
		state  agentoutbox.State
	}{
		{"stale_source", func(x *preferenceInvalidationUnitTx, c *agentoutbox.Claim) { x.rowRevision++ }, nil, agentoutbox.Invalidated},
		{"foreign_owner", func(x *preferenceInvalidationUnitTx, c *agentoutbox.Claim) { c.Subject.ID = controlOwnerB }, agentoutbox.ErrInvalid, ""},
		{"stale_fence", func(x *preferenceInvalidationUnitTx, c *agentoutbox.Claim) { c.Fence++ }, agentoutbox.ErrConflict, ""},
		{"late_after_cleanup", func(x *preferenceInvalidationUnitTx, c *agentoutbox.Claim) { x.lateAfterCleanup = true }, agentoutbox.ErrExpired, ""},
		{"ABA_after_cleanup", func(x *preferenceInvalidationUnitTx, c *agentoutbox.Claim) { x.changeAfterCleanup = true }, agentoutbox.ErrConflict, ""},
		{"budget_remaining_visible", func(x *preferenceInvalidationUnitTx, c *agentoutbox.Claim) { x.priorAttempts = 5; x.more = true }, nil, agentoutbox.DeadLetter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := preferenceInvalidationUnitFixture(false)
			tc.change(x, &c)
			p, e := consumePreferenceOutboxTx(context.Background(), x, c)
			if e != tc.want || (e == nil && p.State != tc.state) || (e != nil && p != (agentoutbox.ConsumerRecord{})) || x.child != 0 {
				t.Fatal(p, e, x.child)
			}
		})
	}
	x, c := preferenceInvalidationUnitFixture(false)
	x.record.State = agentoutbox.MemoryComplete
	x.record.LeaseOwner = ""
	x.record.LeaseUntil = nil
	x.receipt.State = agentoutbox.MemoryComplete
	x.receipt.Reason = agentoutbox.ReasonCleanupComplete
	p, e := consumePreferenceOutboxTx(context.Background(), x, c)
	if e != nil || p != x.receipt || x.cleanup != 0 || x.child != 0 {
		t.Fatal("terminal history must not repeat cleanup", p, e)
	}
}

func TestPreferenceInvalidationClaimActualQuotaAndReceipt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*preferenceInvalidationUnitTx)
		want   error
	}{
		{"allowed", func(x *preferenceInvalidationUnitTx) {}, nil},
		{"global_capacity", func(x *preferenceInvalidationUnitTx) { x.active = 4 }, agentoutbox.ErrDispatchBusy},
		{"owner_capacity", func(x *preferenceInvalidationUnitTx) { x.ownerActive = 1; x.active = 1 }, agentoutbox.ErrDispatchBusy},
		{"chosen_row_missing", func(x *preferenceInvalidationUnitTx) { x.disappeared = true }, agentoutbox.ErrDispatchBusy},
		{"historical_inbox_no_upsert", func(x *preferenceInvalidationUnitTx) { x.noUpsert = true }, agentoutbox.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, _ := preferenceInvalidationUnitFixture(false)
			x.record.State = agentoutbox.Pending
			x.record.Attempt = 0
			x.record.Fence = 0
			x.record.LeaseOwner = ""
			x.record.LeaseUntil = nil
			tc.change(x)
			r, c, e := claimPreferenceOutboxTx(context.Background(), x, "82000000-0000-4000-8000-000000000006", x.record.Event.Subject.ID)
			if e != tc.want || (e == nil && (r.State != agentoutbox.Leased || c.HandlerVersion != agentoutbox.PreferenceHandler)) || (e != nil && c != (agentoutbox.Claim{})) {
				t.Fatal(r, c, e)
			}
			if e == nil {
				if !(preferenceUnitIndex(x, "FOR UPDATE OF ap") < preferenceUnitIndex(x, "FOR UPDATE OF d")) {
					t.Fatal(x.commands)
				}
			}
		})
	}
	x, c := preferenceInvalidationUnitFixture(false)
	x.commitErr = errors.New("UNIT_COMMIT_UNKNOWN")
	if r, k, e := commitMemoryClaimTx(context.Background(), x, x.record, c); e == nil || r != (agentoutbox.Record{}) || k != (agentoutbox.Claim{}) {
		t.Fatal("shared real commit exit must not release fresh claim", r, k, e)
	}
	if p, e := commitMemoryReceiptTx(context.Background(), x, x.receipt); e == nil || p != (agentoutbox.ConsumerRecord{}) {
		t.Fatal("unknown commit is not receipt", p, e)
	}
}
