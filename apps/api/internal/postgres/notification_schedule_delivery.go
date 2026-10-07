package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ ns.Store = (*Store)(nil)
var _ ns.RunnerStore = (*Store)(nil)

// Owner Account precedes relation waits, matching existing receiver writers;
// Session/metadata/schedule follows those waits. Conservative source locks
// preserve the old SQL ACL/fingerprint across delivery. Actual PG
// lock/concurrency/performance checks are separate, not proved by unit spies.
const notificationScheduleSourceLocks = `LOCK TABLE accounts,agents,agent_profiles,sessions,
 native_notification_policies,account_blocks,native_notification_decisions,
 activities,activity_participations,activity_organizers,activity_invitations,cities,places,
 communities,community_memberships,organizations,organization_memberships,
 businesses,business_memberships,business_venue_relations,venues,venue_candidates,
 social_intents,social_intent_audience_targets,contexts,
 connection_requests,person_ties,conversations,conversation_messages,
 community_conversations,community_conversation_members,community_conversation_messages,
 activity_conversations,activity_conversation_members,activity_conversation_messages,
 agent_tasks,activity_candidates,place_candidates,business_claim_controls,
 business_review_grants,business_console_audit_events,business_console_profiles,
 business_public_profile_permissions,business_public_profile_audit,follows IN SHARE MODE`

func (s *Store) ProcessNotificationSchedules(ctx context.Context, owners int) (ns.RunResult, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || owners < 1 || owners > ns.MaxOwnersPerRun {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	return processNotificationScheduleOwners(ctx, s.pool, s.processNotificationScheduleOwner, owners, s.devPhoneEnabled)
}

type notificationScheduleOwnerQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// This is the original production nomination and batch loop, not a second
// scheduler. Current eligibility is checked before LIMIT; per-owner native
// locking, source/version/clock checks and commit behavior still decide delivery.
func processNotificationScheduleOwners(ctx context.Context, query notificationScheduleOwnerQuery, processOwner func(context.Context, string) (ns.RunResult, error), owners int, dev bool) (ns.RunResult, error) {
	var out ns.RunResult
	if ctx == nil || ctx.Err() != nil || query == nil || processOwner == nil || owners < 1 || owners > ns.MaxOwnersPerRun {
		return out, ns.ErrUnavailable
	}
	// Only explicitly selected, currently usable plans. Not a daily task for
	// every account; no model, Task, AIR event or automatic consent is created.
	rows, e := query.Query(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT p.owner_id FROM native_notification_schedules p
 JOIN accounts a ON a.id=p.owner_id AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.id=p.agent_id AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.id=p.session_id AND ss.account_id=a.id CROSS JOIN stamp
 CROSS JOIN LATERAL(SELECT birdtie_notification_schedule_wall_time(p.settings,(stamp.n AT TIME ZONE (p.settings->>'timeZone'))::date) AS due_at) due
	 WHERE (p.settings->>'enabled')::boolean AND (p.settings->>'maxContactsPerDay')::int>0 AND p.expires_at>stamp.n
	 AND ss.revoked_at IS NULL AND ss.expires_at>stamp.n AND ss.idle_expires_at>stamp.n
 AND ($2::boolean OR ss.authentication_method<>'dev_phone')
 AND NOT EXISTS(SELECT 1 FROM native_notification_schedule_slots slot WHERE slot.owner_id=p.owner_id
	 AND slot.local_date=(stamp.n AT TIME ZONE (p.settings->>'timeZone'))::date)
 AND due.due_at IS NOT NULL AND due.due_at>=p.valid_from AND due.due_at<=stamp.n
 AND NOT birdtie_notification_schedule_quiet(p.settings,stamp.n) AND NOT birdtie_notification_schedule_quiet(p.settings,due.due_at)
 ORDER BY p.updated_at,p.owner_id LIMIT $1`, owners, dev)
	if e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return ns.RunResult{}, ns.ErrUnavailable
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ns.RunResult{}, ns.ErrUnavailable
		}
		result, e := processOwner(ctx, id)
		if e != nil {
			return ns.RunResult{}, e
		}
		out.Owners++
		out.Slots += result.Slots
		out.Delivered += result.Delivered
		out.Discarded += result.Discarded
	}
	return out, nil
}
func (s *Store) processNotificationScheduleOwner(ctx context.Context, owner string) (ns.RunResult, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	return processNotificationScheduleOwnerTx(ctx, tx, owner, s.devPhoneEnabled)
}
func processNotificationScheduleOwnerTx(ctx context.Context, tx pgx.Tx, owner string, dev bool) (ns.RunResult, error) {
	var out ns.RunResult
	var e error
	var account string
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR NO KEY UPDATE`, owner).Scan(&account)
	if errors.Is(e, pgx.ErrNoRows) {
		// The nominated owner retired before admission. No slot, Inbox,
		// audit or commit has occurred; outer rollback releases this read.
		return out, nil
	}
	if e == nil && account != owner {
		return out, ns.ErrDenied
	}
	if e != nil {
		return out, ns.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, notificationScheduleSourceLocks); e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('notification-schedule:'||$1::text,0))`, owner); e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	p, e := scanNotificationSchedule(tx.QueryRow(ctx, `SELECT `+notificationScheduleColumns+` FROM native_notification_schedules p WHERE p.owner_id=$1 FOR UPDATE OF p`, owner))
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return ns.RunResult{}, notificationScheduleError(e)
	}
	var token, session, agent string
	var at time.Time
	e = tx.QueryRow(ctx, `SELECT p.xmin::text,p.session_id,p.agent_id,clock_timestamp() FROM native_notification_schedules p
 JOIN accounts a ON a.id=p.owner_id AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.id=p.agent_id AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.id=p.session_id AND ss.account_id=a.id
 WHERE p.owner_id=$1 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp()
 AND ($2::boolean OR ss.authentication_method<>'dev_phone') FOR SHARE OF a,ag,ap,ss`, owner, dev).Scan(&token, &session, &agent, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	if agent != p.AgentID {
		return out, ns.ErrChanged
	}
	due, ok, e := ns.Due(p, at.UTC())
	if e != nil {
		return ns.RunResult{}, e
	}
	if !ok {
		return out, nil
	}
	var slot string
	e = tx.QueryRow(ctx, `INSERT INTO native_notification_schedule_slots(owner_id,agent_id,session_id,schedule_version,schedule_token,local_date,due_at)
 VALUES($1,$2,$3,$4,$5,$6::date,$7) ON CONFLICT(owner_id,local_date) DO NOTHING RETURNING id`, owner, agent, session, p.Version, token, due.LocalDate, due.DueAt).Scan(&slot)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return ns.RunResult{}, notificationScheduleError(e)
	}
	var used int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM native_notification_schedule_deliveries WHERE owner_id=$1 AND delivered AND created_at>clock_timestamp()-interval '24 hours'`, owner).Scan(&used); e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	allowance := notificationScheduleAllowance(p.MaxContactsPerDay, used)
	if allowance > 0 {
		rows, e := tx.Query(ctx, `SELECT d.id,d.kind,d.source_id FROM native_notification_decisions d WHERE d.recipient_id=$1 AND d.disposition='DIGEST'
 AND birdtie_notification_digest_current(d) AND d.category=ANY($2::text[])
 AND NOT EXISTS(SELECT 1 FROM native_notification_schedule_deliveries receipt WHERE receipt.decision_id=d.id)
 ORDER BY d.created_at,d.id LIMIT $3 FOR SHARE OF d`, owner, p.Categories, allowance)
		if e != nil {
			return ns.RunResult{}, ns.ErrUnavailable
		}
		var candidates [][3]string
		for rows.Next() {
			var c [3]string
			if e = rows.Scan(&c[0], &c[1], &c[2]); e != nil {
				rows.Close()
				return ns.RunResult{}, ns.ErrUnavailable
			}
			candidates = append(candidates, c)
		}
		rows.Close()
		if rows.Err() != nil {
			return ns.RunResult{}, ns.ErrUnavailable
		}
		for _, c := range candidates {
			delivered, e := deliverScheduledNotificationDecision(ctx, tx, owner, slot, c[0], agentnotification.Kind(c[1]), c[2], dev)
			if e != nil {
				return ns.RunResult{}, notificationScheduleError(e)
			}
			if delivered {
				out.Delivered++
			} else {
				out.Discarded++
			}
		}
	}
	if e = insertDomainAudit(ctx, tx, owner, "deliver", "notification_schedule", slot, "native_notification_schedule_delivery", nil); e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	if e = finishNotificationScheduleSlot(ctx, tx, slot, dev); e != nil {
		return ns.RunResult{}, e
	}
	state := "EMPTY"
	if out.Delivered > 0 {
		state = "COMPLETED"
	} else if allowance == 0 {
		state = "BUDGET_FULL"
	}
	if _, e = tx.Exec(ctx, `UPDATE native_notification_schedule_slots SET state=$2 WHERE id=$1 AND state='CLAIMED'`, slot, state); e != nil {
		return ns.RunResult{}, notificationScheduleError(e)
	}
	if e = tx.Commit(ctx); e != nil {
		return ns.RunResult{}, ns.ErrUnavailable
	}
	out.Slots = 1
	return out, nil
}

// Rolling 24-hour receipts are independent of editable zone/date/revision.
func notificationScheduleAllowance(limit, used int) int {
	if limit < 0 || limit > ns.MaxContactsPerDay || used < 0 || used >= limit {
		return 0
	}
	return limit - used
}
func deliverScheduledNotificationDecision(ctx context.Context, tx pgx.Tx, owner, slot, decision string, kind agentnotification.Kind, source string, dev bool) (bool, error) {
	delivered, e := deliverNativeNotificationDecision(ctx, tx, decision, kind, source, slot, dev)
	if e != nil {
		return false, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO native_notification_schedule_deliveries(owner_id,slot_id,decision_id,inbox_item_id,delivered)
 SELECT $1,$2,d.id,CASE WHEN $4::boolean THEN (SELECT i.id FROM inbox_items i WHERE i.routing_decision_id=d.id AND i.recipient_account_id=$1) ELSE NULL END,$4
 FROM native_notification_decisions d WHERE d.id=$3 AND d.recipient_id=$1`, owner, slot, decision, delivered)
	return delivered, e
}
func finishNotificationScheduleSlot(ctx context.Context, tx pgx.Tx, slot string, dev bool) error {
	var current bool
	e := tx.QueryRow(ctx, `SELECT birdtie_notification_schedule_current(slot,$2)
 AND NOT EXISTS(SELECT 1 FROM native_notification_schedule_deliveries receipt
 JOIN native_notification_decisions d ON d.id=receipt.decision_id WHERE receipt.slot_id=slot.id AND receipt.delivered
 AND (NOT birdtie_notification_digest_current(d) OR NOT EXISTS(SELECT 1 FROM inbox_items i WHERE i.id=receipt.inbox_item_id AND i.routing_decision_id=d.id AND i.recipient_account_id=slot.owner_id)))
 FROM native_notification_schedule_slots slot WHERE slot.id=$1 AND slot.state='CLAIMED'`, slot, dev).Scan(&current)
	if e != nil {
		return ns.ErrUnavailable
	}
	if !current {
		return ns.ErrChanged
	}
	return nil
}
