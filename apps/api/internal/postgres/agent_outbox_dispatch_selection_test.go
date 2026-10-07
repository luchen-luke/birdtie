package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	maint "github.com/birdtie/birdtie/apps/api/internal/agentoutboxmaintenance"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"github.com/jackc/pgx/v5"
)

const controlOwnerA = "82000000-0000-4000-8000-000000000001"
const controlOwnerB = "82000000-0000-4000-8000-000000000002"

// This transaction is a unit spy over the production native-claim selector;
// it is not a PostgreSQL execution or proof of multi-process admission.
type outboxDispatchUnitTx struct {
	pgx.Tx
	at                                                   time.Time
	active                                               int
	locked                                               bool
	heads                                                []ar.DispatchTenant
	record                                               agentoutbox.Record
	commands                                             []string
	args                                                 [][]any
	rowErr, queryErr                                     error
	lockErr, countErr, selectedErr, rowsErr, rowsScanErr error
	selected                                             int
	rows                                                 *outboxDispatchUnitRows
}

func controlDispatchRecord(at time.Time, owner string) agentoutbox.Record {
	p := actorref.PrincipalRef{Type: actorref.Person, ID: owner}
	e := agentoutbox.Envelope{SchemaVersion: agentoutbox.SchemaVersion, EventType: agentoutbox.MomentCreated,
		Tenant: p, Subject: p, Actor: actorref.ActorRef{Type: actorref.Person, ID: owner},
		AgentID: "82000000-0000-4000-8000-000000000003", Source: agentoutbox.SourceReference{Type: agentoutbox.MomentSource, ID: "82000000-0000-4000-8000-000000000004", Owner: p, Revision: 1, Status: agentoutbox.Draft, Fingerprint: strings.Repeat("a", 64)},
		LogicalOperationID: "82000000-0000-4000-8000-000000000005", RootTraceID: "82000000-0000-4000-8000-000000000005", OccurredAt: at.Add(-time.Minute), ReceivedAt: at.Add(-time.Minute), ExpiresAt: at.Add(14 * time.Minute)}
	e.EventID = agentoutbox.StableEventID(e)
	r, err := agentoutbox.NewPending(e, e.ReceivedAt)
	if err != nil {
		panic(err)
	}
	return r
}

func (x *outboxDispatchUnitTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	return dispatchUnitRow{fn: func(d []any) error {
		if x.rowErr != nil {
			return x.rowErr
		}
		if strings.Contains(q, "pg_try_advisory_xact_lock") {
			if x.lockErr != nil {
				return x.lockErr
			}
			*d[0].(*bool) = x.locked
			return nil
		}
		if len(d) == 2 {
			if x.countErr != nil {
				return x.countErr
			}
			*d[0].(*time.Time) = x.at
			*d[1].(*int) = x.active
			return nil
		}
		x.selected++
		if x.selectedErr != nil {
			return x.selectedErr
		}
		r := x.record
		e := r.Event
		values := []any{e.SchemaVersion, e.EventID, e.EventType, e.Subject.ID, e.AgentID, e.LogicalOperationID, e.RootTraceID, e.CausationID, e.OccurredAt, e.ReceivedAt, e.ExpiresAt, e.Source.ID, e.Source.Revision, e.Source.Status, e.Source.Fingerprint, r.State, r.Attempt, r.Fence, r.LeaseOwner, r.LeaseUntil, r.NextAttemptAt, r.CreatedAt, r.UpdatedAt}
		if len(d) != len(values) {
			panic("unexpected selector row shape")
		}
		for i, v := range values {
			dest := reflect.ValueOf(d[i]).Elem()
			if v == nil {
				dest.Set(reflect.Zero(dest.Type()))
			} else {
				dest.Set(reflect.ValueOf(v).Convert(dest.Type()))
			}
		}
		return nil
	}}
}
func (x *outboxDispatchUnitTx) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	x.commands = append(x.commands, q)
	x.args = append(x.args, args)
	x.rows = &outboxDispatchUnitRows{dispatchUnitRows: dispatchUnitRows{heads: x.heads}, scanErr: x.rowsScanErr, rowsErr: x.rowsErr}
	return x.rows, x.queryErr
}

type outboxDispatchUnitRows struct {
	dispatchUnitRows
	scanErr, rowsErr error
}

func (r *outboxDispatchUnitRows) Scan(d ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return r.dispatchUnitRows.Scan(d...)
}
func (r *outboxDispatchUnitRows) Err() error { return r.rowsErr }

func readyControlDispatchTx() *outboxDispatchUnitTx {
	at := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	return &outboxDispatchUnitTx{at: at, locked: true, record: controlDispatchRecord(at, controlOwnerA), heads: []ar.DispatchTenant{{Owner: controlOwnerA, NextDue: at.Add(-time.Minute)}}}
}

func TestAgentOutboxControlDispatchUnitOriginalClaimQuota(t *testing.T) {
	for _, tc := range []struct {
		name        string
		active      int
		ownerActive int
	}{
		{"global four active controls", agentoutbox.MaxConcurrentControls, 0},
		{"same owner already has one", 1, agentoutbox.MaxConcurrentControlsPerOwner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
			x := &outboxDispatchUnitTx{at: at, active: tc.active, locked: true, record: controlDispatchRecord(at, controlOwnerA), heads: []ar.DispatchTenant{{Owner: controlOwnerA, Active: tc.ownerActive, NextDue: at.Add(-time.Minute)}}}
			r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, controlOwnerA)
			if e != agentoutbox.ErrDispatchBusy || !reflect.DeepEqual(r, agentoutbox.Record{}) || x.selected != 0 {
				t.Fatalf("original claim selector must stop before selecting another quota-consuming event: error=%v selected=%d recordEmpty=%v", e, x.selected, reflect.DeepEqual(r, agentoutbox.Record{}))
			}
		})
	}
}

func TestAgentOutboxControlDispatchUnitLeastServedAndSubjectBound(t *testing.T) {
	for _, tc := range []struct {
		name, subject, wantOwner string
		foreign                  bool
	}{
		{"global burst keeps least served head", "", controlOwnerB, false},
		{"explicit CLI subject stays its own", controlOwnerA, controlOwnerA, false},
		{"injected foreign subject fails", controlOwnerA, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := readyControlDispatchTx()
			last := x.at.Add(-time.Second)
			if tc.subject == "" || tc.foreign {
				x.heads = []ar.DispatchTenant{{Owner: controlOwnerA, LastServed: &last, NextDue: x.at.Add(-10 * time.Minute)}, {Owner: controlOwnerB, NextDue: x.at.Add(-time.Second)}}
			}
			if tc.foreign {
				x.record = controlDispatchRecord(x.at, controlOwnerB)
			} else {
				x.record = controlDispatchRecord(x.at, tc.wantOwner)
			}
			r, observedAt, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV2, tc.subject)
			if tc.foreign {
				if e != agentoutbox.ErrUnavailable || x.selected != 0 || !reflect.DeepEqual(r, agentoutbox.Record{}) {
					t.Fatal(r, e, x.selected)
				}
				return
			}
			if e != nil || !observedAt.Equal(x.at) || r.Event.Subject.ID != tc.wantOwner || len(x.commands) != 4 || x.commands[0] != outboxControlDispatchLockSQL || x.commands[1] != outboxControlDispatchCountSQL || x.commands[2] != outboxControlDispatchHeadsSQL || x.args[3][1] != tc.wantOwner || !x.rows.closed {
				t.Fatal(r, e, x.commands, x.args)
			}
			if x.args[2][0] != agentoutbox.HandlerV2 || x.args[2][1] != x.at || x.args[2][3] != agentoutbox.MaxControlTenantHeads {
				t.Fatal("handler/stamp/head bound not captured", x.args)
			}
			if (tc.subject == "" && x.args[2][2] != nil) || (tc.subject != "" && x.args[2][2] != tc.subject) {
				t.Fatal("subject selection changed", x.args)
			}
			if !strings.Contains(x.commands[3], "WHERE d.subject_id=$2") || !strings.Contains(x.commands[3], "ORDER BY d.occurred_at,d.event_id LIMIT 1 FOR UPDATE OF d SKIP LOCKED") {
				t.Fatal(x.commands[3])
			}
		})
	}
}

// With neither projected service nor terminal progress, the original due order
// remains intact. This fixture has no terminal progress rows; no claim receipt
// or history is fabricated for the two never-served heads.
func TestAgentOutboxControlDispatchUnitNoHistoryKeepsOriginalDueOrder(t *testing.T) {
	x := readyControlDispatchTx()
	x.heads = []ar.DispatchTenant{{Owner: controlOwnerA, NextDue: x.at.Add(-10 * time.Minute)}, {Owner: controlOwnerB, NextDue: x.at.Add(-time.Second)}}
	r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, "")
	if e != nil || r.Event.Subject.ID != controlOwnerA || x.heads[0].LastServed != nil || x.heads[1].LastServed != nil {
		t.Fatal(r, e)
	}
	raw, e := os.ReadFile("agent_outbox.go")
	if e != nil {
		t.Fatal(e)
	}
	src := string(raw)
	start := strings.Index(src, "if terminal != \"\" {")
	if start < 0 {
		t.Fatal("original terminal branch absent")
	}
	end := strings.Index(src[start:], "attempt=d.attempt+1")
	if end < 0 {
		t.Fatal("original claim write absent")
	}
	branch := src[start : start+end]
	if strings.Contains(branch, "INSERT INTO agent_consumer_inbox") || !strings.Contains(branch, "UPDATE agent_consumer_inbox") || !strings.Contains(branch, "agentoutbox.ErrUnavailable") {
		t.Fatal("no-inbox terminal history was replaced by fake service receipt")
	}
}

func TestAgentOutboxControlDispatchUnitCapacityAndFaultClosure(t *testing.T) {
	for _, tc := range []struct {
		name  string
		edit  func(*outboxDispatchUnitTx)
		want  error
		calls int
	}{
		{"try lock occupied", func(x *outboxDispatchUnitTx) { x.locked = false }, agentoutbox.ErrDispatchBusy, 1},
		{"no eligible head", func(x *outboxDispatchUnitTx) { x.heads = nil }, agentoutbox.ErrNotFound, 3},
		{"selected head disappeared", func(x *outboxDispatchUnitTx) { x.selectedErr = pgx.ErrNoRows }, agentoutbox.ErrDispatchBusy, 4},
		{"lock fault", func(x *outboxDispatchUnitTx) { x.lockErr = errors.New("PRIVATE_LOCK_CANARY") }, agentoutbox.ErrUnavailable, 1},
		{"clock fault", func(x *outboxDispatchUnitTx) { x.countErr = errors.New("PRIVATE_CLOCK_CANARY") }, agentoutbox.ErrUnavailable, 2},
		{"heads fault", func(x *outboxDispatchUnitTx) { x.queryErr = errors.New("PRIVATE_QUERY_CANARY") }, agentoutbox.ErrUnavailable, 3},
		{"row scan fault", func(x *outboxDispatchUnitTx) { x.rowsScanErr = errors.New("PRIVATE_SCAN_CANARY") }, agentoutbox.ErrUnavailable, 3},
		{"rows completion fault", func(x *outboxDispatchUnitTx) { x.rowsErr = errors.New("PRIVATE_ROWS_CANARY") }, agentoutbox.ErrUnavailable, 3},
		{"selected fault", func(x *outboxDispatchUnitTx) { x.selectedErr = errors.New("PRIVATE_ROW_CANARY") }, agentoutbox.ErrUnavailable, 4},
		{"selected invalid record", func(x *outboxDispatchUnitTx) { x.record.Event.Source.Fingerprint = "bad" }, agentoutbox.ErrInvalid, 4},
		{"selected foreign record", func(x *outboxDispatchUnitTx) { x.record = controlDispatchRecord(x.at, controlOwnerB) }, agentoutbox.ErrUnavailable, 4},
		{"negative active", func(x *outboxDispatchUnitTx) { x.active = -1 }, agentoutbox.ErrUnavailable, 2},
		{"invalid clock", func(x *outboxDispatchUnitTx) { x.at = time.Time{} }, agentoutbox.ErrUnavailable, 2},
		{"future last service", func(x *outboxDispatchUnitTx) { v := x.at.Add(time.Second); x.heads[0].LastServed = &v }, agentoutbox.ErrUnavailable, 3},
		{"duplicate head", func(x *outboxDispatchUnitTx) { x.heads = append(x.heads, x.heads[0]) }, agentoutbox.ErrUnavailable, 3},
		{"oversize heads", func(x *outboxDispatchUnitTx) {
			for range agentoutbox.MaxControlTenantHeads {
				x.heads = append(x.heads, x.heads[0])
			}
		}, agentoutbox.ErrUnavailable, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := readyControlDispatchTx()
			tc.edit(x)
			r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, "")
			if e != tc.want || len(x.commands) != tc.calls || !reflect.DeepEqual(r, agentoutbox.Record{}) || strings.Contains(e.Error(), "CANARY") {
				t.Fatal(r, e, x.commands)
			}
		})
	}
}

func TestAgentOutboxControlDispatchUnitReclaimAndExpiryStayOriginal(t *testing.T) {
	for _, state := range []agentoutbox.State{agentoutbox.Pending, agentoutbox.Leased, agentoutbox.Unavailable} {
		t.Run(string(state), func(t *testing.T) {
			x := readyControlDispatchTx()
			x.record = controlDispatchRecord(x.at.Add(-20*time.Minute), controlOwnerA)
			x.record.State = state
			if state != agentoutbox.Pending {
				x.record.Attempt = 1
				x.record.Fence = 1
			}
			if state == agentoutbox.Leased {
				lease := x.record.Event.ReceivedAt.Add(time.Second)
				x.record.LeaseUntil = &lease
				x.record.LeaseOwner = "82000000-0000-4000-8000-000000000006"
			}
			before := x.record
			r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, controlOwnerA)
			if e != nil || !reflect.DeepEqual(r, before) || !r.Event.ExpiresAt.Before(x.at) {
				t.Fatal("selection must retain original expired control for original terminal path, not renew", r, e)
			}
		})
	}
}

func TestAgentOutboxControlDispatchUnitInputAndErrorBoundary(t *testing.T) {
	var typedNil *outboxDispatchUnitTx
	for _, tx := range []pgx.Tx{nil, typedNil} {
		if _, _, e := selectAgentOutboxControlTx(context.Background(), tx, agentoutbox.HandlerV1, ""); e != agentoutbox.ErrInvalid {
			t.Fatal(e)
		}
	}
	for _, owner := range []string{"invalid", "00000000-0000-0000-0000-000000000000", "8200000A-0000-4000-8000-000000000001"} {
		x := readyControlDispatchTx()
		if _, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, owner); e != agentoutbox.ErrInvalid || len(x.commands) != 0 {
			t.Fatal(e, x.commands)
		}
	}
	x := readyControlDispatchTx()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := selectAgentOutboxControlTx(ctx, x, agentoutbox.HandlerV1, ""); e != context.Canceled || len(x.commands) != 0 {
		t.Fatal(e, x.commands)
	}
	if _, _, e := selectAgentOutboxControlTx(nil, x, agentoutbox.HandlerV1, ""); e != agentoutbox.ErrInvalid {
		t.Fatal(e)
	}
	if _, _, e := selectAgentOutboxControlTx(context.Background(), x, "unknown-handler", ""); e != agentoutbox.ErrInvalid {
		t.Fatal(e)
	}
	for _, e := range []error{agentoutbox.ErrInvalid, agentoutbox.ErrExpired, agentoutbox.ErrConflict} {
		if outboxControlSelectionError(fmt.Errorf("PRIVATE: %w", e)) != e {
			t.Fatal(e)
		}
	}
	if outboxControlSelectionError(errors.New("PRIVATE_CANARY")) != agentoutbox.ErrUnavailable || outboxControlSelectionError(pgx.ErrNoRows) != agentoutbox.ErrUnavailable {
		t.Fatal("unknown metadata error is not empty")
	}
}

// UNIT_STATIC: these assertions inspect the actual production SQL contract,
// not execution of SQL against PostgreSQL or a pressure/concurrency benchmark.
func TestAgentOutboxControlDispatchUnitSQLAndOriginalCallerContract(t *testing.T) {
	for _, fragment := range []string{"d.lease_until>stamp.n", "d.expires_at>stamp.n", "i.event_id=d.event_id", "i.subject_id=d.subject_id", "i.fence=d.fence", "i.attempt=d.attempt", "i.control_state='LEASED'", "i.handler_version IN('mom-control-v1','mom-control-v2')"} {
		if !strings.Contains(outboxControlDispatchCountSQL, fragment) {
			t.Fatal(fragment)
		}
	}
	if strings.Contains(outboxControlDispatchCountSQL, "mom-candidate") || strings.Contains(outboxControlDispatchCountSQL, "agent_enrichment_runs") {
		t.Fatal("control budget cannot claim candidate/run admission")
	}
	for _, fragment := range []string{"($3::uuid IS NULL OR d.subject_id=$3)", "max(i.updated_at)", "ORDER BY active,last_served NULLS FIRST,due.next_due,due.subject_id LIMIT $4", "$2::timestamptz"} {
		if !strings.Contains(outboxControlDispatchHeadsSQL, fragment) {
			t.Fatal(fragment)
		}
	}
	if strings.Contains(outboxControlDueSQL, "expires_at>") {
		t.Fatal("original expired work disappeared instead of terminal cleanup")
	}
	for _, fragment := range []string{"d.delivery_state='PENDING'", "d.delivery_state='LEASED'", "d.lease_until<=clock_timestamp()", "i.handler_version=$1 AND i.fence=d.fence AND i.control_state='LEASED'", "d.delivery_state='UNAVAILABLE' AND NOT EXISTS"} {
		if !strings.Contains(outboxControlDueSQL, fragment) {
			t.Fatal(fragment)
		}
	}
	raw, e := os.ReadFile("agent_outbox.go")
	if e != nil {
		t.Fatal(e)
	}
	src := string(raw)
	start := strings.Index(src, "func (s *Store) claimAgentOutboxControl(")
	if start < 0 {
		t.Fatal("original caller absent")
	}
	end := strings.Index(src[start:], "// ConsumeAgentOutboxControl")
	if end < 0 {
		t.Fatal("original consumer absent")
	}
	body := src[start : start+end]
	if strings.Index(body, "selectAgentOutboxControlTx(ctx, tx, handler, subjectID)") >= strings.Index(body, "outboxCurrentTx(ctx, tx, r)") || !strings.Contains(body, "agentoutbox.CheckFence(now, claim, claim)") || !strings.Contains(body, "!r.Event.ExpiresAt.After(now)") || !strings.Contains(body, "tx.Commit(ctx)") {
		t.Fatal("original current source and last-clock checks lost")
	}
	guard := strings.Index(body, "outboxControlAdmissionClock(admissionObservedAt, now)")
	if guard < strings.LastIndex(body, "agentoutbox.CheckFence(now, claim, claim)") || guard < strings.LastIndex(body, "!r.Event.ExpiresAt.After(now)") || guard > strings.LastIndex(body, "tx.Commit(ctx)") || !strings.Contains(body, "r, admissionObservedAt, err := selectAgentOutboxControlTx") {
		t.Fatal("admission observedAt must be captured and compared to the original final clock after old fence/expiry priorities")
	}
}

func TestAgentOutboxControlDispatchUnitAdmissionClockCannotRegress(t *testing.T) {
	at := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name             string
		admission, final time.Time
		want             error
	}{
		{"equal", at, at, nil},
		{"later", at, at.Add(time.Nanosecond), nil},
		{"regressed", at, at.Add(-time.Nanosecond), agentoutbox.ErrUnavailable},
		{"missingAdmission", time.Time{}, at, agentoutbox.ErrUnavailable},
		{"missingFinal", at, time.Time{}, agentoutbox.ErrUnavailable},
		{"outOfRangeAdmission", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), at, agentoutbox.ErrUnavailable},
		{"outOfRangeFinal", at, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), agentoutbox.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := outboxControlAdmissionClock(tc.admission, tc.final); e != tc.want {
				t.Fatal(e)
			}
		})
	}
}

type outboxControlDispatchRunnerSpy struct {
	tx               *outboxDispatchUnitTx
	claims, consumes int
}

func (s *outboxControlDispatchRunnerSpy) ClaimAgentOutboxControlForSubject(ctx context.Context, p actorref.PrincipalRef, _ string, h agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
	s.claims++
	_, _, e := selectAgentOutboxControlTx(ctx, s.tx, h, p.ID)
	return agentoutbox.Record{}, agentoutbox.Claim{}, e
}
func (s *outboxControlDispatchRunnerSpy) ConsumeAgentOutboxControl(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	s.consumes++
	return agentoutbox.ConsumerRecord{}, agentoutbox.ErrUnavailable
}

func TestAgentOutboxControlDispatchUnitSelectorFeedsExistingRunner(t *testing.T) {
	for _, tc := range []struct {
		name           string
		edit           func(*outboxDispatchUnitTx)
		status, reason string
	}{
		{"global cap", func(x *outboxDispatchUnitTx) { x.active = agentoutbox.MaxConcurrentControls }, "FINISHED", "DISPATCH_CAPACITY_BUSY"},
		{"owner cap", func(x *outboxDispatchUnitTx) { x.active = 1; x.heads[0].Active = 1 }, "FINISHED", "DISPATCH_CAPACITY_BUSY"},
		{"occupied admission lock", func(x *outboxDispatchUnitTx) { x.locked = false }, "FINISHED", "DISPATCH_CAPACITY_BUSY"},
		{"selected head gone", func(x *outboxDispatchUnitTx) { x.selectedErr = pgx.ErrNoRows }, "FINISHED", "DISPATCH_CAPACITY_BUSY"},
		{"genuine empty", func(x *outboxDispatchUnitTx) { x.heads = nil }, "FINISHED", "NO_ELIGIBLE_WORK"},
		{"query unknown", func(x *outboxDispatchUnitTx) { x.queryErr = errors.New("PRIVATE_SELECTOR_CANARY") }, "STOPPED", "CLAIM_UNCONFIRMED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := readyControlDispatchTx()
			tc.edit(x)
			s := &outboxControlDispatchRunnerSpy{tx: x}
			r := maint.Run(context.Background(), s, maint.Options{Subject: actorref.PrincipalRef{Type: actorref.Person, ID: controlOwnerA}, WorkerID: "82000000-0000-4000-8000-000000000006", Handler: agentoutbox.HandlerV1, Batch: 100, Timeout: time.Second})
			if r.Status != tc.status || r.Reason != tc.reason || s.claims != 1 || s.consumes != 0 || r.ConfirmedReceipts != 0 || r.BusinessExecution != "UNAVAILABLE" {
				t.Fatal(r, s.claims, s.consumes)
			}
		})
	}
}

// This is a query-aware unit projection spy, not a SQL engine: it returns the
// stored synthetic terminal progress only if the production head query projects
// that branch. Dedicated UNIT_STATIC assertions also check its closed predicates.
type controlTerminalProgressUnitRow struct {
	owner              string
	state              agentoutbox.State
	attempt, fence     int64
	leaseOwner         string
	hasLease, hasInbox bool
	updatedAt          time.Time
}
type terminalProgressDispatchUnitTx struct {
	*outboxDispatchUnitTx
	terminal         []controlTerminalProgressUnitRow
	service          map[string]*time.Time
	projectsTerminal bool
}

func (x *terminalProgressDispatchUnitTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if strings.HasPrefix(q, "SELECT "+outboxColumns) {
		x.record = controlDispatchRecord(x.at, args[1].(string))
	}
	return x.outboxDispatchUnitTx.QueryRow(ctx, q, args...)
}
func (x *terminalProgressDispatchUnitTx) Query(ctx context.Context, q string, args ...any) (pgx.Rows, error) {
	x.projectsTerminal = strings.Contains(q, "SELECT max(progress_d.updated_at)")
	var heads []ar.DispatchTenant
	for _, head := range x.heads {
		if args[2] != nil && head.Owner != args[2].(string) {
			continue
		}
		head.LastServed = x.service[head.Owner]
		if x.projectsTerminal {
			for _, row := range x.terminal {
				if row.owner != head.Owner || row.attempt != 0 || row.fence != 0 || row.leaseOwner != "" || row.hasLease || row.hasInbox || (row.state != agentoutbox.Invalidated && row.state != agentoutbox.Expired && row.state != agentoutbox.DeadLetter) {
					continue
				}
				if head.LastServed == nil || row.updatedAt.After(*head.LastServed) {
					v := row.updatedAt
					head.LastServed = &v
				}
			}
		}
		heads = append(heads, head)
	}
	copyTx := *x.outboxDispatchUnitTx
	copyTx.heads = heads
	r, e := copyTx.Query(ctx, q, args...)
	x.commands = copyTx.commands
	x.args = copyTx.args
	x.rows = copyTx.rows
	return r, e
}
func terminalProgressDispatchTx() *terminalProgressDispatchUnitTx {
	x := readyControlDispatchTx()
	x.heads = []ar.DispatchTenant{{Owner: controlOwnerA, NextDue: x.at.Add(-10 * time.Minute)}, {Owner: controlOwnerB, NextDue: x.at.Add(-time.Second)}}
	return &terminalProgressDispatchUnitTx{outboxDispatchUnitTx: x, terminal: []controlTerminalProgressUnitRow{{owner: controlOwnerA, state: agentoutbox.Expired, updatedAt: x.at.Add(-time.Second)}}, service: map[string]*time.Time{}}
}
func TestAgentOutboxTerminalProgressUnitOriginalQueryNeedsTerminalProjection(t *testing.T) {
	x := terminalProgressDispatchTx()
	r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, "")
	if e != nil || !x.projectsTerminal || r.Event.Subject.ID != controlOwnerB {
		t.Fatalf("actual original head query must project committed no-inbox terminal progress before selecting the next tenant: projectsTerminal=%v owner=%s err=%v", x.projectsTerminal, r.Event.Subject.ID, e)
	}
}

func TestAgentOutboxTerminalProgressUnitCommittedAndCoalescedProgress(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state agentoutbox.State
	}{
		{"source invalidated without receipt", agentoutbox.Invalidated},
		{"expired without receipt", agentoutbox.Expired},
		{"dead letter without receipt", agentoutbox.DeadLetter},
		{"coalesced invalidation is progress not service", agentoutbox.Invalidated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := terminalProgressDispatchTx()
			x.terminal[0].state = tc.state
			r, observed, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV2, "")
			if e != nil || !observed.Equal(x.at) || r.Event.Subject.ID != controlOwnerB || !x.projectsTerminal || x.selected != 1 || len(x.commands) != 4 {
				t.Fatal(r, e, x.commands)
			}
			if x.terminal[0].hasInbox || len(x.service) != 0 || !x.rows.closed {
				t.Fatal("fixture must stay no-inbox and not fabricate service", x)
			}
		})
	}
}
func TestAgentOutboxTerminalProgressUnitUsesMaximumHistory(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		serviceAge, progressAge time.Duration
		hasService              bool
		wantOwner               string
	}{
		{"newer service wins max", time.Second, 10 * time.Second, true, controlOwnerB},
		{"newer terminal progress wins max", 10 * time.Second, time.Second, true, controlOwnerB},
		{"older terminal than other tenant", 0, 10 * time.Second, false, controlOwnerA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := terminalProgressDispatchTx()
			x.terminal[0].updatedAt = x.at.Add(-tc.progressAge)
			if tc.hasService {
				v := x.at.Add(-tc.serviceAge)
				x.service[controlOwnerA] = &v
			}
			b := x.at.Add(-5 * time.Second)
			x.service[controlOwnerB] = &b
			r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, "")
			if e != nil || r.Event.Subject.ID != tc.wantOwner {
				t.Fatal(r, e)
			}
		})
	}
}
func TestAgentOutboxTerminalProgressUnitExcludedRowsDoNotInventProgress(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*controlTerminalProgressUnitRow)
	}{
		{"active leased", func(r *controlTerminalProgressUnitRow) { r.state = agentoutbox.Leased }},
		{"pending", func(r *controlTerminalProgressUnitRow) { r.state = agentoutbox.Pending }},
		{"unavailable", func(r *controlTerminalProgressUnitRow) { r.state = agentoutbox.Unavailable }},
		{"candidate staged", func(r *controlTerminalProgressUnitRow) { r.state = agentoutbox.CandidateStaged }},
		{"any inbox including candidate", func(r *controlTerminalProgressUnitRow) { r.hasInbox = true }},
		{"attempted", func(r *controlTerminalProgressUnitRow) { r.attempt = 1 }},
		{"old fencing token", func(r *controlTerminalProgressUnitRow) { r.fence = 1 }},
		{"has worker", func(r *controlTerminalProgressUnitRow) { r.leaseOwner = "82000000-0000-4000-8000-000000000006" }},
		{"has lease deadline", func(r *controlTerminalProgressUnitRow) { r.hasLease = true }},
		{"different owner", func(r *controlTerminalProgressUnitRow) { r.owner = controlOwnerB }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := terminalProgressDispatchTx()
			tc.edit(&x.terminal[0])
			r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, "")
			if e != nil || r.Event.Subject.ID != controlOwnerA {
				t.Fatal(r, e)
			}
		})
	}
}
func TestAgentOutboxTerminalProgressUnitCurrentSubjectAndInvalidClock(t *testing.T) {
	t.Run("CLI does not choose another subject", func(t *testing.T) {
		x := terminalProgressDispatchTx()
		r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, controlOwnerA)
		if e != nil || r.Event.Subject.ID != controlOwnerA || len(x.rows.heads) != 1 {
			t.Fatal(r, e)
		}
	})
	for _, tc := range []struct {
		name string
		at   time.Time
	}{
		{"future progress", time.Date(2026, 10, 6, 15, 0, 1, 0, time.UTC)},
		{"invalid zero progress", time.Time{}},
		{"out of range progress", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := terminalProgressDispatchTx()
			x.terminal[0].updatedAt = tc.at
			r, _, e := selectAgentOutboxControlTx(context.Background(), x, agentoutbox.HandlerV1, "")
			if e != agentoutbox.ErrUnavailable || x.selected != 0 || !reflect.DeepEqual(r, agentoutbox.Record{}) {
				t.Fatal(r, e)
			}
		})
	}
}

// UNIT_STATIC: these are assertions on the production query and original
// mutation paths. The synthetic projection spy above is not SQL execution.
func TestAgentOutboxTerminalProgressUnitSQLContractAndRealProgressWriters(t *testing.T) {
	for _, fragment := range []string{
		"GREATEST((SELECT max(i.updated_at)", "i.handler_version IN('mom-control-v1','mom-control-v2')",
		"SELECT max(progress_d.updated_at)", "FROM agent_domain_outbox progress_d",
		"progress_d.subject_id=due.subject_id", "progress_d.schema_version='agent-outbox-v1'", "progress_d.source_type='MOMENT'",
		"progress_d.delivery_state IN('INVALIDATED','EXPIRED','DEAD_LETTER')",
		"progress_d.attempt=0", "progress_d.fence=0", "progress_d.lease_owner IS NULL", "progress_d.lease_until IS NULL",
		"NOT EXISTS(SELECT 1 FROM agent_consumer_inbox progress_i", "progress_i.event_id=progress_d.event_id", "progress_i.subject_id=progress_d.subject_id",
		"ORDER BY active,last_served NULLS FIRST,due.next_due,due.subject_id LIMIT $4",
		"($3::uuid IS NULL OR d.subject_id=$3)",
	} {
		if !strings.Contains(outboxControlDispatchHeadsSQL, fragment) {
			t.Fatal(fragment)
		}
	}
	for _, forbidden := range []string{"agent_effect_ledger", "consent_grants", "agent_enrichment_runs", "INSERT ", "UPDATE ", "DELETE "} {
		if strings.Contains(outboxControlDispatchHeadsSQL, forbidden) {
			t.Fatal("ranking may only read retained control progress", forbidden)
		}
	}
	for _, path := range []string{"agent_outbox.go", "agent_outbox_coalescing.go"} {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		src := string(raw)
		if path == "agent_outbox.go" {
			if !strings.Contains(src, "SET delivery_state=$2,lease_owner=NULL,lease_until=NULL,updated_at=clock_timestamp()") || !strings.Contains(src, "tx.Commit(ctx)") {
				t.Fatal("original terminal durable progress path absent")
			}
		} else {
			if !strings.Contains(src, "SET delivery_state='INVALIDATED',updated_at=clk.at") || !strings.Contains(src, "d.attempt=0 AND d.fence=0") {
				t.Fatal("original refresh invalidation progress absent")
			}
		}
	}
}
