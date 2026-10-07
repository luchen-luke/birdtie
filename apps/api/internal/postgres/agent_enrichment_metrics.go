package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	aem "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentmetrics"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

func enrichmentObservationError(e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return e
	}
	if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, agentprofile.ErrForbidden) || errors.Is(e, agentmemory.ErrForbidden) {
		return aem.ErrDenied
	}
	if errors.Is(e, agentmemory.ErrNotFound) || errors.Is(e, agentprofile.ErrNotFound) {
		return aem.ErrNotFound
	}
	if errors.Is(e, agentmemory.ErrInvalid) || errors.Is(e, agentprofile.ErrInvalid) {
		return aem.ErrInvalid
	}
	return aem.ErrUnavailable
}
func insertEnrichmentObservation(ctx context.Context, tx pgx.Tx, kind, owner, agent, metric string, audit, admin *int64, occurred time.Time) error {
	if !aem.ValidLifecycle(metric) {
		return aem.ErrInvalid
	}
	_, e := tx.Exec(ctx, `INSERT INTO agent_enrichment_observations(audit_event_id,admin_audit_event_id,owner_type,owner_id,agent_id,metric,occurred_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, audit, admin, kind, owner, agent, metric, occurred, occurred.Add(aem.DefaultRetention))
	return e
}

// Read-only, owner Session derived, no org-admin delegation to private traces.
func (s *Store) beginEnrichmentObservation(ctx context.Context, a agentprofile.PrivateAccess) (pgx.Tx, agentPrivateBinding, error) {
	var b agentPrivateBinding
	if s == nil || s.pool == nil || agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, b, aem.ErrDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, aem.ErrUnavailable
	}
	b, e = lockOwnAgentPrivateBinding(ctx, tx, a, s.devPhoneEnabled)
	if e == nil {
		_, e = lockAgentPrivateMetadata(ctx, tx, b, false)
	}
	if e != nil {
		tx.Rollback(context.Background())
		return nil, b, enrichmentObservationError(e)
	}
	return tx, b, nil
}
func (s *Store) finishEnrichmentObservation(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, payload any) (time.Time, time.Time, error) {
	// Encoding is part of the release boundary; never return a loaded payload
	// after a table/pool/encoding wait without the current native clock check.
	if _, e := json.Marshal(payload); e != nil {
		return time.Time{}, time.Time{}, aem.ErrUnavailable
	}
	var now, until time.Time
	var valid bool
	e := tx.QueryRow(ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT n,least(s.expires_at,s.idle_expires_at,n+interval '30 seconds'),
 s.revoked_at IS NULL AND s.expires_at>n AND s.idle_expires_at>n AND a.status='active' AND a.account_type='person'
 AND g.status='active' AND g.agent_type='personal' AND g.principal_account_id=a.id
 AND p.owner_id=a.id AND p.owner_type='PERSON' AND ($4 OR s.authentication_method<>'dev_phone')
 FROM c CROSS JOIN sessions s JOIN accounts a ON a.id=s.account_id JOIN agents g ON g.id=$3
 JOIN agent_profiles p ON p.agent_id=g.id WHERE s.id=$1 AND a.id=$2`, b.sessionID, b.accountID, b.agentID, s.devPhoneEnabled).Scan(&now, &until, &valid)
	if e != nil {
		return now, until, enrichmentObservationError(e)
	}
	if !valid {
		return now, until, aem.ErrDenied
	}
	return now, until, ctx.Err()
}
func enrichmentWindow(ctx context.Context, tx pgx.Tx, w aem.Window) (aem.Window, time.Time, error) {
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return w, now, aem.ErrUnavailable
	}
	if w.Until.IsZero() {
		w.Until = now
	}
	if w.Since.IsZero() {
		w.Since = now.Add(-aem.DefaultRetention)
	}
	if !w.Valid(now) {
		return w, now, aem.ErrInvalid
	}
	return w, now, nil
}
func readEnrichmentCounters(ctx context.Context, tx pgx.Tx, kind, owner string, w aem.Window) (aem.Snapshot, error) {
	out := aem.Snapshot{SchemaVersion: aem.Schema, Window: w, Metrics: []aem.Metric{}, Policies: []aem.PolicyCount{}, HistoricalClassification: "UNKNOWN_UNCLASSIFIED_NOT_BACKFILLED", Limit: aem.MaxRows, PolicyStatus: "UNAVAILABLE_SOCIAL_POLICY"}
	counts := map[string]int64{}
	rows, e := tx.Query(ctx, `SELECT metric FROM agent_enrichment_observations WHERE owner_type=$1 AND owner_id=$2 AND occurred_at>=$3 AND occurred_at<$4 AND expires_at>clock_timestamp() ORDER BY occurred_at,id LIMIT 10001`, kind, owner, w.Since, w.Until)
	if e != nil {
		return out, aem.ErrUnavailable
	}
	n := 0
	for rows.Next() {
		var m string
		if e = rows.Scan(&m); e != nil {
			rows.Close()
			return out, aem.ErrUnavailable
		}
		n++
		if n <= aem.MaxRows {
			counts[m]++
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return out, aem.ErrUnavailable
	}
	out.Truncated = n > aem.MaxRows
	if kind == "PERSON" {
		rows, e = tx.Query(ctx, `SELECT policy_version,disposition,reason FROM native_notification_decisions WHERE recipient_id=$1 AND created_at>=$2 AND created_at<$3 AND policy_version>0 AND reason IN('exact_rule','default_rule','attention_paused') ORDER BY created_at,id LIMIT 10001`, owner, w.Since, w.Until)
		if e != nil {
			return out, aem.ErrUnavailable
		}
		n = 0
		for rows.Next() {
			var v int64
			var route, reason string
			if e = rows.Scan(&v, &route, &reason); e != nil {
				rows.Close()
				return out, aem.ErrUnavailable
			}
			n++
			if n > aem.MaxRows {
				out.Truncated = true
				continue
			}
			counts[aem.PolicyTriggered]++
			found := false
			for i := range out.Policies {
				p := &out.Policies[i]
				if p.Version == v && p.Disposition == route && p.Reason == reason {
					p.Count++
					found = true
					break
				}
			}
			if !found {
				out.Policies = append(out.Policies, aem.PolicyCount{Family: "NOTIFICATION", Version: v, Disposition: route, Reason: reason, Count: 1})
			}
		}
		rows.Close()
		if rows.Err() != nil {
			return out, aem.ErrUnavailable
		}
		e = tx.QueryRow(ctx, `SELECT count(*) FROM(SELECT a.id FROM audit_events a WHERE a.actor_account_id=$1 AND a.occurred_at>=$2 AND a.occurred_at<$3 AND a.decision='allowed' AND ((a.resource_type='agent_memory' AND a.purpose IN('human_explicit_memory_edit','human_memory_delete')) OR(a.resource_type='memory_candidate' AND a.purpose='human_candidate_decision' AND a.action IN('reject','approve'))) AND NOT EXISTS(SELECT 1 FROM agent_enrichment_observations m WHERE m.audit_event_id=a.id) LIMIT 10001)x`, owner, w.Since, w.Until).Scan(&out.LegacyUnclassified)
	} else {
		e = tx.QueryRow(ctx, `SELECT count(*) FROM(SELECT a.id FROM admin_audit_events a JOIN organizations o ON o.id=a.organization_id WHERE o.account_id=$1 AND a.resource_type='organization_memory' AND a.occurred_at>=$2 AND a.occurred_at<$3 AND NOT EXISTS(SELECT 1 FROM agent_enrichment_observations m WHERE m.admin_audit_event_id=a.id) LIMIT 10001)x`, owner, w.Since, w.Until).Scan(&out.LegacyUnclassified)
	}
	if e != nil {
		return out, aem.ErrUnavailable
	}
	if out.LegacyUnclassified > aem.MaxRows {
		out.LegacyUnclassified = aem.MaxRows
		out.Truncated = true
	}
	for _, m := range aem.MetricKinds() {
		out.Metrics = append(out.Metrics, aem.Metric{Kind: m, Count: counts[m]})
	}
	return out, nil
}
func (s *Store) ReadOwnEnrichmentMetrics(ctx context.Context, a agentprofile.PrivateAccess, w aem.Window) (aem.Snapshot, error) {
	tx, b, e := s.beginEnrichmentObservation(ctx, a)
	if e != nil {
		return aem.Snapshot{}, e
	}
	defer tx.Rollback(context.Background())
	w, _, e = enrichmentWindow(ctx, tx, w)
	if e != nil {
		return aem.Snapshot{}, e
	}
	out, e := readEnrichmentCounters(ctx, tx, "PERSON", b.accountID, w)
	if e != nil {
		return aem.Snapshot{}, e
	}
	out.ObservedAt, out.ValidUntil, e = s.finishEnrichmentObservation(ctx, tx, b, out)
	if e != nil {
		return aem.Snapshot{}, e
	}
	e = tx.Commit(ctx)
	return out, enrichmentObservationError(e)
}
func (s *Store) ReadOrganizationEnrichmentMetrics(ctx context.Context, a agentorganizationmemory.Access, w aem.Window) (aem.Snapshot, error) {
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return aem.Snapshot{}, enrichmentObservationError(e)
	}
	defer tx.Rollback(context.Background())
	w, _, e = enrichmentWindow(ctx, tx, w)
	if e != nil {
		return aem.Snapshot{}, e
	}
	out, e := readEnrichmentCounters(ctx, tx, "ORGANIZATION", b.principalID, w)
	if e != nil {
		return aem.Snapshot{}, e
	}
	if _, e = json.Marshal(out); e != nil {
		return aem.Snapshot{}, aem.ErrUnavailable
	}
	out.ObservedAt, e = s.orgMemoryBoundary(ctx, tx, b, nil)
	if e != nil {
		return aem.Snapshot{}, enrichmentObservationError(e)
	}
	var sessionCurrent bool
	e = tx.QueryRow(ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n) SELECT n,least(expires_at,idle_expires_at,n+interval '30 seconds'),revoked_at IS NULL AND expires_at>n AND idle_expires_at>n FROM sessions CROSS JOIN c WHERE id=$1`, b.sessionID).Scan(&out.ObservedAt, &out.ValidUntil, &sessionCurrent)
	if e != nil {
		return aem.Snapshot{}, enrichmentObservationError(e)
	}
	if !sessionCurrent {
		return aem.Snapshot{}, aem.ErrDenied
	}
	e = tx.Commit(ctx)
	return out, enrichmentObservationError(e)
}

// Bounded maintenance deletes DERIVED metadata only. No ledger, original audit,
// notification decision, permission, Run or fee is deleted or rewritten.
func (s *Store) PruneExpiredEnrichmentObservations(ctx context.Context, limit int) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, aem.ErrUnavailable
	}
	if limit < 1 || limit > 1000 {
		return 0, aem.ErrInvalid
	}
	tag, e := s.pool.Exec(ctx, `WITH expired AS(SELECT id FROM agent_enrichment_observations WHERE expires_at<=clock_timestamp() ORDER BY expires_at,id FOR UPDATE SKIP LOCKED LIMIT $1) DELETE FROM agent_enrichment_observations m USING expired WHERE m.id=expired.id`, limit)
	if e != nil {
		return 0, aem.ErrUnavailable
	}
	return tag.RowsAffected(), nil
}

func (s *Store) ReadOwnEnrichmentBudgetAlerts(ctx context.Context, a agentprofile.PrivateAccess, root, task string) (aem.BudgetAlerts, error) {
	var out aem.BudgetAlerts
	if !egressUUID(root) || !egressUUID(task) {
		return out, aem.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentObservation(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	if e = egressOwnerLock(ctx, tx, b.accountID); e != nil {
		return out, enrichmentObservationError(e)
	}
	var currency string
	e = tx.QueryRow(ctx, `SELECT r.currency FROM model_budget_roots r JOIN model_budget_tasks t ON t.root_trace_id=r.root_trace_id AND t.owner_id=r.owner_id WHERE r.root_trace_id=$1 AND r.owner_id=$2 AND t.task_id=$3 FOR SHARE OF r,t`, root, b.accountID, task).Scan(&currency)
	if e != nil {
		return out, enrichmentObservationError(e)
	}
	budgets, e := readEgressBudgets(ctx, tx, b.accountID, root, task, currency)
	if e != nil {
		return out, aem.ErrUnavailable
	}
	out.SchemaVersion = aem.Schema
	out.Evidence = "LOCAL_NATIVE_ACCOUNTING_NOT_PROVIDER_ACTUAL"
	out.Warnings = []aem.BudgetWarning{}
	for _, r := range budgets {
		for _, d := range []struct {
			name      string
			max, used int64
		}{{"REQUESTS", r.limit.Requests, r.used.Requests}, {"INPUT_TOKENS", r.limit.InputTokens, r.used.InputTokens}, {"OUTPUT_TOKENS", r.limit.OutputTokens, r.used.OutputTokens}, {"COST_MICROS", r.limit.CostMicros, r.used.CostMicros}} {
			v, e := aem.Warning(r.scope, d.name, d.max, d.used)
			if e != nil {
				return aem.BudgetAlerts{}, e
			}
			out.Warnings = append(out.Warnings, v)
		}
	}
	e = tx.QueryRow(ctx, `WITH c AS MATERIALIZED(SELECT date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' n) SELECT n,COALESCE(sum(settled_cost) FILTER(WHERE state='SETTLED'),0),COALESCE(sum(upper_cost) FILTER(WHERE state IN('RESERVED','IN_FLIGHT','UNKNOWN')),0),count(*) FILTER(WHERE state='UNKNOWN') FROM c LEFT JOIN model_budget_reservations r ON r.owner_id=$1 AND r.created_at>=n AND r.currency=$2 GROUP BY n`, b.accountID, currency).Scan(&out.Monthly.MonthStart, &out.Monthly.KnownActualMicros, &out.Monthly.HeldUnknownUpperMicros, &out.Monthly.UnknownAttempts)
	if e != nil {
		return aem.BudgetAlerts{}, aem.ErrUnavailable
	}
	out.Monthly.Currency = currency
	out.Monthly.LimitStatus = "UNAVAILABLE_NO_MONTHLY_QUOTA"
	out.ObservedAt, out.ValidUntil, e = s.finishEnrichmentObservation(ctx, tx, b, out)
	if e != nil {
		return aem.BudgetAlerts{}, e
	}
	e = tx.Commit(ctx)
	return out, enrichmentObservationError(e)
}

// Only controlled metadata leaves this reader. Source IDs are intentionally
// omitted: observation of a historical event does not grant today's source ACL.
func (s *Store) ReadOwnEnrichmentTrace(ctx context.Context, a agentprofile.PrivateAccess, family, id string) (aem.Trace, error) {
	return s.ReadOwnEnrichmentTraceWithinWindow(ctx, a, family, id, aem.DefaultRetention)
}

// The caller can only tighten the finite metadata observation window. This is
// neither a historical model dispatch handle nor a renewal of the original Run.
func (s *Store) ReadOwnEnrichmentTraceWithinWindow(ctx context.Context, a agentprofile.PrivateAccess, family, id string, window time.Duration) (aem.Trace, error) {
	var out aem.Trace
	if window <= 0 || window > aem.DefaultRetention || !egressUUID(id) || (family != "MODEL_REQUEST" && family != "MOMENT_ENRICHMENT") {
		return out, aem.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentObservation(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	out = aem.Trace{SchemaVersion: aem.Schema, Family: family, RunID: id, SourceStatus: "NOT_INCLUDED_REQUIRES_CURRENT_SOURCE_ACL", Steps: []aem.ModelStep{}, ModelAccess: "UNAVAILABLE"}
	var start, end time.Time
	if family == "MOMENT_ENRICHMENT" {
		var event string
		e = tx.QueryRow(ctx, `SELECT state,reason,event_id::text,created_at,updated_at FROM agent_enrichment_runs WHERE id=$1 AND owner_id=$2 AND agent_id=$3 FOR SHARE`, id, b.accountID, b.agentID).Scan(&out.State, &out.Decision, &event, &start, &end)
		if e != nil {
			return aem.Trace{}, enrichmentObservationError(e)
		}
		out.EventID = &event
		out.ErrorCode = "NOT_APPLICABLE"
		if out.State == "FAILED" || out.State == "RETRY_WAIT" {
			out.ErrorCode = out.Decision
		}
	} else {
		if e = egressOwnerLock(ctx, tx, b.accountID); e != nil {
			return aem.Trace{}, aem.ErrUnavailable
		}
		r, e := readModelRun(ctx, tx, id, b.accountID)
		if e != nil || r.agent != b.agentID {
			return aem.Trace{}, aem.ErrDenied
		}
		out.State = string(r.control.State)
		out.Decision = "CONTROLLED_ACCOUNTING_METADATA_ONLY"
		out.ErrorCode = "UNKNOWN_NOT_RECORDED"
		start, end = r.control.CreatedAt, r.control.UpdatedAt
		bind, e := readTaskModelBinding(ctx, tx, r.control.BindingID)
		if e != nil {
			return aem.Trace{}, aem.ErrUnavailable
		}
		if bind.TaskID != r.control.TaskID || bind.Reference.Agent.AgentID != b.agentID || bind.Reference.Agent.Principal.ID != b.accountID {
			return aem.Trace{}, aem.ErrDenied
		}
		config, e := readModelConfiguration(ctx, tx, bind.Reference.ConfigurationVersion)
		if e != nil {
			return aem.Trace{}, aem.ErrUnavailable
		}
		c := config.Configuration
		out.Configuration = &aem.Configuration{Version: c.Version, Fingerprint: config.Fingerprint, PromptVersion: c.PromptVersion, InputSchemaVersion: c.InputSchemaVersion, OutputSchemaVersion: c.OutputSchemaVersion, PolicyVersion: c.PolicyVersion, ToolAllowlist: append([]string{}, c.ToolAllowlist...)}
		for _, cap := range c.CapabilitiesRequired {
			out.Configuration.Capabilities = append(out.Configuration.Capabilities, string(cap))
		}
		if len(r.control.Steps) > aem.MaxTraceSteps {
			return aem.Trace{}, aem.ErrUnavailable
		}
		for _, v := range r.control.Steps {
			p, e := readLocalModelPrice(ctx, tx, v.PriceVersion)
			if e != nil {
				return aem.Trace{}, aem.ErrUnavailable
			}
			step := aem.ModelStep{Ordinal: v.Ordinal, OperationID: v.OperationID, State: string(v.State), PriceVersion: p.Version, Provider: p.Destination.Provider, Model: p.Destination.Model, ModelVersion: p.Destination.Version, WireContract: p.Destination.WireContract, Currency: p.Currency, Evidence: string(p.Evidence), UsageStatus: "NOT_DISPATCHED", ToolDecision: "NO_AUTOMATIC_TOOL_EFFECT"}
			if v.ReservationID != "" {
				e = tx.QueryRow(ctx, `SELECT reported_input,reported_output,settled_cost,CASE WHEN state IN('RESERVED','IN_FLIGHT','UNKNOWN') THEN upper_cost ELSE 0 END,CASE WHEN state='SETTLED' THEN 'KNOWN_REPORTED' ELSE 'UNKNOWN_NOT_SETTLED' END FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2 FOR SHARE`, v.OperationID, b.accountID).Scan(&step.InputTokens, &step.OutputTokens, &step.ActualCostMicros, &step.HeldUpperCostMicros, &step.UsageStatus)
				if e != nil {
					return aem.Trace{}, aem.ErrUnavailable
				}
			}
			out.Steps = append(out.Steps, step)
		}
	}
	out.DurationMicros = end.Sub(start).Microseconds()
	if out.DurationMicros < 0 {
		return aem.Trace{}, aem.ErrUnavailable
	}
	out.ObservedAt, out.ValidUntil, e = s.finishEnrichmentObservation(ctx, tx, b, out)
	if e != nil {
		return aem.Trace{}, e
	}
	windowEnd := start.Add(window)
	if !windowEnd.After(out.ObservedAt) {
		return aem.Trace{}, aem.ErrNotFound
	}
	if windowEnd.Before(out.ValidUntil) {
		out.ValidUntil = windowEnd
	}
	e = tx.Commit(ctx)
	return out, enrichmentObservationError(e)
}
