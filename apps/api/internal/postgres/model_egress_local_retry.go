package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
	"github.com/jackc/pgx/v5"
)

var _ modelegressbudget.LocalRetryPort = (*Store)(nil)

// This sealed in-process receipt is not persistable or reconstructible from
// wire. The native store checks it as well as current original sources every
// time; its deadline alone is not permission to invoke a model.
type nativeLocalRetryProof struct {
	store          *Store
	access         agentevent.Access
	owner, session string
	routes         []nativeLocalRetryRoute
	checkpoint     modelegressbudget.LocalReleaseCheckpoint
}
type nativeLocalRetryRoute struct {
	binding        modelegressbudget.LocalRetryBinding
	preview        modelegressbudget.Preview
	taskGeneration string
}

func (p *nativeLocalRetryProof) Remaining(now time.Time) time.Duration {
	if p == nil || len(p.routes) == 0 {
		return 0
	}
	return p.checkpoint.Remaining(now)
}
func (*nativeLocalRetryProof) MarshalJSON() ([]byte, error) {
	return nil, modelegressbudget.ErrServerOnly
}
func (p *nativeLocalRetryProof) UnmarshalJSON([]byte) error {
	*p = nativeLocalRetryProof{}
	return modelegressbudget.ErrServerOnly
}

func retryMinimum(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func (s *Store) readNativeRetryRoute(ctx context.Context, tx pgx.Tx, a agentevent.Access, owner, session string, b modelegressbudget.LocalRetryBinding) (nativeLocalRetryRoute, time.Time, error) {
	var out nativeLocalRetryRoute
	v, e := readEgressPreview(ctx, tx, b.Input.PreviewID)
	if e != nil {
		return out, time.Time{}, e
	}
	if v.status != "APPROVED" || v.owner != owner || v.session != session || v.in.RootTraceID != b.Input.RootTraceID || v.in.TaskID != b.Input.TaskID {
		return out, time.Time{}, modelegressbudget.ErrDenied
	}
	p, sourceSession, rootExpiry, e := s.buildEgressPreview(ctx, tx, a, v.in)
	if e != nil {
		return out, time.Time{}, e
	}
	if !matchEgressPreview(v, p, sourceSession) || b.Destination.Key != p.Price.Destination || b.Destination.Region != p.Price.Region || b.Destination.Retention != p.Price.Retention {
		return out, time.Time{}, modelegressbudget.ErrDenied
	}
	var generation string
	// buildEgressPreview already holds the Task SHARE lock. Retain its actual
	// row generation too, so restore of visible query/updatedAt cannot revive a
	// previously captured retry plan while it is waiting.
	if e = tx.QueryRow(ctx, `SELECT xmin::text FROM agent_tasks WHERE id=$1`, b.Input.TaskID).Scan(&generation); e != nil {
		return out, time.Time{}, egressError(e)
	}
	return nativeLocalRetryRoute{binding: b, preview: p, taskGeneration: generation}, retryMinimum(retryMinimum(rootExpiry, p.Price.ExpiresAt), p.Request.DeadlineAt), nil
}

// CaptureOwnLocalRetryPlan checks the complete original approved set before
// the first attempt. Prices/digests may differ; source, query/config binding,
// actual actor/session and Task/root must be the same.
func (s *Store) CaptureOwnLocalRetryPlan(ctx context.Context, a agentevent.Access, bindings []modelegressbudget.LocalRetryBinding, c *agentfeature.Controller) (modelegressbudget.LocalRetryProof, error) {
	if len(bindings) < 1 || len(bindings) > modelresilience.MaxAttempts {
		return nil, modelegressbudget.ErrInvalid
	}
	bindings = append([]modelegressbudget.LocalRetryBinding(nil), bindings...)
	seen := map[string]bool{}
	for _, b := range bindings {
		i := b.Input
		if !egressUUID(i.OperationID) || !egressUUID(i.PreviewID) || !egressUUID(i.RootTraceID) || !egressUUID(i.TaskID) || seen[i.OperationID] {
			return nil, modelegressbudget.ErrInvalid
		}
		if i.RootTraceID != bindings[0].Input.RootTraceID || i.TaskID != bindings[0].Input.TaskID {
			return nil, modelegressbudget.ErrDenied
		}
		seen[i.OperationID] = true
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return nil, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return nil, e
	}
	proof := &nativeLocalRetryProof{store: s, access: a, owner: owner, session: session}
	var expiry time.Time
	for _, b := range bindings {
		route, bound, e := s.readNativeRetryRoute(ctx, tx, a, owner, session, b)
		if e != nil {
			return nil, e
		}
		if len(proof.routes) > 0 {
			first := proof.routes[0].preview
			if route.preview.BindingID != first.BindingID || route.preview.SourceToken != first.SourceToken || route.preview.AuthorityToken != first.AuthorityToken || route.preview.Request.Agent != first.Request.Agent {
				return nil, modelegressbudget.ErrDenied
			}
		}
		var present bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_budget_reservations WHERE operation_id=$1)`, b.Input.OperationID).Scan(&present); e != nil {
			return nil, egressError(e)
		}
		if present {
			r, e := readEgressReservation(ctx, tx, b.Input.OperationID, owner)
			if e != nil {
				return nil, e
			}
			if r.State != "RESERVED" || r.PreviewID != b.Input.PreviewID || r.RootTraceID != b.Input.RootTraceID || r.TaskID != b.Input.TaskID || r.RequestDigest != route.preview.RequestDigest {
				return nil, modelegressbudget.ErrConflict
			}
		}
		proof.routes = append(proof.routes, route)
		expiry = retryMinimum(expiry, bound)
	}
	first := proof.routes[0]
	cp, e := commitLocalAttemptCheckpoint(ctx, tx, session, first.binding.Input.OperationID, first.preview.Request, expiry, expiry, c, ticket)
	if e != nil {
		return nil, e
	}
	proof.checkpoint = cp
	if proof.Remaining(time.Now()) <= 0 {
		return nil, modelegressbudget.ErrDenied
	}
	return proof, nil
}

func retryRouteUnchanged(a, b nativeLocalRetryRoute) bool {
	x, y := a.preview, b.preview
	expected, e := json.Marshal(x.Request)
	actual, f := json.Marshal(y.Request)
	return e == nil && f == nil && bytes.Equal(expected, actual) && a.binding == b.binding && a.taskGeneration == b.taskGeneration && x.BindingID == y.BindingID && x.SourceToken == y.SourceToken && x.AuthorityToken == y.AuthorityToken && x.RequestDigest == y.RequestDigest && x.PriceVersion == y.PriceVersion && x.Price.ExpiresAt.Equal(y.Price.ExpiresAt)
}
func (s *Store) checkedNativeRetryProof(a agentevent.Access, p modelegressbudget.LocalRetryProof) (*nativeLocalRetryProof, error) {
	proof, ok := p.(*nativeLocalRetryProof)
	if !ok || proof == nil || proof.store != s || proof.access != a || len(proof.routes) == 0 || proof.Remaining(time.Now()) <= 0 {
		return nil, modelegressbudget.ErrDenied
	}
	return proof, nil
}

func (s *Store) RevalidateOwnLocalRetryPlan(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	return s.revalidateNativeRetry(ctx, a, p, nil, "", modelgateway.Request{}, modelegressbudget.LocalAttemptDestination{}, c)
}
func (s *Store) CheckOwnLocalRetryFailure(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, f modelegressbudget.LocalRetryFailure, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	if _, _, ok := f.OperationRequest(); !ok {
		return modelegressbudget.LocalReleaseCheckpoint{}, modelegressbudget.ErrDenied
	}
	return s.revalidateNativeRetry(ctx, a, p, &f, "", modelgateway.Request{}, modelegressbudget.LocalAttemptDestination{}, c)
}
func (s *Store) CheckOwnLocalRetryDispatch(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, operation string, request modelgateway.Request, destination modelegressbudget.LocalAttemptDestination, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	if !egressUUID(operation) {
		return modelegressbudget.LocalReleaseCheckpoint{}, modelegressbudget.ErrInvalid
	}
	return s.revalidateNativeRetry(ctx, a, p, nil, operation, request, destination, c)
}
func (s *Store) revalidateNativeRetry(ctx context.Context, a agentevent.Access, p modelegressbudget.LocalRetryProof, failure *modelegressbudget.LocalRetryFailure, dispatchID string, dispatchRequest modelgateway.Request, destination modelegressbudget.LocalAttemptDestination, c *agentfeature.Controller) (modelegressbudget.LocalReleaseCheckpoint, error) {
	var empty modelegressbudget.LocalReleaseCheckpoint
	proof, e := s.checkedNativeRetryProof(a, p)
	if e != nil {
		return empty, e
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return empty, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return empty, e
	}
	if owner != proof.owner || session != proof.session {
		return empty, modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return empty, e
	}
	expiry := proof.checkpoint.ValidUntil() // Never extend the originally captured set.
	var failedRoute *nativeLocalRetryRoute
	failureID, failureRequest := "", modelgateway.Request{}
	if failure != nil {
		var ok bool
		failureID, failureRequest, ok = failure.OperationRequest()
		if !ok {
			return empty, modelegressbudget.ErrDenied
		}
	}
	if dispatchID != "" {
		failureID, failureRequest = dispatchID, dispatchRequest
	}
	for _, old := range proof.routes {
		current, bound, e := s.readNativeRetryRoute(ctx, tx, a, owner, session, old.binding)
		if e != nil {
			return empty, e
		}
		if !retryRouteUnchanged(old, current) {
			return empty, modelegressbudget.ErrDenied
		}
		expiry = retryMinimum(expiry, bound)
		if (failure != nil || dispatchID != "") && old.binding.Input.OperationID == failureID {
			copy := old
			failedRoute = &copy
		}
	}
	if failure != nil || dispatchID != "" {
		if failedRoute == nil {
			return empty, modelegressbudget.ErrDenied
		}
		expected, _ := json.Marshal(failedRoute.preview.Request)
		actual, _ := json.Marshal(failureRequest)
		if !bytes.Equal(expected, actual) {
			return empty, modelegressbudget.ErrDenied
		}
		r, e := readEgressReservation(ctx, tx, failureID, owner)
		if e != nil {
			return empty, e
		}
		b := failedRoute.binding.Input
		stateOK := r.State == "UNKNOWN" || r.State == "SETTLED"
		if dispatchID != "" {
			stateOK = r.State == "IN_FLIGHT"
			if destination != failedRoute.binding.Destination {
				return empty, modelegressbudget.ErrDenied
			}
		}
		if !stateOK || r.PreviewID != b.PreviewID || r.RootTraceID != b.RootTraceID || r.TaskID != b.TaskID || r.RequestDigest != failedRoute.preview.RequestDigest || r.PriceVersion != failedRoute.preview.PriceVersion {
			return empty, modelegressbudget.ErrDenied
		}
	}
	first := proof.routes[0]
	if dispatchID != "" {
		first = *failedRoute
	}
	cp, e := commitLocalAttemptCheckpoint(ctx, tx, session, first.binding.Input.OperationID, first.preview.Request, expiry, expiry, c, ticket)
	if e != nil {
		return empty, e
	}
	if proof.Remaining(time.Now()) <= 0 {
		return empty, modelegressbudget.ErrDenied
	}
	return cp, nil
}
