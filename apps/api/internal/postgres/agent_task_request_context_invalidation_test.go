package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
type taskRequestInvalidationUnitTx struct {
	pgx.Tx
	at                                    time.Time
	record, parent                        agentoutbox.Record
	receipt, parentReceipt                agentoutbox.ConsumerRecord
	commands                              []string
	args                                  [][]any
	active, ownerActive                   int
	priorAttempts                         int64
	contextCurrent                        bool
	rowEpoch                              string
	more, lockBusy, disappeared, noUpsert bool
	failQuery, failExec                   string
	finalClock                            time.Time
	lateAfterCleanup, changeAfterCleanup  bool
	contextChangedAfterCleanup            bool
	cleanup, child                        int
	commitErr                             error
	commits                               int
}

func taskRequestInvalidationUnitFixture(child bool) (*taskRequestInvalidationUnitTx, agentoutbox.Claim) {
	at := time.Date(2026, 10, 7, 0, 1, 0, 0, time.UTC)
	p := actorref.PrincipalRef{Type: actorref.Person, ID: controlOwnerA}
	e := agentoutbox.Envelope{SchemaVersion: agentoutbox.TaskRequestSchema, EventType: agentoutbox.TaskRequestChanged, Tenant: p, Subject: p,
		Actor: actorref.ActorRef{Type: actorref.Person, ID: p.ID}, AgentID: "82000000-0000-4000-8000-000000000003",
		Source:     agentoutbox.SourceReference{Type: agentoutbox.TaskRequestSource, ID: "82000000-0000-4000-8000-000000000004", Owner: p, Revision: 0, Status: agentoutbox.TaskRequestActive, Fingerprint: taskRequestUnitFingerprint("450", agentoutbox.TaskRequestActive)},
		OccurredAt: at.Add(-time.Minute), ReceivedAt: at.Add(-time.Minute), ExpiresAt: at.Add(14 * time.Minute)}
	e.LogicalOperationID = agentoutbox.TaskRequestOperationID(e.Source.ID, e.Source.Fingerprint, e.EventType)
	e.RootTraceID = e.LogicalOperationID
	e.EventID = agentoutbox.StableEventID(e)
	parent, _ := agentoutbox.NewPending(e, e.ReceivedAt)
	parent.State = agentoutbox.MemoryComplete
	parent.Attempt = 1
	parent.Fence = 1
	parent.UpdatedAt = at
	pp := agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.TaskRequestHandler, State: agentoutbox.MemoryComplete, Reason: agentoutbox.ReasonCleanupComplete, Fence: 1, Attempt: 1, CreatedAt: at, UpdatedAt: at}
	if child {
		cause := e.EventID
		e.EventType = agentoutbox.TaskContextInvalidation
		e.LogicalOperationID = agentoutbox.TaskRequestOperationID(e.Source.ID, e.Source.Fingerprint, e.EventType)
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
	c := agentoutbox.Claim{EventID: e.EventID, Subject: p, AgentID: e.AgentID, HandlerVersion: agentoutbox.TaskRequestHandler, WorkerID: r.LeaseOwner, Fence: 1, LeaseUntil: until}
	x := &taskRequestInvalidationUnitTx{at: at, record: r, parent: parent, parentReceipt: pp, contextCurrent: true, rowEpoch: "450", receipt: agentoutbox.ConsumerRecord{EventID: e.EventID, Subject: p, HandlerVersion: agentoutbox.TaskRequestHandler, State: agentoutbox.Leased, Fence: 1, Attempt: 1, CreatedAt: at, UpdatedAt: at}}
	if child {
		x.priorAttempts = 1
	}
	return x, c
}

func (x *taskRequestInvalidationUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
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
		case q == taskRequestOutboxCurrentSQL:
			return memoryUnitAssign(d, x.record.Event.Source.Status, x.record.Event.OccurredAt, x.rowEpoch, taskRequestUnitFingerprint(x.rowEpoch, x.record.Event.Source.Status), x.at, x.contextCurrent, []byte(`{"id":"native","row":"450","queryDigest":"opaque-076-digest"}`))
		case q == taskRequestRootAttemptsSQL:
			return memoryUnitAssign(d, x.priorAttempts+x.record.Attempt)
		case q == `SELECT clock_timestamp()`:
			at := x.at
			if !x.finalClock.IsZero() {
				at = x.finalClock
			}
			return memoryUnitAssign(d, at)
		case q == taskRequestPreviewMoreSQL || q == taskRequestGrantMoreSQL:
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
			if args[0] == x.parent.Event.EventID && x.record.Event.EventType == agentoutbox.TaskContextInvalidation {
				return memoryUnitRecordRow(d, x.parent)
			}
			return memoryUnitRecordRow(d, x.record)
		}
		return errors.New("unrecognized unit SQL query")
	}}
}
func (x *taskRequestInvalidationUnitTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	if q != outboxControlDispatchHeadsSQL {
		return nil, errors.New("unrecognized unit SQL rows")
	}
	return &dispatchUnitRows{heads: []ar.DispatchTenant{{Owner: x.record.Event.Subject.ID, Active: x.ownerActive, NextDue: x.record.Event.OccurredAt}}}, nil
}
func (x *taskRequestInvalidationUnitTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	if x.failExec != "" && strings.Contains(q, x.failExec) {
		return pgconn.CommandTag{}, errors.New("UNIT_PRIVATE_CANARY")
	}
	switch {
	case q == taskRequestPreviewCleanupSQL || q == taskRequestGrantCleanupSQL:
		x.cleanup++
		if x.lateAfterCleanup {
			x.finalClock = *x.record.LeaseUntil
		}
		if x.changeAfterCleanup {
			x.rowEpoch += "x"
		}
		if x.contextChangedAfterCleanup {
			x.contextCurrent = !x.contextCurrent
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
func (x *taskRequestInvalidationUnitTx) Commit(context.Context) error {
	x.commits++
	return x.commitErr
}

func taskRequestUnitIndex(x *taskRequestInvalidationUnitTx, needle string) int {
	for i, q := range x.commands {
		if strings.Contains(q, needle) {
			return i
		}
	}
	return -1
}

func taskRequestUnitFingerprint(epoch string, status agentoutbox.SourceStatus) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte("native-test-source:"+epoch+":"+string(status))))
}
func TestTaskRequestInvalidationUnitActualConsumerStages(t *testing.T) {
	for _, tc := range []struct {
		name             string
		child, completed bool
	}{{"active-preview", false, false}, {"active-grant", true, false}, {"completed-preview", false, true}, {"completed-grant", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := taskRequestInvalidationUnitFixture(tc.child)
			if tc.completed {
				x.contextCurrent = false
				parentEvent := x.parent.Event
				parentEvent.Source.Status = agentoutbox.TaskRequestCompleted
				parentEvent.Source.Fingerprint = taskRequestUnitFingerprint(x.rowEpoch, parentEvent.Source.Status)
				parentEvent.LogicalOperationID = agentoutbox.TaskRequestOperationID(parentEvent.Source.ID, parentEvent.Source.Fingerprint, parentEvent.EventType)
				parentEvent.RootTraceID = parentEvent.LogicalOperationID
				parentEvent.EventID = agentoutbox.StableEventID(parentEvent)
				x.parent.Event = parentEvent
				x.parentReceipt.EventID = parentEvent.EventID
				event := parentEvent
				if tc.child {
					cause := event.EventID
					event.EventType = agentoutbox.TaskContextInvalidation
					event.CausationID = &cause
					event.LogicalOperationID = agentoutbox.TaskRequestOperationID(event.Source.ID, event.Source.Fingerprint, event.EventType)
					event.ReceivedAt = x.at
					event.EventID = agentoutbox.StableEventID(event)
				}
				x.record.Event = event
				c.EventID = event.EventID
				x.receipt.EventID = event.EventID
			}
			p, e := consumeTaskRequestOutboxTx(context.Background(), x, c)
			if e != nil || p.State != agentoutbox.MemoryComplete || x.cleanup != 1 || x.child != map[bool]int{false: 1, true: 0}[tc.child] {
				t.Fatal(p, e, x.commands)
			}
			meta, owner, root, source, event := taskRequestUnitIndex(x, "FOR UPDATE OF ap"), taskRequestUnitIndex(x, "76033"), taskRequestUnitIndex(x, "76034"), taskRequestUnitIndex(x, "FOR SHARE OF t"), taskRequestUnitIndex(x, "FOR UPDATE OF d")
			if !(meta >= 0 && meta < owner && owner < root && root < source && source < event) {
				t.Fatal("trusted native lock order", x.commands)
			}
			needle := "DELETE FROM agent_context_purpose_previews"
			if tc.child {
				needle = "UPDATE consent_grants"
			}
			i := taskRequestUnitIndex(x, needle)
			if i < event || x.args[i][2] != x.record.Event.Source.ID || x.args[i][3] != x.record.Event.OccurredAt.Format(time.RFC3339Nano) || x.args[i][4] != x.rowEpoch || x.args[i][5] != !tc.completed || x.args[i][6] != contextPurposeHash(json.RawMessage(`{"id":"native","row":"450","queryDigest":"opaque-076-digest"}`)) || x.args[i][6] == x.record.Event.Source.Fingerprint {
				t.Fatal("076 source namespace/eligibility is not event hash", x.args[i])
			}
		})
	}
}

func TestTaskRequestInvalidationUnitConsumerCurrentBudgetHistory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*taskRequestInvalidationUnitTx, *agentoutbox.Claim)
		want   error
		state  agentoutbox.State
	}{
		{"stale_source", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { x.rowEpoch += "x" }, nil, agentoutbox.Invalidated},
		{"foreign_owner", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { c.Subject.ID = controlOwnerB }, agentoutbox.ErrInvalid, ""},
		{"stale_fence", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { c.Fence++ }, agentoutbox.ErrConflict, ""},
		{"late_after_cleanup", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { x.lateAfterCleanup = true }, agentoutbox.ErrExpired, ""},
		{"ABA_after_cleanup", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { x.changeAfterCleanup = true }, agentoutbox.ErrConflict, ""},
		{"context_after_cleanup", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { x.contextChangedAfterCleanup = true }, agentoutbox.ErrConflict, ""},
		{"budget_remaining_visible", func(x *taskRequestInvalidationUnitTx, c *agentoutbox.Claim) { x.priorAttempts = 5; x.more = true }, nil, agentoutbox.DeadLetter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := taskRequestInvalidationUnitFixture(false)
			tc.change(x, &c)
			p, e := consumeTaskRequestOutboxTx(context.Background(), x, c)
			if e != tc.want || (e == nil && p.State != tc.state) || (e != nil && p != (agentoutbox.ConsumerRecord{})) || x.child != 0 {
				t.Fatal(p, e, x.child)
			}
		})
	}
	x, c := taskRequestInvalidationUnitFixture(false)
	x.record.State = agentoutbox.MemoryComplete
	x.record.LeaseOwner = ""
	x.record.LeaseUntil = nil
	x.receipt.State = agentoutbox.MemoryComplete
	x.receipt.Reason = agentoutbox.ReasonCleanupComplete
	p, e := consumeTaskRequestOutboxTx(context.Background(), x, c)
	if e != nil || p != x.receipt || x.cleanup != 0 || x.child != 0 {
		t.Fatal("terminal history must not repeat cleanup", p, e)
	}
}

func TestTaskRequestInvalidationUnitClaimActualQuotaAndReceipt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*taskRequestInvalidationUnitTx)
		want   error
	}{
		{"allowed", func(x *taskRequestInvalidationUnitTx) {}, nil},
		{"global_capacity", func(x *taskRequestInvalidationUnitTx) { x.active = 4 }, agentoutbox.ErrDispatchBusy},
		{"owner_capacity", func(x *taskRequestInvalidationUnitTx) { x.ownerActive = 1; x.active = 1 }, agentoutbox.ErrDispatchBusy},
		{"chosen_row_missing", func(x *taskRequestInvalidationUnitTx) { x.disappeared = true }, agentoutbox.ErrDispatchBusy},
		{"historical_inbox_no_upsert", func(x *taskRequestInvalidationUnitTx) { x.noUpsert = true }, agentoutbox.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, _ := taskRequestInvalidationUnitFixture(false)
			x.record.State = agentoutbox.Pending
			x.record.Attempt = 0
			x.record.Fence = 0
			x.record.LeaseOwner = ""
			x.record.LeaseUntil = nil
			tc.change(x)
			r, c, e := claimTaskRequestOutboxTx(context.Background(), x, "82000000-0000-4000-8000-000000000006", x.record.Event.Subject.ID)
			if e != tc.want || (e == nil && (r.State != agentoutbox.Leased || c.HandlerVersion != agentoutbox.TaskRequestHandler)) || (e != nil && c != (agentoutbox.Claim{})) {
				t.Fatal(r, c, e)
			}
			if e == nil {
				if !(taskRequestUnitIndex(x, "FOR UPDATE OF ap") < taskRequestUnitIndex(x, "FOR UPDATE OF d")) {
					t.Fatal(x.commands)
				}
			}
		})
	}
	x, c := taskRequestInvalidationUnitFixture(false)
	x.commitErr = errors.New("UNIT_COMMIT_UNKNOWN")
	if r, k, e := commitMemoryClaimTx(context.Background(), x, x.record, c); e == nil || r != (agentoutbox.Record{}) || k != (agentoutbox.Claim{}) {
		t.Fatal("shared real commit exit must not release fresh claim", r, k, e)
	}
	if p, e := commitMemoryReceiptTx(context.Background(), x, x.receipt); e == nil || p != (agentoutbox.ConsumerRecord{}) {
		t.Fatal("unknown commit is not receipt", p, e)
	}
}

func TestTaskRequestInvalidationUnitMigrationPreservesOriginalHistoryAndTypedSource(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	up, down, old := read("../../migrations/132_agent_task_request_context_invalidation.sql"), read("../../migrations/132_agent_task_request_context_invalidation.down.sql"), read("../../migrations/102_agent_preference_context_invalidation.sql")
	for _, name := range []string{"birdtie_guard_agent_outbox", "birdtie_guard_agent_consumer_inbox"} {
		start := strings.Index(old, "CREATE OR REPLACE FUNCTION "+name+"()")
		end := start + strings.Index(old[start:], "END $$;") + len("END $$;")
		body := old[start:end]
		suffix := body[strings.Index(body, "BEGIN\n")+len("BEGIN\n"):]
		if !strings.Contains(up, suffix) || !strings.Contains(down, body) {
			t.Fatal("UNIT_STATIC original102 guard body lost", name)
		}
	}
	for _, text := range []string{"AFTER INSERT OR UPDATE ON agent_tasks", "NEW.acting_user_account_id IS DISTINCT FROM NEW.owner_account_id", "source_revision=0", "source_revision>0", "NEW.received_at=i.updated_at", "p.xmin::text=(txid_current()%4294967296)::text", "i.xmin::text=(txid_current()%4294967296)::text", ">=6", "132 down refuses retained Task request"} {
		if !strings.Contains(up+down, text) {
			t.Fatal("UNIT_STATIC native hook/fence/down missing", text)
		}
	}
	if strings.Contains(up, "NEW=OLD") || strings.Contains(up, "NEW.updated_at=OLD.updated_at") || strings.Contains(up, "NEW.xmin=OLD.xmin") || strings.Contains(up, "DELETE FROM agent_domain_outbox") {
		t.Fatal("same-value native epoch skipped or event history deleted")
	}
	if !strings.Contains(taskRequestGrantMoreSQL, "g.revoked_at IS NULL") || strings.Contains(taskRequestGrantMoreSQL, "g.revision<") || !strings.Contains(taskRequestGrantCleanupSQL, "g.revision=x.revision") || !strings.Contains(taskRequestPreviewCleanupSQL, "NOT EXISTS") {
		t.Fatal("original CAS/bound history/exhaustion was widened")
	}
}
