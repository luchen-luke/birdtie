package modelegressbudget

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// AttemptControl is accounting metadata only. It cannot recover an answer or
// grant permission to send a request. No prompt, digest or private source is included.
type AttemptControl struct {
	OperationID     string `json:"operationId"`
	State           string `json:"state"`
	ExecutionStatus string `json:"executionStatus"`
	ModelAccess     string `json:"modelAccess"`
}

// LocalAttemptPort reuses the original 062 ledger. Implementations must perform
// current native checks; a client-constructed receipt is never an authority.
type LocalAttemptPort interface {
	ReserveOwnModelAttempt(context.Context, agentevent.Access, ReserveInput, *agentfeature.Controller) (Reservation, error)
	BeginOwnLocalModelAttempt(context.Context, agentevent.Access, string, *agentfeature.Controller) (modelgateway.Request, error)
	SettleOwnLocalModelAttempt(context.Context, agentevent.Access, string, LocalUsage) (Reservation, error)
	CancelOwnReservedModelAttempt(context.Context, agentevent.Access, string) (Reservation, error)
	ReadOwnLocalModelAttempt(context.Context, agentevent.Access, string) (AttemptControl, error)
	CheckOwnLocalModelAttemptDispatch(context.Context, agentevent.Access, string, *agentfeature.Controller, modelgateway.Request, LocalAttemptDestination) (LocalReleaseCheckpoint, error)
	ReleaseOwnLocalModelAttempt(context.Context, agentevent.Access, string, *agentfeature.Controller, modelgateway.Request, LocalResultBuffer) (LocalReleaseCheckpoint, error)
}

// This server-only clock checkpoint is a deadline bound, not source authority.
// The native port must still check the original preview and all current sources.
type LocalReleaseCheckpoint struct {
	observedAt, validUntil, monotonicUntil time.Time
	operation                              string
	requestHash                            [32]byte
}

func NewLocalReleaseCheckpoint(operation string, request modelgateway.Request, observedAt, validUntil, queryStartedAt time.Time) LocalReleaseCheckpoint {
	if operation == "" || queryStartedAt.IsZero() || observedAt.IsZero() || !validUntil.After(observedAt) {
		return LocalReleaseCheckpoint{}
	}
	wire, e := json.Marshal(request)
	if e != nil {
		return LocalReleaseCheckpoint{}
	}
	return LocalReleaseCheckpoint{observedAt: observedAt, validUntil: validUntil, monotonicUntil: queryStartedAt.Add(validUntil.Sub(observedAt)), operation: operation, requestHash: sha256.Sum256(wire)}
}
func (c LocalReleaseCheckpoint) ValidAt(now time.Time) bool {
	return !c.observedAt.IsZero() && !now.IsZero() && now.Before(c.monotonicUntil)
}
func (c LocalReleaseCheckpoint) ValidFor(operation string, request modelgateway.Request, now time.Time) bool {
	wire, e := json.Marshal(request)
	return e == nil && operation == c.operation && c.requestHash == sha256.Sum256(wire) && c.ValidAt(now)
}
func (c LocalReleaseCheckpoint) Remaining(now time.Time) time.Duration {
	return c.monotonicUntil.Sub(now)
}
func (c LocalReleaseCheckpoint) ValidUntil() time.Time      { return c.validUntil }
func (LocalReleaseCheckpoint) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (c *LocalReleaseCheckpoint) UnmarshalJSON([]byte) error {
	*c = LocalReleaseCheckpoint{}
	return ErrServerOnly
}

// LocalResultBuffer preserves the provenance of an in-process normalized
// result. It is not an authorization: the original native approval is still
// rechecked. No JSON decoder, exported constructor or mutation can create it.
type LocalResultBuffer struct {
	operation   string
	requestHash [32]byte
	encoded     []byte
}

func (LocalResultBuffer) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (b *LocalResultBuffer) UnmarshalJSON([]byte) error {
	*b = LocalResultBuffer{}
	return ErrServerOnly
}
func (b LocalResultBuffer) EncodedFor(operation string, request modelgateway.Request) ([]byte, error) {
	wire, e := json.Marshal(request)
	if e != nil || b.operation == "" || b.operation != operation || b.requestHash != sha256.Sum256(wire) || len(b.encoded) == 0 || len(b.encoded) > modelgateway.MaxResultBytes {
		return nil, ErrDenied
	}
	return append([]byte(nil), b.encoded...), nil
}
func localResultBuffer(operation string, request modelgateway.Request, encoded []byte) LocalResultBuffer {
	wire, _ := json.Marshal(request)
	return LocalResultBuffer{operation: operation, requestHash: sha256.Sum256(wire), encoded: append([]byte(nil), encoded...)}
}

// LocalAttemptOutcome is server-only and contains an ephemeral, already encoded
// synthetic result. It is not a persisted response or a production model receipt.
type LocalAttemptOutcome struct {
	Control       AttemptControl
	EncodedResult []byte
	retryFailure  *LocalRetryFailure
	outputFailure *LocalOutputFailure
	// Deadline metadata only: never authority. Retained privately so a later
	// idle refresh cannot extend any shorter native bound this attempt observed.
	monotonicDeadline time.Time
}

func (o *LocalAttemptOutcome) tightenDeadline(until time.Time) {
	if !until.IsZero() && (o.monotonicDeadline.IsZero() || until.Before(o.monotonicDeadline)) {
		o.monotonicDeadline = until
	}
}

func (LocalAttemptOutcome) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (o *LocalAttemptOutcome) UnmarshalJSON([]byte) error {
	*o = LocalAttemptOutcome{}
	return ErrServerOnly
}

type LocalAttemptDriver struct {
	validateOutput bool
	port           LocalAttemptPort
	adapter        *capturedLocalAdapter
	gate           *agentfeature.Controller
}

// LocalAttemptDestination is an adapter's server-local declaration, not a grant
// or verified provider capability. Native checks compare it to the exact price
// approved by the person before any request is passed to the adapter.
type LocalAttemptDestination struct {
	Key       modelcapability.Key
	Region    modelcapability.Region
	Retention string
}
type LocalAttemptAdapter interface {
	modelgateway.ProviderAdapter
	LocalDestination() LocalAttemptDestination
}
type capturedLocalAdapter struct {
	original      LocalAttemptAdapter
	descriptor    modelgateway.ProviderDescriptor
	destination   LocalAttemptDestination
	beforeCall    func() error
	delegated     bool
	failureCode   string
	delegateUntil time.Time
	blockedErr    error
}

func (a *capturedLocalAdapter) current() bool {
	return a.original.Descriptor() == a.descriptor && a.original.LocalDestination() == a.destination
}
func (a *capturedLocalAdapter) Descriptor() modelgateway.ProviderDescriptor { return a.descriptor }
func (a *capturedLocalAdapter) Complete(ctx context.Context, r modelgateway.ProviderRequest) ([]byte, error) {
	if !a.current() {
		return nil, ErrUnavailable
	}
	if a.beforeCall == nil {
		return nil, ErrUnavailable
	}
	if err := a.beforeCall(); err != nil {
		a.blockedErr = err
		return nil, err
	}
	if !a.current() {
		return nil, ErrUnavailable
	}
	// A declaration getter may itself wait: recheck the original monotonic
	// deadline and gate after it, immediately before handing over the query.
	if err := a.beforeCall(); err != nil {
		a.blockedErr = err
		return nil, err
	}
	// The final native check may tighten the original checkpoint further.
	// Pass that bound to the actual delegate, never just to outer validation.
	if !a.delegateUntil.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, a.delegateUntil)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		a.blockedErr = err
		return nil, err
	}
	a.delegated = true
	raw, err := a.original.Complete(ctx, r)
	// A later normal idle refresh cannot extend the bound already captured
	// for this delegate. Check before cancelling our own child context.
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !a.delegateUntil.IsZero() && !time.Now().Before(a.delegateUntil) {
		return nil, context.DeadlineExceeded
	}
	if !a.current() {
		return nil, ErrUnavailable
	}
	var provider modelgateway.ProviderError
	var pointer *modelgateway.ProviderError
	if errors.As(err, &pointer) && pointer != nil {
		provider = *pointer
	} else {
		_ = errors.As(err, &provider)
	}
	if provider.Code == "RATE_LIMIT" || provider.Code == "TEMPORARY" {
		a.failureCode = provider.Code
	}
	return raw, err
}

// Only a separate OFFLINE_CONTRACT adapter can be supplied. This does not
// register a transport in LiveGateway, HTTP, main, Service.Complete or AgentRun.
func NewLocalAttemptDriver(port LocalAttemptPort, adapter LocalAttemptAdapter, gate *agentfeature.Controller) *LocalAttemptDriver {
	if port == nil || adapter == nil || gate == nil {
		return nil
	}
	for _, object := range []any{port, adapter} {
		v := reflect.ValueOf(object)
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice, reflect.Chan:
			if v.IsNil() {
				return nil
			}
		}
	}
	a := &capturedLocalAdapter{original: adapter, descriptor: adapter.Descriptor(), destination: adapter.LocalDestination()}
	if a.destination.Key.Provider != a.descriptor.ProviderID || a.destination.Key.Model != a.descriptor.ModelID || a.destination.Key.Version != a.descriptor.ModelVersion {
		return nil
	}
	// The harness captures this immutable wrapper's descriptor, never a second
	// potentially different original descriptor. Recheck construction-time drift.
	_, err := modelgateway.NewOfflineHarness(a)
	if err != nil || !a.current() {
		return nil
	}
	return &LocalAttemptDriver{port: port, adapter: a, gate: gate}
}

func control(r Reservation) AttemptControl {
	return AttemptControl{OperationID: r.OperationID, State: r.State, ExecutionStatus: "UNAVAILABLE", ModelAccess: "UNAVAILABLE"}
}

// Recover reads the same original operation only. IN_FLIGHT, UNKNOWN and
// SETTLED are control states, never instructions to invoke the adapter again.
func (d *LocalAttemptDriver) Recover(ctx context.Context, a agentevent.Access, operation string) (AttemptControl, error) {
	if d == nil || d.port == nil || ctx == nil {
		return AttemptControl{}, ErrUnavailable
	}
	return d.port.ReadOwnLocalModelAttempt(ctx, a, operation)
}

// Once performs at most one local adapter call. Each new attempt needs its own
// original native reservation; retries/fallback orchestration is not installed.
func (d *LocalAttemptDriver) Once(ctx context.Context, a agentevent.Access, in ReserveInput) (LocalAttemptOutcome, error) {
	if d == nil || ctx == nil {
		return LocalAttemptOutcome{}, ErrUnavailable
	}
	ticket, err := d.gate.Capture(agentfeature.Enrichment)
	if err != nil {
		return LocalAttemptOutcome{}, ErrUnavailable
	}
	return d.onceWithTicket(ctx, a, in, ticket, nil)
}

type localDispatchCheck func(context.Context, agentevent.Access, string, *agentfeature.Controller, modelgateway.Request, LocalAttemptDestination) (LocalReleaseCheckpoint, error)

func (d *LocalAttemptDriver) onceWithTicket(ctx context.Context, a agentevent.Access, in ReserveInput, ticket agentfeature.Ticket, check localDispatchCheck) (LocalAttemptOutcome, error) {
	var out LocalAttemptOutcome
	if d == nil || ctx == nil || !d.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	r, err := d.port.ReserveOwnModelAttempt(ctx, a, in, d.gate)
	if err != nil {
		return out, err
	} // Unknown commit: no Begin and no automatic replay.
	out.Control = control(r)
	if r.OperationID != in.OperationID || r.PreviewID != in.PreviewID || r.RootTraceID != in.RootTraceID || r.TaskID != in.TaskID || r.State != "RESERVED" {
		return out, ErrConflict
	}
	if !d.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	req, err := d.port.BeginOwnLocalModelAttempt(ctx, a, in.OperationID, d.gate)
	if err != nil {
		return out, err
	} // Unknown Begin: do not invoke or blindly cancel.
	out.Control.State = "IN_FLIGHT"
	if !d.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	if !d.adapter.current() {
		return out, ErrUnavailable
	}
	if check == nil {
		check = d.port.CheckOwnLocalModelAttemptDispatch
	}
	checkpoint, err := check(ctx, a, in.OperationID, d.gate, req, d.adapter.destination)
	if err != nil {
		return out, err
	}
	if !d.gate.Current(ticket) || !d.adapter.current() {
		return out, ErrUnavailable
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if !checkpoint.ValidFor(in.OperationID, req, time.Now()) {
		return out, ErrDenied
	}
	initialNow := time.Now()
	out.tightenDeadline(initialNow.Add(checkpoint.Remaining(initialNow)))
	callAdapter := *d.adapter // Per-attempt fence; concurrent attempts never share it.
	callContext := ctx
	callAdapter.beforeCall = func() error {
		if callContext.Err() != nil {
			return callContext.Err()
		}
		if !d.gate.Current(ticket) {
			return ErrUnavailable
		}
		if !checkpoint.ValidFor(in.OperationID, req, time.Now()) {
			return ErrDenied
		}
		// Use the selected native fence after each potentially waiting getter:
		// standalone original operation, or the complete original retry plan.
		{
			current, e := check(callContext, a, in.OperationID, d.gate, req, d.adapter.destination)
			if e != nil {
				return e
			}
			if !current.ValidFor(in.OperationID, req, time.Now()) || !checkpoint.ValidFor(in.OperationID, req, time.Now()) || !d.gate.Current(ticket) {
				return ErrDenied
			}
			now := time.Now()
			until := now.Add(current.Remaining(now))
			originalUntil := now.Add(checkpoint.Remaining(now))
			if originalUntil.Before(until) {
				until = originalUntil
			}
			if callAdapter.delegateUntil.IsZero() || until.Before(callAdapter.delegateUntil) {
				callAdapter.delegateUntil = until
			}
			out.tightenDeadline(until)
		}
		if callContext.Err() != nil {
			return callContext.Err()
		}
		return nil
	}
	harness, err := modelgateway.NewOfflineHarness(&callAdapter)
	if err != nil {
		return out, ErrUnavailable
	}
	callStarted := time.Now()
	remaining := checkpoint.Remaining(callStarted)
	if remaining <= 0 {
		return out, ErrDenied
	}
	var callCancel context.CancelFunc
	callContext, callCancel = context.WithDeadline(ctx, callStarted.Add(remaining))
	result, callErr := harness.Complete(callContext, req)
	callCancel()
	usage := modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}
	if callErr == nil {
		usage = result.Usage
	}
	localUsage, err := NewLocalUsage(usage)
	if err != nil {
		return out, err
	}
	r, err = d.port.SettleOwnLocalModelAttempt(ctx, a, in.OperationID, localUsage)
	if err != nil {
		return out, err
	} // Accounting is unresolved, do not release a result.
	out.Control = control(r)
	if !out.monotonicDeadline.IsZero() && !time.Now().Before(out.monotonicDeadline) {
		return out, ErrDenied
	}
	if callErr != nil {
		// The harness intentionally normalizes adapter errors. A native fence
		// which stopped before delegation is not a provider failure/retry fact.
		// Preserve its precise denial only after conservative accounting confirms.
		if !callAdapter.delegated && callAdapter.blockedErr != nil {
			return out, callAdapter.blockedErr
		}
		if d.validateOutput && modelgateway.RepairableOutputJSON(callErr) && callAdapter.delegated && ctx.Err() == nil && d.gate.Current(ticket) && callAdapter.current() && checkpoint.ValidFor(in.OperationID, req, time.Now()) &&
			r.OperationID == in.OperationID && r.PreviewID == in.PreviewID && r.RootTraceID == in.RootTraceID && r.TaskID == in.TaskID && r.State == "UNKNOWN" {
			out.outputFailure = newLocalOutputFailure(in.OperationID, req, "INVALID_JSON")
			return out, modelgateway.ErrOutputSchema
		}
		// Only this attempt's actual adapter response can sign a retry fact.
		// A port error, late/invalid harness response or lost Settle cannot.
		var provider modelgateway.ProviderError
		if errors.As(callErr, &provider) && provider.Retryable && callAdapter.delegated && callAdapter.failureCode == provider.Code &&
			(provider.Code == "RATE_LIMIT" || provider.Code == "TEMPORARY") && ctx.Err() == nil && d.gate.Current(ticket) && callAdapter.current() && checkpoint.ValidFor(in.OperationID, req, time.Now()) &&
			r.OperationID == in.OperationID && r.PreviewID == in.PreviewID && r.RootTraceID == in.RootTraceID && r.TaskID == in.TaskID && r.State == "UNKNOWN" {
			delay, present, valid := modelgateway.RetryAfter(callErr)
			if !present || valid {
				out.retryFailure = newLocalRetryFailure(in.OperationID, req, provider.Code, delay, present)
			}
		}
		return out, callErr
	}
	if result.Mode != modelgateway.OfflineContract || result.RunID != req.RunID || result.Agent != req.Agent {
		return out, ErrInvalid
	}
	if d.validateOutput {
		switch result.Status {
		case modelgateway.Refused:
			return out, modelgateway.ErrOutputRefused
		case modelgateway.Truncated:
			if callAdapter.delegated && ctx.Err() == nil && d.gate.Current(ticket) && callAdapter.current() && checkpoint.ValidFor(in.OperationID, req, time.Now()) && r.OperationID == in.OperationID && r.PreviewID == in.PreviewID && r.RootTraceID == in.RootTraceID && r.TaskID == in.TaskID && (r.State == "SETTLED" || r.State == "UNKNOWN") {
				out.outputFailure = newLocalOutputFailure(in.OperationID, req, "TRUNCATED")
			}
			return out, modelgateway.ErrOutputTruncated
		case modelgateway.Unavailable:
			return out, ErrUnavailable
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > modelgateway.MaxResultBytes {
		return out, ErrInvalid
	}
	// All potentially expensive encoding precedes the final native authority
	// check. Settle/owner identity alone never permits a private answer release.
	if !d.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	buffer := localResultBuffer(in.OperationID, req, encoded)
	checkpoint, err = d.port.ReleaseOwnLocalModelAttempt(ctx, a, in.OperationID, d.gate, req, buffer)
	if err != nil {
		return out, err
	}
	if !d.gate.Current(ticket) {
		return out, ErrUnavailable
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if !checkpoint.ValidFor(in.OperationID, req, time.Now()) {
		return out, ErrDenied
	}
	releasedNow := time.Now()
	out.tightenDeadline(releasedNow.Add(checkpoint.Remaining(releasedNow)))
	if !time.Now().Before(out.monotonicDeadline) {
		return out, ErrDenied
	}
	if modelgateway.ValidateRequest(req, time.Now()) != nil {
		return out, ErrDenied
	}
	out.EncodedResult = encoded
	return out, nil
}
