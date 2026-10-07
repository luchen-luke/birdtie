// Package agentenrichmentworker invokes a finite native batch. It has no
// inference loop, provider transport, Session export or generic action writer.
package agentenrichmentworker

import (
	"context"
	"errors"
	ar "github.com/birdtie/birdtie/apps/api/internal/agentrun"
)

type Backend interface {
	ClaimAgentRun(context.Context, string) (ar.Claim, error)
	ExecuteAgentRun(context.Context, ar.Claim) (ar.Record, error)
	ExpireAgentRuns(context.Context, int) (int64, error)
}
type Result struct {
	Claimed     int   `json:"claimed"`
	Succeeded   int   `json:"succeeded"`
	RetryWait   int   `json:"retryWait"`
	Failed      int   `json:"failed"`
	Cancelled   int   `json:"cancelled"`
	Expired     int64 `json:"expired"`
	Unknown     int   `json:"unknown"`
	Unavailable bool  `json:"unavailable"`
	Throttled   bool  `json:"throttled,omitempty"`
}

func RunOnce(ctx context.Context, b Backend, worker string, limit int) (Result, error) {
	var r Result
	if ctx == nil || b == nil || limit < 1 || limit > 25 {
		return r, ar.ErrInvalid
	}
	n, e := b.ExpireAgentRuns(ctx, limit)
	if e != nil {
		return r, e
	}
	r.Expired = n
	for i := 0; i < limit; i++ {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		claim, e := b.ClaimAgentRun(ctx, worker)
		if errors.Is(e, ar.ErrClaimExhausted) {
			r.Failed++
			continue // consumes one bounded batch slot, not an executable claim
		}
		if errors.Is(e, ar.ErrDispatchBusy) {
			r.Throttled = true
			return r, nil // no immediate polling/resend; next invocation rechecks
		}
		if errors.Is(e, ar.ErrNotFound) {
			return r, nil
		}
		if errors.Is(e, ar.ErrUnavailable) {
			r.Unavailable = true
			return r, nil
		}
		if e != nil {
			return r, e
		}
		r.Claimed++
		rec, e := b.ExecuteAgentRun(ctx, claim)
		if e != nil {
			r.Unknown++
			continue
		} // durable next invocation reconciles, never resend here
		switch rec.State {
		case ar.Succeeded:
			r.Succeeded++
		case ar.RetryWait:
			r.RetryWait++
		case ar.Failed:
			r.Failed++
		case ar.Cancelled:
			r.Cancelled++
		case ar.Expired:
			r.Expired++
		default:
			r.Unknown++
		}
	}
	return r, nil
}
