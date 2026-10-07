package agentrun

import (
	"errors"
	"testing"
	"time"
)

const dispatchOwnerA = "11111111-1111-4111-8111-111111111111"
const dispatchOwnerB = "22222222-2222-4222-8222-222222222222"

func TestAgentRunDispatchUnitBurstUsesTenantHistoryNotQueueSize(t *testing.T) {
	// Synthetic queue kernel only, not a PG load/throughput test. A has 10,000
	// ready runs, B only 10; each successful native claim updates durable history.
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	heads := []DispatchTenant{{Owner: dispatchOwnerA, NextDue: now.Add(-time.Minute)}, {Owner: dispatchOwnerB, NextDue: now.Add(-time.Second)}}
	left := map[string]int{dispatchOwnerA: 10000, dispatchOwnerB: 10}
	for i := 0; i < 20; i++ {
		owner, e := PickDispatchTenant(now, 0, heads)
		want := dispatchOwnerA
		if i%2 == 1 {
			want = dispatchOwnerB
		}
		if e != nil || owner != want {
			t.Fatalf("turn %d owner %s want %s err %v", i, owner, want, e)
		}
		left[owner]--
		for j := range heads {
			if heads[j].Owner == owner {
				served := now
				heads[j].LastServed = &served
			}
		}
		now = now.Add(time.Millisecond)
	}
	if left[dispatchOwnerB] != 0 || left[dispatchOwnerA] != 9990 {
		t.Fatal(left)
	}
}

func TestAgentRunDispatchUnitCapacityAndDeterministicTie(t *testing.T) {
	now := time.Now().UTC()
	heads := []DispatchTenant{{Owner: dispatchOwnerB, NextDue: now}, {Owner: dispatchOwnerA, NextDue: now}}
	if owner, e := PickDispatchTenant(now, 0, heads); e != nil || owner != dispatchOwnerA {
		t.Fatal(owner, e)
	}
	heads[1].Active = 1
	if owner, e := PickDispatchTenant(now, 1, heads); e != nil || owner != dispatchOwnerB {
		t.Fatal("one tenant cannot occupy every slot", owner, e)
	}
	if _, e := PickDispatchTenant(now, MaxConcurrentRuns, heads); !errors.Is(e, ErrDispatchBusy) {
		t.Fatal(e)
	}
	if _, e := PickDispatchTenant(now, 1, heads[1:]); !errors.Is(e, ErrDispatchBusy) {
		t.Fatal("capacity is not an empty queue", e)
	}
	if _, e := PickDispatchTenant(now, 0, nil); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

func TestAgentRunDispatchUnitRejectsCorruptNativeMetadata(t *testing.T) {
	now := time.Now().UTC()
	future := now.Add(time.Second)
	for name, heads := range map[string][]DispatchTenant{
		"foreign identifier": {{Owner: "organization-or-client-confirmed", NextDue: now}},
		"duplicate tenant":   {{Owner: dispatchOwnerA, NextDue: now}, {Owner: dispatchOwnerA, NextDue: now}},
		"negative active":    {{Owner: dispatchOwnerA, Active: -1, NextDue: now}},
		"inconsistent count": {{Owner: dispatchOwnerA, Active: 1, NextDue: now}},
		"future due":         {{Owner: dispatchOwnerA, NextDue: future}},
		"future history":     {{Owner: dispatchOwnerA, NextDue: now, LastServed: &future}},
		"zero due":           {{Owner: dispatchOwnerA}},
		"unbounded input":    make([]DispatchTenant, MaxDispatchTenantHeads+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := PickDispatchTenant(now, 0, heads); !errors.Is(e, ErrUnavailable) {
				t.Fatal(e)
			}
		})
	}
	if _, e := PickDispatchTenant(time.Time{}, 0, nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := PickDispatchTenant(now, -1, nil); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
}
