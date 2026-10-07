package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// An optional optimization, not a permission/producer/receipt. Older migration
// stages retain their original capture when any protective table/column is
// absent. In particular absence of a later preview is not absence of consent.
const outboxRefreshCatalogSQL = `WITH required(tbl,col,typ) AS (VALUES
 ('agent_domain_outbox','event_id','uuid'),('agent_domain_outbox','schema_version','text'),
 ('agent_domain_outbox','event_type','text'),('agent_domain_outbox','tenant_id','uuid'),
 ('agent_domain_outbox','subject_id','uuid'),('agent_domain_outbox','actor_id','uuid'),
 ('agent_domain_outbox','agent_id','uuid'),('agent_domain_outbox','source_type','text'),
 ('agent_domain_outbox','source_id','uuid'),('agent_domain_outbox','source_revision','bigint'),
 ('agent_domain_outbox','source_fingerprint','text'),('agent_domain_outbox','source_status','text'),
 ('agent_domain_outbox','logical_operation_id','uuid'),('agent_domain_outbox','root_trace_id','uuid'),
 ('agent_domain_outbox','causation_id','uuid'),('agent_domain_outbox','occurred_at','timestamp with time zone'),
 ('agent_domain_outbox','received_at','timestamp with time zone'),('agent_domain_outbox','expires_at','timestamp with time zone'),
 ('agent_domain_outbox','delivery_state','text'),('agent_domain_outbox','attempt','bigint'),
 ('agent_domain_outbox','fence','bigint'),('agent_domain_outbox','lease_owner','uuid'),
 ('agent_domain_outbox','lease_until','timestamp with time zone'),('agent_domain_outbox','updated_at','timestamp with time zone'),
 ('agent_consumer_inbox','event_id','uuid'),('agent_consumer_inbox','subject_id','uuid'),
 ('agent_effect_ledger','subject_id','uuid'),('agent_effect_ledger','agent_id','uuid'),
 ('agent_effect_ledger','logical_operation_id','uuid'),('agent_effect_ledger','source_id','uuid'),
 ('agent_effect_ledger','source_revision','bigint'),
 ('agent_enrichment_purpose_previews','owner_id','uuid'),('agent_enrichment_purpose_previews','agent_id','uuid'),
 ('agent_enrichment_purpose_previews','moment_id','uuid'),
 ('agent_candidate_retention_previews','owner_id','uuid'),('agent_candidate_retention_previews','agent_id','uuid'),
 ('agent_candidate_retention_previews','event_id','uuid'),('agent_candidate_retention_previews','logical_operation_id','uuid'),
 ('agent_multi_candidate_previews','owner_id','uuid'),('agent_multi_candidate_previews','agent_id','uuid'),
 ('agent_multi_candidate_previews','event_id','uuid'),('agent_multi_candidate_previews','logical_operation_id','uuid'),
 ('agent_enrichment_runs','owner_id','uuid'),('agent_enrichment_runs','agent_id','uuid'),
 ('agent_enrichment_runs','event_id','uuid'),('agent_enrichment_runs','logical_operation_id','uuid'),
 ('agent_enrichment_runs','source_id','uuid'),('agent_enrichment_runs','source_revision','bigint')
 ) SELECT NOT EXISTS(SELECT 1 FROM required r LEFT JOIN pg_catalog.pg_class c
 ON c.oid=to_regclass('public.'||r.tbl) AND c.relkind IN('r','p')
 LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid AND a.attname=r.col AND a.attnum>0 AND NOT a.attisdropped
 WHERE a.attname IS NULL OR format_type(a.atttypid,a.atttypmod)<>r.typ)`

// The writer already holds the Moment mutation lock. Claim takes the reverse
// row order (outbox then Moment), so never wait for an old event here. All six
// history predicates protect terminal and expired history as well as live rows.
// Lock at most 64 controls, then obtain one clock after the lock CTE is drained.
const outboxRefreshCoalesceSQL = `WITH locked AS MATERIALIZED(
 SELECT d.event_id FROM public.agent_domain_outbox d
 WHERE d.schema_version='agent-outbox-v1' AND d.source_type='MOMENT'
 AND d.tenant_id=$2 AND d.subject_id=$2 AND d.actor_id=$2 AND d.agent_id=$3 AND d.source_id=$4
 AND d.event_id<>$1 AND d.source_revision<$5 AND d.source_status='draft'
 AND d.event_type IN('MOMENT_CREATED','MOMENT_UPDATED')
 AND d.delivery_state='PENDING' AND d.attempt=0 AND d.fence=0 AND d.lease_owner IS NULL AND d.lease_until IS NULL
 AND d.causation_id IS NULL AND d.root_trace_id=d.logical_operation_id
 AND d.received_at BETWEEN $8::timestamptz-INTERVAL '5 seconds' AND $8::timestamptz
 AND d.occurred_at<=$7 AND d.expires_at>$8
 AND NOT EXISTS(SELECT 1 FROM public.agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id)
 AND NOT EXISTS(SELECT 1 FROM public.agent_effect_ledger ef WHERE ef.subject_id=d.subject_id AND ef.agent_id=d.agent_id
  AND(ef.logical_operation_id=d.logical_operation_id OR(ef.source_id=d.source_id AND ef.source_revision=d.source_revision)))
 AND NOT EXISTS(SELECT 1 FROM public.agent_enrichment_purpose_previews p WHERE p.owner_id=d.subject_id AND p.agent_id=d.agent_id AND p.moment_id=d.source_id)
 AND NOT EXISTS(SELECT 1 FROM public.agent_candidate_retention_previews p WHERE p.owner_id=d.subject_id AND p.agent_id=d.agent_id AND(p.event_id=d.event_id OR p.logical_operation_id=d.logical_operation_id))
 AND NOT EXISTS(SELECT 1 FROM public.agent_multi_candidate_previews p WHERE p.owner_id=d.subject_id AND p.agent_id=d.agent_id AND(p.event_id=d.event_id OR p.logical_operation_id=d.logical_operation_id))
 AND NOT EXISTS(SELECT 1 FROM public.agent_enrichment_runs r WHERE r.owner_id=d.subject_id AND r.agent_id=d.agent_id
  AND(r.event_id=d.event_id OR r.logical_operation_id=d.logical_operation_id OR(r.source_id=d.source_id AND r.source_revision=d.source_revision)))
 ORDER BY d.source_revision,d.event_id LIMIT 64 FOR UPDATE OF d SKIP LOCKED
 ), clk AS MATERIALIZED(SELECT clock_timestamp() at,count(*) locked_count FROM locked),
 current_source AS MATERIALIZED(
 SELECT newest.event_id FROM public.agent_domain_outbox newest
 JOIN public.moments m ON m.id=newest.source_id AND m.author_account_id=newest.subject_id
 JOIN public.accounts actor ON actor.id=m.author_account_id
 JOIN public.agents ag ON ag.id=newest.agent_id AND ag.principal_account_id=actor.id
 JOIN public.agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=actor.id AND ap.owner_type='PERSON'
 CROSS JOIN clk WHERE newest.event_id=$1 AND newest.subject_id=$2 AND newest.tenant_id=$2 AND newest.actor_id=$2
 AND newest.agent_id=$3 AND newest.source_id=$4 AND newest.source_revision=$5 AND newest.source_fingerprint=$6
 AND newest.event_type='MOMENT_UPDATED' AND newest.schema_version='agent-outbox-v1' AND newest.source_type='MOMENT'
 AND newest.source_status='draft' AND newest.delivery_state='PENDING' AND newest.attempt=0 AND newest.fence=0
 AND newest.lease_owner IS NULL AND newest.lease_until IS NULL AND newest.causation_id IS NULL
 AND newest.root_trace_id=newest.logical_operation_id
 AND newest.occurred_at=$7 AND newest.received_at=$8 AND newest.expires_at=$9
 AND newest.received_at<=clk.at AND newest.updated_at<=clk.at AND newest.expires_at>clk.at
 AND newest.expires_at=newest.occurred_at+INTERVAL '15 minutes'
 AND m.visibility='private' AND m.status='draft' AND m.revision=newest.source_revision AND m.updated_at=newest.occurred_at
 AND actor.account_type='person' AND actor.status='active' AND ag.agent_type='personal' AND ag.status='active'
 AND newest.source_fingerprint=encode(sha256(convert_to(jsonb_build_object(
 'moment',to_jsonb(m),'moment_xmin',m.xmin::text,
 'actor',to_jsonb(actor),'actor_xmin',actor.xmin::text,
 'agent',to_jsonb(ag),'agent_xmin',ag.xmin::text,
 'metadata',to_jsonb(ap),'metadata_xmin',ap.xmin::text,
 'activity_links',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.activity_id) FROM public.moment_activity_links l WHERE l.moment_id=m.id),'[]'::jsonb),
 'community_links',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.community_id) FROM public.moment_community_links l WHERE l.moment_id=m.id),'[]'::jsonb),
 'organization_links',COALESCE((SELECT jsonb_agg(to_jsonb(l) ORDER BY l.organization_id) FROM public.moment_organization_links l WHERE l.moment_id=m.id),'[]'::jsonb)
 )::text,'UTF8')),'hex')
 ) UPDATE public.agent_domain_outbox d SET delivery_state='INVALIDATED',updated_at=clk.at
 FROM locked,clk,current_source WHERE d.event_id=locked.event_id AND clk.locked_count>=0
 AND d.subject_id=$2 AND d.agent_id=$3 AND d.source_id=$4 AND d.source_revision<$5
 AND d.delivery_state='PENDING' AND d.attempt=0 AND d.fence=0 AND d.lease_owner IS NULL AND d.lease_until IS NULL
 AND d.updated_at<=clk.at AND d.received_at<=clk.at AND d.expires_at>clk.at
 AND d.received_at>=clk.at-INTERVAL '5 seconds'
 AND clk.at-$8::timestamptz<=INTERVAL '5 seconds'
 AND NOT EXISTS(SELECT 1 FROM public.agent_consumer_inbox i WHERE i.event_id=d.event_id AND i.subject_id=d.subject_id)
 AND NOT EXISTS(SELECT 1 FROM public.agent_effect_ledger ef WHERE ef.subject_id=d.subject_id AND ef.agent_id=d.agent_id
  AND(ef.logical_operation_id=d.logical_operation_id OR(ef.source_id=d.source_id AND ef.source_revision=d.source_revision)))
 AND NOT EXISTS(SELECT 1 FROM public.agent_enrichment_purpose_previews p WHERE p.owner_id=d.subject_id AND p.agent_id=d.agent_id AND p.moment_id=d.source_id)
 AND NOT EXISTS(SELECT 1 FROM public.agent_candidate_retention_previews p WHERE p.owner_id=d.subject_id AND p.agent_id=d.agent_id AND(p.event_id=d.event_id OR p.logical_operation_id=d.logical_operation_id))
 AND NOT EXISTS(SELECT 1 FROM public.agent_multi_candidate_previews p WHERE p.owner_id=d.subject_id AND p.agent_id=d.agent_id AND(p.event_id=d.event_id OR p.logical_operation_id=d.logical_operation_id))
 AND NOT EXISTS(SELECT 1 FROM public.agent_enrichment_runs r WHERE r.owner_id=d.subject_id AND r.agent_id=d.agent_id
  AND(r.event_id=d.event_id OR r.logical_operation_id=d.logical_operation_id OR(r.source_id=d.source_id AND r.source_revision=d.source_revision)))`

func coalesceMomentRefreshTx(ctx context.Context, tx pgx.Tx, e agentoutbox.Envelope) error {
	// Creation/withdrawal remain original capture paths, including original
	// initial-capture clock/retry behavior. Never suppress a withdrawal event.
	if e.EventType != agentoutbox.MomentUpdated || e.Source.Revision <= 1 || e.Source.Status != agentoutbox.Draft {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SAVEPOINT birdtie_outbox_refresh_coalescing`); err != nil {
		return err
	}
	var ready bool
	err := tx.QueryRow(ctx, outboxRefreshCatalogSQL).Scan(&ready)
	if err == nil && ready {
		_, err = tx.Exec(ctx, outboxRefreshCoalesceSQL, e.EventID, e.Subject.ID, e.AgentID, e.Source.ID,
			e.Source.Revision, e.Source.Fingerprint, e.OccurredAt, e.ReceivedAt, e.ExpiresAt)
	}
	if err != nil {
		var native *pgconn.PgError
		if !errors.As(err, &native) || (native.Code != "42P01" && native.Code != "42703") {
			return err
		}
		// Only missing optional schema races are recoverable. Roll back this
		// optimization, not the preceding source change/new outbox insertion.
		if _, rollback := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT birdtie_outbox_refresh_coalescing`); rollback != nil {
			return rollback
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `RELEASE SAVEPOINT birdtie_outbox_refresh_coalescing`)
	return err
}
