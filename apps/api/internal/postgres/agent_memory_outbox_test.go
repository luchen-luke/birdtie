package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
)

func memoryUnitPending(x *memoryInvalidationUnitTx) {
	x.record.State = agentoutbox.Pending
	x.record.Attempt = 0
	x.record.Fence = 0
	x.record.LeaseOwner = ""
	x.record.LeaseUntil = nil
	x.record.UpdatedAt = x.record.CreatedAt
}

func TestMemoryOutboxUnitClaimUsesActualAdmissionAndLockOrder(t *testing.T) {
	x, old := memoryInvalidationUnitFixture(false)
	memoryUnitPending(x)
	r, c, e := claimMemoryOutboxTx(context.Background(), x, old.WorkerID, old.Subject.ID)
	if e != nil || r.State != agentoutbox.Leased || c.Fence != 1 || c.Subject != old.Subject || c.HandlerVersion != agentoutbox.MemoryHandler {
		t.Fatal(r, c, e)
	}
	nominate, metadata, memory, locked := memoryUnitIndex(x, "LIMIT 1"), memoryUnitIndex(x, "FOR UPDATE OF ap"), memoryUnitIndex(x, "FOR SHARE OF m"), memoryUnitIndex(x, "FOR UPDATE OF d")
	if !(nominate >= 0 && nominate < metadata && metadata < memory && memory < locked) || strings.Contains(x.commands[nominate], "FOR UPDATE") {
		t.Fatal("nomination must not invert original metadata lock ordering", x.commands)
	}
	if x.commands[0] != outboxControlDispatchLockSQL || x.commands[1] != outboxControlDispatchCountSQL || x.commands[2] != outboxControlDispatchHeadsSQL || x.args[2][2] != old.Subject.ID || x.args[2][3] != agentoutbox.MaxControlTenantHeads {
		t.Fatal("current admission/subject/head cap not reused", x.commands, x.args)
	}
}

func TestMemoryOutboxUnitQuotaNoUpsertAndRootExhaustion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*memoryInvalidationUnitTx)
		want   error
	}{
		{"global_cap", func(x *memoryInvalidationUnitTx) { x.active = 4 }, agentoutbox.ErrDispatchBusy},
		{"owner_cap", func(x *memoryInvalidationUnitTx) { x.ownerActive = 1; x.active = 1 }, agentoutbox.ErrDispatchBusy},
		{"selected_disappeared", func(x *memoryInvalidationUnitTx) { x.disappeared = true }, agentoutbox.ErrDispatchBusy},
		{"immutable_receipt_conflict", func(x *memoryInvalidationUnitTx) { x.noUpsert = true }, agentoutbox.ErrConflict},
		{"source_newer", func(x *memoryInvalidationUnitTx) { x.rowRevision = 3 }, errMemoryTerminalControl},
		{"parent_spent_all_budget_child_visible", func(x *memoryInvalidationUnitTx) { x.priorAttempts = 6 }, errMemoryTerminalControl},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, c := memoryInvalidationUnitFixture(tc.name == "parent_spent_all_budget_child_visible")
			memoryUnitPending(x)
			tc.change(x)
			if tc.name == "parent_spent_all_budget_child_visible" {
				x.parent.Attempt = 6
				x.parent.Fence = 6
				x.parentReceipt.Attempt = 6
				x.parentReceipt.Fence = 6
			}
			r, claim, e := claimMemoryOutboxTx(context.Background(), x, c.WorkerID, c.Subject.ID)
			if e != tc.want || !reflect.DeepEqual(r, agentoutbox.Record{}) || claim != (agentoutbox.Claim{}) {
				t.Fatal(r, claim, e)
			}
			if tc.name == "parent_spent_all_budget_child_visible" && (x.record.State != agentoutbox.DeadLetter || x.cleanup != 0 || x.child != 0) {
				t.Fatal("budget terminal not observable")
			}
			if tc.want == agentoutbox.ErrDispatchBusy && x.cleanup != 0 {
				t.Fatal("capacity became effect")
			}
		})
	}
}

func TestMemoryOutboxUnitUnknownCommitIsNotFreshReceiptOrRetry(t *testing.T) {
	x, c := memoryInvalidationUnitFixture(false)
	x.commitErr = errors.New("UNIT_COMMIT_UNKNOWN")
	r, claim, e := commitMemoryClaimTx(context.Background(), x, x.record, c)
	if e != x.commitErr || !reflect.DeepEqual(r, agentoutbox.Record{}) || claim != (agentoutbox.Claim{}) || x.commits != 1 {
		t.Fatal(r, claim, e)
	}
	p, e := commitMemoryReceiptTx(context.Background(), x, x.receipt)
	if e != x.commitErr || p != (agentoutbox.ConsumerRecord{}) || x.commits != 2 || x.cleanup != 0 {
		t.Fatal(p, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = claimMemoryOutboxTx(ctx, x, c.WorkerID, c.Subject.ID); e != context.Canceled || len(x.commands) != 0 {
		t.Fatal("cancel issued SQL", e)
	}
}
