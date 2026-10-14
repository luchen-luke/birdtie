package postgres

import (
	"context"
	"encoding/json"
	"errors"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	mb "github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
	"time"
)

func visualOperation(ctx context.Context, tx pgx.Tx, id, owner string) (out av.Operation, e error) {
	var report []byte
	e = tx.QueryRow(ctx, `SELECT o.operation_id::text,o.preview_id::text,p.version,o.status,o.report FROM agent_visual_operations o JOIN agent_visual_previews p ON p.id=o.preview_id WHERE o.operation_id=$1 AND o.owner_id=$2`, id, owner).Scan(&out.OperationID, &out.PreviewID, &out.Version, &out.Status, &report)
	if e != nil {
		return out, visualError(e)
	}
	out.SchemaVersion = 1
	if len(report) > 0 {
		out.Report = &av.Report{}
		if json.Unmarshal(report, out.Report) != nil {
			return av.Operation{}, av.ErrUnavailable
		}
	}
	return out, nil
}
func (g *agentVisualGateway) Operation(ctx context.Context, a av.Access, id string) (out av.Operation, e error) {
	if !av.UUID(id) {
		return out, av.ErrInvalid
	}
	tx, e := g.begin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	out, e = visualOperation(ctx, tx, id, a.AccountID)
	if e != nil {
		return out, e
	}
	v, e := visualRead(ctx, tx, out.PreviewID, a.AccountID, false)
	if e != nil {
		return av.Operation{}, e
	}
	c, e := g.validateStored(ctx, tx, a, v, false)
	if e != nil {
		return av.Operation{}, e
	}
	if e = g.finish(ctx, tx, a, c); e != nil {
		return av.Operation{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return av.Operation{}, av.ErrUnavailable
	}
	return out, nil
}
func (g *agentVisualGateway) Analyze(ctx context.Context, a av.Access, id string, in av.ApproveInput) (out av.Operation, e error) {
	if !av.UUID(id) || av.ValidateApproval(in) != nil {
		return out, av.ErrInvalid
	}
	tx, e := g.begin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	v, e := visualRead(ctx, tx, id, a.AccountID, false)
	if e != nil {
		return out, e
	}
	if in.ExpectedVersion != v.preview.Version || in.PayloadDigest != v.preview.PayloadDigest || in.PixelSHA256 != v.preview.Pixel.SHA256 {
		return out, av.ErrConflict
	}
	c, e := g.validateStored(ctx, tx, a, v, false)
	if e != nil {
		return out, e
	}
	if e = egressOwnerLock(ctx, tx, a.AccountID); e != nil {
		return out, visualError(e)
	}
	v, e = visualRead(ctx, tx, id, a.AccountID, true)
	if e != nil {
		return out, e
	}
	existing, readErr := visualOperation(ctx, tx, in.OperationID, a.AccountID)
	if readErr == nil {
		if existing.PreviewID != id {
			return out, av.ErrConflict
		}
		if e = g.finish(ctx, tx, a, c); e != nil {
			return out, e
		}
		if e = tx.Commit(ctx); e != nil {
			return out, av.ErrUnavailable
		}
		return existing, nil
	}
	if !errors.Is(readErr, av.ErrDenied) {
		return out, readErr
	}
	if v.state != "PREVIEW" {
		return out, av.ErrConflict
	}
	c, e = g.validateStored(ctx, tx, a, v, true)
	if e != nil {
		return out, e
	}
	if len(v.pixels) != v.preview.Pixel.ByteSize || av.Digest(v.pixels) != v.preview.Pixel.SHA256 {
		return out, av.ErrDenied
	}
	price, e := readLivePrice(ctx, tx, v.preview.Budget.PriceVersion)
	if e != nil || mb.ValidateVisualPrice(price, c.now) != nil {
		return out, av.ErrUnavailable
	}
	upper, _ := mb.BoundVisual(price, c.now)
	wire, e := visualWire(g, v)
	if e != nil || wire.WireDigest() != v.preview.PayloadDigest || wire.InputTokenBound() != upper.InputTokens || wire.MaxOutputTokens() != int(upper.OutputTokens) {
		return out, av.ErrConflict
	}
	budgets, e := readEgressBudgets(ctx, tx, a.AccountID, v.root, v.root, "CNY")
	if e != nil {
		return out, visualError(e)
	}
	for _, b := range budgets {
		if !mb.Fits(b.limit, b.used, upper) {
			return out, av.ErrBudget
		}
	}
	result, e := tx.Exec(ctx, `UPDATE model_egress_previews SET status='APPROVED',revision=revision+1,approved_at=clock_timestamp() WHERE id=$1 AND owner_id=$2 AND status='DRAFT' AND revision=1 AND scope=$3 AND purpose=$4 AND request_digest=$5`, id, a.AccountID, av.Scope, av.EgressPurpose, v.requestDigest)
	if e != nil || result.RowsAffected() != 1 {
		return out, av.ErrConflict
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_budget_reservations(operation_id,preview_id,root_trace_id,task_id,owner_id,price_version,request_digest,currency,upper_input,upper_output,upper_cost,billing_kind,cash_status,execution_status) VALUES($1,$2,$3,$3,$4,$5,$6,'CNY',$7,$8,$9,'TOKEN','UNKNOWN','LIVE_RESERVED')`, in.OperationID, id, v.root, a.AccountID, price.Base.Version, v.requestDigest, upper.InputTokens, upper.OutputTokens, upper.CostMicros)
	if e != nil {
		return out, av.ErrConflict
	}
	if e = changeEgressBudgets(ctx, tx, a.AccountID, v.root, v.root, 1, upper); e != nil {
		return out, visualError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE agent_visual_previews SET state='APPROVED' WHERE id=$1`, id)
	if e != nil {
		return out, av.ErrUnavailable
	}
	_, e = tx.Exec(ctx, `INSERT INTO agent_visual_operations(operation_id,preview_id,owner_id) VALUES($1,$2,$3)`, in.OperationID, id, a.AccountID)
	if e != nil {
		return out, av.ErrConflict
	}
	for _, decision := range []string{"APPROVE", "RESERVE"} {
		if e = egressAudit(ctx, tx, a.AccountID, v.root, in.OperationID, decision); e != nil {
			return out, visualError(e)
		}
	}
	if e = g.finish(ctx, tx, a, c, v.preview.ExpiresAt, price.Base.ExpiresAt); e != nil {
		return out, e
	}
	if !g.ready(ctx) {
		return out, av.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return out, av.ErrUnavailable
	}
	port := &visualDispatchPort{gateway: g, access: a, operation: in.OperationID, preview: id, wire: wire, stored: v}
	gateway, e := modelgateway.NewNativeVisualGateway(g.options.Gate, g.options.Adapter, port)
	if e == nil {
		_, e = gateway.Complete(ctx, wire)
	}
	// A transport/commit uncertainty is durable UNKNOWN. No client retry is a
	// permission to invoke the provider a second time.
	recovered, readErr := g.Operation(ctx, a, in.OperationID)
	if readErr == nil {
		return recovered, nil
	}
	if e != nil {
		return av.Operation{SchemaVersion: 1, OperationID: in.OperationID, PreviewID: id, Version: v.preview.Version, Status: "UNKNOWN"}, nil
	}
	return out, readErr
}

type visualDispatchPort struct {
	gateway            *agentVisualGateway
	access             av.Access
	operation, preview string
	wire               modelgateway.PreparedVisualWire
	stored             visualStored
}

func (p *visualDispatchPort) matches(w modelgateway.PreparedVisualWire) bool {
	// PreparedVisualWire is package-sealed and immutable. The full byte hash
	// was checked during approval; chunk guards compare that proof and fresh
	// native metadata without repeatedly copying/hash-reading a 10 MiB image.
	return p != nil && w.WireDigest() == p.wire.WireDigest() && w.DeadlineAt().Equal(p.wire.DeadlineAt()) && w.InputTokenBound() == p.wire.InputTokenBound() && w.MaxOutputTokens() == p.wire.MaxOutputTokens()
}
func (p *visualDispatchPort) check(ctx context.Context, tx pgx.Tx, w modelgateway.PreparedVisualWire) (v visualStored, c visualCurrent, e error) {
	if !p.matches(w) || !p.gateway.ready(ctx) {
		return v, c, av.ErrDenied
	}
	v, e = visualMeta(ctx, tx, p.preview, p.access.AccountID)
	if e != nil {
		return v, c, e
	}
	c, e = p.gateway.validateStored(ctx, tx, p.access, v, true)
	if e != nil {
		return v, c, e
	}
	if v.state != "APPROVED" || !v.pixelsPresent || v.preview.Pixel.SHA256 != p.stored.preview.Pixel.SHA256 || v.preview.Pixel.ID != p.stored.preview.Pixel.ID || v.requestDigest != p.stored.requestDigest || v.preview.PayloadDigest != w.WireDigest() {
		return v, c, av.ErrDenied
	}
	price, e := readLivePrice(ctx, tx, v.preview.Budget.PriceVersion)
	if e != nil || mb.ValidateVisualPrice(price, c.now) != nil {
		return v, c, av.ErrDenied
	}
	var valid bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_budget_reservations r JOIN model_egress_previews pr ON pr.id=r.preview_id WHERE r.operation_id=$1 AND r.owner_id=$2 AND r.preview_id=$3 AND r.request_digest=$4 AND r.state IN('RESERVED','IN_FLIGHT') AND r.billing_kind='TOKEN' AND r.upper_input=24576 AND r.upper_output=768 AND r.upper_cost=210432 AND pr.status='APPROVED' AND pr.scope=$5 AND pr.purpose=$6 AND pr.egress_payload_digest=$7 AND pr.session_id=$8 AND pr.expires_at>clock_timestamp())`, p.operation, p.access.AccountID, p.preview, v.requestDigest, av.Scope, av.EgressPurpose, w.WireDigest(), c.session).Scan(&valid)
	if e != nil || !valid {
		return v, c, av.ErrDenied
	}
	return v, c, nil
}
func (p *visualDispatchPort) CheckCurrent(ctx context.Context, w modelgateway.PreparedVisualWire) error {
	tx, e := p.gateway.begin(ctx, p.access)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	v, c, e := p.check(ctx, tx, w)
	if e != nil {
		return e
	}
	if e = p.gateway.finish(ctx, tx, p.access, c, v.preview.ExpiresAt); e != nil {
		return e
	}
	return visualError(tx.Commit(ctx))
}
func (p *visualDispatchPort) ReleaseOnce(ctx context.Context, w modelgateway.PreparedVisualWire, call func(context.Context) (modelgateway.VisualResult, error)) (modelgateway.VisualResult, error) {
	empty := modelgateway.VisualResult{Status: modelgateway.Unavailable, Usage: modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}}
	if call == nil || !p.matches(w) {
		return empty, av.ErrDenied
	}
	tx, e := p.gateway.begin(ctx, p.access)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.Background())
	v, c, e := p.check(ctx, tx, w)
	if e != nil {
		return empty, e
	}
	if e = egressOwnerLock(ctx, tx, p.access.AccountID); e != nil {
		return empty, visualError(e)
	}
	budgets, e := readEgressBudgets(ctx, tx, p.access.AccountID, v.root, v.root, "CNY")
	if e != nil {
		return empty, visualError(e)
	}
	for _, b := range budgets {
		if b.used.Requests < 1 || b.used.InputTokens < 24576 || b.used.OutputTokens < 768 || b.used.CostMicros < 210432 {
			return empty, av.ErrBudget
		}
	}
	changed, e := tx.Exec(ctx, `UPDATE model_budget_reservations SET state='IN_FLIGHT',execution_status='LIVE_IN_FLIGHT' WHERE operation_id=$1 AND owner_id=$2 AND preview_id=$3 AND state='RESERVED'`, p.operation, p.access.AccountID, p.preview)
	if e != nil || changed.RowsAffected() != 1 {
		return empty, av.ErrConflict
	}
	if e = egressAudit(ctx, tx, p.access.AccountID, v.root, p.operation, "IN_FLIGHT"); e != nil {
		return empty, visualError(e)
	}
	if e = p.gateway.finish(ctx, tx, p.access, c, v.preview.ExpiresAt); e != nil {
		return empty, e
	}
	if !p.gateway.ready(ctx) {
		return empty, av.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return empty, av.ErrUnavailable
	}
	result, e := call(ctx)
	if e != nil {
		_ = p.SettleUnknown(ctx, w)
		return empty, e
	}
	if e = p.publish(ctx, w, result); e != nil {
		_ = p.SettleUnknown(ctx, w)
		return empty, e
	}
	return result, nil
}
func (p *visualDispatchPort) settle(ctx context.Context, tx pgx.Tx, result *modelgateway.VisualResult) error {
	var input, output *int64
	status := "UNKNOWN"
	if result != nil && result.Usage.Status == "KNOWN" && result.Usage.CostStatus == "UNKNOWN" && result.Usage.InputTokens != nil && result.Usage.OutputTokens != nil && *result.Usage.InputTokens >= 0 && *result.Usage.InputTokens <= 24576 && *result.Usage.OutputTokens >= 0 && *result.Usage.OutputTokens <= 768 {
		input, output = result.Usage.InputTokens, result.Usage.OutputTokens
		status = "KNOWN"
	}
	changed, e := tx.Exec(ctx, `UPDATE model_budget_reservations SET state='UNKNOWN',execution_status='LIVE_ATTEMPTED',usage_status=$4,reported_input=$5,reported_output=$6 WHERE operation_id=$1 AND owner_id=$2 AND preview_id=$3 AND state='IN_FLIGHT'`, p.operation, p.access.AccountID, p.preview, status, input, output)
	if e != nil {
		return visualError(e)
	}
	if changed.RowsAffected() == 1 {
		return visualError(egressAudit(ctx, tx, p.access.AccountID, p.stored.root, p.operation, "UNKNOWN"))
	}
	return nil
}
func (p *visualDispatchPort) SettleUnknown(_ context.Context, w modelgateway.PreparedVisualWire) error {
	if !p.matches(w) {
		return av.ErrDenied
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, e := p.gateway.store.beginEgress(ctx)
	if e != nil {
		return visualError(e)
	}
	defer tx.Rollback(context.Background())
	if e = egressOwnerLock(ctx, tx, p.access.AccountID); e != nil {
		return visualError(e)
	}
	if e = p.settle(ctx, tx, nil); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_visual_previews SET pixels=NULL WHERE id=$1 AND owner_id=$2`, p.preview, p.access.AccountID); e != nil {
		return av.ErrUnavailable
	}
	return visualError(tx.Commit(ctx))
}
func (p *visualDispatchPort) publish(ctx context.Context, w modelgateway.PreparedVisualWire, result modelgateway.VisualResult) error {
	tx, e := p.gateway.begin(ctx, p.access)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	v, c, e := p.check(ctx, tx, w)
	if e != nil {
		return e
	}
	if e = egressOwnerLock(ctx, tx, p.access.AccountID); e != nil {
		return visualError(e)
	}
	status := string(result.Status)
	var raw []byte
	switch result.Status {
	case modelgateway.Completed:
		if result.Report == nil || !av.Text(result.Report.Summary, 4096, false) || len(result.Report.Observations) > 32 || len(result.Report.Unknowns) > 16 {
			return av.ErrUnavailable
		}
		report := av.Report{Summary: result.Report.Summary, Observations: []av.Observation{}, Unknowns: append([]string{}, result.Report.Unknowns...)}
		for _, item := range result.Report.Observations {
			if !av.Text(item.Value, 2048, false) || !(item.Kind == "VISIBLE_OBJECT" || item.Kind == "VISIBLE_TEXT" || item.Kind == "VISIBLE_SCENE") || item.Confidence.Kind != "QUALITATIVE" || !(item.Confidence.Level == "LOW" || item.Confidence.Level == "MEDIUM" || item.Confidence.Level == "HIGH" || item.Confidence.Level == "UNKNOWN") {
				return av.ErrUnavailable
			}
			report.Observations = append(report.Observations, av.Observation{Kind: item.Kind, Value: item.Value, Confidence: av.Confidence{Kind: item.Confidence.Kind, Level: item.Confidence.Level}, Source: av.Source{Type: "APPROVED_PIXELS", ID: v.preview.Pixel.ID, Revision: v.preview.Pixel.Revision, SHA256: v.preview.Pixel.SHA256}, ObservedAt: c.now.UTC()})
		}
		for _, unknown := range report.Unknowns {
			if !av.Text(unknown, 2048, false) {
				return av.ErrUnavailable
			}
		}
		raw, e = json.Marshal(report)
		if e != nil || len(raw) > 131072 {
			return av.ErrUnavailable
		}
	case modelgateway.Refused, modelgateway.Unavailable:
		if result.Report != nil {
			return av.ErrUnavailable
		}
	default:
		return av.ErrUnavailable
	}
	if e = p.settle(ctx, tx, &result); e != nil {
		return e
	}
	changed, e := tx.Exec(ctx, `UPDATE agent_visual_operations SET status=$3,report=$4,completed_at=clock_timestamp() WHERE operation_id=$1 AND owner_id=$2 AND status='UNKNOWN'`, p.operation, p.access.AccountID, status, raw)
	if e != nil || changed.RowsAffected() != 1 {
		return av.ErrConflict
	}
	if e = p.gateway.finish(ctx, tx, p.access, c, v.preview.ExpiresAt); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE agent_visual_previews SET pixels=NULL WHERE id=$1 AND owner_id=$2`, p.preview, p.access.AccountID); e != nil {
		return av.ErrUnavailable
	}
	if !p.gateway.ready(ctx) {
		return av.ErrDenied
	}
	return visualError(tx.Commit(ctx))
}
