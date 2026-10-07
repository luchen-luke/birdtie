package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

var _ modelegressbudget.LocalAttemptPort = (*Store)(nil)

// CheckOwnLocalModelAttemptDispatch rechecks the original approval and the
// local adapter's declared destination before it receives the private query.
// A declaration is not evidence of real provider region or retention behavior.
func (s *Store) CheckOwnLocalModelAttemptDispatch(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, request modelgateway.Request, destination modelegressbudget.LocalAttemptDestination) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	if !egressUUID(operation) {
		return empty, modelegressbudget.ErrInvalid
	}
	ticket, err := egressTicket(c)
	if err != nil {
		return empty, err
	}
	tx, err := s.beginEgress(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	owner, session, err := s.egressOwner(ctx, tx, a)
	if err != nil {
		return empty, err
	}
	if err = egressOwnerLock(ctx, tx, owner); err != nil {
		return empty, err
	}
	if err = denyLinkedModelOperation(ctx, tx, operation); err != nil {
		return empty, err
	}
	out, err := s.checkLocalModelDispatchTx(ctx, tx, a, operation, c, ticket, request, destination, owner, session)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, egressError(err)
	}
	return out, nil
}
func (s *Store) checkLocalModelDispatchTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, operation string, c *agentfeature.Controller, ticket agentfeature.Ticket, request modelgateway.Request, destination modelegressbudget.LocalAttemptDestination, owner, session string) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	r, err := readLocalEgressReservation(ctx, tx, operation, owner)
	if err != nil {
		return empty, err
	}
	if r.State != "IN_FLIGHT" {
		return empty, modelegressbudget.ErrDenied
	}
	v, err := readEgressPreview(ctx, tx, r.PreviewID)
	if err != nil {
		return empty, err
	}
	if v.status != "APPROVED" || v.owner != owner || v.session != session || r.RootTraceID != v.in.RootTraceID || r.TaskID != v.in.TaskID {
		return empty, modelegressbudget.ErrDenied
	}
	p, sourceSession, rootExpiry, err := s.buildEgressPreview(ctx, tx, a, v.in)
	if err != nil {
		return empty, err
	}
	if !matchEgressPreview(v, p, sourceSession) || r.RequestDigest != p.RequestDigest || r.PriceVersion != p.PriceVersion {
		return empty, modelegressbudget.ErrDenied
	}
	expected, err := json.Marshal(p.Request)
	if err != nil {
		return empty, modelegressbudget.ErrInvalid
	}
	actual, err := json.Marshal(request)
	if err != nil || !bytes.Equal(expected, actual) || destination.Key != p.Price.Destination || destination.Region != p.Price.Region || destination.Retention != p.Price.Retention {
		return empty, modelegressbudget.ErrDenied
	}
	return captureLocalAttemptCheckpoint(ctx, tx, session, operation, request, rootExpiry, p.Price.ExpiresAt, c, ticket)
}

// ReadOwnLocalModelAttempt recovers accounting metadata from the original 062
// operation. Current ownership permits this read even after the source is gone;
// it intentionally does not expose the prompt or grant answer-release permission.
func (s *Store) ReadOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, operation string) (modelegressbudget.AttemptControl, error) {
	var out modelegressbudget.AttemptControl
	if !egressUUID(operation) {
		return out, modelegressbudget.ErrInvalid
	}
	tx, err := s.beginEgress(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	owner, session, err := s.egressOwner(ctx, tx, a)
	if err != nil {
		return out, err
	}
	r, err := readLocalEgressReservation(ctx, tx, operation, owner)
	if err != nil {
		return out, err
	}
	if err = egressFinish(ctx, tx, session, nil); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, egressError(err)
	}
	return modelegressbudget.AttemptControl{OperationID: r.OperationID, State: r.State, ExecutionStatus: "UNAVAILABLE", ModelAccess: "UNAVAILABLE"}, nil
}

// ReleaseOwnLocalModelAttempt checks the exact original approval after encoding.
// This is distinct from accounting Settle, which must remain available when
// sources/rollout are withdrawn. It writes no result, new grant, Run or audit.
func (s *Store) ReleaseOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, request modelgateway.Request, buffer modelegressbudget.LocalResultBuffer) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	result, err := validateLocalModelResult(operation, request, buffer)
	if err != nil {
		return empty, err
	}
	// A standalone operation has no captured domain projection. Nonempty
	// entity proposals require the original native ModelRun's private scope.
	if err = modelgateway.ValidateEntityScope(result, nil); err != nil {
		return empty, err
	}
	ticket, err := egressTicket(c)
	if err != nil {
		return empty, err
	}
	tx, err := s.beginEgress(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)
	owner, session, err := s.egressOwner(ctx, tx, a)
	if err != nil {
		return empty, err
	}
	if err = egressOwnerLock(ctx, tx, owner); err != nil {
		return empty, err
	}
	if err = denyLinkedModelOperation(ctx, tx, operation); err != nil {
		return empty, err
	}
	out, err := s.releaseLocalModelResultTx(ctx, tx, a, operation, c, ticket, request, result, owner, session)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, egressError(err)
	}
	return out, nil
}
func validateLocalModelResult(operation string, request modelgateway.Request, buffer modelegressbudget.LocalResultBuffer) (modelgateway.Result, error) {
	encoded, bufferErr := buffer.EncodedFor(operation, request)
	if bufferErr != nil {
		return modelgateway.Result{}, bufferErr
	}
	if !egressUUID(operation) || len(encoded) == 0 || len(encoded) > modelgateway.MaxResultBytes {
		return modelgateway.Result{}, modelegressbudget.ErrInvalid
	}
	var result modelgateway.Result
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.SchemaVersion != modelgateway.ResultVersion || result.Mode != modelgateway.OfflineContract ||
		result.RunID != request.RunID || result.Agent != request.Agent || result.Candidate != nil || len(result.ToolProposals) != 0 {
		return modelgateway.Result{}, modelegressbudget.ErrInvalid
	}
	switch result.Status {
	case modelgateway.Completed, modelgateway.Refused, modelgateway.Truncated, modelgateway.Unavailable:
	default:
		return modelgateway.Result{}, modelegressbudget.ErrInvalid
	}
	return result, nil
}

func (s *Store) releaseLocalModelResultTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, operation string, c *agentfeature.Controller, ticket agentfeature.Ticket, request modelgateway.Request, result modelgateway.Result, owner, session string) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	r, err := readLocalEgressReservation(ctx, tx, operation, owner)
	if err != nil {
		return empty, err
	}
	if r.State != "SETTLED" && r.State != "UNKNOWN" {
		return empty, modelegressbudget.ErrDenied
	}
	if result.Usage.CostStatus != "UNKNOWN" {
		return empty, modelegressbudget.ErrDenied
	}
	switch result.Usage.Status {
	case "KNOWN":
		if r.State != "SETTLED" || result.Usage.InputTokens == nil || result.Usage.OutputTokens == nil || r.ReportedInput == nil || r.ReportedOutput == nil || *result.Usage.InputTokens != *r.ReportedInput || *result.Usage.OutputTokens != *r.ReportedOutput || *result.Usage.InputTokens < 0 || *result.Usage.OutputTokens < 0 || *result.Usage.InputTokens > r.Upper.InputTokens || *result.Usage.OutputTokens > r.Upper.OutputTokens {
			return empty, modelegressbudget.ErrDenied
		}
	case "UNKNOWN":
		if r.State != "UNKNOWN" || result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
			return empty, modelegressbudget.ErrDenied
		}
	default:
		return empty, modelegressbudget.ErrDenied
	}
	v, err := readEgressPreview(ctx, tx, r.PreviewID)
	if err != nil {
		return empty, err
	}
	if v.status != "APPROVED" || v.owner != owner || v.session != session || r.RootTraceID != v.in.RootTraceID || r.TaskID != v.in.TaskID {
		return empty, modelegressbudget.ErrDenied
	}
	p, sourceSession, rootExpiry, err := s.buildEgressPreview(ctx, tx, a, v.in)
	if err != nil {
		return empty, err
	}
	if !matchEgressPreview(v, p, sourceSession) || r.RequestDigest != p.RequestDigest || r.PriceVersion != p.PriceVersion {
		return empty, modelegressbudget.ErrDenied
	}
	expected, err := json.Marshal(p.Request)
	if err != nil {
		return empty, modelegressbudget.ErrInvalid
	}
	actual, err := json.Marshal(request)
	if err != nil || !bytes.Equal(expected, actual) {
		return empty, modelegressbudget.ErrDenied
	}
	if result.ProviderID != p.Price.Destination.Provider || result.ProviderModelVersion != p.Price.Destination.Model+"@"+p.Price.Destination.Version {
		return empty, modelegressbudget.ErrDenied
	}
	return captureLocalAttemptCheckpoint(ctx, tx, session, operation, request, rootExpiry, p.Price.ExpiresAt, c, ticket)
}

func commitLocalAttemptCheckpoint(ctx context.Context, tx pgx.Tx, session, operation string, request modelgateway.Request, rootExpiry, priceExpiry time.Time, c *agentfeature.Controller, ticket agentfeature.Ticket) (modelegressbudget.LocalReleaseCheckpoint, error) {
	out, e := captureLocalAttemptCheckpoint(ctx, tx, session, operation, request, rootExpiry, priceExpiry, c, ticket)
	if e != nil {
		return modelegressbudget.LocalReleaseCheckpoint{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.LocalReleaseCheckpoint{}, egressError(e)
	}
	return out, nil
}
func captureLocalAttemptCheckpoint(ctx context.Context, tx pgx.Tx, session, operation string, request modelgateway.Request, rootExpiry, priceExpiry time.Time, c *agentfeature.Controller, ticket agentfeature.Ticket) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	// All native source locks remain held. This final statement records the
	// earliest original bound using one PG clock after every source/lock wait.
	var valid bool
	var observed, expiry time.Time
	queryStartedAt := time.Now() // Monotonic anchor before the final PG-clock query.
	err := tx.QueryRow(ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT revoked_at IS NULL AND expires_at>n AND idle_expires_at>n,n,
 LEAST(expires_at,idle_expires_at,$2::timestamptz,$3::timestamptz,$4::timestamptz)
 FROM sessions CROSS JOIN c WHERE id=$1`, session, request.DeadlineAt, rootExpiry, priceExpiry).Scan(&valid, &observed, &expiry)
	if err != nil {
		return empty, egressError(err)
	}
	if !valid || !expiry.After(observed) || modelgateway.ValidateRequest(request, observed) != nil {
		return empty, modelegressbudget.ErrDenied
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if !c.Current(ticket) {
		return empty, modelegressbudget.ErrUnavailable
	}
	checkpoint := modelegressbudget.NewLocalReleaseCheckpoint(operation, request, observed, expiry, queryStartedAt)
	return checkpoint, nil
}
