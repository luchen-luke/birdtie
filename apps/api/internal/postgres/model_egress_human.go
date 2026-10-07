package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/jackc/pgx/v5"
	"time"
)

// Acquire every relation before owner/source row waits. GETs never register,
// approve, reserve, audit, renew a deadline, or create an execution capability.
func (s *Store) beginHumanEgressRead(ctx context.Context, a agentevent.Access) (pgx.Tx, string, string, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return nil, "", "", e
	}
	fail := func(e error) (pgx.Tx, string, string, error) {
		tx.Rollback(context.Background())
		return nil, "", "", e
	}
	_, e = tx.Exec(ctx, `LOCK TABLE sessions,accounts,agents,agent_profiles,agent_tasks,agent_task_model_bindings,model_prompt_versions,model_configuration_policy_versions,model_configuration_versions,model_local_price_versions,model_budget_accounts,model_budget_roots,model_budget_tasks,model_egress_previews IN ACCESS SHARE MODE`)
	if e != nil {
		return fail(egressError(e))
	}
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return fail(e)
	}
	return tx, owner, session, nil
}

func humanReadDenied(e error) bool {
	return errors.Is(e, modelegressbudget.ErrDenied) || errors.Is(e, modelegressbudget.ErrInvalid)
}
func humanDeadlineLimit(v *time.Time, limit time.Time) {
	if limit.Before(*v) {
		*v = limit
	}
}

func (s *Store) ListOwnModelEgressOptions(ctx context.Context, a agentevent.Access) (modelegressbudget.HumanOptions, error) {
	tx, owner, session, e := s.beginHumanEgressRead(ctx, a)
	if e != nil {
		return modelegressbudget.HumanOptions{}, e
	}
	defer tx.Rollback(context.Background())
	out := modelegressbudget.HumanOptions{SchemaVersion: modelegressbudget.HumanSchemaVersion, OwnerID: owner, Options: []modelegressbudget.HumanOption{}, ModelAccess: "UNAVAILABLE"}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelegressbudget.HumanOptions{}, e
	}
	// Bound candidate work as well as output; these selectors grant nothing.
	rows, e := tx.Query(ctx, `SELECT bt.root_trace_id,bt.task_id,p.version FROM model_budget_tasks bt JOIN model_budget_roots r ON r.root_trace_id=bt.root_trace_id AND r.owner_id=bt.owner_id JOIN model_local_price_versions p ON p.currency=r.currency WHERE bt.owner_id=$1 AND r.expires_at>clock_timestamp() AND p.expires_at>clock_timestamp() ORDER BY bt.created_at DESC,bt.root_trace_id,bt.task_id,p.version LIMIT 100`, owner)
	if e != nil {
		return out, egressError(e)
	}
	type choice struct{ root, task, price string }
	choices := []choice{}
	for rows.Next() {
		var c choice
		if e = rows.Scan(&c.root, &c.task, &c.price); e != nil {
			rows.Close()
			return out, egressError(e)
		}
		choices = append(choices, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, egressError(e)
	}
	var sessionExpiry time.Time
	if e = tx.QueryRow(ctx, `SELECT least(expires_at,idle_expires_at),clock_timestamp() FROM sessions WHERE id=$1`, session).Scan(&sessionExpiry, &out.ObservedAt); e != nil {
		return out, egressError(e)
	}
	previews := []modelegressbudget.Preview{}
	roots := []time.Time{}
	for _, c := range choices {
		if len(out.Options) >= 20 {
			break
		}
		root, _, err := s.currentEgressRoot(ctx, tx, a, c.root)
		if err != nil {
			if humanReadDenied(err) {
				continue
			}
			return out, err
		}
		price, err := readLocalModelPrice(ctx, tx, c.price)
		if err != nil {
			if humanReadDenied(err) {
				continue
			}
			return out, err
		}
		var now time.Time
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return out, egressError(e)
		}
		deadline := now.Add(2 * time.Minute)
		humanDeadlineLimit(&deadline, root.expires)
		humanDeadlineLimit(&deadline, price.ExpiresAt)
		humanDeadlineLimit(&deadline, sessionExpiry)
		p, _, rootExpiry, err := s.buildEgressPreview(ctx, tx, a, modelegressbudget.PreviewInput{RootTraceID: c.root, TaskID: c.task, PriceVersion: c.price, MaxOutputTokens: int(price.OutputTokenCeiling), DeadlineAt: deadline})
		if err != nil {
			if humanReadDenied(err) {
				continue
			}
			return out, err
		}
		// Require all four native budget rows. Reading never configures quotas.
		if _, err = readEgressBudgets(ctx, tx, owner, c.root, c.task, price.Currency); err != nil {
			if humanReadDenied(err) {
				continue
			}
			return out, err
		}
		binding, err := readTaskModelBinding(ctx, tx, p.BindingID)
		if err != nil {
			return out, egressError(err)
		}
		var query string
		if e = tx.QueryRow(ctx, `SELECT query FROM agent_tasks WHERE id=$1`, c.task).Scan(&query); e != nil {
			return out, egressError(e)
		}
		out.Options = append(out.Options, modelegressbudget.HumanOption{RootTraceID: c.root, TaskID: c.task, TaskQuery: query, PriceVersion: c.price, ConfigurationVersion: binding.Reference.ConfigurationVersion, PromptVersion: p.Request.PromptVersion, InputSchemaVersion: p.Request.InputSchemaVersion, OutputSchemaVersion: p.Request.OutputSchemaVersion, Destination: price.Destination, Region: price.Region, Retention: price.Retention, Currency: price.Currency, Evidence: price.Evidence, MaxOutputTokens: int(price.OutputTokenCeiling), MaxDeadlineAt: p.ExpiresAt})
		previews = append(previews, p)
		roots = append(roots, rootExpiry)
	}
	// All selected source rows are held. Check time after every potential wait,
	// including later candidates, config/price and quota rows, in the last SQL.
	expiry := sessionExpiry
	for i, p := range previews {
		humanDeadlineLimit(&expiry, p.ExpiresAt)
		humanDeadlineLimit(&expiry, p.Price.ExpiresAt)
		humanDeadlineLimit(&expiry, roots[i])
	}
	if e = egressFinish(ctx, tx, session, nil, expiry); e != nil {
		return modelegressbudget.HumanOptions{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.HumanOptions{}, egressError(e)
	}
	return out, ctx.Err()
}

const humanReceiptColumns = `id,root_trace_id,task_id,price_version,max_output_tokens,status,revision,created_at,expires_at,approved_at,revoked_at`

func scanHumanEgressReceipt(row scanner, owner string) (r modelegressbudget.HumanReceipt, e error) {
	r.SchemaVersion = modelegressbudget.HumanSchemaVersion
	r.OwnerID = owner
	r.ModelAccess = "UNAVAILABLE"
	e = row.Scan(&r.PreviewID, &r.RootTraceID, &r.TaskID, &r.PriceVersion, &r.MaxOutputTokens, &r.Status, &r.Revision, &r.CreatedAt, &r.ExpiresAt, &r.ApprovedAt, &r.RevokedAt)
	r.Revocable = r.Status != "REVOKED"
	return r, egressError(e)
}
func (s *Store) ListOwnModelEgressReceipts(ctx context.Context, a agentevent.Access) ([]modelegressbudget.HumanReceipt, error) {
	tx, owner, session, e := s.beginHumanEgressRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT `+humanReceiptColumns+` FROM model_egress_previews WHERE owner_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50 FOR SHARE`, owner)
	if e != nil {
		return nil, egressError(e)
	}
	out := []modelegressbudget.HumanReceipt{}
	for rows.Next() {
		r, err := scanHumanEgressReceipt(rows, owner)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, egressError(e)
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, egressError(e)
	}
	return out, ctx.Err()
}
func (s *Store) ReadOwnModelEgressReceipt(ctx context.Context, a agentevent.Access, id string) (modelegressbudget.HumanReceipt, error) {
	if !egressUUID(id) {
		return modelegressbudget.HumanReceipt{}, modelegressbudget.ErrInvalid
	}
	tx, owner, session, e := s.beginHumanEgressRead(ctx, a)
	if e != nil {
		return modelegressbudget.HumanReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	// Use the original serialization key; concurrent Approve/Revoke is observed,
	// not inferred from an older receipt or from unchanged budget counters.
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelegressbudget.HumanReceipt{}, e
	}
	r, e := scanHumanEgressReceipt(tx.QueryRow(ctx, `SELECT `+humanReceiptColumns+` FROM model_egress_previews WHERE id=$1 AND owner_id=$2 FOR SHARE`, id, owner), owner)
	if e != nil {
		return r, e
	}
	v, e := readEgressPreview(ctx, tx, id)
	if e != nil {
		return r, e
	}
	if v.session == session && v.status != "REVOKED" {
		p, sourceSession, rootExpiry, err := s.buildEgressPreview(ctx, tx, a, v.in)
		if err == nil && matchEgressPreview(v, p, sourceSession) {
			p.ID = id
			p.Status = v.status
			if err = egressFinish(ctx, tx, session, &p.Request, rootExpiry, p.Price.ExpiresAt); err == nil {
				r.Preview = &p
				r.Reviewable = true
				r.Approvable = v.status == "DRAFT"
			}
		}
		if err != nil && !humanReadDenied(err) {
			return modelegressbudget.HumanReceipt{}, err
		}
	}
	// Owner history/revocation remains available even after the original source,
	// Session or deadline expires. Current owner session must still be live.
	// A reviewable response already used the final combined request/session
	// clock above. Do not append another owner-only statement after it.
	if r.Preview == nil {
		if e = egressFinish(ctx, tx, session, nil); e != nil {
			return modelegressbudget.HumanReceipt{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.HumanReceipt{}, egressError(e)
	}
	return r, ctx.Err()
}
