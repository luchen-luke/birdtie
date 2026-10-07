package agentenrichmentworker

import (
	"context"
	"errors"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
	"testing"
)

// Unit control coverage only; native authority/effect/process recovery is
// independently tested with the actual Postgres/082 writer.
type batchUnitBackend struct {
	claims, executes, expires int
	unavailable               bool
	unknown                   bool
}

func (b *batchUnitBackend) ExpireAgentRuns(context.Context, int) (int64, error) {
	b.expires++
	return 0, nil
}
func (b *batchUnitBackend) ClaimAgentRun(context.Context, string) (ar.Claim, error) {
	b.claims++
	if b.unavailable {
		return ar.Claim{}, ar.ErrUnavailable
	}
	return ar.Claim{}, nil
}
func (b *batchUnitBackend) ExecuteAgentRun(context.Context, ar.Claim) (ar.Record, error) {
	b.executes++
	if b.unknown {
		return ar.Record{}, ar.ErrUnavailable
	}
	return ar.Record{State: ar.RetryWait}, nil
}
func TestAgentRunWorkerFiniteBatchDoesNotResendUnknown(t *testing.T) {
	b := &batchUnitBackend{unknown: true}
	r, e := RunOnce(context.Background(), b, "worker-unit", 3)
	if e != nil || b.claims != 3 || b.executes != 3 || r.Unknown != 3 || r.Claimed != 3 {
		t.Fatal(r, e, b)
	}
	b = &batchUnitBackend{unavailable: true}
	r, e = RunOnce(context.Background(), b, "worker-unit", 25)
	if e != nil || !r.Unavailable || r.Claimed != 0 || b.executes != 0 || b.claims != 1 {
		t.Fatal(r, e, b)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = RunOnce(ctx, &batchUnitBackend{}, "worker-unit", 1)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	for _, n := range []int{0, 26, -1} {
		if _, e = RunOnce(context.Background(), b, "worker-unit", n); !errors.Is(e, ar.ErrInvalid) {
			t.Fatal(n, e)
		}
	}
}

// This unit calls the original bounded worker, without native DB effects.
type dispatchBusyUnitBackend struct { batchUnitBackend }
func (b *dispatchBusyUnitBackend) ClaimAgentRun(context.Context, string) (ar.Claim, error) { b.claims++;return ar.Claim{},ar.ErrDispatchBusy }
func TestAgentRunDispatchUnitWorkerDoesNotSpinOrPretendEmpty(t *testing.T) {
 b:=&dispatchBusyUnitBackend{};r,e:=RunOnce(context.Background(),b,"worker-unit",25)
 if e!=nil||!r.Throttled||r.Unavailable||b.expires!=1||b.claims!=1||b.executes!=0||r.Claimed!=0 {t.Fatal(r,e,b)}
}
