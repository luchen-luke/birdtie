package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentattention"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const notificationPolicyColumns = `version,agent_id,enabled,default_route,rules,pause_until,expires_at,updated_at`

func notificationError(err error) error {
	switch {
	case errors.Is(err, agentprofile.ErrInvalid):
		return agentnotification.ErrInvalid
	case errors.Is(err, agentprofile.ErrForbidden):
		return agentnotification.ErrForbidden
	case errors.Is(err, agentprofile.ErrNotFound):
		return agentnotification.ErrNotFound
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && (pgerr.Code == "23505" || pgerr.Code == "40001" || pgerr.Code == "40P01") {
		return agentnotification.ErrConflict
	}
	return agentnotification.ErrUnavailable
}

func scanNotificationPolicy(row pgx.Row) (agentnotification.Policy, error) {
	p := agentnotification.Policy{SchemaVersion: agentnotification.SchemaVersion}
	var raw []byte
	err := row.Scan(&p.Version, &p.AgentID, &p.Enabled, &p.DefaultRoute, &raw, &p.PauseUntil, &p.ExpiresAt, &p.UpdatedAt)
	if err != nil {
		return agentnotification.Policy{}, err
	}
	decoded, decodeErr := agentnotification.DecodeRules(raw)
	p.Rules = decoded
	if decodeErr != nil || agentnotification.ValidatePolicy(p) != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	return p, nil
}

// Ordinary human self-management, not machine analysis/notification authority.
// Explicit READ COMMITTED prevents a caller's pool defaults freezing current
// authorization before the last source/session/clock verification.
func (s *Store) GetOwnNotificationPolicy(ctx context.Context, access agentprofile.PrivateAccess) (agentnotification.Policy, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	if _, err = lockAgentPrivateMetadata(ctx, tx, binding, false); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	p, err := scanNotificationPolicy(tx.QueryRow(ctx, `SELECT `+notificationPolicyColumns+` FROM native_notification_policies WHERE owner_id=$1 AND agent_id=$2 FOR SHARE`, binding.accountID, binding.agentID))
	if errors.Is(err, pgx.ErrNoRows) {
		p, err = agentnotification.DefaultPolicy(binding.agentID)
	}
	if err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	return p, nil
}

func (s *Store) PutOwnNotificationPolicy(ctx context.Context, access agentprofile.PrivateAccess, input agentnotification.PutInput) (agentnotification.Policy, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	if _, err = lockAgentPrivateMetadata(ctx, tx, binding, false); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	// Absent and present rows serialize on the same owner advisory key. The
	// native owner identity is server-resolved, not a client lock selector.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('native-notification-policy:'||$1::text,0))`, binding.accountID); err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	var current uint64
	err = tx.QueryRow(ctx, `SELECT version FROM native_notification_policies WHERE owner_id=$1 AND agent_id=$2 FOR UPDATE`, binding.accountID, binding.agentID).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	if current != input.ExpectedVersion || current == math.MaxInt64 {
		return agentnotification.Policy{}, agentnotification.ErrConflict
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	normalized, err := agentnotification.NormalizePutInput(input, now.UTC())
	if err != nil {
		return agentnotification.Policy{}, err
	}
	if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	rules, err := json.Marshal(normalized.Rules)
	if err != nil {
		return agentnotification.Policy{}, agentnotification.ErrInvalid
	}
	p, err := scanNotificationPolicy(tx.QueryRow(ctx, `INSERT INTO native_notification_policies
		(owner_id,agent_id,version,enabled,default_route,rules,pause_until,expires_at)
		VALUES($1,$2,1,$3,$4,$5,$6,$7)
		ON CONFLICT(owner_id) DO UPDATE SET version=native_notification_policies.version+1,
		 enabled=EXCLUDED.enabled,default_route=EXCLUDED.default_route,rules=EXCLUDED.rules,
		 pause_until=EXCLUDED.pause_until,expires_at=EXCLUDED.expires_at
		 WHERE native_notification_policies.agent_id=EXCLUDED.agent_id AND native_notification_policies.version=$8
		RETURNING `+notificationPolicyColumns, binding.accountID, binding.agentID, normalized.Enabled,
		normalized.DefaultRoute, rules, normalized.PauseUntil, normalized.ExpiresAt, current))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentnotification.Policy{}, agentnotification.ErrConflict
	}
	if err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	if err = insertDomainAudit(ctx, tx, binding.accountID, "replace", "notification_policy", binding.agentID, "human_notification_policy_edit", nil); err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	var sessionCurrent, policyFuture bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions ss WHERE ss.id=$4 AND ss.account_id=$1
	 AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp()),
	 EXISTS(SELECT 1 FROM native_notification_policies np WHERE np.owner_id=$1 AND np.agent_id=$2 AND np.version=$3
	 AND np.expires_at>clock_timestamp())`, binding.accountID, binding.agentID, p.Version, binding.sessionID).Scan(&sessionCurrent, &policyFuture); err != nil {
		return agentnotification.Policy{}, agentnotification.ErrUnavailable
	}
	if !sessionCurrent {
		return agentnotification.Policy{}, agentnotification.ErrForbidden
	}
	if !policyFuture {
		return agentnotification.Policy{}, agentnotification.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return agentnotification.Policy{}, notificationError(err)
	}
	return p, nil
}

// Only original native writers call this inside their existing transaction.
// Native SQL, not caller facts, resolves actor, audience, source and policy.
// Unknown/business-without-producer kinds fail closed. No payload is copied.
func routeNativeNotification(ctx context.Context, tx pgx.Tx, kind agentnotification.Kind, sourceID, recipientID string) (bool, error) {
	var isolation string
	if err := tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
		return false, err
	}
	if isolation != "read committed" {
		return false, agentnotification.ErrUnavailable
	}
	descriptor, err := agentnotification.LookupKind(kind)
	if err != nil {
		return false, err
	}
	var sourceColumn string
	switch kind {
	case agentnotification.KindOpportunityAvailable, agentnotification.KindActivityReminder, agentnotification.KindActivityChange, agentnotification.KindActivityCancelled:
		sourceColumn = "activity_id"
	case agentnotification.KindActivityReview:
		sourceColumn = "activity_candidate_id"
	case agentnotification.KindPlaceReview:
		sourceColumn = "place_candidate_id"
	case agentnotification.KindDirectMessage:
		sourceColumn = "message_id"
	case agentnotification.KindConnectionRequest, agentnotification.KindConnectionDecision:
		sourceColumn = "connection_request_id"
	case agentnotification.KindCommunityMessage:
		sourceColumn = "community_message_id"
	case agentnotification.KindActivityMessage:
		sourceColumn = "activity_message_id"
	case agentnotification.KindOrganizationInvitation, agentnotification.KindOrganizationMembershipChange:
		sourceColumn = "organization_membership_id"
	case agentnotification.KindAgentTaskCompleted, agentnotification.KindAgentTaskFailed:
		sourceColumn = "agent_task_id"
	case agentnotification.KindBusinessClaimReview:
		sourceColumn = "business_claim_id"
	case agentnotification.KindBusinessUpdate:
		sourceColumn = "business_public_update_id"
	default:
		return false, agentnotification.ErrUnavailable
	}
	// All interpolated SQL identifiers above are constants from the closed
	// server-only registry. No client selector or raw table/column is accepted.
	var decisionID string
	var route agentattention.Route
	err = tx.QueryRow(ctx, `WITH source AS MATERIALIZED (
		SELECT * FROM birdtie_native_notification_source($1,$2,$3)), preference AS MATERIALIZED (
		SELECT * FROM birdtie_native_notification_preference($3,$4))
		INSERT INTO native_notification_decisions(recipient_id,actor_id,kind,category,source_id,
		 event_version,source_version,policy_version,disposition,priority,reason,`+sourceColumn+`)
		SELECT $3,s.actor_id,$1,$4,$2,s.event_version,s.source_version,p.version,p.disposition,p.priority,p.reason,$2
		 FROM source s CROSS JOIN preference p
		ON CONFLICT(recipient_id,kind,source_id,event_version) DO NOTHING RETURNING id,disposition`,
		string(kind), sourceID, recipientID, string(descriptor.Category)).Scan(&decisionID, &route)
	if errors.Is(err, pgx.ErrNoRows) {
		// No current source or an already handled logical native event. Neither
		// case is authorization to replay cached content or re-ping a receiver.
		if kind == agentnotification.KindBusinessUpdate { return false, agentnotification.ErrUnavailable }
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if route != agentattention.Immediate && route != agentattention.Normal {
		return false, nil // DIGEST durable pending only; SILENT/BLOCK no Inbox.
	}
	return deliverNativeNotificationDecision(ctx, tx, decisionID, kind, sourceID, "", false)
}

// Reuses the exact old typed-target projection. A scheduled DIGEST must pass
// the new native current-slot predicate, not route mutation or client facts.
func deliverNativeNotificationDecision(ctx context.Context, tx pgx.Tx, decisionID string, kind agentnotification.Kind, sourceID, slotID string, dev bool) (bool, error) {
	descriptor, err := agentnotification.LookupKind(kind)
	if err != nil {
		return false, err
	}
	resourceType, resourceID := string(kind), decisionID
	detail := descriptor.Detail
	var targetActivity, targetConversation *string
	switch kind {
	case agentnotification.KindActivityReminder:
		resourceType, resourceID = "activity_reminder", sourceID
		targetActivity = &sourceID
	case agentnotification.KindOpportunityAvailable, agentnotification.KindActivityChange, agentnotification.KindActivityCancelled:
		targetActivity = &sourceID
	case agentnotification.KindActivityReview:
		resourceType, resourceID = "activity_candidate", sourceID
		var status string
		if err = tx.QueryRow(ctx, `SELECT status FROM activity_candidates WHERE id=$1`, sourceID).Scan(&status); err != nil {
			return false, err
		}
		switch status {
		case "published":
			detail = "你的活动建议已发布，请查看记录。"
		case "rejected":
			detail = "你的活动建议未通过审核，请查看记录。"
		default:
			return false, agentnotification.ErrUnavailable
		}
	case agentnotification.KindPlaceReview:
		resourceType, resourceID = "place_candidate", sourceID
		var status string
		if err = tx.QueryRow(ctx, `SELECT status FROM place_candidates WHERE id=$1`, sourceID).Scan(&status); err != nil {
			return false, err
		}
		switch status {
		case "published":
			detail = "你的地点建议已发布，请查看记录。"
		case "linked_duplicate":
			detail = "你的地点建议已关联现有地点，请查看记录。"
		case "rejected":
			detail = "你的地点建议未通过审核，请查看记录。"
		default:
			return false, agentnotification.ErrUnavailable
		}
	case agentnotification.KindConnectionRequest, agentnotification.KindConnectionDecision:
		resourceType, resourceID = "connection_request", sourceID
	case agentnotification.KindDirectMessage:
		resourceType, resourceID = "conversation_message", sourceID
		if err = tx.QueryRow(ctx, `SELECT conversation_id FROM conversation_messages WHERE id=$1`, sourceID).Scan(&targetConversation); err != nil {
			return false, err
		}
	case agentnotification.KindActivityMessage:
		if err = tx.QueryRow(ctx, `SELECT cv.activity_id FROM activity_conversation_messages m
		 JOIN activity_conversations cv ON cv.id=m.conversation_id WHERE m.id=$1`, sourceID).Scan(&targetActivity); err != nil {
			return false, err
		}
	case agentnotification.KindOrganizationInvitation, agentnotification.KindOrganizationMembershipChange:
		resourceType = "organization_membership"
	case agentnotification.KindAgentTaskCompleted, agentnotification.KindAgentTaskFailed:
		resourceType = "agent_task"
	}
	visibility := "birdtie_native_notification_visible(d)"
	args := []any{decisionID, descriptor.LegacyCategory, descriptor.Title, detail, resourceType, resourceID, targetActivity, targetConversation}
	if slotID != "" {
		visibility = "birdtie_notification_schedule_delivery_allowed($9,d,$10)"
		args = append(args, slotID, dev)
	}
	command, err := tx.Exec(ctx, `INSERT INTO inbox_items(recipient_account_id,category,title,detail,resource_type,resource_id,
		 target_activity_id,target_conversation_id,routing_decision_id)
		SELECT d.recipient_id,$2,$3,$4,$5,$6,$7,$8,d.id FROM native_notification_decisions d
		 WHERE d.id=$1 AND `+visibility+`
		ON CONFLICT(recipient_account_id,resource_type,resource_id) DO NOTHING`, args...)
	if err != nil {
		return false, err
	}
	return command.RowsAffected() == 1, nil
}

func routeNativeChatRecipients(ctx context.Context, tx pgx.Tx, kind agentnotification.Kind, sourceID, conversationID, senderID string) error {
	table := "community_conversation_members"
	if kind == agentnotification.KindActivityMessage {
		table = "activity_conversation_members"
	} else if kind != agentnotification.KindCommunityMessage {
		return agentnotification.ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT account_id FROM `+table+` WHERE conversation_id=$1 AND status='active' AND account_id<>$2 ORDER BY account_id`, conversationID, senderID)
	if err != nil {
		return err
	}
	var recipients []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, id := range recipients {
		if _, err = routeNativeNotification(ctx, tx, kind, sourceID, id); err != nil {
			return err
		}
	}
	return nil
}
