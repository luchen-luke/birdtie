package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

// This ledger is a real, local database boundary. It has no provider transport,
// paid tariff, tokenizer or AgentRun implementation. All reservations retain
// execution_status=UNAVAILABLE. Only the owner's actual scalar Task query is
// eligible for explicit preview; conversation, images and Memory are excluded.
func egressError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, modelconfiguration.ErrDenied) || errors.Is(err, modelconfiguration.ErrMissing) {
		return modelegressbudget.ErrDenied
	}
	if errors.Is(err, modelconfiguration.ErrInvalid) {
		return modelegressbudget.ErrInvalid
	}
	return modelegressbudget.ErrUnavailable
}
func egressUUID(id string) bool {
	r, e := actorref.Parse("PERSON", id)
	return e == nil && r.ID == id && id != "00000000-0000-0000-0000-000000000000"
}
func (s *Store) beginEgress(ctx context.Context) (pgx.Tx, error) {
	if s == nil || s.pool == nil {
		return nil, modelegressbudget.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	return tx, egressError(e)
}
func egressFinish(ctx context.Context, tx pgx.Tx, session string, request *modelgateway.Request, expiry ...time.Time) error {
	var valid bool
	var now time.Time
	err := tx.QueryRow(ctx, `WITH c AS MATERIALIZED(SELECT clock_timestamp() n) SELECT revoked_at IS NULL AND expires_at>n AND idle_expires_at>n,n FROM sessions CROSS JOIN c WHERE id=$1`, session).Scan(&valid, &now)
	if err != nil {
		return egressError(err)
	}
	if !valid {
		return modelegressbudget.ErrDenied
	}
	for _, v := range expiry {
		if v.IsZero() || !v.After(now) {
			return modelegressbudget.ErrDenied
		}
	}
	if request != nil && modelgateway.ValidateRequest(*request, now) != nil {
		return modelegressbudget.ErrDenied
	}
	return ctx.Err()
}
func (s *Store) egressOwner(ctx context.Context, tx pgx.Tx, a agentevent.Access) (owner, session string, err error) {
	if a.SessionDigest == ([32]byte{}) {
		return "", "", modelegressbudget.ErrDenied
	}
	err = tx.QueryRow(ctx, `SELECT actor.id,ses.id FROM sessions ses JOIN accounts actor ON actor.id=ses.account_id
 WHERE ses.token_sha256=$1 AND actor.account_type='person' AND actor.status='active' AND ses.revoked_at IS NULL
 AND ses.expires_at>clock_timestamp() AND ses.idle_expires_at>clock_timestamp() AND ($2::boolean OR ses.authentication_method<>'dev_phone') FOR SHARE OF ses,actor`, a.SessionDigest[:], s.devPhoneEnabled).Scan(&owner, &session)
	return owner, session, egressError(err)
}

// An owner lock serializes accounting across every root and child of this
// Person. The native SHARE locks are obtained first, so source changes cannot
// pass between revalidation and committing a new reservation.
func egressOwnerLock(ctx context.Context, tx pgx.Tx, owner string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.model-budget.owner:'||$1,0))`, owner)
	return egressError(e)
}
func (s *Store) egressTask(ctx context.Context, tx pgx.Tx, a agentevent.Access, id, binding string) (currentConfigurationTask, string, error) {
	current, e := lockModelConfigurationTask(ctx, tx, a, id, s.devPhoneEnabled)
	if e != nil {
		return current, "", egressError(e)
	}
	if current.status != "ACTIVE" || !egressUUID(binding) {
		return current, "", modelegressbudget.ErrDenied
	}
	b, e := readTaskModelBinding(ctx, tx, binding)
	if e != nil {
		return current, "", egressError(e)
	}
	if b.TaskID != id || b.Reference.Agent != current.agent || b.SourceVersion != current.version || !b.SourceUpdatedAt.Equal(current.updatedAt) {
		return current, "", modelegressbudget.ErrDenied
	}
	var raw []byte
	var generation string
	var accountRaw, agentRaw []byte
	var accountGeneration, agentGeneration string
	e = tx.QueryRow(ctx, `SELECT to_jsonb(ap),ap.xmin::text,to_jsonb(actor),actor.xmin::text,to_jsonb(ag),ag.xmin::text
	 FROM agent_profiles ap JOIN accounts actor ON actor.id=ap.owner_id JOIN agents ag ON ag.id=ap.agent_id
	 WHERE ap.agent_id=$1 AND ap.owner_id=$2 AND ap.owner_type='PERSON' AND actor.status='active' AND ag.status='active'
	 FOR SHARE OF ap,actor,ag`, current.agent.AgentID, current.agent.Principal.ID).Scan(&raw, &generation, &accountRaw, &accountGeneration, &agentRaw, &agentGeneration)
	if e != nil {
		return current, "", egressError(e)
	}
	// These are actual retained row generations. A disable/restore or rebuild
	// cannot revive an earlier preview even if IDs and visible fields match.
	// Session xmin is deliberately excluded: ordinary idle refresh is not a
	// new authorization. Session ID/revocation/expiry are separately current.
	material := append([]byte("birdtie.model-egress.authority.v1\x00ap:"+generation+"\x00"), raw...)
	material = append(material, []byte("\x00account:"+accountGeneration+"\x00")...)
	material = append(material, accountRaw...)
	material = append(material, []byte("\x00agent:"+agentGeneration+"\x00")...)
	material = append(material, agentRaw...)
	h := sha256.Sum256(material)
	return current, hex.EncodeToString(h[:]), nil
}
func readLocalModelPrice(ctx context.Context, tx pgx.Tx, version string) (p modelegressbudget.Price, err error) {
	err = tx.QueryRow(ctx, `SELECT version,provider_id,model_id,model_version,wire_contract,region,retention,currency,input_rate,output_rate,input_ceiling,output_ceiling,evidence,expires_at FROM model_local_price_versions WHERE version=$1 AND billing_kind='LOCAL' FOR SHARE`, version).Scan(&p.Version, &p.Destination.Provider, &p.Destination.Model, &p.Destination.Version, &p.Destination.WireContract, &p.Region, &p.Retention, &p.Currency, &p.InputMicrosPerToken, &p.OutputMicrosPerToken, &p.InputTokenCeiling, &p.OutputTokenCeiling, &p.Evidence, &p.ExpiresAt)
	return p, egressError(err)
}

// RegisterLocalModelPrice is trusted server maintenance, not an HTTP tool or
// permission. LOCAL_SYNTHETIC is the only supported evidence class.
func (s *Store) RegisterLocalModelPrice(ctx context.Context, p modelegressbudget.Price) error {
	p.ExpiresAt = p.ExpiresAt.UTC().Truncate(time.Microsecond)
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return egressError(e)
	}
	if e = modelegressbudget.ValidatePrice(p, now); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_local_price_versions(version,provider_id,model_id,model_version,wire_contract,region,retention,currency,input_rate,output_rate,input_ceiling,output_ceiling,evidence,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, p.Version, p.Destination.Provider, p.Destination.Model, p.Destination.Version, p.Destination.WireContract, p.Region, p.Retention, p.Currency, p.InputMicrosPerToken, p.OutputMicrosPerToken, p.InputTokenCeiling, p.OutputTokenCeiling, p.Evidence, p.ExpiresAt)
	if e != nil {
		return egressError(e)
	}
	stored, e := readLocalModelPrice(ctx, tx, p.Version)
	if e != nil {
		return e
	}
	storedExpiry := stored.ExpiresAt
	stored.ExpiresAt = p.ExpiresAt
	if stored != p || !storedExpiry.Equal(p.ExpiresAt) {
		return modelegressbudget.ErrConflict
	}
	return egressError(tx.Commit(ctx))
}

type egressBudgetRow struct {
	scope       string
	limit, used modelegressbudget.Limits
	currency    string
}

const egressBudgetColumns = `max_requests,max_input,max_output,max_cost,used_requests,allocated_input,allocated_output,allocated_cost`

func scanEgressBudget(row pgx.Row, scope, currency string) (r egressBudgetRow, e error) {
	r.scope = scope
	r.currency = currency
	e = row.Scan(&r.limit.Requests, &r.limit.InputTokens, &r.limit.OutputTokens, &r.limit.CostMicros, &r.used.Requests, &r.used.InputTokens, &r.used.OutputTokens, &r.used.CostMicros)
	return r, egressError(e)
}
func limitsWithin(a, b modelegressbudget.Limits) bool {
	return modelegressbudget.ValidateLimits(a) == nil && a.Requests <= b.Requests && a.InputTokens <= b.InputTokens && a.OutputTokens <= b.OutputTokens && a.CostMicros <= b.CostMicros
}
func (s *Store) ConfigureOwnModelBudget(ctx context.Context, a agentevent.Access, taskID, bindingID, currency string, tenant, subject modelegressbudget.Limits) error {
	if modelegressbudget.ValidateLimits(tenant) != nil || modelegressbudget.ValidateLimits(subject) != nil || len(currency) != 3 {
		return modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	current, _, e := s.egressTask(ctx, tx, a, taskID, bindingID)
	if e != nil {
		return e
	}
	owner := current.agent.Principal.ID
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	for i, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		l := []modelegressbudget.Limits{tenant, subject}[i]
		_, e = tx.Exec(ctx, `INSERT INTO model_budget_accounts(owner_id,scope,currency,max_requests,max_input,max_output,max_cost) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, owner, scope, currency, l.Requests, l.InputTokens, l.OutputTokens, l.CostMicros)
		if e != nil {
			return egressError(e)
		}
		var storedCurrency string
		var stored modelegressbudget.Limits
		e = tx.QueryRow(ctx, `SELECT currency,max_requests,max_input,max_output,max_cost FROM model_budget_accounts WHERE owner_id=$1 AND scope=$2 FOR UPDATE`, owner, scope).Scan(&storedCurrency, &stored.Requests, &stored.InputTokens, &stored.OutputTokens, &stored.CostMicros)
		if e != nil {
			return egressError(e)
		}
		if storedCurrency != currency || stored != l {
			return modelegressbudget.ErrConflict
		}
	}
	if e = egressFinish(ctx, tx, current.sessionID, nil); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

type egressRoot struct {
	owner, agent, task, binding, source, authority, currency string
	limits                                                   modelegressbudget.Limits
	expires                                                  time.Time
}

func readEgressRoot(ctx context.Context, tx pgx.Tx, id string) (r egressRoot, e error) {
	e = tx.QueryRow(ctx, `SELECT owner_id,agent_id,root_task_id,binding_id,source_token,authority_token,currency,max_requests,max_input,max_output,max_cost,expires_at FROM model_budget_roots WHERE root_trace_id=$1`, id).Scan(&r.owner, &r.agent, &r.task, &r.binding, &r.source, &r.authority, &r.currency, &r.limits.Requests, &r.limits.InputTokens, &r.limits.OutputTokens, &r.limits.CostMicros, &r.expires)
	return r, egressError(e)
}
func insertEgressTask(ctx context.Context, tx pgx.Tx, root, owner, task, binding, source, authority string, l modelegressbudget.Limits) error {
	_, e := tx.Exec(ctx, `INSERT INTO model_budget_tasks(root_trace_id,owner_id,task_id,binding_id,source_token,authority_token,max_requests,max_input,max_output,max_cost) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, root, owner, task, binding, source, authority, l.Requests, l.InputTokens, l.OutputTokens, l.CostMicros)
	if e != nil {
		return egressError(e)
	}
	var o, b, so, au string
	var stored modelegressbudget.Limits
	e = tx.QueryRow(ctx, `SELECT owner_id,binding_id,source_token,authority_token,max_requests,max_input,max_output,max_cost FROM model_budget_tasks WHERE root_trace_id=$1 AND task_id=$2 FOR SHARE`, root, task).Scan(&o, &b, &so, &au, &stored.Requests, &stored.InputTokens, &stored.OutputTokens, &stored.CostMicros)
	if e != nil {
		return egressError(e)
	}
	if o != owner || b != binding || so != source || au != authority || stored != l {
		return modelegressbudget.ErrConflict
	}
	return nil
}
func (s *Store) CreateOwnModelBudgetRoot(ctx context.Context, a agentevent.Access, in modelegressbudget.RootInput) error {
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !egressUUID(in.RootTraceID) || modelegressbudget.ValidateLimits(in.Limits) != nil {
		return modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	current, authority, e := s.egressTask(ctx, tx, a, in.TaskID, in.BindingID)
	if e != nil {
		return e
	}
	owner := current.agent.Principal.ID
	if !in.ExpiresAt.After(current.now) || in.ExpiresAt.After(current.now.Add(15*time.Minute)) {
		return modelegressbudget.ErrInvalid
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		var cur string
		var l modelegressbudget.Limits
		e = tx.QueryRow(ctx, `SELECT currency,max_requests,max_input,max_output,max_cost FROM model_budget_accounts WHERE owner_id=$1 AND scope=$2 FOR UPDATE`, owner, scope).Scan(&cur, &l.Requests, &l.InputTokens, &l.OutputTokens, &l.CostMicros)
		if e != nil {
			return egressError(e)
		}
		if cur != in.Currency || !limitsWithin(in.Limits, l) {
			return modelegressbudget.ErrBudget
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_budget_roots(root_trace_id,owner_id,agent_id,root_task_id,binding_id,source_token,authority_token,currency,max_requests,max_input,max_output,max_cost,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT DO NOTHING`, in.RootTraceID, owner, current.agent.AgentID, in.TaskID, in.BindingID, current.version.Token, authority, in.Currency, in.Limits.Requests, in.Limits.InputTokens, in.Limits.OutputTokens, in.Limits.CostMicros, in.ExpiresAt)
	if e != nil {
		return egressError(e)
	}
	root, e := readEgressRoot(ctx, tx, in.RootTraceID)
	if e != nil {
		return e
	}
	if root.owner != owner || root.agent != current.agent.AgentID || root.task != in.TaskID || root.binding != in.BindingID || root.source != current.version.Token || root.authority != authority || root.currency != in.Currency || root.limits != in.Limits || !root.expires.Equal(in.ExpiresAt) {
		return modelegressbudget.ErrConflict
	}
	if e = insertEgressTask(ctx, tx, in.RootTraceID, owner, in.TaskID, in.BindingID, current.version.Token, authority, in.Limits); e != nil {
		return e
	}
	if e = egressFinish(ctx, tx, current.sessionID, nil, root.expires); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}
func (s *Store) currentEgressRoot(ctx context.Context, tx pgx.Tx, a agentevent.Access, rootID string) (egressRoot, currentConfigurationTask, error) {
	root, e := readEgressRoot(ctx, tx, rootID)
	if e != nil {
		return root, currentConfigurationTask{}, e
	}
	current, authority, e := s.egressTask(ctx, tx, a, root.task, root.binding)
	if e != nil {
		return root, current, e
	}
	if root.owner != current.agent.Principal.ID || root.agent != current.agent.AgentID || root.source != current.version.Token || root.authority != authority || !root.expires.After(current.now) {
		return root, current, modelegressbudget.ErrDenied
	}
	return root, current, nil
}
func (s *Store) BindOwnModelBudgetTask(ctx context.Context, a agentevent.Access, in modelegressbudget.TaskInput) error {
	if !egressUUID(in.RootTraceID) || modelegressbudget.ValidateLimits(in.Limits) != nil {
		return modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	root, rootTask, e := s.currentEgressRoot(ctx, tx, a, in.RootTraceID)
	if e != nil {
		return e
	}
	current, authority, e := s.egressTask(ctx, tx, a, in.TaskID, in.BindingID)
	if e != nil {
		return e
	}
	if current.agent != rootTask.agent || !limitsWithin(in.Limits, root.limits) {
		return modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, root.owner); e != nil {
		return e
	}
	if e = insertEgressTask(ctx, tx, in.RootTraceID, root.owner, in.TaskID, in.BindingID, current.version.Token, authority, in.Limits); e != nil {
		return e
	}
	if e = egressFinish(ctx, tx, current.sessionID, nil, root.expires); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

type egressTaskBinding struct{ owner, binding, source, authority string }

func readEgressTask(ctx context.Context, tx pgx.Tx, root, task string) (v egressTaskBinding, e error) {
	e = tx.QueryRow(ctx, `SELECT owner_id,binding_id,source_token,authority_token FROM model_budget_tasks WHERE root_trace_id=$1 AND task_id=$2`, root, task).Scan(&v.owner, &v.binding, &v.source, &v.authority)
	return v, egressError(e)
}
func (s *Store) buildEgressPreview(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.PreviewInput) (p modelegressbudget.Preview, session string, rootExpiry time.Time, err error) {
	// pgx may return the same timestamptz instant in the connection's local
	// location. Normalize before hashing so persisted replay binds exact bytes.
	in.DeadlineAt = in.DeadlineAt.UTC().Truncate(time.Microsecond)
	if !egressUUID(in.RootTraceID) || !egressUUID(in.TaskID) {
		return p, "", rootExpiry, modelegressbudget.ErrInvalid
	}
	root, rt, e := s.currentEgressRoot(ctx, tx, a, in.RootTraceID)
	if e != nil {
		return p, "", rootExpiry, e
	}
	v, e := readEgressTask(ctx, tx, in.RootTraceID, in.TaskID)
	if e != nil {
		return p, "", rootExpiry, e
	}
	current, authority, e := s.egressTask(ctx, tx, a, in.TaskID, v.binding)
	if e != nil {
		return p, "", rootExpiry, e
	}
	if current.agent != rt.agent || v.owner != root.owner || v.source != current.version.Token || v.authority != authority {
		return p, "", rootExpiry, modelegressbudget.ErrDenied
	}
	price, e := readLocalModelPrice(ctx, tx, in.PriceVersion)
	if e != nil {
		return p, "", rootExpiry, e
	}
	if modelegressbudget.ValidatePrice(price, current.now) != nil || price.Currency != root.currency {
		return p, "", rootExpiry, modelegressbudget.ErrDenied
	}
	if _, e = modelegressbudget.Bound(price, in.MaxOutputTokens); e != nil {
		return p, "", rootExpiry, e
	}
	c, e := readTaskModelBinding(ctx, tx, v.binding)
	if e != nil {
		return p, "", rootExpiry, egressError(e)
	}
	config, e := readModelConfiguration(ctx, tx, c.Reference.ConfigurationVersion)
	if e != nil {
		return p, "", rootExpiry, egressError(e)
	}
	if config.Configuration.TaskKind != modelgateway.ActivityQuery || len(config.Configuration.ToolAllowlist) != 0 || config.Configuration.OutputMode == modelgateway.ToolProposals {
		return p, "", rootExpiry, modelegressbudget.ErrDenied
	}
	var originalQuery string
	var filters, conversation []byte
	if e = tx.QueryRow(ctx, `SELECT query,filters,conversation FROM agent_tasks WHERE id=$1`, in.TaskID).Scan(&originalQuery, &filters, &conversation); e != nil {
		return p, "", rootExpiry, egressError(e)
	}
	query, e := currentEgressQuery(originalQuery, filters, conversation)
	if e != nil {
		return p, "", rootExpiry, e
	}
	if !in.DeadlineAt.After(current.now) || in.DeadlineAt.After(current.now.Add(2*time.Minute)) || in.DeadlineAt.After(root.expires) || in.DeadlineAt.After(price.ExpiresAt) {
		return p, "", rootExpiry, modelegressbudget.ErrDenied
	}
	r := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: v.binding, Agent: current.agent, ContextSnapshotRef: in.TaskID, DataPolicyRef: in.RootTraceID, BudgetRef: in.RootTraceID, Budget: modelgateway.Budget{MaxOutputTokens: in.MaxOutputTokens}, Messages: []modelgateway.Message{{Role: "user", Content: query}}, DeadlineAt: in.DeadlineAt}
	r, _, e = modelconfiguration.PrepareRequest(config, r, current.now)
	if e != nil {
		return p, "", rootExpiry, modelegressbudget.ErrDenied
	}
	p = modelegressbudget.Preview{RootTraceID: in.RootTraceID, TaskID: in.TaskID, BindingID: v.binding, SourceToken: v.source, AuthorityToken: authority, RequestDigest: modelegressbudget.Digest(r, price), PriceVersion: price.Version, Request: r, Price: price, ExpiresAt: in.DeadlineAt, Status: "DRAFT"}
	return p, current.sessionID, root.expires, nil
}
func egressAudit(ctx context.Context, tx pgx.Tx, owner, root, op, decision string) error {
	var operation any
	if op != "" {
		operation = op
	}
	_, e := auditExec(ctx, tx, `INSERT INTO model_budget_audit(owner_id,root_trace_id,operation_id,decision) VALUES($1,$2,$3,$4)`, owner, root, operation, decision)
	return egressError(e)
}
func (s *Store) PreviewOwnModelEgress(ctx context.Context, a agentevent.Access, in modelegressbudget.PreviewInput) (modelegressbudget.Preview, error) {
	in.DeadlineAt = in.DeadlineAt.UTC().Truncate(time.Microsecond)
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelegressbudget.Preview{}, e
	}
	defer tx.Rollback(ctx)
	p, session, rootExpiry, e := s.buildEgressPreview(ctx, tx, a, in)
	if e != nil {
		return modelegressbudget.Preview{}, e
	}
	if e = egressOwnerLock(ctx, tx, p.Request.Agent.Principal.ID); e != nil {
		return modelegressbudget.Preview{}, e
	}
	// Lock waits must not turn an expired human preview into an INSERT failure.
	// Recheck current authority before writing; retain the final commit check.
	if e = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); e != nil {
		return modelegressbudget.Preview{}, e
	}
	e = tx.QueryRow(ctx, `INSERT INTO model_egress_previews(root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, p.RootTraceID, p.TaskID, p.Request.Agent.Principal.ID, session, p.Request.Agent.AgentID, p.BindingID, p.SourceToken, p.AuthorityToken, p.RequestDigest, p.PriceVersion, in.MaxOutputTokens, p.ExpiresAt).Scan(&p.ID)
	if e != nil {
		return modelegressbudget.Preview{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, p.Request.Agent.Principal.ID, p.RootTraceID, "", "PREVIEW"); e != nil {
		return modelegressbudget.Preview{}, e
	}
	if e = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); e != nil {
		return modelegressbudget.Preview{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.Preview{}, egressError(e)
	}
	return p, nil
}

type storedEgressPreview struct {
	in                                                                modelegressbudget.PreviewInput
	owner, session, agent, binding, source, authority, digest, status string
}

func readEgressPreview(ctx context.Context, tx pgx.Tx, id string) (v storedEgressPreview, e error) {
	e = tx.QueryRow(ctx, `SELECT root_trace_id,task_id,price_version,max_output_tokens,expires_at,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,status FROM model_egress_previews WHERE id=$1 FOR UPDATE`, id).Scan(&v.in.RootTraceID, &v.in.TaskID, &v.in.PriceVersion, &v.in.MaxOutputTokens, &v.in.DeadlineAt, &v.owner, &v.session, &v.agent, &v.binding, &v.source, &v.authority, &v.digest, &v.status)
	return v, egressError(e)
}
func matchEgressPreview(v storedEgressPreview, p modelegressbudget.Preview, session string) bool {
	return v.owner == p.Request.Agent.Principal.ID && v.session == session && v.agent == p.Request.Agent.AgentID && v.binding == p.BindingID && v.source == p.SourceToken && v.authority == p.AuthorityToken && v.digest == p.RequestDigest
}
func (s *Store) ApproveOwnModelEgress(ctx context.Context, a agentevent.Access, id, expectedDigest string) error {
	if !egressUUID(id) || !modelconfiguration.ValidDigest(expectedDigest) {
		return modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	v, e := readEgressPreview(ctx, tx, id)
	if e != nil {
		return e
	}
	if v.owner != owner || v.session != session || v.digest != expectedDigest || v.status == "REVOKED" {
		return modelegressbudget.ErrDenied
	}
	p, sourceSession, rootExpiry, e := s.buildEgressPreview(ctx, tx, a, v.in)
	if e != nil {
		return e
	}
	if !matchEgressPreview(v, p, sourceSession) {
		return modelegressbudget.ErrDenied
	}
	if v.status == "DRAFT" {
		_, e = tx.Exec(ctx, `UPDATE model_egress_previews SET status='APPROVED',revision=revision+1,approved_at=clock_timestamp() WHERE id=$1`, id)
		if e != nil {
			return egressError(e)
		}
		if e = egressAudit(ctx, tx, owner, p.RootTraceID, "", "APPROVE"); e != nil {
			return e
		}
	}
	if e = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

// Revocation/accounting authenticate the current owner even if the original
// Task or Agent metadata has since been removed. No private source is returned.
func (s *Store) RevokeOwnModelEgress(ctx context.Context, a agentevent.Access, id string) error {
	if !egressUUID(id) {
		return modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	v, e := readEgressPreview(ctx, tx, id)
	if e != nil {
		return e
	}
	if v.owner != owner {
		return modelegressbudget.ErrDenied
	}
	if v.status != "REVOKED" {
		_, e = tx.Exec(ctx, `UPDATE model_egress_previews SET status='REVOKED',revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1`, id)
		if e != nil {
			return egressError(e)
		}
		if e = egressAudit(ctx, tx, owner, v.in.RootTraceID, "", "REVOKE"); e != nil {
			return e
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}
func readEgressReservation(ctx context.Context, tx pgx.Tx, id, owner string) (r modelegressbudget.Reservation, e error) {
	e = tx.QueryRow(ctx, `SELECT operation_id,preview_id,root_trace_id,task_id,price_version,request_digest,state,currency,upper_input,upper_output,upper_cost,reported_input,reported_output,execution_status,created_at FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2 FOR UPDATE`, id, owner).Scan(&r.OperationID, &r.PreviewID, &r.RootTraceID, &r.TaskID, &r.PriceVersion, &r.RequestDigest, &r.State, &r.Currency, &r.Upper.InputTokens, &r.Upper.OutputTokens, &r.Upper.CostMicros, &r.ReportedInput, &r.ReportedOutput, &r.ExecutionStatus, &r.CreatedAt)
	return r, egressError(e)
}

// Shared accounting metadata remains readable by its typed native paths. Old
// LOCAL methods must separately reject live rows before even an idempotent
// UNKNOWN return; state, phase and SQL transition constraints are not enough.
func requireLocalReservation(ctx context.Context, tx pgx.Tx, id, owner string) error {
	var kind string
	if e := tx.QueryRow(ctx, `SELECT billing_kind FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2 FOR UPDATE`, id, owner).Scan(&kind); e != nil {
		return egressError(e)
	}
	if kind != "LOCAL" {
		return modelegressbudget.ErrDenied
	}
	return nil
}
func readLocalEgressReservation(ctx context.Context, tx pgx.Tx, id, owner string) (modelegressbudget.Reservation, error) {
	if e := requireLocalReservation(ctx, tx, id, owner); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	return readEgressReservation(ctx, tx, id, owner)
}
func readEgressBudgets(ctx context.Context, tx pgx.Tx, owner, root, task, currency string) ([]egressBudgetRow, error) {
	rows := make([]egressBudgetRow, 0, 4)
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		r, e := scanEgressBudget(tx.QueryRow(ctx, `SELECT `+egressBudgetColumns+` FROM model_budget_accounts WHERE owner_id=$1 AND scope=$2 AND currency=$3 FOR UPDATE`, owner, scope, currency), scope, currency)
		if e != nil {
			return nil, e
		}
		rows = append(rows, r)
	}
	r, e := scanEgressBudget(tx.QueryRow(ctx, `SELECT `+egressBudgetColumns+` FROM model_budget_roots WHERE owner_id=$1 AND root_trace_id=$2 AND currency=$3 FOR UPDATE`, owner, root, currency), "ROOT", currency)
	if e != nil {
		return nil, e
	}
	rows = append(rows, r)
	r, e = scanEgressBudget(tx.QueryRow(ctx, `SELECT `+egressBudgetColumns+` FROM model_budget_tasks WHERE owner_id=$1 AND root_trace_id=$2 AND task_id=$3 FOR UPDATE`, owner, root, task), "TASK", currency)
	if e != nil {
		return nil, e
	}
	return append(rows, r), nil
}
func changeEgressBudgets(ctx context.Context, tx pgx.Tx, owner, root, task string, requests int64, delta modelegressbudget.Amount) error {
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		_, e := tx.Exec(ctx, `UPDATE model_budget_accounts SET used_requests=used_requests+$3,allocated_input=allocated_input+$4,allocated_output=allocated_output+$5,allocated_cost=allocated_cost+$6 WHERE owner_id=$1 AND scope=$2`, owner, scope, requests, delta.InputTokens, delta.OutputTokens, delta.CostMicros)
		if e != nil {
			return egressError(e)
		}
	}
	_, e := tx.Exec(ctx, `UPDATE model_budget_roots SET used_requests=used_requests+$3,allocated_input=allocated_input+$4,allocated_output=allocated_output+$5,allocated_cost=allocated_cost+$6 WHERE owner_id=$1 AND root_trace_id=$2`, owner, root, requests, delta.InputTokens, delta.OutputTokens, delta.CostMicros)
	if e != nil {
		return egressError(e)
	}
	_, e = tx.Exec(ctx, `UPDATE model_budget_tasks SET used_requests=used_requests+$4,allocated_input=allocated_input+$5,allocated_output=allocated_output+$6,allocated_cost=allocated_cost+$7 WHERE owner_id=$1 AND root_trace_id=$2 AND task_id=$3`, owner, root, task, requests, delta.InputTokens, delta.OutputTokens, delta.CostMicros)
	return egressError(e)
}
func egressTicket(c *agentfeature.Controller) (agentfeature.Ticket, error) {
	t, e := c.Capture(agentfeature.Enrichment)
	if e != nil {
		return t, modelegressbudget.ErrUnavailable
	}
	return t, nil
}
func (s *Store) ReserveOwnModelAttempt(ctx context.Context, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller) (modelegressbudget.Reservation, error) {
	if !egressUUID(in.OperationID) || !egressUUID(in.PreviewID) || !egressUUID(in.RootTraceID) || !egressUUID(in.TaskID) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrInvalid
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = denyLinkedModelOperation(ctx, tx, in.OperationID); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	out, e := s.reserveModelAttemptTx(ctx, tx, a, in, c, ticket, owner, session)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.Reservation{}, egressError(e)
	}
	return out, nil
}

// Internal same-transaction primitive. The caller owns locks, original ticket
// and the single Commit. It never begins or commits another transaction.
func (s *Store) reserveModelAttemptTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller, ticket agentfeature.Ticket, owner, session string) (modelegressbudget.Reservation, error) {
	v, e := readEgressPreview(ctx, tx, in.PreviewID)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if v.owner != owner || v.session != session || v.status != "APPROVED" || v.in.RootTraceID != in.RootTraceID || v.in.TaskID != in.TaskID {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	p, sourceSession, rootExpiry, e := s.buildEgressPreview(ctx, tx, a, v.in)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if !matchEgressPreview(v, p, sourceSession) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	old, oldErr := readLocalEgressReservation(ctx, tx, in.OperationID, owner)
	if oldErr == nil {
		if old.PreviewID != in.PreviewID || old.RootTraceID != in.RootTraceID || old.TaskID != in.TaskID || old.RequestDigest != p.RequestDigest {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
		}
		if e = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		if !c.Current(ticket) {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
		}
		return old, nil
	}
	if !errors.Is(oldErr, modelegressbudget.ErrDenied) {
		return modelegressbudget.Reservation{}, oldErr
	}
	amount, e := modelegressbudget.Bound(p.Price, p.Request.Budget.MaxOutputTokens)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	rows, e := readEgressBudgets(ctx, tx, owner, p.RootTraceID, p.TaskID, p.Price.Currency)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	for _, r := range rows {
		if !modelegressbudget.Fits(r.limit, r.used, amount) {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrBudget
		}
	}
	if e = changeEgressBudgets(ctx, tx, owner, p.RootTraceID, p.TaskID, 1, amount); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	// A global operation UUID conflict rolls this entire transaction back. It
	// cannot return another owner's record or retain partially changed counters.
	_, e = tx.Exec(ctx, `INSERT INTO model_budget_reservations(operation_id,preview_id,root_trace_id,task_id,owner_id,price_version,request_digest,currency,upper_input,upper_output,upper_cost) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`, in.OperationID, in.PreviewID, p.RootTraceID, p.TaskID, owner, p.Price.Version, p.RequestDigest, p.Price.Currency, amount.InputTokens, amount.OutputTokens, amount.CostMicros)
	if e != nil {
		return modelegressbudget.Reservation{}, egressError(e)
	}
	r, e := readLocalEgressReservation(ctx, tx, in.OperationID, owner)
	if e != nil {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
	}
	if e = egressAudit(ctx, tx, owner, p.RootTraceID, in.OperationID, "RESERVE"); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if !c.Current(ticket) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
	}
	return r, nil
}

// BeginOwnLocalModelAttempt consumes a reservation once and returns only the
// server-assembled local contract request. It installs no provider adapter.
func (s *Store) BeginOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller) (modelgateway.Request, error) {
	if !egressUUID(operation) {
		return modelgateway.Request{}, modelegressbudget.ErrInvalid
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return modelgateway.Request{}, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelgateway.Request{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return modelgateway.Request{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelgateway.Request{}, e
	}
	if e = denyLinkedModelOperation(ctx, tx, operation); e != nil {
		return modelgateway.Request{}, e
	}
	out, e := s.beginModelAttemptTx(ctx, tx, a, operation, c, ticket, owner, session)
	if e != nil {
		return modelgateway.Request{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelgateway.Request{}, egressError(e)
	}
	return out, nil
}

// Internal same-transaction primitive. The caller owns locks, original ticket
// and the single Commit. It never begins or commits another transaction.
func (s *Store) beginModelAttemptTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, operation string, c *agentfeature.Controller, ticket agentfeature.Ticket, owner, session string) (modelgateway.Request, error) {
	r, e := readLocalEgressReservation(ctx, tx, operation, owner)
	if e != nil {
		return modelgateway.Request{}, e
	}
	if r.State != "RESERVED" {
		return modelgateway.Request{}, modelegressbudget.ErrConflict
	}
	v, e := readEgressPreview(ctx, tx, r.PreviewID)
	if e != nil {
		return modelgateway.Request{}, e
	}
	if v.status != "APPROVED" || v.owner != owner || v.session != session {
		return modelgateway.Request{}, modelegressbudget.ErrDenied
	}
	p, sourceSession, rootExpiry, e := s.buildEgressPreview(ctx, tx, a, v.in)
	if e != nil {
		return modelgateway.Request{}, e
	}
	if !matchEgressPreview(v, p, sourceSession) || r.RequestDigest != p.RequestDigest {
		return modelgateway.Request{}, modelegressbudget.ErrDenied
	}
	_, e = tx.Exec(ctx, `UPDATE model_budget_reservations SET state='IN_FLIGHT' WHERE operation_id=$1`, operation)
	if e != nil {
		return modelgateway.Request{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, owner, p.RootTraceID, operation, "IN_FLIGHT"); e != nil {
		return modelgateway.Request{}, e
	}
	if e = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); e != nil {
		return modelgateway.Request{}, e
	}
	if !c.Current(ticket) {
		return modelgateway.Request{}, modelegressbudget.ErrUnavailable
	}
	return p.Request, nil
}
func (s *Store) SettleOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, operation string, usage modelegressbudget.LocalUsage) (modelegressbudget.Reservation, error) {
	return s.finishOwnLocalModelAttempt(ctx, a, operation, usage, false)
}
func (s *Store) CancelOwnReservedModelAttempt(ctx context.Context, a agentevent.Access, operation string) (modelegressbudget.Reservation, error) {
	return s.finishOwnLocalModelAttempt(ctx, a, operation, modelegressbudget.LocalUsage{}, true)
}
func (s *Store) finishOwnLocalModelAttempt(ctx context.Context, a agentevent.Access, operation string, usage modelegressbudget.LocalUsage, cancelBeforeSend bool) (modelegressbudget.Reservation, error) {
	if !egressUUID(operation) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	linked, e := lockModelRunAccounting(ctx, tx, owner, operation)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	out, e := s.finishModelAttemptTx(ctx, tx, operation, usage, cancelBeforeSend, owner, session)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if linked != nil {
		if e = syncModelRunAccounting(ctx, tx, linked, out); e != nil {
			return modelegressbudget.Reservation{}, e
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.Reservation{}, egressError(e)
	}
	return out, nil
}
func (s *Store) finishModelAttemptTx(ctx context.Context, tx pgx.Tx, operation string, usage modelegressbudget.LocalUsage, cancelBeforeSend bool, owner, session string) (modelegressbudget.Reservation, error) {
	r, e := readLocalEgressReservation(ctx, tx, operation, owner)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	known, input, output := usage.Values()
	target := "UNKNOWN"
	delta := modelegressbudget.Amount{}
	var reportInput, reportOutput, settledCost any
	if cancelBeforeSend {
		target = "CANCELLED_BEFORE_SEND"
		delta = modelegressbudget.Amount{InputTokens: -r.Upper.InputTokens, OutputTokens: -r.Upper.OutputTokens, CostMicros: -r.Upper.CostMicros}
	} else if known {
		if input > r.Upper.InputTokens || output > r.Upper.OutputTokens {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrInvalid
		}
		p, e := readLocalModelPrice(ctx, tx, r.PriceVersion)
		if e != nil {
			return modelegressbudget.Reservation{}, e
		}
		if p.Evidence != modelegressbudget.LocalPrice || p.Currency != r.Currency {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
		}
		cost := input*p.InputMicrosPerToken + output*p.OutputMicrosPerToken
		if cost > r.Upper.CostMicros {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrInvalid
		}
		target = "SETTLED"
		reportInput = input
		reportOutput = output
		settledCost = cost
		delta = modelegressbudget.Amount{InputTokens: input - r.Upper.InputTokens, OutputTokens: output - r.Upper.OutputTokens, CostMicros: cost - r.Upper.CostMicros}
	}
	if r.State == target {
		if target == "SETTLED" && (r.ReportedInput == nil || r.ReportedOutput == nil || *r.ReportedInput != input || *r.ReportedOutput != output) {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
		}
	} else {
		allowed := (cancelBeforeSend && r.State == "RESERVED") || (!cancelBeforeSend && (r.State == "IN_FLIGHT" || (known && r.State == "UNKNOWN")))
		if !allowed {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
		}
		if _, e = readEgressBudgets(ctx, tx, owner, r.RootTraceID, r.TaskID, r.Currency); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		if e = changeEgressBudgets(ctx, tx, owner, r.RootTraceID, r.TaskID, 0, delta); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		_, e = tx.Exec(ctx, `UPDATE model_budget_reservations SET state=$2,reported_input=$3,reported_output=$4,settled_cost=$5 WHERE operation_id=$1`, operation, target, reportInput, reportOutput, settledCost)
		if e != nil {
			return modelegressbudget.Reservation{}, egressError(e)
		}
		if e = egressAudit(ctx, tx, owner, r.RootTraceID, operation, target); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		r, e = readLocalEgressReservation(ctx, tx, operation, owner)
		if e != nil {
			return modelegressbudget.Reservation{}, e
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	return r, nil
}
func (s *Store) ReadOwnModelBudget(ctx context.Context, a agentevent.Access, root, task string) ([]modelegressbudget.BudgetView, error) {
	if !egressUUID(root) || !egressUUID(task) {
		return nil, modelegressbudget.ErrInvalid
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
	var currency string
	if e = tx.QueryRow(ctx, `SELECT currency FROM model_budget_roots WHERE root_trace_id=$1 AND owner_id=$2`, root, owner).Scan(&currency); e != nil {
		return nil, egressError(e)
	}
	rows, e := readEgressBudgets(ctx, tx, owner, root, task, currency)
	if e != nil {
		return nil, e
	}
	result := make([]modelegressbudget.BudgetView, 0, 4)
	for _, r := range rows {
		result = append(result, modelegressbudget.BudgetView{Scope: r.scope, Limits: r.limit, Allocated: r.used, Currency: r.currency})
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, egressError(e)
	}
	return result, nil
}
