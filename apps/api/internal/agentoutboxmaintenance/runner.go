// Package agentoutboxmaintenance runs bounded native control maintenance.
// A selector, lease shape, receipt or this runner grants no analysis or effect.
package agentoutboxmaintenance

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
)

const MaxBatch = 1000
const MaxTimeout = time.Minute
const MinTimeout = time.Millisecond

type Store interface {
	ClaimAgentOutboxControlForSubject(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error)
	ConsumeAgentOutboxControl(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error)
}

type Options struct {
	Subject  actorref.PrincipalRef
	WorkerID string
	Handler  agentoutbox.HandlerVersion
	Batch    int
	Timeout  time.Duration
}

func ValidateOptions(o Options) bool {
	subject, e := actorref.ParsePrincipal("PERSON", o.Subject.ID)
	_, workerError := agentoutbox.NormalizeWorkerID(o.WorkerID)
	return e == nil && subject == o.Subject && o.Subject.ID != "00000000-0000-0000-0000-000000000000" && workerError == nil && agentoutbox.ValidateHandlerVersion(o.Handler) == nil && o.Batch >= 1 && o.Batch <= MaxBatch && o.Timeout >= MinTimeout && o.Timeout <= MaxTimeout
}

type Counts struct {
	MaintenanceComplete int `json:"maintenance_complete,omitempty"`
	MaintenanceProgress int `json:"maintenance_progress,omitempty"`
	Unavailable         int `json:"unavailable"`
	Invalidated         int `json:"invalidated"`
	Expired             int `json:"expired"`
	DeadLetter          int `json:"dead_letter"`
}

// Report contains aggregate controls only, never the underlying rows/errors.
type Report struct {
	Schema            string `json:"schema"`
	Status            string `json:"status"`
	Stage             string `json:"stage"`
	Reason            string `json:"reason"`
	ConfirmedReceipts int    `json:"confirmed_receipts"`
	Counts            Counts `json:"counts"`
	BusinessExecution string `json:"business_execution"`
}

func NewReport(status, stage, reason string) Report {
	return Report{Schema: "birdtie.outbox-maintenance.v1", Status: status, Stage: stage, Reason: reason, BusinessExecution: "UNAVAILABLE"}
}

func missing(s Store) bool {
	if s == nil {
		return true
	}
	v := reflect.ValueOf(s)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}

func stop(r Report, stage, reason string) Report {
	r.Status, r.Stage, r.Reason = "STOPPED", stage, reason
	return r
}

func contextReason(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	return "CANCELLED"
}

func validClaim(r agentoutbox.Record, c agentoutbox.Claim, o Options) bool {
	// These are closed structural comparisons, never a replacement for native
	// current source/subject/fence/time checks in the original Store.
	if agentoutbox.ValidateRecord(r) != nil || agentoutbox.ValidateClaim(c) != nil ||
		r.State != agentoutbox.Leased || r.LeaseUntil == nil || r.Event.Subject != o.Subject ||
		c.Subject != o.Subject || c.EventID != r.Event.EventID || c.AgentID != r.Event.AgentID ||
		c.WorkerID != o.WorkerID || c.HandlerVersion != o.Handler || r.LeaseOwner != c.WorkerID ||
		r.Fence != c.Fence || !r.LeaseUntil.Equal(c.LeaseUntil) {
		return false
	}
	return true
}

func validReceipt(r agentoutbox.Record, c agentoutbox.Claim, p agentoutbox.ConsumerRecord) bool {
	if agentoutbox.ValidateConsumerRecord(p) != nil || p.EventID != c.EventID ||
		p.Subject != c.Subject || p.HandlerVersion != c.HandlerVersion || p.Fence != c.Fence ||
		p.Attempt != r.Attempt || p.UpdatedAt.Before(r.UpdatedAt) ||
		!p.UpdatedAt.Before(c.LeaseUntil) || !p.UpdatedAt.Before(r.Event.ExpiresAt) {
		return false
	}
	switch p.State {
	case agentoutbox.Pending, agentoutbox.MemoryComplete:
		return c.HandlerVersion == agentoutbox.MemoryHandler || c.HandlerVersion == agentoutbox.PreferenceHandler
	case agentoutbox.Unavailable, agentoutbox.Invalidated, agentoutbox.Expired, agentoutbox.DeadLetter:
		return true
	}
	return false
}

// Run makes at most Batch claims and never retries an unknown operation. The
// native Store is the only source of committed control receipts. Its local
// maintenance role is distinct from ordinary human or Agent authorization.
func Run(parent context.Context, s Store, o Options) Report {
	out := NewReport("STOPPED", "CONFIGURATION", "INVALID_CONFIGURATION")
	if parent == nil || missing(s) || !ValidateOptions(o) {
		return out
	}
	ctx, cancel := context.WithTimeout(parent, o.Timeout)
	defer cancel()
	for i := 0; i < o.Batch; i++ {
		if ctx.Err() != nil {
			return stop(out, "BEFORE_CLAIM", contextReason(ctx))
		}
		record, claim, e := s.ClaimAgentOutboxControlForSubject(ctx, o.Subject, o.WorkerID, o.Handler)
		// Empty ErrUnavailable can accompany an original native terminal update;
		// it is not a confirmed receipt and cannot be counted or silently retried.
		if e == agentoutbox.ErrNotFound && reflect.DeepEqual(record, agentoutbox.Record{}) && claim == (agentoutbox.Claim{}) {
			out.Status, out.Stage, out.Reason = "FINISHED", "CLAIM", "NO_ELIGIBLE_WORK"
			return out
		}
		if e == agentoutbox.ErrDispatchBusy && reflect.DeepEqual(record, agentoutbox.Record{}) && claim == (agentoutbox.Claim{}) {
			if ctx.Err() != nil {
				return stop(out, "CLAIM", contextReason(ctx))
			}
			out.Status, out.Stage, out.Reason = "FINISHED", "CLAIM", "DISPATCH_CAPACITY_BUSY"
			return out
		}
		if e != nil {
			if ctx.Err() != nil {
				return stop(out, "CLAIM", contextReason(ctx))
			}
			return stop(out, "CLAIM", "CLAIM_UNCONFIRMED")
		}
		if !validClaim(record, claim, o) {
			return stop(out, "CLAIM", "INVALID_CLAIM")
		}
		if ctx.Err() != nil {
			return stop(out, "BEFORE_CONSUME", contextReason(ctx))
		}
		receipt, e := s.ConsumeAgentOutboxControl(ctx, claim)
		// The original native method commits a valid terminal control and returns
		// this exact sentinel. Wrapped/joined/empty/unknown results are not success.
		confirmed := e == agentoutbox.ErrUnavailable
		if o.Handler == agentoutbox.MemoryHandler || o.Handler == agentoutbox.PreferenceHandler {
			confirmed = e == nil
		}
		if !confirmed || !validReceipt(record, claim, receipt) {
			if ctx.Err() != nil {
				return stop(out, "CONSUME", contextReason(ctx))
			}
			return stop(out, "CONSUME", "CONSUME_UNCONFIRMED")
		}
		out.ConfirmedReceipts++
		switch receipt.State {
		case agentoutbox.MemoryComplete:
			out.Counts.MaintenanceComplete++
		case agentoutbox.Pending:
			out.Counts.MaintenanceProgress++
		case agentoutbox.Unavailable:
			out.Counts.Unavailable++
		case agentoutbox.Invalidated:
			out.Counts.Invalidated++
		case agentoutbox.Expired:
			out.Counts.Expired++
		case agentoutbox.DeadLetter:
			out.Counts.DeadLetter++
		}
	}
	out.Status, out.Stage, out.Reason = "FINISHED", "BATCH", "BATCH_LIMIT_REACHED"
	return out
}
